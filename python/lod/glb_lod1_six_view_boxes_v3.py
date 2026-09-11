#!/usr/bin/env python3
"""Create six-view textured LOD1 proxy boxes with Blender.

Run with Blender, not the system Python:

    blender --background --python glb_lod1_six_view_boxes.py -- input \
        --output output --slice-count 32 --overwrite

For each input GLB the script:
  1. imports the original textured model;
  2. reproduces the supplied Go single-pole/long-rod/sliced-box strategy;
  3. renders +X, -X, +Y, -Y, +Z and -Z screenshots;
  4. maps each screenshot region onto the matching outward box face;
  5. exports an embedded-texture GLB with the original filename.

Only models recognized as two-post gantries use transparent screenshots. Their
transparent pixels are converted to a binary alpha mask (default cutoff 0.10),
which is more stable than alpha blending for overlapping box faces in Cesium.
All other strategies keep the configured opaque background. Use
--opaque-background to force even gantries to use an opaque background.

The Go code treats glTF Y as vertical. Blender uses Z as vertical, so all
analysis and proxy construction explicitly convert between those coordinates.
"""

from __future__ import annotations

import argparse
import math
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

import bpy
from mathutils import Matrix, Vector


Vec = tuple[float, float, float]


@dataclass
class Options:
    slice_count: int = 32
    min_slice_vertices: int = 3
    min_thickness_y: float = 0.05
    merge_similar: bool = True
    width_tolerance: float = 0.08
    height_tolerance: float = 0.08


@dataclass
class Box:
    minimum: Vec
    maximum: Vec


@dataclass
class Slice:
    has: bool = False
    count: int = 0
    minimum: Vec = (math.inf, math.inf, math.inf)
    maximum: Vec = (-math.inf, -math.inf, -math.inf)


def vmin(a: Vec, b: Vec) -> Vec:
    return tuple(min(a[i], b[i]) for i in range(3))


def vmax(a: Vec, b: Vec) -> Vec:
    return tuple(max(a[i], b[i]) for i in range(3))


def add(a: Vec, b: Vec) -> Vec:
    return tuple(a[i] + b[i] for i in range(3))


def sub(a: Vec, b: Vec) -> Vec:
    return tuple(a[i] - b[i] for i in range(3))


def mul(a: Vec, value: float) -> Vec:
    return tuple(component * value for component in a)


def dot(a: Vec, b: Vec) -> float:
    return sum(a[i] * b[i] for i in range(3))


def cross(a: Vec, b: Vec) -> Vec:
    return (a[1] * b[2] - a[2] * b[1],
            a[2] * b[0] - a[0] * b[2],
            a[0] * b[1] - a[1] * b[0])


def bounds(vertices: list[Vec]) -> Box:
    minimum = maximum = vertices[0]
    for vertex in vertices[1:]:
        minimum, maximum = vmin(minimum, vertex), vmax(maximum, vertex)
    return Box(minimum, maximum)


def gltf_to_blender(value: Vec) -> Vec:
    # glTF Y-up -> Blender Z-up.
    return value[0], -value[2], value[1]


def blender_to_gltf(value: Vector) -> Vec:
    return float(value.x), float(value.z), -float(value.y)


def reset_scene() -> None:
    bpy.ops.object.select_all(action="SELECT")
    bpy.ops.object.delete(use_global=False)
    for collection in (bpy.data.meshes, bpy.data.curves, bpy.data.cameras,
                       bpy.data.lights, bpy.data.materials, bpy.data.images):
        for block in list(collection):
            if block.users == 0:
                collection.remove(block)


def import_glb(path: Path) -> tuple[list[object], list[Vec]]:
    bpy.ops.import_scene.gltf(filepath=str(path))
    mesh_objects = [obj for obj in bpy.context.scene.objects
                    if obj.type == "MESH" and len(obj.data.vertices)]
    vertices: list[Vec] = []
    for obj in mesh_objects:
        matrix = obj.matrix_world
        vertices.extend(blender_to_gltf(matrix @ vertex.co) for vertex in obj.data.vertices)
    if not vertices:
        raise ValueError("model contains no mesh vertices")
    return mesh_objects, vertices


def detect_single_pole(vertices: list[Vec], opt: Options) -> list[Box] | None:
    whole = bounds(vertices)
    sx, sy, sz = sub(whole.maximum, whole.minimum)
    if sy <= 0:
        return None
    split_y = whole.minimum[1] + sy * 0.45
    lower = [v for v in vertices if v[1] < split_y]
    upper = [v for v in vertices if v[1] >= split_y]
    if len(lower) < opt.min_slice_vertices or len(upper) < opt.min_slice_vertices:
        return None
    lower_box, upper_box = bounds(lower), bounds(upper)
    lx, _, lz = sub(lower_box.maximum, lower_box.minimum)
    ux, uy, uz = sub(upper_box.maximum, upper_box.minimum)
    if not (max(ux, uz) > max(sx, sz) * 0.45 and
            max(lx, lz) < max(sx, sz) * 0.25 and
            uy < sy * 0.55 and (ux > lx * 3.0 or uz > lz * 3.0)):
        return None

    pole_min, pole_max = list(lower_box.minimum), list(lower_box.maximum)
    minimum_size = max(max(sx, sz) * 0.035, opt.min_thickness_y)
    for axis in (0, 2):
        if pole_max[axis] - pole_min[axis] < minimum_size:
            center = (pole_min[axis] + pole_max[axis]) * 0.5
            pole_min[axis], pole_max[axis] = center - minimum_size * 0.5, center + minimum_size * 0.5
    return [upper_box, Box(tuple(pole_min), tuple(pole_max))]


def detect_two_post_gantry(vertices: list[Vec], opt: Options) -> Box | None:
    """Detect a portal frame with two end posts and a spanning upper beam.

    Vertex count alone is deliberately not used as the primary signal.  The
    test combines proportions with repeated occupancy at both ends over five
    lower height bands, an empty lower middle, and a top section spanning most
    of the model width.  This prevents an ordinary thin cuboid from matching
    merely because its corner vertices happen to lie at both ends.
    """
    whole = bounds(vertices)
    sx, sy, sz = sub(whole.maximum, whole.minimum)
    if min(sx, sy, sz) <= 0.0:
        return None

    length_axis = 0 if sx >= sz else 2
    span = sx if length_axis == 0 else sz
    depth = sz if length_axis == 0 else sx

    # A gantry is horizontally spanning, vertically substantial, and shallow
    # compared with its span. These limits intentionally remain permissive for
    # different road widths and portal-frame designs.
    if span < sy * 1.20 or span < depth * 4.0 or sy < depth * 3.0:
        return None

    axis_min = whole.minimum[length_axis]
    y_min = whole.minimum[1]

    def in_left(vertex: Vec) -> bool:
        return vertex[length_axis] <= axis_min + span * 0.22

    def in_right(vertex: Vec) -> bool:
        return vertex[length_axis] >= axis_min + span * 0.78

    def in_center(vertex: Vec) -> bool:
        value = vertex[length_axis]
        return axis_min + span * 0.35 <= value <= axis_min + span * 0.65

    # Detailed posts have vertices in several successive lower height bands.
    # Low-poly posts, however, may be plain cuboids with vertices only at their
    # ends. Both forms are supported below.
    occupied_left = occupied_right = 0
    lower_left = lower_right = lower_center = 0
    for band in range(5):
        low = y_min + sy * (band * 0.12)
        high = y_min + sy * ((band + 1) * 0.12)
        band_vertices = [v for v in vertices if low <= v[1] < high]
        left_count = sum(1 for v in band_vertices if in_left(v))
        right_count = sum(1 for v in band_vertices if in_right(v))
        center_count = sum(1 for v in band_vertices if in_center(v))
        if left_count >= opt.min_slice_vertices:
            occupied_left += 1
        if right_count >= opt.min_slice_vertices:
            occupied_right += 1
        lower_left += left_count
        lower_right += right_count
        lower_center += center_count

    if min(lower_left, lower_right) < opt.min_slice_vertices * 2:
        return None
    if lower_center > (lower_left + lower_right) * 0.12:
        return None

    continuous_posts = occupied_left >= 3 and occupied_right >= 3
    # Three simple cuboids (two posts and one beam) already contribute at least
    # 24 vertices. Requiring that minimum keeps an ordinary eight-vertex box
    # from matching the sparse-topology fallback.
    sparse_posts = (occupied_left >= 1 and occupied_right >= 1 and
                    len(vertices) >= 24)
    if not continuous_posts and not sparse_posts:
        return None

    # The upper part must contain a beam covering at least 72% of the overall
    # span and must also have geometry in the middle, not only post tops.
    upper = [v for v in vertices if v[1] >= y_min + sy * 0.68]
    if len(upper) < opt.min_slice_vertices:
        return None
    upper_min = min(v[length_axis] for v in upper)
    upper_max = max(v[length_axis] for v in upper)
    upper_center = sum(1 for v in upper if in_center(v))
    if upper_max - upper_min < span * 0.72:
        return None
    # A subdivided/detail beam must visibly occupy the middle. A low-poly beam
    # can span the middle geometrically while having vertices only at its two
    # ends, so the sparse structural fallback is allowed to omit centre verts.
    if upper_center < opt.min_slice_vertices and not sparse_posts:
        return None

    return whole


def is_long_rod(model_bounds: Box) -> bool:
    lengths = sub(model_bounds.maximum, model_bounds.minimum)
    if min(lengths) <= 0 or math.prod(lengths) > 5.0:
        return False
    minimum, middle, maximum = sorted(lengths)
    return maximum / middle >= 3.0 and maximum / minimum >= 6.0


def can_merge(a: Box, b: Box, use_x: bool, opt: Options) -> bool:
    width_axis = 2 if use_x else 0
    aw = a.maximum[width_axis] - a.minimum[width_axis]
    bw = b.maximum[width_axis] - b.minimum[width_axis]
    ah = a.maximum[1] - a.minimum[1]
    bh = b.maximum[1] - b.minimum[1]
    return (abs(aw - bw) / max(aw, bw, 0.0001) <= opt.width_tolerance and
            abs(ah - bh) / max(ah, bh, 0.0001) <= opt.height_tolerance)


def merge_boxes(boxes: list[Box], use_x: bool, opt: Options) -> list[Box]:
    if len(boxes) <= 1:
        return boxes
    result: list[Box] = []
    current = boxes[0]
    for following in boxes[1:]:
        if can_merge(current, following, use_x, opt):
            current = Box(vmin(current.minimum, following.minimum),
                          vmax(current.maximum, following.maximum))
        else:
            result.append(current)
            current = following
    result.append(current)
    return result


def outline_boxes(vertices: list[Vec], opt: Options) -> list[Box]:
    whole = bounds(vertices)
    sx, sy, sz = sub(whole.maximum, whole.minimum)
    use_x = sx >= sz
    axis, length = (0, sx) if use_x else (2, sz)
    if length == 0:
        return []
    slices = [Slice() for _ in range(opt.slice_count)]
    for vertex in vertices:
        t = (vertex[axis] - whole.minimum[axis]) / length
        index = min(max(int(t * opt.slice_count), 0), opt.slice_count - 1)
        stat = slices[index]
        stat.has, stat.count = True, stat.count + 1
        stat.minimum, stat.maximum = vmin(stat.minimum, vertex), vmax(stat.maximum, vertex)

    base_min, base_max = list(whole.minimum), list(whole.maximum)
    base_max[1] = whole.minimum[1] + sy * 0.18
    if base_max[1] <= whole.minimum[1]:
        base_max[1] = whole.minimum[1] + opt.min_thickness_y
    width_axis = 2 if use_x else 0
    valid = [s for s in slices if s.has and s.count >= opt.min_slice_vertices]
    if valid:
        minimum_width = min(s.minimum[width_axis] for s in valid)
        maximum_width = max(s.maximum[width_axis] for s in valid)
        if minimum_width < maximum_width:
            base_min[width_axis], base_max[width_axis] = minimum_width, maximum_width
    base = Box(tuple(base_min), tuple(base_max))
    result = [base]

    for index, stat in enumerate(slices):
        if not stat.has or stat.count < opt.min_slice_vertices:
            continue
        minimum, maximum = list(stat.minimum), list(stat.maximum)
        minimum[axis] = whole.minimum[axis] + index / opt.slice_count * length
        maximum[axis] = whole.minimum[axis] + (index + 1) / opt.slice_count * length
        if maximum[1] <= base.maximum[1] + opt.min_thickness_y:
            continue
        minimum[1] = base.maximum[1]
        if maximum[1] - minimum[1] < opt.min_thickness_y:
            maximum[1] = minimum[1] + opt.min_thickness_y
        result.append(Box(tuple(minimum), tuple(maximum)))
    if opt.merge_similar and len(result) > 1:
        result = [result[0], *merge_boxes(result[1:], use_x, opt)]
    return result


def build_boxes(vertices: list[Vec], opt: Options) -> tuple[list[Box], str]:
    gantry = detect_two_post_gantry(vertices, opt)
    if gantry is not None:
        return [gantry], "two_post_gantry"
    detected = detect_single_pole(vertices, opt)
    if detected is not None:
        return detected, "single_pole_sign"
    whole = bounds(vertices)
    if is_long_rod(whole):
        return [whole], "long_rod"
    return outline_boxes(vertices, opt), "outline_slices"


# Camera view name -> (outward face normal, camera up), in glTF coordinates.
VIEWS: dict[str, tuple[Vec, Vec]] = {
    "right_px": ((1, 0, 0), (0, 1, 0)),
    "left_nx": ((-1, 0, 0), (0, 1, 0)),
    "top_py": ((0, 1, 0), (0, 0, 1)),
    "bottom_ny": ((0, -1, 0), (0, 0, 1)),
    "front_pz": ((0, 0, 1), (0, 1, 0)),
    "back_nz": ((0, 0, -1), (0, 1, 0)),
}


def view_basis(normal: Vec, up: Vec) -> tuple[Vec, Vec, Vec]:
    direction = mul(normal, -1.0)  # camera-to-model direction
    right = cross(direction, up)
    return direction, right, up


def setup_render(resolution: int, background: float, transparent: bool) -> object:
    scene = bpy.context.scene
    for engine in ("BLENDER_EEVEE_NEXT", "BLENDER_EEVEE"):
        try:
            scene.render.engine = engine
            break
        except TypeError:
            continue
    scene.render.resolution_x = resolution
    scene.render.resolution_y = resolution
    scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = "PNG"
    scene.render.film_transparent = transparent
    scene.render.image_settings.color_mode = "RGBA"
    scene.world.use_nodes = True
    bg = scene.world.node_tree.nodes.get("Background")
    bg.inputs["Color"].default_value = (background, background, background, 1.0)
    bg.inputs["Strength"].default_value = 0.8
    try:
        scene.view_settings.look = "Medium High Contrast"
    except (TypeError, AttributeError):
        pass

    camera_data = bpy.data.cameras.new("lod1_capture_camera")
    camera = bpy.data.objects.new("lod1_capture_camera", camera_data)
    scene.collection.objects.link(camera)
    camera_data.type = "ORTHO"
    scene.camera = camera

    # Broad area lights preserve the imported base-colour textures while
    # avoiding a strongly directional dark side in the six captures.
    for index, position in enumerate(((8, -8, 10), (-8, -8, 6), (0, 8, 10))):
        data = bpy.data.lights.new(f"lod1_light_{index}", "AREA")
        data.energy, data.shape, data.size = 700.0, "DISK", 8.0
        obj = bpy.data.objects.new(f"lod1_light_{index}", data)
        scene.collection.objects.link(obj)
        obj.location = position
        obj.rotation_euler = ((Vector((0, 0, 0)) - obj.location).to_track_quat("-Z", "Y").to_euler())
    return camera


def render_views(model_bounds: Box, directory: Path, resolution: int,
                 background: float, transparent: bool) -> tuple[dict[str, Path], dict[str, float]]:
    camera = setup_render(resolution, background, transparent)
    center = mul(add(model_bounds.minimum, model_bounds.maximum), 0.5)
    extents = sub(model_bounds.maximum, model_bounds.minimum)
    captures: dict[str, Path] = {}
    scales: dict[str, float] = {}
    for name, (normal, up) in VIEWS.items():
        direction, right, camera_up = view_basis(normal, up)
        projected_width = sum(abs(right[i]) * extents[i] for i in range(3))
        projected_height = sum(abs(camera_up[i]) * extents[i] for i in range(3))
        scale = max(projected_width, projected_height, 1e-4) * 1.02
        distance = max(extents) * 2.5 + 1.0
        position = sub(center, mul(direction, distance))

        right_b = Vector(gltf_to_blender(right))
        up_b = Vector(gltf_to_blender(camera_up))
        direction_b = Vector(gltf_to_blender(direction))
        rotation = Matrix((right_b, up_b, -direction_b)).transposed().to_4x4()
        rotation.translation = Vector(gltf_to_blender(position))
        camera.matrix_world = rotation
        camera.data.ortho_scale = scale
        camera.data.clip_start = 0.001
        camera.data.clip_end = distance * 4.0 + 100.0

        path = directory / f"{name}.png"
        bpy.context.scene.render.filepath = str(path)
        bpy.ops.render.render(write_still=True)
        captures[name], scales[name] = path, scale
    return captures, scales


def capture_materials(captures: dict[str, Path], transparent: bool,
                      alpha_cutoff: float) -> dict[str, object]:
    materials: dict[str, object] = {}
    for name, path in captures.items():
        image = bpy.data.images.load(str(path), check_existing=False)
        image.pack()
        material = bpy.data.materials.new(f"lod1_capture_{name}")
        material.use_nodes = True
        nodes, links = material.node_tree.nodes, material.node_tree.links
        principled = nodes.get("Principled BSDF")
        texture = nodes.new("ShaderNodeTexImage")
        texture.image = image
        links.new(texture.outputs["Color"], principled.inputs["Base Color"])
        principled.inputs["Roughness"].default_value = 0.8
        if transparent:
            # Encode a binary alpha mask in the node graph. Blender 4.2+
            # glTF exporters recognize Greater Than -> Principled Alpha as
            # glTF alphaMode=MASK with the corresponding alphaCutoff.
            alpha_mask = nodes.new("ShaderNodeMath")
            alpha_mask.name = "LOD1 Alpha Mask"
            alpha_mask.label = f"Alpha > {alpha_cutoff:g}"
            alpha_mask.operation = "GREATER_THAN"
            alpha_mask.inputs[1].default_value = alpha_cutoff
            links.new(texture.outputs["Alpha"], alpha_mask.inputs[0])
            links.new(alpha_mask.outputs[0], principled.inputs["Alpha"])

            # Compatibility with Blender <= 4.1. Blender 4.2 removed
            # blend_method, so guard the legacy properties explicitly.
            if hasattr(material, "blend_method"):
                material.blend_method = "CLIP"
            if hasattr(material, "alpha_threshold"):
                material.alpha_threshold = alpha_cutoff
            # Eevee Next uses a different real-time surface setting. The node
            # graph above remains authoritative for the exported glTF.
            if hasattr(material, "surface_render_method"):
                try:
                    material.surface_render_method = "DITHERED"
                except (TypeError, ValueError):
                    pass
        materials[name] = material
    return materials


def create_textured_box(index: int, box: Box, model_bounds: Box,
                        scales: dict[str, float], materials: dict[str, object],
                        strategy: str) -> object:
    center = mul(add(box.minimum, box.maximum), 0.5)
    size = sub(box.maximum, box.minimum)
    # Match Go geometry but avoid invalid zero-area faces in GLB.
    size = tuple(max(component, 1e-6) for component in size)
    vertices: list[Vec] = []
    faces: list[tuple[int, int, int, int]] = []
    uvs: list[tuple[float, float]] = []
    view_names = list(VIEWS)
    model_center = mul(add(model_bounds.minimum, model_bounds.maximum), 0.5)

    for name in view_names:
        normal, up = VIEWS[name]
        _, right, camera_up = view_basis(normal, up)
        face_center = list(center)
        normal_axis = next(i for i, value in enumerate(normal) if value)
        face_center[normal_axis] = (box.maximum[normal_axis]
                                    if normal[normal_axis] > 0 else box.minimum[normal_axis])
        half_right = sum(abs(right[i]) * size[i] for i in range(3)) * 0.5
        half_up = sum(abs(camera_up[i]) * size[i] for i in range(3)) * 0.5
        corners = [
            add(add(tuple(face_center), mul(right, -half_right)), mul(camera_up, -half_up)),
            add(add(tuple(face_center), mul(right, half_right)), mul(camera_up, -half_up)),
            add(add(tuple(face_center), mul(right, half_right)), mul(camera_up, half_up)),
            add(add(tuple(face_center), mul(right, -half_right)), mul(camera_up, half_up)),
        ]
        base = len(vertices)
        vertices.extend(corners)
        faces.append((base, base + 1, base + 2, base + 3))
        scale = scales[name]
        for corner in corners:
            relative = sub(corner, model_center)
            uvs.append((0.5 + dot(relative, right) / scale,
                        0.5 + dot(relative, camera_up) / scale))

    mesh = bpy.data.meshes.new(f"lod1_{strategy}_{index}")
    mesh.from_pydata([gltf_to_blender(v) for v in vertices], [], faces)
    mesh.update()
    uv_layer = mesh.uv_layers.new(name="UVMap")
    for polygon in mesh.polygons:
        polygon.material_index = polygon.index
        for loop_index in polygon.loop_indices:
            vertex_index = mesh.loops[loop_index].vertex_index
            uv_layer.data[loop_index].uv = uvs[vertex_index]
    obj = bpy.data.objects.new(mesh.name, mesh)
    bpy.context.scene.collection.objects.link(obj)
    for name in view_names:
        mesh.materials.append(materials[name])
    return obj


def export_proxies(path: Path, proxies: list[object]) -> None:
    bpy.ops.object.select_all(action="DESELECT")
    for obj in proxies:
        obj.select_set(True)
    bpy.context.view_layer.objects.active = proxies[0]
    path.parent.mkdir(parents=True, exist_ok=True)
    bpy.ops.export_scene.gltf(
        filepath=str(path),
        export_format="GLB",
        use_selection=True,
        export_texcoords=True,
        export_normals=True,
        export_materials="EXPORT",
        export_image_format="AUTO",
    )


def convert(source: Path, destination: Path, opt: Options, resolution: int,
            background: float, allow_transparent_gantry: bool,
            alpha_cutoff: float) -> tuple[str, int]:
    reset_scene()
    original_objects, vertices = import_glb(source)
    proxy_boxes, strategy = build_boxes(vertices, opt)
    if not proxy_boxes:
        raise ValueError("proxy strategy produced no boxes")
    model_bounds = bounds(vertices)
    transparent = allow_transparent_gantry and strategy == "two_post_gantry"
    with tempfile.TemporaryDirectory(prefix="lod1_six_views_") as temporary:
        captures, scales = render_views(
            model_bounds, Path(temporary), resolution, background, transparent)
        materials = capture_materials(captures, transparent, alpha_cutoff)
        for obj in original_objects:
            obj.hide_render = True
        proxies = [create_textured_box(i, box, model_bounds, scales, materials, strategy)
                   for i, box in enumerate(proxy_boxes)]
        export_proxies(destination, proxies)
    return strategy, len(proxy_boxes)


def parse_args() -> argparse.Namespace:
    argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    parser = argparse.ArgumentParser(description="Generate six-view textured LOD1 box GLBs")
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, default=Path("output"))
    parser.add_argument("--recursive", "-r", action="store_true")
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument("--slice-count", type=int, default=32)
    parser.add_argument("--no-merge", action="store_true")
    parser.add_argument("--resolution", type=int, default=512,
                        help="resolution of each of the six captures (default: 512)")
    parser.add_argument("--background", type=float, default=0.65,
                        help="opaque capture background brightness (default: 0.65)")
    parser.add_argument("--opaque-background", action="store_true",
                        help="force gantries to use --background instead of transparency")
    parser.add_argument("--alpha-cutoff", type=float, default=0.10,
                        help="alpha mask cutoff for transparent pixels (default: 0.10)")
    return parser.parse_args(argv)


def main() -> int:
    args = parse_args()
    input_dir, output_dir = args.input_dir.resolve(), args.output.resolve()
    if not input_dir.is_dir():
        print(f"error: input directory does not exist: {input_dir}", file=sys.stderr)
        return 2
    if (args.slice_count <= 0 or args.resolution < 32 or
            not 0 <= args.background <= 1 or not 0 <= args.alpha_cutoff <= 1):
        print("error: invalid slice-count, resolution, background, or alpha-cutoff",
              file=sys.stderr)
        return 2
    pattern = "**/*.glb" if args.recursive else "*.glb"
    sources = sorted(path for path in input_dir.glob(pattern) if path.is_file())
    if not sources:
        print(f"no GLB files found in {input_dir}")
        return 0
    opt = Options(slice_count=args.slice_count, merge_similar=not args.no_merge)
    succeeded = skipped = 0
    for source in sources:
        relative, destination = source.relative_to(input_dir), output_dir / source.relative_to(input_dir)
        if destination.exists() and not args.overwrite:
            skipped += 1
            print(f"SKIP {relative} (already exists; use --overwrite)")
            continue
        try:
            strategy, count = convert(
                source, destination, opt, args.resolution, args.background,
                not args.opaque_background, args.alpha_cutoff)
            succeeded += 1
            texture_mode = ("transparent-mask" if strategy == "two_post_gantry"
                            and not args.opaque_background else "opaque-background")
            print(f"OK   {relative} -> {destination} "
                  f"[{strategy}, {count} boxes, {texture_mode}]")
        except Exception as exc:
            print(f"FAIL {relative}: {exc}", file=sys.stderr)
    failed = len(sources) - succeeded - skipped
    print(f"finished: {succeeded} converted, {skipped} skipped, {failed} failed")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

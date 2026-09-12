#!/usr/bin/env python3
"""Replace every Blender Mesh Object in each GLB with one six-view box.

Run this script with Blender rather than ordinary Python:

    blender --background --python glb_mesh_six_view_boxes.py -- input \
        --output output --resolution 512 --overwrite

Each imported Blender Mesh Object is isolated, rendered from six orthographic
directions, replaced by its world-space bounding box, and mapped with those six
captures. glTF primitives/material slots inside one Mesh remain one box.
"""

from __future__ import annotations

import argparse
import math
import re
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

import bpy
from mathutils import Matrix, Vector


Vec = tuple[float, float, float]


@dataclass
class Bounds:
    minimum: Vec
    maximum: Vec


@dataclass
class SourceMesh:
    obj: object
    name: str
    bounds: Bounds


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


def safe_name(value: str) -> str:
    value = re.sub(r"[^\w.-]+", "_", value, flags=re.UNICODE).strip("_.")
    return value or "mesh"


def object_bounds_gltf(obj: object) -> Bounds | None:
    if obj.type != "MESH" or len(obj.data.vertices) == 0:
        return None
    matrix = obj.matrix_world
    vertices = [blender_to_gltf(matrix @ vertex.co) for vertex in obj.data.vertices]
    minimum = tuple(min(vertex[i] for vertex in vertices) for i in range(3))
    maximum = tuple(max(vertex[i] for vertex in vertices) for i in range(3))
    if any(not math.isfinite(value) for value in (*minimum, *maximum)):
        return None
    return Bounds(minimum, maximum)


def import_source_meshes(path: Path) -> list[SourceMesh]:
    bpy.ops.import_scene.gltf(filepath=str(path))
    result: list[SourceMesh] = []
    for obj in bpy.context.scene.objects:
        model_bounds = object_bounds_gltf(obj)
        if model_bounds is None:
            continue
        result.append(SourceMesh(obj=obj, name=obj.name, bounds=model_bounds))
    if not result:
        raise ValueError("GLB contains no Blender Mesh Objects")
    return result


# View name -> (outward box normal, camera up), in glTF coordinates.
VIEWS: dict[str, tuple[Vec, Vec]] = {
    "right_px": ((1, 0, 0), (0, 1, 0)),
    "left_nx": ((-1, 0, 0), (0, 1, 0)),
    "top_py": ((0, 1, 0), (0, 0, 1)),
    "bottom_ny": ((0, -1, 0), (0, 0, 1)),
    "front_pz": ((0, 0, 1), (0, 1, 0)),
    "back_nz": ((0, 0, -1), (0, 1, 0)),
}


def view_basis(normal: Vec, up: Vec) -> tuple[Vec, Vec, Vec]:
    direction = mul(normal, -1.0)
    right = cross(direction, up)
    return direction, right, up


def setup_renderer(resolution: int, background: float,
                   transparent: bool) -> tuple[object, list[object]]:
    scene = bpy.context.scene
    engine_set = False
    for engine in ("BLENDER_EEVEE_NEXT", "BLENDER_EEVEE"):
        try:
            scene.render.engine = engine
            engine_set = True
            break
        except TypeError:
            continue
    if not engine_set:
        raise RuntimeError("no supported Eevee render engine is available")
    scene.render.resolution_x = resolution
    scene.render.resolution_y = resolution
    scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = "PNG"
    scene.render.image_settings.color_mode = "RGBA"
    scene.render.film_transparent = transparent
    scene.world.use_nodes = True
    world_background = scene.world.node_tree.nodes.get("Background")
    world_background.inputs["Color"].default_value = (
        background, background, background, 1.0)
    world_background.inputs["Strength"].default_value = 0.8

    camera_data = bpy.data.cameras.new("mesh_box_capture_camera")
    camera = bpy.data.objects.new("mesh_box_capture_camera", camera_data)
    scene.collection.objects.link(camera)
    camera_data.type = "ORTHO"
    scene.camera = camera

    lights: list[object] = []
    for index in range(3):
        data = bpy.data.lights.new(f"mesh_box_light_{index}", "AREA")
        data.energy, data.shape, data.size = 700.0, "DISK", 8.0
        light = bpy.data.objects.new(f"mesh_box_light_{index}", data)
        scene.collection.objects.link(light)
        lights.append(light)
    return camera, lights


def isolate_for_render(current: SourceMesh, sources: list[SourceMesh]) -> None:
    for source in sources:
        source.obj.hide_render = source is not current


def render_six_views(source: SourceMesh, sources: list[SourceMesh], camera: object,
                     lights: list[object], directory: Path) -> tuple[dict[str, Path], dict[str, float]]:
    isolate_for_render(source, sources)
    center = mul(add(source.bounds.minimum, source.bounds.maximum), 0.5)
    extents = sub(source.bounds.maximum, source.bounds.minimum)
    center_b = Vector(gltf_to_blender(center))
    light_distance = max(extents) * 1.5 + 2.0
    for light, offset in zip(lights, ((1, -1, 1.5), (-1, -1, 0.8), (0, 1, 1.3))):
        light.location = center_b + Vector(offset) * light_distance
        light.data.size = max(extents) + 1.0
        light.rotation_euler = (
            (center_b - light.location).to_track_quat("-Z", "Y").to_euler())
    captures: dict[str, Path] = {}
    scales: dict[str, float] = {}

    for view_name, (normal, up) in VIEWS.items():
        direction, right, camera_up = view_basis(normal, up)
        width = sum(abs(right[i]) * extents[i] for i in range(3))
        height = sum(abs(camera_up[i]) * extents[i] for i in range(3))
        scale = max(width, height, 1e-4) * 1.02
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

        capture_path = directory / f"{view_name}.png"
        bpy.context.scene.render.filepath = str(capture_path)
        bpy.ops.render.render(write_still=True)
        captures[view_name], scales[view_name] = capture_path, scale
    return captures, scales


def build_capture_materials(captures: dict[str, Path], prefix: str,
                            transparent: bool, alpha_cutoff: float,
                            emission_strength: float) -> dict[str, object]:
    result: dict[str, object] = {}
    for view_name, path in captures.items():
        image = bpy.data.images.load(str(path), check_existing=False)
        image.name = f"{prefix}_{view_name}"
        image.pack()
        material = bpy.data.materials.new(f"{prefix}_{view_name}")
        material.use_nodes = True
        nodes, links = material.node_tree.nodes, material.node_tree.links
        principled = nodes.get("Principled BSDF")
        texture = nodes.new("ShaderNodeTexImage")
        texture.image = image
        links.new(texture.outputs["Color"], principled.inputs["Base Color"])
        principled.inputs["Roughness"].default_value = 0.8
        # Blender 4.x/5.x calls this socket "Emission Color"; older Blender
        # versions use "Emission". Reuse the capture as the emissive texture so
        # the exported GLB stays readable in Cesium without changing its alpha.
        emission_color = (principled.inputs.get("Emission Color") or
                          principled.inputs.get("Emission"))
        emission_strength_input = principled.inputs.get("Emission Strength")
        if emission_color is not None and emission_strength > 0:
            links.new(texture.outputs["Color"], emission_color)
            if emission_strength_input is not None:
                emission_strength_input.default_value = emission_strength
        if transparent:
            mask = nodes.new("ShaderNodeMath")
            mask.operation = "GREATER_THAN"
            mask.inputs[1].default_value = alpha_cutoff
            links.new(texture.outputs["Alpha"], mask.inputs[0])
            links.new(mask.outputs[0], principled.inputs["Alpha"])
            if hasattr(material, "blend_method"):
                material.blend_method = "CLIP"
            if hasattr(material, "alpha_threshold"):
                material.alpha_threshold = alpha_cutoff
            if hasattr(material, "surface_render_method"):
                try:
                    material.surface_render_method = "DITHERED"
                except (TypeError, ValueError):
                    pass
        result[view_name] = material
    return result


def create_box(source: SourceMesh, index: int, scales: dict[str, float],
               materials: dict[str, object]) -> object:
    box = source.bounds
    center = mul(add(box.minimum, box.maximum), 0.5)
    size = tuple(max(value, 1e-6) for value in sub(box.maximum, box.minimum))
    vertices: list[Vec] = []
    faces: list[tuple[int, int, int, int]] = []
    uvs: list[tuple[float, float]] = []

    for view_name, (normal, up) in VIEWS.items():
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
        first = len(vertices)
        vertices.extend(corners)
        faces.append((first, first + 1, first + 2, first + 3))
        scale = scales[view_name]
        for corner in corners:
            relative = sub(corner, center)
            uvs.append((0.5 + dot(relative, right) / scale,
                        0.5 + dot(relative, camera_up) / scale))

    name = f"box_{index:04d}_{safe_name(source.name)}"
    mesh = bpy.data.meshes.new(name)
    mesh.from_pydata([gltf_to_blender(vertex) for vertex in vertices], [], faces)
    mesh.update()
    uv_layer = mesh.uv_layers.new(name="UVMap")
    for polygon in mesh.polygons:
        polygon.material_index = polygon.index
        for loop_index in polygon.loop_indices:
            vertex_index = mesh.loops[loop_index].vertex_index
            uv_layer.data[loop_index].uv = uvs[vertex_index]
    for view_name in VIEWS:
        mesh.materials.append(materials[view_name])
    obj = bpy.data.objects.new(name, mesh)
    bpy.context.scene.collection.objects.link(obj)
    return obj


def export_boxes(path: Path, boxes: list[object]) -> None:
    bpy.ops.object.select_all(action="DESELECT")
    for obj in boxes:
        obj.select_set(True)
    bpy.context.view_layer.objects.active = boxes[0]
    path.parent.mkdir(parents=True, exist_ok=True)
    bpy.ops.export_scene.gltf(
        filepath=str(path), export_format="GLB", use_selection=True,
        export_texcoords=True, export_normals=True,
        export_materials="EXPORT", export_image_format="AUTO")


def convert_file(source_path: Path, output_path: Path, resolution: int,
                 background: float, transparent: bool,
                 alpha_cutoff: float, emission_strength: float) -> int:
    reset_scene()
    sources = import_source_meshes(source_path)
    camera, lights = setup_renderer(resolution, background, transparent)
    boxes: list[object] = []
    with tempfile.TemporaryDirectory(prefix="mesh_six_views_") as temporary:
        root = Path(temporary)
        for index, source in enumerate(sources):
            mesh_dir = root / f"mesh_{index:04d}"
            mesh_dir.mkdir(parents=True)
            captures, scales = render_six_views(
                source, sources, camera, lights, mesh_dir)
            prefix = f"mesh_{index:04d}_{safe_name(source.name)}"
            materials = build_capture_materials(
                captures, prefix, transparent, alpha_cutoff,
                emission_strength)
            boxes.append(create_box(source, index, scales, materials))
        for source in sources:
            source.obj.hide_render = True
        export_boxes(output_path, boxes)
    return len(boxes)


def parse_args() -> argparse.Namespace:
    argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    parser = argparse.ArgumentParser(
        description="Replace every Blender Mesh Object with a six-view textured box")
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, default=Path("output"))
    parser.add_argument("--recursive", "-r", action="store_true")
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument("--resolution", type=int, default=512)
    parser.add_argument("--background", type=float, default=0.65)
    parser.add_argument("--transparent-background", action="store_true")
    parser.add_argument("--alpha-cutoff", type=float, default=0.10)
    parser.add_argument(
        "--emission-strength", type=float, default=0.35,
        help="emission strength applied to six-view textures (default: 0.35)")
    return parser.parse_args(argv)


def main() -> int:
    args = parse_args()
    input_dir, output_dir = args.input_dir.resolve(), args.output.resolve()
    if not input_dir.is_dir():
        print(f"error: input directory does not exist: {input_dir}", file=sys.stderr)
        return 2
    if (args.resolution < 32 or not 0 <= args.background <= 1 or
            not 0 <= args.alpha_cutoff <= 1 or args.emission_strength < 0):
        print("error: invalid resolution, background, alpha-cutoff, or "
              "emission-strength", file=sys.stderr)
        return 2
    pattern = "**/*.glb" if args.recursive else "*.glb"
    sources = sorted(path for path in input_dir.glob(pattern) if path.is_file())
    if not sources:
        print(f"no GLB files found in {input_dir}")
        return 0

    succeeded = skipped = 0
    for source in sources:
        relative = source.relative_to(input_dir)
        destination = output_dir / relative
        if destination.exists() and not args.overwrite:
            skipped += 1
            print(f"SKIP {relative} (already exists; use --overwrite)")
            continue
        try:
            count = convert_file(
                source, destination, args.resolution, args.background,
                args.transparent_background, args.alpha_cutoff,
                args.emission_strength)
            succeeded += 1
            mode = "transparent-mask" if args.transparent_background else "opaque-background"
            print(f"OK   {relative} -> {destination} [{count} Mesh Objects, "
                  f"{count} boxes, {count * 6} captures, {mode}]")
        except Exception as exc:
            print(f"FAIL {relative}: {exc}", file=sys.stderr)
    failed = len(sources) - succeeded - skipped
    print(f"finished: {succeeded} converted, {skipped} skipped, {failed} failed")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Convert bridge GLBs into transparent top-projection LOD0 planes.

Run with Blender rather than ordinary Python:

    blender --background --python glb_bridge_top_projection.py -- input \
        --output output --resolution 512 --height-offset 0.15 \
        --segment-length 150 --tower-suffixes zt \
        --top-exclude-suffixes xls --deck-height-suffixes qmb \
        --skip-deck-projection --overwrite

The script preserves each model's world-space horizontal orientation. Unless
--skip-deck-projection is used, it renders the bridge deck as a transparent
orthographic top projection. Meshes
whose object or mesh names match --tower-suffixes are excluded from that top
view and rendered as front/back vertical cards. Meshes matching
--top-exclude-suffixes are omitted entirely from the projection. Exported
deck segments share one height derived from --deck-height-suffixes. Materials
are patched to KHR_materials_unlit and alphaMode=MASK.
"""

from __future__ import annotations

import argparse
import json
import math
import struct
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

import bpy
from mathutils import Matrix, Vector


@dataclass
class ProjectionBounds:
    axis_u: tuple[float, float]
    axis_v: tuple[float, float]
    minimum_u: float
    maximum_u: float
    minimum_v: float
    maximum_v: float
    minimum_z: float
    maximum_z: float
    vertices: list[tuple[float, float, float]]


@dataclass
class TowerBounds:
    minimum_u: float
    maximum_u: float
    minimum_v: float
    maximum_v: float
    minimum_z: float
    maximum_z: float


@dataclass
class TowerCapture:
    front: Path
    back: Path
    card_width: float


def reset_scene() -> None:
    bpy.ops.object.select_all(action="SELECT")
    bpy.ops.object.delete(use_global=False)
    for collection in (
        bpy.data.meshes,
        bpy.data.curves,
        bpy.data.cameras,
        bpy.data.lights,
        bpy.data.materials,
        bpy.data.images,
    ):
        for block in list(collection):
            if block.users == 0:
                collection.remove(block)


def import_meshes(source: Path) -> list[object]:
    bpy.ops.import_scene.gltf(filepath=str(source))
    meshes = [obj for obj in bpy.context.scene.objects
              if obj.type == "MESH" and len(obj.data.vertices)]
    if not meshes:
        raise ValueError("GLB contains no mesh vertices")
    return meshes


def normalized_suffixes(values: list[str]) -> tuple[str, ...]:
    result: list[str] = []
    for value in values:
        for item in value.split(","):
            suffix = item.strip().casefold()
            if suffix and suffix not in result:
                result.append(suffix)
    return tuple(result)


def mesh_name_has_suffix(obj: object, suffixes: tuple[str, ...]) -> bool:
    if not suffixes:
        return False
    object_name = obj.name.casefold()
    mesh_name = obj.data.name.casefold() if obj.data is not None else ""
    return object_name.endswith(suffixes) or mesh_name.endswith(suffixes)


def world_vertices(meshes: list[object]) -> list[tuple[float, float, float]]:
    result: list[tuple[float, float, float]] = []
    for obj in meshes:
        matrix = obj.matrix_world
        for vertex in obj.data.vertices:
            point = matrix @ vertex.co
            if all(math.isfinite(value) for value in point):
                result.append((float(point.x), float(point.y), float(point.z)))
    if not result:
        raise ValueError("model contains no finite world-space vertices")
    return result


def projection_bounds(vertices: list[tuple[float, float, float]]) -> ProjectionBounds:
    count = len(vertices)
    mean_x = sum(vertex[0] for vertex in vertices) / count
    mean_y = sum(vertex[1] for vertex in vertices) / count
    covariance_xx = sum((vertex[0] - mean_x) ** 2 for vertex in vertices) / count
    covariance_yy = sum((vertex[1] - mean_y) ** 2 for vertex in vertices) / count
    covariance_xy = sum(
        (vertex[0] - mean_x) * (vertex[1] - mean_y)
        for vertex in vertices) / count

    angle = 0.5 * math.atan2(
        2.0 * covariance_xy, covariance_xx - covariance_yy)
    axis_u = (math.cos(angle), math.sin(angle))
    axis_v = (-axis_u[1], axis_u[0])

    projected_u = [vertex[0] * axis_u[0] + vertex[1] * axis_u[1]
                   for vertex in vertices]
    projected_v = [vertex[0] * axis_v[0] + vertex[1] * axis_v[1]
                   for vertex in vertices]
    minimum_u, maximum_u = min(projected_u), max(projected_u)
    minimum_v, maximum_v = min(projected_v), max(projected_v)
    minimum_z = min(vertex[2] for vertex in vertices)
    maximum_z = max(vertex[2] for vertex in vertices)
    if maximum_u - minimum_u <= 1e-6 or maximum_v - minimum_v <= 1e-6:
        raise ValueError("bridge horizontal projection has zero area")
    return ProjectionBounds(
        axis_u=axis_u,
        axis_v=axis_v,
        minimum_u=minimum_u,
        maximum_u=maximum_u,
        minimum_v=minimum_v,
        maximum_v=maximum_v,
        minimum_z=minimum_z,
        maximum_z=maximum_z,
        vertices=vertices,
    )


def tower_clusters(vertices: list[tuple[float, float, float]],
                   bridge: ProjectionBounds,
                   configured_gap: float) -> list[TowerBounds]:
    projected = sorted((
        vertex[0] * bridge.axis_u[0] + vertex[1] * bridge.axis_u[1],
        vertex[0] * bridge.axis_v[0] + vertex[1] * bridge.axis_v[1],
        vertex[2],
    ) for vertex in vertices)
    if not projected:
        return []
    bridge_length = bridge.maximum_u - bridge.minimum_u
    gap = configured_gap if configured_gap > 0 else max(bridge_length * 0.05, 5.0)
    groups: list[list[tuple[float, float, float]]] = [[projected[0]]]
    for point in projected[1:]:
        if point[0] - groups[-1][-1][0] > gap:
            groups.append([])
        groups[-1].append(point)
    return [TowerBounds(
        minimum_u=min(point[0] for point in group),
        maximum_u=max(point[0] for point in group),
        minimum_v=min(point[1] for point in group),
        maximum_v=max(point[1] for point in group),
        minimum_z=min(point[2] for point in group),
        maximum_z=max(point[2] for point in group),
    ) for group in groups]


def setup_renderer(resolution: int, background_strength: float) -> tuple[object, list[object]]:
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
    scene.render.film_transparent = True
    scene.world.use_nodes = True
    background = scene.world.node_tree.nodes.get("Background")
    background.inputs["Color"].default_value = (1.0, 1.0, 1.0, 1.0)
    background.inputs["Strength"].default_value = background_strength

    camera_data = bpy.data.cameras.new("bridge_projection_camera")
    camera = bpy.data.objects.new("bridge_projection_camera", camera_data)
    scene.collection.objects.link(camera)
    camera_data.type = "ORTHO"
    scene.camera = camera

    lights: list[object] = []
    for index in range(3):
        data = bpy.data.lights.new(f"bridge_projection_light_{index}", "AREA")
        data.energy = 1000.0
        data.shape = "DISK"
        light = bpy.data.objects.new(f"bridge_projection_light_{index}", data)
        scene.collection.objects.link(light)
        lights.append(light)
    return camera, lights


def render_top_view(bounds: ProjectionBounds, destination: Path,
                    resolution: int, background_strength: float) -> float:
    camera, lights = setup_renderer(resolution, background_strength)
    center_u = (bounds.minimum_u + bounds.maximum_u) * 0.5
    center_v = (bounds.minimum_v + bounds.maximum_v) * 0.5
    center_x = center_u * bounds.axis_u[0] + center_v * bounds.axis_v[0]
    center_y = center_u * bounds.axis_u[1] + center_v * bounds.axis_v[1]
    length = bounds.maximum_u - bounds.minimum_u
    width = bounds.maximum_v - bounds.minimum_v
    scale = max(length, width) * 1.02
    distance = max(bounds.maximum_z - bounds.minimum_z, scale) + 10.0

    right = Vector((bounds.axis_u[0], bounds.axis_u[1], 0.0))
    up = Vector((bounds.axis_v[0], bounds.axis_v[1], 0.0))
    back = Vector((0.0, 0.0, 1.0))
    matrix = Matrix((right, up, back)).transposed().to_4x4()
    matrix.translation = Vector((center_x, center_y, bounds.maximum_z + distance))
    camera.matrix_world = matrix
    camera.data.ortho_scale = scale
    camera.data.clip_start = 0.01
    camera.data.clip_end = distance * 3.0 + 100.0

    offsets = ((0.25, 0.15), (-0.25, 0.15), (0.0, -0.25))
    for light, (offset_u, offset_v) in zip(lights, offsets):
        light.location = Vector((
            center_x + bounds.axis_u[0] * length * offset_u
            + bounds.axis_v[0] * width * offset_v,
            center_y + bounds.axis_u[1] * length * offset_u
            + bounds.axis_v[1] * width * offset_v,
            bounds.maximum_z + distance * 0.5,
        ))
        light.data.size = max(length, width) * 0.7
        target = Vector((center_x, center_y, bounds.minimum_z))
        light.rotation_euler = (target - light.location).to_track_quat("-Z", "Y").to_euler()

    bpy.context.scene.render.filepath = str(destination)
    bpy.ops.render.render(write_still=True)
    return scale


def render_tower_views(bridge: ProjectionBounds, tower: TowerBounds,
                       directory: Path, index: int, resolution: int,
                       background_strength: float) -> TowerCapture:
    captures: dict[str, Path] = {}
    center_u = (tower.minimum_u + tower.maximum_u) * 0.5
    center_v = (tower.minimum_v + tower.maximum_v) * 0.5
    center_z = (tower.minimum_z + tower.maximum_z) * 0.5
    width = max(tower.maximum_v - tower.minimum_v, 1e-4)
    height = max(tower.maximum_z - tower.minimum_z, 1e-4)
    depth = max(tower.maximum_u - tower.minimum_u, 1e-4)
    vertical_scale = height * 1.02
    if width >= height:
        resolution_x = resolution
        resolution_y = max(32, round(resolution * height / width))
    else:
        resolution_x = max(32, round(resolution * width / height))
        resolution_y = resolution
    card_width = vertical_scale * resolution_x / resolution_y
    distance = max(width, height) * 2.5 + 10.0
    center = Vector((
        center_u * bridge.axis_u[0] + center_v * bridge.axis_v[0],
        center_u * bridge.axis_u[1] + center_v * bridge.axis_v[1],
        center_z,
    ))

    for view_name, direction_sign in (("front", 1.0), ("back", -1.0)):
        camera, lights = setup_renderer(resolution, background_strength)
        bpy.context.scene.render.resolution_x = resolution_x
        bpy.context.scene.render.resolution_y = resolution_y
        direction = Vector((
            bridge.axis_u[0] * direction_sign,
            bridge.axis_u[1] * direction_sign,
            0.0,
        ))
        up = Vector((0.0, 0.0, 1.0))
        right = direction.cross(up).normalized()
        back = -direction
        matrix = Matrix((right, up, back)).transposed().to_4x4()
        matrix.translation = center - direction * distance
        camera.matrix_world = matrix
        camera.data.ortho_scale = vertical_scale
        # Limit rendering to this tower's longitudinal slab. This matters when
        # several spatially separated towers belong to one source mesh.
        depth_padding = max(depth * 0.05, 0.1)
        camera.data.clip_start = max(
            0.01, distance - depth * 0.5 - depth_padding)
        camera.data.clip_end = distance + depth * 0.5 + depth_padding

        for light_index, light in enumerate(lights):
            side_offset = (light_index - 1) * width * 0.25
            light.location = (center - direction * distance * 0.45
                              + right * side_offset + up * height * 0.2)
            light.data.size = max(width, height) * 0.7
            light.rotation_euler = (
                center - light.location).to_track_quat("-Z", "Y").to_euler()

        image_path = directory / f"tower_{index:03d}_{view_name}.png"
        bpy.context.scene.render.filepath = str(image_path)
        bpy.ops.render.render(write_still=True)
        captures[view_name] = image_path
    return TowerCapture(
        front=captures["front"],
        back=captures["back"],
        card_width=card_width,
    )


def build_projection_material(image_path: Path, alpha_cutoff: float,
                              material_name: str) -> object:
    image = bpy.data.images.load(str(image_path), check_existing=False)
    image.name = material_name
    image.pack()
    material = bpy.data.materials.new(material_name)
    material.use_nodes = True
    nodes, links = material.node_tree.nodes, material.node_tree.links
    principled = nodes.get("Principled BSDF")
    texture = nodes.new("ShaderNodeTexImage")
    texture.image = image
    links.new(texture.outputs["Color"], principled.inputs["Base Color"])
    alpha_mask = nodes.new("ShaderNodeMath")
    alpha_mask.operation = "GREATER_THAN"
    alpha_mask.inputs[1].default_value = alpha_cutoff
    links.new(texture.outputs["Alpha"], alpha_mask.inputs[0])
    links.new(alpha_mask.outputs[0], principled.inputs["Alpha"])
    principled.inputs["Metallic"].default_value = 0.0
    principled.inputs["Roughness"].default_value = 1.0
    if hasattr(material, "blend_method"):
        material.blend_method = "CLIP"
    if hasattr(material, "alpha_threshold"):
        material.alpha_threshold = alpha_cutoff
    if hasattr(material, "surface_render_method"):
        try:
            material.surface_render_method = "DITHERED"
        except (TypeError, ValueError):
            pass
    return material


def world_point(axis_u: tuple[float, float], axis_v: tuple[float, float],
                value_u: float, value_v: float, height: float) -> tuple[float, float, float]:
    return (
        value_u * axis_u[0] + value_v * axis_v[0],
        value_u * axis_u[1] + value_v * axis_v[1],
        height,
    )


def create_projection_mesh(bounds: ProjectionBounds, texture_scale: float,
                           segment_length: float, projection_height: float,
                           material: object) -> object:
    total_length = bounds.maximum_u - bounds.minimum_u
    segment_count = (1 if segment_length <= 0 else
                     max(1, math.ceil(total_length / segment_length)))
    center_u = (bounds.minimum_u + bounds.maximum_u) * 0.5
    center_v = (bounds.minimum_v + bounds.maximum_v) * 0.5
    vertices: list[tuple[float, float, float]] = []
    faces: list[tuple[int, int, int, int]] = []
    uvs: list[tuple[float, float]] = []

    for index in range(segment_count):
        start_u = bounds.minimum_u + total_length * index / segment_count
        end_u = bounds.minimum_u + total_length * (index + 1) / segment_count
        corners_uv = (
            (start_u, bounds.minimum_v),
            (end_u, bounds.minimum_v),
            (end_u, bounds.maximum_v),
            (start_u, bounds.maximum_v),
        )
        first = len(vertices)
        vertices.extend(
            world_point(bounds.axis_u, bounds.axis_v, u, v,
                        projection_height)
            for u, v in corners_uv
        )
        faces.append((first, first + 1, first + 2, first + 3))
        uvs.extend((
            0.5 + (u - center_u) / texture_scale,
            0.5 + (v - center_v) / texture_scale,
        ) for u, v in corners_uv)

    mesh = bpy.data.meshes.new("bridge_top_projection")
    mesh.from_pydata(vertices, [], faces)
    mesh.update()
    uv_layer = mesh.uv_layers.new(name="UVMap")
    for polygon in mesh.polygons:
        polygon.material_index = 0
        for loop_index in polygon.loop_indices:
            vertex_index = mesh.loops[loop_index].vertex_index
            uv_layer.data[loop_index].uv = uvs[vertex_index]
    mesh.materials.append(material)
    obj = bpy.data.objects.new("bridge_top_projection", mesh)
    bpy.context.scene.collection.objects.link(obj)
    return obj


def create_tower_card(bridge: ProjectionBounds, tower: TowerBounds, index: int,
                      front_material: object, back_material: object,
                      card_width: float) -> object:
    center_u = (tower.minimum_u + tower.maximum_u) * 0.5
    center_v = (tower.minimum_v + tower.maximum_v) * 0.5
    minimum_v = center_v - card_width * 0.5
    maximum_v = center_v + card_width * 0.5

    # Front camera looks along +U, so its screen-right direction is -V.
    front_vertices = [
        world_point(bridge.axis_u, bridge.axis_v, center_u, maximum_v, tower.minimum_z),
        world_point(bridge.axis_u, bridge.axis_v, center_u, minimum_v, tower.minimum_z),
        world_point(bridge.axis_u, bridge.axis_v, center_u, minimum_v, tower.maximum_z),
        world_point(bridge.axis_u, bridge.axis_v, center_u, maximum_v, tower.maximum_z),
    ]
    # Back camera looks along -U, so its screen-right direction is +V.
    back_vertices = [
        world_point(bridge.axis_u, bridge.axis_v, center_u, minimum_v, tower.minimum_z),
        world_point(bridge.axis_u, bridge.axis_v, center_u, maximum_v, tower.minimum_z),
        world_point(bridge.axis_u, bridge.axis_v, center_u, maximum_v, tower.maximum_z),
        world_point(bridge.axis_u, bridge.axis_v, center_u, minimum_v, tower.maximum_z),
    ]
    vertices = [*front_vertices, *back_vertices]
    faces = [(0, 1, 2, 3), (4, 5, 6, 7)]
    face_uvs = (
        ((0.0, 0.0), (1.0, 0.0), (1.0, 1.0), (0.0, 1.0)),
        ((0.0, 0.0), (1.0, 0.0), (1.0, 1.0), (0.0, 1.0)),
    )

    name = f"bridge_tower_card_{index:03d}"
    mesh = bpy.data.meshes.new(name)
    mesh.from_pydata(vertices, [], faces)
    mesh.update()
    uv_layer = mesh.uv_layers.new(name="UVMap")
    for polygon in mesh.polygons:
        polygon.material_index = polygon.index
        for uv, loop_index in zip(face_uvs[polygon.index], polygon.loop_indices):
            uv_layer.data[loop_index].uv = uv
    mesh.materials.append(front_material)
    mesh.materials.append(back_material)
    obj = bpy.data.objects.new(name, mesh)
    bpy.context.scene.collection.objects.link(obj)
    return obj


def remove_source_meshes(meshes: list[object]) -> None:
    for obj in meshes:
        bpy.data.objects.remove(obj, do_unlink=True)


def export_projection(destination: Path, projections: list[object]) -> None:
    bpy.ops.object.select_all(action="DESELECT")
    for projection in projections:
        projection.select_set(True)
    bpy.context.view_layer.objects.active = projections[0]
    destination.parent.mkdir(parents=True, exist_ok=True)
    bpy.ops.export_scene.gltf(
        filepath=str(destination),
        export_format="GLB",
        use_selection=True,
        export_texcoords=True,
        export_normals=True,
        export_materials="EXPORT",
        export_image_format="AUTO",
    )


def patch_unlit_materials(path: Path, alpha_cutoff: float) -> None:
    data = path.read_bytes()
    if len(data) < 20 or data[:4] != b"glTF":
        raise ValueError(f"exported file is not GLB: {path}")
    version = struct.unpack_from("<I", data, 4)[0]
    offset = 12
    chunks: list[tuple[int, bytes]] = []
    while offset < len(data):
        length, chunk_type = struct.unpack_from("<II", data, offset)
        offset += 8
        chunks.append((chunk_type, data[offset:offset + length]))
        offset += length
    json_index = next((i for i, chunk in enumerate(chunks)
                       if chunk[0] == 0x4E4F534A), None)
    if json_index is None:
        raise ValueError("exported GLB has no JSON chunk")
    document = json.loads(chunks[json_index][1].rstrip(b"\x00 ").decode("utf-8"))
    used = document.setdefault("extensionsUsed", [])
    if "KHR_materials_unlit" not in used:
        used.append("KHR_materials_unlit")
    for material in document.get("materials", []):
        material.setdefault("extensions", {})["KHR_materials_unlit"] = {}
        material["alphaMode"] = "MASK"
        material["alphaCutoff"] = alpha_cutoff
        material["doubleSided"] = not material.get("name", "").startswith(
            "bridge_tower_")

    encoded = json.dumps(document, ensure_ascii=False,
                         separators=(",", ":")).encode("utf-8")
    encoded += b" " * ((4 - len(encoded) % 4) % 4)
    chunks[json_index] = (0x4E4F534A, encoded)
    total_length = 12 + sum(8 + len(chunk) for _, chunk in chunks)
    output = bytearray(struct.pack("<4sII", b"glTF", version, total_length))
    for chunk_type, chunk in chunks:
        output.extend(struct.pack("<II", len(chunk), chunk_type))
        output.extend(chunk)
    path.write_bytes(output)


def convert(source: Path, destination: Path, resolution: int,
            height_offset: float, segment_length: float,
            alpha_cutoff: float, background_strength: float,
            tower_suffixes: tuple[str, ...], tower_cluster_gap: float,
            top_exclude_suffixes: tuple[str, ...],
            deck_height_suffixes: tuple[str, ...],
            skip_deck_projection: bool
            ) -> tuple[int, int]:
    reset_scene()
    meshes = import_meshes(source)
    tower_meshes = [obj for obj in meshes
                    if mesh_name_has_suffix(obj, tower_suffixes)]
    top_excluded_meshes = [
        obj for obj in meshes
        if obj not in tower_meshes
        and mesh_name_has_suffix(obj, top_exclude_suffixes)
    ]
    deck_meshes = [obj for obj in meshes
                   if obj not in tower_meshes
                   and obj not in top_excluded_meshes]
    if tower_suffixes and not tower_meshes:
        available = ", ".join(sorted(obj.name for obj in meshes))
        raise ValueError(
            f"no tower mesh matched suffixes {list(tower_suffixes)}; "
            f"available mesh objects: {available}")
    if not deck_meshes:
        raise ValueError(
            "tower/top-exclude suffixes matched every mesh; "
            "no bridge deck remains")
    if skip_deck_projection and not tower_meshes:
        raise ValueError(
            "--skip-deck-projection requires at least one tower mesh")

    bounds = projection_bounds(world_vertices(deck_meshes))
    towers = (tower_clusters(world_vertices(tower_meshes), bounds,
                              tower_cluster_gap)
              if tower_meshes else [])
    with tempfile.TemporaryDirectory(prefix="bridge_top_projection_") as temporary:
        temporary_path = Path(temporary)
        deck_projection: tuple[float, float, object] | None = None
        if not skip_deck_projection:
            for obj in [*tower_meshes, *top_excluded_meshes]:
                obj.hide_render = True
            image_path = temporary_path / "top.png"
            texture_scale = render_top_view(
                bounds, image_path, resolution, background_strength)
            material = build_projection_material(
                image_path, alpha_cutoff, "bridge_top_projection_unlit")
            height_meshes = [
                obj for obj in deck_meshes
                if mesh_name_has_suffix(obj, deck_height_suffixes)
            ]
            if not height_meshes:
                height_meshes = deck_meshes
            projection_height = max(
                vertex[2] for vertex in world_vertices(height_meshes)
            ) + height_offset
            deck_projection = (texture_scale, projection_height, material)

        tower_materials: list[tuple[object, object, float]] = []
        if tower_meshes:
            for obj in deck_meshes:
                obj.hide_render = True
            for obj in top_excluded_meshes:
                obj.hide_render = True
            for obj in tower_meshes:
                obj.hide_render = False
            for index, tower in enumerate(towers):
                captures = render_tower_views(
                    bounds, tower, temporary_path, index, resolution,
                    background_strength)
                front = build_projection_material(
                    captures.front, alpha_cutoff,
                    f"bridge_tower_{index:03d}_front_unlit")
                back = build_projection_material(
                    captures.back, alpha_cutoff,
                    f"bridge_tower_{index:03d}_back_unlit")
                tower_materials.append(
                    (front, back, captures.card_width))

        remove_source_meshes(meshes)
        projections: list[object] = []
        if deck_projection is not None:
            projections.append(create_projection_mesh(
                bounds, deck_projection[0], segment_length,
                deck_projection[1], deck_projection[2]))
        for index, (tower, materials) in enumerate(zip(towers, tower_materials)):
            projections.append(create_tower_card(
                bounds, tower, index, materials[0], materials[1],
                materials[2]))
        export_projection(destination, projections)
        patch_unlit_materials(destination, alpha_cutoff)
    total_length = bounds.maximum_u - bounds.minimum_u
    segments = (0 if skip_deck_projection else
                (1 if segment_length <= 0 else
                 max(1, math.ceil(total_length / segment_length))))
    return segments, len(towers)


def parse_args() -> argparse.Namespace:
    argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    parser = argparse.ArgumentParser(
        description="Generate transparent top-projection LOD0 bridge GLBs")
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument("--recursive", "-r", action="store_true")
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument("--resolution", type=int, default=512)
    parser.add_argument("--height-offset", type=float, default=0.15)
    parser.add_argument(
        "--segment-length", type=float, default=0.0,
        help="maximum segment length in model units; 0 creates one plane")
    parser.add_argument("--alpha-cutoff", type=float, default=0.10)
    parser.add_argument("--background-strength", type=float, default=0.8)
    parser.add_argument(
        "--tower-suffixes", nargs="+", default=[],
        help="mesh/object name suffixes for A-shaped bridge towers, e.g. zt")
    parser.add_argument(
        "--tower-cluster-gap", type=float, default=0.0,
        help="gap used to split towers along the bridge; 0 selects automatically")
    parser.add_argument(
        "--top-exclude-suffixes", nargs="+", default=["xls"],
        help="mesh/object name suffixes omitted from the top projection "
             "(default: xls)")
    parser.add_argument(
        "--deck-height-suffixes", nargs="+", default=["qmb"],
        help="mesh/object name suffixes used to determine the common deck "
             "projection height (default: qmb)")
    parser.add_argument(
        "--skip-deck-projection", action="store_true",
        help="omit the bridge deck texture and horizontal projection planes")
    return parser.parse_args(argv)


def main() -> int:
    args = parse_args()
    input_dir = args.input_dir.resolve()
    output_dir = args.output.resolve()
    if not input_dir.is_dir():
        print(f"error: input directory does not exist: {input_dir}", file=sys.stderr)
        return 2
    if (args.resolution < 32 or args.segment_length < 0 or
            args.tower_cluster_gap < 0 or
            not 0 <= args.alpha_cutoff <= 1 or args.background_strength < 0):
        print("error: invalid resolution, segment-length, tower-cluster-gap, "
              "alpha-cutoff, or background-strength", file=sys.stderr)
        return 2

    tower_suffixes = normalized_suffixes(args.tower_suffixes)
    top_exclude_suffixes = normalized_suffixes(args.top_exclude_suffixes)
    deck_height_suffixes = normalized_suffixes(args.deck_height_suffixes)

    pattern = "**/*.glb" if args.recursive else "*.glb"
    sources = sorted(path for path in input_dir.glob(pattern) if path.is_file())
    if not sources:
        print(f"no GLB files found in {input_dir}")
        return 0

    succeeded = skipped = failed = 0
    for source in sources:
        relative = source.relative_to(input_dir)
        destination = output_dir / relative
        if destination.exists() and not args.overwrite:
            skipped += 1
            print(f"SKIP {relative} (already exists; use --overwrite)")
            continue
        try:
            segments, towers = convert(
                source, destination, args.resolution, args.height_offset,
                args.segment_length, args.alpha_cutoff,
                args.background_strength, tower_suffixes,
                args.tower_cluster_gap, top_exclude_suffixes,
                deck_height_suffixes, args.skip_deck_projection)
            succeeded += 1
            print(f"OK   {relative} -> {destination} "
                  f"[{segments} deck segment(s), {towers} tower card(s)]")
        except Exception as exc:
            failed += 1
            print(f"FAIL {relative}: {exc}", file=sys.stderr)

    print(f"finished: {succeeded} converted, {skipped} skipped, "
          f"{failed} failed")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

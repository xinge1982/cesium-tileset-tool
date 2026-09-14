#!/usr/bin/env python3
"""Extract features from an implicit 3D Tiles tileset into standalone GLBs.

Run with Blender:

    blender --background --python extract_implicit_feature.py -- \
        --tileset-dir /data/features \
        --feature-csv feature_ids.csv \
        --csv-id-field feature_id \
        --id-field id \
        --bounds 121.73,31.13,121.78,31.16 \
        --output /data/extracted \
        --overwrite

The CSV may contain one or many feature IDs. The geographic bounds are
west,south,east,north in degrees and are used only to reduce the candidate
implicit tiles. B3DM features are selected through _BATCHID. I3DM instances
are exported in model-local coordinates and receive a sibling
<feature-id>.placement.json containing the original RTC/instance transform.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
import struct
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import unquote

import bpy
import bmesh
from mathutils import Matrix, Vector


CONTENT_RE = re.compile(
    r"(?:^|[\\/])contents[\\/](\d+)[\\/](\d+)[\\/](\d+)[\\/]"
)
BATCH_ATTRIBUTE_NAMES = ("_BATCHID", "BATCHID", "_FEATURE_ID_0", "FEATURE_ID_0")
COMPONENT_FORMATS = {
    "BYTE": ("b", 1),
    "UNSIGNED_BYTE": ("B", 1),
    "SHORT": ("h", 2),
    "UNSIGNED_SHORT": ("H", 2),
    "INT": ("i", 4),
    "UNSIGNED_INT": ("I", 4),
    "FLOAT": ("f", 4),
    "DOUBLE": ("d", 8),
}
TYPE_COMPONENTS = {
    "SCALAR": 1,
    "VEC2": 2,
    "VEC3": 3,
    "VEC4": 4,
}


@dataclass
class TilePayload:
    kind: str
    feature_json: dict[str, Any]
    feature_binary: bytes
    batch_json: dict[str, Any]
    batch_binary: bytes
    payload: bytes
    source: Path


def blender_argv() -> list[str]:
    return sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Extract CSV-listed feature IDs from an implicit tileset"
    )
    parser.add_argument("--tileset-dir", type=Path, required=True)
    parser.add_argument("--tileset-json", default="tileset.json")
    parser.add_argument("--feature-csv", type=Path, required=True)
    parser.add_argument("--csv-id-field", default="feature_id")
    parser.add_argument(
        "--id-field",
        default="id",
        help="Batch Table property containing the requested feature ID",
    )
    parser.add_argument(
        "--bounds",
        help="optional west,south,east,north bounds in degrees",
    )
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument("--overwrite", action="store_true")
    return parser.parse_args(blender_argv())


def parse_bounds(value: str | None) -> tuple[float, float, float, float] | None:
    if not value:
        return None
    parts = [float(item.strip()) for item in value.split(",")]
    if len(parts) != 4:
        raise ValueError("--bounds requires west,south,east,north")
    west, south, east, north = parts
    if west > east or south > north:
        raise ValueError("--bounds has an invalid minimum/maximum order")
    return west, south, east, north


def load_ids(path: Path, field: str) -> list[str]:
    with path.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        if not reader.fieldnames or field not in reader.fieldnames:
            raise ValueError(
                f"CSV field {field!r} not found; fields={reader.fieldnames}"
            )
        result: list[str] = []
        seen: set[str] = set()
        for row_number, row in enumerate(reader, 2):
            value = (row.get(field) or "").strip()
            if not value:
                print(f"WARN CSV row {row_number}: empty {field}, skipped")
            elif value not in seen:
                seen.add(value)
                result.append(value)
    if not result:
        raise ValueError("feature CSV contains no non-empty IDs")
    return result


def read_json_chunk(data: bytes) -> dict[str, Any]:
    stripped = data.rstrip(b" \t\r\n\x00")
    return json.loads(stripped.decode("utf-8")) if stripped else {}


def parse_b3dm(data: bytes, source: Path) -> TilePayload:
    if len(data) < 28 or data[:4] != b"b3dm":
        raise ValueError(f"invalid b3dm: {source}")
    version, byte_length, ftj, ftb, btj, btb = struct.unpack_from("<6I", data, 4)
    if version != 1 or byte_length > len(data):
        raise ValueError(f"unsupported b3dm header: {source}")
    offset = 28
    feature_json = read_json_chunk(data[offset:offset + ftj])
    offset += ftj
    feature_binary = data[offset:offset + ftb]
    offset += ftb
    batch_json = read_json_chunk(data[offset:offset + btj])
    offset += btj
    batch_binary = data[offset:offset + btb]
    offset += btb
    return TilePayload(
        "b3dm", feature_json, feature_binary, batch_json, batch_binary,
        data[offset:byte_length], source
    )


def parse_i3dm(data: bytes, source: Path) -> TilePayload:
    if len(data) < 32 or data[:4] != b"i3dm":
        raise ValueError(f"invalid i3dm: {source}")
    values = struct.unpack_from("<7I", data, 4)
    version, byte_length, ftj, ftb, btj, btb, gltf_format = values
    if version != 1 or byte_length > len(data) or gltf_format not in (0, 1):
        raise ValueError(f"unsupported i3dm header: {source}")
    offset = 32
    feature_json = read_json_chunk(data[offset:offset + ftj])
    offset += ftj
    feature_binary = data[offset:offset + ftb]
    offset += ftb
    batch_json = read_json_chunk(data[offset:offset + btj])
    offset += btj
    batch_binary = data[offset:offset + btb]
    offset += btb
    return TilePayload(
        f"i3dm-{gltf_format}", feature_json, feature_binary,
        batch_json, batch_binary, data[offset:byte_length], source
    )


def parse_cmpt(data: bytes, source: Path) -> list[TilePayload]:
    if len(data) < 16 or data[:4] != b"cmpt":
        raise ValueError(f"invalid cmpt: {source}")
    version, byte_length, tile_count = struct.unpack_from("<3I", data, 4)
    if version != 1 or byte_length > len(data):
        raise ValueError(f"unsupported cmpt header: {source}")
    result: list[TilePayload] = []
    offset = 16
    for _ in range(tile_count):
        if offset + 12 > byte_length:
            raise ValueError(f"truncated inner tile: {source}")
        inner_length = struct.unpack_from("<I", data, offset + 8)[0]
        inner = data[offset:offset + inner_length]
        if inner[:4] == b"b3dm":
            result.append(parse_b3dm(inner, source))
        elif inner[:4] == b"i3dm":
            result.append(parse_i3dm(inner, source))
        elif inner[:4] == b"cmpt":
            result.extend(parse_cmpt(inner, source))
        offset += inner_length
    return result


def property_values(
    table: dict[str, Any],
    binary: bytes,
    field: str,
    count: int,
) -> list[Any] | None:
    value = table.get(field)
    if isinstance(value, list):
        return value
    if not isinstance(value, dict) or "byteOffset" not in value:
        return None
    component_type = value.get("componentType", "UNSIGNED_SHORT")
    value_type = value.get("type", "SCALAR")
    if component_type not in COMPONENT_FORMATS or value_type not in TYPE_COMPONENTS:
        raise ValueError(
            f"unsupported binary property {field}: {component_type}/{value_type}"
        )
    code, size = COMPONENT_FORMATS[component_type]
    components = TYPE_COMPONENTS[value_type]
    offset = int(value["byteOffset"])
    fmt = "<" + code * components
    stride = size * components
    result: list[Any] = []
    for index in range(count):
        unpacked = struct.unpack_from(fmt, binary, offset + index * stride)
        result.append(unpacked[0] if components == 1 else list(unpacked))
    return result


def tile_intersects(
    path: Path,
    root_region: list[float] | None,
    bounds: tuple[float, float, float, float] | None,
) -> bool:
    if bounds is None or not root_region:
        return True
    match = CONTENT_RE.search(path.as_posix())
    if not match:
        return True
    level, x, y = map(int, match.groups())
    divisor = 1 << level
    root_w, root_s, root_e, root_n = map(math.degrees, root_region[:4])
    west = root_w + (root_e - root_w) * x / divisor
    east = root_w + (root_e - root_w) * (x + 1) / divisor
    south = root_s + (root_n - root_s) * y / divisor
    north = root_s + (root_n - root_s) * (y + 1) / divisor
    query_w, query_s, query_e, query_n = bounds
    return not (
        east < query_w or west > query_e or north < query_s or south > query_n
    )


def candidate_files(
    tileset_dir: Path,
    root_region: list[float] | None,
    bounds: tuple[float, float, float, float] | None,
) -> list[Path]:
    contents = tileset_dir / "contents"
    files = [
        path for path in contents.rglob("*")
        if path.is_file() and path.suffix.lower() in (".b3dm", ".cmpt", ".i3dm")
    ]
    return sorted(
        path for path in files
        if tile_intersects(path, root_region, bounds)
    )


def reset_scene() -> None:
    bpy.ops.object.select_all(action="SELECT")
    bpy.ops.object.delete(use_global=False)


def imported_meshes(path: Path) -> list[Any]:
    reset_scene()
    bpy.ops.import_scene.gltf(filepath=str(path))
    return [
        obj for obj in bpy.context.scene.objects
        if obj.type == "MESH" and len(obj.data.polygons)
    ]


def batch_attribute(mesh: Any) -> Any | None:
    for name in BATCH_ATTRIBUTE_NAMES:
        attribute = mesh.attributes.get(name)
        if attribute is not None:
            return attribute
    folded = {attribute.name.casefold(): attribute for attribute in mesh.attributes}
    for name in BATCH_ATTRIBUTE_NAMES:
        if name.casefold() in folded:
            return folded[name.casefold()]
    return None


def attribute_value(attribute: Any, polygon: Any, mesh: Any) -> int | None:
    domain = attribute.domain
    if domain == "FACE":
        return round(attribute.data[polygon.index].value)
    if domain == "POINT":
        values = {
            round(attribute.data[mesh.loops[i].vertex_index].value)
            for i in polygon.loop_indices
        }
    elif domain == "CORNER":
        values = {round(attribute.data[i].value) for i in polygon.loop_indices}
    else:
        return None
    return next(iter(values)) if len(values) == 1 else None


def retain_batch(meshes: list[Any], batch_index: int) -> list[Any]:
    retained: list[Any] = []
    for obj in meshes:
        attribute = batch_attribute(obj.data)
        if attribute is None:
            continue
        keep = [
            polygon.index for polygon in obj.data.polygons
            if attribute_value(attribute, polygon, obj.data) == batch_index
        ]
        if not keep:
            bpy.data.objects.remove(obj, do_unlink=True)
            continue
        keep_set = set(keep)
        bm = bmesh.new()
        bm.from_mesh(obj.data)
        bm.faces.ensure_lookup_table()
        bmesh.ops.delete(
            bm,
            geom=[face for face in bm.faces if face.index not in keep_set],
            context="FACES",
        )
        loose = [
            vertex for vertex in bm.verts
            if not vertex.link_faces and not vertex.link_edges
        ]
        if loose:
            bmesh.ops.delete(bm, geom=loose, context="VERTS")
        bm.to_mesh(obj.data)
        bm.free()
        obj.data.update()
        retained.append(obj)
    return retained


def safe_name(value: str) -> str:
    name = re.sub(r"[^0-9A-Za-z._-]+", "_", value).strip("._")
    return name or "feature"


def export_selected(objects: list[Any], destination: Path) -> None:
    if not objects:
        raise ValueError("no matching mesh geometry remained after filtering")
    bpy.ops.object.select_all(action="DESELECT")
    for obj in objects:
        obj.select_set(True)
    bpy.context.view_layer.objects.active = objects[0]
    destination.parent.mkdir(parents=True, exist_ok=True)
    bpy.ops.export_scene.gltf(
        filepath=str(destination),
        export_format="GLB",
        use_selection=True,
        export_materials="EXPORT",
        export_texcoords=True,
        export_normals=True,
    )


def write_placement(
    destination: Path,
    feature_id: str,
    rtc_center: list[float],
    position: list[float] | None,
    right: list[float] | None,
    up: list[float] | None,
    scale: list[float],
) -> None:
    payload = {
        "featureId": feature_id,
        "rtcCenter": rtc_center,
        "position": position,
        "normalRight": right,
        "normalUp": up,
        "scale": scale,
    }
    destination.with_suffix(".placement.json").write_text(
        json.dumps(payload, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def feature_vector(
    table: dict[str, Any],
    binary: bytes,
    semantic: str,
    count: int,
) -> list[Any] | None:
    return property_values(table, binary, semantic, count)


def i3dm_model_path(tile: TilePayload, temporary: Path) -> Path:
    if tile.kind == "i3dm-1":
        path = temporary / "instance.glb"
        path.write_bytes(tile.payload)
        return path
    uri = tile.payload.rstrip(b" \t\r\n\x00").decode("utf-8")
    if uri.startswith("data:"):
        raise ValueError("data URI I3DM models are not supported")
    return (tile.source.parent / unquote(uri)).resolve()


def apply_instance_orientation(
    objects: list[Any],
    right: list[float] | None,
    up: list[float] | None,
    scale: list[float],
) -> None:
    if right is None or up is None:
        rotation = Matrix.Identity(4)
    else:
        x_axis = Vector(right).normalized()
        z_axis = Vector(up).normalized()
        y_axis = z_axis.cross(x_axis).normalized()
        rotation = Matrix((x_axis, y_axis, z_axis)).transposed().to_4x4()
    scaling = Matrix.Diagonal(Vector((*scale, 1.0)))
    transform = rotation @ scaling
    for obj in objects:
        obj.matrix_world = transform @ obj.matrix_world


def extract_b3dm(
    tile: TilePayload,
    batch_index: int,
    feature_id: str,
    destination: Path,
    temporary: Path,
) -> None:
    glb = temporary / "source.glb"
    glb.write_bytes(tile.payload)
    meshes = imported_meshes(glb)
    retained = retain_batch(meshes, batch_index)
    if not retained:
        raise ValueError(
            "the imported GLB has no usable _BATCHID/_FEATURE_ID_0 attribute"
        )
    export_selected(retained, destination)
    rtc = tile.feature_json.get("RTC_CENTER", [0.0, 0.0, 0.0])
    write_placement(
        destination, feature_id, rtc, None, None, None, [1.0, 1.0, 1.0]
    )


def extract_i3dm(
    tile: TilePayload,
    instance_index: int,
    feature_id: str,
    destination: Path,
    temporary: Path,
) -> None:
    count = int(tile.feature_json.get("INSTANCES_LENGTH", 0))
    positions = feature_vector(
        tile.feature_json, tile.feature_binary, "POSITION", count
    )
    rights = feature_vector(
        tile.feature_json, tile.feature_binary, "NORMAL_RIGHT", count
    )
    ups = feature_vector(
        tile.feature_json, tile.feature_binary, "NORMAL_UP", count
    )
    uniform = feature_vector(
        tile.feature_json, tile.feature_binary, "SCALE", count
    )
    non_uniform = feature_vector(
        tile.feature_json, tile.feature_binary, "SCALE_NON_UNIFORM", count
    )
    position = positions[instance_index] if positions else None
    right = rights[instance_index] if rights else None
    up = ups[instance_index] if ups else None
    if non_uniform:
        scale = [float(value) for value in non_uniform[instance_index]]
    elif uniform:
        value = float(uniform[instance_index])
        scale = [value, value, value]
    else:
        scale = [1.0, 1.0, 1.0]
    model_path = i3dm_model_path(tile, temporary)
    meshes = imported_meshes(model_path)
    apply_instance_orientation(meshes, right, up, scale)
    export_selected(meshes, destination)
    rtc = tile.feature_json.get("RTC_CENTER", [0.0, 0.0, 0.0])
    write_placement(
        destination, feature_id, rtc, position, right, up, scale
    )


def payloads(path: Path) -> list[TilePayload]:
    data = path.read_bytes()
    if data[:4] == b"b3dm":
        return [parse_b3dm(data, path)]
    if data[:4] == b"i3dm":
        return [parse_i3dm(data, path)]
    if data[:4] == b"cmpt":
        return parse_cmpt(data, path)
    return []


def find_matches(
    files: list[Path],
    requested: set[str],
    id_field: str,
) -> dict[str, tuple[TilePayload, int]]:
    matches: dict[str, tuple[TilePayload, int]] = {}
    for path in files:
        try:
            tile_payloads = payloads(path)
        except Exception as exc:
            print(f"WARN cannot parse {path}: {exc}")
            continue
        for tile in tile_payloads:
            count_key = "BATCH_LENGTH" if tile.kind == "b3dm" else "INSTANCES_LENGTH"
            count = int(tile.feature_json.get(count_key, 0))
            values = property_values(
                tile.batch_json, tile.batch_binary, id_field, count
            )
            if values is None:
                continue
            for index, value in enumerate(values):
                feature_id = str(value)
                if feature_id in requested and feature_id not in matches:
                    matches[feature_id] = (tile, index)
        if len(matches) == len(requested):
            break
    return matches


def main() -> int:
    args = parse_args()
    tileset_dir = args.tileset_dir.resolve()
    tileset_path = tileset_dir / args.tileset_json
    if not tileset_path.is_file():
        print(f"error: tileset JSON not found: {tileset_path}", file=sys.stderr)
        return 2
    try:
        requested = load_ids(args.feature_csv.resolve(), args.csv_id_field)
        bounds = parse_bounds(args.bounds)
        document = json.loads(tileset_path.read_text(encoding="utf-8-sig"))
        region = (
            document.get("root", {})
            .get("boundingVolume", {})
            .get("region")
        )
        files = candidate_files(tileset_dir, region, bounds)
        matches = find_matches(files, set(requested), args.id_field)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    args.output.mkdir(parents=True, exist_ok=True)
    success = skipped = failed = 0
    with tempfile.TemporaryDirectory(prefix="extract_implicit_feature_") as temp:
        temporary = Path(temp)
        for feature_id in requested:
            destination = args.output / f"{safe_name(feature_id)}.glb"
            if destination.exists() and not args.overwrite:
                skipped += 1
                print(f"SKIP {feature_id}: {destination} already exists")
                continue
            match = matches.get(feature_id)
            if match is None:
                failed += 1
                print(f"MISS {feature_id}: not found", file=sys.stderr)
                continue
            tile, index = match
            try:
                if tile.kind == "b3dm":
                    extract_b3dm(
                        tile, index, feature_id, destination, temporary
                    )
                else:
                    extract_i3dm(
                        tile, index, feature_id, destination, temporary
                    )
                success += 1
                print(
                    f"OK   {feature_id} -> {destination} "
                    f"[{tile.kind}, index={index}, tile={tile.source}]"
                )
            except Exception as exc:
                failed += 1
                print(f"FAIL {feature_id}: {exc}", file=sys.stderr)

    print(
        f"finished: {success} extracted, {skipped} skipped, "
        f"{failed} missing/failed"
    )
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

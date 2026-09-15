#!/usr/bin/env python3
"""Generate textured service-area canopy GLBs from POLYGON Z CSV rows.

The canopy top copies the source polygon elevations plus the row's height.
The bottom is offset downward by --thickness. GLB uses standard Y-up axes, so
Blender imports the models with Z pointing upward. Each model is named
<id>.glb and its origin is the center of the source polygon's 3D bounding box:
longitude/latitude/Z are each the midpoint of their original min/max.

Example:

    python csv_polygon_to_service_canopies.py canopies.csv -o output \
        --canopy-types 3,4 \
        --top-texture textures/canopy_top.jpg \
        --bottom-texture textures/canopy_bottom.jpg \
        --side-texture textures/canopy_side.jpg \
        --thickness 0.35 --side-repeat-width 2.0 --overwrite
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import struct
import sys
from pathlib import Path
from typing import Any, Iterable

from csv_polygon_to_flat_buildings import (
    BinaryBuilder,
    EARTH_A,
    EARTH_E2,
    append_accessor,
    mime_type,
    pack_floats,
    pack_indices,
    parse_polygon_z,
    safe_filename,
    signed_area,
    source_bbox_center,
    triangulate,
    vector_min_max,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate service-area canopy GLBs from polygon CSV data"
    )
    parser.add_argument("input_csv", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument(
        "--canopy-types",
        required=True,
        help="comma-separated values from the type column",
    )
    parser.add_argument("--top-texture", type=Path, required=True)
    parser.add_argument("--bottom-texture", type=Path, required=True)
    parser.add_argument("--side-texture", type=Path, required=True)
    parser.add_argument(
        "--thickness",
        type=float,
        default=1.0,
        help="canopy thickness in metres (default: 1.0)",
    )
    parser.add_argument(
        "--side-repeat-width",
        type=float,
        default=2.0,
        help="side texture horizontal repeat size in metres (default: 2.0)",
    )
    parser.add_argument(
        "--surface-repeat-size",
        type=float,
        default=4.0,
        help="top/bottom texture repeat size in metres (default: 4.0)",
    )
    parser.add_argument("--id-field", default="id")
    parser.add_argument("--type-field", default="type")
    parser.add_argument("--height-field", default="height")
    parser.add_argument("--geometry-field", default="WKT")
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument(
        "--limit", type=int, default=0,
        help="maximum number of generated canopies; 0 means unlimited",
    )
    return parser.parse_args()


def selected_types(value: str) -> set[str]:
    result = {item.strip() for item in value.split(",") if item.strip()}
    if not result:
        raise ValueError("--canopy-types contains no values")
    return result


def local_surfaces(
    points: list[tuple[float, float, float]],
    height: float,
    thickness: float,
) -> tuple[
    list[tuple[float, float, float]],
    list[tuple[float, float, float]],
    float,
    float,
    float,
]:
    anchor_lon, anchor_lat, anchor_altitude = source_bbox_center(points)

    latitude = math.radians(anchor_lat)
    sin_latitude = math.sin(latitude)
    radius_n = EARTH_A / math.sqrt(1.0 - EARTH_E2 * sin_latitude ** 2)
    radius_m = (
        EARTH_A * (1.0 - EARTH_E2)
        / (1.0 - EARTH_E2 * sin_latitude ** 2) ** 1.5
    )
    horizontal = [
        (
            math.radians(lon - anchor_lon) * radius_n * math.cos(latitude),
            math.radians(lat - anchor_lat) * radius_m,
        )
        for lon, lat, _ in points
    ]
    if signed_area(horizontal) < 0:
        points = list(reversed(points))
        horizontal.reverse()

    top = [
        (xy[0], xy[1], point[2] + height - anchor_altitude)
        for xy, point in zip(horizontal, points)
    ]
    bottom = [
        (xy[0], xy[1], point[2] + height - thickness - anchor_altitude)
        for xy, point in zip(horizontal, points)
    ]
    return top, bottom, anchor_lon, anchor_lat, anchor_altitude


def subtract(
    a: tuple[float, float, float], b: tuple[float, float, float]
) -> tuple[float, float, float]:
    return a[0] - b[0], a[1] - b[1], a[2] - b[2]


def cross3(
    a: tuple[float, float, float], b: tuple[float, float, float]
) -> tuple[float, float, float]:
    return (
        a[1] * b[2] - a[2] * b[1],
        a[2] * b[0] - a[0] * b[2],
        a[0] * b[1] - a[1] * b[0],
    )


def normalized(value: tuple[float, float, float]) -> tuple[float, float, float]:
    length = math.sqrt(sum(component * component for component in value))
    if length <= 1e-12:
        return 0.0, 0.0, 1.0
    return tuple(component / length for component in value)


def surface_normals(
    points: list[tuple[float, float, float]],
    triangles: list[tuple[int, int, int]],
    upward: bool,
) -> list[tuple[float, float, float]]:
    sums = [[0.0, 0.0, 0.0] for _ in points]
    for triangle in triangles:
        a, b, c = (points[index] for index in triangle)
        normal = cross3(subtract(b, a), subtract(c, a))
        if normal[2] < 0:
            normal = tuple(-value for value in normal)
        if not upward:
            normal = tuple(-value for value in normal)
        for index in triangle:
            for component in range(3):
                sums[index][component] += normal[component]
    return [normalized(tuple(value)) for value in sums]


def to_gltf(value: tuple[float, float, float]) -> tuple[float, float, float]:
    # Local ENU -> glTF Y-up. Blender imports this as (east, north, up).
    return value[0], value[2], -value[1]


def add_primitive(
    document: dict[str, Any],
    builder: BinaryBuilder,
    positions: list[tuple[float, float, float]],
    normals: list[tuple[float, float, float]],
    uvs: list[tuple[float, float]],
    indices: list[int],
    material: int,
) -> dict[str, Any]:
    minimum, maximum = vector_min_max(positions)
    position_accessor = append_accessor(
        document, builder,
        pack_floats(value for point in positions for value in point),
        5126, "VEC3", len(positions), 34962, minimum, maximum,
    )
    normal_accessor = append_accessor(
        document, builder,
        pack_floats(value for point in normals for value in point),
        5126, "VEC3", len(normals), 34962,
    )
    uv_accessor = append_accessor(
        document, builder,
        pack_floats(value for point in uvs for value in point),
        5126, "VEC2", len(uvs), 34962,
    )
    packed_indices, component_type = pack_indices(indices)
    index_accessor = append_accessor(
        document, builder, packed_indices, component_type,
        "SCALAR", len(indices), 34963,
        [min(indices)], [max(indices)],
    )
    return {
        "attributes": {
            "POSITION": position_accessor,
            "NORMAL": normal_accessor,
            "TEXCOORD_0": uv_accessor,
        },
        "indices": index_accessor,
        "material": material,
        "mode": 4,
    }


def create_canopy_glb(
    top_enu: list[tuple[float, float, float]],
    bottom_enu: list[tuple[float, float, float]],
    top_texture: Path,
    bottom_texture: Path,
    side_texture: Path,
    destination: Path,
    side_repeat_width: float,
    surface_repeat_size: float,
) -> None:
    footprint = [(point[0], point[1]) for point in top_enu]
    triangles = triangulate(footprint)
    top_positions = [to_gltf(point) for point in top_enu]
    bottom_positions = [to_gltf(point) for point in bottom_enu]
    top_normals = [to_gltf(value) for value in surface_normals(top_enu, triangles, True)]
    bottom_normals = [to_gltf(value) for value in surface_normals(bottom_enu, triangles, False)]
    surface_uvs = [
        (point[0] / surface_repeat_size, point[1] / surface_repeat_size)
        for point in top_enu
    ]
    top_indices = [value for triangle in triangles for value in triangle]
    bottom_indices = [
        value
        for a, b, c in triangles
        for value in (a, c, b)
    ]

    side_positions: list[tuple[float, float, float]] = []
    side_normals: list[tuple[float, float, float]] = []
    side_uvs: list[tuple[float, float]] = []
    side_indices: list[int] = []
    distance_u = 0.0
    for index, top_start in enumerate(top_enu):
        following = (index + 1) % len(top_enu)
        top_end = top_enu[following]
        bottom_start = bottom_enu[index]
        bottom_end = bottom_enu[following]
        dx = top_end[0] - top_start[0]
        dy = top_end[1] - top_start[1]
        length = math.hypot(dx, dy)
        if length <= 1e-6:
            continue
        normal = to_gltf((dy / length, -dx / length, 0.0))
        first = len(side_positions)
        side_positions.extend(map(to_gltf, (
            bottom_start, bottom_end, top_end, top_start,
        )))
        side_normals.extend((normal, normal, normal, normal))
        u0 = distance_u / side_repeat_width
        u1 = (distance_u + length) / side_repeat_width
        # Flip V so the source side image is upright on the vertical face.
        side_uvs.extend(((u0, 1.0), (u1, 1.0), (u1, 0.0), (u0, 0.0)))
        side_indices.extend((
            first, first + 1, first + 2,
            first, first + 2, first + 3,
        ))
        distance_u += length

    builder = BinaryBuilder()
    document: dict[str, Any] = {
        "asset": {
            "version": "2.0",
            "generator": "csv_polygon_to_service_canopies.py",
        },
        "scene": 0,
        "scenes": [{"nodes": [0]}],
        "nodes": [{"mesh": 0, "name": destination.stem}],
        "meshes": [{"name": destination.stem, "primitives": []}],
        "accessors": [],
        "bufferViews": [],
        "buffers": [],
        "images": [],
        "textures": [],
        "samplers": [{
            "magFilter": 9729,
            "minFilter": 9987,
            "wrapS": 10497,
            "wrapT": 10497,
        }],
        "materials": [],
    }
    primitives = document["meshes"][0]["primitives"]
    primitives.append(add_primitive(
        document, builder, top_positions, top_normals,
        surface_uvs, top_indices, 0,
    ))
    primitives.append(add_primitive(
        document, builder, bottom_positions, bottom_normals,
        surface_uvs, bottom_indices, 1,
    ))
    primitives.append(add_primitive(
        document, builder, side_positions, side_normals,
        side_uvs, side_indices, 2,
    ))

    for role, texture_path in (
        ("top", top_texture),
        ("bottom", bottom_texture),
        ("side", side_texture),
    ):
        image_data = texture_path.read_bytes()
        image_view = builder.add(image_data)
        image_index = len(document["images"])
        document["images"].append({
            "name": f"{destination.stem}_{role}",
            "bufferView": image_view,
            "mimeType": mime_type(texture_path),
        })
        texture_index = len(document["textures"])
        document["textures"].append({"sampler": 0, "source": image_index})
        document["materials"].append({
            "name": f"{destination.stem}_{role}",
            "pbrMetallicRoughness": {
                "baseColorTexture": {"index": texture_index},
                "metallicFactor": 0.0,
                "roughnessFactor": 1.0,
            },
            "doubleSided": False,
        })

    document["bufferViews"] = builder.json_views()
    while len(builder.data) % 4:
        builder.data.append(0)
    document["buffers"] = [{"byteLength": len(builder.data)}]
    json_data = json.dumps(document, separators=(",", ":")).encode("utf-8")
    json_data += b" " * ((4 - len(json_data) % 4) % 4)
    total_length = 12 + 8 + len(json_data) + 8 + len(builder.data)
    output = bytearray(struct.pack("<4sII", b"glTF", 2, total_length))
    output.extend(struct.pack("<II", len(json_data), 0x4E4F534A))
    output.extend(json_data)
    output.extend(struct.pack("<II", len(builder.data), 0x004E4942))
    output.extend(builder.data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(output)


def validate_texture(path: Path, role: str) -> Path:
    path = path.resolve()
    if not path.is_file():
        raise ValueError(f"{role} texture not found: {path}")
    mime_type(path)
    return path


def main() -> int:
    args = parse_args()
    input_path = args.input_csv.resolve()
    output_dir = args.output.resolve()
    if not input_path.is_file():
        print(f"error: input CSV does not exist: {input_path}", file=sys.stderr)
        return 2
    if (
        args.thickness <= 0
        or args.side_repeat_width <= 0
        or args.surface_repeat_size <= 0
        or args.limit < 0
    ):
        print("error: thickness/repeat sizes must be positive and limit non-negative", file=sys.stderr)
        return 2
    try:
        canopy_types = selected_types(args.canopy_types)
        top_texture = validate_texture(args.top_texture, "top")
        bottom_texture = validate_texture(args.bottom_texture, "bottom")
        side_texture = validate_texture(args.side_texture, "side")
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / f"{input_path.stem}_canopy_placements.csv"
    manifest_fields = [
        "id", "type", "model", "longitude", "latitude", "altitude",
        "height", "thickness", "origin", "sourceRow",
    ]
    manifest_rows: list[dict[str, Any]] = []
    generated = skipped = ignored = failed = 0
    with input_path.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        required = {
            args.id_field, args.type_field, args.height_field, args.geometry_field,
        }
        missing = required.difference(reader.fieldnames or [])
        if missing:
            print(f"error: CSV fields not found: {sorted(missing)}", file=sys.stderr)
            return 2
        for row_number, row in enumerate(reader, 2):
            canopy_type = (row.get(args.type_field) or "").strip()
            if canopy_type not in canopy_types:
                ignored += 1
                continue
            if args.limit and generated >= args.limit:
                break
            identifier = (row.get(args.id_field) or "").strip()
            destination = output_dir / f"{safe_filename(identifier)}.glb"
            if destination.exists() and not args.overwrite:
                skipped += 1
                print(f"SKIP row {row_number}: {destination.name} exists")
                continue
            try:
                height = float((row.get(args.height_field) or "").strip())
                if not math.isfinite(height):
                    raise ValueError(f"invalid height {row.get(args.height_field)!r}")
                points = parse_polygon_z(row.get(args.geometry_field) or "")
                top, bottom, longitude, latitude, altitude = local_surfaces(
                    points, height, args.thickness
                )
                create_canopy_glb(
                    top, bottom, top_texture, bottom_texture, side_texture,
                    destination, args.side_repeat_width, args.surface_repeat_size,
                )
                manifest_rows.append({
                    "id": identifier,
                    "type": canopy_type,
                    "model": destination.name,
                    "longitude": f"{longitude:.12f}",
                    "latitude": f"{latitude:.12f}",
                    "altitude": f"{altitude:.6f}",
                    "height": f"{height:.6f}",
                    "thickness": f"{args.thickness:.6f}",
                    "origin": "source-polygon-3d-bounding-box-center",
                    "sourceRow": row_number,
                })
                generated += 1
                print(f"OK   row {row_number}: {destination.name}")
            except Exception as exc:
                failed += 1
                print(f"FAIL row {row_number}, id={identifier!r}: {exc}", file=sys.stderr)

    with manifest_path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=manifest_fields)
        writer.writeheader()
        writer.writerows(manifest_rows)
    print(
        f"finished: {generated} generated, {skipped} skipped, "
        f"{ignored} other-type row(s), {failed} failed; "
        f"placements={manifest_path}"
    )
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

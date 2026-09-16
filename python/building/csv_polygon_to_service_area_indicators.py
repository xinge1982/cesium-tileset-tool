#!/usr/bin/env python3
"""Generate conspicuous LOD0 service-area indicator GLBs from POLYGON Z CSV.

Each GLB is a flat, translucent colored slab with a solid rounded outline. The
model origin follows the other building generators: longitude, latitude and Z
are the center of the source polygon's 3D bounding box. The source Z values are
used for placement only; the indicator itself is horizontal and rises along Z.

Example:

    python csv_polygon_to_service_area_indicators.py service_areas.csv -o output \
        --fill-color '#00C8FF' --fill-opacity 0.35 \
        --outline-color '#FFD400' --outline-width 1.5 \
        --height-offset 1.0 --thickness 0.8 --overwrite
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
import struct
import sys
from pathlib import Path
from typing import Any

from csv_polygon_to_flat_buildings import (
    BinaryBuilder,
    append_accessor,
    local_footprint,
    pack_floats,
    pack_indices,
    parse_polygon_z,
    safe_filename,
    source_bbox_center,
    triangulate,
    vector_min_max,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate colored translucent LOD0 service-area indicator GLBs"
    )
    parser.add_argument("input_csv", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument("--id-field", default="fid")
    parser.add_argument("--name-field", default="name")
    parser.add_argument("--sa-id-field", default="sa_id")
    parser.add_argument("--geometry-field", default="WKT")
    parser.add_argument("--fill-color", default="#00C8FF")
    parser.add_argument("--fill-opacity", type=float, default=0.35)
    parser.add_argument("--outline-color", default="#FFD400")
    parser.add_argument("--outline-opacity", type=float, default=1.0)
    parser.add_argument(
        "--outline-width", type=float, default=1.5,
        help="solid outline width in metres (default: 1.5)",
    )
    parser.add_argument(
        "--outline-segments", type=int, default=8,
        help="rounded outline join segments per vertex (default: 8)",
    )
    parser.add_argument(
        "--outline-lift", type=float, default=0.03,
        help="raise outline above the fill to prevent z-fighting (default: 0.03)",
    )
    parser.add_argument(
        "--height-offset", type=float, default=1.0,
        help="raise slab top above the placement altitude in metres (default: 1)",
    )
    parser.add_argument(
        "--thickness", type=float, default=0.8,
        help="vertical slab thickness in metres (default: 0.8)",
    )
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument("--limit", type=int, default=0)
    return parser.parse_args()


def parse_color(value: str, option: str) -> tuple[float, float, float]:
    text = value.strip().lstrip("#")
    if len(text) != 6 or not re.fullmatch(r"[0-9a-fA-F]{6}", text):
        raise ValueError(f"{option} must use #RRGGBB")

    def to_linear(component: int) -> float:
        srgb = component / 255.0
        if srgb <= 0.04045:
            return srgb / 12.92
        return ((srgb + 0.055) / 1.055) ** 2.4

    return tuple(to_linear(int(text[index:index + 2], 16)) for index in (0, 2, 4))


def to_gltf(point: tuple[float, float, float]) -> tuple[float, float, float]:
    # Local ENU -> glTF Y-up.
    return point[0], point[2], -point[1]


def append_mesh_primitive(
    document: dict[str, Any],
    builder: BinaryBuilder,
    positions_enu: list[tuple[float, float, float]],
    indices: list[int],
    material: int,
) -> dict[str, Any]:
    positions = [to_gltf(point) for point in positions_enu]
    minimum, maximum = vector_min_max(positions)
    position_accessor = append_accessor(
        document, builder,
        pack_floats(value for point in positions for value in point),
        5126, "VEC3", len(positions), 34962, minimum, maximum,
    )
    index_data, component_type = pack_indices(indices)
    index_accessor = append_accessor(
        document, builder, index_data, component_type,
        "SCALAR", len(indices), 34963, [min(indices)], [max(indices)],
    )
    return {
        "attributes": {"POSITION": position_accessor},
        "indices": index_accessor,
        "material": material,
        "mode": 4,
    }


def slab_geometry(
    footprint: list[tuple[float, float]], top_z: float, thickness: float,
) -> tuple[list[tuple[float, float, float]], list[int]]:
    triangles = triangulate(footprint)
    count = len(footprint)
    positions = (
        [(x, y, top_z) for x, y in footprint]
        + [(x, y, top_z - thickness) for x, y in footprint]
    )
    indices = [value for triangle in triangles for value in triangle]
    indices.extend(
        value + count
        for a, b, c in triangles
        for value in (a, c, b)
    )
    for index in range(count):
        following = (index + 1) % count
        indices.extend((
            index, following, following + count,
            index, following + count, index + count,
        ))
    return positions, indices


def outline_geometry(
    footprint: list[tuple[float, float]],
    z: float,
    width: float,
    join_segments: int,
) -> tuple[list[tuple[float, float, float]], list[int]]:
    """Build solid edge rectangles plus round joins for reliable wide outlines."""
    half_width = width * 0.5
    positions: list[tuple[float, float, float]] = []
    indices: list[int] = []
    for index, start in enumerate(footprint):
        end = footprint[(index + 1) % len(footprint)]
        dx, dy = end[0] - start[0], end[1] - start[1]
        length = math.hypot(dx, dy)
        if length <= 1e-6:
            continue
        ox, oy = -dy / length * half_width, dx / length * half_width
        first = len(positions)
        positions.extend((
            (start[0] + ox, start[1] + oy, z),
            (end[0] + ox, end[1] + oy, z),
            (end[0] - ox, end[1] - oy, z),
            (start[0] - ox, start[1] - oy, z),
        ))
        indices.extend((first, first + 1, first + 2, first, first + 2, first + 3))

    # Disks cover rectangle joins and produce a continuous rounded frame even
    # at acute or concave footprint corners.
    for center_x, center_y in footprint:
        center = len(positions)
        positions.append((center_x, center_y, z))
        for segment in range(join_segments):
            angle = 2.0 * math.pi * segment / join_segments
            positions.append((
                center_x + math.cos(angle) * half_width,
                center_y + math.sin(angle) * half_width,
                z,
            ))
        for segment in range(join_segments):
            indices.extend((
                center,
                center + 1 + segment,
                center + 1 + (segment + 1) % join_segments,
            ))
    return positions, indices


def unlit_material(
    name: str,
    color: tuple[float, float, float],
    opacity: float,
) -> dict[str, Any]:
    material: dict[str, Any] = {
        "name": name,
        "pbrMetallicRoughness": {
            "baseColorFactor": [*color, opacity],
            "metallicFactor": 0.0,
            "roughnessFactor": 1.0,
        },
        "doubleSided": True,
        "extensions": {"KHR_materials_unlit": {}},
    }
    if opacity < 1.0:
        material["alphaMode"] = "BLEND"
    return material


def create_indicator_glb(
    footprint: list[tuple[float, float]],
    destination: Path,
    name: str,
    identifier: str,
    sa_id: str,
    fill_color: tuple[float, float, float],
    fill_opacity: float,
    outline_color: tuple[float, float, float],
    outline_opacity: float,
    outline_width: float,
    outline_segments: int,
    outline_lift: float,
    height_offset: float,
    thickness: float,
) -> None:
    slab_positions, slab_indices = slab_geometry(footprint, height_offset, thickness)
    outline_positions, outline_indices = outline_geometry(
        footprint, height_offset + outline_lift, outline_width, outline_segments,
    )
    builder = BinaryBuilder()
    document: dict[str, Any] = {
        "asset": {
            "version": "2.0",
            "generator": "csv_polygon_to_service_area_indicators.py",
        },
        "extensionsUsed": ["KHR_materials_unlit"],
        "scene": 0,
        "scenes": [{"nodes": [0]}],
        "nodes": [{
            "mesh": 0,
            "name": name or destination.stem,
            "extras": {"id": identifier, "name": name, "sa_id": sa_id},
        }],
        "meshes": [{
            "name": destination.stem,
            "primitives": [],
        }],
        "materials": [
            unlit_material(f"{destination.stem}_fill", fill_color, fill_opacity),
            unlit_material(
                f"{destination.stem}_outline", outline_color, outline_opacity,
            ),
        ],
        "accessors": [],
        "bufferViews": [],
        "buffers": [],
    }
    primitives = document["meshes"][0]["primitives"]
    primitives.append(append_mesh_primitive(
        document, builder, slab_positions, slab_indices, 0,
    ))
    primitives.append(append_mesh_primitive(
        document, builder, outline_positions, outline_indices, 1,
    ))
    document["bufferViews"] = builder.json_views()
    while len(builder.data) % 4:
        builder.data.append(0)
    document["buffers"] = [{"byteLength": len(builder.data)}]
    json_data = json.dumps(document, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    json_data += b" " * ((4 - len(json_data) % 4) % 4)
    total_length = 12 + 8 + len(json_data) + 8 + len(builder.data)
    output = bytearray(struct.pack("<4sII", b"glTF", 2, total_length))
    output.extend(struct.pack("<II", len(json_data), 0x4E4F534A))
    output.extend(json_data)
    output.extend(struct.pack("<II", len(builder.data), 0x004E4942))
    output.extend(builder.data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(output)


def main() -> int:
    args = parse_args()
    input_path = args.input_csv.expanduser().resolve()
    output_dir = args.output.expanduser().resolve()
    if not input_path.is_file():
        print(f"error: input CSV does not exist: {input_path}", file=sys.stderr)
        return 2
    numeric_values = (
        args.fill_opacity, args.outline_opacity, args.outline_width,
        args.outline_lift, args.height_offset, args.thickness,
    )
    if (
        not all(math.isfinite(value) for value in numeric_values)
        or not 0.0 <= args.fill_opacity <= 1.0
        or not 0.0 <= args.outline_opacity <= 1.0
        or args.outline_width <= 0.0
        or args.outline_lift < 0.0
        or args.thickness <= 0.0
        or args.outline_segments < 3
        or args.limit < 0
    ):
        print("error: invalid color, opacity, size, segment, or limit option", file=sys.stderr)
        return 2
    try:
        fill_color = parse_color(args.fill_color, "--fill-color")
        outline_color = parse_color(args.outline_color, "--outline-color")
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / f"{input_path.stem}_indicator_placements.csv"
    fields = [
        "id", "name", "saId", "model", "longitude", "latitude", "altitude",
        "heightOffset", "thickness", "fillColor", "fillOpacity",
        "outlineColor", "outlineOpacity", "outlineWidth", "origin", "sourceRow",
    ]
    rows: list[dict[str, Any]] = []
    generated = skipped = failed = 0
    with input_path.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        required = {args.id_field, args.name_field, args.geometry_field}
        missing = required.difference(reader.fieldnames or [])
        if missing:
            print(f"error: CSV fields not found: {sorted(missing)}", file=sys.stderr)
            return 2
        for row_number, row in enumerate(reader, 2):
            if args.limit and generated >= args.limit:
                break
            identifier = (row.get(args.id_field) or "").strip()
            name = (row.get(args.name_field) or "").strip()
            sa_id = (row.get(args.sa_id_field) or "").strip()
            destination = output_dir / f"{safe_filename(identifier)}.glb"
            if destination.exists() and not args.overwrite:
                skipped += 1
                print(f"SKIP row {row_number}: {destination.name} exists")
                continue
            try:
                if not identifier:
                    raise ValueError("id is empty")
                points = parse_polygon_z(row.get(args.geometry_field) or "")
                footprint, longitude, latitude = local_footprint(points)
                _, _, altitude = source_bbox_center(points)
                create_indicator_glb(
                    footprint, destination, name, identifier, sa_id,
                    fill_color, args.fill_opacity,
                    outline_color, args.outline_opacity,
                    args.outline_width, args.outline_segments,
                    args.outline_lift, args.height_offset, args.thickness,
                )
                rows.append({
                    "id": identifier,
                    "name": name,
                    "saId": sa_id,
                    "model": destination.name,
                    "longitude": f"{longitude:.12f}",
                    "latitude": f"{latitude:.12f}",
                    "altitude": f"{altitude:.6f}",
                    "heightOffset": f"{args.height_offset:.6f}",
                    "thickness": f"{args.thickness:.6f}",
                    "fillColor": args.fill_color,
                    "fillOpacity": f"{args.fill_opacity:g}",
                    "outlineColor": args.outline_color,
                    "outlineOpacity": f"{args.outline_opacity:g}",
                    "outlineWidth": f"{args.outline_width:.6f}",
                    "origin": "source-polygon-3d-bounding-box-center",
                    "sourceRow": row_number,
                })
                generated += 1
                print(f"OK   row {row_number}: {destination.name} {name!r}")
            except Exception as exc:
                failed += 1
                print(f"FAIL row {row_number}, id={identifier!r}: {exc}", file=sys.stderr)

    with manifest_path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        writer.writerows(rows)
    print(
        f"finished: {generated} generated, {skipped} skipped, {failed} failed; "
        f"placements={manifest_path}"
    )
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())

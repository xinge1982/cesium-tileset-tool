#!/usr/bin/env python3
"""Generate textured canopy GLBs with XY constrained Delaunay triangulation.

Keep this file beside csv_polygon_to_flat_buildings.py. Dependencies:
    python -m pip install numpy triangle Pillow

All previous canopy command-line arguments are accepted. --surface-mode and
--cross-segments are compatibility options only: every surface uses CDT.
No Steiner vertices are added. Original indices (carried as vertex attributes)
restore source Z after XY triangulation; no Z interpolation is performed.
Top = source Z + height; bottom = top - thickness. --height-disable retains
previous behavior: top = source Z + thickness + 0.01, bottom = source Z + 0.01.
Only simple, single-ring polygons are supported; holes are rejected explicitly.
"""

from __future__ import annotations

import argparse
from collections import Counter
import re
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
    append_textured_materials,
    append_accessor,
    mime_type,
    pack_floats,
    pack_indices,
    parse_polygon_z,
    safe_filename,
    signed_area,
    source_bbox_center,
    vector_min_max,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate canopy GLBs using XY CDT and source-index elevations"
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
    parser.add_argument(
        "--surface-mode", choices=("auto", "polygon", "strip", "cdt"), default="auto",
        help="compatibility option: all values use CDT in this script",
    )
    parser.add_argument(
        "--cross-segments", type=int, default=4,
        help="legacy compatibility option; accepted but unused by CDT",
    )
    parser.add_argument(
        "--texture-max-size", type=int, default=0,
        help="maximum texture width/height in pixels; 0 keeps original size",
    )
    parser.add_argument(
        "--texture-format", choices=("keep", "png", "jpeg"), default="keep",
        help="embedded texture format (default: keep source format)",
    )
    parser.add_argument(
        "--texture-quality", type=int, default=85,
        help="JPEG quality from 1 to 100 (default: 85)",
    )
    parser.add_argument("--id-field", default="id")
    parser.add_argument("--type-field", default="type")
    parser.add_argument("--height-field", default="height")
    parser.add_argument("--height-disable", action="store_true")
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


def aligned_surface_uvs(
    points: list[tuple[float, float]],
    repeat_size: float,
    alignment_boundary: list[tuple[float, float]] | None = None,
) -> list[tuple[float, float]]:
    """Align the surface texture grid with the polygon's longest edge."""
    boundary = alignment_boundary or points
    longest_start: tuple[float, float] | None = None
    longest_dx = longest_dy = longest_length = 0.0
    for index, start in enumerate(boundary):
        end = boundary[(index + 1) % len(boundary)]
        dx, dy = end[0] - start[0], end[1] - start[1]
        length = math.hypot(dx, dy)
        # Keep the first edge when lengths are effectively equal. This avoids
        # UV direction flips on opposite sides of a rectangular footprint.
        if length > longest_length + 1e-9:
            longest_start = start
            longest_dx, longest_dy, longest_length = dx, dy, length

    if longest_start is None or longest_length <= 1e-6:
        raise ValueError("polygon has no usable edge for surface UV alignment")

    # U follows the longest boundary edge. V is perpendicular to it, so the
    # texture grid has one boundary exactly aligned with the canopy outline.
    u_axis = (longest_dx / longest_length, longest_dy / longest_length)
    v_axis = (-u_axis[1], u_axis[0])
    return [
        (
            ((point[0] - longest_start[0]) * u_axis[0]
             + (point[1] - longest_start[1]) * u_axis[1]) / repeat_size,
            ((point[0] - longest_start[0]) * v_axis[0]
             + (point[1] - longest_start[1]) * v_axis[1]) / repeat_size,
        )
        for point in points
    ]


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

    # Preserve the source Z profile. The top is translated upward without
    # flattening it; deriving every bottom vertex from its matching top vertex
    # keeps the shell thickness aligned strictly with the local Z axis.
    top = [
        (xy[0], xy[1], point[2] + height - anchor_altitude)
        for xy, point in zip(horizontal, points)
    ]
    bottom = [
        (point[0], point[1], point[2] - thickness)
        for point in top
    ]
    return top, bottom, anchor_lon, anchor_lat, anchor_altitude


def cdt_surface_mesh(
    boundary: list[tuple[float, float, float]],
) -> tuple[list[tuple[float, float, float]], list[tuple[int, int, int]]]:
    """Triangulate XY and map result indices back to the unchanged XYZ table.

    Triangle's 'p' uses constrained PSLG triangulation. 'Y' forbids boundary
    splitting; 'S0' forbids Steiner points. Do not use q/a/D refinement or j
    vertex deletion here: all elevations must come from original source indices.
    """
    try:
        import numpy as np
        import triangle
    except ImportError as exc:
        raise RuntimeError(
            "CDT requires numpy and triangle: python -m pip install numpy triangle"
        ) from exc

    count = len(boundary)
    if count < 3:
        raise ValueError("CDT requires at least three vertices")
    if not all(math.isfinite(v) for point in boundary for v in point):
        raise ValueError("polygon has non-finite coordinates")
    xy = [(float(p[0]), float(p[1])) for p in boundary]
    span = max(max(p[k] for p in xy) - min(p[k] for p in xy) for k in (0, 1))
    length_eps = max(1e-10, span * 1e-12)
    area_eps = max(1e-18, length_eps * length_eps)

    def orient(a, b, c):
        return (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0])

    def on_segment(a, b, p):
        return (abs(orient(a, b, p)) <= area_eps
                and min(a[0], b[0]) - length_eps <= p[0] <= max(a[0], b[0]) + length_eps
                and min(a[1], b[1]) - length_eps <= p[1] <= max(a[1], b[1]) + length_eps)

    # Validate before entering the native triangulator: repeated XY with even
    # different Z cannot represent a single-valued height surface.
    for i, a in enumerate(xy):
        for j in range(i + 1, count):
            b = xy[j]
            if math.hypot(a[0] - b[0], a[1] - b[1]) <= length_eps:
                raise ValueError(f"duplicate XY at source indices {i} and {j}")
        prev, nxt = xy[i - 1], xy[(i + 1) % count]
        if (abs(orient(prev, a, nxt)) <= area_eps
                and ((prev[0] - a[0]) * (nxt[0] - a[0])
                     + (prev[1] - a[1]) * (nxt[1] - a[1])) > 0):
            raise ValueError(f"overlapping adjacent edges at source index {i}")
        b = nxt
        for j in range(i + 1, count):
            if j == i + 1 or (j + 1) % count == i:
                continue
            c, d = xy[j], xy[(j + 1) % count]
            o1, o2 = orient(a, b, c), orient(a, b, d)
            o3, o4 = orient(c, d, a), orient(c, d, b)
            proper = ((o1 > area_eps and o2 < -area_eps or o1 < -area_eps and o2 > area_eps)
                      and (o3 > area_eps and o4 < -area_eps or o3 < -area_eps and o4 > area_eps))
            if proper or any((on_segment(a, b, c), on_segment(a, b, d),
                              on_segment(c, d, a), on_segment(c, d, b))):
                raise ValueError(f"polygon edges {i} and {j} intersect or touch")
    # Translate for stable shoelace area even when the caller supplies large XY.
    origin = xy[0]
    polygon_area = abs(signed_area([(x - origin[0], y - origin[1]) for x, y in xy]))
    if polygon_area <= area_eps:
        raise ValueError("polygon has zero projected area")

    result = triangle.triangulate({
        "vertices": np.asarray(xy, dtype=np.float64),
        "segments": np.asarray([(i, (i + 1) % count) for i in range(count)], dtype=np.int32),
        "vertex_attributes": np.arange(count, dtype=np.float64).reshape(-1, 1),
    }, "pQYS0")
    if "triangles" not in result or "vertex_attributes" not in result:
        raise ValueError("CDT produced no triangles or lost source vertex indices")
    if len(result["vertices"]) != count:
        raise ValueError("CDT added/deleted vertices; source-index elevation recovery aborted")
    source_indices = []
    for point, attributes in zip(result["vertices"], result["vertex_attributes"]):
        attribute = float(attributes[0])
        if not math.isfinite(attribute):
            raise ValueError("CDT returned a non-finite source index")
        index = round(attribute)
        if (abs(attribute - index) > 1e-8 or not 0 <= index < count
                or math.hypot(point[0] - xy[index][0], point[1] - xy[index][1]) > length_eps):
            raise ValueError("CDT source-index / XY mapping is inconsistent")
        source_indices.append(index)
    if sorted(source_indices) != list(range(count)):
        raise ValueError("CDT source indices are not a bijection")

    triangles = []
    edges = Counter()
    projected_area = 0.0
    for face in result["triangles"]:
        a, b, c = (source_indices[int(i)] for i in face)
        area2 = orient(xy[a], xy[b], xy[c])
        if abs(area2) <= area_eps:
            raise ValueError("CDT produced a degenerate triangle")
        if area2 < 0:
            b, c = c, b
        triangles.append((a, b, c))
        projected_area += abs(area2) / 2
        edges.update(tuple(sorted(e)) for e in ((a, b), (b, c), (c, a)))
    boundary_edges = {tuple(sorted((i, (i + 1) % count))) for i in range(count)}
    actual_boundary = {e for e, uses in edges.items() if uses == 1}
    if (actual_boundary != boundary_edges or any(n not in (1, 2) for n in edges.values())
            or len(triangles) != count - 2):
        raise ValueError("CDT did not preserve the complete polygon boundary")
    if abs(projected_area - polygon_area) > max(1e-9, polygon_area * 1e-9):
        raise ValueError("CDT projected area does not match the polygon")
    # Each triangle now indexes the original XYZ list, preserving Z exactly.
    return list(boundary), triangles


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
    texture_max_size: int,
    texture_format: str,
    texture_quality: int,
    surface_mode: str,
    cross_segments: int,
) -> None:
    footprint = [(point[0], point[1]) for point in top_enu]
    # Retain legacy arguments in the public signature; all modes now use CDT.
    surface_top, triangles = cdt_surface_mesh(top_enu)
    thickness = top_enu[0][2] - bottom_enu[0][2]
    surface_bottom = [
        (point[0], point[1], point[2] - thickness) for point in surface_top
    ]
    top_positions = [to_gltf(point) for point in surface_top]
    bottom_positions = [to_gltf(point) for point in surface_bottom]
    top_normals = [
        to_gltf(value) for value in surface_normals(surface_top, triangles, True)
    ]
    bottom_normals = [
        to_gltf(value) for value in surface_normals(surface_bottom, triangles, False)
    ]
    # Use one aligned UV set for both faces so top and bottom textures follow
    # the same dominant canopy edge instead of the global east/north axes.
    surface_uvs = aligned_surface_uvs(
        [(point[0], point[1]) for point in surface_top],
        surface_repeat_size,
        footprint,
    )
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
            "generator": "csv_polygon_to_service_canopies_cdt.py",
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

    append_textured_materials(
        document, builder, destination,
        (("top", top_texture), ("bottom", bottom_texture), ("side", side_texture)),
        texture_max_size, texture_format, texture_quality,
    )

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
    if args.surface_mode != "auto" or args.cross_segments != 4:
        print("NOTE: --surface-mode / --cross-segments are legacy options; this script always uses CDT.", file=sys.stderr)
    try:
        import numpy
        import triangle
    except ImportError:
        print("error: install CDT dependencies: python -m pip install numpy triangle", file=sys.stderr)
        return 2
    input_path = args.input_csv.resolve()
    output_dir = args.output.resolve()
    if not input_path.is_file():
        print(f"error: input CSV does not exist: {input_path}", file=sys.stderr)
        return 2
    if (
        not all(math.isfinite(v) for v in (
            args.thickness, args.side_repeat_width, args.surface_repeat_size,
        ))
        or args.thickness <= 0
        or args.side_repeat_width <= 0
        or args.surface_repeat_size <= 0
        or args.cross_segments < 1
        or args.limit < 0
        or args.texture_max_size < 0
        or not 1 <= args.texture_quality <= 100
    ):
        print(
            "error: thickness/repeat sizes/quality must be positive and limits non-negative",
            file=sys.stderr,
        )
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
                if args.height_disable:
                    height = args.thickness + 0.01
                else:
                    height = float((row.get(args.height_field) or "").strip())
                    if not math.isfinite(height):
                        raise ValueError(f"invalid height {row.get(args.height_field)!r}")
                wkt = row.get(args.geometry_field) or ""
                if re.search(r"\)\s*,\s*\(", wkt):
                    raise ValueError("polygon holes are not supported; supply a single exterior ring")
                points = parse_polygon_z(wkt)
                if not all(math.isfinite(v) for point in points for v in point):
                    raise ValueError("polygon has non-finite source coordinates")
                top, bottom, longitude, latitude, altitude = local_surfaces(
                    points, height, args.thickness
                )
                create_canopy_glb(
                    top, bottom, top_texture, bottom_texture, side_texture,
                    destination, args.side_repeat_width, args.surface_repeat_size,
                    args.texture_max_size, args.texture_format,
                    args.texture_quality, args.surface_mode,
                    args.cross_segments,
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

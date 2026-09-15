#!/usr/bin/env python3
"""Create textured flat-roof building GLBs from POLYGON Z CSV rows.

The generated model origin is the center of the source polygon's 3D bounding
box: longitude/latitude/Z are each the midpoint of their original min/max.
GLB data follows glTF's Y-up convention, so Blender's glTF importer shows
the building rising along Blender +Z. The single-storey facade texture repeats
vertically exactly <s_height> times and restarts horizontally at every facade
corner. A placement CSV records the WGS84 anchor used by every GLB.

Simple configuration (the same textures for all selected types):

    python csv_polygon_to_flat_buildings.py buildings.csv -o output \
        --building-types 1,3,4 \
        --wall-texture textures/wall.jpg \
        --roof-texture textures/roof.jpg

Per-type configuration:

    python csv_polygon_to_flat_buildings.py buildings.csv -o output \
        --type-config building_types.json

building_types.json:

    {
      "1": {"wallTexture": "textures/wall_1.jpg",
            "roofTexture": "textures/roof_1.jpg"},
      "3": {"wallTexture": "textures/wall_3.png",
            "roofTexture": "textures/roof_3.png"}
    }

Texture paths in the JSON file are resolved relative to that JSON file.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import mimetypes
import re
import struct
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable


EARTH_A = 6378137.0
EARTH_E2 = 6.6943799901413165e-3
WKT_POLYGON_RE = re.compile(
    r"^\s*POLYGON\s*(?:Z|ZM|M)?\s*\(\((.*)\)\)\s*$",
    re.IGNORECASE | re.DOTALL,
)


@dataclass(frozen=True)
class TypeStyle:
    wall_texture: Path
    roof_texture: Path


@dataclass
class BufferView:
    offset: int
    length: int
    target: int | None = None
    stride: int | None = None


class BinaryBuilder:
    def __init__(self) -> None:
        self.data = bytearray()
        self.views: list[BufferView] = []

    def add(
        self,
        data: bytes,
        target: int | None = None,
        stride: int | None = None,
    ) -> int:
        while len(self.data) % 4:
            self.data.append(0)
        index = len(self.views)
        self.views.append(BufferView(len(self.data), len(data), target, stride))
        self.data.extend(data)
        return index

    def json_views(self) -> list[dict[str, Any]]:
        result: list[dict[str, Any]] = []
        for view in self.views:
            item: dict[str, Any] = {
                "buffer": 0,
                "byteOffset": view.offset,
                "byteLength": view.length,
            }
            if view.target is not None:
                item["target"] = view.target
            if view.stride is not None:
                item["byteStride"] = view.stride
            result.append(item)
        return result


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate flat-roof building GLBs from a polygon CSV"
    )
    parser.add_argument("input_csv", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument(
        "--building-types",
        help="comma-separated values from the type column",
    )
    parser.add_argument("--wall-texture", type=Path)
    parser.add_argument("--roof-texture", type=Path)
    parser.add_argument(
        "--type-config",
        type=Path,
        help="JSON map from type to wallTexture/roofTexture",
    )
    parser.add_argument("--id-field", default="id")
    parser.add_argument("--type-field", default="type")
    parser.add_argument("--height-field", default="height")
    parser.add_argument(
        "--storey-field",
        default="s_height",
        help="CSV field containing the wall texture's vertical repeat count "
             "(default: s_height)",
    )
    parser.add_argument("--geometry-field", default="WKT")
    parser.add_argument(
        "--wall-repeat-width",
        type=float,
        default=5.0,
        help="wall texture horizontal repeat size in metres",
    )
    parser.add_argument(
        "--roof-repeat-size",
        type=float,
        default=10.0,
        help="roof texture repeat size in metres",
    )
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument(
        "--limit", type=int, default=0,
        help="maximum number of generated buildings; 0 means unlimited",
    )
    return parser.parse_args()


def load_styles(args: argparse.Namespace) -> dict[str, TypeStyle]:
    if args.type_config:
        config_path = args.type_config.resolve()
        document = json.loads(config_path.read_text(encoding="utf-8-sig"))
        if not isinstance(document, dict) or not document:
            raise ValueError("--type-config must contain a non-empty JSON object")
        styles: dict[str, TypeStyle] = {}
        for type_name, value in document.items():
            if not isinstance(value, dict):
                raise ValueError(f"type {type_name!r} configuration must be an object")
            wall = value.get("wallTexture")
            roof = value.get("roofTexture")
            if not wall or not roof:
                raise ValueError(
                    f"type {type_name!r} requires wallTexture and roofTexture"
                )
            styles[str(type_name)] = TypeStyle(
                (config_path.parent / wall).resolve(),
                (config_path.parent / roof).resolve(),
            )
        return styles

    if not args.building_types or not args.wall_texture or not args.roof_texture:
        raise ValueError(
            "provide --type-config, or all of --building-types, "
            "--wall-texture and --roof-texture"
        )
    types = [value.strip() for value in args.building_types.split(",")]
    types = [value for value in types if value]
    if not types:
        raise ValueError("--building-types contains no values")
    style = TypeStyle(args.wall_texture.resolve(), args.roof_texture.resolve())
    return {value: style for value in types}


def validate_styles(styles: dict[str, TypeStyle]) -> None:
    for type_name, style in styles.items():
        for role, path in (
            ("wallTexture", style.wall_texture),
            ("roofTexture", style.roof_texture),
        ):
            if not path.is_file():
                raise ValueError(f"type {type_name!r} {role} not found: {path}")


def parse_polygon_z(wkt: str) -> list[tuple[float, float, float]]:
    match = WKT_POLYGON_RE.match(wkt)
    if not match:
        raise ValueError("geometry is not a simple POLYGON/POLYGON Z")
    # The model generator only needs the exterior footprint. When WKT has
    # interior rings, discard every hole and continue with the first ring.
    exterior_ring = re.split(
        r"\)\s*,\s*\(", match.group(1), maxsplit=1,
    )[0]
    points: list[tuple[float, float, float]] = []
    for token in exterior_ring.split(","):
        values = token.strip().split()
        if len(values) < 2:
            raise ValueError(f"invalid polygon coordinate: {token!r}")
        lon, lat = float(values[0]), float(values[1])
        altitude = float(values[2]) if len(values) >= 3 else 0.0
        points.append((lon, lat, altitude))
    if len(points) >= 2 and all(
        abs(points[0][index] - points[-1][index]) < 1e-12
        for index in range(2)
    ):
        points.pop()
    if len(points) < 3:
        raise ValueError("polygon has fewer than three unique vertices")
    return points


def parse_storeys(value: Any) -> float:
    """Return a usable facade repeat count, defaulting invalid values to 1."""
    try:
        storeys = float(str(value or "").strip())
    except (TypeError, ValueError):
        return 1.0
    return storeys if math.isfinite(storeys) and storeys > 0 else 1.0


def local_footprint(
    points: list[tuple[float, float, float]],
) -> tuple[list[tuple[float, float]], float, float]:
    anchor_lon, anchor_lat, _ = source_bbox_center(points)
    latitude = math.radians(anchor_lat)
    sin_latitude = math.sin(latitude)
    radius_n = EARTH_A / math.sqrt(1.0 - EARTH_E2 * sin_latitude ** 2)
    radius_m = (
        EARTH_A * (1.0 - EARTH_E2)
        / (1.0 - EARTH_E2 * sin_latitude ** 2) ** 1.5
    )
    footprint = [
        (
            math.radians(lon - anchor_lon) * radius_n * math.cos(latitude),
            math.radians(lat - anchor_lat) * radius_m,
        )
        for lon, lat, _ in points
    ]
    if signed_area(footprint) < 0:
        footprint.reverse()
    return footprint, anchor_lon, anchor_lat


def signed_area(points: list[tuple[float, float]]) -> float:
    return 0.5 * sum(
        points[i][0] * points[(i + 1) % len(points)][1]
        - points[(i + 1) % len(points)][0] * points[i][1]
        for i in range(len(points))
    )


def cross(a: tuple[float, float], b: tuple[float, float], c: tuple[float, float]) -> float:
    return (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0])


def point_in_triangle(
    point: tuple[float, float],
    a: tuple[float, float],
    b: tuple[float, float],
    c: tuple[float, float],
) -> bool:
    c1 = cross(a, b, point)
    c2 = cross(b, c, point)
    c3 = cross(c, a, point)
    epsilon = 1e-9
    return c1 >= -epsilon and c2 >= -epsilon and c3 >= -epsilon


def triangulate(points: list[tuple[float, float]]) -> list[tuple[int, int, int]]:
    remaining = list(range(len(points)))
    triangles: list[tuple[int, int, int]] = []
    guard = len(points) * len(points)
    while len(remaining) > 3 and guard > 0:
        guard -= 1
        ear_found = False
        for position, current in enumerate(remaining):
            previous = remaining[position - 1]
            following = remaining[(position + 1) % len(remaining)]
            if cross(points[previous], points[current], points[following]) <= 1e-10:
                continue
            if any(
                point_in_triangle(
                    points[candidate], points[previous], points[current], points[following]
                )
                for candidate in remaining
                if candidate not in (previous, current, following)
            ):
                continue
            triangles.append((previous, current, following))
            del remaining[position]
            ear_found = True
            break
        if not ear_found:
            raise ValueError("polygon could not be triangulated; it may be invalid")
    if len(remaining) == 3:
        triangles.append(tuple(remaining))
    if not triangles:
        raise ValueError("polygon triangulation produced no faces")
    return triangles


def pack_floats(values: Iterable[float]) -> bytes:
    values = list(values)
    return struct.pack("<" + "f" * len(values), *values)


def pack_indices(values: list[int]) -> tuple[bytes, int]:
    if max(values, default=0) <= 65535:
        return struct.pack("<" + "H" * len(values), *values), 5123
    return struct.pack("<" + "I" * len(values), *values), 5125


def vector_min_max(
    positions: list[tuple[float, float, float]],
) -> tuple[list[float], list[float]]:
    return (
        [min(point[i] for point in positions) for i in range(3)],
        [max(point[i] for point in positions) for i in range(3)],
    )


def mime_type(path: Path) -> str:
    data = path.read_bytes()[:16]
    if data.startswith(b"\x89PNG\r\n\x1a\n"):
        return "image/png"
    if data.startswith(b"\xff\xd8\xff"):
        return "image/jpeg"
    if data.startswith(b"RIFF") and data[8:12] == b"WEBP":
        return "image/webp"
    guessed = mimetypes.guess_type(path.name)[0]
    if guessed in ("image/png", "image/jpeg", "image/webp"):
        return guessed
    raise ValueError(f"unsupported texture image format: {path}")


def append_accessor(
    document: dict[str, Any],
    builder: BinaryBuilder,
    data: bytes,
    component_type: int,
    accessor_type: str,
    count: int,
    target: int,
    minimum: list[float] | None = None,
    maximum: list[float] | None = None,
) -> int:
    view = builder.add(data, target)
    accessor: dict[str, Any] = {
        "bufferView": view,
        "componentType": component_type,
        "count": count,
        "type": accessor_type,
    }
    if minimum is not None:
        accessor["min"] = minimum
    if maximum is not None:
        accessor["max"] = maximum
    index = len(document["accessors"])
    document["accessors"].append(accessor)
    return index


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


def wall_horizontal_segments(
    length: float,
    repeat_width: float,
) -> list[tuple[float, float, float, float]]:
    """Return wall distance/U ranges, using the texture's right quarter as fill."""
    right_quarter_start = 3.0 / 4.0
    if length < repeat_width:
        return [(0.0, length, right_quarter_start, 1.0)]

    full_repeats = math.floor(length / repeat_width)
    full_width = full_repeats * repeat_width
    remainder = length - full_width
    epsilon = 1e-9
    if remainder > epsilon and remainder < repeat_width * (2.0 / 3.0):
        return [
            (0.0, full_width, 0.0, float(full_repeats)),
            (full_width, length, right_quarter_start, 1.0),
        ]

    # A sufficiently wide remainder keeps the original partial-repeat mapping.
    return [(0.0, length, 0.0, length / repeat_width)]


def create_glb(
    footprint: list[tuple[float, float]],
    height: float,
    storeys: float,
    style: TypeStyle,
    destination: Path,
    wall_repeat_width: float,
    roof_repeat_size: float,
) -> None:
    roof_triangles = triangulate(footprint)
    wall_positions: list[tuple[float, float, float]] = []
    wall_normals: list[tuple[float, float, float]] = []
    wall_uvs: list[tuple[float, float]] = []
    wall_indices: list[int] = []
    for index, start in enumerate(footprint):
        end = footprint[(index + 1) % len(footprint)]
        dx, dy = end[0] - start[0], end[1] - start[1]
        length = math.hypot(dx, dy)
        if length <= 1e-6:
            continue
        # Convert the local ENU coordinates to glTF's standard Y-up axes:
        # (east, north, up) -> (X, Y, Z) = (east, up, -north).
        normal = (dy / length, 0.0, dx / length)
        # The facade image represents exactly one storey, so its vertical UV
        # range is the CSV storey count rather than a metre-based estimate.
        # glTF textures use an origin opposite to the source facade images;
        # assign the larger V to the bottom vertices to keep images upright.
        v1 = storeys
        # Every footprint edge owns separate vertices. Normally U restarts at
        # each corner. Narrow faces and narrow final remainders instead map the
        # texture's right quarter, avoiding a squeezed partial facade pattern.
        for distance0, distance1, u0, u1 in wall_horizontal_segments(
            length, wall_repeat_width,
        ):
            ratio0, ratio1 = distance0 / length, distance1 / length
            segment_start = (start[0] + dx * ratio0, start[1] + dy * ratio0)
            segment_end = (start[0] + dx * ratio1, start[1] + dy * ratio1)
            first = len(wall_positions)
            wall_positions.extend((
                (segment_start[0], 0.0, -segment_start[1]),
                (segment_end[0], 0.0, -segment_end[1]),
                (segment_end[0], height, -segment_end[1]),
                (segment_start[0], height, -segment_start[1]),
            ))
            wall_normals.extend((normal, normal, normal, normal))
            wall_uvs.extend(((u0, v1), (u1, v1), (u1, 0.0), (u0, 0.0)))
            wall_indices.extend((
                first, first + 1, first + 2,
                first, first + 2, first + 3,
            ))

    roof_positions = [(x, height, -y) for x, y in footprint]
    roof_normals = [(0.0, 1.0, 0.0)] * len(footprint)
    roof_uvs = [(x / roof_repeat_size, y / roof_repeat_size) for x, y in footprint]
    roof_indices = [value for triangle in roof_triangles for value in triangle]

    builder = BinaryBuilder()
    document: dict[str, Any] = {
        "asset": {"version": "2.0", "generator": "csv_polygon_to_flat_buildings.py"},
        "scene": 0,
        "scenes": [{"nodes": [0]}],
        "nodes": [{"mesh": 0, "name": destination.stem}],
        "meshes": [{"name": destination.stem, "primitives": []}],
        "accessors": [],
        "bufferViews": [],
        "buffers": [],
        "images": [],
        "textures": [],
        "samplers": [{"magFilter": 9729, "minFilter": 9987, "wrapS": 10497, "wrapT": 10497}],
        "materials": [],
    }
    document["meshes"][0]["primitives"].append(add_primitive(
        document, builder, wall_positions, wall_normals,
        wall_uvs, wall_indices, 0,
    ))
    document["meshes"][0]["primitives"].append(add_primitive(
        document, builder, roof_positions, roof_normals,
        roof_uvs, roof_indices, 1,
    ))

    for name, texture_path in (
        ("wall", style.wall_texture),
        ("roof", style.roof_texture),
    ):
        image_data = texture_path.read_bytes()
        image_view = builder.add(image_data)
        image_index = len(document["images"])
        document["images"].append({
            "name": f"{destination.stem}_{name}",
            "bufferView": image_view,
            "mimeType": mime_type(texture_path),
        })
        document["textures"].append({"sampler": 0, "source": image_index})
        document["materials"].append({
            "name": f"{destination.stem}_{name}",
            "pbrMetallicRoughness": {
                "baseColorTexture": {"index": image_index},
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


def safe_filename(value: str) -> str:
    value = re.sub(r"[^0-9A-Za-z._-]+", "_", value.strip()).strip("._")
    return value or "unknown"


def source_bbox_center(
    points: list[tuple[float, float, float]],
) -> tuple[float, float, float]:
    """Return lon, lat and Z at the source polygon's 3D bbox center."""
    return tuple(
        (min(point[axis] for point in points)
         + max(point[axis] for point in points)) * 0.5
        for axis in range(3)
    )


def main() -> int:
    args = parse_args()
    input_path = args.input_csv.resolve()
    output_dir = args.output.resolve()
    if not input_path.is_file():
        print(f"error: input CSV does not exist: {input_path}", file=sys.stderr)
        return 2
    if min(
        args.wall_repeat_width,
        args.roof_repeat_size,
    ) <= 0 or args.limit < 0:
        print("error: texture repeat sizes must be positive and limit non-negative", file=sys.stderr)
        return 2
    try:
        styles = load_styles(args)
        validate_styles(styles)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / f"{input_path.stem}_placements.csv"
    manifest_fields = [
        "id", "type", "model", "longitude", "latitude", "altitude",
        "height", "storeys", "origin", "sourceRow",
    ]
    generated = skipped = ignored = failed = 0
    manifest_rows: list[dict[str, Any]] = []
    with input_path.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        required = {
            args.id_field, args.type_field, args.height_field,
            args.storey_field, args.geometry_field,
        }
        missing = required.difference(reader.fieldnames or [])
        if missing:
            print(f"error: CSV fields not found: {sorted(missing)}", file=sys.stderr)
            return 2
        for row_number, row in enumerate(reader, 2):
            building_type = (row.get(args.type_field) or "").strip()
            style = styles.get(building_type)
            if style is None:
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
                if not math.isfinite(height) or height <= 0:
                    raise ValueError(f"invalid height {row.get(args.height_field)!r}")
                storeys = parse_storeys(row.get(args.storey_field))
                points = parse_polygon_z(row.get(args.geometry_field) or "")
                footprint, longitude, latitude = local_footprint(points)
                _, _, altitude = source_bbox_center(points)
                create_glb(
                    footprint, height, storeys, style, destination,
                    args.wall_repeat_width, args.roof_repeat_size,
                )
                manifest_rows.append({
                    "id": identifier,
                    "type": building_type,
                    "model": destination.name,
                    "longitude": f"{longitude:.12f}",
                    "latitude": f"{latitude:.12f}",
                    "altitude": f"{altitude:.6f}",
                    "height": f"{height:.6f}",
                    "storeys": f"{storeys:g}",
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
        f"{ignored} non-building row(s), {failed} failed; "
        f"placements={manifest_path}"
    )
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

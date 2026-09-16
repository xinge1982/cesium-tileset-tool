#!/usr/bin/env python3
"""Generate one extruded toll-station text GLB for every CSV row.

Run with Blender, for example:

    blender --background --python csv_toll_station_3d_text.py -- \
        stations.csv --output output --height 3.0 \
        --height-offset 1.5 --letter-spacing 1.1 \
        --font C:/Windows/Fonts/simhei.ttf

The model origin stays at the CSV placement point; the text's horizontal center
is above it and its bottom is raised by --height-offset. Blender uses Z-up while
editing; the glTF exporter performs the standard glTF Y-up axis conversion.
Before any optional heading is baked, the text front faces local -Y and local
+Y is its north-reference axis. By default CSV angle is only recorded in the
placement manifest for the tileset instance transform.
"""

from __future__ import annotations

import argparse
import csv
import math
import re
import sys
from pathlib import Path
from typing import Any

try:
    import bmesh
    import bpy
    from mathutils import Matrix, Vector
except ImportError:  # Give a useful message when launched with normal Python.
    bmesh = None
    bpy = None
    Matrix = None
    Vector = None


POINT_WKT_RE = re.compile(
    r"^\s*POINT\s*(?:Z|ZM|M)?\s*\(\s*"
    r"([-+0-9.eE]+)\s+([-+0-9.eE]+)"
    r"(?:\s+([-+0-9.eE]+))?(?:\s+[-+0-9.eE]+)?\s*\)\s*$",
    re.IGNORECASE,
)


def blender_arguments() -> list[str]:
    return sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else sys.argv[1:]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate extruded toll-station name GLBs from CSV data"
    )
    parser.add_argument("input_csv", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument(
        "--height", type=float, required=True,
        help="uniform text model height in metres",
    )
    parser.add_argument(
        "--height-offset", type=float, default=0.0,
        help="raise the entire text above its CSV placement point in metres",
    )
    parser.add_argument(
        "--font", type=Path,
        help="Chinese font file; defaults to a detected SimHei/Noto Sans CJK font",
    )
    parser.add_argument(
        "--text-suffix", default="收费站",
        help="text appended unless name already ends with it (default: 收费站)",
    )
    parser.add_argument(
        "--depth-ratio", type=float, default=0.10,
        help="extrusion depth relative to text height (default: 0.10)",
    )
    parser.add_argument(
        "--bevel-ratio", type=float, default=0.008,
        help="edge bevel depth relative to text height (default: 0.008)",
    )
    parser.add_argument(
        "--curve-resolution", type=int, default=2,
        help="font outline curve resolution; lower values reduce GLB size (default: 2)",
    )
    parser.add_argument(
        "--bevel-resolution", type=int, default=0,
        help="bevel segment count; 0 keeps a small bevel with minimum geometry (default: 0)",
    )
    parser.add_argument(
        "--merge-distance", type=float, default=1e-6,
        help="merge duplicate mesh vertices within this distance in metres (default: 1e-6)",
    )
    parser.add_argument(
        "--decimate-ratio", type=float, default=1.0,
        help="optional mesh simplification ratio in (0, 1]; 1 disables it (default: 1)",
    )
    parser.add_argument(
        "--letter-spacing", "--character-spacing",
        dest="letter_spacing", type=float, default=1.0,
        help="Blender text character spacing multiplier (default: 1.0)",
    )
    parser.add_argument("--color", default="#B88716", help="material color #RRGGBB")
    parser.add_argument("--metallic", type=float, default=0.55)
    parser.add_argument("--roughness", type=float, default=0.32)
    parser.add_argument("--emission-strength", type=float, default=0.08)
    parser.add_argument(
        "--bake-angle", action="store_true",
        help="bake CSV clockwise-from-north angle into GLB; normally leave disabled",
    )
    parser.add_argument("--id-field", default="id")
    parser.add_argument("--name-field", default="name")
    parser.add_argument("--angle-field", default="angle")
    parser.add_argument("--geometry-field", default="WKT")
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument("--limit", type=int, default=0)
    return parser.parse_args(blender_arguments())


def find_font(configured: Path | None) -> Path:
    if configured is not None:
        font = configured.expanduser().resolve()
        if not font.is_file():
            raise ValueError(f"font does not exist: {font}")
        return font

    candidates = [
        Path("C:/Windows/Fonts/simhei.ttf"),
        Path("C:/Windows/Fonts/msyh.ttc"),
        Path("/usr/share/fonts/opentype/noto/NotoSansCJK-Black.ttc"),
        Path("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"),
        Path("/System/Library/Fonts/PingFang.ttc"),
    ]
    for font in candidates:
        if font.is_file():
            return font.resolve()
    raise ValueError("no Chinese font found; specify --font, e.g. simhei.ttf")


def safe_filename(value: str) -> str:
    result = re.sub(r"[^0-9A-Za-z._-]+", "_", value.strip()).strip("._")
    return result or "unknown"


def parse_point_wkt(value: str) -> tuple[float, float, float]:
    match = POINT_WKT_RE.match(value)
    if match is None:
        raise ValueError("geometry is not POINT/POINT Z WKT")
    lon = float(match.group(1))
    lat = float(match.group(2))
    altitude = float(match.group(3)) if match.group(3) is not None else 0.0
    if not all(math.isfinite(number) for number in (lon, lat, altitude)):
        raise ValueError("point coordinates must be finite")
    return lon, lat, altitude


def parse_hex_color(value: str) -> tuple[float, float, float, float]:
    text = value.strip().lstrip("#")
    if len(text) != 6 or not re.fullmatch(r"[0-9a-fA-F]{6}", text):
        raise ValueError("--color must use #RRGGBB")
    rgb = [int(text[index:index + 2], 16) / 255.0 for index in (0, 2, 4)]

    def srgb_to_linear(component: float) -> float:
        if component <= 0.04045:
            return component / 12.92
        return ((component + 0.055) / 1.055) ** 2.4

    return tuple(srgb_to_linear(component) for component in rgb) + (1.0,)


def clear_scene() -> None:
    bpy.ops.object.select_all(action="SELECT")
    bpy.ops.object.delete(use_global=False)
    for collection in (bpy.data.curves, bpy.data.meshes, bpy.data.materials):
        for block in list(collection):
            if block.users == 0:
                collection.remove(block)


def set_principled_input(node: Any, names: tuple[str, ...], value: Any) -> None:
    for name in names:
        socket = node.inputs.get(name)
        if socket is not None:
            socket.default_value = value
            return


def create_material(
    name: str,
    color: tuple[float, float, float, float],
    metallic: float,
    roughness: float,
    emission_strength: float,
) -> Any:
    material = bpy.data.materials.new(name)
    material.use_nodes = True
    principled = material.node_tree.nodes.get("Principled BSDF")
    if principled is None:
        raise RuntimeError("Principled BSDF node is unavailable")
    set_principled_input(principled, ("Base Color",), color)
    set_principled_input(principled, ("Metallic",), metallic)
    set_principled_input(principled, ("Roughness",), roughness)
    set_principled_input(principled, ("Emission Color", "Emission"), color)
    set_principled_input(principled, ("Emission Strength",), emission_strength)
    return material


def evaluated_bounds(obj: Any) -> tuple[list[float], list[float]]:
    # Blender 5 exposes bound_box corners as bpy_prop_array. Matrix
    # multiplication only accepts mathutils.Vector (or compatible values).
    corners = [obj.matrix_world @ Vector(corner) for corner in obj.bound_box]
    minimum = [min(corner[axis] for corner in corners) for axis in range(3)]
    maximum = [max(corner[axis] for corner in corners) for axis in range(3)]
    return minimum, maximum


def optimize_text_mesh(obj: Any, merge_distance: float, decimate_ratio: float) -> None:
    """Remove redundant geometry and optionally simplify the converted glyph mesh."""
    mesh = obj.data
    editable = bmesh.new()
    try:
        editable.from_mesh(mesh)
        if merge_distance > 0 and editable.verts:
            bmesh.ops.remove_doubles(
                editable, verts=list(editable.verts), dist=merge_distance,
            )
        if editable.edges:
            bmesh.ops.dissolve_degenerate(
                editable,
                edges=list(editable.edges),
                dist=max(merge_distance, 1e-12),
            )
        loose_vertices = [vertex for vertex in editable.verts if not vertex.link_edges]
        if loose_vertices:
            bmesh.ops.delete(editable, geom=loose_vertices, context="VERTS")
        editable.to_mesh(mesh)
    finally:
        editable.free()
    mesh.validate(clean_customdata=True)
    mesh.update()

    if decimate_ratio < 1.0:
        modifier = obj.modifiers.new(name="TextSizeDecimate", type="DECIMATE")
        modifier.decimate_type = "COLLAPSE"
        modifier.ratio = decimate_ratio
        modifier.use_collapse_triangulate = True
        bpy.context.view_layer.objects.active = obj
        obj.select_set(True)
        bpy.ops.object.modifier_apply(modifier=modifier.name)


def create_text_model(
    text: str,
    height: float,
    angle: float,
    font: Any,
    output_path: Path,
    args: argparse.Namespace,
    color: tuple[float, float, float, float],
) -> None:
    clear_scene()
    curve = bpy.data.curves.new(output_path.stem, type="FONT")
    curve.body = text
    curve.font = font
    curve.align_x = "CENTER"
    curve.align_y = "BOTTOM_BASELINE"
    curve.size = 1.0
    curve.space_character = args.letter_spacing
    curve.extrude = args.depth_ratio
    curve.bevel_depth = args.bevel_ratio
    curve.bevel_resolution = args.bevel_resolution
    curve.resolution_u = args.curve_resolution

    obj = bpy.data.objects.new(output_path.stem, curve)
    bpy.context.collection.objects.link(obj)
    obj.data.materials.append(create_material(
        f"{output_path.stem}_material", color,
        args.metallic, args.roughness, args.emission_strength,
    ))
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj

    # Font front is local +Z. Rotate it to world -Y while keeping glyph-up at +Z.
    obj.rotation_euler[0] = math.radians(90.0)
    bpy.ops.object.convert(target="MESH")
    bpy.ops.object.transform_apply(location=False, rotation=True, scale=False)

    minimum, maximum = evaluated_bounds(obj)
    current_height = maximum[2] - minimum[2]
    if current_height <= 1e-9:
        raise ValueError(f"font produced empty or zero-height text: {text!r}")
    scale = height / current_height
    obj.scale = (scale, scale, scale)
    bpy.ops.object.transform_apply(location=False, rotation=False, scale=True)
    optimize_text_mesh(obj, args.merge_distance, args.decimate_ratio)

    # Keep the placement origin below the horizontal center of the text and
    # raise its bottom by the configured metre offset.
    minimum, maximum = evaluated_bounds(obj)
    center_x = (minimum[0] + maximum[0]) * 0.5
    center_y = (minimum[1] + maximum[1]) * 0.5
    obj.data.transform(Matrix.Translation((
        -center_x, -center_y, -minimum[2] + args.height_offset,
    )))

    if args.bake_angle:
        # Bearings are clockwise from north (+Y), opposite Blender's positive Z rotation.
        obj.rotation_euler[2] = math.radians(-angle)
        bpy.ops.object.transform_apply(location=False, rotation=True, scale=False)

    bpy.ops.object.select_all(action="DESELECT")
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    output_path.parent.mkdir(parents=True, exist_ok=True)
    bpy.ops.export_scene.gltf(
        filepath=str(output_path),
        export_format="GLB",
        use_selection=True,
        export_materials="EXPORT",
        export_cameras=False,
        export_lights=False,
        export_animations=False,
        export_skins=False,
        export_morph=False,
    )


def output_text(name: str, suffix: str) -> str:
    suffix = suffix.strip()
    return name if not suffix or name.endswith(suffix) else name + suffix


def main() -> int:
    args = parse_args()
    if bpy is None:
        print("error: run this script with Blender's Python", file=sys.stderr)
        return 2
    if (args.limit < 0 or args.depth_ratio <= 0 or args.bevel_ratio < 0
            or args.curve_resolution < 1 or args.bevel_resolution < 0
            or not math.isfinite(args.merge_distance) or args.merge_distance < 0
            or not math.isfinite(args.decimate_ratio)
            or not 0 < args.decimate_ratio <= 1
            or not math.isfinite(args.height) or args.height <= 0
            or not math.isfinite(args.height_offset)):
        print(
            "error: invalid geometry option; resolutions/merge must be non-negative, "
            "curve/depth/height positive, and decimate ratio within (0, 1]",
            file=sys.stderr,
        )
        return 2
    if not 0.0 <= args.metallic <= 1.0 or not 0.0 <= args.roughness <= 1.0:
        print("error: metallic and roughness must be within 0..1", file=sys.stderr)
        return 2
    if args.emission_strength < 0 or args.letter_spacing <= 0:
        print("error: emission must be non-negative and spacing must be positive", file=sys.stderr)
        return 2

    input_path = args.input_csv.expanduser().resolve()
    output_dir = args.output.expanduser().resolve()
    if not input_path.is_file():
        print(f"error: input CSV does not exist: {input_path}", file=sys.stderr)
        return 2
    try:
        font_path = find_font(args.font)
        color = parse_hex_color(args.color)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    font = bpy.data.fonts.load(str(font_path))
    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / f"{input_path.stem}_text_placements.csv"
    fields = [
        "id", "name", "text", "model", "longitude", "latitude", "altitude",
        "height", "heightOffset", "letterSpacing", "sourceAngle",
        "curveResolution", "bevelResolution", "decimateRatio",
        "angleBaked", "recommendedObjAngle",
        "frontDirection", "northReferenceAxis", "origin", "sourceRow",
    ]
    rows: list[dict[str, Any]] = []
    generated = skipped = failed = 0

    with input_path.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        required = {
            args.id_field, args.name_field, args.angle_field, args.geometry_field,
        }
        missing = required.difference(reader.fieldnames or [])
        if missing:
            print(f"error: CSV fields not found: {sorted(missing)}", file=sys.stderr)
            return 2

        for row_number, row in enumerate(reader, 2):
            if args.limit and generated >= args.limit:
                break
            identifier = (row.get(args.id_field) or "").strip()
            name = (row.get(args.name_field) or "").strip()
            destination = output_dir / f"{safe_filename(identifier)}.glb"
            if destination.exists() and not args.overwrite:
                skipped += 1
                continue
            try:
                if not identifier:
                    raise ValueError("id is empty")
                if not name:
                    raise ValueError("name is empty")
                angle = float((row.get(args.angle_field) or "").strip())
                if not math.isfinite(angle):
                    raise ValueError("angle must be finite")
                longitude, latitude, altitude = parse_point_wkt(
                    row.get(args.geometry_field) or ""
                )
                text = output_text(name, args.text_suffix)
                create_text_model(
                    text, args.height, angle, font, destination, args, color,
                )
                rows.append({
                    "id": identifier,
                    "name": name,
                    "text": text,
                    "model": destination.name,
                    "longitude": f"{longitude:.12f}",
                    "latitude": f"{latitude:.12f}",
                    "altitude": f"{altitude:.6f}",
                    "height": f"{args.height:.6f}",
                    "heightOffset": f"{args.height_offset:.6f}",
                    "letterSpacing": f"{args.letter_spacing:g}",
                    "curveResolution": args.curve_resolution,
                    "bevelResolution": args.bevel_resolution,
                    "decimateRatio": f"{args.decimate_ratio:g}",
                    "sourceAngle": f"{angle:.10f}",
                    "angleBaked": str(args.bake_angle).lower(),
                    "recommendedObjAngle": "0" if args.bake_angle else f"{angle:.10f}",
                    "frontDirection": "-Y before optional baked angle",
                    "northReferenceAxis": "+Y",
                    "origin": "placement-point; text-bottom-at-heightOffset",
                    "sourceRow": row_number,
                })
                generated += 1
                print(f"OK   row {row_number}: {destination.name} {text!r}")
            except Exception as exc:
                failed += 1
                print(f"FAIL row {row_number}, id={identifier!r}: {exc}", file=sys.stderr)

    with manifest_path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        writer.writerows(rows)
    print(
        f"finished: {generated} generated, {skipped} skipped, {failed} failed; "
        f"font={font_path}; placements={manifest_path}"
    )
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())

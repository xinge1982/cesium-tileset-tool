#!/usr/bin/env python3
"""Build GLB LOD variants by retaining mesh objects with selected suffixes.

Run with Blender rather than ordinary Python:

    blender --background --python glb_filter_meshes_by_suffix.py -- input \
        --output output/lod2 --mesh-suffixes qd qmb zt --overwrite

The input directory is scanned for GLB files. For every file, mesh objects
whose object name or mesh datablock name ends with one of the supplied suffixes
are retained; all other mesh objects are removed before exporting a GLB with
the same relative path and filename.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import bpy


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


def normalized_suffixes(values: list[str], case_sensitive: bool) -> tuple[str, ...]:
    result: list[str] = []
    seen: set[str] = set()
    for value in values:
        for item in value.split(","):
            suffix = item.strip()
            if not suffix:
                continue
            if not case_sensitive:
                suffix = suffix.casefold()
            if suffix not in seen:
                seen.add(suffix)
                result.append(suffix)
    return tuple(result)


def name_has_suffix(name: str, suffixes: tuple[str, ...],
                    case_sensitive: bool) -> bool:
    candidate = name if case_sensitive else name.casefold()
    return candidate.endswith(suffixes)


def mesh_matches(obj: object, suffixes: tuple[str, ...],
                 case_sensitive: bool) -> bool:
    if name_has_suffix(obj.name, suffixes, case_sensitive):
        return True
    mesh_name = obj.data.name if obj.data is not None else ""
    return name_has_suffix(mesh_name, suffixes, case_sensitive)


def import_and_filter(source: Path, suffixes: tuple[str, ...],
                      case_sensitive: bool) -> tuple[list[str], list[str]]:
    reset_scene()
    bpy.ops.import_scene.gltf(filepath=str(source))
    meshes = [obj for obj in list(bpy.context.scene.objects)
              if obj.type == "MESH"]
    if not meshes:
        raise ValueError("GLB contains no mesh objects")
    available_names = sorted(obj.name for obj in meshes)

    retained: list[str] = []
    removed: list[str] = []
    for obj in meshes:
        if mesh_matches(obj, suffixes, case_sensitive):
            retained.append(obj.name)
        else:
            removed.append(obj.name)
            bpy.data.objects.remove(obj, do_unlink=True)

    if not retained:
        available = ", ".join(available_names)
        raise ValueError(
            f"no mesh names matched suffixes {list(suffixes)}; "
            f"available mesh objects: {available}")
    return retained, removed


def export_glb(destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    bpy.ops.export_scene.gltf(
        filepath=str(destination),
        export_format="GLB",
        use_selection=False,
        export_texcoords=True,
        export_normals=True,
        export_materials="EXPORT",
        export_image_format="AUTO",
    )


def convert(source: Path, destination: Path, suffixes: tuple[str, ...],
            case_sensitive: bool) -> tuple[list[str], list[str]]:
    retained, removed = import_and_filter(source, suffixes, case_sensitive)
    export_glb(destination)
    return retained, removed


def parse_args() -> argparse.Namespace:
    argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    parser = argparse.ArgumentParser(
        description="Retain GLB mesh objects whose names match selected suffixes")
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument(
        "--mesh-suffixes", nargs="+", required=True,
        help="suffixes to retain; values may also be comma-separated")
    parser.add_argument("--recursive", "-r", action="store_true")
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument(
        "--case-sensitive", action="store_true",
        help="match suffixes case-sensitively (default: case-insensitive)")
    return parser.parse_args(argv)


def main() -> int:
    args = parse_args()
    input_dir = args.input_dir.resolve()
    output_dir = args.output.resolve()
    if not input_dir.is_dir():
        print(f"error: input directory does not exist: {input_dir}", file=sys.stderr)
        return 2

    suffixes = normalized_suffixes(
        args.mesh_suffixes, case_sensitive=args.case_sensitive)
    if not suffixes:
        print("error: at least one non-empty mesh suffix is required", file=sys.stderr)
        return 2

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
            retained, removed = convert(
                source, destination, suffixes, args.case_sensitive)
            succeeded += 1
            print(f"OK   {relative} -> {destination} "
                  f"[kept {len(retained)}: {', '.join(retained)}; "
                  f"removed {len(removed)}]")
        except Exception as exc:
            failed += 1
            print(f"FAIL {relative}: {exc}", file=sys.stderr)

    print(f"finished: {succeeded} converted, {skipped} skipped, "
          f"{failed} failed")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

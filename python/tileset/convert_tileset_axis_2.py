#!/usr/bin/env python3
"""Bake model axes and double-sided materials into a copied 3D Tiles tileset.

Source convention:
    X right, Y forward, Z up

Output glTF convention:
    X right, Y up, -Z forward

The conversion is implemented as an Rx(-90 degrees) parent node in each glTF
scene. Vertex buffers, RTC_CENTER values, I3DM instance positions, feature
tables, batch tables and subtree files are preserved.

Every glTF material is set to ``doubleSided: true``. Existing ``alphaMode``,
``alphaCutoff`` and PBR material properties are preserved unchanged.

Supported model locations:
  * JSON glTF or GLB payload inside B3DM
  * embedded GLB inside I3DM
  * B3DM/I3DM recursively inside CMPT
  * standalone .gltf and .glb files (including I3DM external models)

No third-party Python packages are required.
"""

from __future__ import annotations

import argparse
import json
import shutil
import struct
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any


GLB_MAGIC = b"glTF"
JSON_CHUNK_TYPE = 0x4E4F534A
BIN_CHUNK_TYPE = 0x004E4942
AXIS_NODE_NAME = "AxisConversion_ZupYforward_To_GLTF"
AXIS_MARKER = "Z-up/Y-forward to glTF Y-up/-Z-forward"

# glTF matrices are column-major. This maps (x, y, z) -> (x, z, -y).
AXIS_MATRIX = [
    1.0, 0.0, 0.0, 0.0,
    0.0, 0.0, -1.0, 0.0,
    0.0, 1.0, 0.0, 0.0,
    0.0, 0.0, 0.0, 1.0,
]


class ConversionError(RuntimeError):
    pass


@dataclass
class Statistics:
    b3dm_changed: int = 0
    i3dm_changed: int = 0
    cmpt_changed: int = 0
    gltf_changed: int = 0
    glb_changed: int = 0
    already_converted: int = 0
    external_i3dm: int = 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Copy a tileset and bake Z-up/Y-forward models to standard glTF axes."
    )
    parser.add_argument("input_dir", type=Path, help="Input directory containing tileset.json")
    parser.add_argument("output_dir", type=Path, help="Output directory")
    parser.add_argument(
        "--tileset-json", default="tileset.json",
        help="Tileset path relative to input_dir (default: tileset.json)",
    )
    parser.add_argument("--force", action="store_true", help="Replace output_dir if it exists")
    parser.add_argument(
        "--skip-standalone-glb", action="store_true",
        help="Do not modify standalone .glb files; embedded GLBs are still handled",
    )
    return parser.parse_args()


def u32(data: bytes, offset: int) -> int:
    if offset + 4 > len(data):
        raise ConversionError(f"Cannot read uint32 at byte {offset}")
    return struct.unpack_from("<I", data, offset)[0]


def pad(data: bytes, alignment: int, byte: bytes) -> bytes:
    amount = (-len(data)) % alignment
    return data + byte * amount


def decode_json_bytes(data: bytes, description: str) -> dict[str, Any]:
    try:
        value = json.loads(data.rstrip(b" \t\r\n\x00").decode("utf-8-sig"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ConversionError(f"Invalid JSON in {description}: {error}") from error
    if not isinstance(value, dict):
        raise ConversionError(f"Expected a JSON object in {description}")
    return value


def encode_json(value: dict[str, Any]) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def has_axis_marker(document: dict[str, Any]) -> bool:
    extras = document.get("asset", {}).get("extras", {})
    if isinstance(extras, dict) and extras.get("axisConversion") == AXIS_MARKER:
        return True
    return any(
        isinstance(node, dict) and node.get("name") == AXIS_NODE_NAME
        for node in document.get("nodes", [])
    )


def set_materials_double_sided(document: dict[str, Any]) -> bool:
    """Set all glTF materials double-sided without changing alpha/PBR data."""
    materials = document.get("materials", [])
    if not isinstance(materials, list):
        raise ConversionError("glTF materials must be an array")

    changed = False
    for material_index, material in enumerate(materials):
        if not isinstance(material, dict):
            raise ConversionError(f"glTF material {material_index} is not an object")
        if material.get("doubleSided") is not True:
            material["doubleSided"] = True
            changed = True
    return changed


def add_axis_nodes(document: dict[str, Any]) -> bool:
    """Add axis parents and enable double-sided materials."""
    material_changed = set_materials_double_sided(document)
    if has_axis_marker(document):
        return material_changed

    nodes = document.setdefault("nodes", [])
    scenes = document.get("scenes")
    if not isinstance(nodes, list):
        raise ConversionError("glTF nodes must be an array")

    # A glTF without scenes may still have nodes. Create the default scene so
    # the transformed result has unambiguous roots.
    if not isinstance(scenes, list) or not scenes:
        child_indices = list(range(len(nodes)))
        scenes = [{"nodes": child_indices}]
        document["scenes"] = scenes
        document["scene"] = 0

    changed = False
    for scene_index, scene in enumerate(scenes):
        if not isinstance(scene, dict):
            raise ConversionError(f"glTF scene {scene_index} is not an object")
        old_roots = scene.get("nodes", [])
        if not isinstance(old_roots, list):
            raise ConversionError(f"glTF scene {scene_index}.nodes is not an array")
        if not old_roots:
            continue
        new_index = len(nodes)
        nodes.append({
            "name": AXIS_NODE_NAME,
            "matrix": AXIS_MATRIX,
            "children": list(old_roots),
        })
        scene["nodes"] = [new_index]
        changed = True

    if changed:
        asset = document.setdefault("asset", {})
        if not isinstance(asset, dict):
            raise ConversionError("glTF asset must be an object")
        extras = asset.setdefault("extras", {})
        if not isinstance(extras, dict):
            raise ConversionError("glTF asset.extras must be an object when present")
        extras["axisConversion"] = AXIS_MARKER
    return changed or material_changed


def transform_glb_bytes(data: bytes, description: str) -> tuple[bytes, bool]:
    if len(data) < 12 or data[:4] != GLB_MAGIC:
        raise ConversionError(f"Invalid GLB header in {description}")
    version, declared_length = struct.unpack_from("<II", data, 4)
    if version != 2 or declared_length != len(data):
        raise ConversionError(
            f"Expected glTF 2.0 GLB in {description}; version={version}, "
            f"declared={declared_length}, actual={len(data)}"
        )

    chunks: list[tuple[int, bytes]] = []
    offset = 12
    while offset < len(data):
        if offset + 8 > len(data):
            raise ConversionError(f"Truncated GLB chunk header in {description}")
        length, chunk_type = struct.unpack_from("<II", data, offset)
        offset += 8
        payload = data[offset:offset + length]
        if len(payload) != length:
            raise ConversionError(f"Truncated GLB chunk in {description}")
        chunks.append((chunk_type, payload))
        offset += length

    json_indices = [i for i, (kind, _) in enumerate(chunks) if kind == JSON_CHUNK_TYPE]
    if len(json_indices) != 1:
        raise ConversionError(f"Expected exactly one GLB JSON chunk in {description}")
    json_index = json_indices[0]
    document = decode_json_bytes(chunks[json_index][1], description)
    if not add_axis_nodes(document):
        return data, False

    chunks[json_index] = (JSON_CHUNK_TYPE, pad(encode_json(document), 4, b" "))
    body = bytearray()
    for chunk_type, payload in chunks:
        fill = b"\x00" if chunk_type == BIN_CHUNK_TYPE else b" "
        payload = pad(payload, 4, fill)
        body.extend(struct.pack("<II", len(payload), chunk_type))
        body.extend(payload)
    return struct.pack("<4sII", GLB_MAGIC, 2, 12 + len(body)) + body, True


def parse_modern_tile_sections(data: bytes, magic: bytes) -> tuple[int, int, int, int, int]:
    if len(data) < 28 or data[:4] != magic:
        raise ConversionError(f"Invalid {magic.decode()} header")
    version = u32(data, 4)
    declared = u32(data, 8)
    if version != 1 or declared != len(data):
        raise ConversionError(
            f"Invalid {magic.decode()} length/version: version={version}, "
            f"declared={declared}, actual={len(data)}"
        )
    ft_json, ft_bin, bt_json, bt_bin = struct.unpack_from("<4I", data, 12)
    payload_offset = 28 + ft_json + ft_bin + bt_json + bt_bin
    if payload_offset > len(data):
        raise ConversionError(f"Invalid {magic.decode()} table lengths")
    return payload_offset, ft_json, ft_bin, bt_json, bt_bin


def rebuild_b3dm(data: bytes, stats: Statistics, description: str) -> tuple[bytes, bool]:
    payload_offset, _, _, _, _ = parse_modern_tile_sections(data, b"b3dm")
    prefix = data[28:payload_offset]
    payload = data[payload_offset:]

    if payload[:4] == GLB_MAGIC:
        new_payload, changed = transform_glb_bytes(payload, description + " embedded GLB")
    else:
        document = decode_json_bytes(payload, description + " embedded JSON glTF")
        changed = add_axis_nodes(document)
        if changed:
            encoded = encode_json(document)
            # Align the complete B3DM, not merely the payload. In the supplied
            # Metis files the payload starts at byte 172 (4 modulo 8).
            padding = (-(payload_offset + len(encoded))) % 8
            new_payload = encoded + b" " * padding
        else:
            new_payload = payload

    if not changed:
        stats.already_converted += 1
        return data, False
    result = struct.pack("<4sII4I", b"b3dm", 1, 28 + len(prefix) + len(new_payload),
                         *struct.unpack_from("<4I", data, 12)) + prefix + new_payload
    stats.b3dm_changed += 1
    return result, True


def rebuild_i3dm(data: bytes, stats: Statistics, description: str) -> tuple[bytes, bool]:
    if len(data) < 32 or data[:4] != b"i3dm":
        raise ConversionError(f"Invalid i3dm header in {description}")
    version, declared = struct.unpack_from("<II", data, 4)
    if version != 1 or declared != len(data):
        raise ConversionError(f"Invalid i3dm length/version in {description}")
    ft_json, ft_bin, bt_json, bt_bin, gltf_format = struct.unpack_from("<5I", data, 12)
    payload_offset = 32 + ft_json + ft_bin + bt_json + bt_bin
    if payload_offset > len(data):
        raise ConversionError(f"Invalid i3dm table lengths in {description}")
    if gltf_format == 0:
        # The URI target is handled later when scanning standalone .gltf/.glb.
        stats.external_i3dm += 1
        return data, False
    if gltf_format != 1:
        raise ConversionError(f"Unsupported i3dm gltfFormat={gltf_format} in {description}")

    prefix = data[32:payload_offset]
    payload, changed = transform_glb_bytes(data[payload_offset:], description + " embedded GLB")
    if not changed:
        stats.already_converted += 1
        return data, False
    header = struct.pack(
        "<4sII5I", b"i3dm", 1, 32 + len(prefix) + len(payload),
        ft_json, ft_bin, bt_json, bt_bin, gltf_format,
    )
    stats.i3dm_changed += 1
    return header + prefix + payload, True


def rebuild_cmpt(data: bytes, stats: Statistics, description: str) -> tuple[bytes, bool]:
    if len(data) < 16 or data[:4] != b"cmpt":
        raise ConversionError(f"Invalid cmpt header in {description}")
    version, declared, tile_count = struct.unpack_from("<III", data, 4)
    if version != 1 or declared != len(data):
        raise ConversionError(f"Invalid cmpt length/version in {description}")
    offset = 16
    inner_tiles: list[bytes] = []
    changed_any = False
    for index in range(tile_count):
        if offset + 12 > len(data):
            raise ConversionError(f"Truncated inner tile {index} in {description}")
        length = u32(data, offset + 8)
        inner = data[offset:offset + length]
        if len(inner) != length:
            raise ConversionError(f"Truncated inner tile {index} in {description}")
        converted, changed = transform_tile_bytes(inner, stats, f"{description}#{index}")
        converted = pad(converted, 8, b"\x00")
        # Padding is part of the inner tile byteLength when nested in CMPT.
        if len(converted) != u32(converted, 8):
            converted = converted[:8] + struct.pack("<I", len(converted)) + converted[12:]
        inner_tiles.append(converted)
        changed_any = changed_any or changed
        offset += length
    if offset != len(data):
        trailing = data[offset:]
        if trailing.strip(b"\x00"):
            raise ConversionError(f"Unexpected trailing CMPT bytes in {description}")
    if not changed_any:
        return data, False
    body = b"".join(inner_tiles)
    stats.cmpt_changed += 1
    return struct.pack("<4sIII", b"cmpt", 1, 16 + len(body), tile_count) + body, True


def transform_tile_bytes(data: bytes, stats: Statistics, description: str) -> tuple[bytes, bool]:
    magic = data[:4]
    if magic == b"b3dm":
        return rebuild_b3dm(data, stats, description)
    if magic == b"i3dm":
        return rebuild_i3dm(data, stats, description)
    if magic == b"cmpt":
        return rebuild_cmpt(data, stats, description)
    raise ConversionError(f"Unsupported inner tile magic {magic!r} in {description}")


def process_container(path: Path, stats: Statistics) -> None:
    data = path.read_bytes()
    converted, changed = transform_tile_bytes(data, stats, str(path))
    if changed:
        path.write_bytes(converted)


def process_gltf(path: Path, stats: Statistics) -> None:
    document = decode_json_bytes(path.read_bytes(), str(path))
    if add_axis_nodes(document):
        path.write_text(
            json.dumps(document, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8", newline="\n",
        )
        stats.gltf_changed += 1
    else:
        stats.already_converted += 1


def process_glb(path: Path, stats: Statistics) -> None:
    converted, changed = transform_glb_bytes(path.read_bytes(), str(path))
    if changed:
        path.write_bytes(converted)
        stats.glb_changed += 1
    else:
        stats.already_converted += 1


def validate_paths(input_dir: Path, output_dir: Path, tileset_relative: Path) -> None:
    if not input_dir.is_dir():
        raise FileNotFoundError(f"Input directory does not exist: {input_dir}")
    if not (input_dir / tileset_relative).is_file():
        raise FileNotFoundError(f"Tileset JSON does not exist: {input_dir / tileset_relative}")
    if input_dir == output_dir:
        raise ConversionError("input_dir and output_dir must be different")
    if input_dir in output_dir.parents:
        raise ConversionError("output_dir must not be inside input_dir")
    if output_dir == Path(output_dir.anchor):
        raise ConversionError("Refusing to use a filesystem root as output_dir")


def main() -> int:
    args = parse_args()
    input_dir = args.input_dir.resolve()
    output_dir = args.output_dir.resolve()
    tileset_relative = Path(args.tileset_json)
    validate_paths(input_dir, output_dir, tileset_relative)

    if output_dir.exists() and not args.force:
        raise FileExistsError(f"Output already exists: {output_dir}; pass --force to replace it")

    output_parent = output_dir.parent
    output_parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=output_dir.name + "-axis-", dir=output_parent))
    stats = Statistics()
    try:
        shutil.copytree(input_dir, staging, dirs_exist_ok=True)

        # Process containers first. External I3DM models are processed by the
        # standalone glTF/GLB passes below.
        for extension in ("*.b3dm", "*.i3dm", "*.cmpt"):
            for path in sorted(staging.rglob(extension)):
                process_container(path, stats)

        for path in sorted(staging.rglob("*.gltf")):
            process_gltf(path, stats)
        if not args.skip_standalone_glb:
            for path in sorted(staging.rglob("*.glb")):
                process_glb(path, stats)

        if output_dir.exists():
            shutil.rmtree(output_dir)
        staging.replace(output_dir)
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        raise

    print(f"Output: {output_dir}")
    print(f"B3DM changed: {stats.b3dm_changed}")
    print(f"I3DM embedded GLB changed: {stats.i3dm_changed}")
    print(f"CMPT containers rebuilt: {stats.cmpt_changed}")
    print(f"Standalone glTF changed: {stats.gltf_changed}")
    print(f"Standalone GLB changed: {stats.glb_changed}")
    print(f"External-model I3DM preserved: {stats.external_i3dm}")
    print(f"Already converted: {stats.already_converted}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"ERROR: {error}", file=sys.stderr)
        raise SystemExit(1)

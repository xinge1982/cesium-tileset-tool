#!/usr/bin/env python3
"""Extract every glTF texture image referenced by a 3D Tiles directory.

Supported containers: glTF, GLB, B3DM, I3DM and nested CMPT. Images may be
external files, data URIs, or binary bufferView payloads. No third-party Python
packages are required.

Example:

    python extract_tileset_textures.py /data/features \
        --output /data/features_textures --overwrite
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import mimetypes
import re
import shutil
import struct
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import unquote_to_bytes


TILE_SUFFIXES = {".gltf", ".glb", ".b3dm", ".i3dm", ".cmpt"}
MIME_EXTENSIONS = {
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/webp": ".webp",
    "image/ktx2": ".ktx2",
    "image/gif": ".gif",
    "image/bmp": ".bmp",
}


@dataclass
class GltfDocument:
    document: dict[str, Any]
    binary_chunks: list[bytes]
    base_dir: Path
    source_key: str


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Extract texture images referenced by a 3D Tiles directory"
    )
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument(
        "--manifest-name",
        default="textures_manifest.json",
        help="output JSON manifest name (default: textures_manifest.json)",
    )
    return parser.parse_args()


def padded_json(data: bytes) -> dict[str, Any]:
    value = data.rstrip(b"\x00 \t\r\n")
    return json.loads(value.decode("utf-8")) if value else {}


def parse_glb(data: bytes, source_key: str, base_dir: Path) -> GltfDocument:
    if len(data) < 12 or data[:4] != b"glTF":
        raise ValueError("not a GLB")
    version, byte_length = struct.unpack_from("<II", data, 4)
    if version != 2 or byte_length > len(data):
        raise ValueError(f"unsupported GLB version/length: {version}/{byte_length}")
    offset = 12
    document: dict[str, Any] | None = None
    binary_chunks: list[bytes] = []
    while offset + 8 <= byte_length:
        chunk_length, chunk_type = struct.unpack_from("<II", data, offset)
        offset += 8
        chunk = data[offset:offset + chunk_length]
        offset += chunk_length
        if chunk_type == 0x4E4F534A:
            document = padded_json(chunk)
        elif chunk_type == 0x004E4942:
            binary_chunks.append(chunk)
    if document is None:
        raise ValueError("GLB JSON chunk is missing")
    return GltfDocument(document, binary_chunks, base_dir, source_key)


def parse_b3dm(data: bytes, source_key: str, base_dir: Path) -> GltfDocument:
    if len(data) < 28 or data[:4] != b"b3dm":
        raise ValueError("not a B3DM")
    version, byte_length, ftj, ftb, btj, btb = struct.unpack_from(
        "<6I", data, 4
    )
    if version != 1 or byte_length > len(data):
        raise ValueError("invalid B3DM header")
    offset = 28 + ftj + ftb + btj + btb
    payload = data[offset:byte_length]
    if payload[:4] == b"glTF":
        return parse_glb(payload, source_key + "#b3dm", base_dir)
    # Some Metis tiles append a JSON glTF document and keep buffers/textures
    # beside the B3DM rather than embedding a GLB.
    return GltfDocument(
        padded_json(payload), [], base_dir, source_key + "#b3dm-json"
    )


def i3dm_payload(data: bytes) -> tuple[int, bytes]:
    if len(data) < 32 or data[:4] != b"i3dm":
        raise ValueError("not an I3DM")
    version, byte_length, ftj, ftb, btj, btb, gltf_format = struct.unpack_from(
        "<7I", data, 4
    )
    if version != 1 or byte_length > len(data) or gltf_format not in (0, 1):
        raise ValueError("invalid I3DM header")
    offset = 32 + ftj + ftb + btj + btb
    return gltf_format, data[offset:byte_length]


def cmpt_payloads(data: bytes) -> list[tuple[str, bytes]]:
    if len(data) < 16 or data[:4] != b"cmpt":
        raise ValueError("not a CMPT")
    version, byte_length, tiles_length = struct.unpack_from("<3I", data, 4)
    if version != 1 or byte_length > len(data):
        raise ValueError("invalid CMPT header")
    result: list[tuple[str, bytes]] = []
    offset = 16
    for _ in range(tiles_length):
        if offset + 12 > byte_length:
            raise ValueError("truncated CMPT inner tile")
        length = struct.unpack_from("<I", data, offset + 8)[0]
        if length < 12 or offset + length > byte_length:
            raise ValueError("invalid CMPT inner tile length")
        inner = data[offset:offset + length]
        result.append((inner[:4].decode("ascii", "replace"), inner))
        offset += length
    return result


def decode_data_uri(uri: str) -> tuple[str | None, bytes]:
    header, separator, payload = uri.partition(",")
    if not separator or not header.startswith("data:"):
        raise ValueError("invalid data URI")
    metadata = header[5:].split(";")
    mime_type = metadata[0] or None
    if "base64" in metadata[1:]:
        return mime_type, base64.b64decode(payload)
    return mime_type, unquote_to_bytes(payload)


def image_extension(mime_type: str | None, uri: str | None, data: bytes) -> str:
    if mime_type:
        normalized = mime_type.lower().split(";", 1)[0]
        if normalized in MIME_EXTENSIONS:
            return MIME_EXTENSIONS[normalized]
        guessed = mimetypes.guess_extension(normalized)
        if guessed:
            return ".jpg" if guessed == ".jpe" else guessed
    if uri and not uri.startswith("data:"):
        suffix = Path(uri.split("?", 1)[0].split("#", 1)[0]).suffix.lower()
        if suffix:
            return suffix
    signatures = (
        (b"\x89PNG\r\n\x1a\n", ".png"),
        (b"\xff\xd8\xff", ".jpg"),
        (b"RIFF", ".webp"),
        (b"\xabKTX 20\xbb\r\n\x1a\n", ".ktx2"),
        (b"GIF8", ".gif"),
        (b"BM", ".bmp"),
    )
    for signature, extension in signatures:
        if data.startswith(signature):
            return extension
    return ".bin"


def safe_source_name(source_key: str) -> str:
    value = source_key.replace("\\", "/").strip("/")
    value = re.sub(r"[^0-9A-Za-z._/-]+", "_", value)
    value = value.replace("/", "__").replace("#", "_")
    if len(value) > 160:
        digest = hashlib.sha1(source_key.encode("utf-8")).hexdigest()[:12]
        value = value[:140] + "_" + digest
    return value or "tileset"


class TextureExtractor:
    def __init__(self, root: Path, output: Path, overwrite: bool) -> None:
        self.root = root
        self.output = output
        self.overwrite = overwrite
        self.visited_files: set[Path] = set()
        self.visited_virtual: set[str] = set()
        self.hash_outputs: dict[str, str] = {}
        self.records: list[dict[str, Any]] = []
        self.failures: list[dict[str, str]] = []

    def source_key(self, path: Path) -> str:
        try:
            return path.resolve().relative_to(self.root).as_posix()
        except ValueError:
            return path.resolve().as_posix()

    def warn(self, source: str, error: Exception | str) -> None:
        message = str(error)
        self.failures.append({"source": source, "error": message})
        print(f"WARN {source}: {message}", file=sys.stderr)

    def read_buffer(self, gltf: GltfDocument, buffer_index: int) -> bytes:
        buffers = gltf.document.get("buffers", [])
        if buffer_index >= len(buffers):
            raise ValueError(f"buffer index {buffer_index} is out of range")
        definition = buffers[buffer_index]
        uri = definition.get("uri")
        if uri:
            if uri.startswith("data:"):
                return decode_data_uri(uri)[1]
            return (gltf.base_dir / unquote_to_bytes(uri).decode("utf-8")).read_bytes()
        embedded_index = sum(
            1 for item in buffers[:buffer_index] if not item.get("uri")
        )
        if embedded_index >= len(gltf.binary_chunks):
            raise ValueError(f"binary GLB buffer {buffer_index} is missing")
        return gltf.binary_chunks[embedded_index]

    def image_bytes(
        self, gltf: GltfDocument, image: dict[str, Any]
    ) -> tuple[bytes, str | None, str | None]:
        uri = image.get("uri")
        mime_type = image.get("mimeType")
        if uri:
            if uri.startswith("data:"):
                uri_mime, data = decode_data_uri(uri)
                return data, mime_type or uri_mime, uri
            path = gltf.base_dir / unquote_to_bytes(uri).decode("utf-8")
            return path.read_bytes(), mime_type, uri
        view_index = image.get("bufferView")
        views = gltf.document.get("bufferViews", [])
        if not isinstance(view_index, int) or view_index >= len(views):
            raise ValueError("image has neither a valid uri nor bufferView")
        view = views[view_index]
        buffer = self.read_buffer(gltf, int(view.get("buffer", 0)))
        start = int(view.get("byteOffset", 0))
        end = start + int(view["byteLength"])
        if start < 0 or end > len(buffer):
            raise ValueError("image bufferView exceeds its buffer")
        return buffer[start:end], mime_type, None

    def extract_gltf(self, gltf: GltfDocument) -> None:
        if gltf.source_key in self.visited_virtual:
            return
        self.visited_virtual.add(gltf.source_key)
        for index, image in enumerate(gltf.document.get("images", [])):
            try:
                data, mime_type, uri = self.image_bytes(gltf, image)
                extension = image_extension(mime_type, uri, data)
                digest = hashlib.sha256(data).hexdigest()
                previous = self.hash_outputs.get(digest)
                if previous:
                    destination = self.output / previous
                    status = "duplicate"
                else:
                    name = f"{safe_source_name(gltf.source_key)}__image_{index:03d}{extension}"
                    destination = self.output / name
                    status = "written"
                    if destination.exists() and not self.overwrite:
                        status = "existing"
                    else:
                        destination.parent.mkdir(parents=True, exist_ok=True)
                        destination.write_bytes(data)
                    self.hash_outputs[digest] = destination.name
                self.records.append({
                    "output": destination.name,
                    "source": gltf.source_key,
                    "imageIndex": index,
                    "imageName": image.get("name"),
                    "uri": None if uri and uri.startswith("data:") else uri,
                    "mimeType": mime_type,
                    "byteLength": len(data),
                    "sha256": digest,
                    "status": status,
                })
                print(f"{status.upper():8s} {gltf.source_key} image[{index}] -> {destination}")
            except Exception as exc:
                self.warn(f"{gltf.source_key} image[{index}]", exc)

    def process_i3dm(self, data: bytes, path: Path, source_key: str) -> None:
        gltf_format, payload = i3dm_payload(data)
        if gltf_format == 1:
            self.extract_gltf(parse_glb(payload, source_key + "#i3dm", path.parent))
            return
        uri = payload.rstrip(b"\x00 \t\r\n").decode("utf-8")
        if uri.startswith("data:"):
            self.warn(source_key, "I3DM data URI model is not supported")
            return
        self.process_file((path.parent / unquote_to_bytes(uri).decode("utf-8")).resolve())

    def process_binary(self, data: bytes, path: Path, source_key: str) -> None:
        magic = data[:4]
        if magic == b"glTF":
            self.extract_gltf(parse_glb(data, source_key, path.parent))
        elif magic == b"b3dm":
            self.extract_gltf(parse_b3dm(data, source_key, path.parent))
        elif magic == b"i3dm":
            self.process_i3dm(data, path, source_key)
        elif magic == b"cmpt":
            for index, (kind, inner) in enumerate(cmpt_payloads(data)):
                inner_key = f"{source_key}#cmpt-{index}-{kind}"
                try:
                    self.process_binary(inner, path, inner_key)
                except Exception as exc:
                    self.warn(inner_key, exc)

    def process_file(self, path: Path) -> None:
        path = path.resolve()
        if path in self.visited_files:
            return
        self.visited_files.add(path)
        source_key = self.source_key(path)
        try:
            if path.suffix.lower() == ".gltf":
                document = json.loads(path.read_text(encoding="utf-8-sig"))
                self.extract_gltf(GltfDocument(document, [], path.parent, source_key))
            else:
                self.process_binary(path.read_bytes(), path, source_key)
        except Exception as exc:
            self.warn(source_key, exc)

    def run(self) -> None:
        candidates = sorted(
            path for path in self.root.rglob("*")
            if path.is_file() and path.suffix.lower() in TILE_SUFFIXES
        )
        for path in candidates:
            self.process_file(path)


def main() -> int:
    args = parse_args()
    root = args.input_dir.resolve()
    output = args.output.resolve()
    if not root.is_dir():
        print(f"error: input directory does not exist: {root}", file=sys.stderr)
        return 2
    output.mkdir(parents=True, exist_ok=True)
    extractor = TextureExtractor(root, output, args.overwrite)
    extractor.run()
    manifest = {
        "input": str(root),
        "textureCount": len(extractor.records),
        "uniqueTextureCount": len(extractor.hash_outputs),
        "failureCount": len(extractor.failures),
        "textures": extractor.records,
        "failures": extractor.failures,
    }
    manifest_path = output / args.manifest_name
    manifest_path.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    print(
        f"finished: {len(extractor.records)} reference(s), "
        f"{len(extractor.hash_outputs)} unique texture(s), "
        f"{len(extractor.failures)} warning(s); manifest={manifest_path}"
    )
    return 0 if not extractor.failures else 1


if __name__ == "__main__":
    raise SystemExit(main())

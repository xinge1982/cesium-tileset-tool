#!/usr/bin/env python3
"""Compress GLB/glTF files without Draco, Meshopt, Node.js, or Blender.

The script recursively reads .glb and .gltf files, preserves their scene,
geometry, accessors, materials, transparency, emission, extras, and extensions,
then outputs self-contained standard GLB files. Compression is deliberately
limited to safe operations compatible with the project's Go glTF merger:

* resize and recompress PNG/JPEG/WebP textures with Pillow;
* omit unused padding between source bufferViews;
* pack external buffers and images into one GLB binary chunk.

No compressed geometry extension is introduced and vertex/index data is not
quantized or simplified.

Install the only dependency:

    pip install Pillow

Example:

    python compress_gltf_directory.py D:/models/input \
        --output D:/models/compressed \
        --texture-max-size 2048 --jpeg-quality 85 \
        --workers 4 --overwrite
"""

from __future__ import annotations

import argparse
import base64
import concurrent.futures
import io
import json
import mimetypes
import os
import struct
import sys
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Sequence
from urllib.parse import unquote, unquote_to_bytes, urlparse


MODEL_SUFFIXES = {".glb", ".gltf"}
GLB_JSON_CHUNK = 0x4E4F534A
GLB_BIN_CHUNK = 0x004E4942
MANIFEST_VERSION = 2
MIME_TO_FORMAT = {
    "image/png": "PNG",
    "image/jpeg": "JPEG",
    "image/webp": "WEBP",
}
FORMAT_TO_MIME = {
    "PNG": "image/png",
    "JPEG": "image/jpeg",
    "WEBP": "image/webp",
}


@dataclass(frozen=True)
class CompressionOptions:
    texture_max_size: int
    texture_format: str
    jpeg_quality: int
    webp_quality: int
    webp_lossless: bool
    keep_larger_textures: bool


@dataclass(frozen=True)
class CompressionTask:
    source: Path
    destination: Path
    relative_source: str
    relative_destination: str
    source_bytes: int


@dataclass
class CompressionResult:
    source: str
    destination: str
    status: str
    source_bytes: int
    output_bytes: int = 0
    saved_bytes: int = 0
    output_ratio: float | None = None
    reduction_percent: float | None = None
    duration_seconds: float = 0.0
    textures_found: int = 0
    textures_optimized: int = 0
    texture_source_bytes: int = 0
    texture_output_bytes: int = 0
    warnings: list[str] | None = None
    message: str = ""


@dataclass
class LoadedGltf:
    document: dict[str, Any]
    buffers: list[bytes]
    external_resources: set[Path]


class BinaryBuilder:
    def __init__(self) -> None:
        self.data = bytearray()

    def add(self, payload: bytes) -> tuple[int, int]:
        while len(self.data) % 4:
            self.data.append(0)
        offset = len(self.data)
        self.data.extend(payload)
        return offset, len(payload)


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Compress GLB/glTF textures and repack as standard GLB"
    )
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument(
        "--texture-max-size", type=int, default=2048,
        help="maximum texture width/height; 0 preserves dimensions (default: 2048)",
    )
    parser.add_argument(
        "--texture-format", choices=("keep", "webp", "png", "jpeg"),
        default="keep",
        help="texture format; keep preserves PNG/JPEG/WebP types (default: keep)",
    )
    parser.add_argument(
        "--jpeg-quality", type=int, default=85,
        help="JPEG quality from 1 to 100 (default: 85)",
    )
    parser.add_argument(
        "--webp-quality", type=int, default=85,
        help="lossy WebP quality from 1 to 100 (default: 85)",
    )
    parser.add_argument(
        "--webp-lossless", action="store_true",
        help="encode WebP losslessly instead of using --webp-quality",
    )
    parser.add_argument(
        "--allow-larger-textures", action="store_true",
        help="keep recompressed textures even when they are larger than the originals",
    )
    parser.add_argument(
        "--workers", type=int,
        default=max(1, min(4, (os.cpu_count() or 2) // 2)),
        help="parallel worker count (default: up to 4)",
    )
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument(
        "--manifest-name", default="compression_manifest.json",
        help="manifest filename under output directory",
    )
    return parser.parse_args(argv)


def decode_data_uri(uri: str) -> tuple[str | None, bytes]:
    header, separator, payload = uri.partition(",")
    if not separator or not header.startswith("data:"):
        raise ValueError("invalid data URI")
    metadata = header[5:].split(";")
    mime_type = metadata[0] or None
    if "base64" in metadata[1:]:
        return mime_type, base64.b64decode(payload)
    return mime_type, unquote_to_bytes(payload)


def parse_glb(source: Path) -> tuple[dict[str, Any], list[bytes]]:
    data = source.read_bytes()
    if len(data) < 20 or data[:4] != b"glTF":
        raise ValueError("invalid GLB header")
    version, declared_length = struct.unpack_from("<II", data, 4)
    if version != 2 or declared_length != len(data):
        raise ValueError(
            f"unsupported GLB version or length: version={version}, "
            f"declared={declared_length}, actual={len(data)}"
        )
    document: dict[str, Any] | None = None
    binary_chunks: list[bytes] = []
    offset = 12
    while offset + 8 <= declared_length:
        chunk_length, chunk_type = struct.unpack_from("<II", data, offset)
        offset += 8
        end = offset + chunk_length
        if end > declared_length:
            raise ValueError("GLB chunk exceeds declared file length")
        payload = data[offset:end]
        offset = end
        if chunk_type == GLB_JSON_CHUNK:
            document = json.loads(payload.rstrip(b"\x00 \t\r\n").decode("utf-8"))
        elif chunk_type == GLB_BIN_CHUNK:
            binary_chunks.append(payload)
    if document is None:
        raise ValueError("GLB JSON chunk is missing")
    return document, binary_chunks


def load_uri(source_dir: Path, uri: str) -> tuple[str | None, bytes, Path | None]:
    if uri.startswith("data:"):
        mime_type, data = decode_data_uri(uri)
        return mime_type, data, None
    parsed = urlparse(uri)
    if parsed.scheme or parsed.netloc:
        raise ValueError(f"network URI is not supported: {uri}")
    resource = (source_dir / unquote(parsed.path)).resolve()
    if not resource.is_file():
        raise ValueError(f"external resource does not exist: {resource}")
    return mimetypes.guess_type(resource.name)[0], resource.read_bytes(), resource


def load_gltf(source: Path) -> LoadedGltf:
    external_resources: set[Path] = set()
    if source.suffix.lower() == ".glb":
        document, binary_chunks = parse_glb(source)
    else:
        document = json.loads(source.read_text(encoding="utf-8-sig"))
        binary_chunks = []

    buffers: list[bytes] = []
    for index, buffer in enumerate(document.get("buffers", [])):
        uri = buffer.get("uri")
        if isinstance(uri, str):
            _, payload, external = load_uri(source.parent, uri)
            if external is not None:
                external_resources.add(external)
        elif index < len(binary_chunks):
            payload = binary_chunks[index]
        elif index == 0 and binary_chunks:
            payload = binary_chunks[0]
        elif int(buffer.get("byteLength", 0)) == 0:
            payload = b""
        else:
            raise ValueError(f"buffer {index} has no URI or GLB binary chunk")
        declared = int(buffer.get("byteLength", len(payload)))
        if len(payload) < declared:
            raise ValueError(
                f"buffer {index} is shorter than byteLength: {len(payload)} < {declared}"
            )
        buffers.append(payload)
    for image in document.get("images", []):
        uri = image.get("uri") if isinstance(image, dict) else None
        if not isinstance(uri, str) or uri.startswith("data:"):
            continue
        parsed = urlparse(uri)
        if parsed.scheme or parsed.netloc:
            continue
        resource = (source.parent / unquote(parsed.path)).resolve()
        if resource.is_file():
            external_resources.add(resource)
    return LoadedGltf(document, buffers, external_resources)


def image_mime_from_data(data: bytes, configured: str | None) -> str | None:
    if configured:
        return configured.lower().split(";", 1)[0]
    if data.startswith(b"\x89PNG\r\n\x1a\n"):
        return "image/png"
    if data.startswith(b"\xff\xd8\xff"):
        return "image/jpeg"
    if data.startswith(b"RIFF") and data[8:12] == b"WEBP":
        return "image/webp"
    if data.startswith(b"\xabKTX 20\xbb\r\n\x1a\n"):
        return "image/ktx2"
    return None


def extract_buffer_view(
    document: dict[str, Any], buffers: list[bytes], view_index: int,
) -> bytes:
    views = document.get("bufferViews", [])
    if view_index < 0 or view_index >= len(views):
        raise ValueError(f"invalid bufferView index: {view_index}")
    view = views[view_index]
    buffer_index = int(view.get("buffer", 0))
    if buffer_index < 0 or buffer_index >= len(buffers):
        raise ValueError(f"bufferView {view_index} references missing buffer {buffer_index}")
    start = int(view.get("byteOffset", 0))
    end = start + int(view.get("byteLength", 0))
    if start < 0 or end > len(buffers[buffer_index]):
        raise ValueError(f"bufferView {view_index} exceeds buffer {buffer_index}")
    return buffers[buffer_index][start:end]


def load_image(
    source: Path,
    document: dict[str, Any],
    buffers: list[bytes],
    image: dict[str, Any],
) -> tuple[bytes, str | None, Path | None]:
    if isinstance(image.get("uri"), str):
        uri_mime, data, external = load_uri(source.parent, image["uri"])
        mime_type = image_mime_from_data(data, image.get("mimeType") or uri_mime)
        return data, mime_type, external
    if "bufferView" in image:
        data = extract_buffer_view(document, buffers, int(image["bufferView"]))
        return data, image_mime_from_data(data, image.get("mimeType")), None
    raise ValueError("image has neither uri nor bufferView")


def texture_output_format(source_mime: str | None, configured: str) -> str | None:
    if configured != "keep":
        return configured.upper()
    return MIME_TO_FORMAT.get(source_mime or "")


def optimize_image(
    data: bytes,
    source_mime: str | None,
    options: CompressionOptions,
) -> tuple[bytes, str | None, bool, str | None]:
    output_format = texture_output_format(source_mime, options.texture_format)
    if output_format is None:
        return data, source_mime, False, "unsupported texture format was preserved"
    try:
        from PIL import Image
    except ImportError as exc:
        raise ValueError("Pillow is required; install it with 'pip install Pillow'") from exc

    try:
        with Image.open(io.BytesIO(data)) as opened:
            image = opened.copy()
    except Exception as exc:
        return data, source_mime, False, f"texture decode failed and was preserved: {exc}"

    resized = False
    if options.texture_max_size > 0 and max(image.size) > options.texture_max_size:
        image.thumbnail(
            (options.texture_max_size, options.texture_max_size),
            Image.Resampling.LANCZOS,
        )
        resized = True

    warning = None
    has_alpha = "A" in image.getbands() or "transparency" in image.info
    if output_format == "JPEG" and has_alpha:
        if options.texture_format == "jpeg":
            warning = "transparent texture cannot be JPEG; PNG was used instead"
        output_format = "PNG"
    if output_format == "JPEG" and image.mode not in ("RGB", "L"):
        image = image.convert("RGB")

    output = io.BytesIO()
    if output_format == "PNG":
        image.save(output, format="PNG", optimize=True, compress_level=9)
    elif output_format == "JPEG":
        image.save(
            output, format="JPEG", quality=options.jpeg_quality,
            optimize=True, progressive=True,
        )
    elif output_format == "WEBP":
        image.save(
            output, format="WEBP", quality=options.webp_quality,
            lossless=options.webp_lossless, method=6,
        )
    else:
        return data, source_mime, False, "unsupported target format was preserved"

    optimized = output.getvalue()
    changed_format = FORMAT_TO_MIME[output_format] != source_mime
    if (
        not options.keep_larger_textures
        and not resized
        and not changed_format
        and len(optimized) >= len(data)
    ):
        return data, source_mime, False, warning
    return optimized, FORMAT_TO_MIME[output_format], optimized != data, warning


def append_required_extension(document: dict[str, Any], extension: str) -> None:
    used = document.setdefault("extensionsUsed", [])
    if extension not in used:
        used.append(extension)
    required = document.setdefault("extensionsRequired", [])
    if extension not in required:
        required.append(extension)


def remove_extension_used_if_unreferenced(
    document: dict[str, Any], extension: str,
) -> None:
    if extension != "EXT_texture_webp":
        return
    if any(
        extension in (texture.get("extensions") or {})
        for texture in document.get("textures", [])
    ):
        return
    for key in ("extensionsUsed", "extensionsRequired"):
        values = document.get(key)
        if isinstance(values, list) and extension in values:
            values.remove(extension)
        if values == []:
            document.pop(key, None)


def update_texture_image_references(
    document: dict[str, Any], image_formats: dict[int, str | None],
) -> None:
    """Use EXT_texture_webp when an optimized image is encoded as WebP."""
    has_webp = False
    for texture in document.get("textures", []):
        source = texture.get("source")
        extensions = texture.get("extensions")
        if isinstance(extensions, dict):
            webp = extensions.get("EXT_texture_webp")
            if isinstance(webp, dict) and "source" in webp:
                source = webp["source"]
        if not isinstance(source, int):
            continue
        if image_formats.get(source) == "image/webp":
            texture.setdefault("extensions", {})["EXT_texture_webp"] = {
                "source": source,
            }
            texture.pop("source", None)
            has_webp = True
        elif isinstance(texture.get("extensions"), dict):
            texture["extensions"].pop("EXT_texture_webp", None)
            if not texture["extensions"]:
                texture.pop("extensions", None)
            texture["source"] = source
    if has_webp:
        append_required_extension(document, "EXT_texture_webp")
    else:
        remove_extension_used_if_unreferenced(document, "EXT_texture_webp")


def build_glb(
    loaded: LoadedGltf,
    source: Path,
    options: CompressionOptions,
) -> tuple[bytes, int, int, int, int, list[str]]:
    document = loaded.document
    source_views = document.get("bufferViews", [])
    source_images = document.get("images", [])
    image_payloads: dict[int, tuple[bytes, str | None]] = {}
    image_view_replacements: dict[int, tuple[bytes, str | None]] = {}
    external_images: set[Path] = set()
    warnings: list[str] = []
    optimized_count = 0
    texture_source_bytes = 0
    texture_output_bytes = 0

    for image_index, image in enumerate(source_images):
        data, mime_type, external = load_image(
            source, document, loaded.buffers, image,
        )
        if external is not None:
            external_images.add(external)
        optimized, output_mime, changed, warning = optimize_image(
            data, mime_type, options,
        )
        image_payloads[image_index] = (optimized, output_mime)
        texture_source_bytes += len(data)
        texture_output_bytes += len(optimized)
        optimized_count += int(changed)
        if warning:
            warnings.append(f"image {image_index}: {warning}")
        if "bufferView" in image:
            view_index = int(image["bufferView"])
            previous = image_view_replacements.get(view_index)
            if previous is not None and previous != (optimized, output_mime):
                raise ValueError(
                    f"images sharing bufferView {view_index} produced different outputs"
                )
            image_view_replacements[view_index] = (optimized, output_mime)

    builder = BinaryBuilder()
    rebuilt_views: list[dict[str, Any]] = []
    for view_index, view in enumerate(source_views):
        replacement = image_view_replacements.get(view_index)
        payload = (
            replacement[0] if replacement is not None
            else extract_buffer_view(document, loaded.buffers, view_index)
        )
        offset, length = builder.add(payload)
        rebuilt = dict(view)
        rebuilt["buffer"] = 0
        rebuilt["byteOffset"] = offset
        rebuilt["byteLength"] = length
        if replacement is not None:
            rebuilt.pop("byteStride", None)
            rebuilt.pop("target", None)
        rebuilt_views.append(rebuilt)

    image_formats: dict[int, str | None] = {}
    for image_index, image in enumerate(source_images):
        payload, mime_type = image_payloads[image_index]
        if "bufferView" in image:
            view_index = int(image["bufferView"])
        else:
            offset, length = builder.add(payload)
            view_index = len(rebuilt_views)
            rebuilt_views.append({
                "buffer": 0,
                "byteOffset": offset,
                "byteLength": length,
            })
        image.pop("uri", None)
        image["bufferView"] = view_index
        if mime_type:
            image["mimeType"] = mime_type
        image_formats[image_index] = mime_type

    document["bufferViews"] = rebuilt_views
    while len(builder.data) % 4:
        builder.data.append(0)
    document["buffers"] = [{"byteLength": len(builder.data)}]
    update_texture_image_references(document, image_formats)

    json_data = json.dumps(
        document, ensure_ascii=False, separators=(",", ":"),
    ).encode("utf-8")
    json_data += b" " * ((4 - len(json_data) % 4) % 4)
    total_length = 12 + 8 + len(json_data)
    if builder.data:
        total_length += 8 + len(builder.data)
    output = bytearray(struct.pack("<4sII", b"glTF", 2, total_length))
    output.extend(struct.pack("<II", len(json_data), GLB_JSON_CHUNK))
    output.extend(json_data)
    if builder.data:
        output.extend(struct.pack("<II", len(builder.data), GLB_BIN_CHUNK))
        output.extend(builder.data)
    return (
        bytes(output), len(source_images), optimized_count,
        texture_source_bytes, texture_output_bytes, warnings,
    )


def path_is_within(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent)
        return True
    except ValueError:
        return False


def discover_tasks(input_dir: Path, output_dir: Path) -> list[CompressionTask]:
    output_is_inside_input = path_is_within(output_dir, input_dir)
    sources = sorted(
        path for path in input_dir.rglob("*")
        if path.is_file()
        and path.suffix.lower() in MODEL_SUFFIXES
        and not (output_is_inside_input and path_is_within(path, output_dir))
    )
    tasks: list[CompressionTask] = []
    destinations: dict[Path, Path] = {}
    for source in sources:
        relative = source.relative_to(input_dir)
        destination_relative = relative.with_suffix(".glb")
        destination = output_dir / destination_relative
        previous = destinations.get(destination)
        if previous is not None:
            raise ValueError(
                "multiple inputs map to the same GLB output: "
                f"{previous} and {source} -> {destination}"
            )
        loaded = load_gltf(source)
        source_bytes = source.stat().st_size + sum(
            item.stat().st_size
            for item in loaded.external_resources
            if item != source
        )
        destinations[destination] = source
        tasks.append(CompressionTask(
            source=source,
            destination=destination,
            relative_source=relative.as_posix(),
            relative_destination=destination_relative.as_posix(),
            source_bytes=source_bytes,
        ))
    return tasks


def result_with_sizes(
    task: CompressionTask,
    status: str,
    output_bytes: int,
    duration: float,
    **values: Any,
) -> CompressionResult:
    ratio = output_bytes / task.source_bytes if task.source_bytes else None
    reduction = (1.0 - ratio) * 100.0 if ratio is not None else None
    return CompressionResult(
        source=task.relative_source,
        destination=task.relative_destination,
        status=status,
        source_bytes=task.source_bytes,
        output_bytes=output_bytes,
        saved_bytes=task.source_bytes - output_bytes if output_bytes else 0,
        output_ratio=round(ratio, 6) if ratio is not None else None,
        reduction_percent=round(reduction, 3) if reduction is not None else None,
        duration_seconds=round(duration, 3),
        **values,
    )


def compress_one(
    task: CompressionTask,
    options: CompressionOptions,
    overwrite: bool,
) -> CompressionResult:
    started = time.perf_counter()
    if task.destination.exists() and not overwrite:
        return result_with_sizes(
            task, "skipped", task.destination.stat().st_size,
            time.perf_counter() - started,
            warnings=[], message="output exists; pass --overwrite to replace it",
        )
    temporary = task.destination.with_name(
        f"{task.destination.stem}.compressing{task.destination.suffix}"
    )
    try:
        loaded = load_gltf(task.source)
        output, found, optimized, texture_input, texture_output, warnings = build_glb(
            loaded, task.source, options,
        )
        task.destination.parent.mkdir(parents=True, exist_ok=True)
        temporary.write_bytes(output)
        os.replace(temporary, task.destination)
        return result_with_sizes(
            task, "compressed", len(output), time.perf_counter() - started,
            textures_found=found, textures_optimized=optimized,
            texture_source_bytes=texture_input,
            texture_output_bytes=texture_output,
            warnings=warnings,
        )
    except Exception as exc:
        temporary.unlink(missing_ok=True)
        return result_with_sizes(
            task, "failed", 0, time.perf_counter() - started,
            warnings=[], message=str(exc),
        )


def write_manifest(
    output_dir: Path,
    manifest_name: str,
    input_dir: Path,
    options: CompressionOptions,
    args: argparse.Namespace,
    started: datetime,
    results: list[CompressionResult],
) -> Path:
    completed = datetime.now(timezone.utc)
    compressed = [item for item in results if item.status == "compressed"]
    skipped = [item for item in results if item.status == "skipped"]
    failed = [item for item in results if item.status == "failed"]
    source_bytes = sum(item.source_bytes for item in compressed)
    output_bytes = sum(item.output_bytes for item in compressed)
    reduction = (
        (1.0 - output_bytes / source_bytes) * 100.0 if source_bytes else 0.0
    )
    document = {
        "version": MANIFEST_VERSION,
        "status": "failed" if failed else "success",
        "compression": "textures-and-repack-only",
        "geometryCompression": "none",
        "inputDirectory": str(input_dir),
        "outputDirectory": str(output_dir),
        "startedAt": started.isoformat(),
        "completedAt": completed.isoformat(),
        "durationSeconds": round((completed - started).total_seconds(), 3),
        "options": {
            "textureMaxSize": options.texture_max_size,
            "textureFormat": options.texture_format,
            "jpegQuality": options.jpeg_quality,
            "webpQuality": options.webp_quality,
            "webpLossless": options.webp_lossless,
            "keepLargerTextures": options.keep_larger_textures,
            "workers": args.workers,
            "overwrite": args.overwrite,
        },
        "summary": {
            "filesFound": len(results),
            "filesCompressed": len(compressed),
            "filesSkipped": len(skipped),
            "filesFailed": len(failed),
            "compressedSourceBytes": source_bytes,
            "compressedOutputBytes": output_bytes,
            "savedBytes": source_bytes - output_bytes,
            "reductionPercent": round(reduction, 3),
            "texturesFound": sum(item.textures_found for item in compressed),
            "texturesOptimized": sum(item.textures_optimized for item in compressed),
        },
        "files": [asdict(item) for item in results],
    }
    manifest = output_dir / manifest_name
    manifest.write_text(
        json.dumps(document, ensure_ascii=False, indent=2), encoding="utf-8",
    )
    return manifest


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    started = datetime.now(timezone.utc)
    input_dir = args.input_dir.expanduser().resolve()
    output_dir = args.output.expanduser().resolve()
    if not input_dir.is_dir():
        print(f"error: input directory does not exist: {input_dir}", file=sys.stderr)
        return 2
    if input_dir == output_dir:
        print("error: input and output directories must be different", file=sys.stderr)
        return 2
    if args.workers < 1 or args.texture_max_size < 0:
        print("error: workers must be positive and texture-max-size non-negative", file=sys.stderr)
        return 2
    if not 1 <= args.jpeg_quality <= 100 or not 1 <= args.webp_quality <= 100:
        print("error: JPEG/WebP quality must be between 1 and 100", file=sys.stderr)
        return 2
    manifest_name = Path(args.manifest_name)
    if (
        not args.manifest_name.strip()
        or manifest_name.is_absolute()
        or manifest_name.parent != Path(".")
    ):
        print("error: --manifest-name must be a filename without directories", file=sys.stderr)
        return 2
    options = CompressionOptions(
        texture_max_size=args.texture_max_size,
        texture_format=args.texture_format,
        jpeg_quality=args.jpeg_quality,
        webp_quality=args.webp_quality,
        webp_lossless=args.webp_lossless,
        keep_larger_textures=args.allow_larger_textures,
    )
    try:
        from PIL import Image  # noqa: F401
        tasks = discover_tasks(input_dir, output_dir)
    except (ImportError, OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    output_dir.mkdir(parents=True, exist_ok=True)
    print(
        f"found {len(tasks)} GLB/glTF files; workers={args.workers}; "
        "geometry compression=none"
    )
    results: list[CompressionResult] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        pending = {
            pool.submit(compress_one, task, options, args.overwrite): task
            for task in tasks
        }
        for future in concurrent.futures.as_completed(pending):
            result = future.result()
            results.append(result)
            prefix = {
                "compressed": "OK", "skipped": "SKIP", "failed": "FAIL",
            }[result.status]
            size_text = (
                f" {result.source_bytes} -> {result.output_bytes} bytes "
                f"({result.reduction_percent:+.1f}% reduction)"
                if result.output_bytes else ""
            )
            print(f"{prefix:4} {result.source}{size_text}")
            if result.status == "failed":
                print(f"     {result.message}", file=sys.stderr)

    results.sort(key=lambda item: item.source.lower())
    manifest = write_manifest(
        output_dir, args.manifest_name, input_dir, options, args, started, results,
    )
    failed_count = sum(item.status == "failed" for item in results)
    compressed_count = sum(item.status == "compressed" for item in results)
    source_total = sum(
        item.source_bytes for item in results if item.status == "compressed"
    )
    output_total = sum(
        item.output_bytes for item in results if item.status == "compressed"
    )
    reduction = (1.0 - output_total / source_total) * 100.0 if source_total else 0.0
    print(
        f"finished: {compressed_count} compressed, {failed_count} failed, "
        f"{source_total} -> {output_total} bytes ({reduction:.1f}% reduction); "
        f"manifest={manifest}"
    )
    return 1 if failed_count else 0


if __name__ == "__main__":
    raise SystemExit(main())

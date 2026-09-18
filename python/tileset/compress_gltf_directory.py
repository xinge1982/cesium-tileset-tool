#!/usr/bin/env python3
"""Recursively optimize GLB/glTF models into a separate directory.

This script is a parallel batch wrapper around the glTF Transform CLI. It
preserves the source directory hierarchy, outputs self-contained GLBs by
default, and writes a JSON manifest containing sizes, compression ratios,
durations, commands, and failures.

Install the optimizer first:

    npm install --global @gltf-transform/cli

Example:

    python compress_gltf_directory.py D:/models/input \
        --output D:/models/compressed --workers 4 --overwrite

The defaults use EXT_meshopt_compression for geometry and WebP for textures.
Both are supported by current CesiumJS releases. Use ``--geometry-compression
none`` or ``--texture-compression none`` when compressed extensions are not
desired. Additional optimizer arguments can be appended with ``--extra-arg``.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import shlex
import shutil
import subprocess
import sys
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Sequence
from urllib.parse import unquote, urlparse


MODEL_SUFFIXES = {".glb", ".gltf"}
MANIFEST_VERSION = 1
MAX_TOOL_MESSAGE_LENGTH = 8000


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
    command: list[str] | None = None
    message: str = ""


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Recursively compress GLB/glTF files with glTF Transform"
    )
    parser.add_argument("input_dir", type=Path)
    parser.add_argument("--output", "-o", type=Path, required=True)
    parser.add_argument(
        "--executable",
        default="gltf-transform",
        help="glTF Transform executable or absolute path (default: gltf-transform)",
    )
    parser.add_argument(
        "--geometry-compression",
        choices=("meshopt", "draco", "none"),
        default="meshopt",
        help="geometry compression extension (default: meshopt)",
    )
    parser.add_argument(
        "--texture-compression",
        choices=("webp", "jpeg", "png", "none"),
        default="webp",
        help="texture output format (default: webp)",
    )
    parser.add_argument(
        "--workers",
        type=int,
        default=max(1, min(4, (os.cpu_count() or 2) // 2)),
        help="parallel optimizer process count (default: up to 4)",
    )
    parser.add_argument("--overwrite", action="store_true")
    parser.add_argument("--fail-fast", action="store_true")
    parser.add_argument(
        "--extra-arg",
        action="append",
        default=[],
        help="additional optimize argument; repeat for multiple arguments",
    )
    parser.add_argument(
        "--manifest-name",
        default="compression_manifest.json",
        help="manifest filename under output directory",
    )
    return parser.parse_args(argv)


def resolve_executable(value: str) -> str:
    configured = Path(value).expanduser()
    if configured.parent != Path(".") or configured.is_absolute():
        if not configured.is_file():
            raise ValueError(f"optimizer executable does not exist: {configured}")
        return str(configured.resolve())
    resolved = shutil.which(value)
    if resolved is None:
        raise ValueError(
            f"optimizer executable {value!r} was not found in PATH; install it with "
            "'npm install --global @gltf-transform/cli' or pass --executable"
        )
    return resolved


def path_is_within(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent)
        return True
    except ValueError:
        return False


def discover_tasks(
    input_dir: Path,
    output_dir: Path,
) -> list[CompressionTask]:
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
                "multiple inputs map to the same output; rename one file or use "
                f"separate input directories: {previous} and {source} -> {destination}"
            )
        destinations[destination] = source
        tasks.append(CompressionTask(
            source=source,
            destination=destination,
            relative_source=relative.as_posix(),
            relative_destination=destination_relative.as_posix(),
            source_bytes=model_source_bytes(source),
        ))
    return tasks


def model_source_bytes(source: Path) -> int:
    """Count a GLB, or a glTF JSON file plus its local external resources."""
    total = source.stat().st_size
    if source.suffix.lower() != ".gltf":
        return total
    try:
        document = json.loads(source.read_text(encoding="utf-8-sig"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        return total
    resources: set[Path] = set()
    for collection in (document.get("buffers", []), document.get("images", [])):
        for item in collection:
            uri = item.get("uri") if isinstance(item, dict) else None
            if not isinstance(uri, str) or uri.startswith("data:"):
                continue
            parsed = urlparse(uri)
            if parsed.scheme or parsed.netloc:
                continue
            resource = (source.parent / unquote(parsed.path)).resolve()
            if resource.is_file():
                resources.add(resource)
    return total + sum(resource.stat().st_size for resource in resources)


def optimizer_command(
    executable: str,
    task: CompressionTask,
    destination: Path,
    geometry_compression: str,
    texture_compression: str,
    extra_args: Sequence[str],
) -> list[str]:
    command = [
        executable,
        "optimize",
        str(task.source),
        str(destination),
    ]
    if geometry_compression != "none":
        command.extend(("--compress", geometry_compression))
    if texture_compression != "none":
        command.extend(("--texture-compress", texture_compression))
    command.extend(extra_args)
    return command


def compress_one(
    task: CompressionTask,
    executable: str,
    geometry_compression: str,
    texture_compression: str,
    extra_args: Sequence[str],
    overwrite: bool,
) -> CompressionResult:
    started = time.perf_counter()
    source_bytes = task.source_bytes
    if task.destination.exists() and not overwrite:
        output_bytes = task.destination.stat().st_size
        return build_result(
            task, "skipped", source_bytes, output_bytes,
            time.perf_counter() - started, None,
            "output exists; pass --overwrite to replace it",
        )

    task.destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = task.destination.with_name(
        f"{task.destination.stem}.compressing{task.destination.suffix}"
    )
    temporary.unlink(missing_ok=True)
    command = optimizer_command(
        executable, task, temporary,
        geometry_compression, texture_compression, extra_args,
    )
    try:
        process = subprocess.run(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
        )
    except OSError as exc:
        return build_result(
            task, "failed", source_bytes, 0,
            time.perf_counter() - started, command, str(exc),
        )

    output_text = process.stdout.strip()
    if len(output_text) > MAX_TOOL_MESSAGE_LENGTH:
        output_text = output_text[-MAX_TOOL_MESSAGE_LENGTH:]
    if process.returncode != 0:
        temporary.unlink(missing_ok=True)
        return build_result(
            task, "failed", source_bytes, 0,
            time.perf_counter() - started, command,
            output_text or f"optimizer exited with code {process.returncode}",
        )
    if not temporary.is_file():
        return build_result(
            task, "failed", source_bytes, 0,
            time.perf_counter() - started, command,
            "optimizer reported success but did not create the output file",
        )
    try:
        os.replace(temporary, task.destination)
    except OSError as exc:
        temporary.unlink(missing_ok=True)
        return build_result(
            task, "failed", source_bytes, 0,
            time.perf_counter() - started, command,
            f"replace output file failed: {exc}",
        )
    return build_result(
        task, "compressed", source_bytes, task.destination.stat().st_size,
        time.perf_counter() - started, command, output_text,
    )


def build_result(
    task: CompressionTask,
    status: str,
    source_bytes: int,
    output_bytes: int,
    duration: float,
    command: list[str] | None,
    message: str,
) -> CompressionResult:
    ratio = output_bytes / source_bytes if source_bytes else None
    reduction = (1.0 - ratio) * 100.0 if ratio is not None else None
    return CompressionResult(
        source=task.relative_source,
        destination=task.relative_destination,
        status=status,
        source_bytes=source_bytes,
        output_bytes=output_bytes,
        saved_bytes=source_bytes - output_bytes if output_bytes else 0,
        output_ratio=round(ratio, 6) if ratio is not None else None,
        reduction_percent=round(reduction, 3) if reduction is not None else None,
        duration_seconds=round(duration, 3),
        command=command,
        message=message,
    )


def write_manifest(
    output_dir: Path,
    manifest_name: str,
    input_dir: Path,
    executable: str,
    tool_version: str,
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
        "inputDirectory": str(input_dir),
        "outputDirectory": str(output_dir),
        "startedAt": started.isoformat(),
        "completedAt": completed.isoformat(),
        "durationSeconds": round((completed - started).total_seconds(), 3),
        "optimizer": {"executable": executable, "version": tool_version},
        "options": {
            "outputFormat": "glb",
            "geometryCompression": args.geometry_compression,
            "textureCompression": args.texture_compression,
            "workers": args.workers,
            "overwrite": args.overwrite,
            "extraArgs": args.extra_arg,
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
        },
        "files": [asdict(item) for item in results],
    }
    manifest = output_dir / manifest_name
    manifest.parent.mkdir(parents=True, exist_ok=True)
    manifest.write_text(
        json.dumps(document, ensure_ascii=False, indent=2), encoding="utf-8",
    )
    return manifest


def read_tool_version(executable: str) -> str:
    try:
        result = subprocess.run(
            [executable, "--version"],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
            timeout=20,
        )
        return result.stdout.strip() or "unknown"
    except (OSError, subprocess.TimeoutExpired):
        return "unknown"


def printable_command(command: Sequence[str] | None) -> str:
    if not command:
        return ""
    return subprocess.list2cmdline(command) if os.name == "nt" else shlex.join(command)


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
    if args.workers < 1:
        print("error: --workers must be at least 1", file=sys.stderr)
        return 2
    manifest_name = Path(args.manifest_name)
    if (
        not args.manifest_name.strip()
        or manifest_name.is_absolute()
        or manifest_name.parent != Path(".")
    ):
        print("error: --manifest-name must be a filename without directories", file=sys.stderr)
        return 2
    try:
        executable = resolve_executable(args.executable)
        tasks = discover_tasks(input_dir, output_dir)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    output_dir.mkdir(parents=True, exist_ok=True)
    version = read_tool_version(executable)
    print(f"optimizer: {version} ({executable})")
    print(f"found {len(tasks)} GLB/glTF files; workers={args.workers}")
    results: list[CompressionResult] = []

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        pending = {
            pool.submit(
                compress_one, task, executable,
                args.geometry_compression, args.texture_compression,
                tuple(args.extra_arg), args.overwrite,
            ): task
            for task in tasks
        }
        for future in concurrent.futures.as_completed(pending):
            task = pending[future]
            try:
                result = future.result()
            except Exception as exc:  # Keep the batch manifest complete.
                status = "skipped" if isinstance(exc, concurrent.futures.CancelledError) else "failed"
                result = build_result(
                    task, status, task.source_bytes, 0, 0.0, None,
                    "cancelled after an earlier failure" if status == "skipped"
                    else f"unexpected worker error: {exc}",
                )
            results.append(result)
            prefix = {"compressed": "OK", "skipped": "SKIP", "failed": "FAIL"}[result.status]
            size_text = (
                f" {result.source_bytes} -> {result.output_bytes} bytes "
                f"({result.reduction_percent:+.1f}% reduction)"
                if result.output_bytes else ""
            )
            print(f"{prefix:4} {result.source}{size_text}")
            if result.status == "failed":
                if result.command:
                    print(f"     command: {printable_command(result.command)}", file=sys.stderr)
                print(f"     {result.message}", file=sys.stderr)
                if args.fail_fast:
                    for queued in pending:
                        queued.cancel()

    results.sort(key=lambda item: item.source.lower())
    manifest = write_manifest(
        output_dir, args.manifest_name, input_dir, executable, version,
        args, started, results,
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

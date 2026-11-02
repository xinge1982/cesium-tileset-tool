#!/usr/bin/env python3
"""Recursively generate a PNG thumbnail beside every GLB in an input folder."""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

MISSING_DEPENDENCY: ModuleNotFoundError | None = None
try:
    import numpy as np
    import pyrender
    import trimesh
    from PIL import Image
except ModuleNotFoundError as exc:
    MISSING_DEPENDENCY = exc


def look_at(
    camera_position: np.ndarray,
    target: np.ndarray,
    world_up: np.ndarray,
) -> np.ndarray:
    """Return an OpenGL camera pose aimed at target."""
    camera_position = np.asarray(camera_position, dtype=np.float64)
    target = np.asarray(target, dtype=np.float64)

    forward = target - camera_position
    forward_length = np.linalg.norm(forward)
    if forward_length < 1e-12:
        raise ValueError("camera position and target cannot be identical")
    forward /= forward_length

    world_up = np.asarray(world_up, dtype=np.float64)
    world_up /= np.linalg.norm(world_up)
    if abs(np.dot(forward, world_up)) > 0.999:
        # Select a fallback axis that is not parallel to the view direction.
        candidates = np.eye(3)
        world_up = min(candidates, key=lambda axis: abs(np.dot(forward, axis)))

    right = np.cross(forward, world_up)
    right /= np.linalg.norm(right)
    up = np.cross(right, forward)

    pose = np.eye(4)
    pose[:3, 0] = right
    pose[:3, 1] = up
    pose[:3, 2] = -forward
    pose[:3, 3] = camera_position
    return pose


def load_scene(glb_file: Path) -> trimesh.Scene:
    loaded = trimesh.load(glb_file, force="scene", process=False)
    if isinstance(loaded, trimesh.Trimesh):
        return trimesh.Scene(loaded)
    if not isinstance(loaded, trimesh.Scene):
        raise ValueError(f"unsupported trimesh result: {type(loaded).__name__}")
    if not loaded.geometry:
        raise ValueError("GLB contains no renderable geometry")
    return loaded


def generate_thumbnail(
    glb_file: Path,
    output_file: Path,
    size: int = 512,
    background: tuple[int, int, int, int] = (240, 240, 240, 255),
    up_axis: str = "y",
) -> None:
    trimesh_scene = load_scene(glb_file)
    bounds = np.asarray(trimesh_scene.bounds, dtype=np.float64)
    if bounds.shape != (2, 3) or not np.all(np.isfinite(bounds)):
        raise ValueError("model has invalid bounds")

    center = (bounds[0] + bounds[1]) * 0.5
    extent = bounds[1] - bounds[0]
    radius = max(float(np.linalg.norm(extent) * 0.5), 1e-4)

    # Preserve the GLB scene graph, node transforms, materials and textures.
    scene = pyrender.Scene.from_trimesh_scene(
        trimesh_scene,
        bg_color=np.asarray(background, dtype=np.float32) / 255.0,
        ambient_light=np.array([0.35, 0.35, 0.35], dtype=np.float32),
    )

    yfov = np.deg2rad(45.0)
    # A bounding sphere fitted to the vertical FOV, with a small margin.
    camera_distance = radius / np.sin(yfov * 0.5) * 1.15
    axis_vectors = {
        "x": np.array([1.0, 0.0, 0.0]),
        "y": np.array([0.0, 1.0, 0.0]),
        "z": np.array([0.0, 0.0, 1.0]),
    }
    # glTF defines +Y as up. Put the camera above the model along that axis,
    # while viewing the two horizontal axes diagonally.
    world_up = axis_vectors[up_axis]
    view_direction = np.ones(3, dtype=np.float64)
    view_direction["xyz".index(up_axis)] = 0.65
    view_direction /= np.linalg.norm(view_direction)
    camera_position = center + view_direction * camera_distance
    camera_pose = look_at(camera_position, center, world_up)

    camera = pyrender.PerspectiveCamera(
        yfov=yfov,
        znear=max(camera_distance - radius * 1.5, 0.001),
        zfar=camera_distance + radius * 2.5,
    )
    scene.add(camera, pose=camera_pose)

    key_light = pyrender.DirectionalLight(
        color=np.ones(3), intensity=3.0
    )
    scene.add(key_light, pose=camera_pose)

    fill_pose = look_at(
        center - view_direction * camera_distance,
        center,
        world_up,
    )
    fill_light = pyrender.DirectionalLight(
        color=np.array([0.85, 0.90, 1.0]), intensity=1.2
    )
    scene.add(fill_light, pose=fill_pose)

    renderer = None
    try:
        renderer = pyrender.OffscreenRenderer(
            viewport_width=size,
            viewport_height=size,
        )
        color, _ = renderer.render(scene)
    finally:
        if renderer is not None:
            renderer.delete()

    output_file.parent.mkdir(parents=True, exist_ok=True)
    Image.fromarray(color, mode="RGB").save(output_file, format="PNG")


def parse_background(value: str) -> tuple[int, int, int, int]:
    try:
        parts = [int(item.strip()) for item in value.split(",")]
    except ValueError as exc:
        raise argparse.ArgumentTypeError("background must contain integers") from exc
    if len(parts) not in (3, 4) or any(not 0 <= item <= 255 for item in parts):
        raise argparse.ArgumentTypeError(
            "background must be R,G,B or R,G,B,A with values from 0 to 255"
        )
    if len(parts) == 3:
        parts.append(255)
    return tuple(parts)  # type: ignore[return-value]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate a PNG thumbnail beside every GLB in INPUT_FOLDER."
    )
    parser.add_argument("input_folder", type=Path, help="folder containing GLB files")
    parser.add_argument("--size", type=int, default=512, help="image size (default: 512)")
    parser.add_argument(
        "--suffix",
        default="",
        help="suffix before .png; e.g. --suffix _thumbnail (default: none)",
    )
    parser.add_argument(
        "--background",
        type=parse_background,
        default=(240, 240, 240, 255),
        metavar="R,G,B[,A]",
        help="background color (default: 240,240,240,255)",
    )
    parser.add_argument(
        "--skip-existing",
        action="store_true",
        help="do not overwrite thumbnails that already exist",
    )
    parser.add_argument(
        "--up-axis",
        choices=("x", "y", "z"),
        default="y",
        help="model vertical axis; glTF standard is y (default: y)",
    )
    return parser.parse_args()


def main() -> int:
    if MISSING_DEPENDENCY is not None:
        print(
            f"Error: missing Python package {MISSING_DEPENDENCY.name!r}.\n"
            "Install dependencies with:\n"
            "  python -m pip install numpy trimesh pyrender Pillow PyOpenGL",
            file=sys.stderr,
        )
        return 2

    args = parse_args()
    input_folder = args.input_folder.resolve()
    if not input_folder.is_dir():
        print(f"Error: input folder does not exist: {input_folder}", file=sys.stderr)
        return 2
    if args.size < 16:
        print("Error: --size must be at least 16", file=sys.stderr)
        return 2
    if any(char in args.suffix for char in ("/", "\\")):
        print("Error: --suffix cannot contain path separators", file=sys.stderr)
        return 2

    glb_files = sorted(
        path
        for path in input_folder.rglob("*")
        if path.is_file() and path.suffix.lower() == ".glb"
    )
    if not glb_files:
        print(f"No GLB files found in: {input_folder}")
        return 0

    succeeded = skipped = failed = 0
    for glb_file in glb_files:
        output_file = glb_file.with_name(f"{glb_file.stem}{args.suffix}.png")
        relative_glb = glb_file.relative_to(input_folder)
        relative_output = output_file.relative_to(input_folder)

        if args.skip_existing and output_file.exists():
            skipped += 1
            print(f"SKIP {relative_output}: already exists")
            continue

        try:
            print(f"RENDER {relative_glb}")
            generate_thumbnail(
                glb_file=glb_file,
                output_file=output_file,
                size=args.size,
                background=args.background,
                up_axis=args.up_axis,
            )
            succeeded += 1
            print(f"OK     {relative_output}")
        except Exception as exc:
            failed += 1
            print(f"FAIL   {relative_glb}: {exc}", file=sys.stderr)

    print(
        f"Done: files={len(glb_files)}, succeeded={succeeded}, "
        f"skipped={skipped}, failed={failed}"
    )
    return 1 if failed else 0


if __name__ == "__main__":
    # On Linux servers without a display, run with PYOPENGL_PLATFORM=egl.
    raise SystemExit(main())

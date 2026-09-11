#!/usr/bin/env python3
"""Batch-build coloured LOD1 silhouette boxes from GLB files.

This is a Python port of the supplied Go SliceBox implementation.  It applies
scene-node transforms, detects single-pole signs and long rods, and otherwise
builds merged outline boxes from slices along the longest horizontal axis.

Dependencies:
    pip install numpy pillow trimesh

Example:
    python glb_lod1_silhouette_boxes.py input --output output --slice-count 32
"""

from __future__ import annotations

import argparse
import math
import sys
from dataclasses import dataclass
from pathlib import Path

import numpy as np
import trimesh
from PIL import Image


@dataclass
class SliceBoxOptions:
    slice_count: int = 32
    min_slice_vertices: int = 3
    min_thickness_y: float = 0.05
    merge_similar: bool = True
    width_tolerance: float = 0.08
    height_tolerance: float = 0.08


@dataclass
class SliceStat:
    has: bool = False
    count: int = 0
    minimum: np.ndarray | None = None
    maximum: np.ndarray | None = None


@dataclass
class ProxyBox:
    minimum: np.ndarray
    maximum: np.ndarray


@dataclass
class SourceRegion:
    center: np.ndarray
    radius: float
    area: float
    rgba: np.ndarray


def image_average_rgba(image: object) -> np.ndarray:
    if image is None:
        return np.ones(4, dtype=np.float64)
    try:
        source = Image.open(image) if isinstance(image, (str, Path)) else image
        rgba = np.asarray(source.convert("RGBA"), dtype=np.float64) / 255.0
    except (AttributeError, OSError, TypeError, ValueError):
        return np.ones(4, dtype=np.float64)
    visible = rgba[..., 3] > 1.0 / 255.0
    return rgba[visible].mean(axis=0) if np.any(visible) else np.ones(4, dtype=np.float64)


def material_average_rgba(mesh: trimesh.Trimesh) -> np.ndarray:
    material = getattr(mesh.visual, "material", None)
    if material is None:
        colors = getattr(mesh.visual, "face_colors", None)
        if colors is not None and len(colors):
            return np.asarray(colors, dtype=np.float64).mean(axis=0) / 255.0
        return np.array([0.65, 0.65, 0.65, 1.0], dtype=np.float64)

    factor = getattr(material, "baseColorFactor", None)
    if factor is None:
        factor = getattr(material, "diffuse", None)
    if factor is None:
        factor_rgba = np.ones(4, dtype=np.float64)
    else:
        factor_rgba = np.asarray(factor, dtype=np.float64).reshape(-1)
        if factor_rgba.size == 3:
            factor_rgba = np.append(factor_rgba, 255.0 if factor_rgba.max() > 1.0 else 1.0)
        if factor_rgba.max() > 1.0:
            factor_rgba /= 255.0
        factor_rgba = factor_rgba[:4]

    texture = getattr(material, "baseColorTexture", None)
    if texture is None:
        texture = getattr(material, "image", None)
    rgba = np.clip(factor_rgba * image_average_rgba(texture), 0.0, 1.0)
    rgba[3] = max(rgba[3], 0.05)
    return rgba


def transformed_source_meshes(scene: trimesh.Scene) -> list[trimesh.Trimesh]:
    meshes: list[trimesh.Trimesh] = []
    for node_name in scene.graph.nodes_geometry:
        transform, geometry_name = scene.graph[node_name]
        geometry = scene.geometry.get(geometry_name)
        if not isinstance(geometry, trimesh.Trimesh) or len(geometry.vertices) == 0:
            continue
        mesh = geometry.copy()
        mesh.apply_transform(transform)
        meshes.append(mesh)
    return meshes


def source_regions(meshes: list[trimesh.Trimesh]) -> list[SourceRegion]:
    result: list[SourceRegion] = []
    for mesh in meshes:
        bounds = np.asarray(mesh.bounds, dtype=np.float64)
        extent = bounds[1] - bounds[0]
        area = float(mesh.area)
        if not math.isfinite(area) or area <= 0.0:
            area = max(float(np.prod(np.maximum(extent, 1e-4)) ** (2.0 / 3.0)), 1e-4)
        result.append(SourceRegion(
            center=bounds.mean(axis=0),
            radius=max(float(np.linalg.norm(extent)) * 0.5, 1e-4),
            area=area,
            rgba=material_average_rgba(mesh),
        ))
    return result


def representative_color(center: np.ndarray, regions: list[SourceRegion]) -> np.ndarray:
    if not regions:
        return np.array([166, 166, 166, 255], dtype=np.uint8)
    weights = []
    for region in regions:
        distance = max(float(np.linalg.norm(center - region.center)) - region.radius, 0.0)
        weights.append(region.area / ((distance + region.radius * 0.15 + 1e-4) ** 2))
    rgba = np.average(np.vstack([r.rgba for r in regions]), axis=0,
                      weights=np.asarray(weights, dtype=np.float64))
    return np.rint(np.clip(rgba, 0.0, 1.0) * 255.0).astype(np.uint8)


def detect_sign_with_single_pole(vertices: np.ndarray, opt: SliceBoxOptions) -> list[ProxyBox] | None:
    bounds = np.vstack((vertices.min(axis=0), vertices.max(axis=0)))
    size_x, size_y, size_z = bounds[1] - bounds[0]
    if size_y <= 0.0:
        return None

    split_y = bounds[0, 1] + size_y * 0.45
    lower = vertices[vertices[:, 1] < split_y]
    upper = vertices[vertices[:, 1] >= split_y]
    if len(lower) < opt.min_slice_vertices or len(upper) < opt.min_slice_vertices:
        return None

    lower_bounds = np.vstack((lower.min(axis=0), lower.max(axis=0)))
    upper_bounds = np.vstack((upper.min(axis=0), upper.max(axis=0)))
    lower_x, _, lower_z = lower_bounds[1] - lower_bounds[0]
    upper_x, upper_y, upper_z = upper_bounds[1] - upper_bounds[0]

    upper_wide_x = upper_x > lower_x * 3.0
    upper_wide_z = upper_z > lower_z * 3.0
    upper_is_board = max(upper_x, upper_z) > max(size_x, size_z) * 0.45
    lower_is_pole = max(lower_x, lower_z) < max(size_x, size_z) * 0.25
    board_height_ok = upper_y < size_y * 0.55
    if not (upper_is_board and lower_is_pole and board_height_ok and
            (upper_wide_x or upper_wide_z)):
        return None

    pole_min, pole_max = lower_bounds[0].copy(), lower_bounds[1].copy()
    min_pole_size = max(max(size_x, size_z) * 0.035, opt.min_thickness_y)
    if pole_max[0] - pole_min[0] < min_pole_size:
        center_x = (pole_min[0] + pole_max[0]) * 0.5
        pole_min[0], pole_max[0] = center_x - min_pole_size * 0.5, center_x + min_pole_size * 0.5
    if pole_max[2] - pole_min[2] < min_pole_size:
        center_z = (pole_min[2] + pole_max[2]) * 0.5
        pole_min[2], pole_max[2] = center_z - min_pole_size * 0.5, center_z + min_pole_size * 0.5
    return [ProxyBox(upper_bounds[0].copy(), upper_bounds[1].copy()),
            ProxyBox(pole_min, pole_max)]


def can_merge(a: ProxyBox, b: ProxyBox, use_x_as_length: bool,
              opt: SliceBoxOptions) -> bool:
    width_axis = 2 if use_x_as_length else 0
    width_a = a.maximum[width_axis] - a.minimum[width_axis]
    width_b = b.maximum[width_axis] - b.minimum[width_axis]
    height_a = a.maximum[1] - a.minimum[1]
    height_b = b.maximum[1] - b.minimum[1]
    width_diff = abs(width_a - width_b) / max(width_a, width_b, 0.0001)
    height_diff = abs(height_a - height_b) / max(height_a, height_b, 0.0001)
    return width_diff <= opt.width_tolerance and height_diff <= opt.height_tolerance


def merge_similar_boxes(boxes: list[ProxyBox], use_x_as_length: bool,
                        opt: SliceBoxOptions) -> list[ProxyBox]:
    if len(boxes) <= 1:
        return boxes
    merged: list[ProxyBox] = []
    current = ProxyBox(boxes[0].minimum.copy(), boxes[0].maximum.copy())
    for following in boxes[1:]:
        if can_merge(current, following, use_x_as_length, opt):
            current.minimum = np.minimum(current.minimum, following.minimum)
            current.maximum = np.maximum(current.maximum, following.maximum)
        else:
            merged.append(current)
            current = ProxyBox(following.minimum.copy(), following.maximum.copy())
    merged.append(current)
    return merged


def build_continuous_base_box(slices: list[SliceStat], bounds: np.ndarray,
                              use_x_as_length: bool, opt: SliceBoxOptions) -> ProxyBox:
    model_height = bounds[1, 1] - bounds[0, 1]
    base_top = bounds[0, 1] + model_height * 0.18
    if base_top <= bounds[0, 1]:
        base_top = bounds[0, 1] + opt.min_thickness_y
    minimum, maximum = bounds[0].copy(), bounds[1].copy()
    maximum[1] = base_top

    min_width, max_width = math.inf, -math.inf
    width_axis = 2 if use_x_as_length else 0
    for stat in slices:
        if not stat.has or stat.count < opt.min_slice_vertices:
            continue
        min_width = min(min_width, float(stat.minimum[width_axis]))
        max_width = max(max_width, float(stat.maximum[width_axis]))
    if min_width < max_width:
        minimum[width_axis], maximum[width_axis] = min_width, max_width
    return ProxyBox(minimum, maximum)


def build_outline_slice_boxes(vertices: np.ndarray, opt: SliceBoxOptions) -> list[ProxyBox]:
    if opt.slice_count <= 0:
        opt.slice_count = 32
    bounds = np.vstack((vertices.min(axis=0), vertices.max(axis=0)))
    length_x = bounds[1, 0] - bounds[0, 0]
    length_z = bounds[1, 2] - bounds[0, 2]
    use_x_as_length = length_x >= length_z
    length_axis = 0 if use_x_as_length else 2
    model_length = length_x if use_x_as_length else length_z
    if model_length == 0.0:
        return []

    slices = [SliceStat(minimum=np.full(3, math.inf), maximum=np.full(3, -math.inf))
              for _ in range(opt.slice_count)]
    for vertex in vertices:
        t = (vertex[length_axis] - bounds[0, length_axis]) / model_length
        index = min(max(int(t * opt.slice_count), 0), opt.slice_count - 1)
        stat = slices[index]
        stat.has = True
        stat.count += 1
        stat.minimum = np.minimum(stat.minimum, vertex)
        stat.maximum = np.maximum(stat.maximum, vertex)

    base = build_continuous_base_box(slices, bounds, use_x_as_length, opt)
    boxes = [base]
    base_top = base.maximum[1]
    for index, stat in enumerate(slices):
        if not stat.has or stat.count < opt.min_slice_vertices:
            continue
        t0, t1 = index / opt.slice_count, (index + 1) / opt.slice_count
        minimum, maximum = stat.minimum.copy(), stat.maximum.copy()
        minimum[length_axis] = bounds[0, length_axis] + t0 * model_length
        maximum[length_axis] = bounds[0, length_axis] + t1 * model_length
        if maximum[1] <= base_top + opt.min_thickness_y:
            continue
        minimum[1] = base_top
        if maximum[1] - minimum[1] < opt.min_thickness_y:
            maximum[1] = minimum[1] + opt.min_thickness_y
        boxes.append(ProxyBox(minimum, maximum))

    if opt.merge_similar and len(boxes) > 1:
        boxes = [boxes[0], *merge_similar_boxes(boxes[1:], use_x_as_length, opt)]
    return boxes


def is_long_rod_like(bounds: np.ndarray, ratio: float = 3.0,
                     max_volume: float = 5.0) -> bool:
    lengths = bounds[1] - bounds[0]
    if np.any(lengths <= 0.0) or float(np.prod(lengths)) > max_volume:
        return False
    ordered = np.sort(lengths)
    minimum, middle, maximum = ordered
    return maximum / middle >= ratio and maximum / minimum >= ratio * 2.0


def build_proxy_boxes(vertices: np.ndarray, opt: SliceBoxOptions) -> tuple[list[ProxyBox], str]:
    feature = detect_sign_with_single_pole(vertices, opt)
    if feature is not None:
        return feature, "single_pole_sign"
    bounds = np.vstack((vertices.min(axis=0), vertices.max(axis=0)))
    if is_long_rod_like(bounds):
        return [ProxyBox(bounds[0].copy(), bounds[1].copy())], "long_rod"
    return build_outline_slice_boxes(vertices, opt), "outline_slices"


def box_mesh(box: ProxyBox, color: np.ndarray) -> trimesh.Trimesh:
    extents = box.maximum - box.minimum
    if np.any(extents < 0.0):
        raise ValueError(f"invalid proxy box: {box.minimum.tolist()} .. {box.maximum.tolist()}")
    # Go's AddBox accepts a zero-size axis. This can occur for a very simple
    # pole whose vertices exist only at its two ends. GLB renderers handle such
    # degenerate triangles inconsistently, so keep the same centre and add only
    # a negligible output thickness.
    extents = np.maximum(extents, 1e-6)
    center = (box.minimum + box.maximum) * 0.5
    mesh = trimesh.creation.box(
        extents=extents,
        transform=trimesh.transformations.translation_matrix(center),
    )
    mesh.visual.face_colors = np.tile(color, (len(mesh.faces), 1))
    return mesh


def convert_file(source: Path, destination: Path, opt: SliceBoxOptions) -> tuple[int, str]:
    loaded = trimesh.load(source, force="scene", process=False)
    scene = loaded if isinstance(loaded, trimesh.Scene) else trimesh.Scene(loaded)
    meshes = transformed_source_meshes(scene)
    if not meshes:
        raise ValueError("model contains no triangle mesh")
    vertices = np.vstack([np.asarray(mesh.vertices, dtype=np.float64) for mesh in meshes])
    if len(vertices) == 0 or np.any(~np.isfinite(vertices)):
        raise ValueError("model has no valid world-space vertices")

    boxes, strategy = build_proxy_boxes(vertices, opt)
    if not boxes:
        raise ValueError("proxy strategy produced no boxes")
    regions = source_regions(meshes)
    result = trimesh.Scene()
    for index, box in enumerate(boxes):
        center = (box.minimum + box.maximum) * 0.5
        mesh = box_mesh(box, representative_color(center, regions))
        name = f"lod1_{strategy}_{index}"
        result.add_geometry(mesh, node_name=name, geom_name=name)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(result.export(file_type="glb"))
    return len(boxes), strategy


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate coloured LOD1 silhouette boxes for every GLB in a directory")
    parser.add_argument("input_dir", type=Path, help="directory containing source .glb files")
    parser.add_argument("--output", "-o", type=Path, default=Path("output"),
                        help="output directory (default: ./output)")
    parser.add_argument("--recursive", "-r", action="store_true",
                        help="scan subdirectories and preserve their layout")
    parser.add_argument("--overwrite", action="store_true",
                        help="replace existing output GLB files")
    parser.add_argument("--slice-count", type=int, default=32,
                        help="number of outline slices (default: 32)")
    parser.add_argument("--no-merge", action="store_true",
                        help="do not merge adjacent boxes with similar width and height")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    input_dir, output_dir = args.input_dir.resolve(), args.output.resolve()
    if not input_dir.is_dir():
        print(f"error: input directory does not exist: {input_dir}", file=sys.stderr)
        return 2
    if args.slice_count <= 0:
        print("error: --slice-count must be greater than zero", file=sys.stderr)
        return 2

    pattern = "**/*.glb" if args.recursive else "*.glb"
    sources = sorted(path for path in input_dir.glob(pattern) if path.is_file())
    if not sources:
        print(f"no GLB files found in {input_dir}")
        return 0

    opt = SliceBoxOptions(slice_count=args.slice_count, merge_similar=not args.no_merge)
    succeeded, skipped = 0, 0
    for source in sources:
        relative = source.relative_to(input_dir)
        destination = output_dir / relative
        if destination.exists() and not args.overwrite:
            skipped += 1
            print(f"SKIP {relative} (already exists; use --overwrite)")
            continue
        try:
            box_count, strategy = convert_file(source, destination, opt)
            succeeded += 1
            print(f"OK   {relative} -> {destination} [{strategy}, {box_count} boxes]")
        except Exception as exc:
            print(f"FAIL {relative}: {exc}", file=sys.stderr)

    failed = len(sources) - succeeded - skipped
    print(f"finished: {succeeded} converted, {skipped} skipped, {failed} failed")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())

# BarrierWave compilation and Geohash slicing

## Current plugin behavior

`BarrierWavePlugin.BuildLine` dispatches `feature_type` to the common mesh
builder. The existing `CoordinatePipeline.ClipLineFeatureToTile` is a pass-through,
so setting a tile region alone does not cut the guardrail.

| feature_type | Geometry | Default board placement above input Z | UV | Supports |
| --- | --- | --- | --- | --- |
| empty / `wave_two` | Continuous vertical strip with texture | bottom 0.62 m, height 0.31 m | U repeats every 1 m; V spans the texture | GPU instances |
| `wave_three` | Continuous vertical strip with texture | bottom 0.83 m, height 0.41 m | U repeats every 1 m; V spans the texture | GPU instances |
| `wave_nose_end` | Single-sided three-wave strip geometry, double-sided material | bottom 0.83 m, height 0.41 m | 0.2 m repeat with UV rotation (default 45 degrees) | None |

Wave appearance comes from the textures; these active paths do not sweep a
physical corrugated steel profile. Two/three-wave board heights accept the
existing `rail_height`/`board_base_height`/`board_bottom_height` and
`rail_height_all`/`board_height`/`rail_panel_height` aliases. The active nose-end
path uses the default three-wave dimensions. The `direction` field is mapped
by `guardrailDirection` (1 becomes internal 2; 2 becomes internal 1).

Supports use a combined post/connector template, sampled every 4 m along the
original line, plus an end sample when necessary. The current support sampler
does not read feature `spacing` or `startoffset` fields. Nodes retain
`EXT_mesh_gpu_instancing`, `EXT_instance_features`, and structural metadata;
rail primitives retain `EXT_mesh_features`.

## Slicing strategy

The reusable implementation is `map3d/road/barrier_wave_geohash.go`:

```go
result, err := road.BuildBarrierWaveGeohashSlices(features,
    road.BarrierWaveGeohashOptions{
        OutputDir: "output/barrier-wave-geohash",
        Precision: 6,
    })
```

`features` contains WGS84 `LineFeature` values with `BuildTypeBarrierWave`, the
appropriate `feature_type`, materials and business metadata. Precision zero
defaults to six; four through seven are accepted.

1. Build each original feature once in a shared source frame. Keep rail
   primitives separate by feature while cutting, so IDs are not interpolated.
2. Project Geohash boundaries into that frame and clip rail triangles against
   their vertical prisms. Interpolate positions, normals and UVs from the source.
   The shared mesh clipper uses true 3D area so vertical triangles survive.
3. Assign each support anchor to exactly one projected cell. Keep its original
   translation, rotation, scale and feature ID. Supports are not resampled at
   cut edges, so sampling phase and end posts remain unchanged.
4. A vertical face lying on a shared edge belongs only to the first matching
   cell in sorted Geohash order. Shared-edge support anchors use the same rule.
5. Keep GPU instancing. Remove unused nodes and meshes, then compact accessors
   and buffers. Preserve structural metadata and embedded textures.
6. Calculate bounds from active rail vertices and support vertices **after
   instance TRS**, not from the unplaced support template. Convert GLB axes to
   tile axes `(x, -z, y)`; aggregate child bounds into the root box. Apply the
   shared ECEF transform once on the root.

Intact supports may protrude slightly into a neighboring cell. Child boxes
include that geometry; the projected cell boundary is not the content box.
This is intentional single-owner partitioning, not physical cutting of posts.

This is a fixed-precision experiment without LOD, per-tile rebasing or exact
curved ellipsoid cuts. Each tile currently retains the source property table
(including unused rows) and textures. Metadata pruning, mesh-template deduplication,
vertex welding and adaptive subdivision remain future optimizations. Use a fresh
output directory when changing data or precision: stale files are not removed.

## Integration test

`TestBarrierWaveGeohashSlices` loads the three bundled inputs:

| Kind | GeoJSON | Texture |
| --- | --- | --- |
| Two-wave | `test/road_bxhl.geojson` | `bxhl_2.png` |
| Three-wave | `test/road_bxhl_sbb.geojson` | `sbb.png` |
| Nose-end | `test/road_bxhl_bd.geojson` | `bd_01.png` |

From the repository root in PowerShell:

```powershell
$env:MAP3D_TEST_OUTPUT_DIR = "D:/tileset-tests"
$env:MAP3D_TEST_GEOHASH_PRECISION = "6"
go test ./map3d/road -run '^TestBarrierWaveGeohashSlices$' -count=1 -v
```

Optional input overrides:

```powershell
$env:MAP3D_TEST_BARRIER_TWO_FILE = "D:/data/road_bxhl.geojson"
$env:MAP3D_TEST_BARRIER_THREE_FILE = "D:/data/road_bxhl_sbb.geojson"
$env:MAP3D_TEST_BARRIER_NOSE_FILE = "D:/data/road_bxhl_bd.geojson"
$env:MAP3D_TEST_BOUND = "117.29,39.98,117.35,40.05"
```

Bound format is WGS84 degrees `west,south,east,north`. Filtering occurs before
source generation and retains complete features only, including boundary contact;
crossing features are excluded. It applies to all three GeoJSON inputs. The earlier
Jiangxi bound does not overlap the bundled fixtures. A bound selecting no features
fails clearly. An individual subtype may have zero retained features.

Output:

- `<output>/barrier-wave-geohash/tileset.json`
- `<output>/barrier-wave-geohash/tiles/<geohash>/barrier.glb`

Serve that folder over HTTP and load its root tileset in Cesium. Without
`MAP3D_TEST_OUTPUT_DIR`, test output goes to a temporary directory and is removed.
The test checks subtype presence, 3D rail area conservation, source/output/decoded
support counts, metadata and instance attributes, accessor references and all
rendered rail/support vertices inside child and root boxes. Separate analytic
checks cover vertical-face area/UV/winding and unique ownership at a shared edge.

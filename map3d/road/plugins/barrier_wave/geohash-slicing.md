# BarrierWave compilation and Geohash slicing

## Current plugin behavior

`BarrierWavePlugin.BuildLine` dispatches `feature_type` to the common mesh
builder. The existing `CoordinatePipeline.ClipLineFeatureToTile` is a pass-through,
so setting a tile region alone does not cut the guardrail.

| feature_type | Geometry | Default board placement above input Z | UV | Supports |
| --- | --- | --- | --- | --- |
| empty / `wave_two` | Cosine two-wave swept sheet | bottom 0.445 m, height 0.310 m | U repeats every 1 m; V spans the texture | GPU instances |
| `wave_three` | Cosine three-wave swept sheet | bottom 0.444 m, height 0.506 m | U repeats every 1 m; V spans the texture | GPU instances |
| `wave_nose_end` | Single-sided three-wave strip geometry, double-sided material | bottom 0.444 m, height 0.506 m | 0.2 m repeat with UV rotation (default 45 degrees) | None |

Two-/three-wave boards now use an approximate corrugated cross-section swept
along every source line vertex. The nose end remains a separate planar textured
strip; it does not yet use a physically modeled round terminal. Its shared
three-wave default dimensions now follow the updated three-wave dimensions.

For the visual sweep, normalized height `t` is sampled using 16 segments per wave:

```text
height(t) = baseHeight + panelHeight * t
depth(t)  = 0.085 / 2 * (1 - cos(2 * pi * waveCount * t))
```

This produces 33 cross-section vertices for two waves and 49 for three waves.
The nominal straight-section depth envelope is 85 mm, not +/-85 mm. Direction
mirrors the transverse offsets and changes triangle winding. Shared sections use
capped miter offsets on bends (up to 2.5x nominal offset); closed seams share
positions and averaged normals. Near reversals are rejected instead of emitting
folded sheets. Input polyline density still controls longitudinal smoothness.

Input Z is ground datum. The 310/506 mm board heights and 85 mm depth use the
uploaded JTG/T D81-2017 table 6.2.7-1; center heights are 600/697 mm. Thus default
bottom/top are 445/755 mm and 444/950 mm. No extra curb height is added. This is a
cosine visualization approximation, NOT an exact standard manufacturing profile:
rolled edges, thickness, bolts, overlap and validated bend radii are omitted.
Posts/connectors retain their existing simplified geometry and 4 m sampling;
they have not been redesigned to match every standard protection level.

The bundled test uses a uniform silver material for two-/three-wave boards to
show the geometric relief without baked wave shading from the old textures.
Production callers can still supply their own material. A textured material is
not required for the corrugation to be visible.

Two/three-wave board heights accept the
existing `rail_height`/`board_base_height`/`board_bottom_height` and
`rail_height_all`/`board_height`/`rail_panel_height` aliases. The active nose-end
path uses the default three-wave dimensions. The `direction` field is mapped
by `guardrailDirection` (1 becomes internal 2; 2 becomes internal 1).

Supports use a combined post/connector template at 4 m horizontal intervals.
`BuildBarrierWaveGeohashSlices` now prepares connected source chains before
building any geometry. It computes its own offsets; existing SQL `startoffset`
values and business properties remain untouched. Direct plugin calls without
`LineFeature.ChainSampling` retain the legacy per-part sampler.
Nodes retain `EXT_mesh_gpu_instancing`, `EXT_instance_features`, and structural
metadata; rail primitives retain `EXT_mesh_features`.

## Continuous sidewall chains

`common.PrepareSidewallChains(features, coordinates, options)` is the reusable
preprocessor. Each original LineString part gets a `LineChainSampling` entry
with `StartOffset`, `Length`, `Reverse`, `IncludeEnd`, and `ChainID`. Original
geometry and IDs are retained; MultiLineString parts are processed individually.
The slicer invokes this automatically for `wave_two`/`wave_three`, with empty
feature type treated as two-wave. Nose ends do not generate supports and are
excluded. Other sidewall types are not automatically enabled by this change.

Connection rules:

- Match only logical end-to-start endpoints. `direction=2` follows coordinate
  order; all other values follow reverse order, matching the SQL convention.
  Sampling orientation does not change board normals or support sides.
- Group by `bxdl` (equivalent to `type`, used as fallback), wave subtype, raw
  direction, and board height parameters. Unknown classifications are grouped
  together; callers should supply a classification to isolate business types.
- Default horizontal tolerance is 0.05 m, height tolerance 0.05 m, and maximum
  horizontal turn 60 degrees. These are configurable through `options.Chains`.
  Missing Z prevents endpoint joining; it is not assumed to be zero for matching.
- A spatial endpoint index avoids testing every line against every other line.
  Only mutually unique compatible continuations join. Branches terminate rather
  than choosing an arbitrary candidate. Interior planar crossings never join,
  including overpasses with different heights. Closed input rings are independent.
- Open chains start at their directed root. Cycles start at the stable smallest
  source ID/part/geometry key, independent of feature input order. Use stable,
  unique source IDs. The key is a deterministic lexical ordering, not numeric ID
  order. No lines are physically merged or snapped.
- Length and sampling both use the source frame's horizontal XZ cumulative
  length, including the existing float32 accumulation. Z is interpolated;
  this is NOT 3D slope distance or SQL geography length. Small accepted endpoint
  gaps are not added to chain length, so physical joint spacing may differ by
  that gap (at most the connection tolerance).
- Regular samples belong to `[start,end)`. Only the true open-chain terminal
  adds an end post; its last interval may be less than 4 m. Cycles do not add
  a duplicate terminal post; their closing interval may also be less than 4 m.
  Separate chains, including unresolved branches, are not globally deduplicated.

For a segment starting at 10.3 m, the first regular post is at 1.7 m locally,
then 5.7 m, etc. An internal segment with no regular station emits no supports.
The sampler preserves source-coordinate orientation for support rotation.

```go
result, err := road.BuildBarrierWaveGeohashSlices(allFeatures,
    road.BarrierWaveGeohashOptions{
        OutputDir: "output/barrier-wave-geohash",
        Precision: 6,
        Chains: common.SidewallChainOptions{
            XYTolerance: 0.05,
            ZTolerance: 0.05,
            MaxTurnDegrees: 60,
        },
        // Bound: &[4]float64{west, south, east, north},
        // DisableChains: true, // optional legacy per-part behavior
    })
```

Always supply the complete source to the slicer and use its `Bound` option for
partial builds. Offsets and the shared source frame are computed BEFORE bound
filtering, keeping support positions stable with a chain root outside the bound.
Bound filtering keeps entire features, not fragments. Pre-filtering the inputs
in a caller loses upstream context and cannot guarantee the same phase. If using
the preprocessor directly, reuse its source coordinate frame and never apply the
result to clipped/resampled geometry. Changes to the complete source extent can
change the ENU frame and float32 rounding slightly.

This change extends support sampling only: board UV phase still begins at each
original line, while slicing preserves the UVs of each built source primitive.

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

1. Prepare full-source chain sampling, apply the optional bound, then build each original feature once in a shared source frame. Keep rail
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

Bound format is WGS84 degrees `west,south,east,north`. Filtering occurs after
full-source chain preprocessing and before source geometry generation and retains complete features only, including boundary contact;
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

Chain-specific checks:

```powershell
go test ./map3d/road/common -run '^TestSidewallChain' -count=1 -v
go test ./map3d/road -run '^TestBarrierWaveChainSliceTransformsAndBound$' -count=1 -v
```

These cover continuous offsets, reverse stationing, multipart inputs, zero-post
short parts, height/type/direction separation, missing Z, branches, planar
crossings, cycle determinism and seam ownership. The integration fixture decodes
output GLBs and compares every GPU instance translation, rotation and scale with
its prepared source, both for full output and a bound excluding the chain root.

Sweep envelope and bend checks:

```powershell
go test ./map3d/road/common -run '^TestGuardrailCosineSweep' -count=1 -v
```

These verify wave count, ground-relative elevations, total depth, winding,
unit normals, UV distance, graded/bent lines and closed-ring seams. The existing
Geohash test checks true swept surface area and support counts across slicing,
and validates decoded metadata references after compaction.

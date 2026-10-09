# Road surface Geohash slicing prototype

`TestRoadSurfaceGeohashSlices` loads `test/road_face_wgs84.geojson` and
`test/road_line.geojson`, enables input centerlines, and builds road surfaces only.
It does not include markings, curbs, barriers, instanced models or LOD.

The existing triangulation order is retained: CDT first, then centerline-based
and other fallback algorithms. Providing a centerline does not force the
centerline triangulator when CDT succeeds.

## Run

From the repository root, in PowerShell:

```powershell
$env:MAP3D_TEST_OUTPUT_DIR = "D:/tileset-tests"
$env:MAP3D_TEST_GEOHASH_PRECISION = "6"
go test ./map3d/road -run '^TestRoadSurfaceGeohashSlices$' -count=1 -v
```

On Linux:

```sh
MAP3D_TEST_OUTPUT_DIR=/tmp/tileset-tests MAP3D_TEST_GEOHASH_PRECISION=6 \
  go test ./map3d/road -run '^TestRoadSurfaceGeohashSlices$' -count=1 -v
```

Output: `<output>/roadsurface-geohash/tileset.json` and
`<output>/roadsurface-geohash/tiles/<geohash>/surface.glb`.
Serve the output folder over HTTP and load the root tileset in Cesium.
Use a fresh output directory when changing precision; empty tiles are not written
and old output files are not removed. Without the output environment variable,
files are written into a temporary test directory and removed after the test.
Precision defaults to 6; the test accepts 4 through 7.

### Optional input bound

`MAP3D_TEST_BOUND` accepts WGS84 degrees as `west,south,east,north`:

```powershell
$env:MAP3D_TEST_BOUND = "120.9509,31.7113,121.0263,31.8967"
```

Both road polygons and centerlines are filtered **before** calculating the source
frame, triangulating or enumerating tiles. Only entire features contained in the
rectangle are kept, including boundary contact. Crossing features are excluded,
not clipped to this input bound. All components and holes must fit. An unset
variable retains the original full-input behavior. Invalid bounds or a bound
containing no complete road polygons fail with a clear message; zero retained
centerlines is allowed and existing triangulation fallbacks remain available.
A bounded selection may legitimately produce just one tile or no split road.

The Shanghai example above does not overlap the bundled Beijing-area fixtures.
For a subset of the bundled inputs, use `117.29,39.98,117.35,40.05`.
Use a fresh output directory when changing the bound to avoid old tile files.

## Implementation and checks

- `common/road_surface_mesh.go` extracts geometry generation and document writing
  from `BuildRoadSurface`; the original plugin still calls both steps.
- Each original road polygon is triangulated once in a shared source frame.
- Geohash corners are projected into this frame and joined with straight edges.
  A convex polygon clipper splits triangles, interpolating positions, normals and
  UVs with float64 arithmetic before encoding float32 GLB attributes.
- All output tiles share the source origin. The root transform is applied once;
  children carry their own bounds and GLB content references. Road GLB axes are
  east/up/south; Cesium converts these to east/north/up as `(x, -z, y)`.
  Bounding boxes use the converted axes, including the negated north center.
- The test verifies multiple nonempty tiles, a road spanning multiple tiles,
  vertices inside the projected cell (5 mm tolerance), total projected mesh area
  conservation, GLB decoding and metadata presence. Every decoded GLB vertex,
  after Cesium's Y-up to Z-up rotation, must fit both its child box and the root box.
- `TestClipRoadSurfaceMeshPreservesSeamAttributes` checks an analytic sloped mesh:
  matching height/UV on a shared edge, area conservation, winding, disjoint cells,
  boundary-only contact and reversed clipping polygon winding.

## Scope and limitations

This is a fixed-precision slicing experiment, not the production partition builder.
The boundary is a straight projected approximation of a geographic Geohash cell,
not an exact curved ellipsoidal boundary. Neighboring cells use identical shared
corner/edge coordinates. Clip attributes come from the existing float32 source
mesh; using float64 intersection arithmetic does not restore source precision.

Per-tile coordinate rebasing, adaptive subdivision, LOD and database queries are
future integration work. Source mesh size is bounded by each original road polygon,
not by the output tile. The current clipper emits separate vertices per clipped
triangle; vertex welding and spatial candidate filtering are future optimizations.

CDT remains a native-library dependency. Its nested module is wired into the root
module with a local replace directive; run tests from a complete checkout containing
`map3d/road/cdt-go`. The existing CDT loader uses its bundled DLL/SO and requires a
compatible platform/native runtime.

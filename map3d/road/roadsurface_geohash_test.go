package road

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"cesium-tileset-tool/map3d/road/common"
	"cesium-tileset-tool/mapmodel/aabb"
	"github.com/mmcloughlin/geohash"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// TestRoadSurfaceGeohashSlices loads only road polygons and centerlines. Original
// polygons are triangulated once; tile cuts interpolate their mesh attributes.
// This prototype uses one shared ENU frame and projected, straight Geohash edges.
// It does not implement exact curved geodetic cuts, per-tile rebasing or LOD.
func TestRoadSurfaceGeohashSlices(t *testing.T) {
	precision := 6
	if value := os.Getenv("MAP3D_TEST_GEOHASH_PRECISION"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 4 || parsed > 7 {
			t.Fatal("MAP3D_TEST_GEOHASH_PRECISION must be 4..7")
		}
		precision = parsed
	}
	roads := readFeatureCollectionForTest(t, filepath.Join("test", "road_face_wgs84.geojson"))
	centerlines := readFeatureCollectionForTest(t, filepath.Join("test", "road_line.geojson"))
	boundValue := os.Getenv("MAP3D_TEST_BOUND")
	if boundValue != "" {
		bound, err := parseRoadSliceInputBound(boundValue)
		if err != nil {
			t.Fatalf("MAP3D_TEST_BOUND: %v", err)
		}
		roadCount, centerlineCount := len(roads.Features), len(centerlines.Features)
		roads = filterRoadSliceInputBound(roads, bound)
		centerlines = filterRoadSliceInputBound(centerlines, bound)
		t.Logf("input_bound=%v roads=%d/%d centerlines=%d/%d", bound, len(roads.Features), roadCount, len(centerlines.Features), centerlineCount)
		if len(roads.Features) == 0 {
			t.Fatal("MAP3D_TEST_BOUND contains no complete road surface features")
		}
	}
	base, region, err := calcTileContextFromFeatureCollections(roads, centerlines)
	if err != nil {
		t.Fatal(err)
	}
	ctx := TileContext{TileID: "road-surface-geohash-source", Region: region, BasePoint: base, SRID: 4326, UseENU: true}
	options := DefaultBuildOptions()
	options.UseInputCenterline = true
	builder, err := NewRoadTileBuilder(ctx, &options)
	if err != nil {
		t.Fatal(err)
	}
	builder.AddCenterlineFeatures(buildRoadCenterlineFeaturesForTest(centerlines)...)
	sourceRuntime, err := builder.newRuntime(gltf.NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.preloadCenterlines(sourceRuntime); err != nil {
		t.Fatal(err)
	}
	features := buildRoadSurfaceFeaturesForTest(roads, filepath.Join("..", "resources", "road", "lm.jpeg"))
	sourceMeshes := make([][]common.RoadSurfaceMesh, len(features))
	originalArea := 0.0
	for i := range features {
		// Keep business fields, rather than exporting just the test helper's id/type.
		fields := featureFieldsFromGeoJSON(features[i].Fields)
		fields["type"] = "road_surface"
		features[i].Features = []FeatureFields{fields}
		rings, err := sourceRuntime.Coordinates.ToLocalSurface(features[i])
		if err != nil {
			t.Fatal(err)
		}
		sourceMeshes[i], err = sourceRuntime.Meshes.BuildRoadSurfaceMeshes(features[i], rings)
		if err != nil {
			t.Fatalf("road %d: %v", i, err)
		}
		for _, mesh := range sourceMeshes[i] {
			originalArea += roadSliceMeshArea(mesh)
		}
	}
	if originalArea <= 0 {
		t.Fatal("source road mesh is empty")
	}
	hashes := roadSliceGeohashes(region, precision)
	frame, err := aabb.NewENUFrame(base[0], base[1], base[2])
	if err != nil {
		t.Fatal(err)
	}
	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}
	outDir = filepath.Join(outDir, "roadsurface-geohash")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	root := tilesetJSON{Asset: tilesetAsset{Version: "1.1"}, GeometricError: 100, Root: tilesetRoot{Refine: "ADD", GeometricError: 100}}
	var outputMin, outputMax [3]float64
	haveBounds := false
	var emittedDocs []*gltf.Document
	clippedArea := 0.0
	tileCount := 0
	splitRoad := false
	roadTileCounts := make([]int, len(features))
	for _, hash := range hashes {
		box := geohash.BoundingBox(hash)
		boundary := roadSliceProjectedBoundary(frame, box, base[2])
		doc := gltf.NewDocument()
		tileRuntime, err := builder.newRuntime(doc)
		if err != nil {
			t.Fatal(err)
		}
		primitives := []*gltf.Primitive{}
		for i, meshes := range sourceMeshes {
			clipped := []common.RoadSurfaceMesh{}
			for _, mesh := range meshes {
				cut, err := common.ClipRoadSurfaceMesh(mesh, boundary)
				if err != nil {
					t.Fatal(err)
				}
				if len(cut.Indices) == 0 {
					continue
				}
				roadSliceAssertInside(t, cut, boundary)
				clippedArea += roadSliceMeshArea(cut)
				clipped = append(clipped, cut)
			}
			if len(clipped) == 0 {
				continue
			}
			roadTileCounts[i]++
			if roadTileCounts[i] > 1 {
				splitRoad = true
			}
			prs, err := tileRuntime.Meshes.WriteRoadSurfaceMeshes(features[i], clipped)
			if err != nil {
				t.Fatal(err)
			}
			primitives = append(primitives, prs...)
		}
		if len(primitives) == 0 {
			continue
		}
		if err := tileRuntime.Metadata.Finalize(); err != nil {
			t.Fatal(err)
		}
		primitives, err = mergePrimitivesByBatch(doc, primitives)
		if err != nil {
			t.Fatal(err)
		}
		appendPrimitivesAsMesh(doc, primitives)
		if err := CompactDocument(doc); err != nil {
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		encoder := gltf.NewEncoder(&buffer)
		encoder.AsBinary = true
		if err := encoder.Encode(doc); err != nil {
			t.Fatal(err)
		}
		tileDir := filepath.Join(outDir, "tiles", hash)
		if err := os.MkdirAll(tileDir, 0755); err != nil {
			t.Fatal(err)
		}
		glbPath := filepath.Join(tileDir, "surface.glb")
		if err := os.WriteFile(glbPath, buffer.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		decoded, err := gltf.Open(glbPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(decoded.Meshes) == 0 || decoded.Extensions["EXT_structural_metadata"] == nil {
			t.Fatal("missing geometry or feature metadata")
		}
		contentURI := "tiles/" + hash + "/surface.glb"
		leafJSON, err := BuildTilesetJSONForGLB(TilesetBuildOptions{GLBPath: glbPath, ContentURI: contentURI, BasePoint: base})
		if err != nil {
			t.Fatal(err)
		}
		var leaf tilesetJSON
		if err := json.Unmarshal(leafJSON, &leaf); err != nil {
			t.Fatal(err)
		}
		root.Root.Transform = leaf.Root.Transform
		leaf.Root.Transform = nil
		root.Root.Children = append(root.Root.Children, leaf.Root)
		b := leaf.Root.BoundingVolume.Box
		assertRoadGLBInsideTileBox(t, decoded, b)
		emittedDocs = append(emittedDocs, decoded)
		for axis := 0; axis < 3; axis++ {
			lo, hi := b[axis]-b[3+axis*4], b[axis]+b[3+axis*4]
			if !haveBounds || lo < outputMin[axis] {
				outputMin[axis] = lo
			}
			if !haveBounds || hi > outputMax[axis] {
				outputMax[axis] = hi
			}
		}
		haveBounds = true
		tileCount++
		t.Logf("geohash=%s primitives=%d glb_bytes=%d", hash, len(primitives), buffer.Len())
	}
	if tileCount == 0 {
		t.Fatal("no nonempty road surface tiles generated")
	}
	if boundValue == "" && (tileCount < 2 || !splitRoad) {
		t.Fatalf("expected multiple tiles and a split road: tiles=%d split=%v", tileCount, splitRoad)
	}
	// The projected cells must partition the source mesh without area loss/overlap.
	if math.Abs(clippedArea-originalArea)/originalArea > 1e-5 {
		t.Fatalf("area mismatch: source=%f sliced=%f", originalArea, clippedArea)
	}
	b := make([]float64, 12)
	for i := 0; i < 3; i++ {
		b[i] = (outputMin[i] + outputMax[i]) / 2
		b[3+i*4] = (outputMax[i] - outputMin[i]) / 2
	}
	root.Root.BoundingVolume.Box = b
	for _, doc := range emittedDocs {
		assertRoadGLBInsideTileBox(t, doc, b)
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outDir, "tileset.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("roads=%d centerlines=%d tiles=%d source_area=%.3f sliced_area=%.3f tileset=%s", len(features), len(centerlines.Features), tileCount, originalArea, clippedArea, path)
}

func roadSliceGeohashes(region [6]float64, precision int) []string {
	cell := geohash.BoundingBox(geohash.EncodeWithPrecision((region[1]+region[3])/2, (region[0]+region[2])/2, uint(precision)))
	dx, dy := cell.MaxLng-cell.MinLng, cell.MaxLat-cell.MinLat
	// Include one neighboring row/column: projection and mesh interpolation can
	// move extreme vertices slightly outside the input geographic bounding box.
	startX := math.Floor((region[0]+180)/dx)*dx - 180 - dx
	startY := math.Floor((region[1]+90)/dy)*dy - 90 - dy
	seen := map[string]bool{}
	for y := startY; y <= region[3]+dy; y += dy {
		for x := startX; x <= region[2]+dx; x += dx {
			seen[geohash.EncodeWithPrecision(y+dy/2, x+dx/2, uint(precision))] = true
		}
	}
	out := make([]string, 0, len(seen))
	for hash := range seen {
		out = append(out, hash)
	}
	sort.Strings(out)
	return out
}
func roadSliceProjectedBoundary(frame *aabb.ENUFrame, box geohash.Box, height float64) [][2]float64 {
	corners := [][2]float64{{box.MinLng, box.MinLat}, {box.MaxLng, box.MinLat}, {box.MaxLng, box.MaxLat}, {box.MinLng, box.MaxLat}}
	out := make([][2]float64, 4)
	for i, p := range corners {
		v := frame.Offset(p[0], p[1], height)
		out[i] = [2]float64{v[0], v[1]}
	}
	return out
}
func roadSliceMeshArea(mesh common.RoadSurfaceMesh) float64 {
	area := 0.0
	for i := 0; i < len(mesh.Indices); i += 3 {
		a, b, c := mesh.Positions[mesh.Indices[i]], mesh.Positions[mesh.Indices[i+1]], mesh.Positions[mesh.Indices[i+2]]
		area += math.Abs((float64(b[0])-float64(a[0]))*(float64(c[2])-float64(a[2]))-(float64(b[2])-float64(a[2]))*(float64(c[0])-float64(a[0]))) / 2
	}
	return area
}
func roadSliceAssertInside(t *testing.T, mesh common.RoadSurfaceMesh, boundary [][2]float64) {
	t.Helper()
	area := 0.0
	for i, a := range boundary {
		b := boundary[(i+1)%len(boundary)]
		area += a[0]*b[1] - b[0]*a[1]
	}
	sign := 1.0
	if area < 0 {
		sign = -1
	}
	for _, p := range mesh.Positions {
		for i, a := range boundary {
			b := boundary[(i+1)%len(boundary)]
			dx, dz := b[0]-a[0], b[1]-a[1]
			distance := sign * (dx*(float64(p[2])-a[1]) - dz*(float64(p[0])-a[0])) / math.Hypot(dx, dz)
			if distance < -0.005 {
				t.Fatalf("clipped vertex outside tile: %v distance=%f", p, distance)
			}
		}
	}
}

// parseRoadSliceInputBound accepts WGS84 degrees in west,south,east,north order.
func parseRoadSliceInputBound(value string) ([4]float64, error) {
	var bound [4]float64
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return bound, fmt.Errorf("expected west,south,east,north")
	}
	for i, part := range parts {
		number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return bound, fmt.Errorf("coordinate %d must be finite", i+1)
		}
		bound[i] = number
	}
	if bound[0] < -180 || bound[2] > 180 || bound[1] < -90 || bound[3] > 90 || bound[0] >= bound[2] || bound[1] >= bound[3] {
		return bound, fmt.Errorf("require -180 <= west < east <= 180 and -90 <= south < north <= 90")
	}
	return bound, nil
}

// filterRoadSliceInputBound keeps entire features, never clipped fragments.
// Boundary contact is included; holes and all multipart components must fit.
func filterRoadSliceInputBound(fc *geojson.FeatureCollection, bound [4]float64) *geojson.FeatureCollection {
	filtered := *fc
	filtered.Features = nil
	for _, feature := range fc.Features {
		if feature != nil && roadSliceGeometryInsideBound(feature.Geometry, bound) {
			filtered.Features = append(filtered.Features, feature)
		}
	}
	return &filtered
}

func roadSliceGeometryInsideBound(g geom.T, bound [4]float64) bool {
	if g == nil {
		return false
	}
	if collection, ok := g.(*geom.GeometryCollection); ok {
		if collection.NumGeoms() == 0 {
			return false
		}
		for i := 0; i < collection.NumGeoms(); i++ {
			if !roadSliceGeometryInsideBound(collection.Geom(i), bound) {
				return false
			}
		}
		return true
	}
	flat, stride := g.FlatCoords(), g.Stride()
	if stride < 2 || len(flat) == 0 || len(flat)%stride != 0 {
		return false
	}
	for i := 0; i < len(flat); i += stride {
		x, y := flat[i], flat[i+1]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < bound[0] || x > bound[2] || y < bound[1] || y > bound[3] {
			return false
		}
	}
	return true
}

func TestRoadSliceInputBound(t *testing.T) {
	bound, err := parseRoadSliceInputBound(" 120.9509,31.7113,121.0263,31.8967 ")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "1,2,3", "1,2,3,4,5", "bad,2,3,4", "NaN,2,3,4", "1,2,+Inf,4", "3,2,1,4", "1,4,3,2", "-181,2,3,4", "1,2,181,4", "1,-91,3,4", "1,2,3,91"} {
		if _, err := parseRoadSliceInputBound(value); err == nil {
			t.Errorf("invalid bound accepted: %q", value)
		}
	}
	inside := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{bound[0], bound[1], 12}, {bound[2], bound[3], 15}})
	crossing := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{121, 31.8, 12}, {121.1, 31.8, 15}})
	outside := geom.NewPointFlat(geom.XY, []float64{122, 32})
	fc := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: inside}, {Geometry: crossing}, {Geometry: outside}, {}}}
	filtered := filterRoadSliceInputBound(fc, bound)
	if len(filtered.Features) != 1 || filtered.Features[0] != fc.Features[0] || len(fc.Features) != 4 {
		t.Fatal("bound must retain only complete contained features without mutating input")
	}
	multipart := geom.NewMultiLineString(geom.XYZ)
	if err := multipart.Push(inside); err != nil {
		t.Fatal(err)
	}
	if err := multipart.Push(crossing); err != nil {
		t.Fatal(err)
	}
	if roadSliceGeometryInsideBound(multipart, bound) {
		t.Fatal("multipart geometry crossing bound accepted")
	}
}

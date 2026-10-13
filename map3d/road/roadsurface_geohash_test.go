package road

import (
	"encoding/json"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRoadSurfaceGeohashSlices(t *testing.T) {
	precision := 6
	if value := os.Getenv("MAP3D_TEST_GEOHASH_PRECISION"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		precision = parsed
	}
	surfacePath := os.Getenv("MAP3D_TEST_ROAD_SURFACE_FILE")
	if surfacePath == "" {
		surfacePath = filepath.Join("test", "road_face_wgs84.geojson")
	}
	linePath := os.Getenv("MAP3D_TEST_CENTERLINE_FILE")
	if linePath == "" {
		linePath = filepath.Join("test", "road_line.geojson")
	}
	roads := readFeatureCollectionForTest(t, surfacePath)
	centerlines := readFeatureCollectionForTest(t, linePath)
	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}
	bound := os.Getenv("MAP3D_TEST_BOUND")
	result, err := BuildRoadSurfaceGeohashSlices(roads, centerlines, RoadSurfaceGeohashOptions{
		OutputDir: filepath.Join(outDir, "roadsurface-geohash"), Precision: precision, Bound: bound,
		TexturePath: filepath.Join("..", "resources", "road", "lm.jpeg"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if bound == "" && (len(result.Tiles) < 2 || !result.SplitRoad) {
		t.Fatal("expected multiple tiles and a split road")
	}
	if math.Abs(result.SourceArea-result.SlicedArea)/result.SourceArea > 1e-5 {
		t.Fatal("mesh area not conserved")
	}
	data, err := os.ReadFile(result.TilesetPath)
	if err != nil {
		t.Fatal(err)
	}
	var root tilesetJSON
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Root.Children) != len(result.Tiles) {
		t.Fatal("tile count mismatch")
	}
	for i, tile := range result.Tiles {
		doc, err := gltf.Open(tile.GLBPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Meshes) == 0 || doc.Extensions["EXT_structural_metadata"] == nil {
			t.Fatal("missing geometry or metadata")
		}
		assertRoadGLBInsideTileBox(t, doc, root.Root.Children[i].BoundingVolume.Box)
		assertRoadGLBInsideTileBox(t, doc, root.Root.BoundingVolume.Box)
		t.Logf("geohash=%s primitives=%d glb_bytes=%d", tile.Geohash, tile.PrimitiveCount, tile.GLBBytes)
	}
	t.Logf("roads=%d centerlines=%d tiles=%d source_area=%.3f sliced_area=%.3f tileset=%s", result.RoadCount, result.CenterlineCount, len(result.Tiles), result.SourceArea, result.SlicedArea, result.TilesetPath)
}

func TestRoadSliceInputBound(t *testing.T) {
	bound, err := ParseRoadSurfaceBound(" 120.9509,31.7113,121.0263,31.8967 ")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "1,2,3", "1,2,3,4,5", "bad,2,3,4", "NaN,2,3,4", "1,2,+Inf,4", "3,2,1,4", "1,4,3,2", "-181,2,3,4", "1,2,181,4", "1,-91,3,4", "1,2,3,91"} {
		if _, err := ParseRoadSurfaceBound(value); err == nil {
			t.Errorf("invalid bound accepted: %q", value)
		}
	}
	inside := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{bound[0], bound[1], 12}, {bound[2], bound[3], 15}})
	crossing := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{121, 31.8, 12}, {121.1, 31.8, 15}})
	outside := geom.NewPointFlat(geom.XY, []float64{122, 32})
	fc := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: inside}, {Geometry: crossing}, {Geometry: outside}, {}}}
	filtered := FilterRoadSurfaceInputBound(fc, bound)
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

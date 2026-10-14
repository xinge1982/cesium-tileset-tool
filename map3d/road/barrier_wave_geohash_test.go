package road

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"cesium-tileset-tool/map3d/road/common"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// TestBarrierWaveGeohashSlices reads only two-wave, three-wave and nose-end
// GeoJSONs. All input features must fit MAP3D_TEST_BOUND when it is set.
func TestBarrierWaveGeohashSlices(t *testing.T) {
	precision := 6
	if value := os.Getenv("MAP3D_TEST_GEOHASH_PRECISION"); value != "" {
		var err error
		precision, err = strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	specs := []struct {
		env, file, texture, kind string
		convert                  func(*geojson.FeatureCollection, string) []LineFeature
	}{
		{"MAP3D_TEST_BARRIER_TWO_FILE", "road_bxhl.geojson", "bxhl_2.png", "wave_two", buildGuardrailTwoWaveFeaturesForTest},
		{"MAP3D_TEST_BARRIER_THREE_FILE", "road_bxhl_sbb.geojson", "sbb.png", "wave_three", buildGuardrailThreeWaveFeaturesForTest},
		{"MAP3D_TEST_BARRIER_NOSE_FILE", "road_bxhl_bd.geojson", "bd_01.png", "wave_nose_end", buildGuardrailNoseEndFeaturesForTest},
	}
	features := []LineFeature{}
	for _, spec := range specs {
		path := os.Getenv(spec.env)
		if path == "" {
			path = filepath.Join("test", spec.file)
		}
		fc := readFeatureCollectionForTest(t, path)
		inputCount := len(fc.Features)
		if value := os.Getenv("MAP3D_TEST_BOUND"); value != "" {
			bound, err := ParseRoadSurfaceBound(value)
			if err != nil {
				t.Fatal(err)
			}
			fc = FilterRoadSurfaceInputBound(fc, bound)
		}
		lines := spec.convert(fc, filepath.Join("..", "resources", "road", spec.texture))
		for i := range lines {
			fields := featureFieldsFromGeoJSON(lines[i].Fields)
			fields["type"] = "barrier_" + spec.kind
			lines[i].Features = []FeatureFields{fields}
			lines[i].Material.Top.Name = "barrier-" + spec.kind + "-rail"
		}
		features = append(features, lines...)
		t.Logf("%s retained=%d/%d", spec.kind, len(lines), inputCount)
	}
	out := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if out == "" {
		out = t.TempDir()
	}
	result, err := BuildBarrierWaveGeohashSlices(features, BarrierWaveGeohashOptions{OutputDir: filepath.Join(out, "barrier-wave-geohash"), Precision: precision})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MAP3D_TEST_BOUND") == "" && len(result.Tiles) < 2 {
		t.Fatal("expected multiple tiles")
	}
	if math.Abs(result.SourceArea-result.SlicedArea)/result.SourceArea > 1e-5 {
		t.Fatal("rail area changed")
	}
	if result.SourceInstances != result.SlicedInstances {
		t.Fatal("support instances lost or duplicated")
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
		t.Fatal("root child count differs")
	}
	supports := 0
	railTypes := map[string]int{}
	for i, tile := range result.Tiles {
		doc, err := gltf.Open(tile.GLBPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := waveNormalizeExtensions(doc); err != nil {
			t.Fatal(err)
		}
		if doc.Extensions["EXT_structural_metadata"] == nil {
			t.Fatal("missing property table")
		}
		for _, mesh := range doc.Meshes {
			for _, primitive := range mesh.Primitives {
				for _, index := range primitive.Attributes {
					if index < 0 || index >= len(doc.Accessors) {
						t.Fatal("invalid accessor reference after slicing")
					}
				}
				if extras, ok := primitive.Extras.(map[string]any); ok {
					if kind, ok := extras["batchType"].(string); ok {
						railTypes[kind]++
					}
				}
			}
		}
		for _, n := range doc.Nodes {
			tr, _, _, ids, instanced, err := readInstancedNodeAttributes(doc, n)
			if err != nil {
				t.Fatal(err)
			}
			if instanced {
				supports += len(tr)
				if n.Extensions["EXT_instance_features"] == nil || len(ids) != len(tr) {
					t.Fatal("missing support feature IDs")
				}
			}
		}
		if err := waveVisitPositions(doc, func(p [3]float64) {
			local := [3]float64{p[0], -p[2], p[1]}
			for _, box := range [][]float64{root.Root.BoundingVolume.Box, root.Root.Children[i].BoundingVolume.Box} {
				for axis, v := range local {
					if math.Abs(v-box[axis]) > box[3+4*axis]+0.005 {
						t.Fatalf("rendered support/rail vertex %v outside box %v", local, box)
					}
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		for _, node := range doc.Nodes {
			if node.Mesh == nil || node.Extensions["EXT_mesh_gpu_instancing"] != nil {
				continue
			}
			for _, p := range doc.Meshes[*node.Mesh].Primitives {
				if p.Extensions["EXT_mesh_features"] == nil {
					t.Fatal("missing rail feature IDs")
				}
			}
		}
		t.Logf("geohash=%s primitives=%d glb_bytes=%d", tile.Geohash, tile.PrimitiveCount, tile.GLBBytes)
	}
	if supports != result.SourceInstances {
		t.Fatalf("decoded support count %d != source %d", supports, result.SourceInstances)
	}
	if os.Getenv("MAP3D_TEST_BOUND") == "" {
		for _, kind := range []string{"wave_two", "wave_three", "wave_nose_end"} {
			if railTypes["barrier_"+kind] == 0 {
				t.Fatalf("missing compiled subtype %s", kind)
			}
		}
	}
	t.Logf("features=%d tiles=%d rail_area=%.3f sliced_area=%.3f instances=%d tileset=%s", len(features), len(result.Tiles), result.SourceArea, result.SlicedArea, supports, result.TilesetPath)
}

func TestBarrierWaveSeamFaceHasSingleOwner(t *testing.T) {
	cells := []waveSliceCell{{"left", [][2]float64{{0, 0}, {1, 0}, {1, 2}, {0, 2}}}, {"right", [][2]float64{{1, 0}, {2, 0}, {2, 2}, {1, 2}}}}
	mesh := common.RoadSurfaceMesh{Positions: [][3]float32{{1, 0, 0}, {1, 1, 0}, {1, 1, 2}}, Normals: [][3]float32{{1, 0, 0}, {1, 0, 0}, {1, 0, 0}}, UVs: [][2]float32{{0, 0}, {0, 1}, {2, 1}}, Indices: []uint32{0, 1, 2}}
	area := 0.
	for i, cell := range cells {
		cut, err := common.ClipRoadSurfaceMesh(mesh, cell.boundary)
		if err != nil {
			t.Fatal(err)
		}
		cut = waveDropOtherSeamFaces(cut, cells, i)
		area += waveMeshArea(cut)
	}
	if math.Abs(area-1) > 1e-6 {
		t.Fatalf("seam face lost or duplicated: area=%f", area)
	}
	if waveCellOwner([3]float32{1, 0, 1}, cells) != 0 {
		t.Fatal("shared-edge support needs deterministic single ownership")
	}
}

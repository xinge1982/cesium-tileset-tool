package road

import (
	"encoding/json"
	"github.com/mmcloughlin/geohash"
	"github.com/twpayne/go-geom"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"cesium-tileset-tool/map3d/road/common"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Read only two-wave, three-wave and nose-end
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
	opt := BarrierWaveGeohashOptions{OutputDir: filepath.Join(out, "barrier-wave-geohash"), Precision: precision}
	for _, input := range []struct {
		name   string
		target *float32
	}{
		{"MAP3D_TEST_POST_DIAMETER", &opt.Posts.Diameter},
		{"MAP3D_TEST_POST_TOP_OFFSET", &opt.Posts.TopAboveBoardCenter},
		{"MAP3D_TEST_POST_HEIGHT", &opt.Posts.Height},
		{"MAP3D_TEST_POST_SPACING", &opt.Posts.Spacing},
		{"MAP3D_TEST_POST_LATERAL_OFFSET", &opt.Posts.LateralOffset},
	} {
		if value := os.Getenv(input.name); value != "" {
			n, err := strconv.ParseFloat(value, 32)
			if err != nil {
				t.Fatal(err)
			}
			*input.target = float32(n)
		}
	}
	if value := os.Getenv("MAP3D_TEST_POST_SEGMENTS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		opt.Posts.RadialSegments = n
	}

	if value := os.Getenv("MAP3D_TEST_BOUND"); value != "" {
		bound, err := ParseRoadSurfaceBound(value)
		if err != nil {
			t.Fatal(err)
		}
		opt.Bound = &bound
	}
	result, err := BuildBarrierWaveGeohashSlices(features, opt)
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
		if err := ValidateStructuralMetadataBufferViews(doc); err != nil {
			t.Fatal(err)
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

// Verify actual decoded GPU instance TRS values, not just counts. Bound builds
// keep exactly the full-source phase, even when the chain root is outside.
func TestBarrierWaveChainSliceTransformsAndBound(t *testing.T) {
	box := geohash.BoundingBox(geohash.EncodeWithPrecision(31, 121, 6))
	x := box.MaxLng
	y := (box.MinLat + box.MaxLat) / 2
	makeFeature := func(id int, a, b float64) LineFeature {
		return LineFeature{FeatureInput: FeatureInput{BuildType: BuildTypeBarrierWave, Fields: FeatureFields{"id": id, "bxdl": 5, "direction": 2, "feature_type": "wave_two"}, Geom: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{a, y, 10}, {b, y, 10}})}}
	}
	features := []LineFeature{makeFeature(1, x-.00012, x-.00002), makeFeature(2, x-.00002, x+.00012)}
	ctx, err := ResolveTileContextAuto(TileContext{TileID: "source", SRID: 4326, UseENU: true}, nil, features, nil)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := common.NewCoordinatePipeline(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := common.PrepareSidewallChains(features, cp, common.SidewallChainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared[0].ChainSampling[0].ChainID != prepared[1].ChainSampling[0].ChainID {
		t.Fatal("fixture did not form a chain")
	}
	type trs struct {
		T [3]float32
		R [4]float32
		S [3]float32
	}
	collect := func(doc *gltf.Document, out map[trs]int) {
		t.Helper()
		if err := waveNormalizeExtensions(doc); err != nil {
			t.Fatal(err)
		}
		for _, n := range doc.Nodes {
			translations, rotations, scales, _, ok, err := readInstancedNodeAttributes(doc, n)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				for i, p := range translations {
					out[trs{p, rotations[i], scales[i]}]++
				}
			}
		}
	}
	for _, bounded := range []bool{false, true} {
		t.Run(strconv.FormatBool(bounded), func(t *testing.T) {
			selected := prepared
			opt := BarrierWaveGeohashOptions{OutputDir: t.TempDir(), Precision: 6}
			if bounded {
				b := [4]float64{x - .00002, y - .00001, x + .00013, y + .00001}
				opt.Bound = &b
				selected = prepared[1:]
			}
			buildOpt := DefaultBuildOptions()
			buildOpt.MergePrimitives = false
			builder, err := NewRoadTileBuilder(ctx, &buildOpt)
			if err != nil {
				t.Fatal(err)
			}
			builder.AddLineFeatures(selected...)
			source, _, err := builder.BuildDocument()
			if err != nil {
				t.Fatal(err)
			}
			expected := map[trs]int{}
			collect(source, expected)
			result, err := BuildBarrierWaveGeohashSlices(features, opt)
			if err != nil {
				t.Fatal(err)
			}
			if !bounded && len(result.Tiles) < 2 {
				t.Fatal("fixture does not cross a tile seam")
			}
			got := map[trs]int{}
			for _, tile := range result.Tiles {
				doc, err := gltf.Open(tile.GLBPath)
				if err != nil {
					t.Fatal(err)
				}
				collect(doc, got)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("instance TRS changed across slicing/bound: got %v want %v", got, expected)
			}
			for _, count := range got {
				if count != 1 {
					t.Fatal("duplicate support at joint/seam")
				}
			}
			if bounded && prepared[1].ChainSampling[0].StartOffset == 0 {
				t.Fatal("root outside bound reset phase")
			}
		})
	}
}

// Verify global inputs and feature overrides survive construction and GLB slicing.
func TestBarrierWavePostInputOptions(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(strconv.FormatBool(override), func(t *testing.T) {
			f := LineFeature{FeatureInput: FeatureInput{BuildType: BuildTypeBarrierWave, Fields: FeatureFields{"id": 1, "bxdl": 5, "direction": 2, "feature_type": "wave_two"}, Geom: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{121, 31, 10}, {121.0002, 31, 10}})}}
			opt := BarrierWaveGeohashOptions{OutputDir: t.TempDir(), Precision: 6, Posts: common.GuardrailPostOptions{Diameter: .16, Height: 1.25, Spacing: 2, LateralOffset: .3, RadialSegments: 16}}
			height, diameter := float32(1.25), float32(.16)
			if override {
				f.GuardrailPosts = &common.GuardrailPostOptions{Diameter: .2, Height: 1.4, Spacing: 3, LateralOffset: .4, RadialSegments: 16}
				height = 1.4
				diameter = .2
			}
			result, err := BuildBarrierWaveGeohashSlices([]LineFeature{f}, opt)
			if err != nil {
				t.Fatal(err)
			}
			if result.SourceInstances == 0 || result.SourceInstances != result.SlicedInstances {
				t.Fatal("lost posts")
			}
			for _, tile := range result.Tiles {
				doc, err := gltf.Open(tile.GLBPath)
				if err != nil {
					t.Fatal(err)
				}
				if err = waveNormalizeExtensions(doc); err != nil {
					t.Fatal(err)
				}
				for _, node := range doc.Nodes {
					_, _, _, _, instanced, err := readInstancedNodeAttributes(doc, node)
					if err != nil {
						t.Fatal(err)
					}
					if !instanced {
						continue
					}
					for _, primitive := range doc.Meshes[*node.Mesh].Primitives {
						pos, err := modeler.ReadPosition(doc, doc.Accessors[primitive.Attributes[gltf.POSITION]], nil)
						if err != nil {
							t.Fatal(err)
						}
						lo, hi := pos[0], pos[0]
						for _, v := range pos {
							for k := 0; k < 3; k++ {
								lo[k] = min(lo[k], v[k])
								hi[k] = max(hi[k], v[k])
							}
						}
						if math.Abs(float64(hi[1]-height)) > 1e-6 || math.Abs(float64(lo[1])) > 1e-6 || math.Abs(float64(hi[0]-lo[0]-diameter)) > 1e-6 {
							t.Fatal("post inputs not reflected in GLB", lo, hi)
						}
					}
				}
			}
		})
	}
}

package road

import (
	"bytes"
	"cesium-tileset-tool/map3d/mgltf"
	"cesium-tileset-tool/map3d/road/common"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestRoadSurfaceExtrudePluginToGLB(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	geojsonPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_wgs84.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lm.jpeg"))

	buf, err := os.ReadFile(geojsonPath)
	if err != nil {
		t.Fatalf("read geojson %s: %v", geojsonPath, err)
	}

	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal geojson: %v", err)
	}
	if len(fc.Features) == 0 {
		t.Fatalf("geojson has no features: %s", geojsonPath)
	}

	basePoint, region, err := calcTileContextFromFeatureCollection(fc)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-surface-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    false,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      fc.Features[i].Geometry,
				Fields:    featureFieldsFromGeoJSON(fc.Features[i].Properties),
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "道路"),
			},
			Thickness: 0,
			Material: MaterialSet{
				Top: MaterialRef{
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
				Side: MaterialRef{
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
				Bottom: MaterialRef{
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
			},
			UV: UVOptions{
				Mapping:       UVMappingPlanar,
				RepeatX:       8,
				RepeatY:       8,
				FlipV:         true,
				ScaleByMeters: true,
			},
		})
	}
	if len(features) == 0 {
		t.Fatalf("no surface features built from %s", geojsonPath)
	}

	builder.AddSurfaceFeatures(features...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	out := filepath.Join(outDir, "roadsurface-road-surface-extrude.glb")
	if err := os.WriteFile(out, glb, 0o644); err != nil {
		t.Fatalf("write glb: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat glb: %v", err)
	}
	if st.Size() == 0 {
		t.Fatalf("glb is empty: %s", out)
	}
	t.Logf("generated glb: %s (%d bytes)", out, st.Size())
}

func TestRoadCurbToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_lys.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lys.png"))

	lineFC := readFeatureCollectionForTest(t, linePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-curb-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildRoadCurbFeaturesForTest(lineFC, texturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated curb glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-curb", glb, basePoint, false)
}

func TestRoadTollIslandToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lj.jpeg"))
	geojsonPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_island.geojson"))
	fc := readFeatureCollectionForTest(t, geojsonPath)
	if len(fc.Features) == 0 {
		t.Fatalf("geojson has no features: %s", geojsonPath)
	}
	for _, feature := range fc.Features {
		if feature.Properties == nil {
			feature.Properties = map[string]any{}
		}
		feature.Properties["head_model_path"] = `..\resources\model\dt_01.glb`
		feature.Properties["tail_model_path"] = `..\resources\model\dw_01.glb`
		if _, ok := feature.Properties["width"]; !ok {
			if v, ok2 := feature.Properties["widths"]; ok2 {
				feature.Properties["width"] = v
			}
		}
		if _, ok := feature.Properties["height"]; !ok {
			feature.Properties["height"] = 0.45
		}
	}

	basePoint, region, err := calcTileContextFromFeatureCollections(fc)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-toll-island-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildRoadTollIslandFeaturesForTest(fc, texturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated toll island glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = "./test"
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-toll-island", glb, basePoint, false)
}

func TestNewJerseyBarrierToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_xzx.geojson"))
	concretePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_snglq.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "xzx.png"))
	concreteTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "snq_01.png"))

	lineFC := readFeatureCollectionForTest(t, linePath)
	concreteFC := readFeatureCollectionForTest(t, concretePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(lineFC, concreteFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "new-jersey-barrier-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildNewJerseyBarrierFeaturesForTest(lineFC, texturePath)...)
	builder.AddLineFeatures(buildConcreteWallFeaturesForTest(concreteFC, concreteTexturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated new jersey barrier glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-new-jersey", glb, basePoint, false)
}

func TestConcreteWallToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_snglq.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "snq_01.png"))

	lineFC := readFeatureCollectionForTest(t, linePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "concrete-wall-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildConcreteWallFeaturesForTest(lineFC, texturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated concrete wall glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-concrete-wall", glb, basePoint, false)
}

func TestGuardrailTwoWaveToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_bxhl.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "bxhl_2.png"))
	lineFC := readFeatureCollectionForTest(t, linePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "guardrail-two-wave-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildGuardrailTwoWaveFeaturesForTest(lineFC, texturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated guardrail glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-guardrail-two-wave", glb, basePoint, false)
}

func TestGuardrailThreeWaveToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_bxhl_sbb.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "sbb.png"))
	lineFC := readFeatureCollectionForTest(t, linePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "guardrail-three-wave-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildGuardrailThreeWaveFeaturesForTest(lineFC, texturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated guardrail three wave glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-guardrail-three-wave", glb, basePoint, false)
}

func TestGuardrailWaveToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	twoWavePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_bxhl.geojson"))
	threeWavePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_bxhl_sbb.geojson"))
	noseEndPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_bxhl_bd.geojson"))
	noiseWallPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_spz.geojson"))
	twoWaveTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "bxhl_2.png"))
	threeWaveTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "sbb.png"))
	noseEndTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "bd_01.png"))
	noiseWallTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "spz_01.png"))

	twoWaveFC := readFeatureCollectionForTest(t, twoWavePath)
	threeWaveFC := readFeatureCollectionForTest(t, threeWavePath)
	noseEndFC := readFeatureCollectionForTest(t, noseEndPath)
	noiseWallFC := readFeatureCollectionForTest(t, noiseWallPath)
	basePoint, region, err := calcTileContextFromFeatureCollections(twoWaveFC, threeWaveFC, noseEndFC, noiseWallFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "guardrail-wave-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildGuardrailTwoWaveFeaturesForTest(twoWaveFC, twoWaveTexturePath)...)
	builder.AddLineFeatures(buildGuardrailThreeWaveFeaturesForTest(threeWaveFC, threeWaveTexturePath)...)
	builder.AddLineFeatures(buildGuardrailNoseEndFeaturesForTest(noseEndFC, noseEndTexturePath)...)
	builder.AddLineFeatures(buildNoiseWallFeaturesForTest(noiseWallFC, noiseWallTexturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated guardrail wave glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-guardrail-wave", glb, basePoint, false)
}

func TestGuardrailNoseEndToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_bxhl_bd.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "bd_01.png"))
	lineFC := readFeatureCollectionForTest(t, linePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "guardrail-nose-end-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddLineFeatures(buildGuardrailNoseEndFeaturesForTest(lineFC, texturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatal("generated guardrail nose end glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-guardrail-nose-end", glb, basePoint, false)
}

func TestRoadSurfaceAndRoadMarkingToGLB(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	roadPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_face_wgs84.geojson"))
	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	centerlinePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lm.jpeg"))

	roadFC := readFeatureCollectionForTest(t, roadPath)
	lineFC := readFeatureCollectionForTest(t, linePath)
	enrichedLineFC := enrichLineRoadIDsForTest(t, lineFC, roadFC)
	centerlineFC := readFeatureCollectionForTest(t, centerlinePath)

	basePoint, region, err := calcTileContextFromFeatureCollections(roadFC, lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-surface-line-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddCenterlineFeatures(buildRoadCenterlineFeaturesForTest(centerlineFC)...)
	builder.AddSurfaceFeatures(buildRoadSurfaceFeaturesForTest(roadFC, texturePath)...)
	markingFeatures := buildRoadMarkingFeaturesForTest(enrichedLineFC)
	markLineFeaturesLowCost(markingFeatures)
	builder.AddLineFeatures(markingFeatures...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-road-and-marking", glb, basePoint, false)
}

func TestRoadLineTileset(t *testing.T) {
	start := time.Now()
	t.Logf("phase=prepare start")
	expansionJointPath, _ := filepath.Abs(filepath.Join("./test/road_ssf.geojson"))
	expansionJointFC := readFeatureCollectionForTest(t, expansionJointPath)
	phase := time.Now()
	basePoint, region, err := calcTileContextFromFeatureCollections(expansionJointFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}
	t.Logf("phase=calc_context elapsed=%s", time.Since(phase))
	phase = time.Now()
	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-surface-solid-dashed-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}
	t.Logf("phase=new_builder elapsed=%s", time.Since(phase))

	builder.AddLineFeatures(buildRoadMarkingFeaturesForTest(expansionJointFC)...)

	phase = time.Now()
	doc, primitives, err := builder.BuildDocument()
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	t.Logf("phase=build_document elapsed=%s primitives=%d materials=%d meshes=%d nodes=%d",
		time.Since(phase), len(primitives), len(doc.Materials), len(doc.Meshes), len(doc.Nodes))

	phase = time.Now()
	appendPrimitivesAsMesh(doc, primitives)
	if err := CompactDocument(doc); err != nil {
		t.Fatalf("compact document: %v", err)
	}
	t.Logf("phase=compact_document elapsed=%s", time.Since(phase))

	phase = time.Now()
	buff := new(bytes.Buffer)
	enc := gltf.NewEncoder(buff)
	enc.AsBinary = true
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("encode glb: %v", err)
	}
	glb := buff.Bytes()
	t.Logf("phase=encode_glb elapsed=%s bytes=%d", time.Since(phase), len(glb))
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	phase = time.Now()
	outDir := "./test"

	writeRoadTileOutputs(t, outDir, "roadsurface-road-solid-dashed", glb, basePoint, true)
	t.Logf("phase=write_outputs elapsed=%s total=%s", time.Since(phase), time.Since(start))
}

func TestRoadSurfaceSolidAndDashedMarkingsToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}
	start := time.Now()
	t.Logf("phase=prepare start")

	roadPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_face_wgs84.geojson"))
	greenbeltPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_fgd_lhm.geojson"))
	medianIslandPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_fgd_lm.geojson"))
	markPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_mark.geojson"))
	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	expansionJointPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_ssf.geojson"))
	fxbPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line_fxb.geojson"))
	curbPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_lys.geojson"))
	centerlinePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lm.jpeg"))
	greenbeltTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lhm.jpg"))
	medianIslandTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lj.jpeg"))
	expansionJointTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "ssf_01.png"))
	curbTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lys.png"))
	fxbModelPath, _ := filepath.Abs(filepath.Join(`../resources/model`, "fxb_02.glb"))

	roadFC := readFeatureCollectionForTest(t, roadPath)
	greenbeltFC := readFeatureCollectionForTest(t, greenbeltPath)
	medianIslandFC := readFeatureCollectionForTest(t, medianIslandPath)
	markFC := readFeatureCollectionForTest(t, markPath)
	lineFC := readFeatureCollectionForTest(t, linePath)
	expansionJointFC := readFeatureCollectionForTest(t, expansionJointPath)
	fxbFC := readFeatureCollectionForTest(t, fxbPath)
	curbFC := readFeatureCollectionForTest(t, curbPath)
	centerlineFC := readFeatureCollectionForTest(t, centerlinePath)
	enrichedLineFC := enrichLineRoadIDsForTest(t, lineFC, roadFC)
	enrichedExpansionJointFC := enrichLineRoadIDsForTest(t, expansionJointFC, roadFC)
	enrichedFxbFC := enrichLineRoadIDsForTest(t, fxbFC, roadFC)
	enrichedCurbFC := enrichLineRoadIDsForTest(t, curbFC, roadFC)
	t.Logf("phase=read_geojson elapsed=%s", time.Since(start))

	phase := time.Now()
	basePoint, region, err := calcTileContextFromFeatureCollections(roadFC, greenbeltFC, medianIslandFC, markFC, lineFC, expansionJointFC, fxbFC, curbFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}
	t.Logf("phase=calc_context elapsed=%s", time.Since(phase))

	phase = time.Now()
	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-surface-solid-dashed-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}
	t.Logf("phase=new_builder elapsed=%s", time.Since(phase))

	phase = time.Now()
	builder.AddCenterlineFeatures(buildRoadCenterlineFeaturesForTest(centerlineFC)...)
	builder.AddSurfaceFeatures(buildRoadSurfaceFeaturesForTest(roadFC, texturePath)...)
	builder.AddSurfaceFeatures(buildRoadGreenbeltFeaturesForTest(greenbeltFC, greenbeltTexturePath)...)
	builder.AddSurfaceFeatures(buildRoadMedianIslandFeaturesForTest(medianIslandFC, medianIslandTexturePath)...)
	builder.AddSurfaceFeatures(buildRoadMarkSurfaceFeaturesForTest(markFC)...)
	builder.AddLineFeatures(buildRoadMarkingFeaturesForTest(enrichedLineFC)...)
	builder.AddLineFeatures(buildRoadExpansionJointFeaturesForTest(enrichedExpansionJointFC, expansionJointTexturePath)...)
	builder.AddLineFeatures(buildAntiGlareBoardFeaturesForTest(enrichedFxbFC, fxbModelPath, 1.0)...)
	builder.AddLineFeatures(buildRoadCurbFeaturesForTest(enrichedCurbFC, curbTexturePath)...)
	t.Logf("phase=prepare_features elapsed=%s", time.Since(phase))

	phase = time.Now()
	doc, primitives, err := builder.BuildDocument()
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	t.Logf("phase=build_document elapsed=%s primitives=%d materials=%d meshes=%d nodes=%d",
		time.Since(phase), len(primitives), len(doc.Materials), len(doc.Meshes), len(doc.Nodes))

	phase = time.Now()
	appendPrimitivesAsMesh(doc, primitives)
	if err := CompactDocument(doc); err != nil {
		t.Fatalf("compact document: %v", err)
	}
	t.Logf("phase=compact_document elapsed=%s", time.Since(phase))

	phase = time.Now()
	buff := new(bytes.Buffer)
	enc := gltf.NewEncoder(buff)
	enc.AsBinary = true
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("encode glb: %v", err)
	}
	glb := buff.Bytes()
	t.Logf("phase=encode_glb elapsed=%s bytes=%d", time.Since(phase), len(glb))
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	phase = time.Now()
	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-road-solid-dashed", glb, basePoint, true)
	t.Logf("phase=write_outputs elapsed=%s total=%s", time.Since(phase), time.Since(start))
}

func TestRoadSubgradeOnlyToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	subgradePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_lj.geojson"))
	subgradeTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lj.jpeg"))

	subgradeFC := readFeatureCollectionForTest(t, subgradePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(subgradeFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-subgrade-only-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddSurfaceFeatures(buildRoadSubgradeFeaturesForTest(subgradeFC, subgradeTexturePath)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-subgrade-only", glb, basePoint, false)
}

func TestRoadSubgradeOnlyExperimentalToGLBAndTileset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	subgradePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_lj.geojson"))
	subgradeTexturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lj.jpeg"))

	subgradeFC := readFeatureCollectionForTest(t, subgradePath)
	basePoint, region, err := calcTileContextFromFeatureCollections(subgradeFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-subgrade-only-experimental-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	doc := gltf.NewDocument()
	runtime, err := builder.newRuntime(doc)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	metadata := NewFeatureMetadataBuilder(doc)

	primitives := make([]*gltf.Primitive, 0, len(subgradeFC.Features))
	features := buildRoadSubgradeFeaturesForTest(subgradeFC, subgradeTexturePath)
	for i := range features {
		roadID := featureFieldInt64(features[i].Fields, "road_id")
		if roadID == 0 {
			roadID = featureFieldInt64(features[i].Fields, "id")
		}
		rings, err := runtime.Coordinates.ToLocalSurface(features[i])
		if err != nil {
			t.Fatalf("to local surface %d: %v", i, err)
		}
		materials, err := runtime.Materials.ResolveMaterialSet(features[i].Material)
		if err != nil {
			t.Fatalf("resolve material %d: %v", i, err)
		}
		for _, ring := range rings {
			pos, idx, ok := TriangulateSubgradeExperimentalAdaptiveForTest(ring, roadID)
			if !ok || len(idx) < 3 {
				cleanRing := common.CleanSubgradeLocalRingsForTest(ring, 0.01)
				pos, idx, ok = TriangulateSubgradeExperimentalAdaptiveForTest(cleanRing, roadID)
			}
			if !ok || len(idx) < 3 {
				var fallbackErr error
				pos, idx, fallbackErr = TriangulatePlainSurfaceForTest(ring)
				if fallbackErr != nil {
					t.Fatalf("experimental triangulate %d: %v", i, fallbackErr)
				}
			}
			top, err := BuildPlanarSurfacePrimitiveMetersForTest(doc, pos, idx, materials.Top, features[i].UV.RepeatX, features[i].UV.RepeatY, features[i].UV.FlipV)
			if err != nil {
				t.Fatalf("build planar primitive %d: %v", i, err)
			}
			featureIDs := metadata.RegisterFeatureRows(features[i].Features, features[i].Fields)
			if len(featureIDs) > 0 {
				metadata.AttachPrimitiveFeatureID(top, len(pos), featureIDs[0])
			}
			primitives = append(primitives, top)
		}
	}

	if len(primitives) == 0 {
		t.Fatal("generated experimental subgrade glb is empty")
	}

	if err := metadata.Finalize(); err != nil {
		t.Fatalf("finalize experimental metadata: %v", err)
	}
	glb, err := mgltf.BytesForPrimitive(primitives, doc)
	if err != nil {
		t.Fatalf("build experimental glb: %v", err)
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-subgrade-only-experimental", glb, basePoint, false)
}

func TestRoadSurfaceSolidAndDashedMarkingsToGLBAndTileset_WithEstimatedCenterlines(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	roadPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_face_wgs84.geojson"))
	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lm.jpeg"))

	roadFC := readFeatureCollectionForTest(t, roadPath)
	lineFC := readFeatureCollectionForTest(t, linePath)
	enrichedLineFC := enrichLineRoadIDsForTest(t, lineFC, roadFC)

	basePoint, region, err := calcTileContextFromFeatureCollections(roadFC, lineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	surfaceFeatures := buildRoadSurfaceFeaturesForTest(roadFC, texturePath)
	estimatedCenterlines := buildEstimatedRoadCenterlineFeaturesForTest(t, surfaceFeatures)

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-surface-solid-dashed-estimated-centerline-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddCenterlineFeatures(estimatedCenterlines...)
	builder.AddSurfaceFeatures(surfaceFeatures...)
	markingFeatures := buildRoadMarkingFeaturesForTest(enrichedLineFC)
	markLineFeaturesLowCost(markingFeatures)
	builder.AddLineFeatures(markingFeatures...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	writeRoadTileOutputs(t, outDir, "roadsurface-road-solid-dashed-pca-centerline", glb, basePoint, false)
}

func TestRoadSurfaceBuildBreakdownByPlugin(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	roadPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_face_wgs84.geojson"))
	greenbeltPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_fgd_lhm.geojson"))
	medianIslandPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_fgd_lm.geojson"))
	markPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_mark.geojson"))
	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	expansionJointPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_ssf.geojson"))
	fxbPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line_fxb.geojson"))
	curbPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_lys.geojson"))
	centerlinePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lm.jpeg"))
	greenbeltTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lhm.jpg"))
	medianIslandTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lj.jpeg"))
	expansionJointTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "ssf_01.png"))
	curbTexturePath, _ := filepath.Abs(filepath.Join(`../resources/road`, "lys.png"))
	fxbModelPath, _ := filepath.Abs(filepath.Join(`../resources/model`, "fxb_02.glb"))

	roadFC := readFeatureCollectionForTest(t, roadPath)
	greenbeltFC := readFeatureCollectionForTest(t, greenbeltPath)
	medianIslandFC := readFeatureCollectionForTest(t, medianIslandPath)
	markFC := readFeatureCollectionForTest(t, markPath)
	lineFC := readFeatureCollectionForTest(t, linePath)
	expansionJointFC := readFeatureCollectionForTest(t, expansionJointPath)
	fxbFC := readFeatureCollectionForTest(t, fxbPath)
	curbFC := readFeatureCollectionForTest(t, curbPath)
	centerlineFC := readFeatureCollectionForTest(t, centerlinePath)
	enrichedLineFC := enrichLineRoadIDsForTest(t, lineFC, roadFC)
	enrichedExpansionJointFC := enrichLineRoadIDsForTest(t, expansionJointFC, roadFC)
	enrichedFxbFC := enrichLineRoadIDsForTest(t, fxbFC, roadFC)
	enrichedCurbFC := enrichLineRoadIDsForTest(t, curbFC, roadFC)

	basePoint, region, err := calcTileContextFromFeatureCollections(roadFC, greenbeltFC, medianIslandFC, markFC, lineFC, expansionJointFC, fxbFC, curbFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	surfaceFeatures := buildRoadSurfaceFeaturesForTest(roadFC, texturePath)
	greenbeltFeatures := buildRoadGreenbeltFeaturesForTest(greenbeltFC, greenbeltTexturePath)
	medianIslandFeatures := buildRoadMedianIslandFeaturesForTest(medianIslandFC, medianIslandTexturePath)
	markSurfaceFeatures := buildRoadMarkSurfaceFeaturesForTest(markFC)
	markingFeatures := buildRoadMarkingFeaturesForTest(enrichedLineFC)
	markLineFeaturesLowCost(markingFeatures)
	expansionJointFeatures := buildRoadExpansionJointFeaturesForTest(enrichedExpansionJointFC, expansionJointTexturePath)
	markLineFeaturesLowCost(expansionJointFeatures)
	antiGlareFeatures := buildAntiGlareBoardFeaturesForTest(enrichedFxbFC, fxbModelPath, 1.0)
	curbFeatures := buildRoadCurbFeaturesForTest(enrichedCurbFC, curbTexturePath)
	centerlines := buildRoadCenterlineFeaturesForTest(centerlineFC)

	type caseSpec struct {
		name     string
		center   []CenterlineFeature
		surfaces []SurfaceFeature
		lines    []LineFeature
	}
	cases := []caseSpec{
		//{name: "surface-only", surfaces: surfaceFeatures},
		//{name: "surface+marking", surfaces: surfaceFeatures, lines: markingFeatures},
		//{name: "surface+expansion_joint", surfaces: surfaceFeatures, lines: expansionJointFeatures},
		//{name: "surface+curb", surfaces: surfaceFeatures, lines: curbFeatures},
		//{name: "surface+anti_glare", surfaces: surfaceFeatures, lines: antiGlareFeatures},
		{name: "full", center: centerlines, surfaces: append(append(append(append([]SurfaceFeature{}, surfaceFeatures...), greenbeltFeatures...), medianIslandFeatures...), markSurfaceFeatures...), lines: append(append(append([]LineFeature{}, markingFeatures...), expansionJointFeatures...), append(antiGlareFeatures, curbFeatures...)...)},
	}

	for _, tc := range cases {
		builder, err := NewRoadTileBuilder(TileContext{
			TileID:    "road-build-breakdown-" + tc.name,
			Region:    region,
			Center:    [3]float64{basePoint[0], basePoint[1], 0},
			BasePoint: basePoint,
			SRID:      4326,
			UseENU:    true,
		}, nil)
		if err != nil {
			t.Fatalf("%s new road tile builder: %v", tc.name, err)
		}
		if len(tc.center) > 0 {
			builder.AddCenterlineFeatures(tc.center...)
		}
		if len(tc.surfaces) > 0 {
			builder.AddSurfaceFeatures(tc.surfaces...)
		}
		if len(tc.lines) > 0 {
			builder.AddLineFeatures(tc.lines...)
		}

		start := time.Now()
		doc, primitives, err := builder.BuildDocument()
		if err != nil {
			t.Fatalf("%s build document: %v", tc.name, err)
		}
		buildDocElapsed := time.Since(start)

		start = time.Now()
		appendPrimitivesAsMesh(doc, primitives)
		if err := CompactDocument(doc); err != nil {
			t.Fatalf("%s compact document: %v", tc.name, err)
		}
		compactElapsed := time.Since(start)

		start = time.Now()
		buff := new(bytes.Buffer)
		enc := gltf.NewEncoder(buff)
		enc.AsBinary = true
		if err := enc.Encode(doc); err != nil {
			t.Fatalf("%s encode glb: %v", tc.name, err)
		}
		encodeElapsed := time.Since(start)

		t.Logf("case=%s build_document=%s compact=%s encode=%s primitives=%d materials=%d meshes=%d nodes=%d bytes=%d",
			tc.name, buildDocElapsed, compactElapsed, encodeElapsed, len(primitives), len(doc.Materials), len(doc.Meshes), len(doc.Nodes), buff.Len())

		if tc.name == "full" {
			glb, err := builder.BuildBinary()
			if err != nil {
				t.Fatalf("build binary: %v", err)
			}
			if len(glb) == 0 {
				t.Fatalf("generated glb is empty")
			}

			writeRoadTileOutputs(t, "./test", "roadsurface-road-full", glb, basePoint, true)
		}
	}
}

func TestRoadSurfaceAndSingleMarking14011003011ToGLB(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	roadPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_face_wgs84.geojson"))
	linePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	centerlinePath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_line.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lm.jpeg"))

	roadFC := readFeatureCollectionForTest(t, roadPath)
	lineFC := readFeatureCollectionForTest(t, linePath)
	centerlineFC := readFeatureCollectionForTest(t, centerlinePath)

	var targetLineFeatures []*geojson.Feature
	var targetRoadID int64
	for i := range lineFC.Features {
		props := lineFC.Features[i].Properties
		if featureFieldsFromGeoJSON(props)["id"] == float64(14011003011) {
			targetLineFeatures = append(targetLineFeatures, lineFC.Features[i])
			targetRoadID = featureFieldInt64(featureFieldsFromGeoJSON(props), "ldid")
		}
	}
	if len(targetLineFeatures) == 0 {
		t.Fatal("target marking id=14011003011 not found")
	}

	filteredRoadFC := &geojson.FeatureCollection{}
	for i := range roadFC.Features {
		props := featureFieldsFromGeoJSON(roadFC.Features[i].Properties)
		if featureFieldInt64(props, "road_id") == targetRoadID {
			filteredRoadFC.Features = append(filteredRoadFC.Features, roadFC.Features[i])
		}
	}
	if len(filteredRoadFC.Features) == 0 {
		t.Fatalf("no road surface found for ldid=%d", targetRoadID)
	}

	filteredLineFC := &geojson.FeatureCollection{Features: targetLineFeatures}
	filteredCenterlineFC := &geojson.FeatureCollection{}
	for i := range centerlineFC.Features {
		props := featureFieldsFromGeoJSON(centerlineFC.Features[i].Properties)
		if featureFieldInt64(props, "hroad_id") == targetRoadID {
			filteredCenterlineFC.Features = append(filteredCenterlineFC.Features, centerlineFC.Features[i])
		}
	}
	basePoint, region, err := calcTileContextFromFeatureCollections(filteredRoadFC, filteredLineFC)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	builder, err := NewRoadTileBuilder(TileContext{
		TileID:    "road-single-line-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddCenterlineFeatures(buildRoadCenterlineFeaturesForTest(filteredCenterlineFC)...)
	builder.AddSurfaceFeatures(buildRoadSurfaceFeaturesForTest(filteredRoadFC, texturePath)...)
	builder.AddLineFeatures(buildRoadMarkingFeaturesForTest(filteredLineFC)...)

	glb, err := builder.BuildBinary()
	if err != nil {
		t.Fatalf("build binary: %v", err)
	}
	if len(glb) == 0 {
		t.Fatalf("generated glb is empty")
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	out := filepath.Join(outDir, "./test/roadsurface-single-marking-14011003011.glb")
	if err := os.WriteFile(out, glb, 0o644); err != nil {
		t.Fatalf("write glb: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat glb: %v", err)
	}
	if st.Size() == 0 {
		t.Fatalf("glb is empty: %s", out)
	}
	t.Logf("generated glb: %s (%d bytes), road_id=%d", out, st.Size(), targetRoadID)
}

func TestRoadSurface14241000124_CDTToGLB(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	roadPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_face_wgs84.geojson"))
	texturePath, _ := filepath.Abs(filepath.Join(`..\resources\road`, "lm.jpeg"))
	roadFC := readFeatureCollectionForTest(t, roadPath)

	const targetRoadID = int64(14241000124)
	var targetFeature *geojson.Feature
	for _, f := range roadFC.Features {
		if f == nil || f.Geometry == nil {
			continue
		}
		if featureFieldInt64(featureFieldsFromGeoJSON(f.Properties), "road_id") == targetRoadID {
			targetFeature = f
			break
		}
	}
	if targetFeature == nil {
		t.Fatalf("target road not found: %d", targetRoadID)
	}

	surface := buildRoadSurfaceFeaturesForTest(&geojson.FeatureCollection{Features: []*geojson.Feature{targetFeature}}, texturePath)
	if len(surface) != 1 {
		t.Fatalf("expected one target surface")
	}
	basePoint, region, err := calcTileContextFromFeatureCollections(&geojson.FeatureCollection{Features: []*geojson.Feature{targetFeature}})
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}
	pipeline, err := NewCoordinatePipeline(TileContext{
		TileID:    "road-surface-14241000124-cdt",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}
	localRings, err := pipeline.ToLocalSurface(surface[0])
	if err != nil {
		t.Fatalf("to local surface: %v", err)
	}
	if len(localRings) == 0 {
		t.Fatal("no local rings")
	}
	pos, idx, okTri, err := TriangulateSurfaceByCDTExperimentalForTest(localRings[0])
	if err != nil {
		t.Fatalf("cdt triangulate: %v", err)
	}
	if !okTri {
		t.Fatal("cdt triangulate failed")
	}

	doc := gltf.NewDocument()
	materials := NewMaterialResolver(doc)
	materialSet := surface[0].Material
	materialSet.Top.Path = texturePath
	materialSet.Top.DoubleSided = true
	mi, err := materials.ResolveMaterialSet(materialSet)
	if err != nil {
		t.Fatalf("resolve material: %v", err)
	}
	prim, err := BuildPlanarSurfacePrimitiveMetersForTest(doc, pos, idx, mi.Top, 8, 8, surface[0].UV.FlipV)
	if err != nil {
		t.Fatalf("build primitive: %v", err)
	}
	appendPrimitivesAsMesh(doc, []*gltf.Primitive{prim})
	if err := CompactDocument(doc); err != nil {
		t.Fatalf("compact document: %v", err)
	}
	buff := new(bytes.Buffer)
	enc := gltf.NewEncoder(buff)
	enc.AsBinary = true
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("encode glb: %v", err)
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = filepath.Dir(thisFile)
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	out := filepath.Join(outDir, "roadsurface-14241000124-cdt.glb")
	if err := os.WriteFile(out, buff.Bytes(), 0o644); err != nil {
		t.Fatalf("write glb: %v", err)
	}
}

func calcTileContextFromFeatureCollection(fc *geojson.FeatureCollection) ([]float64, [6]float64, error) {
	return calcTileContextFromFeatureCollections(fc)
}

func calcTileContextFromFeatureCollections(collections ...*geojson.FeatureCollection) ([]float64, [6]float64, error) {
	var region [6]float64
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)

	for _, fc := range collections {
		if fc == nil {
			continue
		}
		for i := range fc.Features {
			g := fc.Features[i].Geometry
			if g == nil {
				continue
			}
			flat := g.FlatCoords()
			stride := g.Stride()
			if stride < 2 {
				continue
			}
			for j := 0; j+stride-1 < len(flat); j += stride {
				lon := flat[j]
				lat := flat[j+1]
				if lon < minLon {
					minLon = lon
				}
				if lon > maxLon {
					maxLon = lon
				}
				if lat < minLat {
					minLat = lat
				}
				if lat > maxLat {
					maxLat = lat
				}
			}
		}
	}
	if math.IsInf(minLon, 0) || math.IsInf(minLat, 0) || math.IsInf(maxLon, 0) || math.IsInf(maxLat, 0) {
		return nil, region, os.ErrInvalid
	}

	basePoint := []float64{(minLon + maxLon) / 2, (minLat + maxLat) / 2, 0}
	region = [6]float64{minLon, minLat, maxLon, maxLat, 0, 0}
	return basePoint, region, nil
}

func readFeatureCollectionForTest(t *testing.T, path string) *geojson.FeatureCollection {
	t.Helper()

	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read geojson %s: %v", path, err)
	}

	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal geojson %s: %v", path, err)
	}
	if len(fc.Features) == 0 {
		t.Fatalf("geojson has no features: %s", path)
	}
	return fc
}

func buildRoadSurfaceFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []SurfaceFeature {
	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "surface"
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_surface"),
			},
			Thickness: 0,
			Material: MaterialSet{
				Top:    MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side:   MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Bottom: MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 8, RepeatY: 8, FlipV: true, ScaleByMeters: true},
		})
	}
	return features
}

func buildRoadSubgradeFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []SurfaceFeature {
	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "subgrade"
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSubgrade,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_subgrade"),
			},
			Thickness: 0,
			Material: MaterialSet{
				Top:    MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side:   MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Bottom: MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 8, RepeatY: 8, FlipV: true, ScaleByMeters: true},
		})
	}
	return features
}

func buildRoadGreenbeltFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []SurfaceFeature {
	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "greenbelt"
		height := featureFieldFloat32(fields, "height")
		if height <= 0 {
			height = featureFieldFloat32(fields, "hight")
		}
		if height <= 0 {
			height = 0.3
		}
		fields["height"] = height
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadRaisedSurface,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_raised_surface_greenbelt"),
			},
			Height: height,
			Material: MaterialSet{
				Top:  MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side: MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 8, RepeatY: 8, FlipV: true, ScaleByMeters: true},
		})
	}
	return features
}

func buildRoadMedianIslandFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []SurfaceFeature {
	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "median_island"
		height := featureFieldFloat32(fields, "height")
		if height <= 0 {
			height = featureFieldFloat32(fields, "hight")
		}
		if height <= 0 {
			height = 0.3
		}
		fields["height"] = height
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadRaisedSurface,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_raised_surface_median_island"),
			},
			Height: height,
			Material: MaterialSet{
				Top:  MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side: MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 8, RepeatY: 8, FlipV: true, ScaleByMeters: true},
		})
	}
	return features
}

func buildRoadMarkSurfaceFeaturesForTest(fc *geojson.FeatureCollection) []SurfaceFeature {
	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "mark_surface"
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadMarkSurface,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_mark_surface"),
			},
			Material: MaterialSet{
				Top: MaterialRef{Name: "road-mark-surface", Color: roadMarkColor(fields), Mode: MaterialModeColor, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildRoadCenterlineFeaturesForTest(fc *geojson.FeatureCollection) []CenterlineFeature {
	features := make([]CenterlineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		// The current test fixture reuses line data where the related road ID is stored in `ldid`.
		// Normalize it so the projector can actually associate the reference line with the road.
		if featureFieldInt64(fields, "hroad_id") == 0 && featureFieldInt64(fields, "road_id") == 0 {
			if ldid := featureFieldInt64(fields, "ldid"); ldid != 0 {
				fields["hroad_id"] = ldid
			}
		}
		features = append(features, CenterlineFeature{
			Geom:   fc.Features[i].Geometry,
			Fields: fields,
		})
	}
	return features
}

func buildEstimatedRoadCenterlineFeaturesForTest(t *testing.T, surfaces []SurfaceFeature) []CenterlineFeature {
	t.Helper()

	features := make([]CenterlineFeature, 0, len(surfaces))
	for i := range surfaces {
		feature, err := EstimateCenterlineFeatureFromSurface(surfaces[i])
		if err != nil {
			t.Fatalf("estimate centerline from surface road_id=%d: %v", featureFieldInt64(surfaces[i].Fields, "road_id"), err)
		}
		features = append(features, feature)
	}
	if len(features) == 0 {
		t.Fatal("estimated centerline features are empty")
	}
	return features
}

func buildRoadMarkingFeaturesForTest(fc *geojson.FeatureCollection) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		width := featureFieldFloat32(fields, "width")
		if width <= 0 {
			width = featureFieldFloat32(fields, "bxkd")
		}
		fields["width"] = width
		pattern := featureFieldString(fields, "dash_pattern")
		if pattern == "" {
			pattern = featureFieldString(fields, "bxbl")
		}
		if pattern != "" {
			fields["dash_pattern"] = pattern
			fields["feature_type"] = "dashed"
		} else {
			fields["feature_type"] = "solid"
		}
		color := featureFieldString(fields, "color")
		if color == "" {
			color = featureFieldString(fields, "ys")
			if color != "" {
				fields["color"] = color
			}
		}
		if len(color) == 0 {
			color = "#FFFFFF"
		}
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadMarking,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, markingTypeName(fields)),
			},
			Width:  width,
			Height: 0.05,
			Material: MaterialSet{
				Top: MaterialRef{Name: "road-marking", Color: color, Mode: MaterialModeColor, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingStrip, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildRoadExpansionJointFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "expansion_joint"
		fields["width"] = float32(0.5)
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadExpansionJoint,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_expansion_joint"),
			},
			Width:  0.5,
			Height: 0.01,
			Material: MaterialSet{
				Top: MaterialRef{
					Name:        "road-expansion-joint",
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
			},
			UV: UVOptions{Mapping: UVMappingStrip, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildRoadCurbFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadCurb,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "路缘石"),
			},
			Width:  0.13,
			Height: 0.15,
			Material: MaterialSet{
				Top: MaterialRef{
					Name:        "road-curb-top",
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
				Side: MaterialRef{
					Name:        "road-curb-side",
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
				Bottom: MaterialRef{
					Name:        "road-curb-bottom",
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
			},
			UV: UVOptions{
				Mapping: UVMappingStrip,
				RepeatX: 1,
				RepeatY: 1,
				FlipV:   false,
			},
		})
	}
	return features
}

func buildRoadTollIslandFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadTollIsland,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "收费岛"),
			},
			Width:  featureFieldFloat32(fields, "width"),
			Height: featureFieldFloat32(fields, "height"),
			Material: MaterialSet{
				Top: MaterialRef{
					Name:        "road-toll-island-top",
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
				Side: MaterialRef{
					Name:        "road-toll-island-side",
					Path:        texturePath,
					Mode:        MaterialModeTexture,
					DoubleSided: true,
				},
			},
			UV: UVOptions{
				Mapping: UVMappingStrip,
				RepeatX: 4,
				RepeatY: 4,
				FlipV:   false,
			},
		})
	}
	return features
}

func buildNewJerseyBarrierFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "new_jersey"
		width := featureFieldFloat32(fields, "width")
		height := featureFieldFloat32(fields, "height")
		fields["width"] = width
		fields["height"] = height
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeBarrierRigid,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "barrier_rigid_new_jersey"),
			},
			Width:  width,
			Height: height,
			Material: MaterialSet{
				Top:    MaterialRef{Name: "new-jersey-top", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side:   MaterialRef{Name: "new-jersey-side", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Bottom: MaterialRef{Name: "new-jersey-bottom", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildConcreteWallFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "concrete_wall"
		width := featureFieldFloat32(fields, "width")
		height := featureFieldFloat32(fields, "height")
		fields["width"] = width
		fields["height"] = height
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeBarrierRigid,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "barrier_rigid_concrete_wall"),
			},
			Width:  width,
			Height: height,
			Material: MaterialSet{
				Top:    MaterialRef{Name: "concrete-wall-top", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side:   MaterialRef{Name: "concrete-wall-side", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Bottom: MaterialRef{Name: "concrete-wall-bottom", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildGuardrailTwoWaveFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "wave_two"
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeBarrierWave,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "barrier_wave_two"),
			},
			Material: MaterialSet{
				Top:  MaterialRef{Name: "guardrail-three-wave-rail", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side: MaterialRef{Name: "guardrail-two-wave-support", Color: "#8D949C", Mode: MaterialModeColor, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildGuardrailThreeWaveFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "wave_three"
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeBarrierWave,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "barrier_wave_three"),
			},
			Material: MaterialSet{
				Top:  MaterialRef{Name: "guardrail-three-wave-rail", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side: MaterialRef{Name: "guardrail-three-wave-support", Color: "#8D949C", Mode: MaterialModeColor, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildGuardrailNoseEndFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "wave_nose_end"
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeBarrierWave,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "barrier_wave_nose_end"),
			},
			Material: MaterialSet{
				Top: MaterialRef{Name: "guardrail-two-wave-rail", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 1, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildNoiseWallFeaturesForTest(fc *geojson.FeatureCollection, texturePath string) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "noise_wall"
		height := featureFieldFloat32(fields, "height")
		if height > 0 {
			fields["height"] = height
		}
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeBarrierNoiseWall,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "barrier_noise_wall"),
			},
			Height:    height,
			Width:     featureFieldFloat32(fields, "width"),
			Thickness: featureFieldFloat32(fields, "thickness"),
			Material: MaterialSet{
				Top: MaterialRef{Name: "noise-wall-panel", Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 2, RepeatY: 1, FlipV: false},
		})
	}
	return features
}

func buildAntiGlareBoardFeaturesForTest(fc *geojson.FeatureCollection, modelPath string, spacing float32) []LineFeature {
	features := make([]LineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "anti_glare_board"
		fields["model_path"] = modelPath
		fields["spacing"] = spacing
		features = append(features, LineFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeLinearInstancedModel,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "linear_instanced_model_anti_glare_board"),
			},
		})
	}
	return features
}

type roadSurfaceOutlineForTest struct {
	roadID int64
	outer  [][2]float64
	minX   float64
	maxX   float64
	minY   float64
	maxY   float64
}

func enrichLineRoadIDsForTest(t *testing.T, lineFC, roadFC *geojson.FeatureCollection) *geojson.FeatureCollection {
	t.Helper()
	if lineFC == nil || roadFC == nil {
		return lineFC
	}
	index := buildRoadSurfaceOutlineIndexForTest(roadFC)
	for i := range lineFC.Features {
		if lineFC.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(lineFC.Features[i].Properties)
		if featureFieldInt64(fields, "ldid") != 0 {
			continue
		}
		pt, ok := lineRepresentativePointForTest(lineFC.Features[i].Geometry)
		if !ok {
			continue
		}
		if roadID, ok := lookupRoadIDForPointForTest(pt, index); ok {
			if lineFC.Features[i].Properties == nil {
				lineFC.Features[i].Properties = map[string]interface{}{}
			}
			lineFC.Features[i].Properties["ldid"] = roadID
		}
	}
	return lineFC
}

func buildRoadSurfaceOutlineIndexForTest(fc *geojson.FeatureCollection) []roadSurfaceOutlineForTest {
	out := make([]roadSurfaceOutlineForTest, 0, len(fc.Features))
	for i := range fc.Features {
		g := fc.Features[i].Geometry
		if g == nil {
			continue
		}
		roadID := featureFieldInt64(featureFieldsFromGeoJSON(fc.Features[i].Properties), "road_id")
		if roadID == 0 {
			roadID = featureFieldInt64(featureFieldsFromGeoJSON(fc.Features[i].Properties), "id")
		}
		if roadID == 0 {
			continue
		}
		polys := polygonOutlinesForTest(g)
		for _, outer := range polys {
			if len(outer) < 3 {
				continue
			}
			minX, maxX := outer[0][0], outer[0][0]
			minY, maxY := outer[0][1], outer[0][1]
			for _, p := range outer[1:] {
				if p[0] < minX {
					minX = p[0]
				}
				if p[0] > maxX {
					maxX = p[0]
				}
				if p[1] < minY {
					minY = p[1]
				}
				if p[1] > maxY {
					maxY = p[1]
				}
			}
			out = append(out, roadSurfaceOutlineForTest{
				roadID: roadID,
				outer:  outer,
				minX:   minX,
				maxX:   maxX,
				minY:   minY,
				maxY:   maxY,
			})
		}
	}
	return out
}

func lookupRoadIDForPointForTest(pt [2]float64, outlines []roadSurfaceOutlineForTest) (int64, bool) {
	for i := range outlines {
		if pt[0] < outlines[i].minX || pt[0] > outlines[i].maxX || pt[1] < outlines[i].minY || pt[1] > outlines[i].maxY {
			continue
		}
		if pointInRingForTest(pt, outlines[i].outer) {
			return outlines[i].roadID, true
		}
	}
	return 0, false
}

func polygonOutlinesForTest(g geom.T) [][][2]float64 {
	switch gg := g.(type) {
	case *geom.Polygon:
		return polygonOutlinesFromPolygonForTest(gg)
	case *geom.MultiPolygon:
		out := make([][][2]float64, 0, gg.NumPolygons())
		for i := 0; i < gg.NumPolygons(); i++ {
			out = append(out, polygonOutlinesFromPolygonForTest(gg.Polygon(i))...)
		}
		return out
	default:
		return nil
	}
}

func polygonOutlinesFromPolygonForTest(poly *geom.Polygon) [][][2]float64 {
	if poly == nil {
		return nil
	}
	flat := poly.FlatCoords()
	ends := poly.Ends()
	stride := poly.Stride()
	if len(ends) == 0 {
		return nil
	}
	out := make([][][2]float64, 0, len(ends))
	start := 0
	for _, end := range ends {
		out = append(out, ringXYForTest(flat, start, end, stride))
		start = end
	}
	return out
}

func ringXYForTest(flat []float64, start, end, stride int) [][2]float64 {
	if stride < 2 || end <= start {
		return nil
	}
	n := (end - start) / stride
	if n <= 0 {
		return nil
	}
	ring := make([][2]float64, 0, n)
	for i := start; i+1 < end; i += stride {
		ring = append(ring, [2]float64{flat[i], flat[i+1]})
	}
	if len(ring) > 0 && (ring[0] != ring[len(ring)-1]) {
		ring = append(ring, ring[0])
	}
	return ring
}

func pointInRingForTest(pt [2]float64, ring [][2]float64) bool {
	if len(ring) < 4 {
		return false
	}
	inside := false
	x, y := pt[0], pt[1]
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		xi, yi := ring[i][0], ring[i][1]
		xj, yj := ring[j][0], ring[j][1]
		intersects := ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi+1e-18)+xi)
		if intersects {
			inside = !inside
		}
	}
	return inside
}

func lineRepresentativePointForTest(g geom.T) ([2]float64, bool) {
	switch gg := g.(type) {
	case *geom.LineString:
		return lineStringMidpointForTest(gg)
	case *geom.MultiLineString:
		var best [2]float64
		bestLen := -1.0
		for i := 0; i < gg.NumLineStrings(); i++ {
			pt, ok := lineStringMidpointForTest(gg.LineString(i))
			if !ok {
				continue
			}
			if l := lineStringLengthForTest(gg.LineString(i)); l > bestLen {
				best = pt
				bestLen = l
			}
		}
		return best, bestLen >= 0
	default:
		return [2]float64{}, false
	}
}

func lineStringLengthForTest(line *geom.LineString) float64 {
	if line == nil {
		return 0
	}
	flat := line.FlatCoords()
	stride := line.Stride()
	if len(flat) < stride*2 {
		return 0
	}
	total := 0.0
	for i := stride; i+1 < len(flat); i += stride {
		dx := flat[i] - flat[i-stride]
		dy := flat[i+1] - flat[i-stride+1]
		total += math.Hypot(dx, dy)
	}
	return total
}

func lineStringMidpointForTest(line *geom.LineString) ([2]float64, bool) {
	if line == nil {
		return [2]float64{}, false
	}
	flat := line.FlatCoords()
	stride := line.Stride()
	if len(flat) < stride*2 {
		return [2]float64{}, false
	}
	total := lineStringLengthForTest(line)
	if total <= 0 {
		return [2]float64{flat[0], flat[1]}, true
	}
	half := total * 0.5
	seen := 0.0
	for i := stride; i+1 < len(flat); i += stride {
		ax, ay := flat[i-stride], flat[i-stride+1]
		bx, by := flat[i], flat[i+1]
		seg := math.Hypot(bx-ax, by-ay)
		if seg <= 0 {
			continue
		}
		if seen+seg >= half {
			t := (half - seen) / seg
			return [2]float64{ax + (bx-ax)*t, ay + (by-ay)*t}, true
		}
		seen += seg
	}
	last := len(flat) - stride
	return [2]float64{flat[last], flat[last+1]}, true
}

func markLineFeaturesLowCost(features []LineFeature) {
	for i := range features {
		if features[i].Fields == nil {
			features[i].Fields = FeatureFields{}
		}
		features[i].Fields["low_cost"] = true
	}
}

func featureFieldsFromGeoJSON(props map[string]interface{}) FeatureFields {
	fields := make(FeatureFields, len(props))
	for k, v := range props {
		fields[k] = v
	}
	if _, ok := fields["id"]; !ok {
		fields["id"] = ""
	}
	if _, ok := fields["name"]; !ok {
		fields["name"] = ""
	}
	return fields
}

func roadMarkColor(fields FeatureFields) string {
	switch featureFieldInt64(fields, "color") {
	case 1:
		return "#FF0000"
	case 2:
		return "#87CEEB"
	case 3:
		return "#FFFF00"
	case 4:
		return "#00FF00"
	case 5:
		return "#996600"
	case 6:
		return "#FF681F"
	case 7:
		return "#FFC0CB"
	case 8:
		return "#000000"
	case 9:
		return "#FFFFFF"
	default:
		return "#FFFFFF"
	}
}

func featureRowsFromGeoJSON(props map[string]interface{}, typeName string) []FeatureFields {
	fields := featureFieldsFromGeoJSON(props)
	return []FeatureFields{
		{
			"id":   fields["id"],
			"type": typeName,
		},
	}
}

func markingTypeName(fields FeatureFields) string {
	switch featureFieldString(fields, "feature_type") {
	case "dashed":
		return "road_marking_dashed"
	case "solid", "":
		return "road_marking_solid"
	default:
		return featureFieldString(fields, "feature_type")
	}
}

func writeRoadTileOutputs(t *testing.T, outDir, baseName string, glb []byte, basePoint []float64, writeDefault bool) {
	t.Helper()

	glbPath := filepath.Join(outDir, baseName+".glb")
	if err := os.WriteFile(glbPath, glb, 0o644); err != nil {
		t.Fatalf("write glb: %v", err)
	}
	st, err := os.Stat(glbPath)
	if err != nil {
		t.Fatalf("stat glb: %v", err)
	}
	if st.Size() == 0 {
		t.Fatalf("glb is empty: %s", glbPath)
	}

	tilesetPath := filepath.Join(outDir, baseName+".tileset.json")
	if err := WriteTilesetJSONForGLB(TilesetBuildOptions{
		GLBPath:        glbPath,
		OutputPath:     tilesetPath,
		ContentURI:     filepath.Base(glbPath),
		BasePoint:      basePoint,
		GeometricError: 100,
		PaddingXY:      12,
		PaddingY:       6,
		MinPaddingXZ:   12,
		MinPaddingY:    6,
		PaddingRatioXZ: 0.03,
		PaddingRatioY:  0.2,
	}); err != nil {
		t.Fatalf("write tileset json: %v", err)
	}
	if _, err := os.Stat(tilesetPath); err != nil {
		t.Fatalf("stat tileset json: %v", err)
	}
	defaultTilesetPath := filepath.Join(outDir, "tileset.json")
	if writeDefault {
		if err := WriteTilesetJSONForGLB(TilesetBuildOptions{
			GLBPath:        glbPath,
			OutputPath:     defaultTilesetPath,
			ContentURI:     filepath.Base(glbPath),
			BasePoint:      basePoint,
			GeometricError: 100,
			PaddingXY:      12,
			PaddingY:       6,
			MinPaddingXZ:   12,
			MinPaddingY:    6,
			PaddingRatioXZ: 0.03,
			PaddingRatioY:  0.2,
		}); err != nil {
			t.Fatalf("write default tileset json: %v", err)
		}
		if _, err := os.Stat(defaultTilesetPath); err != nil {
			t.Fatalf("stat default tileset json: %v", err)
		}
	}

	t.Logf("generated glb: %s (%d bytes)", glbPath, st.Size())
	t.Logf("generated tileset: %s", tilesetPath)
	if writeDefault {
		t.Logf("generated default tileset: %s", defaultTilesetPath)
	}
}

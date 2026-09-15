package traffic_feature

import (
	"os"
	"path/filepath"
	"testing"

	"cesium-tileset-tool/config"
)

func TestTileModelRelativePath(t *testing.T) {
	tests := []struct {
		geohash string
		lod     int
		want    string
	}{
		{geohash: "wtw", lod: 0, want: "tiles/w/wtw/lod0.glb"},
		{geohash: "wtw3", lod: 1, want: "tiles/wt/wtw3/lod1.glb"},
		{geohash: "wtw3s", lod: 2, want: "tiles/wt/wtw/wtw3s/lod2.glb"},
		{geohash: "wtw3sjq", lod: 3, want: "tiles/wt/wtw3/wtw3s/wtw3sjq/lod3.glb"},
		{geohash: "wtw3sjq9", lod: 3, want: "tiles/wt/wtw3/wtw3sj/wtw3sjq9/lod3.glb"},
	}

	for _, tt := range tests {
		t.Run(tt.geohash, func(t *testing.T) {
			if got := tileModelRelativePath(tt.geohash, tt.lod); got != tt.want {
				t.Fatalf("tileModelRelativePath(%q, %d) = %q, want %q", tt.geohash, tt.lod, got, tt.want)
			}
		})
	}
}

func TestGeoHashModelGroupKeyIncludesTableName(t *testing.T) {
	first := &GeoHashModel{TableName: "hdtraffic_sign", Model: "shared.glb"}
	second := &GeoHashModel{TableName: "hdpole", Model: "shared.glb"}
	models := make(map[string][]*GeoHashModel)
	appendGeoHashModelsByGroup(models, first, second)

	if len(models) != 2 {
		t.Fatalf("models with the same file name from different tables must use separate groups: %+v", models)
	}
	if len(models["hdtraffic_sign|shared.glb"]) != 1 || len(models["hdpole|shared.glb"]) != 1 {
		t.Fatal("unexpected TableName|Model grouping keys")
	}
}

func TestEnsureLocalLODModelDirectories(t *testing.T) {
	cfg := &config.Config{NetworkFolder: t.TempDir()}
	lod := config.TilesetLODConfig{LOD3: config.TilesetLODLevelConfig{LocalFirst: true}}
	levels := []tileLODLevel{
		{Level: 0, ModelFolder: "lod0", GeometricError: 200},
		{Level: 1, ModelFolder: "lod1", GeometricError: 80},
		{Level: 2, ModelFolder: "lod2", GeometricError: 25},
		{Level: 3, ModelFolder: "lod3", GeometricError: 0},
	}

	if err := ensureLocalLODModelDirectories(cfg, lod, levels); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"lod0", "lod1", "lod2", "lod3"} {
		info, err := os.Stat(filepath.Join(cfg.NetworkFolder, folder))
		if err != nil || !info.IsDir() {
			t.Fatalf("LOD model folder %q was not created", folder)
		}
	}
}

func TestLoadLODModelsBySourceCopiesLOD3Model(t *testing.T) {
	networkFolder := t.TempDir()
	cfg := &config.Config{NetworkFolder: networkFolder}
	level := tileLODLevel{Level: 2, ModelFolder: "lod2", GeometricError: 25}
	original := &GeoHashModel{
		Id: "1", TableName: "hdtraffic_sign", Model: "sign/simple.glb",
		Gltf: &GltfModel{Name: "sign/simple.glb", Content: []byte("lod3-model")},
	}
	source := make(map[string][]*GeoHashModel)
	appendGeoHashModelsByGroup(source, original)
	geoTable := GeoTable{TilesetSources: map[string]config.TilesetSourceConfig{
		"hdtraffic_sign": {LOD: config.TilesetSourceLODConfig{LOD2ModelMode: "copy-lod3"}},
	}}
	cache := &localLODModelCache{models: make(map[string]*GltfModel)}

	loaded, err := loadLODModelsBySource(cfg, geoTable, level, source, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one copied model group, got %d", len(loaded))
	}
	modelPath := filepath.Join(networkFolder, "lod2", "hdtraffic_sign", "sign", "simple.glb")
	content, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "lod3-model" {
		t.Fatalf("unexpected copied model content %q", content)
	}
}

func TestLoadLODModelsBySourceReusesLOD3WithoutCopy(t *testing.T) {
	networkFolder := t.TempDir()
	cfg := &config.Config{NetworkFolder: networkFolder}
	level := tileLODLevel{Level: 2, ModelFolder: "lod2", GeometricError: 25}
	original := &GeoHashModel{
		Id: "1", TableName: "hdgantry", Model: "gantry.glb",
		Gltf: &GltfModel{Name: "gantry.glb", Content: []byte("lod3-model")},
	}
	source := make(map[string][]*GeoHashModel)
	appendGeoHashModelsByGroup(source, original)
	geoTable := GeoTable{TilesetSources: map[string]config.TilesetSourceConfig{
		"hdgantry": {LOD: config.TilesetSourceLODConfig{LOD2ModelMode: "reuse-lod3"}},
	}}
	cache := &localLODModelCache{models: make(map[string]*GltfModel)}

	loaded, err := loadLODModelsBySource(cfg, geoTable, level, source, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one reused model group, got %d", len(loaded))
	}
	modelPath := filepath.Join(networkFolder, "lod2", "hdgantry", "gantry.glb")
	if _, err := os.Stat(modelPath); !os.IsNotExist(err) {
		t.Fatalf("reuse-lod3 unexpectedly created %s", modelPath)
	}
}

func TestResolveLODModelModeByPrefix(t *testing.T) {
	lod := config.TilesetSourceLODConfig{
		LOD2ModelMode:     "copy-lod3-by-prefix",
		LOD2ModelPrefixes: []string{"sxj_", "device/camera/"},
		LOD2UnmatchedMode: "local",
	}
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{name: "file prefix", modelName: "sxj_camera.glb", want: "copy-lod3"},
		{name: "path prefix", modelName: "device/camera/model.glb", want: "copy-lod3"},
		{name: "windows path", modelName: `device\camera\model.glb`, want: "copy-lod3"},
		{name: "unmatched", modelName: "other/model.glb", want: "local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLODModelMode(lod, 2, tt.modelName)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resolveLODModelMode(%q) = %q, want %q", tt.modelName, got, tt.want)
			}
		})
	}
}

func TestResolveLODModelModeRejectsInvalidUnmatchedMode(t *testing.T) {
	lod := config.TilesetSourceLODConfig{
		LOD2ModelMode:     "copy-lod3-by-prefix",
		LOD2ModelPrefixes: []string{"sxj_"},
		LOD2UnmatchedMode: "invalid",
	}
	if _, err := resolveLODModelMode(lod, 2, "other.glb"); err == nil {
		t.Fatal("expected invalid lod2UnmatchedMode to be rejected")
	}
}

func TestResolveLODModelModeForEveryConfiguredLevel(t *testing.T) {
	lod := config.TilesetSourceLODConfig{
		LOD0ModelMode:     "copy-lod3-by-prefix",
		LOD0ModelPrefixes: []string{"lod0_"},
		LOD0UnmatchedMode: "skip",
		LOD1ModelMode:     "reuse-lod3",
		LOD2ModelMode:     "copy-lod3",
	}
	tests := []struct {
		name      string
		level     int
		modelName string
		want      string
	}{
		{name: "lod0 matching prefix", level: 0, modelName: "lod0_sign.glb", want: "copy-lod3"},
		{name: "lod0 unmatched", level: 0, modelName: "sign.glb", want: "skip"},
		{name: "lod1", level: 1, modelName: "sign.glb", want: "reuse-lod3"},
		{name: "lod2", level: 2, modelName: "sign.glb", want: "copy-lod3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLODModelMode(lod, tt.level, tt.modelName)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("LOD%d mode = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

func TestResolveLODModelModeDefaultsToLocal(t *testing.T) {
	for level := 0; level <= 2; level++ {
		got, err := resolveLODModelMode(config.TilesetSourceLODConfig{}, level, "model.glb")
		if err != nil {
			t.Fatal(err)
		}
		if got != "local" {
			t.Fatalf("LOD%d default mode = %q, want local", level, got)
		}
	}
}

func TestEnsureLocalLODModelDirectoriesRejectsInvalidErrors(t *testing.T) {
	cfg := &config.Config{NetworkFolder: t.TempDir()}
	lod := config.TilesetLODConfig{}
	levels := []tileLODLevel{
		{Level: 0, ModelFolder: "lod0", GeometricError: 80},
		{Level: 1, ModelFolder: "lod1", GeometricError: 80},
		{Level: 3, GeometricError: 0},
	}

	if err := ensureLocalLODModelDirectories(cfg, lod, levels); err == nil {
		t.Fatal("expected equal geometricError values to be rejected")
	}
}

func TestBuildLODNodeChain(t *testing.T) {
	transform := [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	region := [6]float64{121.0, 31.0, 121.001, 31.001, 10, 20}
	lods := []builtTileLOD{
		{level: tileLODLevel{Level: 0, GeometricError: 200}, uri: "lod0.glb", built: &BuildModel{Region: region, Transform: transform}},
		{level: tileLODLevel{Level: 2, GeometricError: 25}, uri: "lod2.glb", built: &BuildModel{Region: region, Transform: transform}},
	}

	root := buildLODNodeChain(&GeoHashTile{Geohash: "wtw3", Level: 4}, lods, "20260911")
	if root == nil || root.Content == nil || root.Content.Uri != "lod0.glb?t=20260911" {
		t.Fatal("LOD0 root node was not generated")
	}
	if root.Refine != "REPLACE" || root.GeometricError != 200 || root.Transform == nil {
		t.Fatal("LOD0 root node has invalid refinement properties")
	}
	if len(root.Children) != 1 {
		t.Fatal("expected one child LOD node")
	}
	leaf := root.Children[0]
	if leaf.Content == nil || leaf.Content.Uri != "lod2.glb?t=20260911" {
		t.Fatal("LOD2 leaf node was not generated")
	}
	if leaf.Refine != "REPLACE" || leaf.GeometricError != 0 || leaf.Transform != nil {
		t.Fatal("deepest available LOD must be a zero-error leaf inheriting its transform")
	}
}

func TestGeohashGeometricErrorUsesConfiguredLODBase(t *testing.T) {
	lod := config.TilesetLODConfig{
		Enabled: true,
		LOD0:    config.TilesetLODLevelConfig{ModelFolder: "lod0", GeometricError: 100},
		LOD1:    config.TilesetLODLevelConfig{ModelFolder: "lod1", GeometricError: 40},
		LOD2:    config.TilesetLODLevelConfig{ModelFolder: "lod2", GeometricError: 16},
		LOD3:    config.TilesetLODLevelConfig{GeometricError: 0},
	}
	levels := configuredTileLODLevels(lod)
	if len(levels) != 4 || levels[0].GeometricError != 100 || levels[1].GeometricError != 40 || levels[2].GeometricError != 16 || levels[3].GeometricError != 0 {
		t.Fatalf("unexpected configured LOD levels: %+v", levels)
	}
	if got := geohashGeometricErrorBase(levels); got != 100 {
		t.Fatalf("geohashGeometricErrorBase() = %v, want 100", got)
	}

	box := BoundingVolume{Box: [12]float64{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}}
	lod0 := &TileNode{
		BoundingVolume: box,
		Content:        &TileContent{Uri: "lod0.glb"},
		GeometricError: 100,
		Refine:         "REPLACE",
	}
	geohashParent := &TileNode{
		BoundingVolume: box,
		Children:       []*TileNode{lod0},
		Refine:         "ADD",
	}
	root := &TileNode{
		BoundingVolume: box,
		Children:       []*TileNode{geohashParent},
		Refine:         "ADD",
	}

	refreshGeoHashTileBound(root, geohashGeometricErrorBase(levels))

	if geohashParent.GeometricError != 200 {
		t.Fatalf("Geohash parent geometricError = %v, want 200", geohashParent.GeometricError)
	}
	if root.GeometricError != 400 {
		t.Fatalf("root geometricError = %v, want 400", root.GeometricError)
	}
	if lod0.GeometricError != 100 {
		t.Fatalf("configured LOD0 geometricError changed to %v", lod0.GeometricError)
	}
}

func TestGeohashGeometricErrorBaseFallsBackForZeroErrorLOD(t *testing.T) {
	levels := []tileLODLevel{{Level: 3, GeometricError: 0}}
	if got := geohashGeometricErrorBase(levels); got != 1 {
		t.Fatalf("geohashGeometricErrorBase() = %v, want 1", got)
	}
}

func TestLoadLocalLODModelsUsesDatabaseModelPath(t *testing.T) {
	networkFolder := t.TempDir()
	modelPath := filepath.Join(networkFolder, "lod1", "bridge", "simple.glb")
	if err := os.MkdirAll(filepath.Dir(modelPath), 0755); err != nil {
		t.Fatal(err)
	}
	wantContent := []byte("local-lod-model")
	if err := os.WriteFile(modelPath, wantContent, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{NetworkFolder: networkFolder}
	level := tileLODLevel{Level: 1, ModelFolder: "lod1", GeometricError: 80}
	original := &GeoHashModel{Id: "1", TableName: "hdgantry", Model: "bridge/simple.glb", Gltf: &GltfModel{Content: []byte("minio-lod3")}}
	groupKey := geoHashModelGroupKey(original)
	source := map[string][]*GeoHashModel{groupKey: []*GeoHashModel{original}}
	cache := &localLODModelCache{models: make(map[string]*GltfModel)}

	loaded, err := loadLocalLODModels(cfg, level, source, cache)
	if err != nil {
		t.Fatal(err)
	}
	instances := loaded[groupKey]
	if len(instances) != 1 || string(instances[0].Gltf.Content) != string(wantContent) {
		t.Fatal("local LOD model content was not loaded")
	}
	if string(original.Gltf.Content) != "minio-lod3" {
		t.Fatal("loading a local LOD mutated the LOD3 source instance")
	}
}

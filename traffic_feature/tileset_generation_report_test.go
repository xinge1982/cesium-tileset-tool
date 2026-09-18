package traffic_feature

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cesium-tileset-tool/config"
)

func TestWriteAndCollectTilesetGenerationReport(t *testing.T) {
	output := t.TempDir()
	if err := os.MkdirAll(filepath.Join(output, "tiles"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "tileset.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "tiles", "lod0.glb"), []byte("glb"), 0644); err != nil {
		t.Fatal(err)
	}

	report := newTilesetGenerationReport("test", "partition", output, "1,2,3,4", time.Now())
	report.Status = "success"
	if err := writeTilesetGenerationReport(output, report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, tilesetGenerationReportFilename))
	if err != nil {
		t.Fatal(err)
	}
	var decoded tilesetGenerationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ConfigName != "test" || decoded.PartitionTable != "partition" {
		t.Fatalf("unexpected report: %+v", decoded)
	}

	files, err := collectOutputFileStats(output)
	if err != nil {
		t.Fatal(err)
	}
	if files.GLBFiles != 1 || files.GLBBytes != 3 || files.TilesetJSONBytes != 2 || files.TotalFiles != 2 {
		t.Fatalf("unexpected output file statistics: %+v", files)
	}
}

func TestRecordLODGenerationStatsIncludesMissingLocalModel(t *testing.T) {
	networkFolder := t.TempDir()
	cfg := &config.Config{NetworkFolder: networkFolder}
	level := tileLODLevel{Level: 1, ModelFolder: "lod1", GeometricError: 40}
	model := &GeoHashModel{TableName: "hdservice_area", Model: "missing.glb"}
	key := geoHashModelGroupKey(model)
	requested := map[string][]*GeoHashModel{key: []*GeoHashModel{model}}
	stats := tileGenerationStats{byLOD: make(map[int]*lodGenerationStats)}

	recordLODGenerationStats(
		&stats, cfg, GeoTable{}, "wtt", level,
		requested, map[string][]*GeoHashModel{}, nil,
	)

	lod := stats.byLOD[1]
	if lod == nil || len(lod.MissingModels) != 1 {
		t.Fatalf("missing model was not recorded: %+v", lod)
	}
	missing := lod.MissingModels[0]
	wantPath := filepath.Join(networkFolder, "lod1", "hdservice_area", "missing.glb")
	if missing.ModelMode != "local" || missing.ExpectedPath != wantPath || missing.Geohash != "wtt" {
		t.Fatalf("unexpected missing model information: %+v", missing)
	}
}

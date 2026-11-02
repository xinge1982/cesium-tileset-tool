package traffic_feature

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cesium-tileset-tool/config"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mmcloughlin/geohash"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func roadMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func roadTestTable() GeoTable {
	return GeoTable{PartitionTableName: "road_parts", Threshold: 50, GeoTableNames: []string{"surfaces"}, TilesetSources: map[string]config.TilesetSourceConfig{
		"surfaces": {ID: "surface", ModelSource: "generated-road", Table: config.TableConfig{Name: "surfaces", PrimaryKey: "id", GeometryField: "geom"}, Road: &config.GeneratedRoadConfig{BuildType: "road_surface"}},
	}}
}

// Retain small nonempty leaves and prune obsolete descendants after source changes.
func TestRoadPartitionThresholdAndPruning(t *testing.T) {
	db, mock := roadMockDB(t)
	table := roadTestTable()
	mock.ExpectQuery(`SELECT p.geohash, COUNT\(\*\) AS count`).WillReturnRows(sqlmock.NewRows([]string{"geohash", "count"}).AddRow("wtw", 51).AddRow("wtq", 10))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"id", "geohash", "level"}).AddRow(1, "wtw", 3).AddRow(2, "wtq", 3).AddRow(3, "wtm", 3))
	mock.ExpectExec(`UPDATE "road_parts" SET`).WithArgs(true, false, 51, sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "road_parts" SET`).WithArgs(false, false, 10, sqlmock.AnyArg(), 2).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM "road_parts" WHERE geohash LIKE`).WithArgs("wtq%", 3).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE "road_parts" SET`).WithArgs(false, false, 0, sqlmock.AnyArg(), 3).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM "road_parts" WHERE geohash LIKE`).WithArgs("wtm%", 3).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	if err := updateSourcePartitionCount(db, table, "", 3, projectBound{120, 30, 122, 32}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`DELETE FROM "road_parts" WHERE parent_hash = .* AND level = .* AND total_count = 0`).WithArgs("wtw", 4).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := cleanupSourceChildren(db, table, "wtw", 4, projectBound{}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Run the actual refinement entry point for a road source rather than skipping it.
func TestRefineUntilStableIncludesRoadSources(t *testing.T) {
	db, mock := roadMockDB(t)
	previous := AllTiles
	AllTiles = []GeoTable{roadTestTable()}
	t.Cleanup(func() { AllTiles = previous })
	mock.ExpectQuery(`SELECT count\(\*\) FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT MIN\(level\) FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(3))
	mock.ExpectQuery(`SELECT p.geohash, COUNT\(\*\) AS count`).WillReturnRows(sqlmock.NewRows([]string{"geohash", "count"}).AddRow("wtw", 1))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"id", "geohash", "level"}).AddRow(1, "wtw", 3))
	mock.ExpectExec(`UPDATE "road_parts" SET`).WithArgs(false, false, 1, sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM "road_parts" WHERE geohash LIKE`).WithArgs("wtw%", 3).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	for level := int16(3); level < MAX_GEOHASH_LEVEL; level++ {
		mock.ExpectQuery(`SELECT count\(\*\) FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \* FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"id", "geohash", "level"}))
		mock.ExpectCommit()
		mock.ExpectQuery(`SELECT count\(\*\) FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
	mock.ExpectQuery(`SELECT count\(\*\) FROM "road_parts"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	dir := t.TempDir()
	if err := RefineUntilStable(db, "120,30,122,32", dir); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, tilesetRefinementReportFilename))
	if err != nil {
		t.Fatal(err)
	}
	var report tilesetRefinementReport
	if err = json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "success" || report.TablesProcessed != 1 || report.Tables[0].SourceTables[0] != "surfaces" {
		t.Fatal(report)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Exercise production export with SQL-provided adaptive leaves and source geometry.
// No point-model lookup, MinIO request or nested road tileset is permitted.
func TestGenerateAllGeoHashTileRoadExport(t *testing.T) {
	db, mock := roadMockDB(t)
	coarse := geohash.EncodeWithPrecision(31, 121, 6)
	cell := geohash.BoundingBox(coarse)
	x, y := cell.MaxLng, cell.MinLat+(cell.MaxLat-cell.MinLat)*.37
	fine := geohash.EncodeWithPrecision(y, x+.00005, 7)
	previous := AllTiles
	table := roadTestTable()
	// Even a configured split level must not create source or hierarchy sub-tilesets.
	table.TilesetSplitLevel = 4
	AllTiles = []GeoTable{table}
	t.Cleanup(func() { AllTiles = previous })
	mock.ExpectQuery(`SELECT parent.geohash, parent.level, parent.total_count`).WillReturnRows(sqlmock.NewRows([]string{"geohash", "level", "total_count"}).AddRow(coarse, 6, 1).AddRow(fine, 7, 1))
	polygon := geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{x - .0001, y - .00001, 10}, {x + .0001, y - .00001, 10}, {x + .0001, y + .00001, 10}, {x - .0001, y + .00001, 10}, {x - .0001, y - .00001, 10}}})
	geometry, err := geojson.Marshal(polygon)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT ST_AsGeoJSON`).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties"}).AddRow(string(geometry), `{"id":9007199254740993,"name":"road"}`))
	dir := t.TempDir()
	tileset, err := generateAllGeoHashTile(db, "road-test", table.PartitionTableName, dir, "120,30,122,32")
	if err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{coarse, fine} {
		node := FindNodeByGeohash(tileset.Root, hash)
		if node == nil || node.Level != len(hash) || len(node.Children) != 1 {
			t.Fatal("lost database leaf", hash, node)
		}
	}
	models := 0
	var check func(*TileNode)
	check = func(node *TileNode) {
		if node.Content != nil {
			if !strings.HasSuffix(node.Content.Uri, ".glb") {
				t.Fatal("nested tileset content", node.Content.Uri)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(node.Content.Uri))); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(node.Content.Uri)))
			if err != nil || !bytes.Contains(data, []byte("9007199254740993")) {
				t.Fatal("database feature ID lost precision", err)
			}
			models++
		}
		for _, child := range node.Children {
			check(child)
		}
	}
	check(tileset.Root)
	if models != 2 {
		t.Fatal("unexpected generated model count", models)
	}
	tilesetJSONCount := 0
	if err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == "tileset.json" {
			tilesetJSONCount++
		}
		return err
	}); err != nil || tilesetJSONCount != 1 {
		t.Fatal("expected one road tileset.json", tilesetJSONCount, err)
	}
	content, err := os.ReadFile(filepath.Join(dir, tilesetGenerationReportFilename))
	if err != nil {
		t.Fatal(err)
	}
	var report tilesetGenerationReport
	if err = json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "success" || report.LeafTilesFound != 2 || report.LeafTilesGenerated != 2 || len(report.GeneratedRoads) != 1 || report.ExternalTilesets != 0 {
		t.Fatal(report)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

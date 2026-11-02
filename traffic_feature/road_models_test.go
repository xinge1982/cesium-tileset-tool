package traffic_feature

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cesium-tileset-tool/config"
	"cesium-tileset-tool/map3d/road"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Classify explicit generation without changing legacy source selection.
func TestRoadModelSourceClassification(t *testing.T) {
	for _, source := range []config.TilesetSourceConfig{{}, {Table: config.TableConfig{Name: "hdpsidewall"}}, {ModelSource: "existing"}} {
		mode, err := ResolveModelSource(source)
		if err != nil || mode != ModelSourceExisting {
			t.Fatal(mode, err)
		}
	}
	generated := config.TilesetSourceConfig{ModelSource: "generated-road", Table: config.TableConfig{Name: "walls"}, Road: &config.GeneratedRoadConfig{BuildType: "barrier_wave"}}
	static, roads, err := splitRoadModelSources(GeoTable{GeoTableNames: []string{"walls", "hdpole"}, TilesetSources: map[string]config.TilesetSourceConfig{"walls": generated}})
	if err != nil || len(roads) != 1 || len(static.GeoTableNames) != 1 || static.GeoTableNames[0] != "hdpole" {
		t.Fatal(static, roads, err)
	}
	for _, source := range []config.TilesetSourceConfig{{ModelSource: "generated-road"}, {ModelSource: "unknown"}, {ModelSource: "existing", Road: generated.Road}, {Road: &config.GeneratedRoadConfig{BuildType: "unsupported"}}} {
		if _, err := ResolveModelSource(source); err == nil {
			t.Fatal("accepted invalid classification")
		}
	}
	// The ordinary query must not touch DB or model storage for generated sources.
	models, err := queryGeoHashModelData("", GeoTable{GeoTableNames: []string{"walls"}, TilesetSources: map[string]config.TilesetSourceConfig{"walls": generated}}, nil, "wx4g0e", "", projectBound{})
	if err != nil || len(models) != 0 {
		t.Fatal(models, err)
	}
}

// Verify identifier quoting and bound parameter use, including category filter values.
func TestRoadFeatureQuery(t *testing.T) {
	query, args, err := roadFeatureQuery(config.TableConfig{Schema: "public", Name: "hdpsidewall", PrimaryKey: "fid", GeometryField: "shape"}, &config.GeneratedRoadConfig{FilterField: "bxdl", FilterValues: []string{"5", "7"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`ST_Transform(t."shape",4326)`, `FROM "public"."hdpsidewall"`, `t."bxdl"::text IN ?`, `ORDER BY t."fid"`} {
		if !strings.Contains(query, part) {
			t.Fatal(query)
		}
	}
	if len(args) != 2 || args[0] != "shape" {
		t.Fatal(args)
	}
	query, _, err = roadFeatureQuery(config.TableConfig{Name: `walls"; DROP TABLE x; --`}, nil)
	if err != nil || !strings.Contains(query, `"walls""; DROP TABLE x; --"`) {
		t.Fatal(query, err)
	}
	if _, _, err = roadFeatureQuery(config.TableConfig{Name: "walls"}, &config.GeneratedRoadConfig{FilterField: "type"}); err == nil {
		t.Fatal("empty filter accepted")
	}
}

// Generate surface and barrier child tilesets through the traffic-feature adapter.
// The loader receives complete collections and is invoked once per source, not per tile.
func TestGenerateRoadSourcesAndAttach(t *testing.T) {
	surfaces := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{121, 31, 10}, {121.0001, 31, 10}, {121.0001, 31.0001, 10}, {121, 31.0001, 10}, {121, 31, 10}}}), Properties: map[string]any{"id": "surface-1", "hroad_id": 1}}}}
	walls := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{121, 31, 10}, {121.0002, 31, 10}}), Properties: map[string]any{"id": "rail-1", "direction": 2, "bxdl": 5}}}}
	sources := map[string]config.TilesetSourceConfig{
		"surface": {ID: "surface", Table: config.TableConfig{Name: "surface"}, Road: &config.GeneratedRoadConfig{BuildType: "road_surface", Precision: 6}},
		"walls":   {ID: "walls", Table: config.TableConfig{Name: "walls"}, Road: &config.GeneratedRoadConfig{BuildType: "barrier_wave", Precision: 6, WaveSubtype: "wave_two"}},
	}
	table := GeoTable{GeoTableNames: []string{"surface", "walls"}, TilesetSources: sources}
	reads := map[string]int{}
	load := func(_ context.Context, table config.TableConfig, _ *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error) {
		reads[table.Name]++
		if table.Name == "surface" {
			return surfaces, nil
		}
		return walls, nil
	}
	dir := t.TempDir()
	outputs, err := generateRoadSourceModels(context.Background(), table, dir, "120.99,30.99,121.01,31.01", load)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 || reads["surface"] != 1 || reads["walls"] != 1 {
		t.Fatal(outputs, reads)
	}
	root := &Tileset{Root: &TileNode{Refine: "ADD"}}
	if err = attachGeneratedRoadModels(root, outputs); err != nil {
		t.Fatal(err)
	}
	if len(root.Root.Children) != 2 || root.Root.Transform != nil || math.Abs(root.Root.BoundingVolume.Box[0]) < 1e6 {
		t.Fatal("bad world-space root", root.Root)
	}
	for i, output := range outputs {
		child := root.Root.Children[i]
		if child.Transform != nil || child.Content.Uri != output.TilesetURI {
			t.Fatal("double transform or missing external root")
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(output.TilesetURI)))
		if err != nil {
			t.Fatal(err)
		}
		var nested struct {
			Root struct {
				Transform []float64 `json:"transform"`
			} `json:"root"`
		}
		if err = json.Unmarshal(data, &nested); err != nil || len(nested.Root.Transform) != 16 {
			t.Fatal("nested ENU transform lost")
		}
	}
	repeated, err := generateRoadSourceModels(context.Background(), table, dir, "120.99,30.99,121.01,31.01", load)
	if err != nil {
		t.Fatal(err)
	}
	if repeated[0].TilesetURI == outputs[0].TilesetURI {
		t.Fatal("reused stale procedural cache")
	}
	// Both APIs remain directly callable without a database connection.
	if _, err = road.BuildRoadModelSlices(context.Background(), road.RoadModelInput{Features: walls}, road.RoadModelOptions{BuildType: road.BuildTypeBarrierWave, OutputDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

// Preserve static content and enclose both model categories without double transforms.
func TestAttachGeneratedRoadModelsToExistingRoot(t *testing.T) {
	staticBox := [12]float64{6000000, 100, 200, 10, 0, 0, 0, 20, 0, 0, 0, 30}
	roadBox := [12]float64{6000100, 300, 400, 40, 0, 0, 0, 50, 0, 0, 0, 60}
	staticNode := &TileNode{Content: &TileContent{Uri: "static.json"}}
	tileset := &Tileset{Root: &TileNode{BoundingVolume: BoundingVolume{Box: staticBox}, Children: []*TileNode{staticNode}, GeometricError: 80}}
	if err := attachGeneratedRoadModels(tileset, []GeneratedRoadOutput{{Table: "roads", TilesetURI: "generated-road/source-test/tileset.json", WorldBox: roadBox, GeometricError: 100}}); err != nil {
		t.Fatal(err)
	}
	if len(tileset.Root.Children) != 2 || tileset.Root.Children[0] != staticNode || tileset.GeometricError != 100 {
		t.Fatal("static hierarchy or root error changed incorrectly")
	}
	box := tileset.Root.BoundingVolume.Box
	for _, child := range [][12]float64{staticBox, roadBox} {
		for axis := 0; axis < 3; axis++ {
			if math.Abs(child[axis]-box[axis])+math.Abs(child[3+4*axis]) > math.Abs(box[3+4*axis])+1e-7 {
				t.Fatal("parent does not enclose child", box, child)
			}
		}
	}
}

package traffic_feature

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cesium-tileset-tool/cesium"
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/map3d/road"
	"github.com/mmcloughlin/geohash"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Keep old point sources unchanged and require an independent road export.
func TestRoadModelSourceClassification(t *testing.T) {
	for _, source := range []config.TilesetSourceConfig{{}, {Table: config.TableConfig{Name: "hdpsidewall"}}, {ModelSource: "existing"}} {
		mode, err := ResolveModelSource(source)
		if err != nil || mode != ModelSourceExisting {
			t.Fatal(mode, err)
		}
	}
	generated := config.TilesetSourceConfig{ModelSource: "generated-road", Table: config.TableConfig{Name: "walls"}, Road: &config.GeneratedRoadConfig{BuildType: "barrier_wave"}}
	table := GeoTable{GeoTableNames: []string{"walls"}, TilesetSources: map[string]config.TilesetSourceConfig{"walls": generated}}
	sources, err := roadModelSources(table)
	if err != nil || len(sources) != 1 {
		t.Fatal(sources, err)
	}
	table.GeoTableNames = append(table.GeoTableNames, "hdpole")
	if _, err = roadModelSources(table); err == nil {
		t.Fatal("accepted mixed road/device export")
	}
	for _, source := range []config.TilesetSourceConfig{{ModelSource: "generated-road"}, {ModelSource: "unknown"}, {ModelSource: "existing", Road: generated.Road}, {Road: &config.GeneratedRoadConfig{BuildType: "unsupported"}}} {
		if _, err := ResolveModelSource(source); err == nil {
			t.Fatal("accepted invalid classification")
		}
	}
	models, err := queryGeoHashModelData("", GeoTable{GeoTableNames: []string{"walls"}, TilesetSources: table.TilesetSources}, nil, "wx4g0e", "", projectBound{})
	if err != nil || len(models) != 0 {
		t.Fatal(models, err)
	}
}

// Bind filters consistently and count crossing geometries in all intersected cells.
func TestRoadFeatureAndPartitionQueries(t *testing.T) {
	source := config.TilesetSourceConfig{Table: config.TableConfig{Schema: "public", Name: "hdpsidewall", PrimaryKey: "fid", GeometryField: "shape"}, Road: &config.GeneratedRoadConfig{BuildType: "barrier_wave", FilterField: "bxdl", FilterValues: []string{"5", "7"}}}
	query, args, err := roadFeatureQuery(source.Table, source.Road)
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
	if _, _, err = roadFeatureQuery(source.Table, &config.GeneratedRoadConfig{FilterField: "type"}); err == nil {
		t.Fatal("empty filter accepted")
	}
	source.Road.WaveSubtypeField = "rail_kind"
	source.Road.WaveSubtypeMapping = map[string]string{"a": "wave_two", "b": "wave_three"}
	query, args, err = roadPartitionCountQuery(GeoTable{PartitionTableName: "public.road_partitions"}, source, "wtw3", 5, projectBound{120, 30, 122, 32})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`FROM "public"."road_partitions" AS p`, `JOIN "public"."hdpsidewall" AS t`, `ST_DWithin(ST_Transform(t."shape",4326)::geography, p.bbox::geography, ?)`, `p.parent_hash = ?`, `t."bxdl"::text IN ?`, `t."rail_kind"::text IN ?`} {
		if !strings.Contains(query, part) {
			t.Fatal(query)
		}
	}
	if strings.Contains(query, "ST_GeoHash") || strings.Contains(query, "model like") || len(args) != 13 {
		t.Fatal(query, args)
	}
}

// Use adaptive database leaves directly, including geometries crossing both cells.
// Emit no source tileset JSON and preserve instancing and feature metadata in the GLBs.
func TestGenerateRoadSourcesForDatabaseLeaves(t *testing.T) {
	coarse := geohash.EncodeWithPrecision(31, 121, 6)
	box := geohash.BoundingBox(coarse)
	x, y := box.MaxLng, box.MinLat+(box.MaxLat-box.MinLat)*.37
	fine := geohash.EncodeWithPrecision(y, x+.00005, 7)
	leaves := []*GeoHashTile{{Geohash: coarse, Level: 6}, {Geohash: fine, Level: 7}}
	surfaces := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{x - .0001, y - .00001, 10}, {x + .0001, y - .00001, 10}, {x + .0001, y + .00001, 10}, {x - .0001, y + .00001, 10}, {x - .0001, y - .00001, 10}}}), Properties: map[string]any{"id": "surface-1", "hroad_id": json.Number("9007199254740993")}}}}
	walls := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{x - .00015, y, 10}, {x + .00015, y, 10}}), Properties: map[string]any{"id": "rail-1", "direction": json.Number("2"), "bxdl": 5}}}}
	sources := map[string]config.TilesetSourceConfig{
		"surface": {ID: "surface", Table: config.TableConfig{Name: "surface"}, Road: &config.GeneratedRoadConfig{BuildType: "road_surface", Precision: 4}},
		"walls":   {ID: "walls", Table: config.TableConfig{Name: "walls"}, Road: &config.GeneratedRoadConfig{BuildType: "barrier_wave", Precision: 4, WaveSubtype: "wave_two"}},
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
	nodes, outputs, err := generateRoadSourceModels(context.Background(), table, leaves, dir, "120,30,122,32", load)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || len(outputs) != 2 || reads["surface"] != 1 || reads["walls"] != 1 {
		t.Fatal(nodes, outputs, reads)
	}
	root := &TileNode{Refine: "ADD"}
	for _, leaf := range leaves {
		node := nodes[leaf.Geohash]
		if node == nil || node.Level != leaf.Level || len(node.Children) != 2 {
			t.Fatal("ignored database depth", node)
		}
		root.Children = append(root.Children, node)
	}
	refreshGeoHashTileBound(root, 1)
	if root.Transform != nil || math.Abs(root.BoundingVolume.Box[0]) < 1e6 {
		t.Fatal("root is not in world space")
	}
	for _, node := range root.Children {
		for _, child := range node.Children {
			world := roadWorldBox(child.BoundingVolume.Box, *child.Transform)
			for _, corner := range cesium.Box12ToOBB(world).Corners() {
				for _, parentBox := range [][12]float64{node.BoundingVolume.Box, root.BoundingVolume.Box} {
					for axis := 0; axis < 3; axis++ {
						length2, dot := 0., 0.
						for coordinate, value := range []float64{corner.X, corner.Y, corner.Z} {
							component := parentBox[3+axis*3+coordinate]
							length2 += component * component
							dot += (value - parentBox[coordinate]) * component
						}
						if math.Abs(dot) > length2+1e-3 {
							t.Fatal("generated model outside hierarchy bounds", world, parentBox)
						}
					}
				}
			}
			if child.Content == nil || !strings.HasSuffix(child.Content.Uri, ".glb") || child.Transform == nil {
				t.Fatal("not a direct model leaf", child)
			}
			doc, err := gltf.Open(filepath.Join(dir, filepath.FromSlash(child.Content.Uri)))
			if err != nil {
				t.Fatal(err)
			}
			if err = road.ValidateStructuralMetadataBufferViews(doc); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(child.Content.Uri, "barrier.glb") {
				if doc.Extensions["EXT_structural_metadata"] == nil {
					t.Fatal("metadata lost")
				}
				instanced := false
				for _, n := range doc.Nodes {
					instanced = instanced || n.Extensions["EXT_mesh_gpu_instancing"] != nil
				}
				if !instanced {
					t.Fatal("post instancing lost")
				}
			}
		}
	}
	if err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".json") {
			t.Fatalf("source wrote a separate tileset: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repeated, _, err := generateRoadSourceModels(context.Background(), table, leaves, dir, "120,30,122,32", load)
	if err != nil {
		t.Fatal(err)
	}
	if repeated[coarse].Children[0].Content.Uri == nodes[coarse].Children[0].Content.Uri {
		t.Fatal("reused old models")
	}
	// Missing leaves must not cause a fallback to fixed-precision slicing or a DB read.
	empty, _, err := generateRoadSourceModels(context.Background(), table, nil, dir, "120,30,122,32", func(context.Context, config.TableConfig, *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error) {
		t.Fatal("unexpected source query")
		return nil, nil
	})
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	if _, _, err = generateRoadSourceModels(context.Background(), table, []*GeoHashTile{leaves[0], leaves[0]}, dir, "120,30,122,32", load); err == nil {
		t.Fatal("duplicate database leaf accepted")
	}
}

package road

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mmcloughlin/geohash"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Exercise both public generators without fixtures, textures or a database.
func TestBuildRoadModelSlicesPublicAPI(t *testing.T) {
	box := geohash.BoundingBox(geohash.EncodeWithPrecision(31, 121, 6))
	x, y := box.MaxLng, (box.MinLat+box.MaxLat)/2
	for _, kind := range []BuildType{BuildTypeRoadSurface, BuildTypeBarrierWave} {
		t.Run(string(kind), func(t *testing.T) {
			var geometry geom.T
			if kind == BuildTypeRoadSurface {
				geometry = geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{x - .0001, y - .00002, 10}, {x + .0001, y - .00002, 10}, {x + .0001, y + .00002, 11}, {x - .0001, y + .00002, 11}, {x - .0001, y - .00002, 10}}})
			} else {
				geometry = geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{x - .0001, y, 10}, {x + .0001, y, 11}})
			}
			fc := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geometry, Properties: map[string]any{"id": "9007199254740993", "direction": 2, "feature_type": "wave_three", "custom": "original"}}}}
			original, _ := json.Marshal(fc)
			result, err := BuildRoadModelSlices(context.Background(), RoadModelInput{Features: fc}, RoadModelOptions{BuildType: kind, OutputDir: t.TempDir(), Precision: 6})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Tiles) < 2 {
				t.Fatal("source was not cut across the seam")
			}
			if math.Sqrt(result.WorldBox[0]*result.WorldBox[0]+result.WorldBox[1]*result.WorldBox[1]+result.WorldBox[2]*result.WorldBox[2]) < 6e6 {
				t.Fatal("result box is not in ECEF")
			}
			for _, tile := range result.Tiles {
				doc, err := gltf.Open(tile.GLBPath)
				if err != nil {
					t.Fatal(err)
				}
				if err = ValidateStructuralMetadataBufferViews(doc); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(tile.GLBPath)
				if err != nil {
					t.Fatal(err)
				}
				if len(data) == 0 || filepath.Ext(tile.GLBPath) != ".glb" {
					t.Fatal("missing generated geometry")
				}
			}
			after, _ := json.Marshal(fc)
			if string(original) != string(after) {
				t.Fatal("source mutated")
			}
		})
	}
}

// Map business categories explicitly and retain source IDs/property values.
func TestBarrierWaveFeatureConversion(t *testing.T) {
	line := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{121, 31, 0}, {121.0001, 31, 0}})
	fc := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: line, Properties: map[string]any{"id": "1", "bxdl": 5, "name": "rail"}}, {Geometry: line, Properties: map[string]any{"id": "2", "bxdl": 99}}}}
	features, err := BarrierWaveFeaturesFromGeoJSON(fc, RoadModelOptions{WaveSubtypeField: "bxdl", WaveSubtypeMapping: map[string]string{"5": "wave_two"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 1 || features[0].Fields["feature_type"] != "wave_two" || features[0].Features[0]["name"] != "rail" || features[0].Material.Top.Path != "" {
		t.Fatal(features)
	}
	if !reflect.DeepEqual(fc.Features[0].Properties, map[string]any{"id": "1", "bxdl": 5, "name": "rail"}) {
		t.Fatal("mutated properties")
	}
	if _, err = BuildRoadModelSlices(context.Background(), RoadModelInput{Features: fc}, RoadModelOptions{BuildType: BuildTypeRoadSurface, OutputDir: t.TempDir()}); err == nil {
		t.Fatal("surface accepted a line")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = BuildRoadModelSlices(ctx, RoadModelInput{Features: fc}, RoadModelOptions{BuildType: BuildTypeBarrierWave, OutputDir: t.TempDir()}); err != context.Canceled {
		t.Fatal(err)
	}
}

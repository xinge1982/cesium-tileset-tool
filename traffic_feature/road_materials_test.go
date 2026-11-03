package traffic_feature

import (
	"encoding/json"
	"testing"

	"cesium-tileset-tool/config"
	"cesium-tileset-tool/map3d/road"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"gopkg.in/yaml.v3"
)

// Read public configuration keys and match the resolved business subtype to its parts.
func TestRoadMaterialConfigMapping(t *testing.T) {
	var source config.TilesetSourceConfig
	if err := yaml.Unmarshal([]byte(`
id: wave
road:
  buildType: barrier_wave
  texturePath: legacy.png
  materials:
    rail:
      texturePath: default.png
    post:
      color: "#112233"
  materialOverrides:
    wave_three:
      rail:
        texturePath: three.png
    wave_nose_end:
      rail:
        color: "#445566"
`), &source); err != nil {
		t.Fatal(err)
	}
	if mode, err := ResolveModelSource(source); err != nil || mode != ModelSourceGeneratedRoad {
		t.Fatal(mode, err)
	}
	before, _ := json.Marshal(source)
	materials, overrides := roadMaterialOptionsFromConfig(source.Road)
	line := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{121, 31, 0}, {121.0001, 31, 0}})
	fc := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: line, Properties: map[string]any{"id": "rail-1", "feature_type": "wave_three"}}}}
	features, err := road.BarrierWaveFeaturesFromGeoJSON(fc, road.RoadModelOptions{TexturePath: source.Road.TexturePath, Materials: materials, MaterialOverrides: overrides})
	if err != nil || len(features) != 1 {
		t.Fatal(features, err)
	}
	if features[0].Material.Top.Path != "three.png" || features[0].Material.Side.Color != "#112233" {
		t.Fatal("configuration did not reach material slots", features[0].Material)
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("configuration maps were mutated")
	}
	source.Road.Materials["pots"] = config.GeneratedRoadMaterialConfig{Color: "#ffffff"}
	if _, err = ResolveModelSource(source); err == nil {
		t.Fatal("misspelled part was silently accepted")
	}
}

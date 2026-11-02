package road

import (
	"math"
	"testing"

	"github.com/mmcloughlin/geohash"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Reject ancestor/descendant overlap instead of duplicating faces and posts.
func TestAdaptiveRoadLeafValidation(t *testing.T) {
	for _, hashes := range [][]string{{"wtw", "wtw3"}, {"wtw3", "wtw3"}, {"bad!"}, {"wt"}} {
		if _, err := roadSliceTargetGeohashes([6]float64{}, 6, hashes); err == nil {
			t.Fatal("accepted invalid leaves", hashes)
		}
	}
	if hashes, err := roadSliceTargetGeohashes([6]float64{}, 6, []string{}); err != nil || len(hashes) != 0 {
		t.Fatal("empty explicit leaves caused fixed slicing", hashes, err)
	}
}

// Preserve area and support ownership across adjacent leaves at different depths.
func TestAdaptiveRoadModelConservation(t *testing.T) {
	coarse := geohash.EncodeWithPrecision(31, 121, 6)
	cell := geohash.BoundingBox(coarse)
	x, y := cell.MaxLng, cell.MinLat+(cell.MaxLat-cell.MinLat)*.37
	fine := geohash.EncodeWithPrecision(y, x+.00005, 7)
	hashes := []string{fine, coarse}
	surfaces := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{x - .0001, y - .00001, 10}, {x + .0001, y - .00001, 11}, {x + .0001, y + .00001, 11}, {x - .0001, y + .00001, 10}, {x - .0001, y - .00001, 10}}}), Properties: map[string]any{"id": "surface"}}}}
	result, err := BuildRoadSurfaceGeohashSlices(surfaces, nil, RoadSurfaceGeohashOptions{OutputDir: t.TempDir(), Geohashes: hashes, ModelsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tiles) != 2 || math.Abs(result.SourceArea-result.SlicedArea)/result.SourceArea > 1e-5 || result.TilesetPath != "" {
		t.Fatal("adaptive surface area changed", result)
	}
	lines := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{x - .0001, y, 10}, {x + .0001, y, 11}}), Properties: map[string]any{"id": "rail", "direction": 2}}}}
	features, err := BarrierWaveFeaturesFromGeoJSON(lines, RoadModelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := BuildBarrierWaveGeohashSlices(features, BarrierWaveGeohashOptions{OutputDir: t.TempDir(), Geohashes: hashes, ModelsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(wave.Tiles) != 2 || wave.SourceInstances == 0 || wave.SourceInstances != wave.SlicedInstances || math.Abs(wave.SourceArea-wave.SlicedArea)/wave.SourceArea > 1e-5 || wave.TilesetPath != "" {
		t.Fatal("adaptive rail area/ownership changed", wave)
	}
	// A project may select only one side. Do not require full-source conservation then.
	partial, err := BuildRoadSurfaceGeohashSlices(surfaces, nil, RoadSurfaceGeohashOptions{OutputDir: t.TempDir(), Geohashes: []string{fine}, ModelsOnly: true})
	if err != nil || len(partial.Tiles) != 1 || partial.SlicedArea >= partial.SourceArea {
		t.Fatal("partial project leaves were not clipped", partial, err)
	}
}

// Include crossing geometries and reject a bound located entirely inside a hole.
func TestRoadGeometryIntersectsProjectBound(t *testing.T) {
	bound := [4]float64{1, 1, 2, 2}
	crossing := geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{0, 1.5, 5}, {3, 1.5, 6}})
	outside := geom.NewLineString(geom.XY).MustSetCoords([]geom.Coord{{0, 0}, {.5, .5}})
	polygon := geom.NewPolygon(geom.XY).MustSetCoords([][]geom.Coord{{{0, 0}, {3, 0}, {3, 3}, {0, 3}, {0, 0}}})
	hole := geom.NewPolygon(geom.XY).MustSetCoords([][]geom.Coord{{{0, 0}, {3, 0}, {3, 3}, {0, 3}, {0, 0}}, {{.5, .5}, {2.5, .5}, {2.5, 2.5}, {.5, 2.5}, {.5, .5}}})
	for _, tc := range []struct {
		geometry geom.T
		hit      bool
	}{{crossing, true}, {outside, false}, {polygon, true}, {hole, false}} {
		if hit := roadGeometryIntersectsBound(tc.geometry, bound); hit != tc.hit {
			t.Fatal(tc.geometry, hit, tc.hit)
		}
	}
}

// Preserve upstream stationing when the project range retains only a downstream feature.
func TestAdaptiveRoadBoundKeepsFullChainPhase(t *testing.T) {
	lon, lat := 121., 31.
	step := 1 / (111320 * math.Cos(lat*math.Pi/180))
	features := &geojson.FeatureCollection{Features: []*geojson.Feature{
		{Geometry: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{lon, lat, 0}, {lon + 10.3*step, lat, 0}}), Properties: map[string]any{"id": "upstream", "direction": 2}},
		{Geometry: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{lon + 10.3*step, lat, 0}, {lon + 17.5*step, lat, 0}}), Properties: map[string]any{"id": "downstream", "direction": 2}},
	}}
	lines, err := BarrierWaveFeaturesFromGeoJSON(features, RoadModelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hashes := []string{geohash.EncodeWithPrecision(lat, lon, 6)}
	full, err := BuildBarrierWaveGeohashSlices(lines, BarrierWaveGeohashOptions{OutputDir: t.TempDir(), Geohashes: hashes, ModelsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	bound := [4]float64{lon + 10.4*step, lat - .001, lon + .001, lat + .001}
	partial, err := BuildBarrierWaveGeohashSlices(lines, BarrierWaveGeohashOptions{OutputDir: t.TempDir(), Geohashes: hashes, ModelsOnly: true, IntersectBound: &bound})
	if err != nil {
		t.Fatal(err)
	}
	positions := func(result *BarrierWaveGeohashResult) [][3]float32 {
		doc, err := gltf.Open(result.Tiles[0].GLBPath)
		if err != nil {
			t.Fatal(err)
		}
		if err = waveNormalizeExtensions(doc); err != nil {
			t.Fatal(err)
		}
		out := [][3]float32{}
		for _, node := range doc.Nodes {
			tr, _, _, _, ok, err := readInstancedNodeAttributes(doc, node)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				out = append(out, tr...)
			}
		}
		return out
	}
	all, selected := positions(full), positions(partial)
	if len(all) != 6 || len(selected) != 3 {
		t.Fatal("unexpected sampling", all, selected)
	}
	for _, point := range selected {
		found := false
		for _, original := range all {
			if point == original {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("project filtering reset the chain phase", point, all)
		}
	}
}

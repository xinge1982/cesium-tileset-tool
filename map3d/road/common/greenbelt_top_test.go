package common

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	geomjson "github.com/twpayne/go-geom/encoding/geojson"
)

func TestTriangulatePreferredGreenbeltTopRing_14151000030(t *testing.T) {
	const targetID = int64(14151000030)
	fc := readGeoJSONFeatureCollectionForGreenbeltTest(t, `..\test\road_fgd_lhm.geojson`)
	basePoint, region := calcTileContextForGreenbeltTest(t, fc)
	pipe, err := NewCoordinatePipeline(TileContext{
		TileID:    "greenbelt-top-test",
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    false,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}

	for _, f := range fc.Features {
		if featureIDForGreenbeltTest(f.Properties) != targetID {
			continue
		}
		rings, err := pipe.ToLocalSurface(SurfaceFeature{
			FeatureInput: FeatureInput{Geom: f.Geometry},
		})
		if err != nil {
			t.Fatalf("to local surface: %v", err)
		}
		if len(rings) == 0 {
			t.Fatal("no local rings")
		}

		pos, idx, err := triangulatePreferredGreenbeltTopRing(rings[0])
		if err != nil {
			t.Fatalf("triangulate preferred greenbelt top ring: %v", err)
		}
		if len(idx) < 3 {
			t.Fatal("greenbelt top generated no triangles")
		}

		outer := dedupeClosedRing(cleanGreenbeltTopLocalRings(rings[0], 0.001).Outer)
		if !trianglesFitPolygonXZ(outer, pos, idx) {
			t.Fatal("greenbelt triangles do not fit polygon")
		}

		polyArea := polygonAreaXZ(outer)
		triArea := trianglesAreaXZ(pos, idx)
		if polyArea <= 0 || triArea <= 0 {
			t.Fatalf("invalid area poly=%.6f tri=%.6f", polyArea, triArea)
		}
		if math.Abs(triArea-polyArea)/polyArea > 0.01 {
			t.Fatalf("greenbelt area drift too large poly=%.6f tri=%.6f", polyArea, triArea)
		}
		return
	}

	t.Fatalf("greenbelt id %d not found", targetID)
}

func readGeoJSONFeatureCollectionForGreenbeltTest(t *testing.T, path string) *geomjson.FeatureCollection {
	t.Helper()
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read geojson %s: %v", path, err)
	}
	fc := &geomjson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal geojson %s: %v", path, err)
	}
	return fc
}

func calcTileContextForGreenbeltTest(t *testing.T, fc *geomjson.FeatureCollection) ([]float64, [6]float64) {
	t.Helper()
	var region [6]float64
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)
	for _, f := range fc.Features {
		if f.Geometry == nil {
			continue
		}
		flat := f.Geometry.FlatCoords()
		stride := f.Geometry.Stride()
		if stride < 2 {
			continue
		}
		for i := 0; i+stride-1 < len(flat); i += stride {
			lon, lat := flat[i], flat[i+1]
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
	if math.IsInf(minLon, 0) || math.IsInf(minLat, 0) || math.IsInf(maxLon, 0) || math.IsInf(maxLat, 0) {
		t.Fatal("invalid feature collection bounds")
	}
	base := []float64{(minLon + maxLon) * 0.5, (minLat + maxLat) * 0.5, 0}
	region = [6]float64{minLon, minLat, maxLon, maxLat, 0, 0}
	return base, region
}

func featureIDForGreenbeltTest(props map[string]any) int64 {
	if props == nil {
		return 0
	}
	switch v := props["id"].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

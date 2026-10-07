package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestEstimateCenterlineGeomFromSurfaceGeom_CurvedRoadReturnsPolyline(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}
	roadPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "road_face_wgs84.geojson"))
	fc := &geojson.FeatureCollection{}
	buf, err := os.ReadFile(roadPath)
	if err != nil {
		t.Fatalf("read road geojson: %v", err)
	}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal road geojson: %v", err)
	}

	var g geom.T
	for _, f := range fc.Features {
		if featureFieldInt64(f.Properties, "road_id") == 14241000111 {
			g = f.Geometry
			break
		}
	}
	if g == nil {
		t.Fatal("road_id=14241000111 not found")
	}

	line, err := EstimateCenterlineGeomFromSurfaceGeom(g)
	if err != nil {
		t.Fatalf("estimate centerline: %v", err)
	}
	if line.NumCoords() < 5 {
		t.Fatalf("expected curved polyline centerline, got %d coords", line.NumCoords())
	}
}

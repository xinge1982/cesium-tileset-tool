package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestSubgradeAxisChunksForKnownIDs(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_lj.geojson"))
	buf, err := os.ReadFile(roadPath)
	if err != nil {
		t.Fatalf("read %s: %v", roadPath, err)
	}
	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal %s: %v", roadPath, err)
	}

	targetIDs := map[int64]bool{
		14251000002: false,
		14251000005: false,
		14251000007: false,
		14251000008: false,
		14251000009: false,
		14251000026: false,
	}

	for _, f := range fc.Features {
		if f.Geometry == nil {
			continue
		}
		roadID := int64FromAnyExperimental(f.Properties["road_id"])
		if roadID == 0 {
			roadID = int64FromAnyExperimental(f.Properties["id"])
		}
		if _, ok := targetIDs[roadID]; !ok {
			continue
		}
		targetIDs[roadID] = true

		surface := SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSubgrade,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
		}
		basePoint, region := calcTileContextForGeom(surface.Geom)
		p, err := NewCoordinatePipeline(TileContext{
			Region:    region,
			Center:    [3]float64{basePoint[0], basePoint[1], 0},
			BasePoint: basePoint,
			SRID:      4326,
			UseENU:    true,
		})
		if err != nil {
			t.Fatalf("new coordinate pipeline road_id=%d: %v", roadID, err)
		}
		rings, err := p.ToLocalSurface(surface)
		if err != nil || len(rings) == 0 {
			t.Fatalf("to local surface road_id=%d: %v", roadID, err)
		}
		ring := cleanSubgradeLocalRings(rings[0], 0.01)

		for _, chunk := range []float64{10, 15, 20, 30, 40, 60} {
			pos, idx, ok, poly, tri := triangulateSubgradeByAxisChunksDetailed(ring, chunk)
			t.Logf("road_id=%d chunk=%.0f axis_chunk_ok=%v tri=%d area=%.2f poly=%.2f", roadID, chunk, ok, len(idx)/3, tri, poly)
			_ = pos
		}
		if roadID == 14251000002 {
			for _, cell := range []float64{20, 25, 30, 35, 40} {
				_, idx, ok, poly, tri := triangulateSubgradeByGridCellsDetailed(ring, cell, cell)
				t.Logf("road_id=%d cell=%.0f grid_ok=%v tri=%d area=%.2f poly=%.2f", roadID, cell, ok, len(idx)/3, tri, poly)
			}
		}
	}

	for id, seen := range targetIDs {
		if !seen {
			t.Fatalf("road_id=%d not found in road_lj.geojson", id)
		}
	}
}

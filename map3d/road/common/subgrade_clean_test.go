package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestCleanSubgradeLocalRings_DoesNotCreateSelfIntersection(t *testing.T) {
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
		14251000008: false,
		14251000002: false,
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

		raw := dedupeClosedRing(rings[0].Outer)
		clean := dedupeClosedRing(cleanSubgradeLocalRings(rings[0], 0.01).Outer)

		if hasSelfIntersectionXZ(raw) {
			t.Fatalf("road_id=%d raw ring unexpectedly self-intersects", roadID)
		}
		if hasSelfIntersectionXZ(clean) {
			t.Fatalf("road_id=%d cleaned ring must not self-intersect", roadID)
		}
	}

	for id, seen := range targetIDs {
		if !seen {
			t.Fatalf("road_id=%d not found in road_lj.geojson", id)
		}
	}
}

package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestSubgradeStripDebugForKnownIDs(t *testing.T) {
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
		14251000007: false,
		14251000008: false,
		14251000009: false,
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
		polyArea := polygonAreaXZ(dedupeClosedRing(ring.Outer))
		if cl, ok := estimateDenseCurvedCenterlineFromOuter(dedupeClosedRing(ring.Outer)); ok {
			for _, spacing := range []float64{2, 3, 4} {
				left, right, center, ok2 := buildSubgradeSectionPairsAlongCurve(dedupeClosedRing(ring.Outer), cl.Points, spacing)
				if !ok2 {
					t.Logf("road_id=%d dense spacing=%.0f sections=fail poly=%.2f", roadID, spacing, polyArea)
					continue
				}
				pos, idx, ok3 := triangulateSectionQuads(left, right, center)
				l2, r2 := fitSectionWidthsToArea(left, right, center, polyArea)
				pos2, idx2, ok4 := triangulateSectionQuads(l2, r2, center)
				t.Logf("road_id=%d dense spacing=%.0f ok=%v sections=%d tris=%d area=%.2f fit_ok=%v fit_area=%.2f fit_inside=%v poly=%.2f", roadID, spacing, ok3, len(center), len(idx)/3, trianglesAreaXZ(pos, idx), ok4, trianglesAreaXZ(pos2, idx2), trianglesFitPolygonXZ(dedupeClosedRing(ring.Outer), pos2, idx2), polyArea)
			}
		}
		if cl, ok := estimateLocalCenterlineFromOuterWithMinRatio(dedupeClosedRing(ring.Outer), 1.0); ok {
			for _, spacing := range []float64{2, 3, 4} {
				left, right, center, ok2 := buildSubgradeSectionPairsAlongCurve(dedupeClosedRing(ring.Outer), cl.Points, spacing)
				if !ok2 {
					t.Logf("road_id=%d pca spacing=%.0f sections=fail poly=%.2f", roadID, spacing, polyArea)
					continue
				}
				pos, idx, ok3 := triangulateSectionQuads(left, right, center)
				l2, r2 := fitSectionWidthsToArea(left, right, center, polyArea)
				pos2, idx2, ok4 := triangulateSectionQuads(l2, r2, center)
				t.Logf("road_id=%d pca spacing=%.0f ok=%v sections=%d tris=%d area=%.2f fit_ok=%v fit_area=%.2f fit_inside=%v poly=%.2f", roadID, spacing, ok3, len(center), len(idx)/3, trianglesAreaXZ(pos, idx), ok4, trianglesAreaXZ(pos2, idx2), trianglesFitPolygonXZ(dedupeClosedRing(ring.Outer), pos2, idx2), polyArea)
			}
		}
	}
}

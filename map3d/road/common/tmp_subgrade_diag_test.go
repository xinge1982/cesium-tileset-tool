package common

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestDiagnoseSubgradeTriangulationPathForKnownIDs(t *testing.T) {
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
		roadID := int64FromAnyDiag(f.Properties["road_id"])
		if roadID == 0 {
			roadID = int64FromAnyDiag(f.Properties["id"])
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

		rawRing := rings[0]
		rawOuter := dedupeClosedRing(rawRing.Outer)
		cleanRing := cleanSubgradeLocalRings(rawRing, 0.01)
		cleanOuter := dedupeClosedRing(cleanRing.Outer)
		steiner := subgradePoly2TriSteinerPoints(rawOuter)

		path := "plain-fallback"
		rawPoly2TriOK := false
		cleanPoly2TriOK := false
		longest := 0.0
		minAngle := 0.0
		if len(rawOuter) >= 3 && !hasSelfIntersectionXZ(rawOuter) {
			if pos, idx, ok := triangulateSubgradeByPoly2Tri(rawRing); ok && len(idx) >= 3 {
				path = "raw-poly2tri"
				rawPoly2TriOK = true
				longest = maxTriangleEdgeXZDiag(pos, idx)
				minAngle = minTriangleAngleDegDiag(pos, idx)
			}
		}
		if !rawPoly2TriOK {
			if pos, idx, ok := triangulateSubgradeByPoly2Tri(cleanRing); ok && len(idx) >= 3 {
				path = "clean-poly2tri"
				cleanPoly2TriOK = true
				longest = maxTriangleEdgeXZDiag(pos, idx)
				minAngle = minTriangleAngleDegDiag(pos, idx)
			}
		}
		if _, _, err := triangulatePreferredSubgradeSurfaceRing(rawRing); err != nil {
			t.Fatalf("preferred triangulation road_id=%d: %v", roadID, err)
		}

		t.Logf(
			"road_id=%d raw_points=%d clean_points=%d steiner_points=%d raw_self_intersection=%v clean_self_intersection=%v raw_poly2tri=%v clean_poly2tri=%v path=%s max_edge=%.2f min_angle=%.2f",
			roadID,
			len(rawOuter),
			len(cleanOuter),
			len(steiner),
			hasSelfIntersectionXZ(rawOuter),
			hasSelfIntersectionXZ(cleanOuter),
			rawPoly2TriOK,
			cleanPoly2TriOK,
			path,
			longest,
			minAngle,
		)
	}

	for id, seen := range targetIDs {
		if !seen {
			t.Fatalf("road_id=%d not found in road_lj.geojson", id)
		}
	}
}

func int64FromAnyDiag(v any) int64 {
	switch x := v.(type) {
	case string:
		id, _ := strconv.ParseInt(x, 10, 64)
		return id
	default:
		return int64FromAny(v)
	}
}

func maxTriangleEdgeXZDiag(pos [][3]float32, idx []uint32) float64 {
	maxLen := 0.0
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
		for _, l := range []float64{
			edgeLenXZDiag(a, b),
			edgeLenXZDiag(b, c),
			edgeLenXZDiag(c, a),
		} {
			if l > maxLen {
				maxLen = l
			}
		}
	}
	return maxLen
}

func minTriangleAngleDegDiag(pos [][3]float32, idx []uint32) float64 {
	if len(idx) < 3 {
		return 0
	}
	minA := 180.0
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
		angles := triAnglesDegDiag(a, b, c)
		for _, ang := range angles {
			if ang < minA {
				minA = ang
			}
		}
	}
	if minA == 180.0 {
		return 0
	}
	return minA
}

func triAnglesDegDiag(a, b, c [3]float32) [3]float64 {
	ab := edgeLenXZDiag(a, b)
	bc := edgeLenXZDiag(b, c)
	ca := edgeLenXZDiag(c, a)
	if ab <= 1e-6 || bc <= 1e-6 || ca <= 1e-6 {
		return [3]float64{}
	}
	return [3]float64{
		lawCosDegDiag(bc, ca, ab),
		lawCosDegDiag(ca, ab, bc),
		lawCosDegDiag(ab, bc, ca),
	}
}

func edgeLenXZDiag(a, b [3]float32) float64 {
	return math.Hypot(float64(a[0]-b[0]), float64(a[2]-b[2]))
}

func lawCosDegDiag(side1, side2, opposite float64) float64 {
	den := 2 * side1 * side2
	if den <= 1e-9 {
		return 0
	}
	c := (side1*side1 + side2*side2 - opposite*opposite) / den
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return math.Acos(c) * 180 / math.Pi
}

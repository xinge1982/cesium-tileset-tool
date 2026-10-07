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

func TestSubgradeExperimentalForKnownIDs(t *testing.T) {
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
		14251000008: false,
		14251000035: false,
		14251000005: false,
		14251000007: false,
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

		posExp, idxExp, okExp := TriangulateSubgradeExperimentalAdaptive(ring, roadID)
		posPlain, idxPlain, err := triangulatePlainSurfaceRing(ring)
		if err != nil {
			t.Fatalf("plain triangulation road_id=%d: %v", roadID, err)
		}
		idxOpt := optimizeTrianglesXZ(posPlain, append([]uint32(nil), idxPlain...))

		t.Logf("road_id=%d experimental_ok=%v exp_tri=%d plain_tri=%d opt_tri=%d minAngle_exp=%.2f minAngle_plain=%.2f minAngle_opt=%.2f areaExp=%.2f areaPlain=%.2f areaOpt=%.2f",
			roadID,
			okExp,
			len(idxExp)/3,
			len(idxPlain)/3,
			len(idxOpt)/3,
			minTriangleAngleDegExperimental(posExp, idxExp),
			minTriangleAngleDegExperimental(posPlain, idxPlain),
			minTriangleAngleDegExperimental(posPlain, idxOpt),
			trianglesAreaXZ(posExp, idxExp),
			trianglesAreaXZ(posPlain, idxPlain),
			trianglesAreaXZ(posPlain, idxOpt),
		)
	}

	for id, seen := range targetIDs {
		if !seen {
			t.Fatalf("road_id=%d not found in road_lj.geojson", id)
		}
	}
}

func TestTriangulateSubgradeSurfaceCDTCandidateForKnownIDs(t *testing.T) {
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

		pos, idx, ok := triangulateSubgradeSurfaceCDTCandidate(rings[0])
		if !ok || len(idx) < 3 {
			t.Fatalf("cdt candidate failed road_id=%d", roadID)
		}
		t.Logf("road_id=%d cdt_tri=%d area=%.2f", roadID, len(idx)/3, trianglesAreaXZ(pos, idx))
	}

	for id, seen := range targetIDs {
		if !seen {
			t.Fatalf("road_id=%d not found in road_lj.geojson", id)
		}
	}
}

func int64FromAnyExperimental(v any) int64 {
	switch x := v.(type) {
	case string:
		id, _ := strconv.ParseInt(x, 10, 64)
		return id
	default:
		return int64FromAny(v)
	}
}

func minTriangleAngleDegExperimental(pos [][3]float32, idx []uint32) float64 {
	if len(idx) < 3 {
		return 0
	}
	minA := 180.0
	for i := 0; i+2 < len(idx); i += 3 {
		a := idx[i]
		b := idx[i+1]
		c := idx[i+2]
		if int(a) >= len(pos) || int(b) >= len(pos) || int(c) >= len(pos) {
			continue
		}
		angles := triAnglesDegExperimental(pos[a], pos[b], pos[c])
		for _, ang := range angles {
			if ang < minA {
				minA = ang
			}
		}
	}
	if minA == 180 {
		return 0
	}
	return minA
}

func triAnglesDegExperimental(a, b, c [3]float32) [3]float64 {
	ab := edgeLenXZExperimental(a, b)
	bc := edgeLenXZExperimental(b, c)
	ca := edgeLenXZExperimental(c, a)
	if ab <= 1e-6 || bc <= 1e-6 || ca <= 1e-6 {
		return [3]float64{0, 0, 0}
	}
	angA := lawCosDegExperimental(bc, ca, ab)
	angB := lawCosDegExperimental(ca, ab, bc)
	angC := lawCosDegExperimental(ab, bc, ca)
	return [3]float64{angA, angB, angC}
}

func edgeLenXZExperimental(a, b [3]float32) float64 {
	dx := float64(a[0] - b[0])
	dz := float64(a[2] - b[2])
	return math.Hypot(dx, dz)
}

func lawCosDegExperimental(side1, side2, opposite float64) float64 {
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

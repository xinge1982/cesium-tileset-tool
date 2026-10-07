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

func TestSubgradeDirectionalForKnownIDs(t *testing.T) {
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
		14251000009: false,
	}

	for _, f := range fc.Features {
		if f.Geometry == nil {
			continue
		}
		roadID := int64FromAnyExt(f.Properties["road_id"])
		if roadID == 0 {
			roadID = int64FromAnyExt(f.Properties["id"])
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

		posDir, idxDir, okDir := triangulateSubgradeDirectionalByPCA(ring)
		cl, okCL := estimateLocalCenterlineFromOuterWithMinRatio(dedupeClosedRing(ring.Outer), 1.2)
		var okSplit, okChain, okSample bool
		var splitTri int
		if okCL {
			left, right, ok := splitRoadBoundaryByCenterline(dedupeClosedRing(ring.Outer), cl)
			okSplit = ok
			if ok {
				p2, i2, ok2 := triangulateBoundaryChains(left, right, cl.Points)
				okChain = ok2
				if ok2 {
					splitTri = len(i2) / 3
					_ = p2
				}
				_, _, ok3 := triangulateSurfaceByCenterlineSampling(ring, cl)
				okSample = ok3
			}
		}
		posPlain, idxPlain, err := triangulatePlainSurfaceRing(ring)
		if err != nil {
			t.Fatalf("plain triangulation road_id=%d: %v", roadID, err)
		}

		minDir := minTriangleAngleDeg(posDir, idxDir)
		minPlain := minTriangleAngleDeg(posPlain, idxPlain)
		dirArea := trianglesAreaXZ(posDir, idxDir)
		plainArea := trianglesAreaXZ(posPlain, idxPlain)
		polyArea := polygonAreaXZ(dedupeClosedRing(ring.Outer))
		t.Logf("road_id=%d directional_ok=%v dir_tri=%d plain_tri=%d minAngle_dir=%.2f minAngle_plain=%.2f dirArea=%.2f plainArea=%.2f polyArea=%.2f okCL=%v okSplit=%v okChain=%v splitTri=%d okSample=%v",
			roadID, okDir, len(idxDir)/3, len(idxPlain)/3, minDir, minPlain, dirArea, plainArea, polyArea, okCL, okSplit, okChain, splitTri, okSample)
	}

	for id, seen := range targetIDs {
		if !seen {
			t.Fatalf("road_id=%d not found in road_lj.geojson", id)
		}
	}
}

func int64FromAnyExt(v any) int64 {
	switch x := v.(type) {
	case string:
		id, _ := strconv.ParseInt(x, 10, 64)
		return id
	default:
		return int64FromAny(v)
	}
}

func minTriangleAngleDeg(pos [][3]float32, idx []uint32) float64 {
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
		angles := triAnglesDeg(pos[a], pos[b], pos[c])
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

func triAnglesDeg(a, b, c [3]float32) [3]float64 {
	ab := edgeLenXZ(a, b)
	bc := edgeLenXZ(b, c)
	ca := edgeLenXZ(c, a)
	if ab <= 1e-6 || bc <= 1e-6 || ca <= 1e-6 {
		return [3]float64{0, 0, 0}
	}
	angA := lawCosDeg(bc, ca, ab)
	angB := lawCosDeg(ca, ab, bc)
	angC := lawCosDeg(ab, bc, ca)
	return [3]float64{angA, angB, angC}
}

func edgeLenXZ(a, b [3]float32) float64 {
	dx := float64(a[0] - b[0])
	dz := float64(a[2] - b[2])
	return math.Hypot(dx, dz)
}

func lawCosDeg(side1, side2, opposite float64) float64 {
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

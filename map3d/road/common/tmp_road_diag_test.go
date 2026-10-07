package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func tmpFeatureInt64(props map[string]interface{}, key string) (int64, bool) {
	if props == nil {
		return 0, false
	}
	v, ok := props[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case string:
		var out int64
		_, err := fmt.Sscan(n, &out)
		return out, err == nil
	default:
		return 0, false
	}
}

func TestTriangulateRoadSurfaceRing_UsesPoly2TriFallbackForLongCurvedRoads(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	roadPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "road_face_wgs84.geojson"))
	centerlinePath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "road_line.geojson"))

	roadData, err := os.ReadFile(roadPath)
	if err != nil {
		t.Fatal(err)
	}
	roadFC := &geojson.FeatureCollection{}
	if err := json.Unmarshal(roadData, roadFC); err != nil {
		t.Fatal(err)
	}
	lineData, err := os.ReadFile(centerlinePath)
	if err != nil {
		t.Fatal(err)
	}
	lineFC := &geojson.FeatureCollection{}
	if err := json.Unmarshal(lineData, lineFC); err != nil {
		t.Fatal(err)
	}

	ids := map[int64]bool{14241000260: true, 14241000254: true}
	centerByID := map[int64]*geojson.Feature{}
	for _, f := range lineFC.Features {
		if f == nil || f.Properties == nil {
			continue
		}
		if v, ok := tmpFeatureInt64(f.Properties, "id"); ok && ids[v] {
			centerByID[v] = f
		}
	}

	for _, f := range roadFC.Features {
		if f == nil || f.Properties == nil {
			continue
		}
		id, ok := tmpFeatureInt64(f.Properties, "id")
		if !ok || !ids[id] {
			continue
		}
		surface := SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields(f.Properties),
			},
		}
		basePoint, region := calcTileContextForGeom(surface.Geom)
		cp, err := NewCoordinatePipeline(TileContext{
			Region:    region,
			Center:    [3]float64{basePoint[0], basePoint[1], 0},
			BasePoint: basePoint,
			SRID:      4326,
			UseENU:    true,
		})
		if err != nil {
			t.Fatalf("road %d pipeline: %v", id, err)
		}
		rings, err := cp.ToLocalSurface(surface)
		if err != nil {
			t.Fatalf("road %d rings: %v", id, err)
		}
		if len(rings) == 0 {
			t.Fatalf("road %d no rings", id)
		}
		ring := rings[0]
		outer := dedupeClosedRing(ring.Outer)
		t.Logf("road=%d outerPts=%d holes=%d area=%.2f", id, len(outer), len(ring.Holes), polygonAreaXZ(outer))

		var cl LocalLine
		hasCL := false
		if lf := centerByID[id]; lf != nil {
			lines, err := cp.ToLocalLine(LineFeature{
				FeatureInput: FeatureInput{
					BuildType: BuildTypeRoadMarkingSolid,
					Geom:      lf.Geometry,
					Fields:    FeatureFields(lf.Properties),
				},
			})
			if err == nil && len(lines) > 0 {
				cl = lines[0]
				hasCL = true
			}
		}
		t.Logf("road=%d hasCL=%v clPts=%d match=%v", id, hasCL, len(cl.Points), hasCL && centerlineMatchesRing(outer, cl.Points))
		if hasCL {
			pos, idx, ok := triangulateRoadSurfaceByCenterline(ring, cl)
			fit := false
			area := 0.0
			if ok {
				fit = trianglesFitPolygonXZ(outer, pos, idx)
				area = trianglesAreaXZ(pos, idx)
			}
			t.Logf("road=%d centerline ok=%v tris=%d area=%.2f fit=%v", id, ok, len(idx)/3, area, fit)
		}

		posP2, idxP2, okP2 := triangulateSubgradeByPoly2Tri(ring)
		fitP2 := false
		areaP2 := 0.0
		if okP2 {
			fitP2 = trianglesFitPolygonXZ(outer, posP2, idxP2)
			areaP2 = trianglesAreaXZ(posP2, idxP2)
		}
		t.Logf("road=%d poly2tri ok=%v tris=%d area=%.2f fit=%v", id, okP2, len(idxP2)/3, areaP2, fitP2)

		posE, idxE, err := triangulatePlainSurfaceRing(ring)
		if err != nil {
			t.Fatalf("road=%d earcut: %v", id, err)
		}
		t.Logf("road=%d earcut tris=%d area=%.2f fit=%v", id, len(idxE)/3, trianglesAreaXZ(posE, idxE), trianglesFitPolygonXZ(outer, posE, idxE))

		pos, idx, err := triangulateRoadSurfaceRing(ring, cl, hasCL)
		if err != nil {
			t.Fatalf("road=%d surface ring: %v", id, err)
		}
		if !trianglesFitPolygonXZ(outer, pos, idx) {
			t.Fatalf("road=%d surface ring does not fit polygon", id)
		}
		if !okP2 {
			t.Fatalf("road=%d poly2tri candidate unavailable", id)
		}
		if len(idx)/3 != len(idxP2)/3 {
			t.Fatalf("road=%d expected poly2tri fallback tris=%d, got=%d", id, len(idxP2)/3, len(idx)/3)
		}
		if len(idx)/3 <= len(idxE)/3 {
			t.Fatalf("road=%d expected denser poly2tri fallback than earcut, got result=%d earcut=%d", id, len(idx)/3, len(idxE)/3)
		}
	}
}

package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestTmpRawExperimentalChoiceFor007009(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_lj.geojson"))
	buf, err := os.ReadFile(roadPath)
	if err != nil {
		t.Fatal(err)
	}
	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{14251000007, 14251000009} {
		for _, f := range fc.Features {
			if f.Geometry == nil {
				continue
			}
			roadID := int64FromAnyExperimental(f.Properties["road_id"])
			if roadID == 0 {
				roadID = int64FromAnyExperimental(f.Properties["id"])
			}
			if roadID != want {
				continue
			}
			surface := SurfaceFeature{FeatureInput: FeatureInput{BuildType: BuildTypeRoadSubgrade, Geom: f.Geometry, Fields: FeatureFields{"road_id": roadID}}}
			basePoint, region := calcTileContextForGeom(surface.Geom)
			p, err := NewCoordinatePipeline(TileContext{Region: region, Center: [3]float64{basePoint[0], basePoint[1], 0}, BasePoint: basePoint, SRID: 4326, UseENU: true})
			if err != nil {
				t.Fatal(err)
			}
			rings, err := p.ToLocalSurface(surface)
			if err != nil || len(rings) == 0 {
				t.Fatal(err)
			}
			ring := rings[0]
			outer := dedupeClosedRing(ring.Outer)
			posP2, idxP2, okP2 := triangulateSubgradeByPoly2Tri(ring)
			fitP2 := false
			if okP2 {
				fitP2 = trianglesFitPolygonXZ(outer, posP2, idxP2)
			}
			_, idxExp, okExp := TriangulateSubgradeExperimentalAdaptive(ring, roadID)
			t.Logf("road_id=%d rawPts=%d poly2tri_ok=%v poly2tri_tri=%d fit=%v exp_ok=%v exp_tri=%d", roadID, len(outer), okP2, len(idxP2)/3, fitP2, okExp, len(idxExp)/3)
			break
		}
	}
}

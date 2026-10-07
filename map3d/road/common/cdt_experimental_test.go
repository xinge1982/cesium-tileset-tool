package common

import (
	"path/filepath"
	"testing"
)

func TestTriangulateSurfaceByCDTExperimental_SucceedsForRoad14241000124(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_face_wgs84.geojson"))
	roadFC := readFeatureCollectionForStripTest(t, roadPath)

	const roadID = int64(14241000124)
	var surfaceFeature SurfaceFeature
	found := false
	for _, f := range roadFC.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		surfaceFeature = SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("road_id=%d not found", roadID)
	}

	basePoint, region := calcTileContextForGeom(surfaceFeature.Geom)
	pipeline, err := NewCoordinatePipeline(TileContext{
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}
	localRings, err := pipeline.ToLocalSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("localize surface: %v", err)
	}
	if len(localRings) == 0 {
		t.Fatal("expected localized surface ring")
	}
	ring := localRings[0]
	pos, idx, ok, err := triangulateSurfaceByCDTExperimental(ring)
	if err != nil {
		t.Fatalf("cdt experimental: %v", err)
	}
	if !ok {
		t.Fatalf("expected cdt triangulation to succeed")
	}
	if len(idx) < 3 {
		t.Fatalf("expected triangles")
	}
	if !trianglesFitPolygonXZ(dedupeClosedRing(ring.Outer), pos, idx) {
		t.Fatalf("cdt result does not fit polygon")
	}
}

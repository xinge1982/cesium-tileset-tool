package common

import (
	"math"
	"testing"

	"github.com/twpayne/go-geom"
)

func TestCalcBasePointAndRegionFromGeometries(t *testing.T) {
	line := geom.NewLineStringFlat(geom.XYZ, []float64{
		120.10, 31.20, 5,
		120.30, 31.40, 6,
	})
	poly := geom.NewPolygonFlat(geom.XYZ, []float64{
		120.00, 31.10, 0,
		120.40, 31.10, 0,
		120.40, 31.50, 0,
		120.00, 31.50, 0,
		120.00, 31.10, 0,
	}, []int{15})

	basePoint, region, err := CalcBasePointAndRegionFromGeometries(line, poly)
	if err != nil {
		t.Fatalf("calc basepoint and region: %v", err)
	}
	if len(basePoint) != 3 {
		t.Fatalf("unexpected basepoint length: %d", len(basePoint))
	}
	if math.Abs(basePoint[0]-120.20) > 1e-9 || math.Abs(basePoint[1]-31.30) > 1e-9 {
		t.Fatalf("unexpected basepoint: %+v", basePoint)
	}
	wantRegion := [6]float64{120.00, 31.10, 120.40, 31.50, 0, 0}
	if region != wantRegion {
		t.Fatalf("unexpected region: got=%v want=%v", region, wantRegion)
	}
}

func TestResolveTileContextAuto(t *testing.T) {
	lines := []LineFeature{
		{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadMarking,
				Geom: geom.NewLineStringFlat(geom.XYZ, []float64{
					120.10, 31.20, 0,
					120.30, 31.40, 0,
				}),
				Fields: FeatureFields{"id": 1},
			},
		},
	}
	surfaces := []SurfaceFeature{
		{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom: geom.NewPolygonFlat(geom.XYZ, []float64{
					120.00, 31.10, 0,
					120.40, 31.10, 0,
					120.40, 31.50, 0,
					120.00, 31.50, 0,
					120.00, 31.10, 0,
				}, []int{15}),
				Fields: FeatureFields{"id": 2},
			},
		},
	}

	ctx, err := ResolveTileContextAuto(TileContext{
		TileID: "auto-test",
		SRID:   4326,
		UseENU: true,
	}, nil, lines, surfaces)
	if err != nil {
		t.Fatalf("resolve tile context: %v", err)
	}
	if len(ctx.BasePoint) != 3 {
		t.Fatalf("unexpected basepoint: %+v", ctx.BasePoint)
	}
	if math.Abs(ctx.BasePoint[0]-120.20) > 1e-9 || math.Abs(ctx.BasePoint[1]-31.30) > 1e-9 {
		t.Fatalf("unexpected basepoint: %+v", ctx.BasePoint)
	}
	wantRegion := [6]float64{120.00, 31.10, 120.40, 31.50, 0, 0}
	if ctx.Region != wantRegion {
		t.Fatalf("unexpected region: got=%v want=%v", ctx.Region, wantRegion)
	}
	if ctx.Center != [3]float64{120.20, 31.30, 0} {
		t.Fatalf("unexpected center: %+v", ctx.Center)
	}
}

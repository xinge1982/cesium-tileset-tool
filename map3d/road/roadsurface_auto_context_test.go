package road

import (
	"testing"

	"github.com/twpayne/go-geom"
)

func TestRoadTileBuilderAutoResolvesBasePoint(t *testing.T) {
	builder, err := NewRoadTileBuilder(TileContext{
		TileID: "auto-builder-test",
		SRID:   4326,
		UseENU: true,
	}, nil)
	if err != nil {
		t.Fatalf("new road tile builder: %v", err)
	}

	builder.AddSurfaceFeatures(SurfaceFeature{
		FeatureInput: FeatureInput{
			BuildType: BuildTypeRoadSurface,
			Geom: geom.NewPolygonFlat(geom.XYZ, []float64{
				120.00, 31.10, 0,
				120.40, 31.10, 0,
				120.40, 31.50, 0,
				120.00, 31.50, 0,
				120.00, 31.10, 0,
			}, []int{15}),
			Fields: FeatureFields{"id": 1},
		},
	})

	if err := builder.validateInputs(); err != nil {
		t.Fatalf("validate inputs: %v", err)
	}
	if len(builder.ctx.BasePoint) != 3 {
		t.Fatalf("unexpected basepoint: %+v", builder.ctx.BasePoint)
	}
	if builder.ctx.BasePoint[0] != 120.20 || builder.ctx.BasePoint[1] != 31.30 {
		t.Fatalf("unexpected basepoint: %+v", builder.ctx.BasePoint)
	}
	if builder.ctx.Region != [6]float64{120.00, 31.10, 120.40, 31.50, 0, 0} {
		t.Fatalf("unexpected region: %+v", builder.ctx.Region)
	}
	if builder.ctx.Center != [3]float64{120.20, 31.30, 0} {
		t.Fatalf("unexpected center: %+v", builder.ctx.Center)
	}
}

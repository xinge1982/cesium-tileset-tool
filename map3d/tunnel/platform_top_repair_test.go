package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"math"
	"strings"
	"testing"
)

// 原始 Feature 555 的米制近似，交叉边高差约 1.77 毫米。
func crossingPlatformFixture() [][3]float32 {
	return [][3]float32{{0, 0, 0}, {0.20983823, -0.00177002, 0.05965655}, {-0.14775681, -0.00177002, 0.99480355}, {-0.12712551, 0, 1.00066899}}
}

func TestPlatformTopCrossingRepair(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		input := crossingPlatformFixture()
		if reverse {
			for i, j := 0, len(input)-1; i < j; i, j = i+1, j-1 {
				input[i], input[j] = input[j], input[i]
			}
		}
		if _, err := mgltf.CDTRingKeepWinding(input); err == nil {
			t.Fatal("fixture must reproduce original CDT rejection")
		}
		output, indices, repaired, err := repairPlatformTopCrossing(input)
		if err != nil || !repaired {
			t.Fatalf("repair %v %v", repaired, err)
		}
		if len(output) != 5 || len(indices) != 6 {
			t.Fatalf("mesh size %d/%d", len(output), len(indices))
		}
		for i := range input {
			if input[i] != output[i] {
				t.Fatal("original vertex moved")
			}
		}
		var net float32
		for i, a := range input {
			b := input[(i+1)%4]
			net += a[0]*b[2] - b[0]*a[2]
		}
		for i := 0; i < len(indices); i += 3 {
			a, b, c := output[indices[i]], output[indices[i+1]], output[indices[i+2]]
			signed := (b[0]-a[0])*(c[2]-a[2]) - (b[2]-a[2])*(c[0]-a[0])
			if signed*net <= 0 {
				t.Fatal("triangle does not follow net winding")
			}
		}
	}
	input := crossingPlatformFixture()
	input[1][1] = -0.2
	input[2][1] = -0.2
	if _, _, _, err := repairPlatformTopCrossing(input); err == nil || !strings.Contains(err.Error(), "tolerance") {
		t.Fatalf("large vertical mismatch accepted: %v", err)
	}
}

func TestRepairedPlatformCenterlineUV(t *testing.T) {
	doc := gltf.NewDocument()
	reported := -1
	line := &tunnelCenterline{points: [][3]float32{{-1, 0, 0}, {2, 0, 0}}, stations: []float64{0, 3}}
	p, err := TunnelPlatformMultiPolygonPrimitive(doc, [][][3]float32{crossingPlatformFixture()}, "platform_top", 0, mgltf.UvsParams{}, TunnelPlatformOptions{Centerline: line, TileSize: 0.5, SourceFeatures: []int{555}, OnRepair: func(feature int) { reported = feature }})
	if err != nil {
		t.Fatal(err)
	}
	if reported != 555 {
		t.Fatal("missing original feature index")
	}
	positions, err := modeler.ReadPosition(doc, doc.Accessors[p.Attributes[gltf.POSITION]], nil)
	if err != nil {
		t.Fatal(err)
	}
	uv, err := modeler.ReadTextureCoord(doc, doc.Accessors[p.Attributes[gltf.TEXCOORD_0]], nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 5 || len(uv) != 5 {
		t.Fatal("intersection missing from UV mesh")
	}
	hasZero := false
	for i, pos := range positions {
		s, _ := line.coordinates(pos)
		if uv[i][0] != float32(s/0.5) || uv[i][1] < 0 {
			t.Fatal("intersection UV not aligned with platform reference")
		}
		if uv[i][1] == 0 {
			hasZero = true
		}
	}
	if !hasZero {
		t.Fatal("left boundary does not start at zero")
	}

}

func TestPlatformCrossingConfigurableTolerance(t *testing.T) {
	input := crossingPlatformFixture()
	input[1][1] = -0.010925293
	input[2][1] = -0.010925293
	if _, _, _, err := repairPlatformTopCrossing(input); err == nil {
		t.Fatal("default 5mm tolerance changed")
	}
	positions, indices, repaired, err := repairPlatformTopCrossingWithTolerance(input, 0.012)
	if err != nil || !repaired || len(positions) != 5 || len(indices) != 6 {
		t.Fatalf("12mm repair failed: %v", err)
	}
	doc := gltf.NewDocument()
	if _, err := TunnelPlatformMultiPolygonPrimitive(doc, [][][3]float32{input}, "platform_top", 0, mgltf.UvsParams{}, TunnelPlatformOptions{TileSize: 0.5, RepairTolerance: 0.012}); err != nil {
		t.Fatal("primitive did not use configured tolerance", err)
	}
	for _, tolerance := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, _, _, err := repairPlatformTopCrossingWithTolerance(input, tolerance); err == nil {
			t.Fatal("invalid tolerance accepted")
		}
	}
}

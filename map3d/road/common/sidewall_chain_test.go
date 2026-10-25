package common

import (
	"math"
	"reflect"
	"testing"

	"github.com/twpayne/go-geom"
)

func chainTestFeature(id int, direction int, coords ...geom.Coord) LineFeature {
	return LineFeature{FeatureInput: FeatureInput{BuildType: BuildTypeBarrierWave, Fields: FeatureFields{"id": id, "bxdl": 5, "feature_type": "wave_two", "direction": direction}, Geom: geom.NewLineString(geom.XYZ).MustSetCoords(coords)}}
}
func chainTestPrepare(t *testing.T, features []LineFeature) []LineFeature {
	t.Helper()
	cp, err := NewCoordinatePipeline(TileContext{BasePoint: []float64{0, 0, 0}, SRID: 3857})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSidewallChains(features, cp, SidewallChainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}
func chainTestStations(t *testing.T, features []LineFeature) [][][3]float32 {
	t.Helper()
	cp, _ := NewCoordinatePipeline(TileContext{BasePoint: []float64{0, 0, 0}, SRID: 3857})
	out := make([][][3]float32, len(features))
	for i, f := range features {
		lines, err := cp.ToLocalLine(f)
		if err != nil {
			t.Fatal(err)
		}
		for j, line := range lines {
			tr, err := guardrailChainSupportTransforms(line, &f.ChainSampling[j])
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range tr {
				out[i] = append(out[i], v.Translation)
			}
		}
	}
	return out
}

func TestSidewallChainContinuousSpacing(t *testing.T) {
	features := []LineFeature{chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{10.3, 0, 0}), chainTestFeature(2, 2, geom.Coord{10.3, 0, 0}, geom.Coord{17.5, 0, 0})}
	features[0].Fields["startoffset"] = 123.0
	before := features[0].Geom.FlatCoords()[0]
	p := chainTestPrepare(t, features)
	if p[0].ChainSampling[0].IncludeEnd || !p[1].ChainSampling[0].IncludeEnd {
		t.Fatal("interior/terminal ownership")
	}
	if math.Abs(p[1].ChainSampling[0].StartOffset-10.3) > 1e-5 {
		t.Fatal("offset")
	}
	positions := chainTestStations(t, p)
	expected := [][]float32{{0, 4, 8}, {12, 16, 17.5}}
	for i := range positions {
		if len(positions[i]) != len(expected[i]) {
			t.Fatalf("unexpected posts %v", positions)
		}
		for j, v := range positions[i] {
			if math.Abs(float64(v[0]-expected[i][j])) > 1e-5 {
				t.Fatalf("unexpected posts %v", positions)
			}
		}
	}
	if features[0].ChainSampling != nil || features[0].Fields["startoffset"] != 123.0 || features[0].Geom.FlatCoords()[0] != before {
		t.Fatal("mutated input")
	}
	// Stable source IDs, not input order, determine chain roots and offsets.
	q := chainTestPrepare(t, []LineFeature{features[1], features[0]})
	if !reflect.DeepEqual(p[0].ChainSampling, q[1].ChainSampling) || !reflect.DeepEqual(p[1].ChainSampling, q[0].ChainSampling) {
		t.Fatal("input order changed offsets")
	}
}
func TestSidewallChainReverseAndExactJoint(t *testing.T) {
	p := chainTestPrepare(t, []LineFeature{chainTestFeature(1, 1, geom.Coord{0, 0, 0}, geom.Coord{8, 0, 0}), chainTestFeature(2, 1, geom.Coord{8, 0, 0}, geom.Coord{16, 0, 0})})
	posts := chainTestStations(t, p)
	seen := map[float32]bool{}
	for _, part := range posts {
		for _, v := range part {
			if seen[v[0]] {
				t.Fatalf("duplicate joint: %v", posts)
			}
			seen[v[0]] = true
		}
	}
	if len(seen) != 5 || !seen[0] || !seen[4] || !seen[8] || !seen[12] || !seen[16] {
		t.Fatal(posts)
	}
	if !p[0].ChainSampling[0].Reverse || p[1].ChainSampling[0].StartOffset != 0 || p[0].ChainSampling[0].StartOffset != 8 {
		t.Fatal("logical direction")
	}
}
func TestSidewallChainIsolation(t *testing.T) {
	for _, kind := range []string{"height", "type", "kind", "direction", "missing_z", "gap", "turn"} {
		t.Run(kind, func(t *testing.T) {
			a := chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{8, 0, 0})
			b := chainTestFeature(2, 2, geom.Coord{8, 0, 0}, geom.Coord{16, 0, 0})
			switch kind {
			case "height":
				b.Geom = geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{8, 0, 3}, {16, 0, 3}})
			case "type":
				b.Fields["bxdl"] = 6
			case "kind":
				b.Fields["feature_type"] = "wave_three"
			case "direction":
				b.Fields["direction"] = 1
			case "missing_z":
				b.Geom = geom.NewLineString(geom.XY).MustSetCoords([]geom.Coord{{8, 0}, {16, 0}})
			case "gap":
				b.Geom = geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{8.2, 0, 0}, {16, 0, 0}})
			case "turn":
				b.Geom = geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{8, 0, 0}, {8, 8, 0}})
			}
			p := chainTestPrepare(t, []LineFeature{a, b})
			if p[0].ChainSampling[0].ChainID == p[1].ChainSampling[0].ChainID {
				t.Fatalf("connected %s", kind)
			}
		})
	}
}
func TestSidewallChainBranchAndPlanCrossing(t *testing.T) {
	a := chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{8, 0, 0})
	b := chainTestFeature(2, 2, geom.Coord{8, 0, 0}, geom.Coord{16, 1, 0})
	c := chainTestFeature(3, 2, geom.Coord{8, 0, 0}, geom.Coord{16, -1, 0})
	p := chainTestPrepare(t, []LineFeature{a, b, c})
	for _, f := range p {
		if !f.ChainSampling[0].IncludeEnd || f.ChainSampling[0].StartOffset != 0 {
			t.Fatal("arbitrary branch selected")
		}
	}
	// Crossing interiors at different levels are never graph nodes.
	p = chainTestPrepare(t, []LineFeature{chainTestFeature(1, 2, geom.Coord{-8, 0, 0}, geom.Coord{8, 0, 0}), chainTestFeature(2, 2, geom.Coord{0, -8, 4}, geom.Coord{0, 8, 4})})
	if p[0].ChainSampling[0].ChainID == p[1].ChainSampling[0].ChainID {
		t.Fatal("joined interior crossing")
	}
}
func TestSidewallChainCycleAndMultiPart(t *testing.T) {
	// A square permits its 90-degree corners with an explicit turn policy.
	cp, _ := NewCoordinatePipeline(TileContext{BasePoint: []float64{0, 0, 0}, SRID: 3857})
	points := []geom.Coord{{0, 0, 0}, {8, 0, 0}, {8, 8, 0}, {0, 8, 0}, {0, 0, 0}}
	features := []LineFeature{}
	for i := 0; i < 4; i++ {
		features = append(features, chainTestFeature(i+1, 2, points[i], points[i+1]))
	}
	p, err := PrepareSidewallChains(features, cp, SidewallChainOptions{MaxTurnDegrees: 100})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[[3]float32]bool{}
	for _, part := range chainTestStations(t, p) {
		for _, v := range part {
			if seen[v] {
				t.Fatal("duplicate cycle seam")
			}
			seen[v] = true
		}
	}
	if len(seen) != 8 {
		t.Fatalf("cycle posts %v", seen)
	}
	for _, f := range p {
		if f.ChainSampling[0].IncludeEnd {
			t.Fatal("cycle end cap")
		}
	}
	reversed := []LineFeature{features[3], features[2], features[1], features[0]}
	q, err := PrepareSidewallChains(reversed, cp, SidewallChainOptions{MaxTurnDegrees: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i := range p {
		if !reflect.DeepEqual(p[i].ChainSampling, q[3-i].ChainSampling) {
			t.Fatal("cycle zero moved")
		}
	}
	f := chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{8, 0, 0})
	f.Geom = geom.NewMultiLineString(geom.XYZ).MustSetCoords([][]geom.Coord{{{0, 0, 0}, {10.3, 0, 0}}, {{10.3, 0, 0}, {17.5, 0, 0}}})
	m := chainTestPrepare(t, []LineFeature{f})
	if len(m[0].ChainSampling) != 2 || math.Abs(m[0].ChainSampling[1].StartOffset-10.3) > 1e-5 {
		t.Fatal("multipart phase reset")
	}
}

func TestSidewallChainSingleClosedAndShortPart(t *testing.T) {
	f := chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{8, 0, 0}, geom.Coord{8, 8, 0}, geom.Coord{0, 8, 0}, geom.Coord{0, 0, 0})
	p := chainTestPrepare(t, []LineFeature{f})
	if p[0].ChainSampling[0].IncludeEnd {
		t.Fatal("single closed part has an end post")
	}
	seen := map[[3]float32]bool{}
	for _, v := range chainTestStations(t, p)[0] {
		if seen[v] {
			t.Fatal("duplicate closed seam")
		}
		seen[v] = true
	}
	if len(seen) != 8 {
		t.Fatal(seen)
	}
	// The middle part contains no multiple of four and must emit zero supports.
	p = chainTestPrepare(t, []LineFeature{chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{5, 0, 0}), chainTestFeature(2, 2, geom.Coord{5, 0, 0}, geom.Coord{6, 0, 0}), chainTestFeature(3, 2, geom.Coord{6, 0, 0}, geom.Coord{9, 0, 0})})
	posts := chainTestStations(t, p)
	if len(posts[1]) != 0 || len(posts[0]) != 2 || len(posts[2]) != 2 {
		t.Fatal(posts)
	}
}

func TestSidewallChainShortOpenNotClosed(t *testing.T) {
	p := chainTestPrepare(t, []LineFeature{chainTestFeature(1, 2, geom.Coord{0, 0, 0}, geom.Coord{0.04, 0, 0})})
	if !p[0].ChainSampling[0].IncludeEnd || len(chainTestStations(t, p)[0]) != 2 {
		t.Fatal("short open line treated as loop")
	}
}

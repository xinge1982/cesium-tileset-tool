package common

import (
	"math"
	"testing"
)

// Check derived ground-relative height and the minimum permitted diameter.
func TestGuardrailPostOptionsDefaultsAndOverrides(t *testing.T) {
	for _, spec := range []struct{ base, panel, want float32 }{{.445, .310, .750}, {.444, .506, .847}} {
		p, err := resolveGuardrailPosts(nil, spec.base, spec.panel)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(float64(p.Height-spec.want)) > 1e-6 || p.Diameter != .114 {
			t.Fatal(p)
		}
	}
	custom := GuardrailPostOptions{Diameter: .16, TopAboveBoardCenter: .2, Spacing: 2, LateralOffset: .3, RadialSegments: 16}
	p, err := resolveGuardrailPosts(&custom, .444, .506)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(p.Height-.897)) > 1e-6 || p.Spacing != 2 || p.LateralOffset != .3 || custom.Height != 0 {
		t.Fatal(p)
	}
	custom.Height = 1.25
	p, err = resolveGuardrailPosts(&custom, .444, .506)
	if err != nil || p.Height != 1.25 {
		t.Fatal(p, err)
	}
	for _, bad := range []GuardrailPostOptions{{Diameter: .113}, {Diameter: -1}, {Height: -1}, {Spacing: -1}, {RadialSegments: 2}, {RadialSegments: 257}, {Diameter: float32(math.NaN())}} {
		if _, err := resolveGuardrailPosts(&bad, .445, .31); err == nil {
			t.Fatal("accepted invalid post options", bad)
		}
	}
}

// Match custom dimensions against actual mesh bounds in both installation directions.
func TestGuardrailPostMeshParameters(t *testing.T) {
	input := GuardrailPostOptions{Diameter: .16, Height: 1.25, LateralOffset: .3, RadialSegments: 16}
	p, err := resolveGuardrailPosts(&input, .445, .31)
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []int{1, 2} {
		pos, _, _, _ := buildGuardrailPostMesh(direction, p)
		lo, hi := pos[0], pos[0]
		for _, v := range pos {
			for k := 0; k < 3; k++ {
				lo[k] = min(lo[k], v[k])
				hi[k] = max(hi[k], v[k])
			}
		}
		if math.Abs(float64(hi[1]-1.25)) > 1e-6 || math.Abs(float64(lo[1])) > 1e-6 || math.Abs(float64(hi[0]-lo[0]-.16)) > 1e-6 || math.Abs(float64(hi[2]-lo[2]-.16)) > 1e-6 || math.Abs(math.Abs(float64((hi[0]+lo[0])/2))-.3) > 1e-6 {
			t.Fatal(lo, hi)
		}
	}
}

// Apply one shared spacing phase across original parts, including legacy standalone sampling.
func TestGuardrailPostSpacingParameters(t *testing.T) {
	line := LocalLine{Points: [][3]float32{{0, 0, 0}, {10, 0, 0}}}
	for _, sampling := range []*LineChainSampling{nil, {Length: 10, IncludeEnd: true}} {
		instances, err := guardrailChainSupportTransforms(line, sampling, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(instances) != 6 {
			t.Fatal(len(instances))
		}
		for i, v := range instances {
			if math.Abs(float64(v.Translation[0])-2*float64(i)) > 1e-5 {
				t.Fatal(v)
			}
		}
	}
	line.Points = [][3]float32{{5, 0, 0}, {10, 0, 0}}
	instances, err := guardrailChainSupportTransforms(line, &LineChainSampling{Length: 5, StartOffset: 5, IncludeEnd: true}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 3 || instances[0].Translation[0] != 6 || instances[1].Translation[0] != 8 || instances[2].Translation[0] != 10 {
		t.Fatal(instances)
	}
}

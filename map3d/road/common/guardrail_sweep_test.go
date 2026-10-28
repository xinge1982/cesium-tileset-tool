package common

import (
	"math"
	"testing"
)

// Check standard envelopes, wave count, lighting normals and reversed winding.
func TestGuardrailCosineSweepDimensions(t *testing.T) {
	for _, spec := range []struct {
		waves        int
		base, height float32
	}{{2, .445, .310}, {3, .444, .506}} {
		for _, direction := range []int{1, 2} {
			line := LocalLine{Points: [][3]float32{{0, 5, 0}, {10, 5, 0}}}
			pos, normals, uv, idx, err := buildGuardrailWaveSweep(line, direction, spec.waves, spec.base, spec.height)
			if err != nil {
				t.Fatal(err)
			}
			stride := spec.waves*16 + 1
			if len(pos) != 2*stride || len(idx) != spec.waves*16*6 || len(normals) != len(pos) || len(uv) != len(pos) {
				t.Fatal("unexpected mesh size")
			}
			minimum, maximum := float32(100), float32(-100)
			depth := float32(0)
			troughs := 0
			for j := 0; j < stride; j++ {
				p := pos[j]
				minimum = min(minimum, p[1])
				maximum = max(maximum, p[1])
				depth = max(depth, float32(math.Abs(float64(p[2]))))
				if j > 0 && j < stride-1 && math.Abs(float64(p[2])) < math.Abs(float64(pos[j-1][2])) && math.Abs(float64(p[2])) < math.Abs(float64(pos[j+1][2])) {
					troughs++
				}
			}
			if math.Abs(float64(minimum-5-spec.base)) > 1e-6 || math.Abs(float64(maximum-5-spec.base-spec.height)) > 1e-6 || math.Abs(float64(depth-.085)) > 1e-6 || troughs != spec.waves {
				t.Fatalf("incorrect envelope: min=%g max=%g depth=%g troughs=%d", minimum, maximum, depth, troughs)
			}
			for i, n := range normals {
				length := math.Sqrt(float64(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]))
				if math.Abs(length-1) > 1e-5 {
					t.Fatal("invalid normal")
				}
				if math.Abs(float64(uv[i][0])-float64(i/stride)*10) > 1e-6 {
					t.Fatal("longitudinal UV reset")
				}
			}
			area := 0.0
			for i := 0; i < len(idx); i += 3 {
				a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
				n := triangleNormal(a, b, c)
				if direction == 1 && n[2] <= 0 || direction == 2 && n[2] >= 0 {
					t.Fatal("incorrect winding")
				}
				area += math.Sqrt(float64(n[0]*n[0]+n[1]*n[1]+n[2]*n[2])) / 2
			}
			if area <= float64(spec.height)*10 {
				t.Fatal("corrugation has no additional surface area")
			}
		}
	}
}

// Keep adjacent stations joined on bends and preserve a rising ground datum.
func TestGuardrailCosineSweepBendAndGrade(t *testing.T) {
	line := LocalLine{Points: [][3]float32{{0, 0, 0}, {5, 1, 0}, {10, 2, 5}}}
	pos, normals, _, indices, err := buildGuardrailWaveSweep(line, 1, 2, .445, .310)
	if err != nil {
		t.Fatal(err)
	}
	const stride = 33
	for i, point := range line.Points {
		if math.Abs(float64(pos[i*stride][1]-point[1]-.445)) > 1e-6 || math.Abs(float64(pos[i*stride+stride-1][1]-point[1]-.755)) > 1e-6 {
			t.Fatal("lost ground elevation")
		}
	}
	for _, index := range indices {
		if int(index) >= len(pos) {
			t.Fatal("bad index")
		}
	}
	for _, n := range normals {
		for _, v := range n {
			if !chainFinite(float64(v)) {
				t.Fatal("nonfinite bend normal")
			}
		}
	}
	closed := LocalLine{Points: [][3]float32{{0, 0, 0}, {5, 0, 0}, {5, 0, 5}, {0, 0, 5}, {0, 0, 0}}}
	pos, normals, _, _, err = buildGuardrailWaveSweep(closed, 1, 2, .445, .310)
	if err != nil {
		t.Fatal(err)
	}
	for j := 0; j < stride; j++ {
		if pos[j] != pos[len(pos)-stride+j] || normals[j] != normals[len(normals)-stride+j] {
			t.Fatal("closed sweep seam")
		}
	}
}

// Verify the phase against the actual post template after its instance rotation.
func TestGuardrailSweepPhaseFacesPosts(t *testing.T) {
	for _, direction := range []int{1, 2} {
		for _, waves := range []int{2, 3} {
			for _, points := range [][][3]float32{{{0, 0, 0}, {10, 0, 0}}, {{0, 0, 0}, {0, 0, 10}}, {{10, 0, 10}, {0, 0, 0}}} {
				line := LocalLine{Points: points}
				pos, _, _, _, err := buildGuardrailWaveSweep(line, direction, waves, .445, .31)
				if err != nil {
					t.Fatal(err)
				}
				post, _, _, _ := buildGuardrailSupportMesh(direction, .445, .31)
				supports, err := guardrailSupportTransforms(line)
				if err != nil {
					t.Fatal(err)
				}
				// The symmetric cylinder's local center transforms with the source heading.
				cx := float32(0)
				for _, p := range post {
					cx += p[0]
				}
				cx /= float32(len(post))
				q := supports[0].Rotation
				postOffset := [3]float32{(1 - 2*q[1]*q[1]) * cx, 0, -2 * q[1] * q[3] * cx}
				edge := sub3(pos[0], points[0])
				valley := sub3(pos[8], points[0])
				if edge[0]*postOffset[0]+edge[2]*postOffset[2] <= 0 {
					t.Fatal("edge is not on the post side")
				}
				if math.Abs(float64(edge[0]*edge[0]+edge[2]*edge[2]-.085*.085)) > 1e-6 {
					t.Fatal("phase changed depth envelope")
				}
				if math.Hypot(float64(valley[0]), float64(valley[2])) > 1e-6 {
					t.Fatal("half-wave valley did not move to source plane")
				}
			}
		}
	}
}

// Check that the active instance template contains no connector block vertices.
func TestGuardrailSupportMeshPostOnly(t *testing.T) {
	for _, direction := range []int{1, 2} {
		got, gn, gu, gi := buildGuardrailSupportMesh(direction, .445, .31)
		var want [][3]float32
		var wn [][3]float32
		var wu [][2]float32
		var wi []uint32
		sign := float32(1)
		if direction == 1 {
			sign = -1
		}
		offset := sign * .230
		appendCylinderGeometry(&want, &wn, &wu, &wi, [3]float32{offset, .750 / 2, 0}, .114, .750, .114, 12)
		if len(got) != len(want) || len(gi) != len(wi) || len(gn) != len(wn) || len(gu) != len(wu) {
			t.Fatal("support contains geometry beyond the post")
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatal("post location or dimensions changed")
			}
		}
	}
}

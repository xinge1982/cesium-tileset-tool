package common

import "testing"

func TestBuildGuardrailTwoWaveMesh(t *testing.T) {
	line := LocalLine{
		Points: [][3]float32{
			{0, 0, 0},
			{10, 0, 0},
			{16, 0, 4},
		},
	}

	pos, normals, uv, indices, err := buildGuardrailTwoWaveMesh(
		line,
		1,
		defaultGuardrailRailHeight,
		defaultGuardrailRailHeightAll,
	)
	if err != nil {
		t.Fatalf("build guardrail mesh: %v", err)
	}
	if len(pos) == 0 || len(indices) == 0 {
		t.Fatal("guardrail mesh is empty")
	}
	if len(pos) != len(normals) || len(pos) != len(uv) {
		t.Fatalf("attribute length mismatch: pos=%d normals=%d uv=%d", len(pos), len(normals), len(uv))
	}
}

package common

import "testing"

func TestAntiGlareBoardStartDistance(t *testing.T) {
	tests := []struct {
		name        string
		spacing     float32
		startOffset float32
		want        float32
	}{
		{name: "zero", spacing: 1, startOffset: 0, want: 0},
		{name: "exact multiple", spacing: 1, startOffset: 3, want: 0},
		{name: "positive remainder", spacing: 1, startOffset: 0.35, want: 0.65},
		{name: "large offset", spacing: 1, startOffset: 12.2, want: 0.8},
	}
	for _, tt := range tests {
		got := antiGlareBoardStartDistance(tt.spacing, tt.startOffset)
		if diff := got - tt.want; diff > 0.0001 || diff < -0.0001 {
			t.Fatalf("%s: got %.4f want %.4f", tt.name, got, tt.want)
		}
	}
}

func TestBuildAntiGlareBoardTransforms(t *testing.T) {
	line := LocalLine{
		Points: [][3]float32{
			{0, 0, 0},
			{3, 0, 0},
		},
	}
	transforms, err := buildAntiGlareBoardTransforms(line, 1, 0.25)
	if err != nil {
		t.Fatalf("build transforms: %v", err)
	}
	if len(transforms) != 3 {
		t.Fatalf("unexpected transform count: %d", len(transforms))
	}
	wantX := []float32{0.75, 1.75, 2.75}
	for i := range transforms {
		if diff := transforms[i].Translation[0] - wantX[i]; diff > 0.0001 || diff < -0.0001 {
			t.Fatalf("transform %d x got %.4f want %.4f", i, transforms[i].Translation[0], wantX[i])
		}
		wantRot := quaternionFromForward([3]float32{1, 0, 0})
		if transforms[i].Rotation != wantRot {
			t.Fatalf("transform %d rotation got %v want %v", i, transforms[i].Rotation, wantRot)
		}
	}
}

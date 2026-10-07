package common

import "testing"

func TestSmoothRoadSurfaceRingHeights_ReducesSpikes(t *testing.T) {
	ring := LocalRings{
		Outer: [][3]float32{
			{0, 0, 0},
			{1, 10, 0},
			{2, 0, 0},
			{2, 0, 2},
			{0, 0, 2},
			{0, 0, 0},
		},
	}

	smoothed := smoothRoadSurfaceRingHeights(ring)
	if len(smoothed.Outer) != len(ring.Outer) {
		t.Fatalf("expected point count preserved, got %d want %d", len(smoothed.Outer), len(ring.Outer))
	}
	if smoothed.Outer[0] != smoothed.Outer[len(smoothed.Outer)-1] {
		t.Fatalf("expected closed ring after smoothing")
	}
	if smoothed.Outer[0][0] != ring.Outer[0][0] || smoothed.Outer[0][2] != ring.Outer[0][2] {
		t.Fatalf("expected x/z of first point unchanged")
	}

	origMax := maxAdjacentYDelta(ring.Outer)
	newMax := maxAdjacentYDelta(smoothed.Outer)
	if newMax >= origMax {
		t.Fatalf("expected smoothing to reduce spikes, got old=%.2f new=%.2f", origMax, newMax)
	}
}

func maxAdjacentYDelta(points [][3]float32) float32 {
	if len(points) < 2 {
		return 0
	}
	maxDelta := float32(0)
	last := len(points) - 1
	for i := 0; i < last; i++ {
		d := points[i+1][1] - points[i][1]
		if d < 0 {
			d = -d
		}
		if d > maxDelta {
			maxDelta = d
		}
	}
	return maxDelta
}

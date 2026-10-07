package common

import "testing"

func TestRoadTriangleGridIndexQuery(t *testing.T) {
	idx := newRoadTriangleGridIndex(10)
	idx.add(0, roadTriangle{minX: 0, maxX: 9, minZ: 0, maxZ: 9})
	idx.add(1, roadTriangle{minX: 100, maxX: 109, minZ: 100, maxZ: 109})

	gotA := idx.query([3]float32{1, 0, 1})
	if len(gotA) != 1 || gotA[0] != 0 {
		t.Fatalf("unexpected query for near point: %+v", gotA)
	}
	gotB := idx.query([3]float32{101, 0, 101})
	if len(gotB) != 1 || gotB[0] != 1 {
		t.Fatalf("unexpected query for far point: %+v", gotB)
	}
}

func TestRoadSurfaceProjectorSpatialIndexProjection(t *testing.T) {
	p := NewRoadSurfaceProjector()
	pos := [][3]float32{
		{0, 5, 0},
		{10, 5, 0},
		{0, 5, 10},
		{100, 50, 100},
		{110, 50, 100},
		{100, 50, 110},
	}
	p.AddRoadSurface(1, pos, []uint32{0, 1, 2, 3, 4, 5})

	out, ok := p.ProjectPoints(0, [][3]float32{{2, 0, 2}})
	if !ok {
		t.Fatalf("expected projection success")
	}
	if len(out) != 1 {
		t.Fatalf("unexpected output length: %d", len(out))
	}
	if out[0][1] < 4.9 || out[0][1] > 5.1 {
		t.Fatalf("unexpected projected height: %v", out[0][1])
	}
}

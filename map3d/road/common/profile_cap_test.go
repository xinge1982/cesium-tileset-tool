package common

import "testing"

func TestAppendProfileCap_ClosesLastToFirstEdge(t *testing.T) {
	frame := [][3]float32{
		{0, 0, 0},
		{1, 0, 0},
		{1, 1, 0},
		{0, 1, 0},
	}

	var (
		pos     [][3]float32
		normals [][3]float32
		uv      [][2]float32
		indices []uint32
	)
	appendProfileCap(&pos, &normals, &uv, &indices, frame, false, nil)

	if len(pos) != 5 {
		t.Fatalf("unexpected cap vertex count: %d", len(pos))
	}
	if len(indices) != 12 {
		t.Fatalf("expected 4 cap triangles, got %d indices", len(indices))
	}
	last := [3]uint32{indices[9], indices[10], indices[11]}
	if last[0] != 0 || last[1] != 4 || last[2] != 1 {
		t.Fatalf("expected closing triangle to connect last->first edge, got %v", last)
	}
}

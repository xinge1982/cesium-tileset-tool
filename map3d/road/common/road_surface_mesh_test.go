package common

import (
	"math"
	"testing"
)

func TestClipRoadSurfaceMeshPreservesSeamAttributes(t *testing.T) {
	mesh := RoadSurfaceMesh{Positions: [][3]float32{{0, 1, 0}, {2, 5, 0}, {2, 11, 2}, {0, 7, 2}}, Indices: []uint32{0, 1, 2, 0, 2, 3}, Normals: [][3]float32{{0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {0, 1, 0}}, UVs: [][2]float32{{0, 0}, {2, 0}, {2, 2}, {0, 2}}}
	boxes := [][][2]float64{{{0, 0}, {1, 0}, {1, 2}, {0, 2}}, {{1, 0}, {2, 0}, {2, 2}, {1, 2}}}
	totalArea := 0.0
	seams := make([]map[[2]float32][3]float32, 2)
	for side, box := range boxes {
		clipped, err := ClipRoadSurfaceMesh(mesh, box)
		if err != nil {
			t.Fatal(err)
		}
		if len(clipped.Indices) == 0 {
			t.Fatal("empty half")
		}
		seams[side] = map[[2]float32][3]float32{}
		for i, p := range clipped.Positions {
			if math.Abs(float64(p[1]-(1+2*p[0]+3*p[2]))) > 1e-5 {
				t.Fatalf("height changed: %v", p)
			}
			uv := clipped.UVs[i]
			if uv[0] != p[0] || uv[1] != p[2] {
				t.Fatalf("UV changed: %v %v", p, uv)
			}
			if p[0] < float32(side) || p[0] > float32(side+1) || p[2] < 0 || p[2] > 2 {
				t.Fatalf("outside half: %v", p)
			}
			if p[0] == 1 {
				seams[side][[2]float32{p[0], p[2]}] = [3]float32{p[1], uv[0], uv[1]}
			}
		}
		for i := 0; i < len(clipped.Indices); i += 3 {
			a, b, c := clipped.Positions[clipped.Indices[i]], clipped.Positions[clipped.Indices[i+1]], clipped.Positions[clipped.Indices[i+2]]
			cross := float64((b[0]-a[0])*(c[2]-a[2]) - (b[2]-a[2])*(c[0]-a[0]))
			if cross <= 0 {
				t.Fatal("triangle winding changed")
			}
			totalArea += cross / 2
		}
	}
	if math.Abs(totalArea-4) > 1e-6 {
		t.Fatalf("area lost or duplicated: %f", totalArea)
	}
	if len(seams[0]) < 2 || len(seams[0]) != len(seams[1]) {
		t.Fatalf("inconsistent seam vertices: %v", seams)
	}
	for key, value := range seams[0] {
		if seams[1][key] != value {
			t.Fatalf("seam attributes differ: %v", key)
		}
	}
	// Reverse winding of the clipping polygon must yield the same covered area.
	reversed := [][2]float64{{0, 2}, {1, 2}, {1, 0}, {0, 0}}
	clipped, err := ClipRoadSurfaceMesh(mesh, reversed)
	if err != nil || len(clipped.Indices) == 0 {
		t.Fatalf("reversed boundary: %v", err)
	}
	empty, err := ClipRoadSurfaceMesh(mesh, [][2]float64{{3, 0}, {4, 0}, {4, 2}, {3, 2}})
	if err != nil || len(empty.Indices) != 0 {
		t.Fatalf("disjoint clip: %v", err)
	}
	boundaryOnly, err := ClipRoadSurfaceMesh(mesh, [][2]float64{{2, 0}, {3, 0}, {3, 2}, {2, 2}})
	if err != nil || len(boundaryOnly.Indices) != 0 {
		t.Fatalf("boundary-only clip: %v", err)
	}
}

func TestClipRoadSurfaceMeshRejectsInvalidInput(t *testing.T) {
	bad := RoadSurfaceMesh{Indices: []uint32{0, 1, 2}}
	if _, err := ClipRoadSurfaceMesh(bad, [][2]float64{{0, 0}, {1, 0}, {0, 1}}); err == nil {
		t.Fatal("invalid indices accepted")
	}
	if _, err := ClipRoadSurfaceMesh(RoadSurfaceMesh{}, [][2]float64{{0, 0}, {1, 0}, {2, 0}}); err == nil {
		t.Fatal("degenerate boundary accepted")
	}
}

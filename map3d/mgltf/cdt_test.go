package mgltf

import "testing"

func TestCDTRingKeepWinding(t *testing.T) {
	// A concave vertical wall; a triangle fan would cover the missing corner.
	base := [][3]float32{{0, 0, 0}, {4, 0, 0}, {4, 1, 0}, {1, 1, 0}, {1, 4, 0}, {0, 4, 0}}
	for axis := 0; axis < 3; axis++ {
		for _, reverse := range []bool{false, true} {
			pos := make([][3]float32, len(base))
			for i, p := range base {
				pos[i][axis] = p[0]
				pos[i][(axis+1)%3] = p[1]
			}
			if reverse {
				for i, j := 0, len(pos)-1; i < j; i, j = i+1, j-1 {
					pos[i], pos[j] = pos[j], pos[i]
				}
			}
			pos = append(pos, pos[0])
			before := append([][3]float32(nil), pos...)
			idx, err := CDTRingKeepWinding(pos)
			if err != nil {
				t.Fatal(err)
			}
			if len(idx) != 12 {
				t.Fatalf("triangle count: %d", len(idx)/3)
			}
			var area float32
			for i := 0; i < len(idx); i += 3 {
				a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
				signed := (b[axis]-a[axis])*(c[(axis+1)%3]-a[(axis+1)%3]) - (b[(axis+1)%3]-a[(axis+1)%3])*(c[axis]-a[axis])
				if (!reverse && signed <= 0) || (reverse && signed >= 0) {
					t.Fatal("winding changed")
				}
				area += signed / 2
			}
			if reverse {
				area = -area
			}
			if area != 7 {
				t.Fatalf("area %v, want 7", area)
			}
			for i := range pos {
				if pos[i] != before[i] {
					t.Fatal("positions changed")
				}
			}
		}
	}
	if _, err := CDTRingKeepWinding([][3]float32{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}}); err == nil {
		t.Fatal("expected degenerate error")
	}
}

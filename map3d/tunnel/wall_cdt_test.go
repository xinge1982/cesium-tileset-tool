package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"math"
	"testing"
)

func TestCurvedTunnelWallCDT(t *testing.T) {
	// 下边界沿折返路径行进，上边界反向返回；整面投影无法一对一表达。
	path := [][3]float32{{0, 0, 0}, {4, 0, 0}, {4, 0, 4}, {0, 0, 4}, {0, 0, 8}, {4, 0, 8}}
	for _, reverse := range []bool{false, true} {
		ring := append([][3]float32(nil), path...)
		for i := len(path) - 1; i >= 0; i-- {
			p := path[i]
			p[1] += 2.5
			ring = append(ring, p)
		}
		if reverse {
			for i, j := 0, len(ring)-1; i < j; i, j = i+1, j-1 {
				ring[i], ring[j] = ring[j], ring[i]
			}
		}
		before := append([][3]float32(nil), ring...)
		indices, err := triangulateTunnelWallCDT(ring)
		if err != nil {
			t.Fatal(err)
		}
		if len(indices) != (len(path)-1)*6 {
			t.Fatalf("indices %d", len(indices))
		}
		var area float64
		for segment := 0; segment < len(path)-1; segment++ {
			n := len(ring)
			allowed := map[uint32]bool{uint32(segment): true, uint32(segment + 1): true, uint32(n - 2 - segment): true, uint32(n - 1 - segment): true}
			want := cross3(sub3(ring[segment+1], ring[segment]), sub3(ring[n-1-segment], ring[segment]))
			for k := segment * 6; k < segment*6+6; k += 3 {
				for _, idx := range indices[k : k+3] {
					if !allowed[idx] {
						t.Fatal("triangle crosses wall segments")
					}
				}
				a, b, c := ring[indices[k]], ring[indices[k+1]], ring[indices[k+2]]
				normal := cross3(sub3(b, a), sub3(c, a))
				if dot3(normal, want) <= 0 {
					t.Fatal("winding changed")
				}
				area += math.Sqrt(float64(dot3(normal, normal))) / 2
			}
		}
		if math.Abs(area-50) > 1e-6 {
			t.Fatalf("area %v", area)
		}
		for i := range ring {
			if ring[i] != before[i] {
				t.Fatal("positions changed")
			}
		}
	}
}

func TestUnpairedTunnelWallCDTFallback(t *testing.T) {
	ring := [][3]float32{{0, 0, 0}, {4, 0, 0}, {3, 3, 0}, {1, 2, 0}}
	want, err := mgltf.CDTRingKeepWinding(ring)
	if err != nil {
		t.Fatal(err)
	}
	got, err := triangulateTunnelWallCDT(ring)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatal("fallback changed triangle count")
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatal("unpaired polygon did not use generic CDT")
		}
	}
}

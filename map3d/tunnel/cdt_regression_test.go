package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"encoding/json"
	"github.com/twpayne/go-geom/encoding/geojson"
	"os"
	"path/filepath"
	"testing"
)

func TestTunnelCDTFixtures(t *testing.T) {
	dir := os.Getenv("MAP3D_TUNNEL_FIXTURES")
	if dir == "" {
		t.Skip("set MAP3D_TUNNEL_FIXTURES to test real tunnel GeoJSON")
	}
	files, err := filepath.Glob(filepath.Join(dir, "tunnel_*_walls.geojson"))
	if err != nil || len(files) == 0 {
		t.Fatal("no tunnel fixtures", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			fc := new(geojson.FeatureCollection)
			if err = json.Unmarshal(data, fc); err != nil {
				t.Fatal(err)
			}
			base, _, err := calcTileContextFromFeatureCollections(fc)
			if err != nil {
				t.Fatal(err)
			}
			for i, f := range fc.Features {
				flat, stride, ok := outerRingFlatCoordsFromWGS84Geom(f.Geometry)
				if !ok {
					t.Fatalf("feature %d unsupported", i)
				}
				var pos [][3]float32
				for j := 0; j < len(flat); j += stride {
					e, n, u := wgs84ToLocal(flat[j], flat[j+1], flat[j+2], base[0], base[1], 0)
					pos = append(pos, [3]float32{float32(e), float32(u), float32(-n)})
				}
				pos = normalizeClosedRing3D(pos)
				if len(pos) < 3 {
					t.Logf("skip degenerate feature %d", i)
					continue
				}
				var indices []uint32
				surfaceType, _ := f.Properties["surface_type"].(string)
				if surfaceType == "left_wall" || surfaceType == "right_wall" {
					indices, err = triangulateTunnelWallCDT(pos)
				} else {
					indices, err = mgltf.CDTRingKeepWinding(pos)
				}
				if err != nil {
					t.Fatalf("feature %d (%v): %v", i, f.Properties["surface_type"], err)
				}
				uniqueCount := 0
				for j, p := range pos {
					if j == 0 || p != pos[j-1] {
						uniqueCount++
					}
				}
				if len(pos) > 1 && pos[0] == pos[len(pos)-1] {
					uniqueCount--
				}
				if len(indices) != (uniqueCount-2)*3 {
					t.Fatalf("feature %d triangle count %d", i, len(indices)/3)
				}
			}
		})
	}
}

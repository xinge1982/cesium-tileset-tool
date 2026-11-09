package road

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"cesium-tileset-tool/map3d/road/common"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func indexedRoadQuad(x, z, width, depth float32) common.RoadSurfaceMesh {
	return common.RoadSurfaceMesh{
		Positions: [][3]float32{{x, 1, z}, {x + width, 2, z}, {x + width, 3, z + depth}, {x, 2, z + depth}},
		Normals:   [][3]float32{{0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {0, 1, 0}},
		UVs:       [][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}},
		Indices:   []uint32{0, 1, 2, 0, 2, 3},
	}
}

type indexedRoadCut struct {
	feature, mesh int
	cut           common.RoadSurfaceMesh
}

// Compare complete clipped attributes against the original all-mesh traversal.
func TestRoadMeshIndexMatchesExhaustiveClipping(t *testing.T) {
	meshes := [][]common.RoadSurfaceMesh{}
	for i := 0; i < 128; i++ {
		x, z := float32(i%16)*4-32, float32(i/16)*4-16
		meshes = append(meshes, []common.RoadSurfaceMesh{indexedRoadQuad(x, z, 2, 2)})
	}
	// Include a long crossing polygon and separate parts belonging to one feature.
	meshes = append(meshes, []common.RoadSurfaceMesh{indexedRoadQuad(-100, -.5, 200, 1), indexedRoadQuad(100, 100, 1, 1)})
	index, err := newRoadMeshIndex(meshes)
	if err != nil {
		t.Fatal(err)
	}
	random := rand.New(rand.NewSource(42))
	for trial := 0; trial < 80; trial++ {
		x, z := random.Float64()*90-45, random.Float64()*50-25
		boundary := [][2]float64{{x - 3, z - 2}, {x + 3, z - 1}, {x + 2, z + 2}, {x - 3, z + 1}}
		var expected, actual []indexedRoadCut
		for feature, parts := range meshes {
			for part, mesh := range parts {
				cut, err := common.ClipRoadSurfaceMesh(mesh, boundary)
				if err != nil {
					t.Fatal(err)
				}
				if len(cut.Indices) > 0 {
					expected = append(expected, indexedRoadCut{feature, part, cut})
				}
			}
		}
		for _, ref := range index.query(boundary) {
			cut, err := common.ClipRoadSurfaceMesh(meshes[ref.feature][ref.mesh], boundary)
			if err != nil {
				t.Fatal(err)
			}
			if len(cut.Indices) > 0 {
				actual = append(actual, indexedRoadCut{ref.feature, ref.mesh, cut})
			}
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("candidate clipping changed geometry, UVs, normals or feature order in trial %d", trial)
		}
	}
	if _, err = newRoadMeshIndex([][]common.RoadSurfaceMesh{{{Indices: []uint32{0, 1, 2}}}}); err == nil {
		t.Fatal("invalid mesh accepted")
	}
	empty, err := newRoadMeshIndex(nil)
	if err != nil || len(empty.query([][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}})) != 0 {
		t.Fatal("empty index failed", err)
	}
	touch, _ := newRoadMeshIndex([][]common.RoadSurfaceMesh{{indexedRoadQuad(3, 3, 1, 1)}})
	if len(touch.query([][2]float64{{4, 4}, {5, 4}, {5, 5}, {4, 5}})) != 1 {
		t.Fatal("touching boundary was excluded")
	}
}

// Compare bounds computed before encoding with the previous GLB readback path.
func TestRoadSliceBoundsMatchGLBReadback(t *testing.T) {
	fc := &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{121, 31, 10}, {121.0002, 31, 12}, {121.0002, 31.0002, 14}, {121, 31.0002, 11}, {121, 31, 10}}}), Properties: map[string]any{"id": "bounds"}}}}
	result, err := BuildRoadSurfaceGeohashSlices(fc, nil, RoadSurfaceGeohashOptions{OutputDir: t.TempDir(), Precision: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, tile := range result.Tiles {
		previous, err := BuildTilesetJSONForGLB(TilesetBuildOptions{GLBPath: tile.GLBPath, ContentURI: "surface.glb", BasePoint: []float64{121.0001, 31.0001, 0}})
		if err != nil {
			t.Fatal(err)
		}
		var old tilesetJSON
		if err = json.Unmarshal(previous, &old); err != nil {
			t.Fatal(err)
		}
		for axis, value := range old.Root.BoundingVolume.Box {
			if math.Abs(value-tile.Box[axis]) > 1e-9 {
				t.Fatal("in-memory box differs from encoded GLB box", tile.Box, old.Root.BoundingVolume.Box)
			}
		}
	}
}

// Isolate candidate selection and exact clipping; file/texture/GLB costs are excluded.
func BenchmarkRoadSurfaceCandidateClipping(b *testing.B) {
	meshes := make([][]common.RoadSurfaceMesh, 2048)
	for i := range meshes {
		meshes[i] = []common.RoadSurfaceMesh{indexedRoadQuad(float32(i%64)*2, float32(i/64)*2, 1, 1)}
	}
	var cells [][][2]float64
	for row := 0; row < 8; row++ {
		for col := 0; col < 16; col++ {
			x, z := float64(col*8)-.25, float64(row*8)-.25
			cells = append(cells, [][2]float64{{x, z}, {x + 7.5, z}, {x + 7.5, z + 7.5}, {x, z + 7.5}})
		}
	}
	for _, indexed := range []bool{false, true} {
		name := "exhaustive"
		if indexed {
			name = "indexed"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var calls int64
			for iteration := 0; iteration < b.N; iteration++ {
				area := 0.
				var index *roadMeshIndex
				if indexed {
					var err error
					index, err = newRoadMeshIndex(meshes)
					if err != nil {
						b.Fatal(err)
					}
				}
				for _, cell := range cells {
					clip := func(mesh common.RoadSurfaceMesh) {
						calls++
						cut, err := common.ClipRoadSurfaceMesh(mesh, cell)
						if err != nil {
							b.Fatal(err)
						}
						area += roadSliceMeshArea(cut)
					}
					if indexed {
						for _, ref := range index.query(cell) {
							clip(meshes[ref.feature][ref.mesh])
						}
					} else {
						for _, parts := range meshes {
							for _, mesh := range parts {
								clip(mesh)
							}
						}
					}
				}
				if math.Abs(area-2048) > 1e-6 {
					b.Fatal("benchmark clipping lost area", area)
				}
			}
			b.ReportMetric(float64(calls)/float64(b.N), "clips/op")
		})
	}
}

package tunnel

import (
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTunnelCenterlineCoordinates(t *testing.T) {
	line := &tunnelCenterline{points: [][3]float32{{0, 0, 0}, {10, 0, 0}, {10, 0, 10}}, stations: []float64{0, 10, 20}}
	for _, tc := range []struct {
		p    [3]float32
		s, l float64
	}{{[3]float32{4, 300, 2}, 4, 2}, {[3]float32{8, 600, 7}, 17, 2}} {
		s, l := line.coordinates(tc.p)
		if math.Abs(s-tc.s) > 1e-6 || math.Abs(l-tc.l) > 1e-6 {
			t.Fatalf("got %v,%v", s, l)
		}
	}
	pos := [][3]float32{{0, 5, 1}, {10, 6, 1}, {10, 8.5, 1}, {0, 7.5, 1}}
	straight := &tunnelCenterline{points: [][3]float32{{0, 0, 0}, {20, 0, 0}}, stations: []float64{0, 20}}
	uv := tunnelWallUVs(pos, straight, 2, 0.5)
	if uv[0] != [2]float32{0, 0} || uv[1] != [2]float32{5, 0} || uv[2] != [2]float32{5, 5} || uv[3] != [2]float32{0, 5} {
		t.Fatalf("UVs %v", uv)
	}
}

func TestLoadTunnelCenterlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "center.geojson")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"group_id":17},"geometry":{"type":"LineString","coordinates":[[116,30,100],[116.001,30,101]]}}]}`)
	refs, err := loadTunnelCenterlines(path, []float64{116, 30, 0})
	if err != nil {
		t.Fatal(err)
	}
	if refs["17"] == nil || len(refs["17"].points) != 2 {
		t.Fatal("numeric group_id mismatch")
	}
	write(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[116,30],[116.001,30]]}}]}`)
	if _, err := loadTunnelCenterlines(path, []float64{116, 30, 0}); err == nil {
		t.Fatal("missing group_id accepted")
	}
}

func TestTunnelCenterlineTileset(t *testing.T) {
	dir := t.TempDir()
	center := filepath.Join(dir, "center.geojson")
	walls := filepath.Join(dir, "walls.geojson")
	if err := os.WriteFile(center, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"group_id":7},"geometry":{"type":"MultiLineString","coordinates":[[[116,30,0],[116.0005,30,0]],[[116.0005,30,0],[116.001,30,0]]]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	input := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"group_id":"7","surface_type":"left_wall"},"geometry":{"type":"Polygon","coordinates":[[[116,29.99999,0],[116.001,29.99999,0],[116.001,29.99999,2.5],[116,29.99999,2.5],[116,29.99999,0]]]}},{"type":"Feature","properties":{"group_id":"7","surface_type":"flat_roof","from_station":0,"to_station":100},"geometry":{"type":"Polygon","coordinates":[[[116,29.99999,2.5],[116.001,29.99999,2.5],[116.001,30.00001,2.5],[116,30.00001,2.5],[116,29.99999,2.5]]]}}]}`
	if err := os.WriteFile(walls, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAP3D_TUNNEL_CENTERLINE", center)
	t.Setenv("MAP3D_TUNNEL_TEXTURE_REPEAT_U", "2")
	t.Setenv("MAP3D_TUNNEL_TEXTURE_REPEAT_V", "0.5")
	t.Setenv("MAP3D_TUNNEL_WALL_TEXTURE", "assets/RMLFZQ00002.jpg")
	t.Setenv("MAP3D_TUNNEL_ROOF_TEXTURE", "")
	output := filepath.Join(dir, "with-center")
	if err := generateTunnelWallGlbTileset(t, output, walls); err != nil {
		t.Fatal(err)
	}
	doc, err := gltf.Open(filepath.Join(output, "walls", "walls.glb"))
	if err != nil {
		t.Fatal(err)
	}
	var geometry [][][3]float32
	for i, primitive := range doc.Meshes[0].Primitives {
		positions, err := modeler.ReadPosition(doc, doc.Accessors[primitive.Attributes[gltf.POSITION]], nil)
		if err != nil {
			t.Fatal(err)
		}
		geometry = append(geometry, positions)
		uv, err := modeler.ReadTextureCoord(doc, doc.Accessors[primitive.Attributes[gltf.TEXCOORD_0]], nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(uv) != len(positions) {
			t.Fatal("UV count mismatch")
		}
		if math.Abs(float64(uv[1][0]-uv[0][0])) < 40 {
			t.Fatalf("primitive %d U not aligned with centerline: %v", i, uv)
		}
		if i == 0 && math.Abs(float64(uv[2][1]-uv[1][1])-5) > 0.01 {
			t.Fatalf("wall V not height based: %v", uv)
		}
		if i == 1 && math.Abs(float64(uv[2][1]-uv[1][1])) < 4 {
			t.Fatalf("roof V not cross-section based: %v", uv)
		}
	}
	t.Setenv("MAP3D_TUNNEL_CENTERLINE", "")
	without := filepath.Join(dir, "without-center")
	if err := generateTunnelWallGlbTileset(t, without, walls); err != nil {
		t.Fatal(err)
	}
	original, err := gltf.Open(filepath.Join(without, "walls", "walls.glb"))
	if err != nil {
		t.Fatal(err)
	}
	for i, primitive := range original.Meshes[0].Primitives {
		positions, err := modeler.ReadPosition(original, original.Accessors[primitive.Attributes[gltf.POSITION]], nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(positions, geometry[i]) {
			t.Fatal("centerline changed model positions")
		}
	}
	t.Setenv("MAP3D_TUNNEL_CENTERLINE", center)
	if err := os.WriteFile(walls, []byte(strings.ReplaceAll(input, `"group_id":"7"`, `"group_id":"8"`)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := generateTunnelWallGlbTileset(t, filepath.Join(dir, "missing"), walls); err == nil || !strings.Contains(err.Error(), "no centerline") {
		t.Fatalf("missing match error: %v", err)
	}
}

func TestRoofSectionArcUV(t *testing.T) {
	graph := roofSectionGraph{0: {1: 2}, 1: {0: 2, 2: 3}, 2: {1: 3}}
	uv := [][2]float32{{0, -1}, {0, 0}, {0, 1}}
	if err := graph.apply(uv, 0.5); err != nil {
		t.Fatal(err)
	}
	if uv[0][1] != 0 || uv[1][1] != 4 || uv[2][1] != 10 {
		t.Fatalf("arc UV %v", uv)
	}
}

func TestMergedRoofSectionUV(t *testing.T) {
	// 两个断面在右端合并，两个左端均需从零起算。
	graph := roofSectionGraph{0: {1: 2}, 1: {0: 2, 2: 3}, 2: {1: 3, 3: 3}, 3: {2: 3, 4: 2}, 4: {3: 2}}
	uv := [][2]float32{{0, -2}, {0, -1}, {0, 2}, {1, -1}, {1, -2}}
	if err := graph.apply(uv, 1); err != nil {
		t.Fatal(err)
	}
	if uv[0][1] != 0 || uv[4][1] != 0 || uv[1][1] != 2 || uv[3][1] != 2 || uv[2][1] != 5 {
		t.Fatalf("merged UV %v", uv)
	}
}

func TestMultiLineStringCenterline(t *testing.T) {
	origin := []float64{116, 30, 0}
	load := func(kind, coordinates string) (*tunnelCenterline, error) {
		path := filepath.Join(t.TempDir(), "center.geojson")
		data := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"group_id":"17"},"geometry":{"type":"` + kind + `","coordinates":` + coordinates + `}}]}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		refs, err := loadTunnelCenterlines(path, origin)
		return refs["17"], err
	}
	line, err := load("LineString", `[[116,30,10],[116.001,30,11],[116.002,30.001,12]]`)
	if err != nil {
		t.Fatal(err)
	}
	multi, err := load("MultiLineString", `[[[116,30,10],[116.001,30,11]],[[116.001,30,11],[116.002,30.001,12]]]`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(line, multi) {
		t.Fatal("split line changed points or mileage")
	}
	for _, p := range line.points {
		s, l := line.coordinates(p)
		ms, ml := multi.coordinates(p)
		if s != ms || l != ml {
			t.Fatal("split line changed UV coordinates")
		}
	}
	if _, err = load("MultiLineString", `[[[116,30],[116.001,30]],[[116.001,30],[116.002,30]]]`); err != nil {
		t.Fatal("XY lines", err)
	}
	for _, coordinates := range []string{
		`[]`,
		`[[[116,30],[116.001,30]],[[116.01,30],[116.02,30]]]`,
		`[[[116,30],[116.001,30]],[[116.002,30],[116.001,30]]]`,
		`[[[116,30],[116.001,30]],[[116.001,30],[116.002,30]],[[116.001,30],[116.001,30.001]]]`,
		`[[[116,30,0],[116.001,30,0]],[[116.001,30,10],[116.002,30,10]]]`,
	} {
		if _, err = load("MultiLineString", coordinates); err == nil {
			t.Fatalf("invalid multiline accepted: %s", coordinates)
		}
	}
}

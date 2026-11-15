package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformSquareTiles(t *testing.T) {
	line := &tunnelCenterline{points: [][3]float32{{0, 0, 0}, {20, 0, 0}}, stations: []float64{0, 20}}
	polygons := [][][3]float32{
		{{0, 2, 1}, {4, 2, 1}, {4, 2, 2}, {0, 2, 2}},
		{{4, 2, 1}, {8, 2, 1}, {8, 2, 2}, {4, 2, 2}},
	}
	doc := gltf.NewDocument()
	primitive, err := TunnelPlatformMultiPolygonPrimitive(doc, polygons, "platform_top", 0, mgltf.UvsParams{}, TunnelPlatformOptions{Centerline: line, TileSize: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	uv, err := modeler.ReadTextureCoord(doc, doc.Accessors[primitive.Attributes[gltf.TEXCOORD_0]], nil)
	if err != nil {
		t.Fatal(err)
	}
	if uv[0] != [2]float32{0, 2} || uv[1] != [2]float32{8, 2} || uv[2] != [2]float32{8, 4} {
		t.Fatalf("not 0.5m square tiles: %v", uv)
	}
	if uv[1] != uv[4] || uv[2] != uv[7] {
		t.Fatalf("UV reset at polygon seam: %v", uv)
	}
	positions, err := modeler.ReadPosition(doc, doc.Accessors[primitive.Attributes[gltf.POSITION]], nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range positions {
		if p != polygons[i/4][i%4] {
			t.Fatal("positions changed")
		}
	}
}

func TestPlatformCurvedStripCDT(t *testing.T) {
	line := &tunnelCenterline{points: [][3]float32{{0, 0, 0}, {10, 0, 0}, {10, 0, 10}}, stations: []float64{0, 10, 20}}
	// 边界配对的折弯平台，断面保持在中心线相应里程上。
	pos := [][3]float32{{0, 1, 1}, {9, 1, 1}, {9, 1, 8}, {8, 1, 8}, {8, 1, 2}, {0, 1, 2}}
	indices, err := triangulatePlatformStripCDT(pos, line)
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 12 {
		t.Fatalf("indices %v", indices)
	}
	var area float64
	for i := 0; i < len(indices); i += 3 {
		a, b, c := pos[indices[i]], pos[indices[i+1]], pos[indices[i+2]]
		normal := cross3(sub3(b, a), sub3(c, a))
		if normal[1] >= 0 {
			t.Fatal("winding changed")
		}
		area += math.Sqrt(float64(dot3(normal, normal))) / 2
	}
	if area <= 0 {
		t.Fatal("empty strip")
	}
}

func TestPlatformCenterlineTileset(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "platform.geojson")
	center := filepath.Join(dir, "center.geojson")
	data := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"group_id":"17","side":"left","surface_type":"platform_top"},"geometry":{"type":"Polygon","coordinates":[[[116,30.00001,2],[116.0001,30.00001,2],[116.0001,30.00002,2],[116,30.00002,2],[116,30.00001,2]]]}},{"type":"Feature","properties":{"group_id":"17","side":"left","surface_type":"platform_inner_wall"},"geometry":{"type":"Polygon","coordinates":[[[116,30.00001,0],[116.0001,30.00001,0],[116.0001,30.00001,2],[116,30.00001,2],[116,30.00001,0]]]}}]}`
	if err := os.WriteFile(input, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(center, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"group_id":17},"geometry":{"type":"MultiLineString","coordinates":[[[116,30,0],[116.00005,30,0]],[[116.00005,30,0],[116.0001,30,0]]]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAP3D_TUNNEL_CENTERLINE", center)
	t.Setenv("MAP3D_TUNNEL_PLATFORM_TILE_SIZE", "0.5")
	t.Setenv("MAP3D_TUNNEL_PLATFORM_WALL_TEXTURE", "assets/image_6.png")
	t.Setenv("MAP3D_TUNNEL_PLATFORM_TOP_TEXTURE", "assets/RMLFJ00001.jpg")
	if err := generateTunnelPlatformGlbTileset(t, dir, input); err != nil {
		t.Fatal(err)
	}
	doc, err := gltf.Open(filepath.Join(dir, "platform", "platform.glb"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Meshes[0].Primitives) != 2 || len(doc.Images) != 2 {
		t.Fatal("missing surfaces or textures")
	}
	for _, texture := range doc.Textures {
		if texture.Sampler == nil || doc.Samplers[*texture.Sampler].WrapS != gltf.WrapRepeat || doc.Samplers[*texture.Sampler].WrapT != gltf.WrapRepeat {
			t.Fatal("texture does not repeat")
		}
	}
	for _, p := range doc.Meshes[0].Primitives {
		uv, err := modeler.ReadTextureCoord(doc, doc.Accessors[p.Attributes[gltf.TEXCOORD_0]], nil)
		if err != nil {
			t.Fatal(err)
		}
		if *p.Material == 1 && math.Abs(float64(uv[1][0]-uv[0][0])) < 18 {
			t.Fatal("platform tile count does not follow meter length")
		}
	}
	if err := os.WriteFile(input, []byte(strings.ReplaceAll(data, `"group_id":"17"`, `"group_id":"18"`)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := generateTunnelPlatformGlbTileset(t, dir, input); err == nil || !strings.Contains(err.Error(), "no centerline") {
		t.Fatalf("missing centerline: %v", err)
	}
	t.Setenv("MAP3D_TUNNEL_PLATFORM_TILE_SIZE", "-1")
	if err := generateTunnelPlatformGlbTileset(t, dir, input); err == nil {
		t.Fatal("invalid tile size accepted")
	}
}

func TestPlatformLongStripUsesLocalSegments(t *testing.T) {
	line := &tunnelCenterline{points: [][3]float32{{0, 0, 0}, {8, 0, 0}}, stations: []float64{0, 8}}
	pos := [][3]float32{{0, 1, 1}, {4, 1, 1.5}, {8, 1, 1}, {8, 1, 2}, {4, 1, 2.5}, {0, 1, 2}}
	indices, err := triangulatePlatformStripCDT(pos, line)
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 12 {
		t.Fatal(indices)
	}
	for segment := 0; segment < 2; segment++ {
		allowed := map[uint32]bool{uint32(segment): true, uint32(segment + 1): true, uint32(4 - segment): true, uint32(5 - segment): true}
		for _, index := range indices[segment*6 : segment*6+6] {
			if !allowed[index] {
				t.Fatal("triangle spans unrelated sections")
			}
		}
	}
}

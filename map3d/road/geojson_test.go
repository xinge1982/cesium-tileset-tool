package road

import (
	"cesium-tileset-tool/map3d/mgltf"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestGeojson3D(t *testing.T) {
	var basePoint = []float64{286051.17951776314294, 3487343.447437774855644, 19.189059725962}
	f := `C:\MapABC\code\roadgltf\road32651.geojson`
	buf, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	fc := &geojson.FeatureCollection{}
	err = json.Unmarshal(buf, fc)
	if err != nil {
		t.Fatal(err)
	}
	doc := gltf.NewDocument()
	faces := []*gltf.Primitive{}
	img := `C:\MapABC\code\roadgltf\resources\textures\asphalt_shoulder.jpg`
	img = `C:\MapABC\code\roadgltf\resources\road\lm.jpeg`

	materIndex, err := mgltf.TextureMaterial(doc, img)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("box:", fc.Features[0].Geometry.Bounds())
	bounds := fc.Features[0].Geometry.Bounds().Clone()

	for i := range fc.Features {
		pos := mgltf.ConvertToRelativeCoordinates(fc.Features[i].Geometry.FlatCoords(), basePoint)
		if err != nil {
			t.Fatal(err)
		}
		face, err := mgltf.Primitive(doc, pos, materIndex, mgltf.UvsParams{
			RepeatX: 256,
			RepeatY: 256,
			Axis:    "XY",
			FlipV:   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		faces = append(faces, face)

	}
	fmt.Println("bounds:", bounds)
	//err = mgltf.SaveGltfForPrimitive(faces, doc, `C:\Users\lujie\Desktop\tool\poc\html\road.gltf`)
	err = mgltf.SaveGlbForPrimitive(faces, doc, `C:\MapABC\web\shanghai\r.glb`)

	if err != nil {
		t.Fatal(err)
	}

}
func TestLJD(t *testing.T) {
	var basePoint = []float64{286051.17951776314294, 3487343.447437774855644, 19.189059725962}
	f := `C:\MapABC\code\roadgltf\lj32651.geojson`
	buf, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	fc := &geojson.FeatureCollection{}
	err = json.Unmarshal(buf, fc)
	if err != nil {
		t.Fatal(err)
	}
	doc := gltf.NewDocument()
	faces := []*gltf.Primitive{}
	img := `C:\MapABC\code\roadgltf\resources\road\lj.jpeg`
	//img = `C:\MapABC\code\roadgltf\resources\road\lumian.jpg`

	materIndex, err := mgltf.TextureMaterial(doc, img)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("box:", fc.Features[0].Geometry.Bounds())
	bounds := fc.Features[0].Geometry.Bounds().Clone()

	for i := range fc.Features {

		pos := mgltf.ConvertToRelativeCoordinates(fc.Features[i].Geometry.FlatCoords(), basePoint)
		fmt.Println("---:", pos)
		if err != nil {
			t.Fatal(err)
		}

		face, err := mgltf.Primitive(doc, pos, materIndex, mgltf.UvsParams{
			RepeatX: 256,
			RepeatY: 256,
			Axis:    "XY",
			FlipV:   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		faces = append(faces, face)

	}
	fmt.Println("bounds:", bounds)
	//err = mgltf.SaveGltfForPrimitive(faces, doc, `C:\Users\lujie\Desktop\tool\poc\html\lj.gltf`)
	err = mgltf.SaveGlbForPrimitive(faces, doc, `C:\MapABC\web\shanghai\r.glb`)
	if err != nil {
		t.Fatal(err)
	}

}

type Plane struct {
	geom.Polygon
}

type Coodrs [][3]float32

func (c Coodrs) flatCoords() {

}

func Test_Gltf(t *testing.T) {
	doc := gltf.NewDocument()
	var c Coodrs = [][3]float32{
		{0, 0, 0},
		{1, 0, 0},
		{1, 1, 0},
		{0, 1, 0},
	}
	img := `C:\MapABC\code\roadgltf\resources\road\img_1.png`

	materIndex, err := mgltf.TextureMaterial(doc, img)
	if err != nil {
		t.Fatal(err)
	}
	face, err := mgltf.Primitive(doc, c, materIndex, mgltf.UvsParams{})
	if err != nil {
		t.Fatal(err)
	}
	mgltf.SaveGltfForPrimitive([]*gltf.Primitive{face}, doc, `C:\Users\lujie\Desktop\tool\poc\html\road.gltf`)
}

func Geometry(geometry geom.T) [][3]float32 {
	coords := len(geometry.FlatCoords()) / 3
	var pos [][3]float32
	for i := 0; i < coords-3; i += 3 {
		pos = append(pos, [3]float32{float32(geometry.FlatCoords()[i]), float32(geometry.FlatCoords()[i+1]), float32(geometry.FlatCoords()[i+2])})
	}
	for i := range pos {
		fmt.Println(pos[i])
	}
	return pos
}

package mapmodel

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom/encoding/wkt"
)

type TileSetHash struct {
	GeoHash string         `json:"geoHash"`
	Bbox    string         `json:"bbox"`
	Center  string         `json:"center"`
	Models  []TileSetModel `json:"models"`
}

type TileSetModel struct {
	Model    string  `json:"model"`
	ObjAngle float64 `json:"objAngle"`
	Location string  `json:"location"`
}

func TestNewBuildModelsBatch(t *testing.T) {

	//box: [111.005859 30.541992 111.049805 30.585938] &{{1 2 [111.027832 30.563964999999996] 0}}

	var box [4]float64 = [4]float64{111.005859, 30.541992, 111.049805, 30.585938}
	var center = []float64{111.027832, 30.563964999999996, 0}

	bm, err := NewBuildModels(center, box, &BmOption{RootNode: true})
	if err != nil {
		t.Fatal(err)
	}
	//T := [][3]float64{{111.005859, 30.541992, 0}}
	T := [][3]float64{{center[0], center[1], 0}}
	//T1 := [][3]float64{{center[0], center[1], 0}}

	err = bm.AddModel(makeModel("glb/qingfu_4051000124.glb", T, [][3]float64{{0, 0, 0}}))
	err = bm.AddModel(makeModel("glb/lmj_01_03.glb", T, [][3]float64{{0, 0, 0}}))
	err = bm.AddModel(makeModel("glb/lmj_01_03.glb", T, [][3]float64{{45, 0, 0}}))
	err = bm.AddModel(makeModel("glb/lmj_01_03.glb", T, [][3]float64{{90, 0, 0}}))
	err = bm.AddModel(makeModel("glb/lmj_01_03.glb", T, [][3]float64{{135, 0, 0}}))
	T[0][0] += 0.005
	T[0][1] += 0.005

	T[0][2] += 10
	//
	err = bm.AddModel(makeModel("glb/qingfu_4051000124.glb", [][3]float64{T[0], T[0], T[0], T[0]}, [][3]float64{{180, 0, 0}, {235, 0, 0}, {280, 0, 0}, {315, 0, 0}}))
	if err != nil {
		panic(err)
	}
	//T[0][2] += 20.0
	//err = bm.AddModel(makeModel("glb/sdbj_01_01.glb", T, []float64{0}))
	//if err != nil {
	//	panic(err)
	//}
	glbbuf, err := bm.BuildBinary()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("C:\\MapABC\\web\\shanghai\\r.glb", glbbuf, 0644)
	fmt.Println("region:")
	for _, f := range bm.Region() {
		fmt.Printf("%.6f,", f)
	}
	//fmt.Println()

	//doc, err := bm.BuildGLTF()
	//if err != nil {
	//	panic(err)
	//}
	//gltf.Save(doc, "C:\\MapABC\\web\\shanghai\\r.gltf")
	//
}

func TestNewBuildModels(t *testing.T) {
	buf, _ := os.ReadFile("t.json")
	tsh := TileSetHash{}
	err := json.Unmarshal(buf, &tsh)
	if err != nil {
		t.Fatal(err)
	}

	g, err := wkt.Unmarshal(tsh.Bbox)
	if err != nil {
		t.Fatal(err)
	}
	var box [4]float64
	g.Bounds().Max(0)

	box[0] = g.Bounds().Min(0)
	box[1] = g.Bounds().Min(1)
	box[2] = g.Bounds().Max(0)
	box[3] = g.Bounds().Max(1)
	center, err := wkt.Unmarshal(tsh.Center)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("box:", box, center)

	bm, err := NewBuildModels(center.FlatCoords(), box)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range tsh.Models {
		p, err := wkt.Unmarshal(m.Location)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(p.FlatCoords())

	}

	mdoc, err := gltf.Open("glb/lmj_01_03.glb")
	if err != nil {
		t.Fatal(err)
	}

	m := &Model{
		Doc:    mdoc,
		Coords: make([][3]float64, 0),
		S:      make([][3]float64, 0),
		Fields: make(map[string][]string),
	}

	m.Coords = append(m.Coords, [3]float64{center.FlatCoords()[0], center.FlatCoords()[1], 10})
	m.R = append(m.R, [3]float64{0, 0, 0})
	m.S = append(m.S, [3]float64{1, 1, 1})
	m.Fields["id"] = []string{"1"}
	err = bm.AddModel(m)
	if err != nil {
		t.Fatal(err)
	}

	mdoc1, err := gltf.Open("glb/lmj_01_03.glb")
	if err != nil {
		t.Fatal(err)
	}

	m1 := &Model{
		Doc:    mdoc1,
		Coords: make([][3]float64, 0),
		S:      make([][3]float64, 0),
		Fields: make(map[string][]string),
	}

	m1.Coords = append(m1.Coords, [3]float64{center.FlatCoords()[0], center.FlatCoords()[1], 20})

	fmt.Println("m1", m1.Coords)

	m1.R = append(m1.R, [3]float64{45, 0, 0})
	m1.S = append(m1.S, [3]float64{1, 1, 1})
	m1.Fields["id"] = []string{"2"}
	err = bm.AddModel(m1)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("region:")
	for _, f := range bm.Region() {
		fmt.Printf("%.6f,", f)
	}
	fmt.Println()

	fmt.Println("transform:")

	for _, f := range bm.Transform() {
		fmt.Printf("%.6f,", f)
	}
	fmt.Println()

	glbbuf, err := bm.BuildBinary()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("C:\\MapABC\\web\\shanghai\\r.glb", glbbuf, 0644)

}

func makeModel(fn string, coords [][3]float64, r [][3]float64) *Model {
	n := "fid"
	if len(coords) > 1 {
		n = "gpu"
	}
	doc, err := gltf.Open(fn)
	if err != nil {
		panic(err)
	}
	s := make([][3]float64, 0)

	fields := make(map[string][]string)
	for i, _ := range coords {
		fields["id"] = append(fields["id"], fmt.Sprintf("%s-%s", n, strconv.Itoa(i)))
		s = append(s, gltf.DefaultScale)
	}
	return &Model{
		Doc:    doc,
		Fields: fields,
		Coords: coords,
		R:      r,
		S:      s,
	}
}

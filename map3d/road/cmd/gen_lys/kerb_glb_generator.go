package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	wgs84A  = 6378137.0
	wgs84F  = 1.0 / 298.257223563
	glbJSON = 0x4E4F534A
	glbBIN  = 0x004E4942
)

var (
	wgs84B  = wgs84A * (1.0 - wgs84F)
	wgs84E2 = 1.0 - (wgs84B*wgs84B)/(wgs84A*wgs84A)
)

type Vec2 struct{ X, Y float64 }
type Vec3 struct{ X, Y, Z float64 }

type GeoJSON struct {
	Type     string    `json:"type"`
	Name     string    `json:"name,omitempty"`
	Features []Feature `json:"features"`
}

type Feature struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Geometry   Geometry               `json:"geometry"`
}

type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type MeshData struct {
	Positions []float32
	Normals   []float32
	UVs       []float32
	Indices   []uint32
}

type GLTF struct {
	Asset       Asset        `json:"asset"`
	Scene       int          `json:"scene"`
	Scenes      []Scene      `json:"scenes"`
	Nodes       []Node       `json:"nodes"`
	Meshes      []Mesh       `json:"meshes"`
	Materials   []Material   `json:"materials,omitempty"`
	Textures    []Texture    `json:"textures,omitempty"`
	Images      []Image      `json:"images,omitempty"`
	Samplers    []Sampler    `json:"samplers,omitempty"`
	Buffers     []Buffer     `json:"buffers"`
	BufferViews []BufferView `json:"bufferViews"`
	Accessors   []Accessor   `json:"accessors"`
}

type Asset struct {
	Version   string `json:"version"`
	Generator string `json:"generator,omitempty"`
}

type Scene struct {
	Nodes []int `json:"nodes"`
}

type Node struct {
	Mesh int `json:"mesh"`
}

type Mesh struct {
	Primitives []Primitive `json:"primitives"`
}

type Primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   int            `json:"material,omitempty"`
	Mode       int            `json:"mode,omitempty"`
}

type Material struct {
	Name                 string               `json:"name,omitempty"`
	PBRMetallicRoughness PBRMetallicRoughness `json:"pbrMetallicRoughness"`
	NormalTexture        *NormalTextureInfo   `json:"normalTexture,omitempty"`
	DoubleSided          bool                 `json:"doubleSided,omitempty"`
}

type PBRMetallicRoughness struct {
	BaseColorTexture *TextureInfo `json:"baseColorTexture,omitempty"`
	MetallicFactor   float64      `json:"metallicFactor"`
	RoughnessFactor  float64      `json:"roughnessFactor"`
}

type TextureInfo struct {
	Index int `json:"index"`
}

type NormalTextureInfo struct {
	Index int     `json:"index"`
	Scale float64 `json:"scale,omitempty"`
}

type Texture struct {
	Sampler int `json:"sampler,omitempty"`
	Source  int `json:"source"`
}

type Image struct {
	BufferView int    `json:"bufferView"`
	MimeType   string `json:"mimeType"`
	Name       string `json:"name,omitempty"`
}

type Sampler struct {
	WrapS int `json:"wrapS,omitempty"`
	WrapT int `json:"wrapT,omitempty"`
	Mag   int `json:"magFilter,omitempty"`
	Min   int `json:"minFilter,omitempty"`
}

type Buffer struct {
	ByteLength int `json:"byteLength"`
}

type BufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset,omitempty"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

type Accessor struct {
	BufferView    int       `json:"bufferView"`
	ByteOffset    int       `json:"byteOffset,omitempty"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Max           []float32 `json:"max,omitempty"`
	Min           []float32 `json:"min,omitempty"`
}

type imageAsset struct {
	name     string
	mimeType string
	bytes    []byte
}

func main() {
	input := flag.String("input", "road_lys.geojson", "input curb GeoJSON in EPSG:4326 (lon,lat,height)")
	output := flag.String("output", "curbs.glb", "output GLB file")
	width := flag.Float64("width", 0.13, "curb width in meters")
	height := flag.Float64("height", 0.13, "curb height in meters")
	texUnit := flag.Float64("texUnit", 0.5, "texture repeat length along U in meters")
	albedo := flag.String("albedo", "", "optional albedo/baseColor texture (.png/.jpg), embedded into GLB")
	normal := flag.String("normal", "", "optional normal texture (.png/.jpg), embedded into GLB")
	flag.Parse()

	if err := run(*input, *output, *width, *height, *texUnit, *albedo, *normal); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run(input, output string, width, height, texUnit float64, albedoPath, normalPath string) error {
	raw, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var gj GeoJSON
	if err := json.Unmarshal(raw, &gj); err != nil {
		return err
	}
	if len(gj.Features) == 0 {
		return errors.New("no features found")
	}

	baseLon, baseLat, baseH, totalPts, err := computeBasePoint(gj.Features)
	if err != nil {
		return err
	}
	baseECEF := geodeticToECEF(baseLon, baseLat, baseH)

	var mesh MeshData
	used := 0
	for _, ft := range gj.Features {
		lines, err := decodeLines(ft.Geometry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip feature: %v\n", err)
			continue
		}
		dir := readDirection(ft.Properties)
		for _, line := range lines {
			enu := make([]Vec3, 0, len(line))
			for _, llh := range line {
				if len(llh) < 2 {
					continue
				}
				h := 0.0
				if len(llh) > 2 {
					h = llh[2]
				}
				enu = append(enu, geodeticToENU(llh[0], llh[1], h, baseLon, baseLat, baseH, baseECEF))
			}
			clean := dedupeClosePoints(enu, 0.001)
			if len(clean) < 2 {
				continue
			}
			buildCurbMesh(&mesh, clean, dir, width, height, texUnit)
			used++
		}
	}
	if len(mesh.Indices) == 0 {
		return errors.New("no valid curb geometry was generated")
	}

	images, err := loadImages(albedoPath, normalPath)
	if err != nil {
		return err
	}

	if err := writeGLB(output, mesh, images); err != nil {
		return err
	}

	fmt.Printf("Generated %s\n", output)
	fmt.Printf("BasePoint (lon, lat, h): %.12f, %.12f, %.4f\n", baseLon, baseLat, baseH)
	fmt.Printf("Input points: %d, used curb lines: %d\n", totalPts, used)
	fmt.Printf("Vertices: %d, Triangles: %d\n", len(mesh.Positions)/3, len(mesh.Indices)/3)
	return nil
}

func computeBasePoint(features []Feature) (lon, lat, h float64, n int, err error) {
	minLon, minLat := math.MaxFloat64, math.MaxFloat64
	maxLon, maxLat := -math.MaxFloat64, -math.MaxFloat64
	sumH := 0.0
	total := 0
	for _, ft := range features {
		lines, derr := decodeLines(ft.Geometry)
		if derr != nil {
			continue
		}
		for _, line := range lines {
			for _, p := range line {
				if len(p) < 2 {
					continue
				}
				if p[0] < minLon {
					minLon = p[0]
				}
				if p[0] > maxLon {
					maxLon = p[0]
				}
				if p[1] < minLat {
					minLat = p[1]
				}
				if p[1] > maxLat {
					maxLat = p[1]
				}
				if len(p) > 2 {
					sumH += p[2]
				}
				total++
			}
		}
	}
	if total == 0 {
		return 0, 0, 0, 0, errors.New("no valid coordinates found")
	}
	lon = (minLon + maxLon) * 0.5
	lat = (minLat + maxLat) * 0.5
	h = sumH / float64(total)
	return lon, lat, h, total, nil
}

func decodeLines(g Geometry) ([][][]float64, error) {
	switch g.Type {
	case "LineString":
		var line [][]float64
		if err := json.Unmarshal(g.Coordinates, &line); err != nil {
			return nil, err
		}
		return [][][]float64{line}, nil
	case "MultiLineString":
		var lines [][][]float64
		if err := json.Unmarshal(g.Coordinates, &lines); err != nil {
			return nil, err
		}
		return lines, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type: %s", g.Type)
	}
}

func readDirection(props map[string]interface{}) int {
	v, ok := props["direction"]
	if !ok || v == nil {
		return 2
	}
	switch t := v.(type) {
	case float64:
		if int(t) == 1 {
			return 1
		}
		if int(t) == 2 {
			return 2
		}
	case string:
		if strings.TrimSpace(t) == "1" {
			return 1
		}
		if strings.TrimSpace(t) == "2" {
			return 2
		}
	}
	return 2
}

func buildCurbMesh(mesh *MeshData, line []Vec3, direction int, width, height, texUnit float64) {
	sign := -1.0
	if direction == 1 {
		sign = 1.0
	}
	n := len(line)
	normals2D := make([]Vec3, n)
	for i := 0; i < n; i++ {
		t := tangentAt(line, i)
		left := normalize(Vec3{-t.Y, t.X, 0})
		if length(left) == 0 {
			left = Vec3{0, 1, 0}
		}
		normals2D[i] = mul(left, sign)
	}

	dist := make([]float64, n)
	for i := 1; i < n; i++ {
		dist[i] = dist[i-1] + length(sub(line[i], line[i-1]))
	}

	topInner := make([]Vec3, n)
	topOuter := make([]Vec3, n)
	botInner := make([]Vec3, n)
	botOuter := make([]Vec3, n)
	for i := 0; i < n; i++ {
		p := line[i]
		off := add(p, mul(normals2D[i], width))
		botInner[i] = p
		botOuter[i] = off
		topInner[i] = add(p, Vec3{0, 0, height})
		topOuter[i] = add(off, Vec3{0, 0, height})
	}

	for i := 0; i < n-1; i++ {
		u0 := dist[i] / texUnit
		u1 := dist[i+1] / texUnit
		// top face: map into top band of the texture
		addQuad(mesh,
			topInner[i], topOuter[i], topOuter[i+1], topInner[i+1],
			Vec2{u0, 1.0}, Vec2{u0, 0.6}, Vec2{u1, 0.6}, Vec2{u1, 1.0},
		)
		// inner side face (reference edge)
		addQuad(mesh,
			botInner[i], botInner[i+1], topInner[i+1], topInner[i],
			Vec2{u0, 0.0}, Vec2{u1, 0.0}, Vec2{u1, 0.5}, Vec2{u0, 0.5},
		)
		// outer side face (expanded edge)
		addQuad(mesh,
			botOuter[i+1], botOuter[i], topOuter[i], topOuter[i+1],
			Vec2{u1, 0.0}, Vec2{u0, 0.0}, Vec2{u0, 0.5}, Vec2{u1, 0.5},
		)
	}

	// end caps, no bottom face.
	addQuad(mesh,
		botOuter[0], botInner[0], topInner[0], topOuter[0],
		Vec2{0, 0}, Vec2{1, 0}, Vec2{1, 1}, Vec2{0, 1},
	)
	li := n - 1
	addQuad(mesh,
		botInner[li], botOuter[li], topOuter[li], topInner[li],
		Vec2{0, 0}, Vec2{1, 0}, Vec2{1, 1}, Vec2{0, 1},
	)
}

func addQuad(mesh *MeshData, a, b, c, d Vec3, uva, uvb, uvc, uvd Vec2) {
	nrm := normalize(cross(sub(b, a), sub(c, a)))
	base := uint32(len(mesh.Positions) / 3)
	verts := []Vec3{a, b, c, d}
	uvs := []Vec2{uva, uvb, uvc, uvd}
	for i := 0; i < 4; i++ {
		mesh.Positions = append(mesh.Positions, float32(verts[i].X), float32(verts[i].Y), float32(verts[i].Z))
		mesh.Normals = append(mesh.Normals, float32(nrm.X), float32(nrm.Y), float32(nrm.Z))
		mesh.UVs = append(mesh.UVs, float32(uvs[i].X), float32(uvs[i].Y))
	}
	mesh.Indices = append(mesh.Indices,
		base, base+1, base+2,
		base, base+2, base+3,
	)
}

func tangentAt(line []Vec3, i int) Vec3 {
	if len(line) < 2 {
		return Vec3{1, 0, 0}
	}
	if i == 0 {
		return normalize(flatten(sub(line[1], line[0])))
	}
	if i == len(line)-1 {
		return normalize(flatten(sub(line[i], line[i-1])))
	}
	t1 := normalize(flatten(sub(line[i], line[i-1])))
	t2 := normalize(flatten(sub(line[i+1], line[i])))
	t := add(t1, t2)
	if length(t) < 1e-9 {
		t = t2
	}
	return normalize(t)
}

func flatten(v Vec3) Vec3        { return Vec3{v.X, v.Y, 0} }
func add(a, b Vec3) Vec3         { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func sub(a, b Vec3) Vec3         { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func mul(a Vec3, s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func cross(a, b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func length(v Vec3) float64 { return math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z) }
func normalize(v Vec3) Vec3 {
	l := length(v)
	if l < 1e-12 {
		return Vec3{}
	}
	return Vec3{v.X / l, v.Y / l, v.Z / l}
}

func dedupeClosePoints(in []Vec3, eps float64) []Vec3 {
	if len(in) == 0 {
		return nil
	}
	out := []Vec3{in[0]}
	for i := 1; i < len(in); i++ {
		if length(sub(in[i], out[len(out)-1])) > eps {
			out = append(out, in[i])
		}
	}
	return out
}

func geodeticToENU(lonDeg, latDeg, h, lon0Deg, lat0Deg, h0 float64, ecef0 Vec3) Vec3 {
	p := geodeticToECEF(lonDeg, latDeg, h)
	d := sub(p, ecef0)

	lon0 := lon0Deg * math.Pi / 180.0
	lat0 := lat0Deg * math.Pi / 180.0
	sinLon, cosLon := math.Sin(lon0), math.Cos(lon0)
	sinLat, cosLat := math.Sin(lat0), math.Cos(lat0)

	east := -sinLon*d.X + cosLon*d.Y
	north := -sinLat*cosLon*d.X - sinLat*sinLon*d.Y + cosLat*d.Z
	up := cosLat*cosLon*d.X + cosLat*sinLon*d.Y + sinLat*d.Z
	_ = h0
	return Vec3{east, north, up}
}

func geodeticToECEF(lonDeg, latDeg, h float64) Vec3 {
	lon := lonDeg * math.Pi / 180.0
	lat := latDeg * math.Pi / 180.0
	sinLat, cosLat := math.Sin(lat), math.Cos(lat)
	sinLon, cosLon := math.Sin(lon), math.Cos(lon)
	N := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
	x := (N + h) * cosLat * cosLon
	y := (N + h) * cosLat * sinLon
	z := (N*(1.0-wgs84E2) + h) * sinLat
	return Vec3{x, y, z}
}

func loadImages(albedoPath, normalPath string) ([]imageAsset, error) {
	var assets []imageAsset
	if albedoPath != "" {
		img, err := readImageAsset(albedoPath, "baseColor")
		if err != nil {
			return nil, err
		}
		assets = append(assets, img)
	}
	if normalPath != "" {
		img, err := readImageAsset(normalPath, "normal")
		if err != nil {
			return nil, err
		}
		assets = append(assets, img)
	}
	return assets, nil
}

func readImageAsset(path, defaultName string) (imageAsset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return imageAsset{}, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime := ""
	switch ext {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	default:
		return imageAsset{}, fmt.Errorf("unsupported texture type: %s", ext)
	}
	return imageAsset{name: defaultName, mimeType: mime, bytes: b}, nil
}

func writeGLB(path string, mesh MeshData, images []imageAsset) error {
	var bin bytes.Buffer

	posOffset := alignedOffset(&bin)
	if err := binary.Write(&bin, binary.LittleEndian, mesh.Positions); err != nil {
		return err
	}
	padTo4(&bin)

	nrmOffset := alignedOffset(&bin)
	if err := binary.Write(&bin, binary.LittleEndian, mesh.Normals); err != nil {
		return err
	}
	padTo4(&bin)

	uvOffset := alignedOffset(&bin)
	if err := binary.Write(&bin, binary.LittleEndian, mesh.UVs); err != nil {
		return err
	}
	padTo4(&bin)

	idxOffset := alignedOffset(&bin)
	if err := binary.Write(&bin, binary.LittleEndian, mesh.Indices); err != nil {
		return err
	}
	padTo4(&bin)

	imgInfos := make([]struct{ offset, length int }, len(images))
	for i, img := range images {
		imgInfos[i].offset = alignedOffset(&bin)
		if _, err := bin.Write(img.bytes); err != nil {
			return err
		}
		imgInfos[i].length = len(img.bytes)
		padTo4(&bin)
	}

	posMin, posMax := minMax3(mesh.Positions)

	views := []BufferView{
		{Buffer: 0, ByteOffset: posOffset, ByteLength: len(mesh.Positions) * 4, Target: 34962},
		{Buffer: 0, ByteOffset: nrmOffset, ByteLength: len(mesh.Normals) * 4, Target: 34962},
		{Buffer: 0, ByteOffset: uvOffset, ByteLength: len(mesh.UVs) * 4, Target: 34962},
		{Buffer: 0, ByteOffset: idxOffset, ByteLength: len(mesh.Indices) * 4, Target: 34963},
	}

	accessors := []Accessor{
		{BufferView: 0, ComponentType: 5126, Count: len(mesh.Positions) / 3, Type: "VEC3", Min: posMin, Max: posMax},
		{BufferView: 1, ComponentType: 5126, Count: len(mesh.Normals) / 3, Type: "VEC3"},
		{BufferView: 2, ComponentType: 5126, Count: len(mesh.UVs) / 2, Type: "VEC2"},
		{BufferView: 3, ComponentType: 5125, Count: len(mesh.Indices), Type: "SCALAR"},
	}

	gltf := GLTF{
		Asset:  Asset{Version: "2.0", Generator: "curb-glb-generator-go"},
		Scene:  0,
		Scenes: []Scene{{Nodes: []int{0}}},
		Nodes:  []Node{{Mesh: 0}},
		Meshes: []Mesh{{Primitives: []Primitive{{
			Attributes: map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2},
			Indices:    3,
			Mode:       4,
		}}}},
		Buffers:     []Buffer{{ByteLength: bin.Len()}},
		BufferViews: views,
		Accessors:   accessors,
	}

	if len(images) > 0 {
		gltf.Samplers = []Sampler{{WrapS: 10497, WrapT: 33071, Mag: 9729, Min: 9987}}
		gltf.Materials = []Material{{
			Name: "curb-material",
			PBRMetallicRoughness: PBRMetallicRoughness{
				MetallicFactor:  0.0,
				RoughnessFactor: 1.0,
			},
		}}
		gltf.Meshes[0].Primitives[0].Material = 0
		for i, img := range images {
			viewIndex := len(gltf.BufferViews)
			gltf.BufferViews = append(gltf.BufferViews, BufferView{Buffer: 0, ByteOffset: imgInfos[i].offset, ByteLength: imgInfos[i].length})
			gltf.Images = append(gltf.Images, Image{BufferView: viewIndex, MimeType: img.mimeType, Name: img.name})
			texIndex := len(gltf.Textures)
			gltf.Textures = append(gltf.Textures, Texture{Sampler: 0, Source: len(gltf.Images) - 1})
			switch img.name {
			case "baseColor":
				gltf.Materials[0].PBRMetallicRoughness.BaseColorTexture = &TextureInfo{Index: texIndex}
			case "normal":
				gltf.Materials[0].NormalTexture = &NormalTextureInfo{Index: texIndex, Scale: 1.0}
			}
		}
	}

	jsonBytes, err := json.Marshal(gltf)
	if err != nil {
		return err
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, 0x20)
	}
	binBytes := bin.Bytes()
	for len(binBytes)%4 != 0 {
		binBytes = append(binBytes, 0)
	}

	totalLen := 12 + 8 + len(jsonBytes) + 8 + len(binBytes)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := binary.Write(f, binary.LittleEndian, uint32(0x46546C67)); err != nil { // glTF
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(2)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(totalLen)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(len(jsonBytes))); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(glbJSON)); err != nil {
		return err
	}
	if _, err := f.Write(jsonBytes); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(len(binBytes))); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(glbBIN)); err != nil {
		return err
	}
	if _, err := f.Write(binBytes); err != nil {
		return err
	}
	return nil
}

func minMax3(vals []float32) (minv, maxv []float32) {
	minv = []float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	maxv = []float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for i := 0; i < len(vals); i += 3 {
		for j := 0; j < 3; j++ {
			if vals[i+j] < minv[j] {
				minv[j] = vals[i+j]
			}
			if vals[i+j] > maxv[j] {
				maxv[j] = vals[i+j]
			}
		}
	}
	return minv, maxv
}

func alignedOffset(buf *bytes.Buffer) int {
	padTo4(buf)
	return buf.Len()
}

func padTo4(buf *bytes.Buffer) {
	for buf.Len()%4 != 0 {
		_ = buf.WriteByte(0)
	}
}

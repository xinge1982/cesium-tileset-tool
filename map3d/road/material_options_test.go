package road

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Resolve each part independently and preserve legacy board-only texture behavior.
func TestRoadMaterialFallbacks(t *testing.T) {
	legacy, err := ResolveRoadMaterials(BuildTypeBarrierWave, "wave_two", "legacy.png", nil, nil)
	if err != nil || legacy.Top.Path != "legacy.png" || legacy.Side.Path != "" || legacy.Side.Color != "#8D949C" {
		t.Fatal(legacy, err)
	}
	defaults := map[string]RoadMaterialOptions{"rail": {TexturePath: "default.png"}, "post": {Color: "#112233"}}
	overrides := map[string]map[string]RoadMaterialOptions{
		"wave_three":    {"rail": {TexturePath: "three.png"}},
		"wave_nose_end": {"rail": {Color: "#445566"}, "post": {}},
	}
	before, _ := json.Marshal(overrides)
	for _, tc := range []struct{ kind, path, color string }{{"wave_two", "default.png", ""}, {"wave_three", "three.png", ""}, {"wave_nose_end", "", "#445566"}} {
		set, err := ResolveRoadMaterials(BuildTypeBarrierWave, tc.kind, "legacy.png", defaults, overrides)
		if err != nil || set.Top.Path != tc.path || set.Top.Color != tc.color || set.Side.Color != "#112233" {
			t.Fatal(tc, set, err)
		}
	}
	after, _ := json.Marshal(overrides)
	if !bytes.Equal(before, after) {
		t.Fatal("material maps were mutated")
	}
	surface, err := ResolveRoadMaterials(BuildTypeRoadSurface, "road_surface", "legacy.png", map[string]RoadMaterialOptions{"top": {TexturePath: "top.png"}, "side": {Color: "#123456"}}, map[string]map[string]RoadMaterialOptions{"road_surface": {"top": {Color: "#ABCDEF"}}})
	if err != nil || surface.Top.Path != "" || surface.Top.Color != "#ABCDEF" || surface.Side.Color != "#123456" || surface.Bottom.Path != "legacy.png" {
		t.Fatal(surface, err)
	}
	fallback, err := ResolveRoadMaterials(BuildTypeRoadSurface, "road_surface", "", nil, nil)
	if err != nil || fallback.Top.Color != "#555555" || !reflect.DeepEqual(fallback.Top, fallback.Bottom) {
		t.Fatal(fallback, err)
	}
	// Empty overrides inherit the configured default, not a stale legacy texture.
	inherited, err := ResolveRoadMaterials(BuildTypeBarrierWave, "wave_three", "legacy.png", defaults, map[string]map[string]RoadMaterialOptions{"wave_three": {"rail": {}}})
	if err != nil || inherited.Top.Path != "default.png" {
		t.Fatal(inherited, err)
	}
}

// Fail clearly for misspelled material slots, subtype keys and conflicting values.
func TestRoadMaterialValidation(t *testing.T) {
	for _, tc := range []struct {
		build     BuildType
		defaults  map[string]RoadMaterialOptions
		overrides map[string]map[string]RoadMaterialOptions
	}{
		{BuildTypeBarrierWave, map[string]RoadMaterialOptions{"top": {Color: "#ffffff"}}, nil},
		{BuildTypeRoadSurface, map[string]RoadMaterialOptions{"rail": {Color: "#ffffff"}}, nil},
		{BuildTypeBarrierWave, map[string]RoadMaterialOptions{"rail": {TexturePath: "a.png", Color: "#ffffff"}}, nil},
		{BuildTypeBarrierWave, nil, map[string]map[string]RoadMaterialOptions{"wave_tree": {"rail": {TexturePath: "a.png"}}}},
		{BuildTypeRoadSurface, nil, map[string]map[string]RoadMaterialOptions{"road_surface": {"post": {Color: "#ffffff"}}}},
	} {
		if err := ValidateRoadMaterials(tc.build, tc.defaults, tc.overrides); err == nil {
			t.Fatal("invalid materials accepted", tc)
		}
	}
}

func writeRoadTestTexture(t *testing.T, directory, name string, value color.RGBA) string {
	t.Helper()
	path := filepath.Join(directory, name+".png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetRGBA(x, y, value)
		}
	}
	if err = png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func roadTestMaterialPixel(t *testing.T, doc *gltf.Document, material int) color.RGBA {
	t.Helper()
	pbr := doc.Materials[material].PBRMetallicRoughness
	if pbr == nil || pbr.BaseColorTexture == nil {
		t.Fatal("material is not textured")
	}
	texture := doc.Textures[pbr.BaseColorTexture.Index]
	if texture.Source == nil {
		t.Fatal("texture has no image")
	}
	im := doc.Images[*texture.Source]
	if im.BufferView == nil {
		t.Fatal("texture image is not embedded")
	}
	view := doc.BufferViews[*im.BufferView]
	data := doc.Buffers[view.Buffer].Data[view.ByteOffset : view.ByteOffset+view.ByteLength]
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := decoded.At(0, 0).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

// Inspect the generated GLB, including textures on instanced posts and nose-end boards.
func TestGeneratedRoadPartTextures(t *testing.T) {
	dir := t.TempDir()
	colors := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255}}
	paths := []string{}
	for i, name := range []string{"two", "three", "nose", "post"} {
		paths = append(paths, writeRoadTestTexture(t, dir, name, colors[i]))
	}
	fc := &geojson.FeatureCollection{}
	for i, kind := range []string{"wave_two", "wave_three", "wave_nose_end"} {
		x := 121 + float64(i)*.0002
		fc.Features = append(fc.Features, &geojson.Feature{Geometry: geom.NewLineString(geom.XYZ).MustSetCoords([]geom.Coord{{x, 31, 10}, {x + .0001, 31, 10}}), Properties: map[string]any{"id": kind, "feature_type": kind, "direction": 2}})
	}
	opt := RoadModelOptions{BuildType: BuildTypeBarrierWave, OutputDir: filepath.Join(dir, "tileset"), Precision: 6,
		Materials:         map[string]RoadMaterialOptions{"rail": {TexturePath: paths[0]}, "post": {TexturePath: paths[3]}},
		MaterialOverrides: map[string]map[string]RoadMaterialOptions{"wave_three": {"rail": {TexturePath: paths[1]}}, "wave_nose_end": {"rail": {TexturePath: paths[2]}}},
	}
	before, _ := json.Marshal(fc)
	converted, err := BarrierWaveFeaturesFromGeoJSON(fc, opt)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range converted {
		if f.Material.Top.Path != paths[i] || f.Material.Side.Path != paths[3] {
			t.Fatal("subtype or part mapping incorrect", f.Material)
		}
	}
	result, err := BuildRoadModelSlices(context.Background(), RoadModelInput{Features: fc}, opt)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[color.RGBA]bool{}
	postNodes := 0
	for _, tile := range result.Tiles {
		doc, err := gltf.Open(tile.GLBPath)
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateStructuralMetadataBufferViews(doc); err != nil {
			t.Fatal(err)
		}
		for _, node := range doc.Nodes {
			if node.Mesh == nil {
				continue
			}
			for _, primitive := range doc.Meshes[*node.Mesh].Primitives {
				if primitive.Material == nil {
					t.Fatal("missing primitive material")
				}
				pixel := roadTestMaterialPixel(t, doc, *primitive.Material)
				seen[pixel] = true
				if node.Extensions["EXT_mesh_gpu_instancing"] != nil {
					postNodes++
					if pixel != colors[3] {
						t.Fatal("post used board texture", pixel)
					}
				}
			}
		}
	}
	for _, value := range colors {
		if !seen[value] {
			t.Fatal("texture not used by rendered geometry", value, seen)
		}
	}
	if postNodes < 2 {
		t.Fatal("textured posts lost instancing")
	}
	after, _ := json.Marshal(fc)
	if !bytes.Equal(before, after) {
		t.Fatal("material conversion mutated source properties")
	}
	// Surface top overrides must also reach the actual material in the output GLB.
	polygon := geom.NewPolygon(geom.XYZ).MustSetCoords([][]geom.Coord{{{121, 31, 10}, {121.0001, 31, 10}, {121.0001, 31.0001, 10}, {121, 31.0001, 10}, {121, 31, 10}}})
	surface, err := BuildRoadModelSlices(context.Background(), RoadModelInput{Features: &geojson.FeatureCollection{Features: []*geojson.Feature{{Geometry: polygon, Properties: map[string]any{"id": "surface"}}}}}, RoadModelOptions{BuildType: BuildTypeRoadSurface, OutputDir: filepath.Join(dir, "surface"), Materials: map[string]RoadMaterialOptions{"top": {TexturePath: paths[0]}}, MaterialOverrides: map[string]map[string]RoadMaterialOptions{"road_surface": {"top": {TexturePath: paths[1]}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tile := range surface.Tiles {
		doc, err := gltf.Open(tile.GLBPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, mesh := range doc.Meshes {
			for _, p := range mesh.Primitives {
				if p.Material == nil || roadTestMaterialPixel(t, doc, *p.Material) != colors[1] {
					t.Fatal("surface top override was not used")
				}
			}
		}
	}
}

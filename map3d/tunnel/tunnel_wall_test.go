package tunnel

import (
	"bytes"
	"cesium-tileset-tool/map3d/mgltf"
	"cesium-tileset-tool/map3d/texture"
	"cesium-tileset-tool/utils"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type PlatformFaceGroup struct {
	Side        string
	SurfaceType string
	Material    int
	Polygons    [][][3]float32
}

func platformGroupKey(side, surfaceType string) string {
	return side + "|" + surfaceType
}

func TestRoadWallBuildLine(t *testing.T) {

	var files []string
	baseDir := os.Getenv("MAP3D_TUNNEL_INPUT")
	if baseDir == "" {
		t.Skip("set MAP3D_TUNNEL_INPUT to a GeoJSON file or directory")
	}
	outPath := os.Getenv("MAP3D_TUNNEL_OUTPUT")
	if outPath == "" {
		outPath = t.TempDir()
	}
	if stat, err := os.Stat(baseDir); err == nil {
		if stat.IsDir() {
			err := filepath.Walk(baseDir, func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					return err
				}

				// 判断是否是文件
				if info.IsDir() {
					return nil
				}

				_, fn := filepath.Split(info.Name())
				ext := filepath.Ext(fn)
				if ext == ".geojson" {
					files = append(files, path)
				}

				return nil
			})

			if err != nil {
				t.Fatal(err)
			}
		} else {
			_, fn := filepath.Split(baseDir)
			ext := filepath.Ext(fn)
			if ext == ".geojson" {
				files = append(files, baseDir)
			}
		}
	} else {
		t.Fatal(fmt.Errorf("error in config file, file not exists: %s", baseDir))
	}

	for _, gs := range files {
		err := generateTunnelWallGlbTileset(t, outPath, gs)
		if err != nil {
			t.Fatalf("failed to generate tileset %s: %s", gs, err.Error())
		}
	}
}

func generateTunnelWallGlbTileset(t *testing.T, outPath, geojsonPath string) error {
	buf, err := os.ReadFile(geojsonPath)
	if err != nil {
		return fmt.Errorf("read geojson %s: %v", geojsonPath, err)
	}

	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		return fmt.Errorf("unmarshal geojson: %v", err)
	}
	if len(fc.Features) == 0 {
		return fmt.Errorf("geojson has no features: %s", geojsonPath)
	}

	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)
	var count int

	for i := range fc.Features {
		g := fc.Features[i].Geometry
		if g == nil {
			continue
		}
		flat := g.FlatCoords()
		stride := g.Stride()
		if stride < 2 {
			continue
		}
		for j := 0; j+stride-1 < len(flat); j += stride {
			lon := flat[j]
			lat := flat[j+1]

			if lon < minLon {
				minLon = lon
			}
			if lon > maxLon {
				maxLon = lon
			}
			if lat < minLat {
				minLat = lat
			}
			if lat > maxLat {
				maxLat = lat
			}
			count++
		}
	}
	if !isFinite(minLon) || !isFinite(minLat) || !isFinite(maxLon) || !isFinite(maxLat) {
		return fmt.Errorf("failed to compute bounds for %s", geojsonPath)
	}

	originLon := (minLon + maxLon) / 2
	originLat := (minLat + maxLat) / 2
	originH := 0.0

	t.Logf("box: %.8f, %.8f,%.8f, %.8f", minLon, minLat, maxLon, maxLat)
	t.Logf("origin lon/lat/h: %.8f, %.8f, %.3f", originLon, originLat, originH)

	var centerlines map[string]*tunnelCenterline
	repeatU, repeatV := 1.0, 1.0
	for name, target := range map[string]*float64{"MAP3D_TUNNEL_TEXTURE_REPEAT_U": &repeatU, "MAP3D_TUNNEL_TEXTURE_REPEAT_V": &repeatV} {
		if value := os.Getenv(name); value != "" {
			parsed, parseErr := strconv.ParseFloat(value, 64)
			if parseErr != nil || !isFinite(parsed) || parsed <= 0 {
				return fmt.Errorf("%s must be a positive number in meters", name)
			}
			*target = parsed
		}
	}
	if path := os.Getenv("MAP3D_TUNNEL_CENTERLINE"); path != "" {
		centerlines, err = loadTunnelCenterlines(path, []float64{originLon, originLat, originH})
		if err != nil {
			return fmt.Errorf("centerline: %w", err)
		}
	}
	doc := gltf.NewDocument()

	//贴图
	wallImg, _ := filepath.Abs("./assets/RMLFZQ00002.jpg")
	if v := os.Getenv("MAP3D_TUNNEL_WALL_TEXTURE"); v != "" {
		wallImg = v
	}
	wallIdx, err := addWallMaterial(t, wallImg, doc, false)
	if err != nil {
		t.Fatal(err)
	}

	// 拱顶和平顶可单独配置贴图，未设置时复用墙面材质。
	roofIdx := wallIdx
	if roofImg := os.Getenv("MAP3D_TUNNEL_ROOF_TEXTURE"); roofImg != "" {
		for _, feature := range fc.Features {
			surfaceType, _ := feature.Properties["surface_type"].(string)
			if surfaceType == "arched_roof" || surfaceType == "flat_roof" {
				roofIdx, err = addWallMaterial(t, roofImg, doc, false)
				if err != nil {
					return fmt.Errorf("roof texture: %w", err)
				}
				break
			}
		}
	}

	portalIdx := -1
	for _, feature := range fc.Features {
		if feature.Properties["surface_type"] == "portal_wall" {
			portalImg := os.Getenv("MAP3D_TUNNEL_PORTAL_TEXTURE")
			if portalImg == "" {
				portalImg = "./assets/portal_wall.png"
			}
			portalIdx, err = addWallMaterial(t, portalImg, doc, true)
			if err != nil {
				return err
			}
			break
		}
	}

	//处理primitive
	var primitives []*gltf.Primitive
	var roofPositions [][3]float32
	var roofIndices []uint32
	roofWeld := NewVertexWelder(0.005) // 5mm 容差，可改 0.01

	roofSections := make(roofSectionGraph)
	var roofCenterUVs [][2]float32
	roofWelders := make(map[string]*VertexWelder)
	for i := range fc.Features {
		g := fc.Features[i].Geometry
		switch polygon := g.(type) {
		case *geom.Polygon:
			if len(polygon.Ends()) != 1 {
				return fmt.Errorf("feature %d: tunnel CDT expects one exterior ring; holes are unsupported", i)
			}
		case *geom.MultiPolygon:
			return fmt.Errorf("feature %d: split MultiPolygon into Polygon features before tunnel generation", i)
		}
		flat, stride, ok := outerRingFlatCoordsFromWGS84Geom(g)
		if !ok || stride < 2 {
			continue
		}

		pos := make([][3]float32, 0, len(flat)/stride)
		for j := 0; j+stride-1 < len(flat); j += stride {
			lon := flat[j]
			lat := flat[j+1]
			h := 0.0
			if stride >= 3 {
				h = flat[j+2]
			}

			e, n, u := wgs84ToLocal(lon, lat, h, originLon, originLat, originH)
			pos = append(pos, [3]float32{float32(e), float32(u), float32(-n)})
		}

		sourceRing := pos
		pos = normalizeClosedRing3D(pos)
		if len(pos) < 3 {
			t.Logf("skip degenerate feature %d (%v): fewer than three distinct consecutive vertices", i, fc.Features[i].Properties["surface_type"])
			continue
		}

		surfaceType, _ := fc.Features[i].Properties["surface_type"].(string)
		var reference *tunnelCenterline
		groupID := tunnelGroupID(fc.Features[i].Properties["group_id"])
		if centerlines != nil && (surfaceType == "left_wall" || surfaceType == "right_wall" || surfaceType == "arched_roof" || surfaceType == "flat_roof") {
			reference = centerlines[groupID]
			if reference == nil {
				return fmt.Errorf("feature %d: no centerline for group_id %q", i, groupID)
			}
		}

		if surfaceType == "arched_roof" || surfaceType == "flat_roof" {
			localIndices, err := mgltf.CDTRingKeepWinding(pos)
			if err != nil {
				return fmt.Errorf("roof feature %d: %w", i, err)
			}
			welder := roofWeld
			if reference != nil {
				if roofWelders[groupID] == nil {
					roofWelders[groupID] = NewVertexWelder(0.005)
				}
				welder = roofWelders[groupID]
			}
			localToGlobal := make([]uint32, len(pos))
			for k, p := range pos {
				idx := welder.Add(p, &roofPositions)
				if reference != nil && int(idx) == len(roofCenterUVs) {
					s, l := reference.coordinates(p)
					roofCenterUVs = append(roofCenterUVs, [2]float32{float32(s / repeatU), float32(l / repeatV)})
				}
				localToGlobal[k] = idx
			}

			if reference != nil {
				sectionIDs := localToGlobal
				ordered := fc.Features[i].Properties["from_station"] != nil && fc.Features[i].Properties["to_station"] != nil
				if ordered && len(sourceRing) == 5 && sourceRing[0] == sourceRing[4] {
					sectionIDs = make([]uint32, 4)
					for j, p := range sourceRing[:4] {
						sectionIDs[j] = welder.Add(p, &roofPositions)
					}
				}
				if err := roofSections.addQuad(sectionIDs, roofPositions, reference, ordered); err != nil {
					return fmt.Errorf("roof feature %d UV: %w", i, err)
				}
			}
			for _, idx := range localIndices {
				roofIndices = append(roofIndices, localToGlobal[idx])
			}

			continue
		} else if surfaceType == "left_wall" || surfaceType == "right_wall" {
			face, err := TunnelWallPrimitive(doc, pos, surfaceType, wallIdx, mgltf.UvsParams{RepeatX: 1, RepeatY: 1})
			if err != nil {
				return fmt.Errorf("%s feature %d: %w", surfaceType, i, err)
			}
			if reference != nil {
				face.Attributes[gltf.TEXCOORD_0] = modeler.WriteTextureCoord(doc, tunnelWallUVs(pos, reference, repeatU, repeatV))
			}
			primitives = append(primitives, face)
			continue
		} else if surfaceType == "portal_wall" {
			face, errT := TunnelPortalPrimitive(
				doc,
				pos,
				portalIdx,
				mgltf.UvsParams{RepeatX: 1, RepeatY: 1},
			)
			if errT != nil {
				return fmt.Errorf("portal primitive: %v", errT)
			}
			primitives = append(primitives, face)
		} else if surfaceType == "tunnel_name_sign" {
			tunnelName := utils.ToString(fc.Features[i].Properties["tunnel_name"])
			width := utils.ToFloat64(fc.Features[i].Properties["sign_width"])
			height := utils.ToFloat64(fc.Features[i].Properties["sign_height"])

			fontPath := os.Getenv("MAP3D_TUNNEL_SIGN_FONT")
			if fontPath == "" {
				fontPath = "./assets/simkai.ttf"
			}
			signMaterialIdx, errM := addSignNameMaterial(t, doc, width, height, tunnelName, fontPath)
			if errM != nil {
				return errM
			}
			face, errT := TunnelSignPrimitive(
				doc,
				pos,
				signMaterialIdx,
				mgltf.UvsParams{FlipU: true},
			)
			if errT != nil {
				return fmt.Errorf("portal primitive: %v", errT)
			}
			primitives = append(primitives, face)
		}
	}

	// 循环结束后，统一生成弧顶 primitive
	if len(roofPositions) >= 3 && len(roofIndices) >= 3 {
		//FixRoofWinding(
		//	roofPositions,
		//	roofIndices,
		//)
		const tileSize = float32(1.0)
		roofNormals := GenerateSmoothNormalsByIndices(roofPositions, roofIndices)
		roofUVs := GenerateRoofUVsByZY(roofPositions, 40, 1)
		for i, p := range roofPositions {
			// Pick projection plane for roof.
			// For mostly horizontal/sloped roofs, use X/Y or X/Z depending on your coordinate system.
			u := p[0] / tileSize // X
			v := p[1] / tileSize // Y, or p[2] if Z is your depth axis

			roofUVs[i] = [2]float32{u, v}
		}
		if centerlines != nil {
			if err := roofSections.apply(roofCenterUVs, repeatV); err != nil {
				return err
			}
			roofUVs = roofCenterUVs
		}
		roofPrim := &gltf.Primitive{
			Attributes: gltf.PrimitiveAttributes{
				gltf.POSITION:   modeler.WritePosition(doc, roofPositions),
				gltf.NORMAL:     modeler.WriteNormal(doc, roofNormals),
				gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, roofUVs),
			},
			Indices:  gltf.Index(modeler.WriteIndices(doc, roofIndices)),
			Material: gltf.Index(roofIdx),
		}

		primitives = append(primitives, roofPrim)
	}

	if len(primitives) == 0 {
		return fmt.Errorf("no primitives built from %s", geojsonPath)
	}

	glb, err := mgltf.BytesForPrimitive(primitives, doc)
	if err != nil {
		t.Fatalf("save glb: %v", err)
	}

	basePoint, _, err := calcTileContextFromFeatureCollections(fc)
	if err != nil {
		t.Fatalf("calc tile context: %v", err)
	}

	_, fn := filepath.Split(geojsonPath)
	ext := filepath.Ext(fn)
	baseName := fn[:len(fn)-len(ext)]
	outDir := filepath.Join(outPath, baseName)
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		// 目录不存在，创建
		if errC := os.MkdirAll(outDir, os.ModePerm); errC != nil {
			t.Error(errC)
		}
	}

	if err := writeRoadTileOutputs(outDir, baseName, glb, basePoint, true); err != nil {
		t.Fatalf("write road surface: %v", err)
	}

	fmt.Println("✅ success write road surface of folder: " + outDir)
	return nil
}

func addWallMaterial(t *testing.T, img string, doc *gltf.Document, doubleSide bool) (int, error) {
	tre, err := texture.NewTexture(img)
	if err != nil {
		return -1, err
	}
	imgIdx, err := modeler.WriteImage(doc, tre.Name(), tre.Format(), bytes.NewReader(tre.GetBuf()))
	if err != nil {
		return -1, err
	}
	doc.Textures = append(doc.Textures, &gltf.Texture{
		Source: gltf.Index(imgIdx),
	},
	)
	doc.Materials = append(doc.Materials, &gltf.Material{
		Name: "Texture" + tre.Name(),
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{
				Index: len(doc.Textures) - 1,
			},
			MetallicFactor:  gltf.Float(0),
			RoughnessFactor: gltf.Float(1),
		},
		EmissiveTexture: &gltf.TextureInfo{
			Index: len(doc.Textures) - 1,
		},
		EmissiveFactor: [3]float64{0.5, 0.5, 0.5},
		DoubleSided:    doubleSide,
	})
	material := len(doc.Materials) - 1
	return material, nil
}

func addSignNameMaterial(
	t *testing.T,
	doc *gltf.Document,
	signWidthMeter float64,
	signHeightMeter float64,
	signName string,
	fontPath string,
) (int, error) {

	// 根据牌子宽高比例生成图片尺寸
	baseHeight := 512
	aspect := signWidthMeter / signHeightMeter
	if aspect <= 0 {
		aspect = 4.0
	}

	imgW := int(math.Round(float64(baseHeight) * aspect))
	imgH := baseHeight

	if imgW < 512 {
		imgW = 512
	}
	if imgW > 4096 {
		imgW = 4096
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// 背景色：深色牌匾
	draw.Draw(
		img,
		img.Bounds(),
		&image.Uniform{C: color.RGBA{R: 18, G: 28, B: 45, A: 255}},
		image.Point{},
		draw.Src,
	)

	// 可选边框
	borderColor := color.RGBA{R: 220, G: 220, B: 200, A: 255}
	border := 8
	for i := 0; i < border; i++ {
		drawRectangleBorder(img, i, borderColor)
	}

	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		return -1, err
	}

	ft, err := opentype.Parse(fontBytes)
	if err != nil {
		return -1, err
	}

	fontSize := float64(imgH) * 0.8

	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return -1, err
	}
	defer face.Close()

	textColor := image.NewUniform(color.RGBA{R: 245, G: 230, B: 170, A: 255})

	d := &font.Drawer{
		Dst:  img,
		Src:  textColor,
		Face: face,
	}

	textWidth := d.MeasureString(signName).Ceil()
	metrics := face.Metrics()

	x := (imgW - textWidth) / 2
	y := (imgH + (metrics.Ascent.Ceil() - metrics.Descent.Ceil())) / 2

	d.Dot = fixed.P(x, y)
	d.DrawString(signName)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return -1, err
	}

	imgIdx, err := modeler.WriteImage(
		doc,
		"tunnel_name_sign.png",
		"image/png",
		bytes.NewReader(buf.Bytes()),
	)
	if err != nil {
		return -1, err
	}

	doc.Textures = append(doc.Textures, &gltf.Texture{
		Source: gltf.Index(imgIdx),
	})

	texIdx := len(doc.Textures) - 1

	doc.Materials = append(doc.Materials, &gltf.Material{
		Name: "TunnelNameSign_" + signName,
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{
				Index: texIdx,
			},
			MetallicFactor:  gltf.Float(0),
			RoughnessFactor: gltf.Float(1),
		},
		EmissiveTexture: &gltf.TextureInfo{
			Index: texIdx,
		},
		EmissiveFactor: [3]float64{1.0, 1.0, 1.0},
		DoubleSided:    true,
	})

	return len(doc.Materials) - 1, nil
}

func drawRectangleBorder(img *image.RGBA, offset int, c color.RGBA) {
	b := img.Bounds()
	minX := b.Min.X + offset
	minY := b.Min.Y + offset
	maxX := b.Max.X - offset - 1
	maxY := b.Max.Y - offset - 1

	for x := minX; x <= maxX; x++ {
		img.SetRGBA(x, minY, c)
		img.SetRGBA(x, maxY, c)
	}
	for y := minY; y <= maxY; y++ {
		img.SetRGBA(minX, y, c)
		img.SetRGBA(maxX, y, c)
	}
}

package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"encoding/json"
	"fmt"
	"github.com/twpayne/go-geom"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestRoadPlatformBuildLine(t *testing.T) {

	var files []string
	baseDir := os.Getenv("MAP3D_TUNNEL_PLATFORM_INPUT")
	if baseDir == "" {
		t.Skip("set MAP3D_TUNNEL_PLATFORM_INPUT to a GeoJSON file or directory")
	}
	outPath := os.Getenv("MAP3D_TUNNEL_PLATFORM_OUTPUT")
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
		err := generateTunnelPlatformGlbTileset(t, outPath, gs)
		if err != nil {
			t.Fatalf("platform file %s: %v", gs, err)
		}
	}
}

func generateTunnelPlatformGlbTileset(t *testing.T, outPath, geojsonPath string) error {
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

	doc := gltf.NewDocument()

	//贴图
	img, _ := filepath.Abs("./assets/image_6.png")
	if value := os.Getenv("MAP3D_TUNNEL_PLATFORM_WALL_TEXTURE"); value != "" {
		img = value
	}
	material1, err := addWallMaterial(t, img, doc, true)
	if err != nil {
		return err
	}
	topImg, _ := filepath.Abs("./assets/RMLFJ00001.jpg")
	if value := os.Getenv("MAP3D_TUNNEL_PLATFORM_TOP_TEXTURE"); value != "" {
		topImg = value
	}
	materialTop, err := addWallMaterial(t, topImg, doc, true)
	if err != nil {
		return err
	}

	var centerlines map[string]*tunnelCenterline
	if path := os.Getenv("MAP3D_TUNNEL_CENTERLINE"); path != "" {
		centerlines, err = loadTunnelCenterlines(path, []float64{originLon, originLat, originH})
		if err != nil {
			return err
		}
	}
	tileSize := 0.5
	if value := os.Getenv("MAP3D_TUNNEL_PLATFORM_TILE_SIZE"); value != "" {
		tileSize, err = strconv.ParseFloat(value, 64)
		if err != nil || !isFinite(tileSize) || tileSize <= 0 {
			return fmt.Errorf("MAP3D_TUNNEL_PLATFORM_TILE_SIZE must be positive meters")
		}
	}
	references := make(map[string]*tunnelCenterline)
	// 显式重复采样，使超过 0～1 的米制 UV 继续平铺。
	sampler := len(doc.Samplers)
	doc.Samplers = append(doc.Samplers, &gltf.Sampler{WrapS: gltf.WrapRepeat, WrapT: gltf.WrapRepeat})
	for _, texture := range doc.Textures {
		texture.Sampler = gltf.Index(sampler)
	}
	//处理primitive
	var groups = map[string]*PlatformFaceGroup{}

	for i := range fc.Features {
		g := fc.Features[i].Geometry
		switch polygon := g.(type) {
		case *geom.Polygon:
			if len(polygon.Ends()) != 1 {
				return fmt.Errorf("feature %d: holes are unsupported", i)
			}
		case *geom.MultiPolygon:
			return fmt.Errorf("feature %d: split MultiPolygon into Polygon features", i)
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

		pos = normalizeClosedRing3D(pos)
		if len(pos) < 3 {
			continue
		}

		surfaceType, _ := fc.Features[i].Properties["surface_type"].(string)
		side, _ := fc.Features[i].Properties["side"].(string)

		material := material1
		if surfaceType == "platform_top" {
			material = materialTop
		}

		groupID := tunnelGroupID(fc.Features[i].Properties["group_id"])
		key := groupID + "|" + platformGroupKey(side, surfaceType)
		if centerlines != nil {
			reference := centerlines[groupID]
			if reference == nil {
				return fmt.Errorf("feature %d: no centerline for group_id %q", i, groupID)
			}
			references[key] = reference
		}

		if groups[key] == nil {
			groups[key] = &PlatformFaceGroup{
				Side:        side,
				SurfaceType: surfaceType,
				Material:    material,
				Polygons:    make([][][3]float32, 0),
			}
		}

		groups[key].Polygons = append(groups[key].Polygons, pos)
	}

	var primitives []*gltf.Primitive

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		primitive, errT := TunnelPlatformMultiPolygonPrimitive(
			doc,
			group.Polygons,
			group.SurfaceType,
			group.Material,
			mgltf.UvsParams{RepeatX: 1, RepeatY: 1},
			TunnelPlatformOptions{Centerline: references[key], TileSize: tileSize},
		)
		if errT != nil {
			return fmt.Errorf("primitive %s/%s: %v", group.Side, group.SurfaceType, errT)
		}

		primitives = append(primitives, primitive)
	}

	if len(primitives) == 0 {
		return fmt.Errorf("no primitives built from %s", geojsonPath)
	}

	glb, err := mgltf.BytesForPrimitive(primitives, doc)
	if err != nil {
		return fmt.Errorf("save platform glb: %w", err)
	}

	basePoint, _, err := calcTileContextFromFeatureCollections(fc)
	if err != nil {
		return fmt.Errorf("calc platform tile context: %w", err)
	}

	_, fn := filepath.Split(geojsonPath)
	ext := filepath.Ext(fn)
	baseName := fn[:len(fn)-len(ext)]
	outDir := filepath.Join(outPath, baseName)
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		// 目录不存在，创建
		if errC := os.MkdirAll(outDir, os.ModePerm); errC != nil {
			return errC
		}
	}

	if err := writeRoadTileOutputs(outDir, baseName, glb, basePoint, true); err != nil {
		return fmt.Errorf("write platform: %w", err)
	}

	fmt.Println("✅ success write road surface of folder: " + outDir)
	return nil
}

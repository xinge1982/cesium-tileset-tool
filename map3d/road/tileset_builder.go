package road

import (
	"cesium-tileset-tool/cesium"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

type TilesetBuildOptions struct {
	GLBPath        string
	OutputPath     string
	ContentURI     string
	BasePoint      []float64
	GeometricError float64
	PaddingXY      float64
	PaddingY       float64
	PaddingX       float64
	PaddingZ       float64
	MinPaddingXZ   float64
	MinPaddingY    float64
	PaddingRatioXZ float64
	PaddingRatioY  float64
	Refine         string
}

type tilesetJSON struct {
	Asset          tilesetAsset `json:"asset"`
	GeometricError float64      `json:"geometricError"`
	Root           tilesetRoot  `json:"root"`
}

type tilesetAsset struct {
	Version string `json:"version"`
}

type tilesetRoot struct {
	BoundingVolume tilesetBoundingVolume `json:"boundingVolume"`
	GeometricError float64               `json:"geometricError"`
	Refine         string                `json:"refine,omitempty"`
	Transform      []float64             `json:"transform,omitempty"`
	Content        *tilesetContent       `json:"content,omitempty"`
	Children       []tilesetRoot         `json:"children,omitempty"`
}

type tilesetBoundingVolume struct {
	Box []float64 `json:"box"`
}

type tilesetContent struct {
	URI string `json:"uri"`
}

type MultiTilesetContent struct {
	GLBPath    string
	ContentURI string
}

func WriteTilesetJSONForGLB(opt TilesetBuildOptions) error {
	buf, err := BuildTilesetJSONForGLB(opt)
	if err != nil {
		return err
	}
	return os.WriteFile(opt.OutputPath, buf, 0o644)
}

func BuildTilesetJSONForGLB(opt TilesetBuildOptions) ([]byte, error) {
	if opt.GLBPath == "" {
		return nil, fmt.Errorf("glb path is empty")
	}
	if len(opt.BasePoint) < 2 {
		return nil, fmt.Errorf("base point requires at least lon/lat")
	}
	if opt.ContentURI == "" {
		opt.ContentURI = filepath.Base(opt.GLBPath)
	}
	if opt.GeometricError <= 0 {
		opt.GeometricError = 100
	}
	if opt.PaddingXY <= 0 {
		opt.PaddingXY = 5
	}
	if opt.PaddingY <= 0 {
		opt.PaddingY = 2
	}
	if opt.PaddingX <= 0 {
		opt.PaddingX = opt.PaddingXY
	}
	if opt.PaddingZ <= 0 {
		opt.PaddingZ = opt.PaddingXY
	}
	if opt.MinPaddingXZ <= 0 {
		opt.MinPaddingXZ = 12
	}
	if opt.MinPaddingY <= 0 {
		opt.MinPaddingY = 6
	}
	if opt.PaddingRatioXZ <= 0 {
		opt.PaddingRatioXZ = 0.03
	}
	if opt.PaddingRatioY <= 0 {
		opt.PaddingRatioY = 0.2
	}
	if opt.Refine == "" {
		opt.Refine = "ADD"
	}

	minv, maxv, err := readLocalBoundsFromGLB(opt.GLBPath)
	if err != nil {
		return nil, err
	}
	box := buildTilesetBox(minv, maxv, opt)

	height := 0.0
	if len(opt.BasePoint) >= 3 {
		height = opt.BasePoint[2]
	}
	transformArr := cesium.GenerateTransformMatrixUP(opt.BasePoint[0], opt.BasePoint[1], height, 0)
	transform := make([]float64, 16)
	for i := range transformArr {
		transform[i] = transformArr[i]
	}

	ts := tilesetJSON{
		Asset:          tilesetAsset{Version: "1.1"},
		GeometricError: opt.GeometricError,
		Root: tilesetRoot{
			BoundingVolume: tilesetBoundingVolume{Box: box},
			GeometricError: 0,
			Refine:         opt.Refine,
			Transform:      transform,
			Content:        &tilesetContent{URI: opt.ContentURI},
		},
	}
	return json.MarshalIndent(ts, "", "  ")
}

func readLocalBoundsFromGLB(glbPath string) ([3]float64, [3]float64, error) {
	doc, err := gltf.Open(glbPath)
	if err != nil {
		return [3]float64{}, [3]float64{}, fmt.Errorf("open glb %s: %w", glbPath, err)
	}

	var (
		minv  [3]float64
		maxv  [3]float64
		found bool
	)

	for _, mesh := range doc.Meshes {
		if mesh == nil {
			continue
		}
		for _, prim := range mesh.Primitives {
			if prim == nil {
				continue
			}
			accIdx, ok := prim.Attributes[gltf.POSITION]
			if !ok || accIdx < 0 || accIdx >= len(doc.Accessors) {
				continue
			}
			acc := doc.Accessors[accIdx]
			if acc == nil {
				continue
			}
			pos, err := modeler.ReadPosition(doc, acc, nil)
			if err != nil || len(pos) == 0 {
				continue
			}
			for _, p := range pos {
				x, y, z := float64(p[0]), float64(p[1]), float64(p[2])
				if !found {
					minv = [3]float64{x, y, z}
					maxv = minv
					found = true
					continue
				}
				if x < minv[0] {
					minv[0] = x
				}
				if y < minv[1] {
					minv[1] = y
				}
				if z < minv[2] {
					minv[2] = z
				}
				if x > maxv[0] {
					maxv[0] = x
				}
				if y > maxv[1] {
					maxv[1] = y
				}
				if z > maxv[2] {
					maxv[2] = z
				}
			}
		}
	}
	if !found {
		return [3]float64{}, [3]float64{}, fmt.Errorf("no position data found in glb: %s", glbPath)
	}
	return minv, maxv, nil
}

func buildTilesetBox(minv, maxv [3]float64, opt TilesetBuildOptions) []float64 {
	// GLB local axes are x=east, y=up, z=north; 3D Tiles box uses x=east, y=north, z=up.
	spanX := maxv[0] - minv[0]
	spanUp := maxv[1] - minv[1]
	spanNorth := maxv[2] - minv[2]

	padX := maxFloat64(opt.PaddingX, opt.MinPaddingXZ, spanX*opt.PaddingRatioXZ)
	padNorth := maxFloat64(opt.PaddingZ, opt.MinPaddingXZ, spanNorth*opt.PaddingRatioXZ)
	padUp := maxFloat64(opt.PaddingY, opt.MinPaddingY, spanUp*opt.PaddingRatioY)

	cx := (minv[0] + maxv[0]) / 2
	cnorth := (minv[2] + maxv[2]) / 2
	cup := (minv[1] + maxv[1]) / 2
	hx := spanX/2 + padX
	hnorth := spanNorth/2 + padNorth
	hup := spanUp/2 + padUp
	return []float64{
		cx, cnorth, cup,
		hx, 0, 0,
		0, hnorth, 0,
		0, 0, hup,
	}
}

func maxFloat64(values ...float64) float64 {
	maxv := values[0]
	for i := 1; i < len(values); i++ {
		if values[i] > maxv {
			maxv = values[i]
		}
	}
	return maxv
}

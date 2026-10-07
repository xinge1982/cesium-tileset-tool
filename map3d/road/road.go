// 道路生成GLTF程序
package road

import (
	"cesium-tileset-tool/map3d/mgltf"
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/sirupsen/logrus"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
)

type Results map[string]interface{}

type Road struct {
	Fields     []Results
	TextureImg string
}

func (rs Results) ToGeom() (geom.T, error) {
	if g, ok := rs["geom"]; ok {
		buf, ok := g.([]byte)
		if !ok {
			return nil, fmt.Errorf("geom field type is %T, expect []byte", g)
		}
		return wkb.Unmarshal(buf)
	}
	return nil, fmt.Errorf("geom field not found")
}

// GetHeight 高度字段
func (rs Results) GetHeight() (float64, error) {
	if g, ok := rs["height"]; ok {
		switch v := g.(type) {
		case float64:
			return v, nil
		case float32:
			return float64(v), nil
		case int:
			return float64(v), nil
		case int32:
			return float64(v), nil
		case int64:
			return float64(v), nil
		default:
			return 0, fmt.Errorf("height field type is %T, expect number", g)
		}
	}
	return 0, fmt.Errorf("height field not found")
}

func pr(rs []Results, basePoint []float64, textureImg string) (*gltf.Document, []*gltf.Primitive) {
	doc := gltf.NewDocument()
	materIndex, err := mgltf.TextureMaterial(doc, textureImg)
	if err != nil {
		logrus.Errorf("texture material error:%v\n", err)
		materIndex, err = mgltf.TextureMaterialForColor(doc, "#666666")
		if err != nil {
			logrus.Errorf("fallback material error:%v\n", err)
			return doc, nil
		}
	}
	var prs []*gltf.Primitive

	for i := range rs {
		geometry, err := rs[i].ToGeom()
		if err != nil {
			logrus.Errorf("geom to geom error:%v\n", err)
			continue
		}

		polygons, err := geometryToLocalRings(geometry, basePoint)
		if err != nil {
			logrus.Errorf("geometry to rings error:%v\n", err)
			continue
		}
		for _, rings := range polygons {
			face, err := mgltf.PrimitiveWithHoles(doc, rings, materIndex, mgltf.UvsParams{FlipV: true, RepeatX: 256, RepeatY: 256})
			if err != nil {
				logrus.Errorf("primitive error:%v\n", err)
				continue
			}
			prs = append(prs, face)
		}
	}

	return doc, prs

}

func toXYZFlat(flat []float64, stride int) ([]float64, error) {
	if stride < 2 {
		return nil, fmt.Errorf("unsupported stride: %d", stride)
	}
	if len(flat)%stride != 0 {
		return nil, fmt.Errorf("invalid flat coords length %d for stride %d", len(flat), stride)
	}

	out := make([]float64, 0, len(flat)/stride*3)
	for i := 0; i < len(flat); i += stride {
		x, y := flat[i], flat[i+1]
		z := 0.0
		if stride >= 3 {
			z = flat[i+2]
		}
		out = append(out, x, y, z)
	}
	return out, nil
}

func polygonToLocalRings(p *geom.Polygon, basePoint []float64) ([][][3]float32, error) {
	flat := p.FlatCoords()
	ends := p.Ends()
	stride := p.Stride()
	if len(ends) == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}

	start := 0
	rings := make([][][3]float32, 0, len(ends))
	for _, end := range ends {
		if end <= start || end > len(flat) {
			return nil, fmt.Errorf("invalid ring end: %d", end)
		}
		ringFlat, err := toXYZFlat(flat[start:end], stride)
		if err != nil {
			return nil, err
		}
		rings = append(rings, mgltf.ConvertToRelativeCoordinates(ringFlat, basePoint))
		start = end
	}
	return rings, nil
}

func geometryToLocalRings(geometry geom.T, basePoint []float64) ([][][][3]float32, error) {
	if len(basePoint) < 2 {
		return nil, fmt.Errorf("basePoint length must be >=2, got %d", len(basePoint))
	}

	switch g := geometry.(type) {
	case *geom.Polygon:
		rings, err := polygonToLocalRings(g, basePoint)
		if err != nil {
			return nil, err
		}
		return [][][][3]float32{rings}, nil
	case *geom.MultiPolygon:
		if g.NumPolygons() == 0 {
			return nil, fmt.Errorf("empty multipolygon")
		}
		out := make([][][][3]float32, 0, g.NumPolygons())
		for i := 0; i < g.NumPolygons(); i++ {
			rings, err := polygonToLocalRings(g.Polygon(i), basePoint)
			if err != nil {
				return nil, err
			}
			out = append(out, rings)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", geometry)
	}
}

func prSolid(rs []Results, basePoint []float64, textureImg string, thickness float32) (*gltf.Document, []*gltf.Primitive) {
	doc := gltf.NewDocument()
	materIndex, err := mgltf.TextureMaterial(doc, textureImg)
	if err != nil {
		logrus.Errorf("texture material error:%v\n", err)
		materIndex, err = mgltf.TextureMaterialForColor(doc, "#666666")
		if err != nil {
			logrus.Errorf("fallback material error:%v\n", err)
			return doc, nil
		}
	}

	var prs []*gltf.Primitive
	for i := range rs {
		geometry, err := rs[i].ToGeom()
		if err != nil {
			logrus.Errorf("geom to geom error:%v\n", err)
			continue
		}
		polygons, err := geometryToLocalRings(geometry, basePoint)
		if err != nil {
			logrus.Errorf("geometry to rings error:%v\n", err)
			continue
		}
		for _, rings := range polygons {
			solids, err := mgltf.ExtrudePolygonSolidPrimitivesWithHoles(doc, rings, thickness, materIndex, mgltf.UvsParams{FlipV: true, RepeatX: 256, RepeatY: 256})
			if err != nil {
				logrus.Errorf("extrude primitive error:%v\n", err)
				continue
			}
			prs = append(prs, solids...)
		}
	}
	return doc, prs
}

// ProductRoadSolidFile 保存成带厚度的道路实体
func ProductRoadSolidFile(rs []Results, basePoint []float64, textureImg, tablename string, thickness float32) error {
	doc, prs := prSolid(rs, basePoint, textureImg, thickness)
	return mgltf.SaveGltfForPrimitive(prs, doc, fmt.Sprintf("gltfassets/%s.gltf", tablename))
}

// ProductRoadSolidBytes 读取带厚度的道路实体GLTF流
func ProductRoadSolidBytes(rs []Results, basePoint []float64, textureImg string, thickness float32) ([]byte, error) {
	doc, prs := prSolid(rs, basePoint, textureImg, thickness)
	return mgltf.BytesForPrimitive(prs, doc)
}

// ProductRoadFile 保存到目录
func ProductRoadFile(rs []Results, basePoint []float64, textureImg, tablename string) error {
	doc, prs := pr(rs, basePoint, textureImg)
	return mgltf.SaveGltfForPrimitive(prs, doc, fmt.Sprintf("gltfassets/%s.gltf", tablename))

}

// ProductRoadBytes 读取道路GLTF流
func ProductRoadBytes(rs []Results, basePoint []float64, textureImg string) ([]byte, error) {
	doc, prs := pr(rs, basePoint, textureImg)
	return mgltf.BytesForPrimitive(prs, doc)
}

package road

import (
	"cesium-tileset-tool/map3d/mgltf"
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/sirupsen/logrus"
)

func rail(line [][3]float32, height float32) [][3]float32 {
	for i := len(line) - 1; i >= 0; i-- {
		line = append(line, [3]float32{line[i][0], line[i][1], line[i][2] + height})
	}
	return line
}

func getRailPrimitives(rs []Results, basePoint []float64, textureImg string) (*gltf.Document, []*gltf.Primitive, error) {
	doc := gltf.NewDocument()
	var faces []*gltf.Primitive
	materIndex, err := mgltf.TextureMaterialZ(doc, textureImg)
	if err != nil {
		return doc, nil, err
	}

	for i := range rs {
		geom, err := rs[i].ToGeom()
		if err != nil {
			logrus.Errorf("get to geom error:%v\n", err)
			continue
		}
		height, err := rs[i].GetHeight()
		if err != nil {
			logrus.Errorf("get height error:%v\n", err)
		}
		pos := mgltf.ConvertToRelativeCoordinates(geom.FlatCoords(), basePoint)
		pos = rail(pos, float32(height))
		face, err := mgltf.PrimitiveEXX(doc, pos, materIndex)
		if err != nil {
			logrus.Errorf("primitive error:%v\n", err)
			continue
		}

		faces = append(faces, face)
	}

	return doc, faces, nil
}

// ProductRailToFile 把护栏模型保存到目录
func ProductRailToFile(rs []Results, basePoint []float64, tablename string, textureImg string) error {
	doc, prs, err := getRailPrimitives(rs, basePoint, textureImg)
	if err != nil {
		return err
	}
	return mgltf.SaveGltfForPrimitive(prs, doc, fmt.Sprintf("gltfassets/%s.gltf", tablename))
}

// ProductRailToEncode 读取护栏模型GLTF流
func ProductRailToEncode(rs []Results, basePoint []float64, textureImg string) ([]byte, error) {
	doc, prs, err := getRailPrimitives(rs, basePoint, textureImg)
	if err != nil {
		return nil, err
	}
	return mgltf.BytesForPrimitive(prs, doc)

}

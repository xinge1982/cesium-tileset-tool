package mgltf

import (
	"bytes"
	"cesium-tileset-tool/map3d/texture"
	"fmt"
	"math"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// 根据纹理图片创建 glTF 材质。
//
// 功能:
// 1. 读取图片纹理。
// 2. 将图片写入 glTF Image。
// 3. 创建 Texture。
// 4. 创建 PBR Material。
// 5. 设置 BaseColorTexture。
//
// 材质特点:
// - Metallic = 0。
// - DoubleSided=true，支持双面渲染。
//
// 参数:
// doc:
//
//	glTF 文档对象。
//
// path:
//
//	纹理文件路径。
//
// 返回:
// 材质索引。
func TextureMaterial(doc *gltf.Document, path string) (int, error) {
	tre, err := texture.NewTexture(path)
	if err != nil {
		return 0, err
	}
	imgIdx, err := modeler.WriteImage(doc, tre.Name(), tre.Format(), bytes.NewReader(tre.GetBuf()))
	if err != nil {
		return 0, err
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
			MetallicFactor: gltf.Float(0)},
		DoubleSided: true,
	})
	return len(doc.Materials) - 1, nil
}

// 创建支持透明混合的纹理材质。
//
// 与 TextureMaterial 区别:
// - 增加 AlphaBlend。
// - 支持带透明通道纹理。
//
// 适用:
// - 标牌。
// - 贴图文字。
// - 半透明模型。
//
// 参数:
// doc:
//
//	glTF文档。
//
// path:
//
//	图片路径。
//
// 返回:
// 材质索引。
func TextureMaterialZ(doc *gltf.Document, path string) (int, error) {
	tre, err := texture.NewTexture(path)
	if err != nil {
		return 0, err
	}
	fmt.Println(tre.Name())

	imgIdx, err := modeler.WriteImage(doc, tre.Name(), tre.Format(), bytes.NewReader(tre.GetBuf()))
	if err != nil {
		return 0, err
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
			MetallicFactor: gltf.Float(0)},
		AlphaMode:   gltf.AlphaBlend,
		DoubleSided: true,
	})
	return len(doc.Materials) - 1, nil
}

// 根据十六进制颜色创建纯颜色 glTF 材质。
//
// 输入格式:
// #RRGGBB
// 或
// #RRGGBBAA
//
// 使用:
// PBR BaseColorFactor。
//
// 特点:
// - 无纹理。
// - Metallic=0。
//
// 返回:
// 材质索引。
func TextureMaterialForColor(doc *gltf.Document, color string) (int, error) {
	rgba, err := ParseHexColor(color)
	if err != nil {
		return -1, err
	}

	doc.Materials = append(doc.Materials, &gltf.Material{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &rgba,
			MetallicFactor:  gltf.Float(0)}})
	return len(doc.Materials) - 1, nil
}

// 创建支持透明和双面的纯颜色材质。
//
// 与 TextureMaterialForColor 区别:
// - AlphaMode = Blend。
// - DoubleSided=true。
//
// 适用:
// - 透明标牌。
// - 薄片模型。
// - 双面显示面。
func TextureMaterialForColorDoubleSide(doc *gltf.Document, color string) (int, error) {
	rgba, err := ParseHexColor(color)
	if err != nil {
		return -1, err
	}

	doc.Materials = append(doc.Materials, &gltf.Material{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &rgba,
			MetallicFactor:  gltf.Float(0)},
		AlphaMode:   gltf.AlphaBlend,
		DoubleSided: true})
	return len(doc.Materials) - 1, nil
}

// 将十六进制颜色字符串解析为 RGBA 浮点颜色。
//
// 支持格式:
//
// #RRGGBB
//
// 默认 Alpha=1。
//
// #RRGGBBAA
//
// 返回:
//
//	[4]float64{
//	  R,
//	  G,
//	  B,
//	  A,
//	}
//
// 数值范围:
// 0.0 ~ 1.0
func ParseHexColor(color string) ([4]float64, error) {
	var rgba [4]float64
	if !strings.HasPrefix(color, "#") || (len(color) != 7 && len(color) != 9) {
		return rgba, fmt.Errorf("invalid color format: %s", color)
	}
	var r, g, b, a uint8
	if len(color) == 7 {
		a = 255 // 默认透明度为 1.0
		_, err := fmt.Sscanf(color, "#%02x%02x%02x", &r, &g, &b)
		if err != nil {
			return rgba, fmt.Errorf("failed to parse color: %v", err)
		}
	} else {
		_, err := fmt.Sscanf(color, "#%02x%02x%02x%02x", &r, &g, &b, &a)
		if err != nil {
			return rgba, fmt.Errorf("failed to parse color: %v", err)
		}
	}
	rgba[0] = float64(r) / 255.0
	rgba[1] = float64(g) / 255.0
	rgba[2] = float64(b) / 255.0
	rgba[3] = float64(a) / 255.0
	return rgba, nil
}

// 定义 Primitive UV 生成参数。
//
// 字段:
//
// RepeatX:
//
//	U方向纹理重复次数。
//
// RepeatY:
//
//	V方向纹理重复次数。
//
// Axis:
//
//	UV生成参考方向。
//
// Rotation:
//
//	UV旋转角度。
//
// FlipV:
//
//	是否翻转V方向。
//
// FlipU:
//
//	是否翻转U方向。
type UvsParams struct {
	RepeatX, RepeatY float32
	Axis             string
	Rotation         int
	FlipV            bool
	FlipU            bool
}

// 根据三维顶点沿 Z 方向距离生成 UV 坐标。
//
// 主要用于:
// - 长条形面。
// - 隧道壁。
// - 拉伸结构。
//
// 算法:
// 1. 计算顶点累计距离。
// 2. 使用距离作为 U 坐标。
// 3. 顶部和底部生成对应 V。
//
// 返回:
// UV坐标数组。
func GenerateUVsZ(vertices [][3]float32) [][2]float32 {
	pl := len(vertices)
	var zuv, fuv [][2]float32
	var w float32 = 2.0
	zuv = append(zuv, [2]float32{0, 1})
	fuv = append(fuv, [2]float32{0, 0})
	var dist float32 = 0
	for i := 0; i < pl/2-1; i++ {
		dist = Distance(vertices[i], vertices[i+1])/w + dist
		zuv = append(zuv, [2]float32{dist, 1})
		fuv = append(fuv, [2]float32{dist, 0})
	}
	for i := len(fuv) - 1; i >= 0; i-- {
		zuv = append(zuv, fuv[i])
	}

	return zuv
}

// 计算两个三维点之间的二维距离。
//
// 注意:
// 当前实现只使用:
//
// # X,Y
//
// 不考虑 Z。
//
// 用途:
// UV累计长度计算。
//
// 返回:
// 两点距离。
func Distance(p1, p2 [3]float32) float32 {
	d0 := p1[0] - p2[0]
	d1 := p1[1] - p2[1]
	return float32(math.Sqrt(float64(d0*d0) + float64(d1*d1)))
}

// 将绝对坐标转换为 glTF 局部相对坐标。
//
// 用途:
// 解决大范围 GIS 坐标直接进入 glTF
// 导致 float 精度不足的问题。
//
// 坐标转换:
//
// X:
//
//	原X - originX
//
// Y:
//
//	保持高度
//
// Z:
//
//	原Y - originY
//
// 返回:
// glTF使用的局部XYZ坐标。
func ConvertToRelativeCoordinates(Coords []float64, origin []float64) [][3]float32 {
	// 创建一个新的切片来存储相对坐标
	var relativeCoords [][3]float32
	for i := 0; i < len(Coords); i += 3 {
		relativeCoords = append(relativeCoords,
			[3]float32{
				float32(Coords[i] - origin[0]), // X 相对坐标
				float32(Coords[i+2]),
				float32(Coords[i+1] - origin[1]), // Y 相对坐标

			},
		)
		//fmt.Printf("Coords[i]:%f - origin[0]:%f = %f\n", Coords[i], origin[0], Coords[i]-origin[0])
		//fmt.Printf("Coords[i+1]:%f - origin[1]:%f = %f\n", Coords[i+1], origin[1], Coords[i+1]-origin[1])

	}
	return relativeCoords
}

// 根据第一个坐标点作为原点生成局部坐标。
//
// 与 ConvertToRelativeCoordinates 区别:
// - 自动选择 origin。
// - 保留原始 origin 返回。
//
// 适用:
// - 建筑模型。
// - 单体模型生成。
//
// 返回:
// 1. 局部坐标。
// 2. 原始坐标原点。
func ConvertToRelativeCoordinatesBS(Coords []float64) ([][3]float32, []float64) {
	// 创建一个新的切片来存储相对坐标
	var relativeCoords [][3]float32
	origin := Coords[0:1]
	for i := 0; i < len(Coords); i += 3 {
		relativeCoords = append(relativeCoords,
			[3]float32{
				float32(Coords[i] - origin[0]), // X 相对坐标
				float32(Coords[i+1] - origin[1]),
				float32(Coords[i+2]),
			},
		)
		//fmt.Printf("Coords[i]:%f - origin[0]:%f = %f\n", Coords[i], origin[0], Coords[i]-origin[0])
		//fmt.Printf("Coords[i+1]:%f - origin[1]:%f = %f\n", Coords[i+1], origin[1], Coords[i+1]-origin[1])

	}
	return relativeCoords, origin
}

// 将坐标转换为 glTF 局部坐标。
//
// 与 ConvertToRelativeCoordinatesBS 类似，
// 但保持项目内部:
//
// X:
//
//	east
//
// Y:
//
//	height(up)
//
// Z:
//
//	north
//
// 的坐标约定。
//
// 返回:
// 局部坐标和原点。
func ConvertToRelativeCoordinatesOrigin(Coords []float64) ([][3]float32, []float64) {
	// 创建一个新的切片来存储相对坐标
	var relativeCoords [][3]float32
	origin := Coords[0:1]
	for i := 0; i < len(Coords); i += 3 {
		relativeCoords = append(relativeCoords,
			[3]float32{
				float32(Coords[i] - origin[0]), // X 相对坐标
				float32(Coords[i+2]),
				float32(Coords[i+1] - origin[1]),
			},
		)
		//fmt.Printf("Coords[i]:%f - origin[0]:%f = %f\n", Coords[i], origin[0], Coords[i]-origin[0])
		//fmt.Printf("Coords[i+1]:%f - origin[1]:%f = %f\n", Coords[i+1], origin[1], Coords[i+1]-origin[1])

	}
	return relativeCoords, origin
}

// 根据道路顶点范围生成道路纹理 UV 坐标。
//
// 功能:
// 1. 计算道路二维范围:
//   - X方向宽度。
//   - Y方向长度。
//
// 2. 将顶点坐标归一化到纹理空间。
//
// 3. 根据纹理尺寸参数进行 UV 缩放。
//
// 适用:
// - 道路面贴图。
// - 大范围路面纹理映射。
//
// 参数:
//
// vertices:
//
//	道路顶点列表。
//
// textureWidth:
//
//	纹理横向重复比例。
//
// textureHeight:
//
//	纹理纵向重复比例。
//
// 返回:
// UV坐标数组。
func NewGenerateRoadUVs(vertices [][3]float32, textureWidth, textureHeight float32) [][2]float32 {
	// 找到道路的最大宽度和长度
	var minX, minY, maxX, maxY float32
	for _, v := range vertices {
		if v[0] < minX {
			minX = v[0]
		}
		if v[1] < minY {
			minY = v[1]
		}
		if v[0] > maxX {
			maxX = v[0]
		}
		if v[1] > maxY {
			maxY = v[1]
		}
	}

	// 计算道路的宽度和长度
	roadWidth := maxX - minX
	roadLength := maxY - minY

	// 生成UV坐标
	uvs := make([][2]float32, len(vertices))
	for i, v := range vertices {
		// 计算每个点的相对坐标
		relativeX := v[0] - minX
		relativeY := v[1] - minY

		// 根据相对坐标计算UV坐标
		uvX := relativeX / roadWidth
		uvY := relativeY / roadLength

		// 根据纹理尺寸缩放UV坐标
		uvs[i] = [2]float32{
			uvX * textureWidth,  // 乘以纹理宽度，得到对应的X坐标
			uvY * textureHeight, // 乘以纹理高度，得到对应的Y坐标
		}
	}

	return uvs
}

// 根据 Polygon 顶点生成一个完整 glTF Primitive。
//
// 功能:
// 1. 使用 EarcutXZ 对 Polygon 三角化。
// 2. 根据 XZ 平面生成 UV。
// 3. 根据三角面计算顶点法线。
// 4. 写入 glTF Buffer。
// 5. 创建 glTF Primitive。
//
// 坐标约定:
//
// X:
//
//	水平方向
//
// Y:
//
//	高度(up)
//
// Z:
//
//	水平方向
//
// 适用:
// - 道路面。
// - 地面。
// - 普通三维面。
//
// 参数:
//
// doc:
//
//	glTF文档。
//
// pos:
//
//	三维顶点。
//
// material:
//
//	材质索引。
//
// params:
//
//	UV参数。
//
// 返回:
// glTF Primitive。
func Primitive(doc *gltf.Document, pos [][3]float32, material int, params UvsParams) (*gltf.Primitive, error) {
	// This repo uses XZ as the ground plane and Y as height(up).
	indices, err := EarcutXZ(pos)
	if err != nil {
		return nil, err
	}

	repeatX := params.RepeatX
	repeatY := params.RepeatY
	if repeatX == 0 {
		repeatX = 1
	}
	if repeatY == 0 {
		repeatY = 1
	}

	uvs := GenerateUVsXZ(pos, repeatX, repeatY)

	if params.FlipV {
		for i := range uvs {
			uvs[i][1] = repeatY - uvs[i][1]
		}
	}

	normals := GenerateSmoothNormals(pos, indices)

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 使用已有三角索引和法线生成道路区域 Primitive。
//
// 与 Primitive 区别:
//
// Primitive:
//
//	内部自动三角化和计算法线。
//
// 本函数:
//
//	外部提供:
//	- indices。
//	- normals。
//
// 功能:
// 1. 生成道路 UV。
// 2. 根据 Rotation 调整 UV方向。
// 3. 支持 FlipV。
// 4. 写入 glTF Primitive。
//
// 适用:
// - 已完成三角化的道路区域。
//
// 参数:
//
// pos:
//
//	顶点列表。
//
// indices:
//
//	三角索引。
//
// normals:
//
//	顶点法线。
//
// material:
//
//	材质。
//
// params:
//
//	UV配置。
//
// 返回:
// glTF Primitive。
func RoadAreaPrimitiveWithNormals(
	doc *gltf.Document,
	pos [][3]float32,
	indices []uint32,
	normals [][3]float32,
	material int,
	params UvsParams,
) (*gltf.Primitive, error) {

	repeatX := params.RepeatX
	repeatY := params.RepeatY
	if repeatX == 0 {
		repeatX = 1
	}
	if repeatY == 0 {
		repeatY = 1
	}

	uvs, _ :=
		GenerateUVsByDistance(
			pos,
			[3]float32{1, 0, 0}, // U沿X
			[3]float32{0, 1, 0}, // V沿Y
			2,
			2,
			false,
			false,
		)

	// rotate UV first
	for i := range uvs {
		u := uvs[i][0]
		v := uvs[i][1]

		switch ((params.Rotation % 360) + 360) % 360 {
		case 90:
			uvs[i][0] = v
			uvs[i][1] = repeatX - u

		case 180:
			uvs[i][0] = repeatX - u
			uvs[i][1] = repeatY - v

		case 270:
			uvs[i][0] = repeatY - v
			uvs[i][1] = u

		default:
			uvs[i][0] = u
			uvs[i][1] = v
		}
	}

	if params.FlipV {
		for i := range uvs {
			uvs[i][1] = repeatY - uvs[i][1]
		}
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 使用指定顶点、索引和法线创建 glTF Primitive。
//
// 功能:
// 1. 自动根据三维面方向选择最佳 UV 投影平面。
// 2. 支持 UV旋转。
// 3. 支持 UV翻转。
// 4. 写入 POSITION/NORMAL/UV Attribute。
//
// 与 RoadAreaPrimitiveWithNormals 区别:
//
// RoadArea:
//
//	固定使用 XZ 平面 UV。
//
// 本函数:
//
//	使用 GenerateUVsBestPlane，支持任意方向面。
//
// 适用:
// - 墙面。
// - 立面。
// - 任意三维面。
//
// 返回:
// glTF Primitive。
func PrimitiveWithNormals(
	doc *gltf.Document,
	pos [][3]float32,
	indices []uint32,
	normals [][3]float32,
	material int,
	params UvsParams,
) (*gltf.Primitive, error) {

	repeatX := params.RepeatX
	repeatY := params.RepeatY
	if repeatX == 0 {
		repeatX = 1
	}
	if repeatY == 0 {
		repeatY = 1
	}

	uvs := GenerateUVsBestPlane(pos, repeatX, repeatY)

	// rotate UV first
	for i := range uvs {
		u := uvs[i][0]
		v := uvs[i][1]

		switch ((params.Rotation % 360) + 360) % 360 {
		case 90:
			uvs[i][0] = v
			uvs[i][1] = repeatX - u

		case 180:
			uvs[i][0] = repeatX - u
			uvs[i][1] = repeatY - v

		case 270:
			uvs[i][0] = repeatY - v
			uvs[i][1] = u

		default:
			uvs[i][0] = u
			uvs[i][1] = v
		}
	}

	if params.FlipV {
		for i := range uvs {
			uvs[i][1] = repeatY - uvs[i][1]
		}
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 创建带纹理条带控制的三维 Primitive。
//
// 功能:
// 1. 根据最佳投影平面生成 UV。
// 2. 根据 stripSize 控制纹理重复距离。
// 3. 支持旋转 UV。
// 4. 支持翻转 UV。
//
// 适用:
// - 隧道壁。
// - 长条结构。
// - 道路附属设施。
//
// 参数:
//
// stripSize:
//
//	控制纹理重复间隔。
//
// 其他参数:
//
//	与 PrimitiveWithNormals 相同。
//
// 返回:
// glTF Primitive。
func PrimitiveWithNormalsStrip(
	doc *gltf.Document,
	pos [][3]float32,
	indices []uint32,
	normals [][3]float32,
	material int,
	stripSize float32,
	params UvsParams,
) (*gltf.Primitive, error) {

	repeatX := params.RepeatX
	repeatY := params.RepeatY
	if repeatX == 0 {
		repeatX = 1
	}
	if repeatY == 0 {
		repeatY = 1
	}

	uvs := GenerateUVsBestPlane(pos, repeatX, repeatY)

	// rotate UV first
	for i := range uvs {
		u := uvs[i][0]
		v := uvs[i][1]

		switch ((params.Rotation % 360) + 360) % 360 {
		case 90:
			uvs[i][0] = v
			uvs[i][1] = repeatX - u

		case 180:
			uvs[i][0] = repeatX - u
			uvs[i][1] = repeatY - v

		case 270:
			uvs[i][0] = repeatY - v
			uvs[i][1] = u

		default:
			uvs[i][0] = u
			uvs[i][1] = v
		}
	}

	if params.FlipV {
		for i := range uvs {
			uvs[i][1] = repeatY - uvs[i][1]
		}
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 创建四边形 Tile Primitive，并生成基于距离的 UV。
//
// 要求:
//
// pos 必须包含4个顶点。
//
// 功能:
// 1. 根据四个顶点计算 UV。
// 2. 支持 U/V 翻转。
// 3. 创建带法线的 glTF Primitive。
//
// 适用:
//
// - 标牌。
// - 图片面板。
// - 隧道牌面。
// - 文字贴图面。
//
// 参数:
//
// flipU:
//
//	是否水平翻转纹理。
//
// flipV:
//
//	是否垂直翻转纹理。
//
// 返回:
// glTF Primitive。
func PrimitiveWithNormalsTile(
	doc *gltf.Document,
	pos [][3]float32,
	indices []uint32,
	normals [][3]float32,
	material int,
	flipU bool,
	flipV bool,
) (*gltf.Primitive, error) {

	if len(pos) != 4 {
		return nil, fmt.Errorf("sign name primitive requires 4 vertices, got %d", len(pos))
	}

	uvs, err := GeneratePortalUVByDistance(pos, 1, 1, flipU, flipV)
	if err != nil {
		return nil, err
	}

	if flipV {
		for i := range uvs {
			uvs[i][1] = 1 - uvs[i][1]
		}
	}

	if flipU {
		for i := range uvs {
			uvs[i][0] = 1 - uvs[i][0]
		}
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 根据四边形实际空间距离生成 UV。
//
// 与固定:
//
// [0,0]-[1,1]
//
// UV不同，本函数根据真实尺寸计算:
//
// U:
//
//	沿面长度方向累计距离。
//
// V:
//
//	根据高度方向生成纹理比例。
//
// 适用:
//
// - 隧道洞口。
// - 门牌。
// - 标识牌。
//
// 要求:
//
// pos必须为4个顶点。
//
// 参数:
//
// tileSizeU:
//
//	U方向纹理尺寸。
//
// tileSizeV:
//
//	V方向纹理尺寸。
//
// flipU:
//
//	U方向翻转。
//
// flipV:
//
//	V方向翻转。
//
// 返回:
// UV数组。
func GeneratePortalUVByDistance(
	pos [][3]float32,
	tileSizeU float32,
	tileSizeV float32,
	flipU bool,
	flipV bool,
) ([][2]float32, error) {

	n := len(pos)
	if n < 2 {
		return nil, fmt.Errorf("invalid pos size")
	}

	if len(pos) != 4 {
		return nil, fmt.Errorf("sign name primitive requires 4 vertices, got %d", len(pos))
	}

	// default tile size
	if tileSizeU <= 0 {
		tileSizeU = 2.0
	}
	if tileSizeV <= 0 {
		tileSizeV = 2.0
	}

	// --------------------------------------
	// STEP 1: compute cumulative U distance
	// --------------------------------------
	uDist := make([]float32, n)
	uDist[0] = 0

	totalU := float32(0)

	for i := 1; i < n; i++ {
		dx := pos[i][0] - pos[i-1][0]
		dy := pos[i][1] - pos[i-1][1]
		dz := pos[i][2] - pos[i-1][2]

		d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
		totalU += d
		uDist[i] = totalU
	}

	if totalU < 1e-6 {
		return nil, fmt.Errorf("invalid geometry length")
	}

	// --------------------------------------
	// STEP 2: compute V once (assume vertical strip)
	// --------------------------------------
	vMax := float32(totalU / tileSizeV)

	// --------------------------------------
	// STEP 3: build UV
	// --------------------------------------
	uvs := make([][2]float32, n)

	for i := 0; i < n; i++ {

		u := uDist[i] / tileSizeU

		// V: normalized along strip height (0~1 mapped to vMax)
		v := float32(0.0)
		if i < n/2 {
			v = 0
		} else {
			v = vMax
		}

		uvs[i] = [2]float32{u, float32(v)}
	}

	// --------------------------------------
	// STEP 4: flipU
	// --------------------------------------
	if flipU {
		for i := range uvs {
			uvs[i][0] = uvs[n-1][0] - uvs[i][0]
		}
	}

	// --------------------------------------
	// STEP 5: flipV
	// --------------------------------------
	if flipV {
		for i := range uvs {
			uvs[i][1] = vMax - uvs[i][1]
		}
	}

	return uvs, nil
}

// 创建带文字贴图的标牌 Primitive。
//
// 特点:
//
// 使用固定四边形 UV:
//
// 左下:
//
//	(0,1)
//
// 左上:
//
//	(0,0)
//
// 右上:
//
//	(1,0)
//
// 右下:
//
//	(1,1)
//
// 支持:
// - 水平翻转。
// - 垂直翻转。
//
// 适用:
// - 道路名称牌。
// - 隧道名称牌。
// - 文字贴图。
//
// 返回:
// glTF Primitive。
func SignNamePrimitiveWithNormals(
	doc *gltf.Document,
	pos [][3]float32,
	indices []uint32,
	normals [][3]float32,
	material int,
	flipU bool,
	flipV bool,
) (*gltf.Primitive, error) {

	if len(pos) != 4 {
		return nil, fmt.Errorf("sign name primitive requires 4 vertices, got %d", len(pos))
	}

	uvs := [][2]float32{
		{0, 1}, // left_bottom
		{0, 0}, // left_top
		{1, 0}, // right_top
		{1, 1}, // right_bottom
	}

	if flipV {
		for i := range uvs {
			uvs[i][1] = 1 - uvs[i][1]
		}
	}

	if flipU {
		for i := range uvs {
			uvs[i][0] = 1 - uvs[i][0]
		}
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 根据三维顶点空间距离生成连续 UV 坐标。
//
// 与基于单个四边形固定 UV 的方式不同，
// 本函数根据整个 Mesh 的空间范围计算 UV，
// 因此可以支持:
// - 多个三角形组成的连续面。
// - 任意数量顶点的 Polygon。
// - 已经过 Earcut 三角化的 Mesh。
// - 多个 Primitive 共享连续纹理坐标。
//
// 算法流程:
//  1. 使用 axisU 和 axisV 定义纹理映射方向。
//  2. 将每个三维顶点投影到 UV 平面:
//     U = dot(position, axisU)
//     V = dot(position, axisV)
//
// 3. 计算所有顶点投影后的:
//   - 最小 U。
//   - 最大 U。
//   - 最小 V。
//   - 最大 V。
//
// 4. 根据 tileSizeU / tileSizeV 将空间距离转换为纹理重复比例。
//
// 5. 根据 flipU / flipV 参数调整纹理方向。
//
// 参数:
//
// pos:
//
//	三维顶点列表。
//	坐标格式:
//	[X,Y,Z]
//
// axisU:
//
//	纹理 U 方向单位向量。
//	用于定义纹理横向展开方向。
//
// axisV:
//
//	纹理 V 方向单位向量。
//	用于定义纹理纵向展开方向。
//
// tileSizeU:
//
//	U方向纹理重复实际尺寸。
//
// tileSizeV:
//
//	V方向纹理重复实际尺寸。
//
// flipU:
//
//	是否水平翻转纹理。
//
// flipV:
//
//	是否垂直翻转纹理。
//
// 返回:
//
// [][2]float32:
//
//	每个顶点对应的 UV 坐标。
//
// error:
//
//	输入顶点为空或参数非法时返回错误。
//
// 适用场景:
//
// - 隧道墙面连续贴图。
// - Portal / 标牌面板纹理。
// - 建筑墙面。
// - 大面积 Mesh 纹理映射。
func GenerateUVsByDistance(
	pos [][3]float32,
	axisU [3]float32,
	axisV [3]float32,
	tileSizeU float32,
	tileSizeV float32,
	flipU bool,
	flipV bool,
) ([][2]float32, error) {

	if len(pos) == 0 {
		return nil, fmt.Errorf("empty vertices")
	}

	if tileSizeU <= 0 {
		tileSizeU = 1
	}

	if tileSizeV <= 0 {
		tileSizeV = 1
	}

	uv := make([][2]float32, len(pos))

	minU := float32(math.MaxFloat32)
	maxU := float32(-math.MaxFloat32)

	minV := float32(math.MaxFloat32)
	maxV := float32(-math.MaxFloat32)

	projected := make([][2]float32, len(pos))

	for i, p := range pos {

		u :=
			p[0]*axisU[0] +
				p[1]*axisU[1] +
				p[2]*axisU[2]

		v :=
			p[0]*axisV[0] +
				p[1]*axisV[1] +
				p[2]*axisV[2]

		projected[i] = [2]float32{u, v}

		if u < minU {
			minU = u
		}

		if u > maxU {
			maxU = u
		}

		if v < minV {
			minV = v
		}

		if v > maxV {
			maxV = v
		}
	}

	du := maxU - minU
	dv := maxV - minV

	if du < 1e-6 {
		du = 1
	}

	if dv < 1e-6 {
		dv = 1
	}

	for i, p := range projected {

		u :=
			(p[0] - minU) /
				du *
				du / tileSizeU

		v :=
			(p[1] - minV) /
				dv *
				dv / tileSizeV

		uv[i] = [2]float32{
			u,
			v,
		}
	}

	if flipU {

		max := uv[0][0]

		for _, v := range uv {
			if v[0] > max {
				max = v[0]
			}
		}

		for i := range uv {
			uv[i][0] = max - uv[i][0]
		}
	}

	if flipV {

		max := uv[0][1]

		for _, v := range uv {
			if v[1] > max {
				max = v[1]
			}
		}

		for i := range uv {
			uv[i][1] = max - uv[i][1]
		}
	}

	return uv, nil
}

func GenerateSmoothNormals(pos [][3]float32, indices []uint32) [][3]float32 {
	normals := make([][3]float32, len(pos))

	for i := 0; i+2 < len(indices); i += 3 {
		i0 := indices[i]
		i1 := indices[i+1]
		i2 := indices[i+2]

		v0 := pos[i0]
		v1 := pos[i1]
		v2 := pos[i2]

		e1 := sub3(v1, v0)
		e2 := sub3(v2, v0)

		// 不先 normalize，直接累加 cross，等价于面积加权
		fn := cross3(e1, e2)

		normals[i0] = add3(normals[i0], fn)
		normals[i1] = add3(normals[i1], fn)
		normals[i2] = add3(normals[i2], fn)
	}

	for i := range normals {
		normals[i] = normalize3(normals[i])
	}

	return normals
}

func GenerateUVsBestPlane(pos [][3]float32, repeatX, repeatY float32) [][2]float32 {
	proj, _ := bestProject2D(pos)

	minU, minV := math.Inf(1), math.Inf(1)
	maxU, maxV := math.Inf(-1), math.Inf(-1)

	for _, p := range proj {
		if p[0] < minU {
			minU = p[0]
		}
		if p[0] > maxU {
			maxU = p[0]
		}
		if p[1] < minV {
			minV = p[1]
		}
		if p[1] > maxV {
			maxV = p[1]
		}
	}

	du := maxU - minU
	dv := maxV - minV
	if du == 0 {
		du = 1
	}
	if dv == 0 {
		dv = 1
	}

	uvs := make([][2]float32, 0, len(pos))
	for _, p := range proj {
		u := float32((p[0]-minU)/du) * repeatX
		v := float32((p[1]-minV)/dv) * repeatY
		uvs = append(uvs, [2]float32{u, v})
	}

	return uvs
}

func GenerateUVsBestPlaneTile(pos [][3]float32, repeatX, repeatY float32, stripSize float32) [][2]float32 {
	proj, _ := bestProject2D(pos)

	minU, minV := math.Inf(1), math.Inf(1)
	maxU, maxV := math.Inf(-1), math.Inf(-1)

	for _, p := range proj {
		if p[0] < minU {
			minU = p[0]
		}
		if p[0] > maxU {
			maxU = p[0]
		}
		if p[1] < minV {
			minV = p[1]
		}
		if p[1] > maxV {
			maxV = p[1]
		}
	}

	du := maxU - minU
	dv := maxV - minV
	if du == 0 {
		du = 1
	}
	if dv == 0 {
		dv = 1
	}

	if du > dv {
		repeatX = float32(du) / stripSize
	} else {
		repeatY = float32(dv) / stripSize
	}

	uvs := make([][2]float32, 0, len(pos))
	for _, p := range proj {
		u := float32((p[0]-minU)/du) * repeatX
		v := float32((p[1]-minV)/dv) * repeatY
		uvs = append(uvs, [2]float32{u, v})
	}

	return uvs
}

// PrimitiveWithHoles triangulates one polygon with optional inner holes on XZ plane.
// rings[0] is outer ring, rings[1:] are holes.
func PrimitiveWithHoles(doc *gltf.Document, rings [][][3]float32, material int, params UvsParams) (*gltf.Primitive, error) {
	pos, indices, err := EarcutXZRings(rings)
	if err != nil {
		return nil, err
	}

	repeatX := params.RepeatX
	repeatY := params.RepeatY
	if repeatX == 0 {
		repeatX = 1
	}
	if repeatY == 0 {
		repeatY = 1
	}

	uvs := GenerateUVsXZ(pos, repeatX, repeatY)
	if params.FlipV {
		for i := range uvs {
			uvs[i][1] = repeatY - uvs[i][1]
		}
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

func PrimitiveEXX(doc *gltf.Document, pos [][3]float32, material int) (*gltf.Primitive, error) {

	indices, err := EarcutXYZ(pos)
	if err != nil {
		return nil, err
	}

	uvs := GenerateUVsZ(pos)
	fmt.Println(uvs)
	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION: modeler.WritePosition(doc, pos),
			//gltf.NORMAL:     modeler.WriteNormal(doc, ComputeVertexNormals(pos, indices)),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},

		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil

}
func PrimitiveColor(doc *gltf.Document, pos [][3]float32, material int) (*gltf.Primitive, error) {
	indices, err := EarcutXY(pos)
	if err != nil {
		return nil, err
	}
	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION: modeler.WritePosition(doc, pos),
			//gltf.NORMAL:   modeler.WriteNormal(doc, ComputeVertexNormals(pos, indices)),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil

}

func getDocFromPrimitive(primitives []*gltf.Primitive, doc *gltf.Document) *gltf.Document {
	mesh := &gltf.Mesh{Primitives: primitives}
	meshIndex := len(doc.Meshes)
	doc.Meshes = append(doc.Meshes, mesh)
	node := &gltf.Node{
		Mesh: gltf.Index(meshIndex),
	}
	nodeIndex := len(doc.Nodes)
	doc.Nodes = append(doc.Nodes, node)
	if len(doc.Scenes) == 0 {
		scene := &gltf.Scene{
			Nodes: []int{nodeIndex},
		}
		doc.Scenes = []*gltf.Scene{scene}
		doc.Scene = gltf.Index(0)
	} else {
		doc.Scenes[0].Nodes = append(doc.Scenes[0].Nodes, nodeIndex)
		if doc.Scene == nil {
			doc.Scene = gltf.Index(0)
		}
	}
	fixPositionAccessorBounds(doc)
	return doc
}

func fixPositionAccessorBounds(doc *gltf.Document) {
	if doc == nil {
		return
	}
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
			minv := [3]float64{float64(pos[0][0]), float64(pos[0][1]), float64(pos[0][2])}
			maxv := minv
			for i := 1; i < len(pos); i++ {
				x, y, z := float64(pos[i][0]), float64(pos[i][1]), float64(pos[i][2])
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
			acc.Min = []float64{minv[0], minv[1], minv[2]}
			acc.Max = []float64{maxv[0], maxv[1], maxv[2]}
		}
	}
}

func SaveGltfForPrimitive(primitives []*gltf.Primitive, doc *gltf.Document, path string) error {
	doc = getDocFromPrimitive(primitives, doc)
	return gltf.Save(doc, path)
}

func SaveGlbForPrimitive(primitives []*gltf.Primitive, doc *gltf.Document, path string) error {
	doc = getDocFromPrimitive(primitives, doc)
	return gltf.SaveBinary(doc, path)
}
func BytesForPrimitive(primitives []*gltf.Primitive, doc *gltf.Document) ([]byte, error) {
	doc = getDocFromPrimitive(primitives, doc)
	buff := new(bytes.Buffer)
	e := gltf.NewEncoder(buff)
	e.AsBinary = true
	err := e.Encode(doc)
	return buff.Bytes(), err
}

func sub3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[0] - b[0],
		a[1] - b[1],
		a[2] - b[2],
	}
}

func add3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[0] + b[0],
		a[1] + b[1],
		a[2] + b[2],
	}
}

func cross3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func normalize3(v [3]float32) [3]float32 {
	l := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	if l == 0 {
		return [3]float32{0, 0, 1}
	}
	return [3]float32{
		v[0] / l,
		v[1] / l,
		v[2] / l,
	}
}

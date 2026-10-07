package mgltf

import (
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// 判断两个三维顶点在 XZ 平面上的位置是否相同。
//
// 注意:
// 忽略 Y 高度值，只比较:
// - X 坐标。
// - Z 坐标。
//
// 用途:
// 判断 Polygon Ring 是否已经闭合。
//
// 参数:
// a,b:
//
//	三维顶点 [X,Y,Z]
//
// 返回:
// true:
//
//	两个点在 XZ 平面重合。
func sameVertexXZ(a, b [3]float32) bool {
	return a[0] == b[0] && a[2] == b[2]
}

// 标准化 Polygon Ring。
//
// 如果 Ring 的最后一个点与第一个点在 XZ 平面重合，
// 删除最后一个重复闭合点。
//
// 例如:
//
// 输入:
// [
//
//	A,
//	B,
//	C,
//	A
//
// ]
//
// 输出:
// [
//
//	A,
//	B,
//	C
//
// ]
//
// 用途:
// 避免 Earcut 三角化时出现重复顶点
func normalizeClosedRing(pos [][3]float32) [][3]float32 {
	if len(pos) >= 2 && sameVertexXZ(pos[0], pos[len(pos)-1]) {
		return pos[:len(pos)-1]
	}
	return pos
}

// 根据 XY 平面范围生成纹理 UV 坐标。
//
// 方法:
// 1. 计算顶点 XY 包围盒。
// 2. 将坐标归一化到 0~1。
// 3. 根据 repeat 参数进行纹理重复缩放。
//
// 用途:
// 为 glTF 顶面生成纹理坐标。
//
// 参数:
// vertices:
//
//	三维顶点。
//
// repeatX/repeatY:
//
//	纹理重复次数。
//
// 返回:
// UV 坐标数组。
func GenerateUVsXY(vertices [][3]float32, repeatX, repeatY float32) [][2]float32 {
	uvs := make([][2]float32, len(vertices))
	if len(vertices) == 0 {
		return uvs
	}

	minX, maxX := vertices[0][0], vertices[0][0]
	minY, maxY := vertices[0][1], vertices[0][1]
	for _, v := range vertices[1:] {
		if v[0] < minX {
			minX = v[0]
		}
		if v[0] > maxX {
			maxX = v[0]
		}
		if v[2] < minY {
			minY = v[1]
		}
		if v[2] > maxY {
			maxY = v[1]
		}
	}
	w := maxX - minX
	l := maxY - minY
	if w == 0 {
		w = 1
	}
	if l == 0 {
		l = 1
	}

	for i, v := range vertices {
		u := (v[0] - minX) / w
		vv := (v[1] - minY) / l
		uvs[i] = [2]float32{u * repeatX, vv * repeatY}
	}
	return uvs
}

// 根据 XZ 平面范围生成纹理 UV 坐标。
//
// 适用:
// - 水平面。
// - 道路面。
// - 建筑楼板。
//
// 方法:
// 使用 X,Z 坐标计算包围盒，
// 映射到纹理坐标空间。
//
// 参数:
// vertices:
//
//	三维顶点。
//
// repeatX/repeatY:
//
//	UV重复比例。
//
// 返回:
// UV坐标。
func GenerateUVsXZ(vertices [][3]float32, repeatX, repeatY float32) [][2]float32 {
	uvs := make([][2]float32, len(vertices))
	if len(vertices) == 0 {
		return uvs
	}

	minX, maxX := vertices[0][0], vertices[0][0]
	minZ, maxZ := vertices[0][2], vertices[0][2]
	for _, v := range vertices[1:] {
		if v[0] < minX {
			minX = v[0]
		}
		if v[0] > maxX {
			maxX = v[0]
		}
		if v[2] < minZ {
			minZ = v[2]
		}
		if v[2] > maxZ {
			maxZ = v[2]
		}
	}
	w := maxX - minX
	h := maxZ - minZ
	if w == 0 {
		w = 1
	}
	if h == 0 {
		h = 1
	}

	for i, v := range vertices {
		u := (v[0] - minX) / w
		vv := (v[2] - minZ) / h
		uvs[i] = [2]float32{u * repeatX, vv * repeatY}
	}
	return uvs
}

// 反转三角形索引方向。
//
// 功能:
// 将:
//
// (a,b,c)
//
// 转换为:
//
// (a,c,b)
//
// 用途:
// 改变 Mesh 面朝向。
// 常用于生成底面，使法向量朝向相反方向。
//
// 参数:
// indices:
//
//	三角索引。
//
// 返回:
// 反向后的索引。
func reverseTriangles(indices []uint32) []uint32 {
	out := make([]uint32, len(indices))
	copy(out, indices)
	for i := 0; i+2 < len(out); i += 3 {
		out[i+1], out[i+2] = out[i+2], out[i+1]
	}
	return out
}

// 将 Polygon 顶面沿 Y 轴向下拉伸生成底面。
//
// 坐标约定:
//
// X:
//
//	水平方向
//
// Y:
//
//	高度方向(up)
//
// Z:
//
//	水平方向
//
// 参数:
//
// pos:
//
//	顶面顶点。
//
// thickness:
//
//	拉伸厚度。
//
// 返回:
// 下表面顶点。
func extrudeDown(pos [][3]float32, thickness float32) [][3]float32 {
	out := make([][3]float32, len(pos))
	for i := range pos {
		out[i] = [3]float32{pos[i][0], pos[i][1] - thickness, pos[i][2]}
	}
	return out
}

// 根据顶部和底部两组顶点生成侧壁三角索引。
//
// 顶点布局:
//
// 0 ~ n-1:
//
//	顶部Ring
//
// n ~ 2n-1:
//
//	底部Ring
//
// 每两个相邻点生成两个三角形:
//
// topA-topB-bottomB
// topA-bottomB-bottomA
//
// 用途:
// 生成实体侧墙 Mesh。
func wallIndices(n int) []uint32 {
	// vertices layout: 0..n-1 top, n..2n-1 bottom
	indices := make([]uint32, 0, n*6)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a := uint32(i)
		b := uint32(j)
		c := uint32(j + n)
		d := uint32(i + n)
		indices = append(indices, a, b, c)
		indices = append(indices, a, c, d)
	}
	return indices
}

// 生成带顶点偏移的侧壁三角索引。
//
// 用途:
// 当多个 Ring 合并到同一个顶点数组时，
// 调整索引对应的顶点起始位置。
//
// 参数:
//
// n:
//
//	当前 Ring 顶点数量。
//
// base:
//
//	顶点数组中的偏移量。
func wallIndicesOffset(n int, base uint32) []uint32 {
	out := wallIndices(n)
	for i := range out {
		out[i] += base
	}
	return out
}

// 根据 Polygon Ring 周长生成侧壁 UV 坐标。
//
// UV规则:
//
// U:
//
//	沿 Ring 周向累计距离映射。
//
// V:
//
//	顶部为0。
//	底部为repeatV。
//
// 用途:
// 生成拉伸实体侧面的连续纹理。
//
// 参数:
//
// top:
//
//	顶部Ring点。
//
// thickness:
//
//	拉伸高度。
//
// repeatU/repeatV:
//
//	纹理重复参数。
func wallUVsXZ(top [][3]float32, thickness float32, repeatU, repeatV float32) [][2]float32 {
	// U: cumulative distance along ring in XZ
	// V: 0 (top) -> repeatV (bottom)
	uvs := make([][2]float32, 0, len(top)*2)
	if len(top) == 0 {
		return uvs
	}

	cum := make([]float32, len(top))
	var total float32
	for i := 1; i < len(top); i++ {
		total += Distance([3]float32{top[i-1][0], top[i-1][2], 0}, [3]float32{top[i][0], top[i][2], 0})
		cum[i] = total
	}
	// close ring
	total += Distance([3]float32{top[len(top)-1][0], top[len(top)-1][2], 0}, [3]float32{top[0][0], top[0][2], 0})
	if total == 0 {
		total = 1
	}

	for i := range top {
		u := (cum[i] / total) * repeatU
		uvs = append(uvs, [2]float32{u, 0})
	}
	for i := range top {
		u := (cum[i] / total) * repeatU
		uvs = append(uvs, [2]float32{u, repeatV})
	}
	_ = thickness
	return uvs
}

// 将单个 Polygon 外环拉伸生成闭合三维实体。
//
// 生成三个 glTF Primitive:
//
//  1. Top:
//     顶面三角网。
//
//  2. Bottom:
//     底面三角网。
//
//  3. Walls:
//     四周侧壁。
//
// 坐标约定:
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
// 用途:
// - 建筑模型。
// - 道路设施。
// - 隧道结构。
// - 三维面实体化。
//
// 参数:
//
// ring:
//
//	Polygon外环。
//
// thickness:
//
//	拉伸厚度。
//
// material:
//
//	glTF材质索引。
//
// params:
//
//	UV参数。
//
// 返回:
// glTF Primitive列表。
func ExtrudePolygonSolidPrimitives(doc *gltf.Document, ring [][3]float32, thickness float32, material int, params UvsParams) ([]*gltf.Primitive, error) {
	if thickness <= 0 {
		return nil, fmt.Errorf("thickness must be > 0")
	}

	ring = normalizeClosedRing(ring)
	if len(ring) < 3 {
		return nil, fmt.Errorf("ring needs at least 3 points")
	}

	// Top
	topIdx, err := EarcutXZ(ring)
	if err != nil {
		return nil, err
	}
	topUV := GenerateUVsXZ(ring, params.RepeatX, params.RepeatY)
	top := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, ring),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, topUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, topIdx)),
		Material: gltf.Index(material),
	}

	// Bottom
	bottomPos := extrudeDown(ring, thickness)
	bottomIdx := reverseTriangles(topIdx)
	bottomUV := topUV
	bottom := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, bottomPos),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, bottomUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, bottomIdx)),
		Material: gltf.Index(material),
	}

	// Walls
	n := len(ring)
	wallPos := make([][3]float32, 0, n*2)
	wallPos = append(wallPos, ring...)
	wallPos = append(wallPos, bottomPos...)
	wallIdx := wallIndices(n)
	wallUV := wallUVsXZ(ring, thickness, params.RepeatX, params.RepeatY)
	walls := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, wallPos),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, wallUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, wallIdx)),
		Material: gltf.Index(material),
	}

	return []*gltf.Primitive{top, bottom, walls}, nil
}

// 将带洞 Polygon 拉伸生成闭合三维实体。
//
// 支持:
//
// rings[0]:
//
//	外轮廓。
//
// rings[1:]:
//
//	内孔洞。
//
// 生成:
//
// Top:
//
//	支持Hole的三角面。
//
// Bottom:
//
//	对应底面。
//
// Walls:
//
//	外环和所有洞边界侧壁。
//
// 用途:
//
// - 建筑中空结构。
// - 隧道断面。
// - 带孔地面模型。
//
// 参数:
//
// rings:
//
//	Polygon Ring集合。
//
// thickness:
//
//	拉伸厚度。
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
// glTF Primitive列表。
func ExtrudePolygonSolidPrimitivesWithHoles(doc *gltf.Document, rings [][][3]float32, thickness float32, material int, params UvsParams) ([]*gltf.Primitive, error) {
	if thickness <= 0 {
		return nil, fmt.Errorf("thickness must be > 0")
	}
	if len(rings) == 0 {
		return nil, fmt.Errorf("rings are empty")
	}

	topPos, topIdx, err := EarcutXZRings(rings)
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

	topUV := GenerateUVsXZ(topPos, repeatX, repeatY)
	top := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, topPos),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, topUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, topIdx)),
		Material: gltf.Index(material),
	}

	bottomPos := extrudeDown(topPos, thickness)
	bottom := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, bottomPos),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, topUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, reverseTriangles(topIdx))),
		Material: gltf.Index(material),
	}

	wallPos := make([][3]float32, 0)
	wallUV := make([][2]float32, 0)
	wallIdx := make([]uint32, 0)
	for _, ring := range rings {
		r := normalizeClosedRing(ring)
		if len(r) < 3 {
			continue
		}
		bottomRing := extrudeDown(r, thickness)
		base := uint32(len(wallPos))
		wallPos = append(wallPos, r...)
		wallPos = append(wallPos, bottomRing...)
		wallUV = append(wallUV, wallUVsXZ(r, thickness, repeatX, repeatY)...)
		wallIdx = append(wallIdx, wallIndicesOffset(len(r), base)...)
	}
	if len(wallPos) == 0 {
		return []*gltf.Primitive{top, bottom}, nil
	}

	walls := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, wallPos),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, wallUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, wallIdx)),
		Material: gltf.Index(material),
	}

	return []*gltf.Primitive{top, bottom, walls}, nil
}

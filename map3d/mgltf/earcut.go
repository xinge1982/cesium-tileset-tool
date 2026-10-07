package mgltf

import (
	"fmt"
	"math"

	"github.com/rclancey/go-earcut"
)

// 将三维 Polygon 顶点投影到 XZ 平面后进行 Earcut 三角剖分。
//
// 适用场景:
// - 水平面。
// - 地面。
// - 道路面。
// - glTF 中 Y 轴作为 up 方向的模型面。
//
// 输入:
// pos:
//
//	三维顶点列表，格式:
//	[X, Y(up), Z]
//
// 处理:
// 1. 忽略 Y 坐标。
// 2. 使用 X,Z 构造二维 Polygon。
// 3. 调用 Earcut 算法生成三角索引。
//
// 返回:
// - 三角形索引。
// - 错误信息。
func EarcutXZ(pos [][3]float32) ([]uint32, error) {
	pos64 := make([]float64, 0, len(pos)*2)
	for _, p := range pos {
		// 用 X,Z 做 2D 剖分
		pos64 = append(pos64, float64(p[0]), float64(p[2]))
	}
	ect, err := earcut.Earcut(pos64, nil, 2)
	if err != nil {
		return nil, err
	}
	uect := make([]uint32, len(ect))
	for i := range ect {
		uect[i] = uint32(ect[i])
	}
	return uect, nil
}

// 对 XZ 平面的多 Ring Polygon 进行三角剖分。
//
// 支持:
// - 一个外环。
// - 多个内部洞(Hole)。
//
// rings:
//
//	rings[0] 为外轮廓。
//	rings[1:] 为洞区域。
//
// 功能:
// 1. 标准化每个闭合 Ring。
// 2. 展平成 Earcut 输入格式。
// 3. 设置 Hole 起始索引。
// 4. 返回顶点数组和三角索引。
//
// 适用:
// - 带孔道路面。
// - 建筑楼板。
// - 隧道截面。
func EarcutXZRings(rings [][][3]float32) ([][3]float32, []uint32, error) {
	flat := make([][3]float32, 0)
	holes := make([]int, 0)

	for i := range rings {
		r := normalizeClosedRing(rings[i])
		if len(r) < 3 {
			continue
		}
		if i > 0 {
			holes = append(holes, len(flat))
		}
		flat = append(flat, r...)
	}
	if len(flat) < 3 {
		return nil, nil, fmt.Errorf("rings need at least 3 vertices")
	}

	pos64 := make([]float64, 0, len(flat)*2)
	for _, p := range flat {
		pos64 = append(pos64, float64(p[0]), float64(p[2]))
	}

	ect, err := earcut.Earcut(pos64, holes, 2)
	if err != nil {
		return nil, nil, err
	}

	uect := make([]uint32, len(ect))
	for i := range ect {
		uect[i] = uint32(ect[i])
	}
	return flat, uect, nil
}

// 在 XY 平面上对 Polygon 进行 Earcut 三角剖分。
//
// 输入:
// pos:
//
//	三维点列表。
//
// 处理:
// 使用:
//
//	X,Y
//
// 作为二维剖分坐标。
//
// 适用:
// - 顶视图面。
// - 普通二维 Polygon。
// - glTF XY 平面模型。
//
// 返回:
// 三角索引。
func EarcutXY(pos [][3]float32) ([]uint32, error) {
	var pos64 []float64
	for i := range pos {
		pos64 = append(pos64, float64(pos[i][0]), float64(pos[i][1]), float64(pos[i][2]))
	}
	ect, err := earcut.Earcut(pos64, nil, 3)
	if err != nil {
		return nil, err
	}

	var uect = make([]uint32, len(ect))
	for i := range ect {
		uect[i] = uint32(ect[i])
	}
	return uect, nil
}

// 对规则三维带状 Polygon 直接生成三角索引。
//
// 注意:
// 该函数不进行真正的 Earcut 几何计算，
// 而是假设输入点已经按照规则排列。
//
// 生成方式:
// 从两端向中间连接生成三角形。
//
// 适用:
// - 隧道墙面。
// - 拉伸面。
// - 已知拓扑结构的矩形带状面。
//
// 返回:
// 三角索引。
func EarcutXYZ(pos [][3]float32) ([]uint32, error) {
	var uect []uint32
	pl := len(pos)
	for i := 0; i < pl/2-1; i++ {
		uect = append(uect, uint32(i), uint32(i+1), uint32(pl-2-i))
		uect = append(uect, uint32(i), uint32(pl-2-i), uint32(pl-1-i))
	}
	return uect, nil
}

// 将三维 Polygon 投影到 YZ 平面后进行三角剖分。
//
// 适用场景:
// - 垂直墙面。
// - 隧道侧壁。
// - 立面模型。
//
// 输入:
// pos:
//
//	[X,Y,Z]
//
// 使用:
//
//	Y,Z
//
// 作为二维剖分坐标。
//
// 返回:
// 三角索引。
func EarcutYZ(pos [][3]float32) ([]uint32, error) {
	if len(pos) < 3 {
		return nil, fmt.Errorf("not enough points")
	}

	coords := make([]float64, 0, len(pos)*2)

	for _, p := range pos {
		// YZ projection
		coords = append(coords, float64(p[1]), float64(p[2]))
	}

	triangles, err := earcut.Earcut(coords, nil, 2)
	if err != nil {
		return nil, err
	}

	indices := make([]uint32, len(triangles))
	for i, v := range triangles {
		indices[i] = uint32(v)
	}

	return indices, nil
}

// 自动选择最佳二维投影平面对三维 Polygon 进行三角剖分。
//
// 流程:
// 1. 计算 Polygon 法向量。
// 2. 根据法向量方向选择投影平面:
//   - 法向量 X 最大 -> YZ 平面。
//   - 法向量 Y 最大 -> XZ 平面。
//   - 法向量 Z 最大 -> XY 平面。
//
// 3. 使用 Earcut 完成二维三角化。
//
// 适用:
// - 任意方向三维面。
// - 模型表面。
// - glTF Mesh 自动生成。
func EarcutBestPlane(pos [][3]float32) ([]uint32, error) {
	proj, _ := bestProject2D(pos)

	pos64 := make([]float64, 0, len(proj)*2)
	for _, p := range proj {
		pos64 = append(pos64, p[0], p[1])
	}

	ect, err := earcut.Earcut(pos64, nil, 2)
	if err != nil {
		return nil, err
	}

	uect := make([]uint32, len(ect))
	for i := range ect {
		uect[i] = uint32(ect[i])
	}
	return uect, nil
}

// 根据 Polygon 法向量选择最佳二维投影方式。
//
// 原理:
// 投影时删除法向量最大方向的轴，
// 可以减少投影失真。
//
// 示例:
//
// 法向量主要为 Y:
//
//	表示水平面
//	使用 XZ
//
// 法向量主要为 X:
//
//	表示侧面
//	使用 YZ
//
// 法向量主要为 Z:
//
//	使用 XY
//
// 返回:
// - 二维投影坐标。
// - 被删除的轴编号。
func bestProject2D(pos [][3]float32) ([][2]float64, int) {
	n := polygonNormal(pos)

	ax := math.Abs(n[0])
	ay := math.Abs(n[1])
	az := math.Abs(n[2])

	out := make([][2]float64, 0, len(pos))

	// drop axis with largest normal component
	// normal mostly Y => horizontal face => use XZ
	// normal mostly X => vertical face => use YZ
	// normal mostly Z => vertical face => use XY
	if ax >= ay && ax >= az {
		for _, p := range pos {
			out = append(out, [2]float64{float64(p[1]), float64(p[2])}) // YZ
		}
		return out, 0
	}

	if ay >= ax && ay >= az {
		for _, p := range pos {
			out = append(out, [2]float64{float64(p[0]), float64(p[2])}) // XZ
		}
		return out, 1
	}

	for _, p := range pos {
		out = append(out, [2]float64{float64(p[0]), float64(p[1])}) // XY
	}
	return out, 2
}

// 使用 Newell 方法计算 Polygon 法向量。
//
// 功能:
// 根据 Polygon 顶点顺序计算面朝向。
//
// 用途:
// - 判断 Polygon 所在主要平面。
// - 自动选择 Earcut 投影方向。
// - 判断模型表面方向。
//
// 返回:
// [nx,ny,nz]
//
// 表示 Polygon 法向量。
func polygonNormal(pos [][3]float32) [3]float64 {
	var nx, ny, nz float64

	n := len(pos)
	for i := 0; i < n; i++ {
		p := pos[i]
		q := pos[(i+1)%n]

		x1, y1, z1 := float64(p[0]), float64(p[1]), float64(p[2])
		x2, y2, z2 := float64(q[0]), float64(q[1]), float64(q[2])

		nx += (y1 - y2) * (z1 + z2)
		ny += (z1 - z2) * (x1 + x2)
		nz += (x1 - x2) * (y1 + y2)
	}

	return [3]float64{nx, ny, nz}
}

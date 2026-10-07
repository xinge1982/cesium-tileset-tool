package common

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const defaultCurbStoneLength = float32(0.5)
const defaultNewJerseyTextureLength = float32(2.0)

// 根据道路边线生成普通路缘石（road curb）的三维 Mesh。
// 处理流程:
//  1. 清理输入道路线重复点
//  2. 根据 direction 对道路边线做单侧 offset，生成外侧边界
//  3. 对原始线和 offset 后的线进行高度拉伸
//  4. 创建:
//     - 顶面
//     - 外侧面
//     - 内侧面
//     - 起终端封口面
//  5. 返回可用于 glTF/3D Tiles 的 Mesh 数据
//
// 返回:
// positions:
//
//	Mesh 顶点坐标
//
// normals:
//
//	顶点法线，用于光照计算
//
// uv:
//
//	纹理坐标，沿道路方向按照固定长度重复
//
// indices:
//
//	三角面索引
//
// 参数:
// line:
//
//	道路边界线
//
// width:
//
//	路缘石向外扩展宽度
//
// height:
//
//	路缘石高度
//
// direction:
//
//	偏移方向
func buildRoadCurbMesh(line LocalLine, width, height float32, direction int) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("road curb line needs at least 2 points")
	}
	if width <= 0 || height <= 0 {
		return nil, nil, nil, nil, fmt.Errorf("road curb width/height must be > 0")
	}

	// 根据道路方向生成一侧偏移线
	// 该线作为路缘石外侧边界
	outer := offsetPolylineOneSide(points, width, direction)

	// 生成顶部边界
	innerTop := raisePolyline(points, height)
	outerTop := raisePolyline(outer, height)

	// 计算累计长度
	// 用于生成连续纹理 UV
	cum := polylineCumLengthsF32(points)
	section := height + width + height
	vInnerTop := height / section
	vOuterTop := (height + width) / section
	vOuterBottom := float32(1)
	pos := make([][3]float32, 0, len(points)*6+8)
	normals := make([][3]float32, 0, len(points)*6+8)
	uv := make([][2]float32, 0, len(points)*6+8)
	indices := make([]uint32, 0, (len(points)-1)*18+12)

	// 计算外侧面的法线
	// 用于保证光照方向正确
	sideNormals := make([][3]float32, len(points))
	for i := range points {
		sideNormals[i] = normalize3([3]float32{
			outer[i][0] - points[i][0],
			0,
			outer[i][2] - points[i][2],
		})
	}

	// 生成顶部水平面
	appendCurbStrip(&pos, &normals, &uv, &indices, innerTop, outerTop, [3]float32{0, 1, 0}, cum, vInnerTop, vOuterTop)
	// 生成外侧垂直面
	appendCurbStripWithNormals(&pos, &normals, &uv, &indices, outer, outerTop, sideNormals, cum, vOuterBottom, vOuterTop)

	// 生成道路侧垂直面
	innerNormals := make([][3]float32, len(sideNormals))
	for i := range sideNormals {
		innerNormals[i] = [3]float32{-sideNormals[i][0], 0, -sideNormals[i][2]}
	}
	appendCurbStripWithNormals(&pos, &normals, &uv, &indices, points, innerTop, innerNormals, cum, 0, vInnerTop)

	// 起点封口
	startNormal := endCapNormal(points[0], points[1], true)
	appendQuadFacing(&pos, &normals, &uv, &indices,
		points[0], outer[0], outerTop[0], innerTop[0],
		startNormal,
		[2]float32{0, 0}, [2]float32{1, 0}, [2]float32{1, 1}, [2]float32{0, 1},
	)

	// 终点封口
	last := len(points) - 1
	endNormal := endCapNormal(points[last-1], points[last], false)
	appendQuadFacing(&pos, &normals, &uv, &indices,
		points[last], innerTop[last], outerTop[last], outer[last],
		endNormal,
		[2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0},
	)

	return pos, normals, uv, indices, nil
}

// 创建一个沿折线方向展开的矩形条带面。
// 主要用于:
//   - 路缘石顶部面
//   - 两条平行 polyline 之间的面
//
// 该函数使用统一法线，
// 内部调用 appendCurbStripWithNormals。
func appendCurbStrip(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, left, right [][3]float32, normal [3]float32, cum []float32, v0, v1 float32) {
	ns := make([][3]float32, len(left))
	for i := range ns {
		ns[i] = normal
	}
	appendCurbStripWithNormals(pos, normals, uv, indices, left, right, ns, cum, v0, v1)
}

// 根据两条对应 polyline 生成 Mesh 面。
// 每个点生成两个顶点:
//
//	left[i]
//	right[i]
//
// 相邻两个点组成两个三角形:
//
//	A----B
//	|   /
//	|  /
//	C----D
//
// 同时生成:
//   - 顶点
//   - 法线
//   - UV
//   - 三角索引
//
// UV:
//
//	U方向根据累计距离计算
//	V方向由调用者指定
func appendCurbStripWithNormals(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, left, right, perPointNormals [][3]float32, cum []float32, v0, v1 float32) {
	if len(left) != len(right) || len(left) != len(perPointNormals) || len(left) < 2 {
		return
	}
	base := uint32(len(*pos))
	for i := range left {
		u := cum[i] / defaultCurbStoneLength
		*pos = append(*pos, left[i], right[i])
		*normals = append(*normals, perPointNormals[i], perPointNormals[i])
		*uv = append(*uv, [2]float32{u, v0}, [2]float32{u, v1})
	}
	for i := 0; i < len(left)-1; i++ {
		a := base + uint32(i*2)
		b := a + 1
		c := a + 2
		d := a + 3
		*indices = append(*indices, a, c, b, b, c, d)
	}
}

// 根据 FeatureFields 中的 direction 属性确定路缘石偏移方向。
// 数据来源通常是道路设施 GeoJSON 属性:
// direction:
//
//	1 -> 转换为内部方向 2
//	2 -> 转换为内部方向 1
//
// 未指定时默认使用方向 2。
// 返回值:
//
//	1 / 2
func curbDirection(fields FeatureFields) int {
	v := int64FromFields(fields, "direction")
	if v == 1 {
		return 2
	}
	if v == 2 {
		return 1
	}
	return 2
}

// 根据 New Jersey 防撞墙截面 profile，
// 沿道路 LineString 扫掠生成三维混凝土护栏 Mesh。
// 生成流程:
//  1. 创建二维防撞墙截面
//  2. 根据道路方向调整截面左右方向
//  3. 沿 LocalLine 进行 sweep
//  4. 生成:
//     - positions
//     - normals
//     - uv
//     - indices
//
// 该函数适用于:
//   - 高速公路中央隔离带
//   - 路侧混凝土护栏
//   - 隧道边墙防撞结构
//
// 参数:
// line:
//
//	护栏中心线
//
// width:
//
//	防撞墙底部宽度
//
// height:
//
//	防撞墙高度
//
// direction:
//
//	扫掠方向
func buildNewJerseyBarrierMesh(line LocalLine, width, height float32, direction int) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	profiles := NewProfileFactory()
	profile := Profile2D{
		Name:   "new_jersey_barrier",
		Points: buildNewJerseyBarrierProfile(width, height),
		Closed: false,
	}
	sign := float32(1)
	if direction == 2 {
		sign = -1
	}
	pos, normals, uv, indices, err := profiles.SweepProfileMesh(profile, line, SweepMeshOptions{
		LateralSign: sign,
		RepeatU:     defaultNewJerseyTextureLength,
		CloseCaps:   true,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	section := width + 2*height
	if section > 0 {
		for i := range uv {
			uv[i][1] /= section
			uv[i][0] = 1 - uv[i][0]
			uv[i][1] = 1 - uv[i][1]
		}
	}
	return pos, normals, uv, indices, nil
}

// 从 FeatureFields 中安全读取整数属性。
// 支持输入类型:
//
//	int
//	int32
//	int64
//	float32
//	float64
//	string
//
// 主要用于解析 GeoJSON properties 中
// 可能存在的动态类型字段。
func int64FromFields(fields FeatureFields, key string) int64 {
	if fields == nil {
		return 0
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err == nil {
			return n
		}
	}
	return 0
}

// 对道路折线进行单侧偏移。
// 与简单平移不同，该函数考虑:
//  1. 每个线段方向
//  2. 相邻线段夹角
//  3. 转角 join 修正
//
// 主要用于:
//
//	原始道路边线
//	       |
//	       |
//	       +------
//
// 生成:
//
//	偏移后的路缘石外边界
//
// 参数:
// points:
//
//	原始折线点
//
// width:
//
//	偏移距离
//
// direction:
//
//	偏移方向
func offsetPolylineOneSide(points [][3]float32, width float32, direction int) [][3]float32 {
	segNormals := buildSegmentNormals(points)
	out := make([][3]float32, len(points))
	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}
	for i := range points {
		// 获取当前点 join normal
		nx, nz := lineJoinNormal(points, segNormals, i)
		scale := width
		// 中间点进行 miter 修正
		if i > 0 && i < len(points)-1 {
			prevNX, prevNZ := segNormals[i-1][0], segNormals[i-1][1]
			dot := float32(nx*prevNX + nz*prevNZ)
			if dot < 0.25 {
				dot = 0.25
			}
			scale = width / dot
		}
		// 限制尖角无限放大
		if scale > width*4 {
			scale = width * 4
		}
		out[i] = [3]float32{
			points[i][0] + sign*float32(nx)*scale,
			points[i][1],
			points[i][2] + sign*float32(nz)*scale,
		}
	}
	return out
}

// 将二维折线沿高度方向抬升。
// 当前坐标约定:
// X:
//
//	横向
//
// Y:
//
//	高度
//
// Z:
//
//	纵向
//
// 常用于生成:
//
//	底部线 -> 顶部线
func raisePolyline(points [][3]float32, height float32) [][3]float32 {
	out := make([][3]float32, len(points))
	for i := range points {
		out[i] = [3]float32{points[i][0], points[i][1] + height, points[i][2]}
	}
	return out
}

// 计算折线累计长度。
// 结果用于 UV 映射:
// U = distance / textureLength
// 例如:
// 每 0.5m 重复一次路缘石纹理。
func polylineCumLengthsF32(points [][3]float32) []float32 {
	out := make([]float32, len(points))
	for i := 1; i < len(points); i++ {
		dx := points[i][0] - points[i-1][0]
		dz := points[i][2] - points[i-1][2]
		out[i] = out[i-1] + float32(math.Hypot(float64(dx), float64(dz)))
	}
	return out
}

// 创建一个四边形面，并转换为两个三角形。
// 输入:
// a,b,c,d:
//
//	四边形四个顶点
//
// normal:
//
//	面法线
//
// ua,ub,uc,ud:
//
//	四个顶点 UV 坐标
//
// 输出:
// positions:
//
//	增加4个顶点
//
// normals:
//
//	增加4个法线
//
// uv:
//
//	增加4个纹理坐标
//
// indices:
//
//	增加两个三角面
//
// 三角划分:
// a-----b
// |    /|
// |   / |
// |  /  |
// d-----c
func appendQuad(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, a, b, c, d [3]float32, normal [3]float32, ua, ub, uc, ud [2]float32) {
	base := uint32(len(*pos))
	// 添加顶点
	*pos = append(*pos, a, b, c, d)
	// 四个顶点共享同一个法线
	*normals = append(*normals, normal, normal, normal, normal)
	// 添加 UV
	*uv = append(*uv, ua, ub, uc, ud)
	// 两个三角形
	*indices = append(*indices,
		base, base+1, base+2,
		base, base+2, base+3,
	)
}

// 创建四边形封口面，并检查三角形方向。
// 由于顶点输入顺序可能导致:
// 正面:
//
//	normal ->
//
// 或:
// 背面:
//
//	<- normal
//
// 这里通过 triangleNormal 判断，
// 必要时交换 b/d 顺序，保证 Mesh 朝向正确。
func appendQuadFacing(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, a, b, c, d [3]float32, normal [3]float32, ua, ub, uc, ud [2]float32) {
	if dot3(triangleNormal(a, b, c), normal) < 0 {
		// 翻转绕序
		b, d = d, b
		ub, ud = ud, ub
	}
	appendQuad(pos, normals, uv, indices, a, b, c, d, normal, ua, ub, uc, ud)
}

// 计算三角面的水平法线。
// 与普通 triangleNormal 不同:
// 忽略 Y 方向分量，
// 只保留 X/Z 平面方向。
// 常用于:
// - 道路方向
// - 水平面 offset
func horizontalNormal(a, b, c [3]float32) [3]float32 {
	n := triangleNormal(a, b, c)
	n[1] = 0
	return normalize3(n)
}

// 计算道路端面封口法线。
// 用于路缘石开始和结束位置的 cap 面。
// reverse=true:
//
//	起点方向反转
//
// reverse=false:
//
//	终点方向
func endCapNormal(a, b [3]float32, reverse bool) [3]float32 {
	dx := b[0] - a[0]
	dz := b[2] - a[2]
	l := float32(math.Hypot(float64(dx), float64(dz)))
	if l == 0 {
		return [3]float32{0, 0, 1}
	}
	n := [3]float32{dx / l, 0, dz / l}
	if reverse {
		n[0] = -n[0]
		n[2] = -n[2]
	}
	return n
}

// 三维向量点积。
// 用于:
// - 判断两个方向关系
// - 判断法线方向
// - 判断面朝向
func dot3(a, b [3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

// 生成圆角路缘石二维截面。
// 相比简单矩形:
// +-----+
// 圆角版本:
//
//	  ___
//	/     \
//
// |       |
// |       |
// +-------+
// 用于生成更真实的道路边缘模型。
// width:
//
//	宽度
//
// height:
//
//	高度
func buildRoundedCurbProfile(width, height float32) [][2]float32 {
	radius := width * 0.18
	if radius > height*0.22 {
		radius = height * 0.22
	}
	if radius <= 0 {
		return [][2]float32{{0, 0}, {0, height}, {width, height}, {width, 0}}
	}
	segments := 2
	points := make([][2]float32, 0, 2*segments+4)
	// 左下 -> 左上
	points = append(points, [2]float32{0, 0}, [2]float32{0, height - radius})
	// 左上圆角
	appendArcPoints(&points, radius, height-radius, radius, math.Pi, math.Pi/2, segments, false)
	points = append(points, [2]float32{width - radius, height})
	// 右上圆角
	appendArcPoints(&points, width-radius, height-radius, radius, math.Pi/2, 0, segments, true)
	points = append(points, [2]float32{width, 0})
	return dedupeProfilePoints(points)
}

// 创建 New Jersey 防撞墙二维截面。
// 该 profile 会沿道路线 sweep，
// 形成连续混凝土护栏。
// 返回:
// 一组二维点:
// (width,height)
// 坐标比例根据输入尺寸缩放。
func buildNewJerseyBarrierProfile(width, height float32) [][2]float32 {
	return [][2]float32{
		{0.00 * width, 0.00 * height},
		{0.08 * width, 0.06 * height},
		{0.18 * width, 0.24 * height},
		{0.24 * width, 0.56 * height},
		{0.28 * width, 0.82 * height},
		{0.34 * width, 1.00 * height},
		{0.64 * width, 1.00 * height},
		{0.72 * width, 0.72 * height},
		{0.84 * width, 0.34 * height},
		{1.00 * width, 0.00 * height},
	}
}

// 在二维 profile 中追加圆弧采样点。
// 用于生成:
// - 路缘石圆角
// - 曲面截面
// 参数:
// cx,cy:
//
//	圆心
//
// r:
//
//	半径
//
// start/end:
//
//	起止角度
//
// segments:
//
//	分段数量
func appendArcPoints(dst *[][2]float32, cx, cy, r float32, start, end float64, segments int, includeEnd bool) {
	for i := 1; i <= segments; i++ {
		if i == segments && !includeEnd {
			break
		}
		t := float64(i) / float64(segments)
		ang := start + (end-start)*t
		*dst = append(*dst, [2]float32{
			cx + r*float32(math.Cos(ang)),
			cy + r*float32(math.Sin(ang)),
		})
	}
}

// 删除二维 profile 中连续重复点。
// 避免 sweep mesh 时:
//   - 零长度边
//   - 重复三角形
//   - 法线异常
func dedupeProfilePoints(points [][2]float32) [][2]float32 {
	if len(points) == 0 {
		return nil
	}
	out := make([][2]float32, 0, len(points))
	out = append(out, points[0])
	for i := 1; i < len(points); i++ {
		if points[i] == out[len(out)-1] {
			continue
		}
		out = append(out, points[i])
	}
	return out
}

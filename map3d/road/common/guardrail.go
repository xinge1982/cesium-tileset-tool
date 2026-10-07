package common

import (
	"fmt"
	"math"
)

//双波 / 三波护栏 mesh 生成
//单面 / 双面面片生成
//支撑柱、连接件实例化
//UV 生成和旋转
//护栏断面 profile
//沿线采样生成立柱位置
//向量和几何辅助函数

const (
	defaultGuardrailPostSpacing            = float32(4.0)
	defaultGuardrailRailHeight             = float32(0.62)
	defaultGuardrailRailHeightAll          = float32(0.31)
	defaultGuardrailThreeWaveRailHeight    = float32(0.83)
	defaultGuardrailThreeWaveRailHeightAll = float32(0.41)
	defaultGuardrailWaveDepth              = float32(0.085)
	defaultGuardrailRailLengthUV           = float32(1.0)
	defaultGuardrailNoseEndTileMeters      = float32(0.2)
	defaultGuardrailPostWidth              = float32(0.14)
	defaultGuardrailPostDepth              = float32(0.14)
	defaultGuardrailPostHeight             = float32(1.10)
	defaultGuardrailSpacerDepth            = float32(0.14)
	defaultGuardrailSpacerHeight           = float32(0.15)
	defaultGuardrailSpacerWidth            = float32(0.09)
	defaultGuardrailRailClearance          = float32(0.02)
)

// 表示沿护栏线采样得到的一个点信息。
// Point 表示空间位置。
// Forward 表示护栏方向向量。
// Right 表示护栏右侧方向向量，用于计算偏移位置。
type sampledLinePoint struct {
	Point   [3]float32
	Forward [3]float32
	Right   [3]float32
}

// 表示一个实例化模型的变换信息。
// 用于生成护栏立柱、连接件等重复结构。
type instancedTransform struct {
	Translation [3]float32
	Rotation    [4]float32
	Scale       [3]float32
}

// 创建两波护栏网格模型。
//
// 根据输入的护栏线，生成护栏主体的顶点、法向量、UV以及三角索引。
// 函数内部会：
// 1. 删除连续重复点。
// 2. 根据方向计算护栏偏移方向。
// 3. 创建护栏平面几何。
//
// 参数：
// line        护栏中心线。
// direction   护栏方向。
// railHeight  护栏底部高度。
// railHeightAll 护栏波纹板高度。
//
// 返回：
// 顶点、法线、UV、索引以及可能的错误。
func buildGuardrailTwoWaveMesh(line LocalLine, direction int, railHeight, railHeightAll float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("guardrail line requires at least 2 distinct points")
	}

	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}

	return buildGuardrailRailPlane(points, sign, railHeight, railHeightAll),
		buildGuardrailRailPlaneNormals(points, sign),
		buildGuardrailRailPlaneUV(points),
		buildGuardrailRailPlaneIndices(points), nil
}

// 创建三波护栏网格模型。
//
// 功能与两波护栏生成方式一致，区别在于调用方传入不同的高度参数。
// 用于生成符合三波护栏尺寸规格的几何数据。
func buildGuardrailThreeWaveMesh(line LocalLine, direction int, railHeight, railHeightAll float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("guardrail line requires at least 2 distinct points")
	}

	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}

	return buildGuardrailRailPlane(points, sign, railHeight, railHeightAll),
		buildGuardrailRailPlaneNormals(points, sign),
		buildGuardrailRailPlaneUV(points),
		buildGuardrailRailPlaneIndices(points), nil
}

// 创建双面护栏平面模型。
//
// 在单面护栏面的基础上复制一份反向面，生成双面可见模型。
// 主要用于避免从背面观察护栏时由于背面剔除导致不可见。
//
// 参数：
// tiledUV 是否使用按照实际长度平铺的UV。
func buildGuardrailDoubleSidedPlaneMesh(line LocalLine, direction int, railHeight, railHeightAll float32, tiledUV bool) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("guardrail line requires at least 2 distinct points")
	}

	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}

	frontPos := buildGuardrailRailPlane(points, sign, railHeight, railHeightAll)
	frontNormals := buildGuardrailRailPlaneNormals(points, sign)
	frontUV := buildGuardrailRailPlaneUV(points)
	if tiledUV {
		frontUV = buildGuardrailRailPlaneUVTiled(points, railHeightAll)
	}
	frontIdx := buildGuardrailRailPlaneIndices(points)

	pos := make([][3]float32, 0, len(frontPos)*2)
	normals := make([][3]float32, 0, len(frontNormals)*2)
	uv := make([][2]float32, 0, len(frontUV)*2)
	indices := make([]uint32, 0, len(frontIdx)*2)

	pos = append(pos, frontPos...)
	normals = append(normals, frontNormals...)
	uv = append(uv, frontUV...)
	indices = append(indices, frontIdx...)

	backBase := uint32(len(pos))
	pos = append(pos, frontPos...)
	for _, n := range frontNormals {
		normals = append(normals, [3]float32{-n[0], -n[1], -n[2]})
	}
	uv = append(uv, frontUV...)
	for i := 0; i+2 < len(frontIdx); i += 3 {
		a := backBase + frontIdx[i]
		b := backBase + frontIdx[i+1]
		c := backBase + frontIdx[i+2]
		indices = append(indices, a, c, b)
	}

	return pos, normals, uv, indices, nil
}

// 创建单面护栏平面模型。
//
// 根据护栏中心线生成单面网格。
// 支持：
// 1. 长度方向UV平铺。
// 2. UV旋转。
// 3. 自定义护栏高度。
//
// 返回生成后的几何数据。
func buildGuardrailSingleSidedPlaneMesh(line LocalLine, direction int, railHeight, railHeightAll float32, tiledUV bool, uvRotateDeg float64) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("guardrail line requires at least 2 distinct points")
	}

	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}

	pos := buildGuardrailRailPlane(points, sign, railHeight, railHeightAll)
	normals := buildGuardrailRailPlaneNormals(points, sign)
	uv := buildGuardrailRailPlaneUV(points)
	if tiledUV {
		uv = buildGuardrailRailPlaneUVTiled(points, railHeightAll)
	}
	if math.Abs(uvRotateDeg) > 1e-6 {
		rotateUVs(uv, uvRotateDeg)
	}
	indices := buildGuardrailRailPlaneIndices(points)
	return pos, normals, uv, indices, nil
}

// 创建双面两波护栏平面模型。
//
// 使用默认两波护栏尺寸参数生成护栏网格。
// 生成结果包含正反两个方向的面，适用于需要双面显示的场景。
func buildGuardrailTwoWaveDoubleSidedPlaneMesh(line LocalLine, direction int) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	return buildGuardrailDoubleSidedPlaneMesh(line, direction, defaultGuardrailRailHeight, defaultGuardrailRailHeightAll, false)
}

// 创建双面三波护栏平面模型。
//
// 使用三波护栏默认高度参数生成双面护栏网格。
// 适用于高速公路中央隔离带、防撞护栏等需要双侧显示的模型。
func buildGuardrailThreeWaveDoubleSidedPlaneMesh(line LocalLine, direction int) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	return buildGuardrailDoubleSidedPlaneMesh(line, direction, defaultGuardrailThreeWaveRailHeight, defaultGuardrailThreeWaveRailHeightAll, false)
}

// 创建支持UV平铺的双面三波护栏模型。
//
// 与普通三波双面护栏相比，该函数使用真实长度比例生成UV。
// 当护栏长度较长时，可以避免纹理被整体拉伸。
func buildGuardrailThreeWaveDoubleSidedPlaneMeshTiled(line LocalLine, direction int) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	return buildGuardrailDoubleSidedPlaneMesh(line, direction, defaultGuardrailThreeWaveRailHeight, defaultGuardrailThreeWaveRailHeightAll, true)
}

// 创建单面平铺UV三波护栏模型。
//
// 生成单面三波护栏网格，并支持：
// 1. 根据实际长度平铺纹理。
// 2. 对UV进行旋转调整。
//
// 常用于需要优化护栏纹理方向的场景。
func buildGuardrailThreeWaveSingleSidedPlaneMeshTiled(line LocalLine, direction int, uvRotateDeg float64) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	return buildGuardrailSingleSidedPlaneMesh(line, direction, defaultGuardrailThreeWaveRailHeight, defaultGuardrailThreeWaveRailHeightAll, true, uvRotateDeg)
}

// 根据要素属性获取护栏高度参数。
//
// 会按照优先级从 FeatureFields 中读取护栏高度：
//
// 底部高度读取顺序：
// 1. rail_height
// 2. board_base_height
// 3. board_bottom_height
// 4. 默认值
//
// 面板高度读取顺序：
// 1. rail_height_all
// 2. board_height
// 3. rail_panel_height
// 4. 默认值
//
// 用于兼容不同数据源字段定义。
func guardrailRailHeights(fields FeatureFields, defaultBase, defaultPanel float32) (float32, float32) {
	base := featureFieldFloat32(fields, "rail_height")
	if base <= 0 {
		base = featureFieldFloat32(fields, "board_base_height")
	}
	if base <= 0 {
		base = featureFieldFloat32(fields, "board_bottom_height")
	}
	if base <= 0 {
		base = defaultBase
	}

	panel := featureFieldFloat32(fields, "rail_height_all")
	if panel <= 0 {
		panel = featureFieldFloat32(fields, "board_height")
	}
	if panel <= 0 {
		panel = featureFieldFloat32(fields, "rail_panel_height")
	}
	if panel <= 0 {
		panel = defaultPanel
	}
	return base, panel
}

// 生成护栏实例化组件的位置和姿态。
//
// 根据护栏中心线进行采样，计算：
// 1. 立柱（post）的位置、旋转和缩放。
// 2. 连接件（spacer）的位置、旋转和缩放。
//
// 返回的数据可以直接用于实例化渲染，避免重复生成大量几何数据。
//
// 参数：
// line             护栏中心线。
// direction        护栏安装方向。
// railHeight       护栏主体高度。
// railHeightAll    护栏板高度。
func guardrailInstanceTransforms(line LocalLine, direction int, railHeight, railHeightAll float32) ([]instancedTransform, []instancedTransform, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, fmt.Errorf("guardrail line requires at least 2 distinct points")
	}
	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}
	samples := samplePolylineForPosts(points, defaultGuardrailPostSpacing)
	posts := make([]instancedTransform, 0, len(samples))
	spacers := make([]instancedTransform, 0, len(samples))
	for _, s := range samples {
		postCenter := s.Point
		postOffset := sign * (defaultGuardrailRailClearance + defaultGuardrailSpacerDepth + defaultGuardrailPostWidth/2)
		postCenter[0] += s.Right[0] * postOffset
		postCenter[1] += defaultGuardrailPostHeight / 2
		postCenter[2] += s.Right[2] * postOffset
		rot := quaternionFromForward(s.Forward)
		posts = append(posts, instancedTransform{
			Translation: postCenter,
			Rotation:    rot,
			Scale:       [3]float32{defaultGuardrailPostWidth, defaultGuardrailPostHeight, defaultGuardrailPostDepth},
		})

		spacerCenter := s.Point
		spacerOffset := sign * (defaultGuardrailRailClearance + defaultGuardrailSpacerDepth/2)
		spacerCenter[0] += s.Right[0] * spacerOffset
		spacerCenter[1] += railHeight + railHeightAll/2
		spacerCenter[2] += s.Right[2] * spacerOffset
		spacers = append(spacers, instancedTransform{
			Translation: spacerCenter,
			Rotation:    rot,
			Scale:       [3]float32{defaultGuardrailSpacerDepth, defaultGuardrailSpacerHeight, defaultGuardrailSpacerWidth},
		})
	}
	return posts, spacers, nil
}

// 生成护栏支撑结构实例变换。
//
// 根据护栏线采样点生成支撑结构的位置和方向。
// 返回的数据用于实例化加载固定支撑模型。
func guardrailSupportTransforms(line LocalLine) ([]instancedTransform, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, fmt.Errorf("guardrail line requires at least 2 distinct points")
	}
	samples := samplePolylineForPosts(points, defaultGuardrailPostSpacing)
	out := make([]instancedTransform, 0, len(samples))
	for _, s := range samples {
		out = append(out, instancedTransform{
			Translation: s.Point,
			Rotation:    quaternionFromForward(s.Forward),
			Scale:       [3]float32{1, 1, 1},
		})
	}
	return out, nil
}

// 创建护栏支撑结构组合网格。
//
// 将护栏立柱和连接件组合成一个局部模型。
// 与实例化方式不同，该函数直接生成完整几何数据。
//
// 参数：
// direction       护栏方向。
// railHeight      护栏主体高度。
// railHeightAll   护栏面板高度。
//
// 返回：
// 顶点、法线、UV以及索引数据。
func buildGuardrailSupportMesh(direction int, railHeight, railHeightAll float32) ([][3]float32, [][3]float32, [][2]float32, []uint32) {
	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}
	// Combined support mesh is rotated as one instanced object; local X needs
	// to match the previous world-space right-offset direction.
	sign = -sign
	postOffset := sign * (defaultGuardrailRailClearance + defaultGuardrailSpacerDepth + defaultGuardrailPostWidth/2)
	spacerOffset := sign * (defaultGuardrailRailClearance + defaultGuardrailSpacerDepth/2)

	var pos [][3]float32
	var normals [][3]float32
	var uv [][2]float32
	var indices []uint32

	appendCylinderGeometry(&pos, &normals, &uv, &indices,
		[3]float32{postOffset, defaultGuardrailPostHeight / 2, 0},
		defaultGuardrailPostWidth, defaultGuardrailPostHeight, defaultGuardrailPostDepth, 12)
	appendBoxGeometry(&pos, &normals, &uv, &indices,
		[3]float32{spacerOffset, railHeight + railHeightAll/2, 0},
		defaultGuardrailSpacerDepth, defaultGuardrailSpacerHeight, defaultGuardrailSpacerWidth)
	return pos, normals, uv, indices
}

// 根据属性字段获取护栏方向。
//
// 将数据中的方向编码转换为内部使用的方向值。
// 当前规则：
// direction=1 转换为 2。
// direction=2 转换为 1。
// 其他情况默认返回 1
func guardrailDirection(fields FeatureFields) int {
	v := int64FromFields(fields, "direction")
	if v == 1 {
		return 2
	}
	if v == 2 {
		return 1
	}
	return 1
}

// 获取二维护栏断面的最大X坐标。
//
// 用于计算护栏profile在横向方向上的最大宽度。
//
// 参数：
// profile 二维截面轮廓。
//
// 返回：
// profile所有点中的最大X值。
func profileMaxX(profile Profile2D) float32 {
	var maxX float32
	for i, p := range profile.Points {
		if i == 0 || p[0] > maxX {
			maxX = p[0]
		}
	}
	return maxX
}

// 根据方向向量生成旋转四元数。
//
// 根据水平面上的forward方向计算绕Y轴旋转角度。
// 用于将护栏立柱、实例模型旋转到线路方向。
//
// 参数：
// forward 水平方向向量。
//
// 返回：
// 表示绕Y轴旋转的四元数。
func quaternionFromForward(forward [3]float32) [4]float32 {
	heading := math.Atan2(float64(forward[0]), float64(forward[2]))
	s := float32(math.Sin(heading / 2))
	c := float32(math.Cos(heading / 2))
	return [4]float32{0, s, 0, c}
}

// 创建两波护栏二维截面轮廓。
//
// 根据护栏深度和高度生成波浪形截面。
// 该Profile可用于后续挤压生成三维护栏模型。
//
// 参数：
// depth  护栏波纹深度。
// height 护栏整体高度。
//
// 返回：
// 二维护栏截面Profile。
func buildTwoWaveGuardrailProfile(depth, height float32) Profile2D {
	points := [][2]float32{
		{0, 0},
		{depth, height * 0.18},
		{0, height * 0.40},
		{depth, height * 0.62},
		{0, height * 0.84},
		{depth * 0.65, height},
	}
	return Profile2D{
		Name:   "guardrail-two-wave",
		Points: points,
		Closed: false,
	}
}

// 根据护栏线路生成护栏平面顶点。
//
// 每个线路点生成两个顶点：
// 1. 护栏底部点。
// 2. 护栏顶部点。
//
// 通过这些点连接形成连续护栏面。
//
// 参数：
// points        护栏线路点。
// sign          偏移方向（当前函数保留该参数用于接口统一）。
// railHeight    护栏底部高度。
// railHeightAll 护栏板高度。
//
// 返回：
// 护栏面的顶点列表。
func buildGuardrailRailPlane(points [][3]float32, sign float32, railHeight, railHeightAll float32) [][3]float32 {
	pos := make([][3]float32, 0, len(points)*2)
	for i := range points {
		base := [3]float32{
			points[i][0],
			points[i][1] + railHeight,
			points[i][2],
		}
		top := [3]float32{
			points[i][0],
			points[i][1] + railHeight + railHeightAll,
			points[i][2],
		}
		_ = sign
		pos = append(pos, base, top)
	}
	return pos
}

// 计算护栏平面法线。
//
// 根据线路方向计算每个顶点对应的法线方向。
// 法线方向由护栏安装方向sign控制。
//
// 参数：
// points 护栏线路点。
// sign   法线方向控制参数。
//
// 返回：
// 每个顶点对应的法线数组。
func buildGuardrailRailPlaneNormals(points [][3]float32, sign float32) [][3]float32 {
	normals := make([][3]float32, 0, len(points)*2)
	for i := range points {
		right := guardrailRightAt(points, i)
		n := scale3(right, sign)
		normals = append(normals, n, n)
	}
	return normals
}

// 创建护栏平面UV坐标。
//
// U方向沿护栏长度方向递增。
// V方向固定映射到完整纹理高度。
//
// 适用于纹理不需要按照真实尺寸平铺的情况。
func buildGuardrailRailPlaneUV(points [][3]float32) [][2]float32 {
	uv := make([][2]float32, 0, len(points)*2)
	cum := polylineCumLengthsF32(points)
	for i := range points {
		u := float32(0)
		if len(cum) > 0 {
			u = cum[i] / defaultGuardrailRailLengthUV
		}
		// U 沿护栏长度方向平铺；V 在板高方向固定拉伸到整张贴图。
		uv = append(uv, [2]float32{u, 1}, [2]float32{u, 0})
	}
	return uv
}

// 创建真实尺寸比例的护栏UV。
//
// 根据护栏实际长度和高度计算UV比例。
// 用于长距离护栏模型，避免纹理被拉伸。
//
// 参数：
// points        护栏线路点。
// railHeightAll 护栏纹理区域高度。
//
// 返回：
// 平铺后的UV坐标。
func buildGuardrailRailPlaneUVTiled(points [][3]float32, railHeightAll float32) [][2]float32 {
	uv := make([][2]float32, 0, len(points)*2)
	cum := polylineCumLengthsF32(points)
	vTop := railHeightAll / defaultGuardrailNoseEndTileMeters
	for i := range points {
		u := float32(0)
		if len(cum) > 0 {
			u = cum[i] / defaultGuardrailNoseEndTileMeters
		}
		uv = append(uv, [2]float32{u, vTop}, [2]float32{u, 0})
	}
	return uv
}

// 绕UV中心点旋转纹理坐标。
//
// 用于调整护栏纹理方向。
// 旋转中心固定为UV空间中的(0.5,0.5)。
//
// 参数：
// uv   UV坐标数组。
// deg 旋转角度（度）。
func rotateUVs(uv [][2]float32, deg float64) {
	if len(uv) == 0 {
		return
	}
	rad := deg * math.Pi / 180
	c := float32(math.Cos(rad))
	s := float32(math.Sin(rad))
	for i := range uv {
		u := uv[i][0] - 0.5
		v := uv[i][1] - 0.5
		uv[i][0] = u*c - v*s + 0.5
		uv[i][1] = u*s + v*c + 0.5
	}
}

// 创建护栏平面三角索引。
//
// 根据护栏顶点排列方式，将相邻两个线路点之间连接成两个三角面。
// 每个线段生成两个三角形。
//
// 参数：
// points 护栏线路点。
//
// 返回：
// 三角面索引列表。
func buildGuardrailRailPlaneIndices(points [][3]float32) []uint32 {
	if len(points) < 2 {
		return nil
	}
	indices := make([]uint32, 0, (len(points)-1)*6)
	for i := 0; i < len(points)-1; i++ {
		a := uint32(i * 2)
		b := a + 1
		c := a + 2
		d := a + 3
		indices = append(indices, a, c, b, b, c, d)
	}
	return indices
}

// 沿护栏线路按照固定间距采样支柱位置。
//
// 用于确定护栏立柱、连接件等重复结构的位置。
// 会保证线路终点也包含在采样结果中。
//
// 参数：
// points  护栏线路点。
// spacing 采样间距。
//
// 返回：
// 包含位置、方向信息的采样点数组。
func samplePolylineForPosts(points [][3]float32, spacing float32) []sampledLinePoint {
	if len(points) < 2 || spacing <= 0 {
		return nil
	}
	cum := polylineCumLengthsF32(points)
	total := cum[len(cum)-1]
	count := int(math.Floor(float64(total/spacing))) + 1
	samples := make([]sampledLinePoint, 0, count+1)
	for dist := float32(0); dist <= total; dist += spacing {
		samples = append(samples, samplePolylineAt(points, cum, dist))
	}
	if len(samples) == 0 || segmentLengthXZ(samples[len(samples)-1].Point, points[len(points)-1]) > 0.01 {
		samples = append(samples, samplePolylineAt(points, cum, total))
	}
	return dedupeSampledPosts(samples)
}

// 删除重复的采样点。
//
// 当线路长度较短或者采样间距与线路节点重合时，
// 可能产生位置重复的采样点。
//
// 返回去重后的采样结果。
func dedupeSampledPosts(samples []sampledLinePoint) []sampledLinePoint {
	if len(samples) == 0 {
		return nil
	}
	out := make([]sampledLinePoint, 0, len(samples))
	out = append(out, samples[0])
	for i := 1; i < len(samples); i++ {
		if segmentLengthXZ(samples[i-1].Point, samples[i].Point) < 0.01 {
			continue
		}
		out = append(out, samples[i])
	}
	return out
}

// 根据距离在线上进行插值采样。
//
// 根据累计长度数组找到对应线段，
// 然后计算该位置的空间坐标和方向。
//
// 参数：
// points 折线点。
// cum    累计长度数组。
// dist   距离起点的长度。
//
// 返回：
// 指定距离处的采样点信息。
func samplePolylineAt(points [][3]float32, cum []float32, dist float32) sampledLinePoint {
	if dist <= 0 {
		return buildSampledLinePoint(points, 0, 0)
	}
	total := cum[len(cum)-1]
	if dist >= total {
		return buildSampledLinePoint(points, len(points)-2, 1)
	}
	for i := 0; i < len(cum)-1; i++ {
		if dist <= cum[i+1] {
			segLen := cum[i+1] - cum[i]
			t := float32(0)
			if segLen > 0 {
				t = (dist - cum[i]) / segLen
			}
			return buildSampledLinePoint(points, i, t)
		}
	}
	return buildSampledLinePoint(points, len(points)-2, 1)
}

// 根据线段和插值比例生成采样点。
//
// 计算：
// 1. 空间位置。
// 2. 前进方向。
// 3. 右侧方向。
//
// 用于后续计算护栏组件的位置和旋转。
func buildSampledLinePoint(points [][3]float32, seg int, t float32) sampledLinePoint {
	if seg < 0 {
		seg = 0
	}
	if seg >= len(points)-1 {
		seg = len(points) - 2
	}
	p := interpolatePoint3(points[seg], points[seg+1], t)
	forward := normalize3([3]float32{
		points[seg+1][0] - points[seg][0],
		0,
		points[seg+1][2] - points[seg][2],
	})
	right := [3]float32{-forward[2], 0, forward[0]}
	return sampledLinePoint{Point: p, Forward: forward, Right: right}
}

// 获取护栏线路某个节点处的右方向向量。
//
// 根据相邻线段方向计算平滑连接处的法线方向。
// 用于生成护栏面的法线和偏移位置。
func guardrailRightAt(points [][3]float32, idx int) [3]float32 {
	segNormals := buildSegmentNormals(points)
	nx, nz := lineJoinNormal(points, segNormals, idx)
	return [3]float32{float32(nx), 0, float32(nz)}
}

// 添加带方向旋转的盒状网格。
//
// 根据中心点、方向向量以及尺寸生成一个盒子模型。
// 支持指定UV，并自动生成六个面的顶点、法线和索引。
//
// 用于生成护栏立柱、连接件等规则几何。
func appendOrientedBoxMesh(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, center, right, forward [3]float32, width, depth, height float32, u0, v0, u1, v1 float32) {
	halfW := width / 2
	halfD := depth / 2
	halfH := height / 2

	up := [3]float32{0, 1, 0}
	rt := scale3(right, halfW)
	fw := scale3(forward, halfD)
	uh := scale3(up, halfH)

	p000 := sub3(sub3(sub3(center, rt), fw), uh)
	p001 := add3(sub3(sub3(center, rt), fw), uh)
	p010 := sub3(add3(sub3(center, rt), fw), uh)
	p011 := add3(add3(sub3(center, rt), fw), uh)
	p100 := sub3(sub3(add3(center, rt), fw), uh)
	p101 := add3(sub3(add3(center, rt), fw), uh)
	p110 := sub3(add3(add3(center, rt), fw), uh)
	p111 := add3(add3(add3(center, rt), fw), uh)

	appendQuadFacing(pos, normals, uv, indices, p001, p101, p111, p011, up, [2]float32{u0, v0}, [2]float32{u1, v0}, [2]float32{u1, v1}, [2]float32{u0, v1})
	appendQuadFacing(pos, normals, uv, indices, p000, p010, p110, p100, [3]float32{0, -1, 0}, [2]float32{u0, v0}, [2]float32{u1, v0}, [2]float32{u1, v1}, [2]float32{u0, v1})
	appendQuadFacing(pos, normals, uv, indices, p000, p001, p011, p010, scale3(right, -1), [2]float32{u0, v0}, [2]float32{u1, v0}, [2]float32{u1, v1}, [2]float32{u0, v1})
	appendQuadFacing(pos, normals, uv, indices, p100, p110, p111, p101, right, [2]float32{u0, v0}, [2]float32{u1, v0}, [2]float32{u1, v1}, [2]float32{u0, v1})
	appendQuadFacing(pos, normals, uv, indices, p000, p100, p101, p001, scale3(forward, -1), [2]float32{u0, v0}, [2]float32{u1, v0}, [2]float32{u1, v1}, [2]float32{u0, v1})
	appendQuadFacing(pos, normals, uv, indices, p010, p011, p111, p110, forward, [2]float32{u0, v0}, [2]float32{u1, v0}, [2]float32{u1, v1}, [2]float32{u0, v1})
}

// 对两个三维向量进行逐分量相加。
func add3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

// 对两个三维向量进行逐分量相减。
func sub3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

// 对三维向量进行比例缩放。
//
// 返回 vector * scale。
func scale3(a [3]float32, s float32) [3]float32 {
	return [3]float32{a[0] * s, a[1] * s, a[2] * s}
}

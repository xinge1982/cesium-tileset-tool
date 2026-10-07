package common

import (
	"math"
	"os"
	"path/filepath"
	"runtime"

	"cesium-tileset-tool/map3d/road/cdt-go/cdt"
)

// 使用 CDT (Constrained Delaunay Triangulation)
// 对 Surface 面进行三角剖分。
//
// 该函数是 CDT 三角化入口，负责:
//
//  1. 尝试直接使用输入 LocalRings 进行 CDT
//  2. 如果失败:
//     对输入 ring 进行轻量清理
//  3. 再次尝试 CDT
//  4. 最后使用更强清理策略后重试
//
// 设计目的:
//
// 在道路面、地形面等复杂 Polygon 三角化过程中，
// 尽可能保留原始边界点，避免普通清理算法改变几何形状。
//
// 输入:
//
// ring:
//
//	LocalRings
//
//	包含:
//
//	  Outer:
//	    外边界点
//
//	  Holes:
//	    内洞区域
//
// 输出:
//
// pos:
//
//	三维 Mesh 顶点:
//
//	  [X,Y,Z]
//
// idx:
//
//	三角面索引
//
// ok:
//
//	是否 CDT 成功生成结果
//
// error:
//
//	三角化过程错误
//
// 注意:
//
// 当前 CDT 实现优先处理无 Hole 的 polygon。
// 对包含 Hole 的情况会返回失败并交给其他流程处理。
func triangulateSurfaceByCDTExperimental(ring LocalRings) ([][3]float32, []uint32, bool, error) {
	pos, idx, ok, err := triangulateSurfaceByCDTExperimentalSingle(ring)
	if ok || err == nil {
		return pos, idx, ok, err
	}
	// CDT fallback should be conservative: keep edge corner points whenever possible.
	// 第一次失败:
	// 进行轻量 ring 清理
	//
	// 保留边界关键点，
	// 删除过近或重复点。
	clean := cleanSubgradeLocalRings(ring, 0.002)
	pos, idx, ok, err = triangulateSurfaceByCDTExperimentalSingle(clean)
	if ok || err == nil {
		return pos, idx, ok, err
	}
	// 第二次失败:
	// 使用更严格 CDT 专用清理
	clean = cleanCDTExperimentalRing(ring)
	return triangulateSurfaceByCDTExperimentalSingle(clean)
}

// 执行一次完整 CDT 三角剖分。
//
// 主要流程:
//
//  1. 检查输入 Polygon 是否支持 CDT
//  2. 清理闭合 ring
//  3. 将三维点转换为二维 CDT 输入
//  4. 创建约束边
//  5. 调用 CDT 库进行三角剖分
//  6. 将二维结果恢复为三维 Mesh
//
// CDT 处理空间:
//
// 输入:
//
//	LocalRings:
//
//	    X,Y,Z
//
// CDT:
//
//	X,Z
//
// 输出:
//
//	X,Y,Z
//
// 说明:
//
// CDT 只关心水平投影平面。
// 高程 Y 不参与三角化，
// 后续通过 resolveCDTPointHeight 恢复。
//
// 返回:
// pos:
//
//	三维顶点列表
//
// indices:
//
//	三角索引
//
// ok:
//
//	是否成功
func triangulateSurfaceByCDTExperimentalSingle(ring LocalRings) ([][3]float32, []uint32, bool, error) {
	if len(ring.Holes) > 0 {
		return nil, nil, false, nil
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 3 {
		return nil, nil, false, nil
	}

	// CDT 输入二维点
	//
	// 使用 XZ 平面:
	//
	// X:
	//   水平方向
	//
	// Z:
	//   纵向
	//
	points := make([]cdt.Point2, 0, len(outer))

	// 保存三维原始点
	//
	// 后续恢复高度使用
	pos3 := make([][3]float32, 0, len(outer))
	for _, p := range outer {
		points = append(points, cdt.Point2{X: float64(p[0]), Y: float64(p[2])})
		pos3 = append(pos3, p)
	}

	// 创建约束边
	//
	// 保证 CDT 不破坏原始 polygon 边界
	edges := make([]cdt.Edge, 0, len(outer))
	for i := range outer {
		j := (i + 1) % len(outer)
		edges = append(edges, cdt.Edge{A: int32(i), B: int32(j)})
	}

	restore, err := chdirCDTWorkdir()
	if err != nil {
		return nil, nil, false, err
	}
	defer restore()
	defer cdt.Close()

	outPoints, outTriangles, err := cdt.TriangulateInside(points, edges)
	if err != nil {
		return nil, nil, false, err
	}
	if len(outPoints) == 0 || len(outTriangles) == 0 {
		return nil, nil, false, nil
	}

	// 恢复三维顶点
	pos := make([][3]float32, 0, len(outPoints))
	for _, p := range outPoints {
		y := resolveCDTPointHeight(pos3, float32(p.X), float32(p.Y))
		pos = append(pos, [3]float32{float32(p.X), y, float32(p.Y)})
	}
	// 转换三角索引
	indices := make([]uint32, 0, len(outTriangles)*3)
	for _, tri := range outTriangles {
		indices = append(indices, uint32(tri.A), uint32(tri.B), uint32(tri.C))
	}
	return pos, indices, true, nil
}

// 根据 CDT 输出的二维点坐标，恢复对应的三维高度 Y。
//
// CDT 三角化过程只使用:
//
//	X,Z 平面
//
// 不参与:
//
//	Y 高程
//
// 因此 CDT 新生成的内部点需要重新计算高度。
//
// 恢复策略:
//
// 1. 优先检查是否命中原始顶点:
//
//	距离 <= pointTol
//
//	直接使用原始顶点高度
//
// 2. 如果不是顶点:
//
//	查找最近边段
//
//	将点投影到边段
//
//	根据边段两个端点高度线性插值
//
// 3. 如果距离边界较远:
//
//	使用最近顶点高度作为 fallback
//
// 参数:
//
// outer:
//
//	原始三维边界点
//
// x,z:
//
//	CDT 输出二维点坐标
//
// 返回:
//
//	对应的 Y 高程
func resolveCDTPointHeight(outer [][3]float32, x, z float32) float32 {
	if len(outer) == 0 {
		return 0
	}
	const pointTol = 0.01
	const segTol = 0.05

	bestVertexDist := math.MaxFloat64
	bestVertexY := outer[0][1]
	// ----------------------------------
	// 第一阶段:
	// 查找最近原始顶点
	// ----------------------------------
	for _, p := range outer {
		dx := float64(p[0] - x)
		dz := float64(p[2] - z)
		d2 := dx*dx + dz*dz
		if d2 <= pointTol*pointTol {
			return p[1]
		}
		if d2 < bestVertexDist {
			bestVertexDist = d2
			bestVertexY = p[1]
		}
	}

	bestSegDist := math.MaxFloat64
	bestSegY := bestVertexY
	// ----------------------------------
	// 第二阶段:
	// 查找最近边段
	// ----------------------------------
	for i := range outer {
		a := outer[i]
		b := outer[(i+1)%len(outer)]
		t, d2 := projectPointToSegmentXZ(a, b, x, z)
		if d2 < bestSegDist {
			bestSegDist = d2
			// 根据在线段上的比例
			// 插值高度
			bestSegY = a[1] + (b[1]-a[1])*float32(t)
		}
	}
	// 点足够接近边界
	if bestSegDist <= segTol*segTol {
		return bestSegY
	}
	// 使用最近顶点高度
	return bestVertexY
}

// 将一个二维点投影到 XZ 平面的线段上。
// 用于:
//
//  1. 查找 CDT 新生成点距离哪个边界最近
//
//  2. 根据边界线段恢复高度
//
// 输入:
//
// a,b:
//
//	三维线段两个端点
//
// x,z:
//
//	待投影二维点
//
// 输出:
//
// t:
//
//	投影在线段上的比例
//
//	0:
//	   起点
//
//	1:
//	   终点
//
// d2:
//
//	投影点到目标点的平方距离
func projectPointToSegmentXZ(a, b [3]float32, x, z float32) (float64, float64) {
	ax := float64(a[0])
	az := float64(a[2])
	bx := float64(b[0])
	bz := float64(b[2])
	px := float64(x)
	pz := float64(z)

	// 线段方向
	vx := bx - ax
	vz := bz - az
	// 点到起点向量
	wx := px - ax
	wz := pz - az
	// 线段长度平方
	den := vx*vx + vz*vz
	// 退化线段
	if den <= 1e-9 {
		dx := px - ax
		dz := pz - az
		return 0, dx*dx + dz*dz
	}
	// 投影比例
	t := (wx*vx + wz*vz) / den
	// 限制在线段范围
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	// 投影点
	cx := ax + t*vx
	cz := az + t*vz
	dx := px - cx
	dz := pz - cz
	return t, dx*dx + dz*dz
}

// CDT 三角化测试接口。
//
// 将内部实现:
//
//	triangulateSurfaceByCDTExperimental
//
// 暴露给测试代码调用。
//
// 主要用途:
//
//   - 单元测试
//   - CDT 算法验证
//   - 不经过完整 Mesh pipeline 测试三角化结果
func TriangulateSurfaceByCDTExperimentalForTest(ring LocalRings) ([][3]float32, []uint32, bool, error) {
	return triangulateSurfaceByCDTExperimental(ring)
}

// chdirCDTWorkdir
//
// 切换 CDT 库运行目录。
//
// 某些 CDT 实现可能依赖:
//
//   - 相对路径资源
//   - 本地配置文件
//   - 动态库加载路径
//
// 因此在调用 CDT 前:
//
//  1. 保存当前工作目录
//  2. 切换到 cdt-go/cdt 目录
//
// 返回:
//
// restore:
//
//	用于恢复原工作目录的函数
//
// error:
//
//	切换失败原因
func chdirCDTWorkdir() (func(), error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, os.ErrInvalid
	}
	target := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "cdt-go", "cdt"))
	if err := os.Chdir(target); err != nil {
		return nil, err
	}
	return func() {
		_ = os.Chdir(wd)
	}, nil
}

// 对 CDT 输入的 LocalRings 做专用清理。
//
// 目的:
//
// 避免 CDT 因为以下问题失败:
//
//   - 重复点
//   - 极短边
//   - 几何噪声
//   - 接近共线点
//
// 处理:
//
// Outer:
//
//	进行外边界清理
//
// Holes:
//
//	对每个内部洞进行清理
//
// 返回:
//
// 清理后的 LocalRings
func cleanCDTExperimentalRing(ring LocalRings) LocalRings {
	out := LocalRings{
		Outer: sanitizeSubgradeRing(ring.Outer, 0.002, 0.005, true),
		Holes: make([][][3]float32, 0, len(ring.Holes)),
	}
	for _, hole := range ring.Holes {
		out.Holes = append(out.Holes, sanitizeSubgradeRing(hole, 0.002, 0.005, false))
	}
	return out
}

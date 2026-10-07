package common

import (
	"math"
	"sort"

	"github.com/sirupsen/logrus"
)

// 道路表面投影模块

type roadTriangle struct {
	roadID     int64
	a, b, c    [3]float32
	minX, maxX float32
	minZ, maxZ float32
}

type roadTriangleGridIndex struct {
	cellSize float32
	buckets  map[int64][]int
}

// 创建道路三角形空间网格索引。
// 使用固定大小的二维网格(cell)将大量道路三角形进行空间划分，
// 用于后续快速查询某个点或区域附近可能相关的三角形，避免遍历全部道路三角面。
// 当输入 cellSize 非法时，默认使用 30 米网格大小。
func newRoadTriangleGridIndex(cellSize float32) *roadTriangleGridIndex {
	if cellSize <= 0 {
		cellSize = 30.0
	}
	return &roadTriangleGridIndex{
		cellSize: cellSize,
		buckets:  make(map[int64][]int, 1024),
	}
}

// 向空间索引中加入一个道路三角形。
// 根据三角形的 XZ 平面包围盒计算覆盖的网格范围，
// 并将三角形索引写入对应 bucket。
// 一个三角形可能会被加入多个网格单元中，以支持快速空间查询。
func (g *roadTriangleGridIndex) add(idx int, tri roadTriangle) {
	if g == nil {
		return
	}
	minCellX := int(math.Floor(float64(tri.minX / g.cellSize)))
	maxCellX := int(math.Floor(float64(tri.maxX / g.cellSize)))
	minCellZ := int(math.Floor(float64(tri.minZ / g.cellSize)))
	maxCellZ := int(math.Floor(float64(tri.maxZ / g.cellSize)))
	for x := minCellX; x <= maxCellX; x++ {
		for z := minCellZ; z <= maxCellZ; z++ {
			key := packGridCell(x, z)
			g.buckets[key] = append(g.buckets[key], idx)
		}
	}
}

// 查询包含指定点附近的候选三角形索引。
// 根据点所在网格，搜索周围 3x3 网格区域，
// 返回可能包含该点的道路三角形编号。
// 使用 seen 去重，避免同一个三角形重复返回。
func (g *roadTriangleGridIndex) query(pt [3]float32) []int {
	if g == nil || len(g.buckets) == 0 {
		return nil
	}
	baseX := int(math.Floor(float64(pt[0] / g.cellSize)))
	baseZ := int(math.Floor(float64(pt[2] / g.cellSize)))
	seen := make(map[int]struct{}, 32)
	out := make([]int, 0, 32)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			key := packGridCell(baseX+dx, baseZ+dz)
			for _, idx := range g.buckets[key] {
				if _, ok := seen[idx]; ok {
					continue
				}
				seen[idx] = struct{}{}
				out = append(out, idx)
			}
		}
	}
	return out
}

// 根据一个 XZ 平面范围查询可能相交的三角形。
// 根据输入 min/max X、Z 计算覆盖网格，
// 返回所有可能位于该区域内的三角形索引。
// 主要用于线段穿越道路 Mesh 时快速筛选候选三角形。
func (g *roadTriangleGridIndex) queryBounds(minX, maxX, minZ, maxZ float32) []int {
	if g == nil || len(g.buckets) == 0 {
		return nil
	}
	minCellX := int(math.Floor(float64(minX / g.cellSize)))
	maxCellX := int(math.Floor(float64(maxX / g.cellSize)))
	minCellZ := int(math.Floor(float64(minZ / g.cellSize)))
	maxCellZ := int(math.Floor(float64(maxZ / g.cellSize)))
	seen := make(map[int]struct{}, 64)
	out := make([]int, 0, 64)
	for x := minCellX; x <= maxCellX; x++ {
		for z := minCellZ; z <= maxCellZ; z++ {
			key := packGridCell(x, z)
			for _, idx := range g.buckets[key] {
				if _, ok := seen[idx]; ok {
					continue
				}
				seen[idx] = struct{}{}
				out = append(out, idx)
			}
		}
	}
	return out
}

// 将二维网格坐标(x,z)压缩成 int64 key。
// 用于作为 map 的 key 存储空间索引。
// 高 32 位保存 X，低 32 位保存 Z。
func packGridCell(x, z int) int64 {
	return (int64(x) << 32) | (int64(uint32(z)) & 0xffffffff)
}

type RoadSurfaceProjector struct {
	byRoad            map[int64][]roadTriangle
	byRoadIndex       map[int64]*roadTriangleGridIndex
	allTriangles      []roadTriangle
	allTriangleIndex  *roadTriangleGridIndex
	primaryCenterline map[int64]LocalLine
	projectSearchDist float32
	projectSmoothStep float64
}

// 创建道路表面投影器。
// 初始化：
// 1. 按道路 ID 存储的三角网。
// 2. 每个道路独立的空间索引。
// 3. 全局道路三角网索引。
// 4. 道路主中心线缓存。
// 5. 投影搜索距离和高度平滑参数。
func NewRoadSurfaceProjector() *RoadSurfaceProjector {
	return &RoadSurfaceProjector{
		byRoad:            make(map[int64][]roadTriangle),
		byRoadIndex:       make(map[int64]*roadTriangleGridIndex),
		allTriangles:      make([]roadTriangle, 0, 1024),
		allTriangleIndex:  newRoadTriangleGridIndex(30.0),
		primaryCenterline: make(map[int64]LocalLine),
		projectSearchDist: 2.0,
		projectSmoothStep: 1.5,
	}
}

// 配置道路投影参数。
// searchDist 控制点距离道路 Mesh 多远以内仍尝试投影。
// smoothWindow 控制投影后高度 Y 的平滑窗口。
// 参数大于 0 时才会覆盖默认配置。
func (p *RoadSurfaceProjector) Configure(searchDist float32, smoothWindow float64) {
	if p == nil {
		return
	}
	if searchDist > 0 {
		p.projectSearchDist = searchDist
	}
	if smoothWindow > 0 {
		p.projectSmoothStep = smoothWindow
	}
}

// 添加道路中心线数据。
// 输入同一个 roadID 下多条中心线候选，
// 会去除重复点后计算每条线长度，
// 最终保留最长的一条作为该道路的主中心线。
// 用于后续道路方向分析或投影辅助。
func (p *RoadSurfaceProjector) AddRoadCenterline(roadID int64, lines []LocalLine) {
	if p == nil || roadID == 0 || len(lines) == 0 {
		return
	}
	best := p.primaryCenterline[roadID]
	bestLen := polylineLengthXZ(best.Points)
	for _, line := range lines {
		points := dedupeLinePoints(line.Points)
		if len(points) < 2 {
			continue
		}
		candidate := LocalLine{Points: points, Closed: false}
		candidateLen := polylineLengthXZ(candidate.Points)
		if candidateLen > bestLen {
			best = candidate
			bestLen = candidateLen
		}
	}
	if bestLen > 0 {
		p.primaryCenterline[roadID] = best
	}
}

// 获取指定道路保存的主中心线。
// 如果道路不存在或中心线点不足两个，则返回 false。
func (p *RoadSurfaceProjector) RoadCenterline(roadID int64) (LocalLine, bool) {
	if p == nil || roadID == 0 {
		return LocalLine{}, false
	}
	line, ok := p.primaryCenterline[roadID]
	return line, ok && len(line.Points) >= 2
}

// 添加道路 Mesh 表面三角数据。
// 输入道路顶点数组和三角索引，
// 将每三个索引组成一个 roadTriangle。
// 同时计算三角形 X/Z 包围盒，
// 加入道路专属索引和全局空间索引。
// 后续所有投影计算都基于这些三角形。
func (p *RoadSurfaceProjector) AddRoadSurface(roadID int64, pos [][3]float32, indices []uint32) {
	if p == nil || roadID == 0 {
		return
	}
	localIndex := p.byRoadIndex[roadID]
	if localIndex == nil {
		localIndex = newRoadTriangleGridIndex(30.0)
		p.byRoadIndex[roadID] = localIndex
	}
	for i := 0; i+2 < len(indices); i += 3 {
		ia, ib, ic := indices[i], indices[i+1], indices[i+2]
		if int(ia) >= len(pos) || int(ib) >= len(pos) || int(ic) >= len(pos) {
			continue
		}
		a, b, c := pos[ia], pos[ib], pos[ic]
		tri := roadTriangle{
			roadID: roadID,
			a:      a,
			b:      b,
			c:      c,
			minX:   min3(a[0], b[0], c[0]),
			maxX:   max3(a[0], b[0], c[0]),
			minZ:   min3(a[2], b[2], c[2]),
			maxZ:   max3(a[2], b[2], c[2]),
		}
		localIdx := len(p.byRoad[roadID])
		p.byRoad[roadID] = append(p.byRoad[roadID], tri)
		if localIndex != nil {
			localIndex.add(localIdx, tri)
		}
		if p.allTriangleIndex != nil {
			p.allTriangleIndex.add(len(p.allTriangles), tri)
		}
		p.allTriangles = append(p.allTriangles, tri)
	}
}

// 将一条线投影到道路表面。
// 主要用于道路标线、边界线等线状要素贴合道路高度。
// 流程：
// 1. 根据 mode 决定是否先按三角边界细分。
// 2. 根据 sampleStep 对线进行加密采样。
// 3. 对每个点查询道路三角面并计算高度。
// 4. 对未匹配点进行高度补齐。
// 5. 对高度进行平滑处理。
// 最终返回贴合道路表面的三维线。
func (p *RoadSurfaceProjector) ProjectLine(roadID int64, line LocalLine, sampleStep float32, mode string) (LocalLine, bool) {
	if p == nil {
		return line, false
	}
	tris := p.byRoad[roadID]
	useSpatialFallback := len(tris) == 0
	if useSpatialFallback && len(p.allTriangles) == 0 {
		return line, false
	}
	if mode == "" {
		mode = "fast"
	}
	points := line.Points
	if mode == "boundary_split" {
		points = p.refineLineByTriangleBoundaries(roadID, line.Points, tris, useSpatialFallback)
	}
	if sampleStep > 0 {
		points = densifyLine(points, sampleStep)
	}
	if len(points) == 0 {
		return line, true
	}

	projected := make([][3]float32, 0, len(points))
	matched := make([]bool, 0, len(points))
	matchedCount := 0
	for _, pt := range points {
		y, ok := p.projectPointIndexed(p.byRoadIndex[roadID], tris, pt)
		if !ok && useSpatialFallback {
			y, ok = p.projectPointSpatial(pt)
		}
		if ok {
			pt[1] = y
			matchedCount++
		}
		projected = append(projected, pt)
		matched = append(matched, ok)
	}

	if matchedCount == 0 {
		return line, false
	}
	projected = fillUnmatchedProjectedLineY(projected, matched)
	projected = smoothProjectedLineY(projected, p.projectSmoothStep)
	return LocalLine{
		Points: dedupeLinePoints(projected),
		Closed: line.Closed,
	}, true
}

// 根据道路三角形边界细分线。
// 遍历线段与道路三角形边的交点，
// 在道路 Mesh 三角边界处插入新的采样点。
// 这样可以避免一条线跨越多个三角面时高度变化不连续。
func (p *RoadSurfaceProjector) refineLineByTriangleBoundaries(roadID int64, points [][3]float32, tris []roadTriangle, useSpatialFallback bool) [][3]float32 {
	if p == nil || len(points) <= 1 {
		return points
	}
	index := p.byRoadIndex[roadID]
	sourceTris := tris
	if len(sourceTris) == 0 && useSpatialFallback {
		index = p.allTriangleIndex
		sourceTris = p.allTriangles
	}
	if len(sourceTris) == 0 {
		return points
	}
	out := make([][3]float32, 0, len(points)*2)
	out = append(out, points[0])
	const eps = 1e-5
	for i := 0; i < len(points)-1; i++ {
		a := points[i]
		b := points[i+1]
		ts := []float64{0, 1}
		candidates := p.queryTrianglesForSegment(index, sourceTris, a, b)
		for _, tri := range candidates {
			for _, edge := range [][2][3]float32{{tri.a, tri.b}, {tri.b, tri.c}, {tri.c, tri.a}} {
				if t, ok := segmentIntersectionParamXZ(a, b, edge[0], edge[1]); ok && t > eps && t < 1.0-eps {
					ts = append(ts, t)
				}
			}
		}
		sort.Float64s(ts)
		last := -1.0
		for _, t := range ts[1:] {
			if last >= 0 && math.Abs(t-last) <= 1e-4 {
				continue
			}
			last = t
			out = append(out, interpolateLinePoint(a, b, float32(t)))
		}
	}
	return dedupeLinePoints(out)
}

// 查询与一条线段可能相关的道路三角形。
// 根据线段 XZ 包围盒扩大搜索范围，
// 利用空间索引快速获取可能相交的三角面。
func (p *RoadSurfaceProjector) queryTrianglesForSegment(index *roadTriangleGridIndex, tris []roadTriangle, a, b [3]float32) []roadTriangle {
	if len(tris) == 0 {
		return nil
	}
	if index == nil {
		return tris
	}
	minX := minFloat32(a[0], b[0]) - p.projectSearchDist
	maxX := maxFloat32(a[0], b[0]) + p.projectSearchDist
	minZ := minFloat32(a[2], b[2]) - p.projectSearchDist
	maxZ := maxFloat32(a[2], b[2]) + p.projectSearchDist
	idxs := index.queryBounds(minX, maxX, minZ, maxZ)
	if len(idxs) == 0 {
		return nil
	}
	out := make([]roadTriangle, 0, len(idxs))
	for _, idx := range idxs {
		if idx >= 0 && idx < len(tris) {
			out = append(out, tris[idx])
		}
	}
	return out
}

// 判断两个二维线段是否相交。
// 使用 XZ 平面计算线段交点参数。
// 如果相交，返回交点在线段 AB 上的比例 t。
func segmentIntersectionParamXZ(a, b, c, d [3]float32) (float64, bool) {
	ax, az := float64(a[0]), float64(a[2])
	bx, bz := float64(b[0]), float64(b[2])
	cx, cz := float64(c[0]), float64(c[2])
	dx, dz := float64(d[0]), float64(d[2])
	rx, rz := bx-ax, bz-az
	sx, sz := dx-cx, dz-cz
	den := rx*sz - rz*sx
	if math.Abs(den) < 1e-9 {
		return 0, false
	}
	qpx, qpz := cx-ax, cz-az
	t := (qpx*sz - qpz*sx) / den
	u := (qpx*rz - qpz*rx) / den
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return 0, false
	}
	return t, true
}

// 在线段 AB 上按照比例 t 插值生成点。
// 用于计算线段与三角边界交点位置。
func interpolateLinePoint(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

// 将多个离散点投影到道路表面。
// 与 ProjectLine 类似，但是输入已经是点集合。
// 对每个点计算道路高度 Y，成功匹配的点更新高度。
func (p *RoadSurfaceProjector) ProjectPoints(roadID int64, points [][3]float32) ([][3]float32, bool) {
	if p == nil || len(points) == 0 {
		return points, false
	}
	tris := p.byRoad[roadID]
	useSpatialFallback := len(tris) == 0
	if useSpatialFallback && len(p.allTriangles) == 0 {
		return points, false
	}
	out := make([][3]float32, len(points))
	copy(out, points)
	matched := 0
	for i := range out {
		y, ok := p.projectPointIndexed(p.byRoadIndex[roadID], tris, out[i])
		if !ok && useSpatialFallback {
			y, ok = p.projectPointSpatial(out[i])
		}
		if ok {
			out[i][1] = y
			matched++
		}
	}
	return out, matched > 0
}

// 使用全局道路三角索引进行点投影。
// 当指定道路没有对应三角面时，作为全局搜索 fallback。
func (p *RoadSurfaceProjector) projectPointSpatial(pt [3]float32) (float32, bool) {
	if p == nil || len(p.allTriangles) == 0 {
		return 0, false
	}
	return p.projectPointIndexed(p.allTriangleIndex, p.allTriangles, pt)
}

// 输出道路缺失 Mesh 的警告。
// 当道路标线关联不到道路表面三角网时，提示保留原始高度。
func (p *RoadSurfaceProjector) WarnMissingRoad(lineFeature LineFeature) {
	logrus.Warnf("road marking id=%v ldid=%v has no related road surface triangles, keep original height",
		lineFeature.Fields["id"], lineFeature.Fields["ldid"])
}

// 对一组三角形执行点高度投影。
// 简单封装 projectPointIndexed，不使用空间索引。
func (p *RoadSurfaceProjector) projectPoint(tris []roadTriangle, pt [3]float32) (float32, bool) {
	return p.projectPointIndexed(nil, tris, pt)
}

// 使用空间索引优化点投影。
// 先通过 grid index 查找附近三角形，
// 如果找到候选则只在候选三角形中计算。
// 如果没有索引，则退化为遍历全部三角形。
func (p *RoadSurfaceProjector) projectPointIndexed(index *roadTriangleGridIndex, tris []roadTriangle, pt [3]float32) (float32, bool) {
	if p == nil || len(tris) == 0 {
		return 0, false
	}
	if index != nil {
		if idxs := index.query(pt); len(idxs) > 0 {
			cands := make([]roadTriangle, 0, len(idxs))
			for _, idx := range idxs {
				if idx >= 0 && idx < len(tris) {
					cands = append(cands, tris[idx])
				}
			}
			if len(cands) > 0 {
				return p.projectPointLinear(cands, pt)
			}
		}
	}
	return p.projectPointLinear(tris, pt)
}

// 在候选道路三角形中计算点对应高度。
// 判断点是否位于三角形内部：
// - 内部：使用重心坐标插值 Y。
// - 外部但距离较近：投影到最近边并估算高度。
// 最后根据距离和高度差进行加权平均，得到稳定高度。
func (p *RoadSurfaceProjector) projectPointLinear(tris []roadTriangle, pt [3]float32) (float32, bool) {
	px := float64(pt[0])
	pz := float64(pt[2])
	py := float64(pt[1])

	type candidate struct {
		dist float64
		y    float32
		w    float64
		zd   float64
	}
	cands := make([]candidate, 0, 8)
	for _, tri := range tris {
		searchDist := p.projectSearchDist
		if searchDist <= 0 {
			searchDist = 2.0
		}
		if pt[0] < tri.minX-searchDist || pt[0] > tri.maxX+searchDist || pt[2] < tri.minZ-searchDist || pt[2] > tri.maxZ+searchDist {
			continue
		}
		if inside, w1, w2, w3 := barycentricXZ(pt, tri.a, tri.b, tri.c); inside {
			y := float32(w1*float64(tri.a[1]) + w2*float64(tri.b[1]) + w3*float64(tri.c[1]))
			cands = append(cands, candidate{dist: 0, y: y, w: 1, zd: math.Abs(float64(y) - py)})
			continue
		}
		dist := pointTriangleDistanceXZ(px, pz, tri)
		if dist <= float64(searchDist) {
			y := interpolateTriangleYClosest(px, pz, tri)
			zd := math.Abs(float64(y) - py)
			w := 1.0 / math.Max(0.05, dist*dist+0.25*zd*zd)
			cands = append(cands, candidate{dist: dist, y: y, w: w, zd: zd})
		}
	}
	if len(cands) == 0 {
		return 0, false
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].zd < cands[j].zd
	})
	if cands[0].dist == 0 {
		var sum, weight float64
		for _, c := range cands {
			if c.dist > 0 {
				break
			}
			w := 1.0 / math.Max(0.02, 0.1+c.zd)
			sum += float64(c.y) * w
			weight += w
		}
		if weight > 0 {
			return float32(sum / weight), true
		}
	}
	var sum, weight float64
	limit := 4
	if len(cands) < limit {
		limit = len(cands)
	}
	for i := 0; i < limit; i++ {
		sum += float64(cands[i].y) * cands[i].w
		weight += cands[i].w
	}
	if weight == 0 {
		return cands[0].y, true
	}
	return float32(sum / weight), true
}

// 根据最大采样距离细分折线。
// 如果两个点之间距离超过 step，
// 自动插入中间点，使线段长度满足采样要求。
// 用于提高道路投影精度。
func densifyLine(points [][3]float32, step float32) [][3]float32 {
	if len(points) <= 1 {
		return points
	}
	out := make([][3]float32, 0, len(points))
	out = append(out, points[0])
	for i := 0; i < len(points)-1; i++ {
		a, b := points[i], points[i+1]
		dx := b[0] - a[0]
		dy := b[1] - a[1]
		dz := b[2] - a[2]
		dist := float32(math.Hypot(float64(dx), float64(dz)))
		if dist <= step {
			out = append(out, b)
			continue
		}
		parts := int(math.Ceil(float64(dist / step)))
		for j := 1; j <= parts; j++ {
			t := float32(j) / float32(parts)
			out = append(out, [3]float32{
				a[0] + dx*t,
				a[1] + dy*t,
				a[2] + dz*t,
			})
		}
	}
	return out
}

// 使用重心坐标判断点是否位于三角形内部。
// 在 XZ 平面计算三个权重。
// 如果三个权重均非负，则点位于三角形内部。
// 权重同时用于插值高度
func barycentricXZ(p, a, b, c [3]float32) (bool, float64, float64, float64) {
	den := float64((b[2]-c[2])*(a[0]-c[0]) + (c[0]-b[0])*(a[2]-c[2]))
	if den == 0 {
		return false, 0, 0, 0
	}
	w1 := float64((b[2]-c[2])*(p[0]-c[0])+(c[0]-b[0])*(p[2]-c[2])) / den
	w2 := float64((c[2]-a[2])*(p[0]-c[0])+(a[0]-c[0])*(p[2]-c[2])) / den
	w3 := 1 - w1 - w2
	eps := 1e-6
	return w1 >= -eps && w2 >= -eps && w3 >= -eps, w1, w2, w3
}

// 计算点到三角形边界的最近距离。
// 只考虑 XZ 平面。
// 用于判断点是否接近道路三角形。
func pointTriangleDistanceXZ(px, pz float64, tri roadTriangle) float64 {
	d1 := pointSegmentDistance2D(px, pz, float64(tri.a[0]), float64(tri.a[2]), float64(tri.b[0]), float64(tri.b[2]))
	d2 := pointSegmentDistance2D(px, pz, float64(tri.b[0]), float64(tri.b[2]), float64(tri.c[0]), float64(tri.c[2]))
	d3 := pointSegmentDistance2D(px, pz, float64(tri.c[0]), float64(tri.c[2]), float64(tri.a[0]), float64(tri.a[2]))
	return math.Min(d1, math.Min(d2, d3))
}

// 根据最近三角边估算高度。
// 当点不在三角形内部时，找到距离最近的边，
// 根据边上的两个端点高度插值计算 Y。
func interpolateTriangleYClosest(px, pz float64, tri roadTriangle) float32 {
	bestDist := math.MaxFloat64
	bestY := tri.a[1]
	for _, edge := range [][2][3]float32{{tri.a, tri.b}, {tri.b, tri.c}, {tri.c, tri.a}} {
		_, y, _, dist := closestPointSegment3DOnXZ(px, pz, edge[0], edge[1])
		if dist < bestDist {
			bestDist = dist
			bestY = y
		}
	}
	return bestY
}

// 计算二维点到线段距离。
// 返回最近点相关信息。
// 用于三角形边界距离计算。
func pointSegmentDistance2D(px, py, ax, ay, bx, by float64) float64 {
	_, _, _, dist := closestPointSegment2D(px, py, ax, ay, bx, by)
	return dist
}

// 计算三维线段在 XZ 平面的最近点。
// X/Z 使用二维投影计算，Y 使用线性插值恢复。
func closestPointSegment3DOnXZ(px, pz float64, a, b [3]float32) (float64, float32, float64, float64) {
	t, x, z, dist := closestPointSegment2D(px, pz, float64(a[0]), float64(a[2]), float64(b[0]), float64(b[2]))
	y := float32(float64(a[1]) + (float64(b[1])-float64(a[1]))*t)
	return x, y, z, dist
}

// 计算二维点在线段上的最近投影点。
// 返回：
// t 参数
// 最近点坐标
// 最近距离
func closestPointSegment2D(px, py, ax, ay, bx, by float64) (float64, float64, float64, float64) {
	abx := bx - ax
	aby := by - ay
	den := abx*abx + aby*aby
	t := 0.0
	if den > 0 {
		t = ((px-ax)*abx + (py-ay)*aby) / den
	}
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := ax + abx*t
	cy := ay + aby*t
	return t, cx, cy, math.Hypot(px-cx, py-cy)
}

// 返回三个 float32 中的最小值。
// 用于计算三角形包围盒。
func min3(a, b, c float32) float32 {
	if a > b {
		a = b
	}
	if a > c {
		a = c
	}
	return a
}

// 返回三个 float32 中的最大值。
// 用于计算三角形包围盒。
func max3(a, b, c float32) float32 {
	if a < b {
		a = b
	}
	if a < c {
		a = c
	}
	return a
}

// 对投影后的线高度进行距离窗口平滑。
// 使用沿线累计距离作为窗口，而不是固定点数量。
// 可以减少道路 Mesh 三角化误差造成的高度跳变。
func smoothProjectedLineY(points [][3]float32, windowMeters float64) [][3]float32 {
	if len(points) < 3 || windowMeters <= 0 {
		return points
	}
	cum := make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		cum[i] = cum[i-1] + math.Hypot(float64(points[i][0]-points[i-1][0]), float64(points[i][2]-points[i-1][2]))
	}
	out := make([][3]float32, len(points))
	copy(out, points)
	prefixY := make([]float64, len(points)+1)
	for i := range points {
		prefixY[i+1] = prefixY[i] + float64(points[i][1])
	}
	left := 0
	right := 0
	for i := range points {
		center := cum[i]
		for left < len(points) && center-cum[left] > windowMeters {
			left++
		}
		if right < i {
			right = i
		}
		for right < len(points) && cum[right]-center <= windowMeters {
			right++
		}
		if left < right {
			sumY := prefixY[right] - prefixY[left]
			out[i][1] = float32(sumY / float64(right-left))
		}
	}
	return out
}

// 对没有成功投影的点补充高度。
// 使用前后已匹配点之间进行线性插值。
// 避免局部道路 Mesh 缺失导致线高度断裂。
func fillUnmatchedProjectedLineY(points [][3]float32, matched []bool) [][3]float32 {
	if len(points) == 0 || len(points) != len(matched) {
		return points
	}
	out := make([][3]float32, len(points))
	copy(out, points)
	cum := make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		cum[i] = cum[i-1] + math.Hypot(float64(points[i][0]-points[i-1][0]), float64(points[i][2]-points[i-1][2]))
	}
	nextMatched := make([]int, len(points))
	next := -1
	for i := len(points) - 1; i >= 0; i-- {
		if matched[i] {
			next = i
		}
		nextMatched[i] = next
	}
	prevMatched := -1
	for i := range out {
		if matched[i] {
			prevMatched = i
			continue
		}
		nextIdx := nextMatched[i]
		switch {
		case prevMatched >= 0 && nextIdx >= 0:
			den := cum[nextIdx] - cum[prevMatched]
			if den <= 0 {
				out[i][1] = out[prevMatched][1]
			} else {
				t := (cum[i] - cum[prevMatched]) / den
				out[i][1] = float32((1-t)*float64(out[prevMatched][1]) + t*float64(out[nextIdx][1]))
			}
		case prevMatched >= 0:
			out[i][1] = out[prevMatched][1]
		case nextIdx >= 0:
			out[i][1] = out[nextIdx][1]
		}
	}
	return out
}

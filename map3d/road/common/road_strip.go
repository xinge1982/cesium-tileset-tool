package common

import (
	"cesium-tileset-tool/map3d/mgltf"
	"math"
	"sort"
)

const defaultStripChunkLength = 50.0

// 根据道路边界和中心线生成道路表面三角网。
// 适用于没有孔洞的长条形道路区域。
// 首先检查中心线是否有效并匹配道路范围，然后根据中心线将道路边界拆分为左右两侧。
// 最后连接左右边界生成条带 Mesh，并通过面积比例和边界包含检查验证生成结果。
func triangulateRoadSurfaceByCenterline(ring LocalRings, centerline LocalLine) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 || len(centerline.Points) < 2 {
		return nil, nil, false
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 6 {
		return nil, nil, false
	}
	if !centerlineMatchesRing(outer, centerline.Points) {
		return nil, nil, false
	}
	left, right, ok := splitRoadBoundaryByCenterline(outer, centerline)
	if !ok {
		return nil, nil, false
	}
	pos, indices, ok := triangulateBoundaryChains(left, right, centerline.Points)
	if !ok {
		return nil, nil, false
	}
	polyArea := polygonAreaXZ(outer)
	triArea := trianglesAreaXZ(pos, indices)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false
	}
	ratio := triArea / polyArea
	if ratio < 0.985 || ratio > 1.02 {
		return nil, nil, false
	}
	if !trianglesFitPolygonXZ(outer, pos, indices) {
		return nil, nil, false
	}
	return pos, indices, true
}

// 根据中心线采样方式生成道路表面条带 Mesh。
// 沿中心线逐点计算切线方向，并通过横断线与道路外轮廓求交得到左右边界采样点。
// 对左右边界高程进行平滑处理后，将道路拆分为多个条带块进行三角化。
func triangulateSurfaceByCenterlineSampling(ring LocalRings, centerline LocalLine) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 || len(centerline.Points) < 3 {
		return nil, nil, false
	}
	outer := dedupeClosedRing(ring.Outer)
	cl := dedupeLinePoints(centerline.Points)
	if len(outer) < 6 || len(cl) < 3 {
		return nil, nil, false
	}
	if !centerlineMatchesRing(outer, cl) {
		return nil, nil, false
	}

	left := make([][3]float32, 0, len(cl))
	right := make([][3]float32, 0, len(cl))
	for i := range cl {
		tan, ok := centerlineTangent(cl, i)
		if !ok {
			continue
		}
		dir := [2]float64{-tan[1], tan[0]}
		l, r, ok := crossSectionHits(outer, cl[i], dir)
		if !ok {
			continue
		}
		left = append(left, l)
		right = append(right, r)
	}
	left = dedupeLinePoints(left)
	right = dedupeLinePoints(right)
	if len(left) < 2 || len(right) < 2 || len(left) != len(right) {
		return nil, nil, false
	}
	smoothStripElevations(left, right)
	return triangulateStripChunks(left, right, defaultStripChunkLength)
}

// 根据中心点和横向方向计算道路边界上的左右交点。
// 将中心点沿指定方向延伸为无限直线，与道路外轮廓每条边求交。
// 对所有交点按照距离排序，选择中心点两侧最近的两个交点作为左右边界点。
func crossSectionHits(outer [][3]float32, center [3]float32, dir [2]float64) ([3]float32, [3]float32, bool) {
	hits := make([]sampleHit, 0, 8)
	hitPts := make([][3]float32, 0, 8)
	base := [2]float64{float64(center[0]), float64(center[2])}
	for i := 0; i < len(outer); i++ {
		a := outer[i]
		b := outer[(i+1)%len(outer)]
		u, v, ok := infiniteLineSegmentIntersectionUV(base, dir, [2]float64{float64(a[0]), float64(a[2])}, [2]float64{float64(b[0]), float64(b[2])})
		if !ok {
			continue
		}
		hits = append(hits, sampleHit{u: u})
		hitPts = append(hitPts, interpolatePoint3(a, b, float32(v)))
	}
	if len(hits) < 2 {
		return [3]float32{}, [3]float32{}, false
	}
	order := make([]int, len(hits))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return hits[order[i]].u < hits[order[j]].u })
	leftIdx, rightIdx, ok := chooseNearestBracketHits(order, hits)
	if !ok {
		return [3]float32{}, [3]float32{}, false
	}
	left := hitPts[leftIdx]
	right := hitPts[rightIdx]
	if segmentLengthXZ(left, right) < 0.01 {
		return [3]float32{}, [3]float32{}, false
	}
	return left, right, true
}

// 从多个横断面交点中选择距离中心线最近的左右边界交点。
// 优先选择参数 u 小于等于 0 的最近点作为左侧，
// 选择参数 u 大于等于 0 的最近点作为右侧。
// 如果无法找到严格左右点，则使用最远两个交点作为备用。
func chooseNearestBracketHits(order []int, hits []sampleHit) (int, int, bool) {
	leftIdx := -1
	rightIdx := -1
	bestLeft := math.MaxFloat64
	bestRight := math.MaxFloat64
	for _, idx := range order {
		u := hits[idx].u
		if u <= 0 {
			d := math.Abs(u)
			if d < bestLeft {
				bestLeft = d
				leftIdx = idx
			}
		}
		if u >= 0 {
			d := math.Abs(u)
			if d < bestRight {
				bestRight = d
				rightIdx = idx
			}
		}
	}
	if leftIdx >= 0 && rightIdx >= 0 && leftIdx != rightIdx {
		return leftIdx, rightIdx, true
	}
	if len(order) >= 2 {
		return order[0], order[len(order)-1], true
	}
	return -1, -1, false
}

// 将左右边界链按照固定长度切分成多个 Mesh 块并进行三角化。
// 根据左边界累计长度确定切分范围，
// 每个 Chunk 单独生成三角网，最后合并所有顶点和索引。
// 用于控制单个 Mesh 块规模，避免超长道路生成过大的 Primitive。
func triangulateStripChunks(left, right [][3]float32, chunkLength float64) ([][3]float32, []uint32, bool) {
	if len(left) < 2 || len(right) < 2 || len(left) != len(right) {
		return nil, nil, false
	}
	if chunkLength <= 0 {
		chunkLength = defaultStripChunkLength
	}
	cum := polylineCumLengths(left)
	if len(cum) != len(left) {
		return nil, nil, false
	}
	var allPos [][3]float32
	var allIdx []uint32
	start := 0
	for start < len(left)-1 {
		end := start + 1
		limit := cum[start] + chunkLength
		for end < len(left)-1 && cum[end] < limit {
			end++
		}
		if end <= start {
			end = start + 1
		}
		chunkPos, chunkIdx, ok := triangulateStripSegment(left[start:end+1], right[start:end+1])
		if !ok {
			return nil, nil, false
		}
		base := uint32(len(allPos))
		allPos = append(allPos, chunkPos...)
		for _, idx := range chunkIdx {
			allIdx = append(allIdx, base+idx)
		}
		start = end
	}
	return allPos, allIdx, len(allIdx) >= 3
}

// 对单个左右边界条带区域进行三角化。
// 首先重新平衡左右边界采样点，使两侧具有相同参数位置。
// 然后构造成闭合多边形，使用 Earcut 生成初始三角网。
// 最后通过内部边翻转优化三角形质量。
func triangulateStripSegment(left, right [][3]float32) ([][3]float32, []uint32, bool) {
	if len(left) < 2 || len(right) < 2 || len(left) != len(right) {
		return nil, nil, false
	}
	leftSamples, rightSamples, ok := rebalanceStripSamples(left, right)
	if !ok {
		return nil, nil, false
	}

	// Build a local polygon chunk (left chain + reversed right chain) and triangulate it.
	ring := make([][3]float32, 0, len(leftSamples)+len(rightSamples)+1)
	ring = append(ring, leftSamples...)
	for i := len(rightSamples) - 1; i >= 0; i-- {
		ring = append(ring, rightSamples[i])
	}
	if len(ring) < 4 {
		return nil, nil, false
	}
	if ring[0] != ring[len(ring)-1] {
		ring = append(ring, ring[0])
	}
	pos, idx, err := mgltf.EarcutXZRings([][][3]float32{ring})
	if err != nil || len(idx) < 3 {
		return nil, nil, false
	}
	// Apply constrained-Delaunay-like refinement by flipping internal edges only.
	idx = optimizeTrianglesXZ(pos, idx)
	return pos, idx, true
}

// 根据左右边界累计距离重新分配采样点。
// 将左右两侧边界转换为 0~1 的归一化长度参数，
// 合并两侧采样位置后重新插值生成对应点。
// 保证左右边界在相同位置具有对应顶点，便于生成规则条带 Mesh。
func rebalanceStripSamples(left, right [][3]float32) ([][3]float32, [][3]float32, bool) {
	if len(left) < 2 || len(right) < 2 {
		return nil, nil, false
	}
	leftCum := polylineCumLengths(left)
	rightCum := polylineCumLengths(right)
	leftParams, ok := normalizedPolylineParams(leftCum)
	if !ok {
		return nil, nil, false
	}
	rightParams, ok := normalizedPolylineParams(rightCum)
	if !ok {
		return nil, nil, false
	}
	params := mergedBoundaryParams(leftParams, rightParams)
	if len(params) < 2 {
		return nil, nil, false
	}
	leftSamples, ok := sampleBoundaryByParam(left, leftParams, params)
	if !ok {
		return nil, nil, false
	}
	rightSamples, ok := sampleBoundaryByParam(right, rightParams, params)
	if !ok {
		return nil, nil, false
	}
	return leftSamples, rightSamples, true
}

// 平滑道路条带左右两侧的高程。
// 将左右边界转换为中心高度和横向高度差两部分处理。
// 对中心高度进行较强平滑，对左右高差进行较弱平滑，
// 同时限制最大横坡差，避免生成异常倾斜道路面。
func smoothStripElevations(left, right [][3]float32) {
	if len(left) != len(right) || len(left) < 5 {
		return
	}
	centerY := make([]float32, len(left))
	diffY := make([]float32, len(left))
	for i := range left {
		centerY[i] = (left[i][1] + right[i][1]) * 0.5
		diffY[i] = left[i][1] - right[i][1]
	}

	smoothScalar(centerY, 2, 2)
	smoothScalar(diffY, 1, 1)

	const maxCrossDelta = float32(0.8)
	for i := range diffY {
		if diffY[i] > maxCrossDelta {
			diffY[i] = maxCrossDelta
		} else if diffY[i] < -maxCrossDelta {
			diffY[i] = -maxCrossDelta
		}
	}

	for i := range left {
		half := diffY[i] * 0.5
		left[i][1] = centerY[i] + half
		right[i][1] = centerY[i] - half
	}
}

// 对一组标量数据执行滑动窗口平滑。
// 根据指定半径计算邻域平均值，
// 可以多次执行平滑过程，用于降低道路高程采样噪声。
func smoothScalar(values []float32, radius, passes int) {
	if len(values) < 3 || radius <= 0 || passes <= 0 {
		return
	}
	buf := make([]float32, len(values))
	for p := 0; p < passes; p++ {
		copy(buf, values)
		for i := 0; i < len(values); i++ {
			start := i - radius
			if start < 0 {
				start = 0
			}
			end := i + radius
			if end >= len(values) {
				end = len(values) - 1
			}
			var sum float32
			var cnt float32
			for j := start; j <= end; j++ {
				sum += buf[j]
				cnt += 1
			}
			values[i] = sum / cnt
		}
	}
}

// 判断中心线是否位于道路外轮廓范围附近。
// 通过道路外轮廓包围盒检测中心线点是否落入合理范围。
// 至少需要两个中心线点匹配道路范围才认为有效。
func centerlineMatchesRing(outer, centerline [][3]float32) bool {
	if len(outer) < 3 || len(centerline) < 2 {
		return false
	}
	minX, maxX := outer[0][0], outer[0][0]
	minZ, maxZ := outer[0][2], outer[0][2]
	for i := 1; i < len(outer); i++ {
		if outer[i][0] < minX {
			minX = outer[i][0]
		}
		if outer[i][0] > maxX {
			maxX = outer[i][0]
		}
		if outer[i][2] < minZ {
			minZ = outer[i][2]
		}
		if outer[i][2] > maxZ {
			maxZ = outer[i][2]
		}
	}
	const tol = float32(5.0)
	matchCount := 0
	for _, p := range centerline {
		if p[0] >= minX-tol && p[0] <= maxX+tol && p[2] >= minZ-tol && p[2] <= maxZ+tol {
			matchCount++
			if matchCount >= 2 {
				return true
			}
		}
	}
	return false
}

// 根据中心线起终点将道路外轮廓拆分成左右两条边界链。
// 首先找到中心线端点在道路边界上的对应位置，
// 然后沿 Ring 两个方向提取两条边界路径。
// 根据中心线方向判断左右关系，并调整边界方向保证与中心线一致。
func splitRoadBoundaryByCenterline(outer [][3]float32, centerline LocalLine) ([][3]float32, [][3]float32, bool) {
	cl := dedupeLinePoints(centerline.Points)
	if len(cl) < 2 {
		return nil, nil, false
	}
	ring, startIdx, endIdx, ok := splitRingAtCenterlineEnds(outer, cl[0], cl[len(cl)-1])
	if !ok {
		return nil, nil, false
	}
	startTan, ok1 := centerlineTangent(cl, 0)
	endTan, ok2 := centerlineTangent(cl, len(cl)-1)
	if !ok1 || !ok2 {
		return nil, nil, false
	}
	mx, mz := normalize2(startTan[0]+endTan[0], startTan[1]+endTan[1])
	midTan := [2]float64{mx, mz}
	if midTan[0] == 0 && midTan[1] == 0 {
		midTan = startTan
	}

	pathA := extractRingPath(ring, startIdx, endIdx)
	pathB := extractRingPath(ring, endIdx, startIdx)
	if len(pathA) < 2 || len(pathB) < 2 {
		return nil, nil, false
	}

	sideA := boundarySideSign(pathA, cl[0], midTan)
	sideB := boundarySideSign(pathB, cl[0], midTan)
	left := pathA
	right := pathB
	if sideA < sideB {
		left, right = pathB, pathA
	}

	left = orientBoundaryAlongCenterline(left, cl)
	right = orientBoundaryAlongCenterline(right, cl)
	if !stripChainsAreUsable(left, right, cl) {
		return nil, nil, false
	}
	return left, right, true
}

// 在道路外轮廓上根据中心线端点插入切分点。
// 找到目标点距离道路边界最近的位置，
// 将中心线起点和终点插入 Ring，
// 返回新的 Ring 以及两个切分索引。
func splitRingAtCenterlineEnds(outer [][3]float32, start, end [3]float32) ([][3]float32, int, int, bool) {
	startSeg, startT, startPt, ok := closestBoundaryLocation(outer, start)
	if !ok {
		return nil, 0, 0, false
	}
	endSeg, endT, endPt, ok := closestBoundaryLocation(outer, end)
	if !ok {
		return nil, 0, 0, false
	}

	ring := append([][3]float32(nil), outer...)
	startIdx := 0
	endIdx := 0
	insertions := []struct {
		seg int
		t   float64
		pt  [3]float32
		tag string
	}{
		{seg: startSeg, t: startT, pt: startPt, tag: "start"},
		{seg: endSeg, t: endT, pt: endPt, tag: "end"},
	}
	if insertions[0].seg > insertions[1].seg || (insertions[0].seg == insertions[1].seg && insertions[0].t > insertions[1].t) {
		insertions[0], insertions[1] = insertions[1], insertions[0]
	}
	offset := 0
	for _, ins := range insertions {
		var idx int
		ring, idx = insertPointAfterSegment(ring, ins.seg+offset, ins.pt)
		if ins.tag == "start" {
			startIdx = idx
		} else {
			endIdx = idx
		}
		offset++
	}
	if startIdx == endIdx {
		return nil, 0, 0, false
	}
	return ring, startIdx, endIdx, true
}

// 查找目标点在道路边界上的最近位置。
// 遍历所有边界线段，
// 使用点到线段距离计算最近线段以及对应插值位置。
func closestBoundaryLocation(outer [][3]float32, target [3]float32) (int, float64, [3]float32, bool) {
	bestSeg := -1
	bestT := 0.0
	bestDist := math.MaxFloat64
	var bestPt [3]float32
	for i := 0; i < len(outer); i++ {
		a := outer[i]
		b := outer[(i+1)%len(outer)]
		t, _, _, dist := closestPointSegment2D(
			float64(target[0]), float64(target[2]),
			float64(a[0]), float64(a[2]),
			float64(b[0]), float64(b[2]),
		)
		if dist >= bestDist {
			continue
		}
		bestDist = dist
		bestSeg = i
		bestT = t
		bestPt = interpolatePoint3(a, b, float32(t))
	}
	return bestSeg, bestT, bestPt, bestSeg >= 0
}

// 在 Ring 的指定边之后插入一个新的边界点。
// 如果目标点已经存在，则直接返回已有索引。
// 否则创建新的 Ring，并返回插入后的点索引。
func insertPointAfterSegment(ring [][3]float32, seg int, pt [3]float32) ([][3]float32, int) {
	if len(ring) == 0 {
		return ring, 0
	}
	seg = ((seg % len(ring)) + len(ring)) % len(ring)
	next := (seg + 1) % len(ring)
	if ring[seg] == pt {
		return ring, seg
	}
	if ring[next] == pt {
		return ring, next
	}
	insertAt := next
	out := make([][3]float32, 0, len(ring)+1)
	out = append(out, ring[:insertAt]...)
	out = append(out, pt)
	out = append(out, ring[insertAt:]...)
	return out, insertAt
}

// 根据左右边界链和中心线生成连续三角带。
// 先将左右边界按照归一化长度参数重新采样，
// 然后左右点一一对应连接，生成两个三角形组成一个矩形条带单元。
func triangulateBoundaryChains(left, right, centerline [][3]float32) ([][3]float32, []uint32, bool) {
	if len(left) < 2 || len(right) < 2 {
		return nil, nil, false
	}
	_ = centerline
	leftCum := polylineCumLengths(left)
	rightCum := polylineCumLengths(right)
	leftParams, ok := normalizedPolylineParams(leftCum)
	if !ok {
		return nil, nil, false
	}
	rightParams, ok := normalizedPolylineParams(rightCum)
	if !ok {
		return nil, nil, false
	}
	params := mergedBoundaryParams(leftParams, rightParams)
	if len(params) < 2 {
		return nil, nil, false
	}
	leftSamples, ok := sampleBoundaryByParam(left, leftParams, params)
	if !ok {
		return nil, nil, false
	}
	rightSamples, ok := sampleBoundaryByParam(right, rightParams, params)
	if !ok {
		return nil, nil, false
	}

	pos := make([][3]float32, 0, len(leftSamples)+len(rightSamples))
	pos = append(pos, leftSamples...)
	rightBase := uint32(len(pos))
	pos = append(pos, rightSamples...)
	indices := make([]uint32, 0, (len(params)-1)*6)

	for i := 0; i < len(params)-1; i++ {
		l0 := uint32(i)
		l1 := uint32(i + 1)
		r0 := rightBase + uint32(i)
		r1 := rightBase + uint32(i+1)
		appendStripTriangle(&indices, pos, l0, r0, l1)
		appendStripTriangle(&indices, pos, l1, r0, r1)
	}
	return pos, indices, len(indices) >= 3
}

// 将累计长度转换为 0~1 范围的归一化参数。
// 用于不同长度边界之间建立对应采样关系。
func normalizedPolylineParams(cum []float64) ([]float64, bool) {
	if len(cum) < 2 {
		return nil, false
	}
	total := cum[len(cum)-1]
	if total <= 1e-6 {
		return nil, false
	}
	params := make([]float64, len(cum))
	for i := range cum {
		params[i] = cum[i] / total
	}
	return params, true
}

// 计算折线沿线累计长度。
// 每个点保存从起点开始的 XZ 平面累计距离。
// 用于路径参数化和等比例采样。
func polylineCumLengths(points [][3]float32) []float64 {
	cum := make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		cum[i] = cum[i-1] + segmentLengthXZ(points[i-1], points[i])
	}
	return cum
}

// 合并左右边界的采样参数。
// 合并两个边界的归一化位置并排序，
// 删除距离过近的重复参数。
// 用于生成统一采样位置。
func mergedBoundaryParams(leftParams, rightParams []float64) []float64 {
	params := make([]float64, 0, len(leftParams)+len(rightParams))
	params = append(params, leftParams...)
	params = append(params, rightParams...)
	sort.Float64s(params)
	const eps = 1e-4
	out := params[:0]
	for _, s := range params {
		if len(out) == 0 || math.Abs(s-out[len(out)-1]) > eps {
			out = append(out, s)
		}
	}
	return out
}

// 根据归一化参数在线段路径中插值采样。
// 根据目标参数找到所在路径段，
// 使用线性插值计算对应三维坐标。
func sampleBoundaryByParam(path [][3]float32, pathParams, sampleParams []float64) ([][3]float32, bool) {
	if len(path) != len(pathParams) || len(path) < 2 {
		return nil, false
	}
	out := make([][3]float32, len(sampleParams))
	seg := 0
	for i, s := range sampleParams {
		if s <= pathParams[0] {
			out[i] = path[0]
			continue
		}
		if s >= pathParams[len(pathParams)-1] {
			out[i] = path[len(path)-1]
			continue
		}
		for seg < len(pathParams)-2 && pathParams[seg+1] < s {
			seg++
		}
		next := seg + 1
		for next < len(pathParams)-1 && pathParams[next]-pathParams[seg] < 1e-6 {
			next++
		}
		den := pathParams[next] - pathParams[seg]
		if den <= 1e-6 {
			out[i] = path[next]
			continue
		}
		t := float32((s - pathParams[seg]) / den)
		out[i] = interpolatePoint3(path[seg], path[next], t)
	}
	return out, true
}

// 向三角索引列表追加一个条带三角形。
// 检查三个顶点是否重复，
// 检查三角形面积是否接近零，
// 避免生成无效退化三角形。
func appendStripTriangle(indices *[]uint32, pos [][3]float32, a, b, c uint32) {
	if a == b || b == c || a == c {
		return
	}
	if math.Abs(cross2DXZ(pos[a], pos[b], pos[c])) < 1e-7 {
		return
	}
	*indices = append(*indices, a, b, c)
}

// 计算多边形在 XZ 平面的面积。
// 使用鞋带公式计算二维投影面积。
// 用于验证三角化结果是否覆盖完整道路区域。
func polygonAreaXZ(points [][3]float32) float64 {
	if len(points) < 3 {
		return 0
	}
	var area float64
	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		area += float64(points[i][0])*float64(points[j][2]) - float64(points[j][0])*float64(points[i][2])
	}
	if area < 0 {
		area = -area
	}
	return area * 0.5
}

// 计算三角网在 XZ 平面的总面积。
// 遍历所有三角形索引，
// 累加每个三角形投影面积。
func trianglesAreaXZ(pos [][3]float32, indices []uint32) float64 {
	var area float64
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := indices[i], indices[i+1], indices[i+2]
		if int(a) >= len(pos) || int(b) >= len(pos) || int(c) >= len(pos) {
			continue
		}
		area += math.Abs(cross2DXZ(pos[a], pos[b], pos[c])) * 0.5
	}
	return area
}

// 检查生成的三角形是否全部位于道路边界内部。
// 对每个三角形采样中心点以及靠近边的位置，
// 判断这些采样点是否位于道路多边形内部。
// 防止三角化产生越界三角形。
func trianglesFitPolygonXZ(outer [][3]float32, pos [][3]float32, indices []uint32) bool {
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := indices[i], indices[i+1], indices[i+2]
		if int(a) >= len(pos) || int(b) >= len(pos) || int(c) >= len(pos) {
			return false
		}
		pa, pb, pc := pos[a], pos[b], pos[c]
		cx := (pa[0] + pb[0] + pc[0]) / 3
		cz := (pa[2] + pb[2] + pc[2]) / 3
		samples := [][2]float32{
			{cx, cz},
			{(cx + (pa[0]+pb[0])/2) / 2, (cz + (pa[2]+pb[2])/2) / 2},
			{(cx + (pb[0]+pc[0])/2) / 2, (cz + (pb[2]+pc[2])/2) / 2},
			{(cx + (pc[0]+pa[0])/2) / 2, (cz + (pc[2]+pa[2])/2) / 2},
		}
		for _, s := range samples {
			if !pointInPolygonXZ(outer, s[0], s[1]) {
				return false
			}
		}
	}
	return true
}

// 判断二维点是否位于多边形内部。
// 使用射线法判断点与多边形边界关系。
// 如果点落在边界线上，也认为属于多边形。
func pointInPolygonXZ(poly [][3]float32, x, z float32) bool {
	if len(poly) < 3 {
		return false
	}
	inside := false
	j := len(poly) - 1
	for i := 0; i < len(poly); i++ {
		xi, zi := poly[i][0], poly[i][2]
		xj, zj := poly[j][0], poly[j][2]
		onEdge := pointOnSegmentXZ(x, z, xi, zi, xj, zj)
		if onEdge {
			return true
		}
		intersects := ((zi > z) != (zj > z)) &&
			(float64(x) < (float64(xj-xi)*float64(z-zi))/float64(zj-zi)+float64(xi))
		if intersects {
			inside = !inside
		}
		j = i
	}
	return inside
}

// 判断二维点是否在线段上。
// 使用叉积判断共线关系，
// 使用点积判断是否位于线段范围内。
func pointOnSegmentXZ(px, pz, ax, az, bx, bz float32) bool {
	const eps = 1e-4
	cross := math.Abs((float64(px-ax) * float64(bz-az)) - (float64(pz-az) * float64(bx-ax)))
	if cross > eps {
		return false
	}
	dot := float64(px-ax)*float64(px-bx) + float64(pz-az)*float64(pz-bz)
	return dot <= eps
}

// 检查左右边界链是否适合作为道路条带。
// 判断左右边界长度比例是否合理，
// 判断中心线长度与道路宽度关系是否符合长条道路特征。
// 防止错误中心线导致异常 Mesh。
func stripChainsAreUsable(left, right, centerline [][3]float32) bool {
	leftLen := polylineLengthXZ(left)
	rightLen := polylineLengthXZ(right)
	centerLen := polylineLengthXZ(centerline)
	if leftLen <= 0 || rightLen <= 0 || centerLen <= 0 {
		return false
	}
	shorter := leftLen
	longer := rightLen
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	if longer/shorter > 1.25 {
		return false
	}
	if centerLen/shorter < 0.7 {
		return false
	}
	return true
}

// 按 Ring 索引方向提取一段边界路径。
// 从 startIdx 开始沿 Ring 顺序遍历，
// 直到 endIdx，形成一条连续边界链。
func extractRingPath(outer [][3]float32, startIdx, endIdx int) [][3]float32 {
	n := len(outer)
	if n == 0 {
		return nil
	}
	out := make([][3]float32, 0, n)
	i := startIdx
	for {
		out = append(out, outer[i])
		if i == endIdx {
			break
		}
		i = (i + 1) % n
	}
	return dedupeLinePoints(out)
}

// 根据中心线方向判断边界位于哪一侧。
// 使用中心点到边界中点的向量和中心线切向量叉积判断左右关系。
func boundarySideSign(path [][3]float32, center [3]float32, tangent [2]float64) float64 {
	if len(path) == 0 {
		return 0
	}
	mid := path[len(path)/2]
	vx := float64(mid[0] - center[0])
	vz := float64(mid[2] - center[2])
	return tangent[0]*vz - tangent[1]*vx
}

// 调整边界方向，使其与中心线方向一致。
// 比较边界首尾点和中心线首尾点距离，
// 如果反向距离更短，则翻转边界点顺序。
func orientBoundaryAlongCenterline(path, centerline [][3]float32) [][3]float32 {
	if len(path) < 2 || len(centerline) < 2 {
		return path
	}
	startDist := segmentLengthXZ(path[0], centerline[0]) + segmentLengthXZ(path[len(path)-1], centerline[len(centerline)-1])
	reverseDist := segmentLengthXZ(path[len(path)-1], centerline[0]) + segmentLengthXZ(path[0], centerline[len(centerline)-1])
	if reverseDist < startDist {
		return reversePoints(path)
	}
	return path
}

// 计算中心线某个点位置的切线方向。
// 优先使用前后两个有效点计算方向，
// 起点或终点使用单侧方向。
// 最终返回归一化二维方向向量。
func centerlineTangent(points [][3]float32, idx int) ([2]float64, bool) {
	prev := idx - 1
	for prev >= 0 && segmentLengthXZ(points[prev], points[idx]) == 0 {
		prev--
	}
	next := idx + 1
	for next < len(points) && segmentLengthXZ(points[idx], points[next]) == 0 {
		next++
	}
	var dx, dz float64
	switch {
	case prev >= 0 && next < len(points):
		dx = float64(points[next][0] - points[prev][0])
		dz = float64(points[next][2] - points[prev][2])
	case next < len(points):
		dx = float64(points[next][0] - points[idx][0])
		dz = float64(points[next][2] - points[idx][2])
	case prev >= 0:
		dx = float64(points[idx][0] - points[prev][0])
		dz = float64(points[idx][2] - points[prev][2])
	default:
		return [2]float64{}, false
	}
	x, z := normalize2(dx, dz)
	return [2]float64{x, z}, true
}

func dedupeClosedRing(points [][3]float32) [][3]float32 {
	points = dedupeLinePoints(points)
	if len(points) > 1 && points[0] == points[len(points)-1] {
		points = points[:len(points)-1]
	}
	return points
}

func reversePoints(points [][3]float32) [][3]float32 {
	out := make([][3]float32, len(points))
	for i := range points {
		out[i] = points[len(points)-1-i]
	}
	return out
}

package common

import (
	"fmt"
	"math"

	"github.com/twpayne/go-geom"
)

//PCA（Principal Component Analysis，主成分分析）是一种寻找数据主要方向的数学方法。
//从道路面（Polygon / MultiPolygon）估算道路中心线；
//优先通过 PCA 主方向 + 横断面采样生成弯曲中心线；
//如果无法生成曲线中心线，则退化为 PCA 方向上的直线中心线；
//保留 Z 高程；

//中心线入口:
// ├── EstimateCenterlineFeatureFromSurface
// └── EstimateCenterlineGeomFromSurfaceGeom
//
//曲线中心线:
// ├── estimateCurvedCenterlineLocal
// ├── centerlinePointAtSample
// ├── nearestBracketSampleHits
//
//几何计算:
// ├── infiniteLineSegmentIntersectionUV
// ├── linePolygonIntersections
// ├── infiniteLineSegmentIntersection
// ├── projectionSpan64
// └── projectionSpan
//
//点处理:
// ├── dedupeCurvePoints
// ├── smoothCenterlinePoints
// ├── dedupeClosedRing64
// └── dedupeSampleHits
//
//Polygon处理:
// ├── largestPolygonOuterRing
// ├── polygonOuterRing64
// └── polygonAreaLonLat64
//
//数学:
// ├── principalAxis2D
// ├── ringMeanXYZ
// └── almostEqualPoint64
//
//排序:
// ├── sortFloat64s
// └── sortSampleHitsByU

//Polygon道路面
//
//       |
//       v
//
//提取外环点
//
//       |
//       v
//
//PCA
//
//       |
//       |
//       +----------------+
//       |                |
//       v                v
//
//道路主方向          道路中心点
//
//
//       |
//       v
//
//沿主方向切横断面
//
//       |
//       v
//
//计算左右边界中点
//
//       |
//       v
//
//中心线

// 根据道路面 SurfaceFeature 估算中心线 Feature。
//
// 功能:
//  1. 校验输入道路面数据是否合法。
//  2. 从 Polygon / MultiPolygon 几何中提取并计算中心线。
//  3. 复制原始 Feature 属性字段。
//  4. 自动补充 hroad_id 字段，优先使用:
//     hroad_id -> road_id -> id。
//
// 返回:
// - CenterlineFeature: 包含中心线 Geometry 和属性。
// - error: 输入非法或中心线计算失败时返回错误。
func EstimateCenterlineFeatureFromSurface(feature SurfaceFeature) (CenterlineFeature, error) {
	if err := feature.Validate(); err != nil {
		return CenterlineFeature{}, err
	}
	line, err := EstimateCenterlineGeomFromSurfaceGeom(feature.Geom)
	if err != nil {
		return CenterlineFeature{}, err
	}
	fields := cloneFeatureFields(feature.Fields)
	if featureFieldInt64(fields, "hroad_id") == 0 {
		if roadID := featureFieldInt64(fields, "road_id"); roadID != 0 {
			fields["hroad_id"] = roadID
		} else if id := featureFieldInt64(fields, "id"); id != 0 {
			fields["hroad_id"] = id
		}
	}
	return CenterlineFeature{Geom: line, Fields: fields}, nil
}

// 根据道路面几何 Geometry 计算中心线。
//
// 支持:
// - Polygon
// - MultiPolygon
//
// 算法流程:
// 1. 获取最大面积 Polygon 的外环。
// 2. 去除重复点并计算局部平面坐标。
// 3. 使用 PCA 分析道路面的主方向。
// 4. 沿主方向采样横断面:
//   - 如果能够计算有效横断面中心点，则生成曲线中心线。
//   - 如果失败，则使用 PCA 主方向生成三点直线中心线。
//
// 5. 将局部坐标转换回 WGS84 经纬度坐标。
//
// 返回:
// - *geom.LineString: 带 XYZ 坐标的中心线。
// - error: 几何类型不支持或计算失败。
func EstimateCenterlineGeomFromSurfaceGeom(g geom.T) (*geom.LineString, error) {
	outer, err := largestPolygonOuterRing(g)
	if err != nil {
		return nil, err
	}
	if len(outer) < 3 {
		return nil, fmt.Errorf("surface outer ring has insufficient points")
	}
	pts := dedupeClosedRing64(outer)
	if len(pts) < 3 {
		return nil, fmt.Errorf("surface outer ring has insufficient unique points")
	}

	originLon, originLat, avgY := ringMeanXYZ(pts)
	scaleX := 111000.0 * math.Cos(originLat*math.Pi/180.0)
	if math.Abs(scaleX) < 1e-6 {
		scaleX = 111000.0
	}

	local2 := make([][2]float64, len(pts))
	local3 := make([][3]float64, len(pts))
	for i, p := range pts {
		x := (p[0] - originLon) * scaleX
		z := (p[2] - originLat) * 111000.0
		local2[i] = [2]float64{x, z}
		local3[i] = [3]float64{x, p[1], z}
	}

	cx, cz, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok || (axisX == 0 && axisZ == 0) {
		return nil, fmt.Errorf("failed to estimate centerline principal axis")
	}

	centerlineLocal, ok := estimateCurvedCenterlineLocal(local3, cx, cz, axisX, axisZ)
	if !ok {
		minT, maxT, ok2 := linePolygonIntersections(local2, [2]float64{cx, cz}, [2]float64{axisX, axisZ})
		if !ok2 || maxT-minT < 1e-3 {
			minT, maxT, ok2 = projectionSpan(local2, cx, cz, axisX, axisZ)
			if !ok2 {
				return nil, fmt.Errorf("failed to estimate centerline intersections")
			}
		}
		centerlineLocal = [][3]float64{
			{cx + axisX*minT, avgY, cz + axisZ*minT},
			{cx, avgY, cz},
			{cx + axisX*maxT, avgY, cz + axisZ*maxT},
		}
	}

	flat := make([]float64, 0, len(centerlineLocal)*3)
	for _, p := range centerlineLocal {
		flat = append(flat,
			originLon+p[0]/scaleX,
			originLat+p[2]/111000.0,
			p[1],
		)
	}
	return geom.NewLineStringFlat(geom.XYZ, flat), nil
}

// 根据道路面局部坐标估算弯曲中心线。
//
// 方法:
// 1. 根据 PCA 主方向计算道路长度方向范围。
// 2. 沿主方向等间隔生成采样位置。
// 3. 每个采样位置沿垂直方向切割道路面。
// 4. 使用横断面交点的中点作为中心线点。
// 5. 对生成的中心线点进行去重和平滑处理。
//
// 参数:
// poly:
//
//	道路面局部三维点集合。
//
// cx, cz:
//
//	PCA 中心点。
//
// axisX, axisZ:
//
//	道路主方向单位向量。
//
// 返回:
// - 中心线采样点集合。
// - 是否成功生成中心线。
func estimateCurvedCenterlineLocal(poly [][3]float64, cx, cz, axisX, axisZ float64) ([][3]float64, bool) {
	if len(poly) < 3 {
		return nil, false
	}
	minT, maxT, ok := projectionSpan64(poly, cx, cz, axisX, axisZ)
	if !ok {
		return nil, false
	}
	span := maxT - minT
	if span <= 1e-3 {
		return nil, false
	}

	count := int(math.Ceil(span / 4.0))
	if count < 8 {
		count = 8
	}
	if count > 48 {
		count = 48
	}
	perpX, perpZ := -axisZ, axisX
	points := make([][3]float64, 0, count+1)
	for i := 0; i <= count; i++ {
		t := minT + span*float64(i)/float64(count)
		base := [2]float64{cx + axisX*t, cz + axisZ*t}
		if p, ok := centerlinePointAtSample(poly, base, [2]float64{perpX, perpZ}); ok {
			points = append(points, p)
		}
	}
	points = dedupeCurvePoints(points, 1.0)
	if len(points) < 3 {
		return nil, false
	}
	smoothCenterlinePoints(points)
	return points, true
}

// 在指定道路横断面位置计算中心点。
//
// 功能:
// 1. 使用无限直线与道路边界线段求交。
// 2. 收集所有有效交点。
// 3. 按横断面方向排序交点。
// 4. 找到中心线两侧最近的一对边界点。
// 5. 返回两个边界点之间的中点作为中心点。
//
// 参数:
// poly:
//
//	道路边界三维点。
//
// base:
//
//	横断面中心参考位置。
//
// dir:
//
//	横断面方向向量。
//
// 返回:
// - [3]float64: 中心点坐标。
// - bool: 是否成功计算。
func centerlinePointAtSample(poly [][3]float64, base, dir [2]float64) ([3]float64, bool) {
	hits := make([]sampleHit, 0, 8)
	for i := 0; i < len(poly); i++ {
		a := poly[i]
		b := poly[(i+1)%len(poly)]
		u, v, ok := infiniteLineSegmentIntersectionUV(base, dir, [2]float64{a[0], a[2]}, [2]float64{b[0], b[2]})
		if !ok {
			continue
		}
		y := a[1] + (b[1]-a[1])*v
		hits = append(hits, sampleHit{u: u, y: y})
	}
	if len(hits) < 2 {
		return [3]float64{}, false
	}
	sortSampleHitsByU(hits)
	hits = dedupeSampleHits(hits, 0.2)
	if len(hits) < 2 {
		return [3]float64{}, false
	}
	left, right, ok := nearestBracketSampleHits(hits)
	if !ok {
		return [3]float64{}, false
	}
	midu := (left.u + right.u) * 0.5
	return [3]float64{
		base[0] + dir[0]*midu,
		(left.y + right.y) * 0.5,
		base[1] + dir[1]*midu,
	}, true
}

// 从横断面交点集合中寻找位于中心位置两侧的最近交点。
//
// 选择规则:
// 1. 优先寻找:
//   - u <= 0 的最近左侧点。
//   - u >= 0 的最近右侧点。
//
// 2. 如果无法找到，则退化选择最外侧两个交点。
//
// 返回:
// - 左侧交点。
// - 右侧交点。
// - 是否找到有效组合。
func nearestBracketSampleHits(hits []sampleHit) (sampleHit, sampleHit, bool) {
	leftIdx := -1
	rightIdx := -1
	bestLeft := math.MaxFloat64
	bestRight := math.MaxFloat64
	for i, h := range hits {
		if h.u <= 0 {
			d := math.Abs(h.u)
			if d < bestLeft {
				bestLeft = d
				leftIdx = i
			}
		}
		if h.u >= 0 {
			d := math.Abs(h.u)
			if d < bestRight {
				bestRight = d
				rightIdx = i
			}
		}
	}
	if leftIdx >= 0 && rightIdx >= 0 && leftIdx != rightIdx {
		return hits[leftIdx], hits[rightIdx], true
	}
	if len(hits) >= 2 {
		return hits[0], hits[len(hits)-1], true
	}
	return sampleHit{}, sampleHit{}, false
}

// 计算无限直线与线段的二维交点。
//
// 参数:
// center:
//
//	无限直线上的参考点。
//
// dir:
//
//	无限直线方向向量。
//
// a,b:
//
//	线段两个端点。
//
// 返回:
// u:
//
//	交点在无限直线上的参数。
//
// v:
//
//	交点在线段上的比例参数。
//
// 当:
// - 两条线平行。
// - 交点不在线段范围内。
//
// 返回 false。
func infiniteLineSegmentIntersectionUV(center, dir, a, b [2]float64) (float64, float64, bool) {
	sx := b[0] - a[0]
	sz := b[1] - a[1]
	den := dir[0]*sz - dir[1]*sx
	if math.Abs(den) < 1e-9 {
		return 0, 0, false
	}
	rx := a[0] - center[0]
	rz := a[1] - center[1]
	u := (rx*sz - rz*sx) / den
	v := (rx*dir[1] - rz*dir[0]) / den
	if v < -1e-6 || v > 1+1e-6 {
		return 0, 0, false
	}
	return u, v, true
}

// 对生成的中心线点进行去重处理。
//
// 功能:
// 1. 按顺序遍历中心线点。
// 2. 删除距离前一个保留点过近的点。
// 3. 保留中心线整体方向和形状。
//
// 参数:
// points:
//
//	待处理的中心线点集合。
//
// minDist:
//
//	相邻点允许的最小距离。
//
// 返回:
// 去除冗余点后的中心线点集合。
func dedupeCurvePoints(points [][3]float64, minDist float64) [][3]float64 {
	if len(points) == 0 {
		return nil
	}
	out := make([][3]float64, 0, len(points))
	out = append(out, points[0])
	for i := 1; i < len(points); i++ {
		prev := out[len(out)-1]
		if math.Hypot(points[i][0]-prev[0], points[i][2]-prev[2]) < minDist {
			continue
		}
		out = append(out, points[i])
	}
	return out
}

// 对中心线点进行简单平滑处理。
//
// 使用三点加权平均:
// 当前点权重为 2，前后点权重为 1。
//
// 目的:
// - 减少横断面采样造成的局部抖动。
// - 提升中心线曲线连续性。
//
// 注意:
// 点数量少于5个时不进行平滑。
func smoothCenterlinePoints(points [][3]float64) {
	if len(points) < 5 {
		return
	}
	src := append([][3]float64(nil), points...)
	for i := 1; i < len(points)-1; i++ {
		points[i][0] = (src[i-1][0] + 2*src[i][0] + src[i+1][0]) * 0.25
		points[i][1] = (src[i-1][1] + 2*src[i][1] + src[i+1][1]) * 0.25
		points[i][2] = (src[i-1][2] + 2*src[i][2] + src[i+1][2]) * 0.25
	}
}

// 计算三维点集合在指定方向轴上的投影范围。
//
// 功能:
// 将所有点投影到 PCA 主方向轴上，计算:
// - 最小投影值。
// - 最大投影值。
//
// 用于:
// - 确定中心线采样起止范围。
//
// 参数:
// points:
//
//	三维局部坐标点。
//
// cx,cz:
//
//	主方向中心点。
//
// axisX,axisZ:
//
//	主方向单位向量。
//
// 返回:
// minT:
//
//	最小投影距离。
//
// maxT:
//
//	最大投影距离。
func projectionSpan64(points [][3]float64, cx, cz, axisX, axisZ float64) (float64, float64, bool) {
	if len(points) == 0 {
		return 0, 0, false
	}
	minT := math.MaxFloat64
	maxT := -math.MaxFloat64
	for _, p := range points {
		t := (p[0]-cx)*axisX + (p[2]-cz)*axisZ
		if t < minT {
			minT = t
		}
		if t > maxT {
			maxT = t
		}
	}
	return minT, maxT, maxT > minT
}

// 深复制 FeatureFields 属性集合。
//
// 功能:
// 创建新的字段 Map，避免修改原始 Feature 的属性。
//
// 参数:
// fields:
//
//	原始属性字段。
//
// 返回:
// 独立复制后的属性字段。
func cloneFeatureFields(fields FeatureFields) FeatureFields {
	if fields == nil {
		return FeatureFields{}
	}
	out := make(FeatureFields, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// 从 Geometry 中提取面积最大的 Polygon 外环。
//
// 支持:
// - Polygon
// - MultiPolygon
//
// 对 MultiPolygon:
// 遍历所有 Polygon，计算二维面积，
// 返回面积最大的外边界环。
//
// 用途:
// 道路面可能包含多个区域时，选择主要道路区域进行中心线计算。
func largestPolygonOuterRing(g geom.T) ([][3]float64, error) {
	switch gg := g.(type) {
	case *geom.Polygon:
		return polygonOuterRing64(gg), nil
	case *geom.MultiPolygon:
		var best [][3]float64
		bestArea := -1.0
		for i := 0; i < gg.NumPolygons(); i++ {
			ring := polygonOuterRing64(gg.Polygon(i))
			area := math.Abs(polygonAreaLonLat64(ring))
			if area > bestArea {
				bestArea = area
				best = ring
			}
		}
		if len(best) == 0 {
			return nil, fmt.Errorf("multipolygon has no polygon")
		}
		return best, nil
	default:
		return nil, fmt.Errorf("unsupported surface geom type %T", g)
	}
}

// 提取 Polygon 的第一个外部 Ring，并转换为三维点数组。
//
// 输入:
// geom.Polygon
//
// 输出:
// [][3]float64:
//
//	每个点格式:
//	[longitude, height, latitude]
//
// 支持:
// - XY Polygon
// - XYZ Polygon
//
// 如果没有外环，则返回空结果。
func polygonOuterRing64(poly *geom.Polygon) [][3]float64 {
	flat := poly.FlatCoords()
	ends := poly.Ends()
	if len(ends) == 0 {
		return nil
	}
	stride := poly.Stride()
	out := make([][3]float64, 0, ends[0]/stride)
	for i := 0; i < ends[0]; i += stride {
		p := [3]float64{flat[i], 0, flat[i+1]}
		if stride >= 3 {
			p[1] = flat[i+2]
		}
		out = append(out, p)
	}
	return out
}

// 对闭合 Polygon Ring 进行重复点清理。
//
// 功能:
// 1. 删除连续重复点。
// 2. 删除最后一个与第一个重复的闭合点。
//
// 目的:
// 避免 Polygon 环在后续 PCA 和采样计算中产生异常。
func dedupeClosedRing64(points [][3]float64) [][3]float64 {
	if len(points) == 0 {
		return nil
	}
	out := make([][3]float64, 0, len(points))
	var last [3]float64
	hasLast := false
	for _, p := range points {
		if hasLast && almostEqualPoint64(last, p) {
			continue
		}
		out = append(out, p)
		last = p
		hasLast = true
	}
	if len(out) > 1 && almostEqualPoint64(out[0], out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out
}

// 判断两个三维坐标点是否近似相等。
//
// 比较:
// X:
//
//	精度 1e-12
//
// Y:
//
//	精度 1e-6
//
// Z:
//
//	精度 1e-12
//
// 用于:
// - Ring 去重。
// - 几何点匹配。
func almostEqualPoint64(a, b [3]float64) bool {
	return math.Abs(a[0]-b[0]) < 1e-12 && math.Abs(a[1]-b[1]) < 1e-6 && math.Abs(a[2]-b[2]) < 1e-12
}

// 计算点集合的平均中心坐标。
//
// 返回:
// lon:
//
//	X 坐标平均值。
//
// lat:
//
//	Z 坐标平均值。
//
// y:
//
//	高程平均值。
//
// 注意:
// 内部输入格式为:
// [longitude, height, latitude]
//
// 返回顺序调整为:
// longitude, latitude, height
func ringMeanXYZ(points [][3]float64) (float64, float64, float64) {
	var sx, sy, sz float64
	for _, p := range points {
		sx += p[0]
		sy += p[1]
		sz += p[2]
	}
	n := float64(len(points))
	return sx / n, sz / n, sy / n
}

// 使用 PCA 计算二维点集的主要方向轴。
//
// 功能:
// 1. 计算点集中心。
// 2. 构建协方差矩阵。
// 3. 求最大特征值对应的特征向量。
// 4. 返回点集主要方向。
//
// 用途:
// 道路面中心线估计时，确定道路长度方向。
//
// 返回:
// cx,cz:
//
//	点集中心。
//
// axisX,axisZ:
//
//	主方向单位向量。
//
// bool:
//
//	是否计算成功。
func principalAxis2D(points [][2]float64) (float64, float64, float64, float64, bool) {
	if len(points) < 2 {
		return 0, 0, 0, 0, false
	}
	var cx, cz float64
	for _, p := range points {
		cx += p[0]
		cz += p[1]
	}
	n := float64(len(points))
	cx /= n
	cz /= n

	var sxx, szz, sxz float64
	for _, p := range points {
		dx := p[0] - cx
		dz := p[1] - cz
		sxx += dx * dx
		szz += dz * dz
		sxz += dx * dz
	}
	if sxx+szz <= 1e-9 {
		return 0, 0, 0, 0, false
	}
	trace := sxx + szz
	det := sxx*szz - sxz*sxz
	delta := trace*trace - 4*det
	if delta < 0 {
		delta = 0
	}
	lambda := 0.5 * (trace + math.Sqrt(delta))
	var vx, vz float64
	if math.Abs(sxz) > 1e-9 {
		vx = lambda - szz
		vz = sxz
	} else if sxx >= szz {
		vx, vz = 1, 0
	} else {
		vx, vz = 0, 1
	}
	lenv := math.Hypot(vx, vz)
	if lenv <= 1e-9 {
		return 0, 0, 0, 0, false
	}
	return cx, cz, vx / lenv, vz / lenv, true
}

// 计算指定方向无限直线与 Polygon 边界的交点范围。
//
// 功能:
// 1. 遍历 Polygon 所有边。
// 2. 计算无限直线与每条边的交点。
// 3. 收集所有有效投影参数。
// 4. 返回最小和最大方向参数。
//
// 用途:
// 当无法通过横断面采样生成曲线中心线时，
// 使用该结果确定道路主方向上的中心线范围。
//
// 参数:
// poly:
//
//	二维 Polygon 边界点。
//
// center:
//
//	无限直线中心点。
//
// dir:
//
//	无限直线方向。
//
// 返回:
// minT:
//
//	起点投影参数。
//
// maxT:
//
//	终点投影参数。
func linePolygonIntersections(poly [][2]float64, center, dir [2]float64) (float64, float64, bool) {
	ts := make([]float64, 0, len(poly))
	for i := 0; i < len(poly); i++ {
		a := poly[i]
		b := poly[(i+1)%len(poly)]
		t, ok := infiniteLineSegmentIntersection(center, dir, a, b)
		if ok {
			ts = append(ts, t)
		}
	}
	if len(ts) < 2 {
		return 0, 0, false
	}
	sortFloat64s(ts)
	return ts[0], ts[len(ts)-1], true
}

// 计算二维无限直线和线段的交点。
//
// 与 infiniteLineSegmentIntersectionUV 类似，
// 但只返回无限直线方向上的参数 t。
//
// 参数:
// center:
//
//	无限直线上的参考点。
//
// dir:
//
//	无限直线方向向量。
//
// a,b:
//
//	线段两个端点。
//
// 返回:
// t:
//
//	交点在无限直线上的位置参数。
//
// bool:
//
//	是否存在有效交点。
func infiniteLineSegmentIntersection(center, dir, a, b [2]float64) (float64, bool) {
	sx := b[0] - a[0]
	sz := b[1] - a[1]
	den := dir[0]*sz - dir[1]*sx
	if math.Abs(den) < 1e-9 {
		return 0, false
	}
	rx := a[0] - center[0]
	rz := a[1] - center[1]
	t := (rx*sz - rz*sx) / den
	u := (rx*dir[1] - rz*dir[0]) / den
	if u < -1e-6 || u > 1+1e-6 {
		return 0, false
	}
	return t, true
}

// 计算二维点集合沿指定方向的投影范围。
//
// 功能:
// 将二维点投影到道路主方向轴，
// 找到:
// - 最小投影距离。
// - 最大投影距离。
//
// 用途:
// PCA 直线中心线生成时，
// 确定中心线覆盖范围。
//
// 参数:
// points:
//
//	二维坐标点集合。
//
// cx,cz:
//
//	PCA 中心点。
//
// axisX,axisZ:
//
//	主方向单位向量。
//
// 返回:
// minT,maxT:
//
//	投影范围。
func projectionSpan(points [][2]float64, cx, cz, axisX, axisZ float64) (float64, float64, bool) {
	if len(points) == 0 {
		return 0, 0, false
	}
	minT := math.MaxFloat64
	maxT := -math.MaxFloat64
	for _, p := range points {
		t := (p[0]-cx)*axisX + (p[1]-cz)*axisZ
		if t < minT {
			minT = t
		}
		if t > maxT {
			maxT = t
		}
	}
	return minT, maxT, maxT > minT
}

// 计算二维经纬度 Polygon 面积。
//
// 使用鞋带公式计算:
//
// area = 1/2 * Σ(x_i*y_(i+1)-x_(i+1)*y_i)
//
// 用途:
// MultiPolygon 中选择最大面积 Polygon。
//
// 参数:
// points:
//
//	Polygon 外环点集合。
//
// 返回:
// Polygon 有符号面积。
func polygonAreaLonLat64(points [][3]float64) float64 {
	if len(points) < 3 {
		return 0
	}
	var area float64
	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		area += points[i][0]*points[j][2] - points[j][0]*points[i][2]
	}
	return area * 0.5
}

// 对 float64 数组进行升序排序。
//
// 使用插入排序实现。
//
// 用途:
// 对道路边界交点参数进行排序。
//
// 参数:
// values:
//
//	待排序浮点数组。
func sortFloat64s(values []float64) {
	for i := 1; i < len(values); i++ {
		v := values[i]
		j := i - 1
		for j >= 0 && values[j] > v {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = v
	}
}

// 表示道路横断面与边界相交产生的采样点。
//
// 字段:
// u:
//
//	交点在线方向上的参数距离。
//
// y:
//
//	交点高度值。
//
// 用途:
// 在中心线计算过程中，
// 保存横断面的边界交点信息。
type sampleHit struct {
	u float64
	y float64
}

// 对横断面采样交点进行去重。
//
// 功能:
// 当多个 Polygon 边在同一位置产生近似交点时:
// 1. 合并相近的 u 参数。
// 2. 同时平均对应高度值。
//
// 参数:
// hits:
//
//	横断面交点集合。
//
// tol:
//
//	判断重复交点的距离阈值。
//
// 返回:
// 去重后的交点集合。
func dedupeSampleHits(hits []sampleHit, tol float64) []sampleHit {
	if len(hits) == 0 {
		return nil
	}
	out := hits[:1]
	for i := 1; i < len(hits); i++ {
		if math.Abs(hits[i].u-out[len(out)-1].u) <= tol {
			out[len(out)-1].u = (out[len(out)-1].u + hits[i].u) * 0.5
			out[len(out)-1].y = (out[len(out)-1].y + hits[i].y) * 0.5
			continue
		}
		out = append(out, hits[i])
	}
	return out
}

// 根据横断面方向参数 u 对交点排序。
//
// 排序规则:
// u 从小到大排列。
//
// 用途:
// 方便寻找道路横断面左右边界点。
//
// 参数:
// hits:
//
//	横断面交点列表。
func sortSampleHitsByU(hits []sampleHit) {
	for i := 1; i < len(hits); i++ {
		v := hits[i]
		j := i - 1
		for j >= 0 && hits[j].u > v.u {
			hits[j+1] = hits[j]
			j--
		}
		hits[j+1] = v
	}
}

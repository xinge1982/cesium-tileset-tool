package aabb

import "math"

// ---------- WGS84 椭球 ----------
const (
	aWGS84  = 6378137.0
	fWGS84  = 1.0 / 298.257223563
	e2WGS84 = fWGS84 * (2 - fWGS84)
)

// ---------- 小工具 ----------
func deg2rad(d float64) float64 { return d * math.Pi / 180.0 }
func rad2deg(r float64) float64 { return r * 180.0 / math.Pi }

//
//// 4x4（列主序）相乘：r = a * b
//func mul4x4(a, b [16]float64) [16]float64 {
//	var r [16]float64
//	for c := 0; c < 4; c++ {
//		for r0 := 0; r0 < 4; r0++ {
//			r[c*4+r0] = a[0*4+r0]*b[c*4+0] +
//				a[1*4+r0]*b[c*4+1] +
//				a[2*4+r0]*b[c*4+2] +
//				a[3*4+r0]*b[c*4+3]
//		}
//	}
//	return r
//}

// 4x4（列主序）* 点（w=1）
func mulPoint4x4(m [16]float64, p [3]float64) (x, y, z float64) {
	x = m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12]
	y = m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13]
	z = m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14]
	return
}

// 组装 TRS（列主序），等价于 M = T * R * S（这里 T 传 [0,0,0]）
func composeTRS64(T [3]float64, Q [4]float64, S [3]float64) [16]float64 {
	// 四元数单位化
	x, y, z, w := Q[0], Q[1], Q[2], Q[3]
	n := math.Sqrt(x*x + y*y + z*z + w*w)
	if n == 0 {
		w = 1
		x, y, z = 0, 0, 0
	} else {
		x, y, z, w = x/n, y/n, z/n, w/n
	}

	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z

	// 旋转（无缩放），列主序
	r00 := 1 - 2*(yy+zz)
	r01 := 2 * (xy + wz)
	r02 := 2 * (xz - wy)
	r10 := 2 * (xy - wz)
	r11 := 1 - 2*(xx+zz)
	r12 := 2 * (yz + wx)
	r20 := 2 * (xz + wy)
	r21 := 2 * (yz - wx)
	r22 := 1 - 2*(xx+yy)

	sx, sy, sz := S[0], S[1], S[2] // 列缩放
	return [16]float64{
		r00 * sx, r10 * sx, r20 * sx, 0,
		r01 * sy, r11 * sy, r21 * sy, 0,
		r02 * sz, r12 * sz, r22 * sz, 0,
		T[0], T[1], T[2], 1,
	}
}

// ENU->ECEF（3x3）提升为 4x4（列主序）
func enuToEcef4(lonRad, latRad float64) [16]float64 {
	sλ, cλ := math.Sin(lonRad), math.Cos(lonRad)
	sφ, cφ := math.Sin(latRad), math.Cos(latRad)
	// ENU->ECEF = (ECEF->ENU)^T
	// 3x3:
	// [-sinλ,      -cosλ sinφ,  cosλ cosφ;
	//   cosλ,      -sinλ sinφ,  sinλ cosφ;
	//   0,              cosφ,        sinφ]
	return [16]float64{
		-sλ, -cλ * sφ, cλ * cφ, 0,
		cλ, -sλ * sφ, sλ * cφ, 0,
		0, cφ, sφ, 0,
		0, 0, 0, 1,
	}
}

// 大地→ECEF
func geodeticToECEF(lonRad, latRad, h float64) (x, y, z float64) {
	sλ, cλ := math.Sin(lonRad), math.Cos(lonRad)
	sφ, cφ := math.Sin(latRad), math.Cos(latRad)
	N := aWGS84 / math.Sqrt(1-e2WGS84*sφ*sφ)
	x = (N + h) * cφ * cλ
	y = (N + h) * cφ * sλ
	z = (N*(1-e2WGS84) + h) * sφ
	return
}

// ECEF→大地（Bowring）
func ecefToGeodetic(x, y, z float64) (lonRad, latRad, h float64) {
	b := aWGS84 * (1 - fWGS84)
	ep2 := (aWGS84*aWGS84 - b*b) / (b * b)

	lonRad = math.Atan2(y, x)
	p := math.Hypot(x, y)
	if p < 1e-12 {
		latRad = math.Copysign(math.Pi/2, z)
		h = math.Abs(z) - b
		return
	}
	theta := math.Atan2(z*aWGS84, p*b)
	st, ct := math.Sin(theta), math.Cos(theta)

	latRad = math.Atan2(z+ep2*b*st*st*st, p-e2WGS84*aWGS84*ct*ct*ct)
	sinLat := math.Sin(latRad)
	N := aWGS84 / math.Sqrt(1-e2WGS84*sinLat*sinLat)
	h = p/math.Cos(latRad) - N
	return
}

// AABB 采样：8 角 + 12 边中点 = 20 点
func sampleAABB20(b Box) [][3]float64 {
	min, max := b.Min, b.Max
	c := [8][3]float64{
		{min[0], min[1], min[2]}, {max[0], min[1], min[2]},
		{min[0], max[1], min[2]}, {max[0], max[1], min[2]},
		{min[0], min[1], max[2]}, {max[0], min[1], max[2]},
		{min[0], max[1], max[2]}, {max[0], max[1], max[2]},
	}
	pts := make([][3]float64, 0, 20)
	pts = append(pts, c[:]...)
	edges := [][2]int{
		{0, 1}, {0, 2}, {1, 3}, {2, 3},
		{4, 5}, {4, 6}, {5, 7}, {6, 7},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	}
	for _, e := range edges {
		a, b := c[e[0]], c[e[1]]
		pts = append(pts, [3]float64{(a[0] + b[0]) * 0.5, (a[1] + b[1]) * 0.5, (a[2] + b[2]) * 0.5})
	}
	return pts
}

// 经度解缠绕：把一组弧度经度展开到以 ref 为中心的连续域
func unwrapLongitudes(lons []float64, ref float64) {
	ref = math.Atan2(math.Sin(ref), math.Cos(ref))
	for i := range lons {
		L := math.Atan2(math.Sin(lons[i]), math.Cos(lons[i]))
		k := math.Round((ref - L) / (2 * math.Pi))
		lons[i] = L + k*2*math.Pi
	}
}

func rotX(deg float64) [16]float64 {
	θ := deg2rad(deg)
	c, s := math.Cos(θ), math.Sin(θ)
	// 列主序，右乘列向量
	return [16]float64{
		1, 0, 0, 0,
		0, c, s, 0,
		0, -s, c, 0,
		0, 0, 0, 1,
	}
}

// ---------------- 主函数 ----------------
// 输入：模型整体 AABB（box，单位米，ENU 轴向）、WGS84 位置（度/米）、旋转四元数 R、缩放 S、留白 buffer（米）
// 输出：region = [west,south,east,north,minH,maxH]（经纬弧度 / 高程米）
func RegionFromBoxPoseWGS84(
	box Box,
	originLonDeg, originLatDeg, originH float64,
	R [4]float64, S [3]float64,
	bufferMeters float64,
) [6]float64 {

	var region [6]float64
	if !box.Valid() {
		return region
	}

	// 1) 位置（WGS84）→ 平移(ECEF)
	lonRad := deg2rad(originLonDeg)
	latRad := deg2rad(originLatDeg)
	x0, y0, z0 := geodeticToECEF(lonRad, latRad, originH)
	T := [16]float64{
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x0, y0, z0, 1,
	}

	// 2) ENU->ECEF（只旋不缩放）
	Renu := enuToEcef4(lonRad, latRad)

	// 3) 模型自身 R 与 S（不含平移）
	Mrs := composeTRS64([3]float64{0, 0, 0}, R, S) // 列主序 = R * S

	//Y轴向上的专门方法
	Ralign := rotX(90) // Y-up → Z-up(ENU)

	// 4) 组合 transform：T * Renu * (R * S)
	M := mul4x4(mul4x4(mul4x4(T, Renu), Ralign), Mrs) // Mrs=composeTRS64(0,R,S)（R 为 Y-up 下的旋转）

	// 5) 采样 AABB → 乘 M 得 ECEF → 转 WGS84 → 取范围
	samples := sampleAABB20(box)
	lons := make([]float64, 0, len(samples))
	lats := make([]float64, 0, len(samples))
	hs := make([]float64, 0, len(samples))

	for _, p := range samples {
		x, y, z := mulPoint4x4(M, p) // ECEF
		lon, lat, h := ecefToGeodetic(x, y, z)
		lons = append(lons, lon)
		lats = append(lats, lat)
		hs = append(hs, h)
	}

	// 参考经度用于解缠绕
	lonRef, _, _ := ecefToGeodetic(x0, y0, z0)
	unwrapLongitudes(lons, lonRef)

	// 6) 极值 + buffer
	w, s, e, n := +math.Inf(1), +math.Inf(1), -math.Inf(1), -math.Inf(1)
	hmin, hmax := +math.Inf(1), -math.Inf(1)
	for i := range lons {
		if lons[i] < w {
			w = lons[i]
		}
		if lons[i] > e {
			e = lons[i]
		}
		if lats[i] < s {
			s = lats[i]
		}
		if lats[i] > n {
			n = lats[i]
		}
		if hs[i] < hmin {
			hmin = hs[i]
		}
		if hs[i] > hmax {
			hmax = hs[i]
		}
	}

	if bufferMeters > 0 {
		dLat := bufferMeters / aWGS84
		clat := math.Cos(latRad)
		dLon := bufferMeters / (aWGS84 * math.Max(1e-6, clat))
		w -= dLon
		e += dLon
		s -= dLat
		n += dLat
		hmin -= bufferMeters
		hmax += bufferMeters
	}

	region[0], region[1], region[2], region[3], region[4], region[5] = w, s, e, n, hmin, hmax
	return region
}

// ----------------- 主函数：AABB + transform -> region -----------------

// 输入：
//
//	box            : 模型整体 AABB（局部米制，与 tileset.transform 同一局部）
//	transform      : tileset.root.transform（列主序 4×4，已验证正确）
//	bufferMeters   : 额外留白（米），会体现在：东西/南北（换算成弧度）以及 minH/maxH（±米）
//
// 输出：
//
//	[west, south, east, north, minH, maxH] —— 经纬为“弧度”，高程为“米”
func RegionFromBoxAndTransform(box Box, transform [16]float64, bufferMeters float64) [6]float64 {
	var region [6]float64
	if !box.Valid() {
		return region // 全 0
	}

	// 参考点：取 transform 的平移作为 ECEF 原点，换到经纬用来做经度解缠绕 & 水平 buffer 换算
	lon0, lat0, _ := ecefToGeodetic(transform[12], transform[13], transform[14])

	// 采样 20 点 → 变换到 ECEF → 转 WGS84
	samples := sampleAABB20(box)
	lons := make([]float64, 0, len(samples))
	lats := make([]float64, 0, len(samples))
	hs := make([]float64, 0, len(samples))

	for _, p := range samples {
		x, y, z := mulPoint4x4(transform, p)
		lon, lat, h := ecefToGeodetic(x, y, z)
		lons = append(lons, lon)
		lats = append(lats, lat)
		hs = append(hs, h)
	}

	// 经度解缠绕（跨 ±180° 安全）
	unwrapLongitudes(lons, lon0)

	// 取极值
	w := +math.Inf(1)
	s := +math.Inf(1)
	e := -math.Inf(1)
	n := -math.Inf(1)
	hmin := +math.Inf(1)
	hmax := -math.Inf(1)
	for i := range lons {
		if lons[i] < w {
			w = lons[i]
		}
		if lons[i] > e {
			e = lons[i]
		}
		if lats[i] < s {
			s = lats[i]
		}
		if lats[i] > n {
			n = lats[i]
		}
		if hs[i] < hmin {
			hmin = hs[i]
		}
		if hs[i] > hmax {
			hmax = hs[i]
		}
	}

	// buffer：水平（弧度）+ 垂直（米）
	if bufferMeters > 0 {
		dLat := bufferMeters / aWGS84
		clat := math.Cos(lat0)
		dLon := bufferMeters / (aWGS84 * math.Max(1e-6, clat)) // 极区保护
		w -= dLon
		e += dLon
		s -= dLat
		n += dLat
		hmin -= bufferMeters
		hmax += bufferMeters
	}

	region[0] = w
	region[1] = s
	region[2] = e
	region[3] = n
	region[4] = hmin
	region[5] = hmax
	return region
}

func WGS84BoxToRegionDeg(westDeg, southDeg, eastDeg, northDeg, minH, maxH float64) [6]float64 {
	if westDeg > eastDeg {
		westDeg, eastDeg = eastDeg, westDeg
	}
	if southDeg > northDeg {
		southDeg, northDeg = northDeg, southDeg
	}
	if southDeg < -90 {
		southDeg = -90
	}
	if northDeg > 90 {
		northDeg = 90
	}

	return [6]float64{
		deg2rad(westDeg),
		deg2rad(southDeg),
		deg2rad(eastDeg),
		deg2rad(northDeg),
		minH, maxH,
	}
}

// region = [west, south, east, north, minH, maxH]
// 输入经纬度（度）与高度（米）；bufferM（米）可选：不传则为 0
func WGS84BoxToRegionDegBuf(
	westDeg, southDeg, eastDeg, northDeg, minH, maxH float64,
	bufferM ...int,
) [6]float64 {
	// 规范顺序
	if westDeg > eastDeg {
		westDeg, eastDeg = eastDeg, westDeg
	}
	if southDeg > northDeg {
		southDeg, northDeg = northDeg, southDeg
	}

	// 处理可选 buffer（米）
	buf := 0
	if len(bufferM) > 0 {
		buf = bufferM[0]
	}
	b := math.Abs(float64(buf)) // 允许传负数，取绝对值

	if b > 0 {
		const R = 6378137.0 // WGS84 半径（近似）；也可用更复杂的公式
		// 小角度近似：dLat = b/R, dLon = b/(R*cosφ)
		dLatDeg := rad2deg(b / R)
		// 计算中心纬度（用于经度方向换算）
		latC := (southDeg + northDeg) * 0.5
		cosφ := math.Cos(deg2rad(latC))
		var dLonDeg float64
		if math.Abs(cosφ) < 1e-12 {
			dLonDeg = 0 // 极区保护（在中国用不到）
		} else {
			dLonDeg = rad2deg(b / (R * cosφ))
		}

		westDeg -= dLonDeg
		eastDeg += dLonDeg
		southDeg -= dLatDeg
		northDeg += dLatDeg

		minH -= b
		maxH += b
	}

	// 夹紧纬度
	if southDeg < -90 {
		southDeg = -90
	}
	if northDeg > 90 {
		northDeg = 90
	}

	// 输出弧度/米
	return [6]float64{
		deg2rad(westDeg),
		deg2rad(southDeg),
		deg2rad(eastDeg),
		deg2rad(northDeg),
		minH, maxH,
	}
}

package aabb

import (
	"cesium-tileset-tool/mapmodel/utm"
	"errors"
	"fmt"
	"math"

	"github.com/qmuntal/gltf"
)

func ecefToEnuR(lon, lat float64) [9]float64 {
	sλ, cλ := math.Sin(lon), math.Cos(lon)
	sφ, cφ := math.Sin(lat), math.Cos(lat)
	// 列主序（column-major）展开：每三项是一列
	// col0 = [-sinλ, -sinφ*cosλ,  cosφ*cosλ]
	// col1 = [ cosλ, -sinφ*sinλ,  cosφ*sinλ]
	// col2 = [    0,        cosφ,         sinφ]
	return [9]float64{
		-sλ, -sφ * cλ, cφ * cλ,
		cλ, -sφ * sλ, cφ * sλ,
		0.0, cφ, sφ,
	}
}
func mul3x3v3(R [9]float64, v [3]float64) (o [3]float64) {
	o[0] = R[0]*v[0] + R[3]*v[1] + R[6]*v[2] // East
	o[1] = R[1]*v[0] + R[4]*v[1] + R[7]*v[2] // North
	o[2] = R[2]*v[0] + R[5]*v[1] + R[8]*v[2] // Up
	return
}

// ---- ENU 框架：一次设原点，多次算偏移 ----
type ENUFrame struct {
	lon0, lat0     float64 // 弧度
	x0, y0, z0     float64 // ECEF(m)
	sλ, cλ, sφ, cφ float64 // 参考点的正余弦
}

func NewENUFrame(lonDeg, latDeg, h float64) (*ENUFrame, error) {
	lon0 := deg2rad(lonDeg)
	lat0 := deg2rad(latDeg)
	x0, y0, z0 := geodeticToECEF(lon0, lat0, h)
	sλ, cλ := math.Sin(lon0), math.Cos(lon0)
	sφ, cφ := math.Sin(lat0), math.Cos(lat0)
	return &ENUFrame{lon0: lon0, lat0: lat0, x0: x0, y0: y0, z0: z0, sλ: sλ, cλ: cλ, sφ: sφ, cφ: cφ}, nil
}

func (f *ENUFrame) Offset(lonDeg, latDeg, h float64) [3]float64 {
	lon := deg2rad(lonDeg)
	lat := deg2rad(latDeg)
	x, y, z := geodeticToECEF(lon, lat, h)
	dx, dy, dz := x-f.x0, y-f.y0, z-f.z0

	// 经典公式（ECEF 差分 → ENU）
	e := -f.sλ*dx + f.cλ*dy
	n := -(-f.sφ*f.cλ*dx - f.sφ*f.sλ*dy + f.cφ*dz)
	u := f.cφ*f.cλ*dx + f.cφ*f.sλ*dy + f.sφ*dz
	return [3]float64{e, n, u}
}

type UTMFrame struct {
	x, y, z float64
	proj    utm.MyProjection
}

func NewUTMFrame(lng, lat, h float64) (*UTMFrame, error) {
	crs := &utm.CRSWgs84utm{}
	utmFrame := &UTMFrame{}
	var epsg = LatLonToUTMEPSG(lng, lat)
	utmFrame.proj = crs.GetProjection(fmt.Sprintf("epsg:%d", epsg))
	if utmFrame.proj == nil {
		return nil, errors.New("proj is nil")
	}
	p := utmFrame.proj.FromWgs84(lng, lat)
	utmFrame.x = p[0]
	utmFrame.y = p[1]
	utmFrame.z = h
	return utmFrame, nil
}

func (f *UTMFrame) Offset(lon, lat, h float64) [3]float64 {
	p := f.proj.FromWgs84(lon, lat)
	return [3]float64{p[0] - f.x, f.y - p[1], h - f.z}
}

// LatLonToUTMEPSG 根据纬度(lat) 和 经度(lon) 返回对应的 UTM EPSG 编号
func LatLonToUTMEPSG(lon, lat float64) int {
	// 经度范围处理
	if lon < -180 || lon > 180 {
		panic("longitude must be between -180 and 180")
	}
	if lat < -80 || lat > 84 {
		panic("latitude must be between -80 and 84 for UTM zones")
	}

	// 计算 UTM 带号 (1-60)
	zone := int(math.Floor((lon+180.0)/6.0)) + 1

	// 北半球 / 南半球 判断
	if lat >= 0 {
		return 32600 + zone // 北半球 326xx
	}
	return 32700 + zone // 南半球 327xx
}

// ---- 航向(正北为0，顺时针为正) → glTF四元数 [x,y,z,w]（绕+Y）----
func QuatFromHeadingDeg(headingDeg float64) [4]float64 {
	θ := -headingDeg * math.Pi / 180.0 // 顺时针为正 → 右手系取负
	h := θ * 0.5
	return [4]float64{0, math.Sin(h), 0, math.Cos(h)} // 绕 +Y
}

// 四元数乘法：q = a * b
// glTF quaternion: [x, y, z, w]
func quatMul(a, b [4]float64) [4]float64 {
	ax, ay, az, aw := a[0], a[1], a[2], a[3]
	bx, by, bz, bw := b[0], b[1], b[2], b[3]

	return [4]float64{
		aw*bx + ax*bw + ay*bz - az*by, // x
		aw*by - ax*bz + ay*bw + az*bx, // y
		aw*bz + ax*by - ay*bx + az*bw, // z
		aw*bw - ax*bx - ay*by - az*bz, // w
	}
}

// 绕单位轴的四元数
func quatFromAxisAngle(x, y, z, angleRad float64) [4]float64 {
	h := angleRad * 0.5
	s := math.Sin(h)
	return [4]float64{x * s, y * s, z * s, math.Cos(h)}
}

// ---- HPR(角度制) → glTF 四元数 [x,y,z,w] ----
//
// 约定：
//   - Heading: 正北为 0，顺时针为正
//   - Pitch:   抬头为正
//   - Roll:    右倾为正
//
// 映射到 glTF 局部轴：
//   - Heading 绕 +Y
//   - Pitch   绕 +X
//   - Roll    绕 +Z
//
// 组合顺序：Heading -> Pitch -> Roll
func QuatFromHPRDeg(headingDeg, pitchDeg, rollDeg float64) [4]float64 {
	// Heading: 顺时针为正，而右手系绕 +Y 的正角通常是逆时针，所以取负
	h := -headingDeg * math.Pi / 180.0
	p := pitchDeg * math.Pi / 180.0
	r := rollDeg * math.Pi / 180.0

	qh := quatFromAxisAngle(0, 1, 0, h) // heading about +Y
	qp := quatFromAxisAngle(1, 0, 0, p) // pitch about +X
	qr := quatFromAxisAngle(0, 0, 1, r) // roll about +Z

	// intrinsic H->P->R
	return quatMul(quatMul(qh, qp), qr)
}

// 把额外的地理 TRS (T_add/R_add/S_add) 当作“父变换”烘焙进当前 node
func BakeTRSIntoNode(nd *gltf.Node, T_add [3]float64, R_add [4]float64, S_add [3]float64) {
	// 现有局部矩阵（支持 nd.Matrix 或 TRS）
	MLocal := localMatrix(nd)
	// 额外的地理矩阵
	MAdd := composeTRS64(T_add, R_add, S_add)

	// 约定：列主序，父*子 → world；要“加一个父变换”，就是前乘
	MNew := mul4x4(MAdd, MLocal)

	// 分解回 TRS 写回 node（避免长期存 Matrix 带来兼容/体积问题）
	T, R, S := DecomposeTRS(MNew)
	nd.Matrix = gltf.DefaultMatrix
	nd.Translation = T
	nd.Rotation = R
	nd.Scale = S
}

// decomposeTRS 将列主序 4x4 仿射矩阵 m 分解为 T(平移)、R(四元数[x,y,z,w])、S(缩放)。
// 约定：m 由 composeTRS64 生成或等价（无剪切/或剪切极小）。
func DecomposeTRS(m [16]float64) (T [3]float64, R [4]float64, S [3]float64) {
	// 1) Translation（列主序第 12/13/14 元素）
	T = [3]float64{m[12], m[13], m[14]}

	// 2) 取三列作为基向量（含缩放）
	x := [3]float64{m[0], m[1], m[2]}
	y := [3]float64{m[4], m[5], m[6]}
	z := [3]float64{m[8], m[9], m[10]}

	len3 := func(v [3]float64) float64 {
		return math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	}
	sx, sy, sz := len3(x), len3(y), len3(z)

	const eps = 1e-12
	// 退化保护：若缩放太小，直接返回单位旋转
	if sx < eps || sy < eps || sz < eps {
		S = [3]float64{sx, sy, sz}
		R = [4]float64{0, 0, 0, 1}
		return
	}
	S = [3]float64{sx, sy, sz}

	// 3) 去尺度，得到旋转的三个列向量
	rx := [3]float64{x[0] / sx, x[1] / sx, x[2] / sx}
	ry := [3]float64{y[0] / sy, y[1] / sy, y[2] / sy}
	rz := [3]float64{z[0] / sz, z[1] / sz, z[2] / sz}

	// 4) 若存在反射(det<0)，把符号并入最小尺度轴，并翻转对应列向量
	det := rx[0]*(ry[1]*rz[2]-ry[2]*rz[1]) -
		rx[1]*(ry[0]*rz[2]-ry[2]*rz[0]) +
		rx[2]*(ry[0]*rz[1]-ry[1]*rz[0])

	if det < 0 {
		// 找到 |S| 最小的轴
		ax, ay, az := math.Abs(sx), math.Abs(sy), math.Abs(sz)
		if ax <= ay && ax <= az {
			sx = -sx
			S[0] = sx
			rx = [3]float64{-rx[0], -rx[1], -rx[2]}
		} else if ay <= az {
			sy = -sy
			S[1] = sy
			ry = [3]float64{-ry[0], -ry[1], -ry[2]}
		} else {
			sz = -sz
			S[2] = sz
			rz = [3]float64{-rz[0], -rz[1], -rz[2]}
		}
	}

	// 5) 正交基 → 四元数（列主序三列转元素）
	r00, r01, r02 := rx[0], ry[0], rz[0]
	r10, r11, r12 := rx[1], ry[1], rz[1]
	r20, r21, r22 := rx[2], ry[2], rz[2]

	trace := r00 + r11 + r22
	var qx, qy, qz, qw float64
	if trace > 0 {
		s := math.Sqrt(trace+1.0) * 2
		qw = 0.25 * s
		qx = (r21 - r12) / s
		qy = (r02 - r20) / s
		qz = (r10 - r01) / s
	} else if r00 > r11 && r00 > r22 {
		s := math.Sqrt(1.0+r00-r11-r22) * 2
		qw = (r21 - r12) / s
		qx = 0.25 * s
		qy = (r01 + r10) / s
		qz = (r02 + r20) / s
	} else if r11 > r22 {
		s := math.Sqrt(1.0-r00+r11-r22) * 2
		qw = (r02 - r20) / s
		qx = (r01 + r10) / s
		qy = 0.25 * s
		qz = (r12 + r21) / s
	} else {
		s := math.Sqrt(1.0-r00-r11+r22) * 2
		qw = (r10 - r01) / s
		qx = (r02 + r20) / s
		qy = (r12 + r21) / s
		qz = 0.25 * s
	}

	// 6) 归一化四元数（数值稳）
	nq := math.Sqrt(qx*qx + qy*qy + qz*qz + qw*qw)
	if nq < eps {
		R = [4]float64{0, 0, 0, 1}
	} else {
		inv := 1.0 / nq
		R = [4]float64{qx * inv, qy * inv, qz * inv, qw * inv}
	}
	return
}

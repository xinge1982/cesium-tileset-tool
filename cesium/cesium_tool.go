package cesium

import (
	"math"
)

// WGS84椭球参数
const (
	a  = 6378137.0         // 长半轴
	f  = 1 / 298.257223563 // 扁率
	b  = a * (1 - f)       // semi-minor axis
	e2 = f * (2 - f)       // 第一偏心率平方
)

type Matrix4 [16]float64

// 从经纬度 + 高程计算 ECEF 坐标
func llaToECEF(latDeg, lonDeg, height float64) [3]float64 {
	lat := latDeg * math.Pi / 180
	lon := lonDeg * math.Pi / 180

	N := a / math.Sqrt(1-e2*math.Sin(lat)*math.Sin(lat))

	x := (N + height) * math.Cos(lat) * math.Cos(lon)
	y := (N + height) * math.Cos(lat) * math.Sin(lon)
	z := ((1-e2)*N + height) * math.Sin(lat)

	return [3]float64{x, y, z}
}

// 计算 ENU 坐标系的旋转矩阵（朝向角为北向0度，顺时针）
func enuRotationMatrix(latDeg, lonDeg, headingDeg float64) [9]float64 {
	lat := latDeg * math.Pi / 180
	lon := lonDeg * math.Pi / 180
	heading := headingDeg * math.Pi / 180

	// 基础 ENU 坐标轴
	east := [3]float64{-math.Sin(lon), math.Cos(lon), 0}
	north := [3]float64{
		-math.Sin(lat) * math.Cos(lon),
		-math.Sin(lat) * math.Sin(lon),
		math.Cos(lat),
	}
	up := [3]float64{
		math.Cos(lat) * math.Cos(lon),
		math.Cos(lat) * math.Sin(lon),
		math.Sin(lat),
	}

	// 根据 heading 旋转 ENU 平面
	ch := math.Cos(heading)
	sh := math.Sin(heading)

	// East' = East * ch + North * sh
	// North' = -East * sh + North * ch
	eastRot := [3]float64{
		east[0]*ch + north[0]*sh,
		east[1]*ch + north[1]*sh,
		east[2]*ch + north[2]*sh,
	}
	northRot := [3]float64{
		-north[0]*sh + north[0]*ch,
		-north[1]*sh + north[1]*ch,
		-north[2]*sh + north[2]*ch,
	}

	// 构造列主序 3x3 旋转矩阵
	return [9]float64{
		eastRot[0], northRot[0], up[0],
		eastRot[1], northRot[1], up[1],
		eastRot[2], northRot[2], up[2],
	}
}

// 生成 4x4 transform 矩阵
func computeTransformMatrix(lon, lat, height, heading float64) [16]float64 {
	pos := llaToECEF(lat, lon, height)
	rot := enuRotationMatrix(lat, lon, heading)

	return [16]float64{
		rot[0], rot[1], rot[2], 0,
		rot[3], rot[4], rot[5], 0,
		rot[6], rot[7], rot[8], 0,
		pos[0], pos[1], pos[2], 1,
	}
}

type Quaternion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
	W float64 `json:"w"`
}

// FromAxisAngle creates a quaternion from an axis of rotation and an angle (in radians).
func FromAxisAngle(axis [3]float64, angle float64) Quaternion {
	halfAngle := angle / 2.0
	sinHalfAngle := math.Sin(halfAngle)
	cosHalfAngle := math.Cos(halfAngle)

	x := axis[0] * sinHalfAngle
	y := axis[1] * sinHalfAngle
	z := axis[2] * sinHalfAngle

	return Quaternion{x, y, z, cosHalfAngle}
}

// Multiply multiplies two quaternions.
func (q Quaternion) Multiply(r Quaternion) Quaternion {
	return Quaternion{
		X: q.W*r.X + q.X*r.W + q.Y*r.Z - q.Z*r.Y,
		Y: q.W*r.Y + q.Y*r.W + q.Z*r.X - q.X*r.Z,
		Z: q.W*r.Z + q.Z*r.W + q.X*r.Y - q.Y*r.X,
		W: q.W*r.W - q.X*r.X - q.Y*r.Y - q.Z*r.Z,
	}
}

func SameRotation(a, b Quaternion, eps float64) bool {
	dot := a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
	return math.Abs(math.Abs(dot)-1.0) < eps
}

// FromHpr creates a quaternion from heading, pitch, and roll (in degrees).
func FromHpr(headingDegrees, pitchDegrees, rollDegrees float64) Quaternion {
	// Start from identity, not zero
	totalQuaternion := Quaternion{0, 0, 0, 1}

	headingRadians := math.Pi * headingDegrees / 180.0
	pitchRadians := math.Pi * pitchDegrees / 180.0
	rollRadians := math.Pi * rollDegrees / 180.0

	if headingDegrees != 0 {
		headingQuaternion := FromAxisAngle([3]float64{0, 0, 1}, headingRadians)
		totalQuaternion = totalQuaternion.Multiply(headingQuaternion)
	}

	if pitchDegrees != 0 {
		pitchQuaternion := FromAxisAngle([3]float64{1, 0, 0}, pitchRadians)
		totalQuaternion = totalQuaternion.Multiply(pitchQuaternion)
	}

	if rollDegrees != 0 {
		rollQuaternion := FromAxisAngle([3]float64{0, 1, 0}, rollRadians)
		totalQuaternion = totalQuaternion.Multiply(rollQuaternion)
	}

	return totalQuaternion
}

// ToHpr converts a quaternion back to heading, pitch, roll (degrees).
// Assumes FromHpr does Q_total = Qz * Qx * Qy (heading -> pitch -> roll).
func ToHpr(q Quaternion) (heading, pitch, roll float64) {
	norm := math.Sqrt(q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W)
	if norm == 0 {
		return 0, 0, 0
	}

	x := q.X / norm
	y := q.Y / norm
	z := q.Z / norm
	w := q.W / norm

	// Rotation matrix from quaternion
	m00 := 1 - 2*(y*y+z*z)
	m01 := 2 * (x*y - z*w)
	m10 := 2 * (x*y + z*w)
	m11 := 1 - 2*(x*x+z*z)
	m20 := 2 * (x*z - y*w)
	m21 := 2 * (y*z + x*w)
	m22 := 1 - 2*(x*x+y*y)

	// For R = Rz(heading) * Rx(pitch) * Ry(roll):
	// m21 = sin(pitch)
	// m01 = -sin(heading) cos(pitch)
	// m11 =  cos(heading) cos(pitch)
	// m20 = -sin(roll) cos(pitch)
	// m22 =  cos(roll) cos(pitch)

	pitchRad := math.Asin(clamp(m21, -1.0, 1.0))

	// Detect gimbal lock: cos(pitch) ~= 0
	cp := math.Cos(pitchRad)
	if math.Abs(cp) > 1e-8 {
		headingRad := math.Atan2(-m01, m11)
		rollRad := math.Atan2(-m20, m22)

		heading = headingRad * 180.0 / math.Pi
		pitch = pitchRad * 180.0 / math.Pi
		roll = rollRad * 180.0 / math.Pi
	} else {
		// Gimbal lock: heading and roll are coupled
		// Choose one convention; here we set roll = 0
		// and recover heading from m10/m00
		headingRad := math.Atan2(m10, m00)

		heading = headingRad * 180.0 / math.Pi
		pitch = pitchRad * 180.0 / math.Pi
		roll = 0
	}

	heading = normalizeAngle(heading)
	pitch = normalizeAngleSigned(pitch)
	roll = normalizeAngleSigned(roll)
	return
}

func normalizeAngle(a float64) float64 {
	a = math.Mod(a, 360.0)
	if a < 0 {
		a += 360.0
	}
	return a
}

func normalizeAngleSigned(a float64) float64 {
	a = math.Mod(a+180.0, 360.0)
	if a < 0 {
		a += 360.0
	}
	return a - 180.0
}

// 辅助：把值限制在 [-1,1]，避免 asin 因数值误差崩溃
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// CesiumMathToRadians 将角度转换为弧度（与 Cesium.Math.toRadians 等价）
func CesiumMathToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180.0
}

func toRadians(deg float64) float64 {
	return deg * math.Pi / 180.0
}

// 经纬度 → ECEF
func CartesianFromDegrees(lon, lat, height float64) [3]float64 {
	lonRad := toRadians(lon)
	latRad := toRadians(lat)

	N := a / math.Sqrt(1-e2*math.Sin(latRad)*math.Sin(latRad))
	x := (N + height) * math.Cos(latRad) * math.Cos(lonRad)
	y := (N + height) * math.Cos(latRad) * math.Sin(lonRad)
	z := (N*(1-e2) + height) * math.Sin(latRad)

	return [3]float64{x, y, z}
}

// Convert lon/lat/height to ECEF
func lonLatHeightToECEF(lon, lat, h float64) (x, y, z float64) {
	lonRad := lon * math.Pi / 180.0
	latRad := lat * math.Pi / 180.0

	sinLat := math.Sin(latRad)
	cosLat := math.Cos(latRad)
	sinLon := math.Sin(lonRad)
	cosLon := math.Cos(lonRad)

	N := a / math.Sqrt(1.0-e2*sinLat*sinLat)

	x = (N + h) * cosLat * cosLon
	y = (N + h) * cosLat * sinLon
	z = (N*(1-e2) + h) * sinLat

	return
}

// Normalize quaternion
func (q Quaternion) Normalize() Quaternion {
	n := math.Sqrt(q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W)
	if n == 0 {
		return Quaternion{0, 0, 0, 1}
	}
	return Quaternion{q.X / n, q.Y / n, q.Z / n, q.W / n}
}

// Quaternion → rotation matrix (3x3)
func quatToMatrix3(q Quaternion) [9]float64 {
	q = q.Normalize()

	x, y, z, w := q.X, q.Y, q.Z, q.W

	xx := x * x
	yy := y * y
	zz := z * z
	xy := x * y
	xz := x * z
	yz := y * z
	wx := w * x
	wy := w * y
	wz := w * z

	return [9]float64{
		1 - 2*(yy+zz), 2 * (xy - wz), 2 * (xz + wy),
		2 * (xy + wz), 1 - 2*(xx+zz), 2 * (yz - wx),
		2 * (xz - wy), 2 * (yz + wx), 1 - 2*(xx+yy),
	}
}

// Build ENU frame at position
func enuFrame(lon, lat float64) [9]float64 {
	lonRad := lon * math.Pi / 180.0
	latRad := lat * math.Pi / 180.0

	sinLat := math.Sin(latRad)
	cosLat := math.Cos(latRad)
	sinLon := math.Sin(lonRad)
	cosLon := math.Cos(lonRad)

	// East
	ex := -sinLon
	ey := cosLon
	ez := 0.0

	// North
	nx := -sinLat * cosLon
	ny := -sinLat * sinLon
	nz := cosLat

	// Up
	ux := cosLat * cosLon
	uy := cosLat * sinLon
	uz := sinLat

	// column-major 3x3
	return [9]float64{
		ex, nx, ux,
		ey, ny, uy,
		ez, nz, uz,
	}
}

// Multiply 3x3 matrices (column-major)
func mul3x3(a, b [9]float64) [9]float64 {
	var r [9]float64
	for col := 0; col < 3; col++ {
		for row := 0; row < 3; row++ {
			r[col*3+row] =
				a[0*3+row]*b[col*3+0] +
					a[1*3+row]*b[col*3+1] +
					a[2*3+row]*b[col*3+2]
		}
	}
	return r
}

// Final function
func TransformFromTRS(
	longitude, latitude, altitude float64,
	q Quaternion,
	scaleX, scaleY, scaleZ float64,
) [16]float64 {

	// 1. Position in ECEF
	tx, ty, tz := lonLatHeightToECEF(longitude, latitude, altitude)

	// 2. ENU frame
	enu := enuFrame(longitude, latitude)

	// 3. Quaternion rotation
	rot := quatToMatrix3(q)

	// 4. Combine: ENU * rotation
	finalRot := mul3x3(enu, rot)

	// 5. Apply scale
	finalRot[0] *= scaleX
	finalRot[1] *= scaleX
	finalRot[2] *= scaleX

	finalRot[3] *= scaleY
	finalRot[4] *= scaleY
	finalRot[5] *= scaleY

	finalRot[6] *= scaleZ
	finalRot[7] *= scaleZ
	finalRot[8] *= scaleZ

	// 6. Build Matrix4 (column-major)
	return [16]float64{
		finalRot[0], finalRot[1], finalRot[2], 0,
		finalRot[3], finalRot[4], finalRot[5], 0,
		finalRot[6], finalRot[7], finalRot[8], 0,
		tx, ty, tz, 1,
	}
}

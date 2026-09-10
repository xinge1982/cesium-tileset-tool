package cesium

import (
	"math"
)

// 定义常量
const (
	wgs84SemiMajorAxis = 6378137.0
	wgs84Flattening    = 1 / 298.257223563
	wgs84Eccentricity2 = 2*wgs84Flattening - wgs84Flattening*wgs84Flattening
)

// 定义类型
type Cartesian3 struct {
	X, Y, Z float64
}

type Matrix3 [3][3]float64
type Transform [16]float32

// 弧度和角度转换
func degreesToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

// 经纬度转笛卡尔坐标(球心坐标)
func FromDegrees(longitude, latitude, height float64) Cartesian3 {
	latRad := degreesToRadians(latitude)
	lonRad := degreesToRadians(longitude)

	N := wgs84SemiMajorAxis / math.Sqrt(1-wgs84Eccentricity2*math.Sin(latRad)*math.Sin(latRad))
	cosLat := math.Cos(latRad)
	sinLat := math.Sin(latRad)
	cosLon := math.Cos(lonRad)
	sinLon := math.Sin(lonRad)

	return Cartesian3{
		X: (N + height) * cosLat * cosLon,
		Y: (N + height) * cosLat * sinLon,
		Z: (N*(1-wgs84Eccentricity2) + height) * sinLat,
	}
}

// 计算East-North-Up (ENU)变换矩阵，输入经纬度，转出原始的旋转矩阵
func enuMatrix(longitude, latitude float64) Matrix3 {
	latRad := degreesToRadians(latitude)
	lonRad := degreesToRadians(longitude)

	sinLat := math.Sin(latRad)
	cosLat := math.Cos(latRad)
	sinLon := math.Sin(lonRad)
	cosLon := math.Cos(lonRad)

	return Matrix3{
		{-sinLon, cosLon, 0},
		{-sinLat * cosLon, -sinLat * sinLon, cosLat},
		{cosLat * cosLon, cosLat * sinLon, sinLat},
	}
}

// 计算绕Z轴的旋转矩阵
func rotationMatrixZ(theta float64) Matrix3 {
	cosTheta := math.Cos(theta)
	sinTheta := math.Sin(theta)

	return Matrix3{
		{cosTheta, -sinTheta, 0},
		{sinTheta, cosTheta, 0},
		{0, 0, 1},
	}
}

// 矩阵乘法
func matrixMultiply(a, b Matrix3) Matrix3 {
	var result Matrix3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			result[i][j] = 0
			for k := 0; k < 3; k++ {
				result[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return result
}

// 生成变换矩阵
func GenerateTransformMatrix(longitude, latitude, height, headingDegrees float64) [16]float64 {
	origin := FromDegrees(longitude, latitude, height)
	enuMat := enuMatrix(longitude, latitude)
	// 将heading角度转换为弧度
	heading := degreesToRadians(headingDegrees)

	if headingDegrees != 0 {
		// 计算绕Z轴的旋转矩阵
		Rz := rotationMatrixZ(heading)
		// 计算新的旋转矩阵
		enuMat = matrixMultiply(Rz, enuMat)
	}

	var nf [16]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			nf[4*i+j] = enuMat[i][j]
		}
	}
	nf[15] = 1
	nf[14] = origin.Z
	nf[13] = origin.Y
	nf[12] = origin.X
	return nf
}

// 生成变换矩阵
func GenerateTransformMatrixUP(longitude, latitude, height, headingDegrees float64) [16]float64 {
	origin := FromDegrees(longitude, latitude, height)
	enuMat := enuMatrix(longitude, latitude)
	// 将heading角度转换为弧度
	heading := degreesToRadians(headingDegrees)

	if headingDegrees != 0 {
		// 计算绕Z轴的旋转矩阵
		Rz := rotationMatrixZ(heading)
		// 计算新的旋转矩阵
		enuMat = matrixMultiply(Rz, enuMat)
	}

	var nf [16]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			nf[4*i+j] = enuMat[i][j]
		}
	}
	nf[15] = 1
	nf[14] = origin.Z
	nf[13] = origin.Y
	nf[12] = origin.X
	return nf
}

// 生成带缩放的变换矩阵 (支持X/Y/Z独立缩放)
func GenerateTransformMatrixUPScale(longitude, latitude, height, headingDegrees float64, scaleX, scaleY, scaleZ float64) [16]float64 {
	origin := FromDegrees(longitude, latitude, height)
	enuMat := enuMatrix(longitude, latitude)
	heading := degreesToRadians(headingDegrees)

	// heading旋转
	if headingDegrees != 0 {
		Rz := rotationMatrixZ(heading)
		enuMat = matrixMultiply(Rz, enuMat)
	}

	// 缩放矩阵
	scaleMat := Matrix3{
		{scaleX, 0, 0},
		{0, scaleY, 0},
		{0, 0, scaleZ},
	}

	// ✅ 修正顺序：先缩放，再旋转（在局部坐标系中缩放）
	enuMat = matrixMultiply(scaleMat, enuMat)

	// 输出4x4矩阵
	var nf [16]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			nf[4*i+j] = enuMat[i][j]
		}
	}
	nf[12] = origin.X
	nf[13] = origin.Y
	nf[14] = origin.Z
	nf[15] = 1

	return nf
}

func GenerateTransformMatrixHPRScale(
	longitude, latitude, height float64,
	headingDegrees, pitchDegrees, rollDegrees float64,
	scaleX, scaleY, scaleZ float64,
) [16]float64 {
	origin := FromDegrees(longitude, latitude, height)
	enuMat := enuMatrix(longitude, latitude)

	heading := degreesToRadians(headingDegrees)
	pitch := degreesToRadians(pitchDegrees)
	roll := degreesToRadians(rollDegrees)

	rotMat := identityMatrix3()

	// Cesium convention
	if headingDegrees != 0 {
		rotMat = matrixMultiply(rotationMatrixZ(heading), rotMat)
	}
	if pitchDegrees != 0 {
		rotMat = matrixMultiply(rotationMatrixX(pitch), rotMat)
	}
	if rollDegrees != 0 {
		rotMat = matrixMultiply(rotationMatrixY(roll), rotMat)
	}

	enuMat = matrixMultiply(rotMat, enuMat)

	scaleMat := Matrix3{
		{scaleX, 0, 0},
		{0, scaleY, 0},
		{0, 0, scaleZ},
	}

	enuMat = matrixMultiply(scaleMat, enuMat)

	var nf [16]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			nf[4*i+j] = enuMat[i][j]
		}
	}
	nf[12] = origin.X
	nf[13] = origin.Y
	nf[14] = origin.Z
	nf[15] = 1

	return nf
}

func identityMatrix3() Matrix3 {
	return Matrix3{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
}

func rotationMatrixX(theta float64) Matrix3 {
	c := math.Cos(theta)
	s := math.Sin(theta)
	return Matrix3{
		{1, 0, 0},
		{0, c, -s},
		{0, s, c},
	}
}

func rotationMatrixY(theta float64) Matrix3 {
	c := math.Cos(theta)
	s := math.Sin(theta)
	return Matrix3{
		{c, 0, s},
		{0, 1, 0},
		{-s, 0, c},
	}
}

package transform

import (
	"math"
)

// 定义常量
const (
	wgs84SemiMajorAxis = 6378137.0
	wgs84Flattening    = 1 / 298.257223563
	wgs84Eccentricity2 = 2*wgs84Flattening - wgs84Flattening*wgs84Flattening
)

// Cartesian3 定义类型
type Cartesian3 struct {
	X, Y, Z float64
}

type Matrix3 [3][3]float64
type Transform [16]float32

// 弧度和角度转换
//func degreesToRadians(degrees float64) float64 {
//	return degrees * math.Pi / 180
//}

// FromDegrees 经纬度转笛卡尔坐标(球心坐标)
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

// GenerateTransformMatrix 生成变换矩阵
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

// GenerateTransformMatrixUP 生成变换矩阵
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

package transform

import "math"

// degreesToRadians 将角度转换为弧度
func degreesToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180.0
}

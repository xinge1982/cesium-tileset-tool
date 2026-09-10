package transform

import (
	"fmt"
	"testing"
)

//lon := 120.677197187618
//lat := 31.2096525066435
//height := 20.7890000000066
//heading := 357.246 // 正北为0，顺时针为正角度

func TestGenerateTransformMatrixUP(t *testing.T) {
	lon := 119.0085956472552
	lat := 31.642187395419047
	height := 10.0
	heading := 0.0 // 正北为0，顺时针为正角度
	transform := GenerateTransformMatrix(lon, lat, height, heading)
	for i := 0; i < 4; i++ {
		fmt.Printf("%.6f,%.6f,%.6f,%.6f,",
			transform[i], transform[i+4], transform[i+8], transform[i+12])
	}

}
func TestGenerateTransformMatrix(t *testing.T) {
	/*

		lonDeg := 121.48810934293468
			latDeg := 31.05105738623917
			hMeter := 10.0
	*/

	lon := 121.48810934293468
	lat := 31.05105738623917
	height := 10.0
	heading := 0.0 // 正北为0，顺时针为正角度
	transform := GenerateTransformMatrix(lon, lat, height, heading)
	for i := 0; i < 16; i++ {
		fmt.Printf("%.6f,",
			transform[i])
	}

}

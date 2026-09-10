package cesium

import (
	"fmt"
	"math"
	"testing"

	"github.com/magiconair/properties/assert"
)

func Test_computeTransformMatrix(t *testing.T) {
	// 示例：经纬度、高程、方向角（单位：度）
	lon := 120.677197187618
	lat := 31.2096525066435
	height := 20.7890000000066
	heading := 357.246 // 正北为0，顺时针为正角度

	transform := computeTransformMatrix(lon, lat, height, heading)

	fmt.Println("Transform matrix:")
	for i := 0; i < 4; i++ {
		fmt.Printf("[%.6f %.6f %.6f %.6f]\n",
			transform[i], transform[i+4], transform[i+8], transform[i+12])
	}
}

func Test_hprTest(t *testing.T) {
	// Example: heading, pitch, roll in degrees
	headingDegrees := 247.9617702115 // Z-axis
	pitchDegrees := -31.0            // X-axis
	rollDegrees := 0.0               // Y-axis
	var scaleX float64 = 1.0
	var scaleY float64 = 1.0
	var scaleZ float64 = 1.0

	// Generate quaternion from HPR angles
	quaternion := FromHpr(headingDegrees, float64(pitchDegrees), float64(rollDegrees))
	fmt.Printf("Quaternion: {0.0,0.0,0.0,%v,%v,%v,%v,%f,%f,%f}\n", quaternion.X, quaternion.Y, quaternion.Z, -quaternion.W, scaleX, scaleY, scaleZ)

	heading, pitch, roll := ToHpr(quaternion)
	assert.Equal(t, FloatEqual(headingDegrees, heading), true)
	assert.Equal(t, FloatEqual(pitchDegrees, pitch), true)
	assert.Equal(t, FloatEqual(rollDegrees, roll), true)

	q := FromHpr(247.9617702115, -31.0, 0)
	h, p, r := ToHpr(q)
	fmt.Printf("H=%.10f P=%.10f R=%.10f\n", h, p, r)

	//0.00000000000000000,0.00000000000000000,0.86902067037026360,-0.49477578201566980
	//0.00000000000000000,0.00000000000000000,0.51214903593063350,0.85889661312103270,

	q = Quaternion{
		X: 0,
		Y: 0,
		Z: 0.86902067037026360,
		W: 0.49477578201566980,
	}
	h, p, r = ToHpr(q)
	fmt.Printf("H=%.10f P=%.10f R=%.10f\n", h, p, r)

	q = Quaternion{
		X: 0,
		Y: 0,
		Z: 0.38461306691169740,
		W: 0.92307788133621220,
	}
	h, p, r = ToHpr(q)
	fmt.Printf("H=%.10f P=%.10f R=%.10f\n", h, p, r)

	/*
		117.2658820283,10763.png,"","{0.00000000000000000,0.00000000000000000,0.00000000000000000,0.00000000000000000,0.00000000000000000,0.8690206703702636,-0.4947757820156698,1.00000000000000000,1.00000000000000000,1.00000000000000000}"
	*/
}

func Test_hprTest3(t *testing.T) {
	// Example: heading, pitch, roll in degrees
	headingDegrees := 247.9617702115 // Z-axis
	pitchDegrees := 0.0              // X-axis
	rollDegrees := 0.0               // Y-axis
	q := FromHpr(headingDegrees, pitchDegrees, rollDegrees)
	h, p, r := ToHpr(q)
	fmt.Printf("H=%.10f P=%.10f R=%.10f\n", h, p, r)

}

func FloatEqual(a, b float64) bool {
	const eps = 1e-9
	return math.Abs(a-b) < eps
}

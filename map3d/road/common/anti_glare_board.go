package common

import (
	"fmt"
	"math"
)

const defaultAntiGlareBoardSpacing = float32(1.0)

// buildAntiGlareBoardTransforms 按线型、间距和 startoffset 生成防眩板实例变换。
//
// 说明：
// 1. 几何点已经是本地米制坐标，直接按米采样；
// 2. startoffset 与虚线规则一致，按 spacing 取模后决定当前线的首个摆放位置；
// 3. 朝向取插入点所在小线段的方向；
// 4. 防眩板模板模型沿线方向按本地 +Z 贴合线段方向。
func buildAntiGlareBoardTransforms(line LocalLine, spacing, startOffset float32) ([]instancedTransform, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, fmt.Errorf("anti glare board line requires at least 2 distinct points")
	}
	if spacing <= 0 {
		spacing = defaultAntiGlareBoardSpacing
	}

	cum := polylineCumLengthsF32(points)
	total := cum[len(cum)-1]
	if total <= 0 {
		return nil, nil
	}

	first := antiGlareBoardStartDistance(spacing, startOffset)
	if first > total+0.0001 {
		return nil, nil
	}

	count := int(math.Floor(float64((total-first)/spacing))) + 1
	transforms := make([]instancedTransform, 0, count)
	for dist := first; dist <= total+0.0001; dist += spacing {
		sample := samplePolylineAt(points, cum, dist)
		transforms = append(transforms, instancedTransform{
			Translation: sample.Point,
			Rotation:    quaternionFromForward(sample.Forward),
			Scale:       [3]float32{1, 1, 1},
		})
	}
	return transforms, nil
}

func antiGlareBoardStartDistance(spacing, startOffset float32) float32 {
	if spacing <= 0 {
		return 0
	}
	offset := float64(startOffset)
	step := float64(spacing)
	rem := math.Mod(offset, step)
	if rem < 0 {
		rem += step
	}
	if rem < 1e-6 || step-rem < 1e-6 {
		return 0
	}
	return float32(step - rem)
}

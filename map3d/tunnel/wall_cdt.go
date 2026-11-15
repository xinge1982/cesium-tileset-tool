package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"fmt"
	"math"
)

// 将上下边界配对的竖向长墙拆成局部片段，用 CDT 剖分并保留原始绕序。
// 索引始终引用原始三维坐标，避免弯曲长墙在单一平面投影后产生交叉或跨弯连接。
func triangulateTunnelWallCDT(pos [][3]float32) ([]uint32, error) {
	half := pairedTunnelWallHalf(pos)
	if half == 0 {
		return mgltf.CDTRingKeepWinding(pos)
	}
	n := half * 2
	indices := make([]uint32, 0, (half-1)*6)
	for i := 0; i+1 < half; i++ {
		source := [4]uint32{uint32(i), uint32(i + 1), uint32(n - 2 - i), uint32(n - 1 - i)}
		quad := [][3]float32{pos[source[0]], pos[source[1]], pos[source[2]], pos[source[3]]}
		if quad[0] == quad[1] && quad[2] == quad[3] {
			continue
		}
		local, err := mgltf.CDTRingKeepWinding(quad)
		if err != nil {
			return nil, fmt.Errorf("wall segment %d: %w", i, err)
		}
		for _, index := range local {
			indices = append(indices, source[index])
		}
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("CDT: wall has no nondegenerate segments")
	}
	return indices, nil
}

// 判断墙面是否由反向排列的上下边界构成，返回每条边界的点数。
func pairedTunnelWallHalf(pos [][3]float32) int {
	n := len(pos)
	if n > 1 && pos[0] == pos[n-1] {
		n--
	}
	if n < 4 || n%2 != 0 {
		return 0
	}
	half := n / 2
	// ENU 坐标中，同经纬度不同高程的点会有毫米级水平偏移。
	const horizontalTolerance = 0.02
	sign := float64(pos[n-1][1]) - float64(pos[0][1])
	for i := 0; i < half; i++ {
		a, b := pos[i], pos[n-1-i]
		dx, dz := float64(a[0])-float64(b[0]), float64(a[2])-float64(b[2])
		dy := float64(b[1]) - float64(a[1])
		if math.Hypot(dx, dz) > horizontalTolerance || dy*sign <= 0 {
			return 0
		}
	}
	return half
}

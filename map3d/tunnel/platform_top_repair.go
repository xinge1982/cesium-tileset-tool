package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"fmt"
	"math"
)

// 将水平投影自交的四边形按奇偶填充规则拆为两个简单面。
// 仅允许交叉边的三维间距不超过 5 毫米，交点取两条边插值点的中点。
// 原始顶点保持不动；其余自交情况仍交由 CDT 明确报错。
func repairPlatformTopCrossing(pos [][3]float32) ([][3]float32, []uint32, bool, error) {
	return repairPlatformTopCrossingWithTolerance(pos, 0.005)
}

// 使用调用者明确指定的米制容差修复交叉，保留默认 5 毫米的入口行为。
func repairPlatformTopCrossingWithTolerance(pos [][3]float32, tolerance float64) ([][3]float32, []uint32, bool, error) {
	if !isFinite(tolerance) || tolerance <= 0 {
		return nil, nil, false, fmt.Errorf("platform repair tolerance must be positive meters")
	}
	if len(pos) != 4 {
		return pos, nil, false, nil
	}
	for _, pair := range [][2]int{{0, 2}, {1, 3}} {
		i, j := pair[0], pair[1]
		a, b, c, d := pos[i], pos[(i+1)%4], pos[j], pos[(j+1)%4]
		vx, vz := float64(b[0])-float64(a[0]), float64(b[2])-float64(a[2])
		wx, wz := float64(d[0])-float64(c[0]), float64(d[2])-float64(c[2])
		dx, dz := float64(c[0])-float64(a[0]), float64(c[2])-float64(a[2])
		determinant := vx*wz - vz*wx
		if math.Abs(determinant) < 1e-12 {
			continue
		}
		t, u := (dx*wz-dz*wx)/determinant, (dx*vz-dz*vx)/determinant
		if t <= 1e-9 || t >= 1-1e-9 || u <= 1e-9 || u >= 1-1e-9 {
			continue
		}
		var intersection [3]float32
		var separation float64
		for axis := 0; axis < 3; axis++ {
			p := float64(a[axis]) + t*(float64(b[axis])-float64(a[axis]))
			q := float64(c[axis]) + u*(float64(d[axis])-float64(c[axis]))
			separation += (p - q) * (p - q)
			intersection[axis] = float32((p + q) / 2)
		}
		if math.Sqrt(separation) > tolerance {
			return nil, nil, false, fmt.Errorf("self-crossing platform has %.6f m edge separation at crossing, exceeding %.6f m repair tolerance (MAP3D_TUNNEL_PLATFORM_REPAIR_TOLERANCE)", math.Sqrt(separation), tolerance)
		}
		output := append(append([][3]float32(nil), pos...), intersection)
		// 输入顶面的净有向面积决定法线方向；两片都保持该方向。
		area := 0.0
		for k := 0; k < 4; k++ {
			p, q := pos[k], pos[(k+1)%4]
			area += (float64(p[0])-float64(pos[0][0]))*(float64(q[2])-float64(pos[0][2])) - (float64(q[0])-float64(pos[0][0]))*(float64(p[2])-float64(pos[0][2]))
		}
		if math.Abs(area) < 1e-10 {
			return nil, nil, false, fmt.Errorf("self-crossing platform has ambiguous zero net winding")
		}
		var indices []uint32
		for _, ends := range [][2]int{{(i + 1) % 4, j}, {(j + 1) % 4, i}} {
			source := []uint32{4, uint32(ends[0]), uint32(ends[1])}
			triangle := [][3]float32{output[source[0]], output[source[1]], output[source[2]]}
			local, err := mgltf.CDTRingKeepWinding(triangle)
			if err != nil {
				return nil, nil, false, fmt.Errorf("repair crossing triangle: %w", err)
			}
			p, q, r := triangle[0], triangle[1], triangle[2]
			signed := (float64(q[0])-float64(p[0]))*(float64(r[2])-float64(p[2])) - (float64(q[2])-float64(p[2]))*(float64(r[0])-float64(p[0]))
			for k := 0; k < len(local); k += 3 {
				a, b, c := source[local[k]], source[local[k+1]], source[local[k+2]]
				if signed*area < 0 {
					b, c = c, b
				}
				indices = append(indices, a, b, c)
			}
		}
		return output, indices, true, nil
	}
	return pos, nil, false, nil
}

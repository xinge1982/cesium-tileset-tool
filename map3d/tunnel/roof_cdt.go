package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"math"
)

// 对具备前后断面配对语义的拱顶四边形展开后执行 CDT，索引仍指向原始顶点。
func triangulateRoofSectionQuad(pos [][3]float32) ([]uint32, error) {
	if len(pos) != 4 {
		return mgltf.CDTRingKeepWinding(pos)
	}
	distance := func(a, b [3]float32) float64 {
		var d float64
		for k := 0; k < 3; k++ {
			v := float64(a[k]) - float64(b[k])
			d += v * v
		}
		return math.Sqrt(d)
	}
	length := math.Max(distance(pos[0], pos[1]), distance(pos[3], pos[2]))
	a, b := distance(pos[0], pos[3]), distance(pos[1], pos[2])
	projected := [][3]float32{{0, 0, 0}, {float32(length), 0, 0}, {float32(length), float32(b), 0}, {0, float32(a), 0}}
	indices, err := mgltf.CDTRingKeepWinding(projected)
	if err != nil {
		return nil, err
	}
	return nondegenerateSpatialTriangles(pos, indices), nil
}

// 只剔除原始三维坐标中确实为零面积的三角形，不改变其余面的顶点或绕序。
func nondegenerateSpatialTriangles(pos [][3]float32, indices []uint32) []uint32 {
	kept := indices[:0]
	for i := 0; i < len(indices); i += 3 {
		a, b, c := pos[indices[i]], pos[indices[i+1]], pos[indices[i+2]]
		var normal [3]float64
		for axis := 0; axis < 3; axis++ {
			j, k := (axis+1)%3, (axis+2)%3
			normal[axis] = (float64(b[j])-float64(a[j]))*(float64(c[k])-float64(a[k])) - (float64(b[k])-float64(a[k]))*(float64(c[j])-float64(a[j]))
		}
		if normal[0] != 0 || normal[1] != 0 || normal[2] != 0 {
			kept = append(kept, indices[i:i+3]...)
		}
	}
	return kept
}

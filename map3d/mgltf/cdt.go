package mgltf

import (
	"cesium-tileset-tool/map3d/road/cdt-go/cdt"
	"fmt"
	"math"
)

// Project a spatial ring onto its dominant plane, preserving original vertices and winding.
// The returned indices address the supplied positions, including when the ring is closed.
// Nonplanar rings retain their original elevations; no height reconstruction is performed.
func CDTRingKeepWinding(pos [][3]float32) ([]uint32, error) {
	n := len(pos)
	if n > 1 && pos[0] == pos[n-1] {
		n--
	}
	if n < 3 {
		return nil, fmt.Errorf("CDT: need at least three vertices")
	}
	var normal [3]float64
	for i := 0; i < n; i++ {
		a, b := pos[i], pos[(i+1)%n]
		for _, v := range a {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nil, fmt.Errorf("CDT: nonfinite vertex %d", i)
			}
		}
		for k := 0; k < 3; k++ {
			j, l := (k+1)%3, (k+2)%3
			normal[k] += (float64(a[j]) - float64(b[j])) * (float64(a[l]) + float64(b[l]))
		}
	}
	axis := 0
	for k := 1; k < 3; k++ {
		if math.Abs(normal[k]) > math.Abs(normal[axis]) {
			axis = k
		}
	}
	if math.Abs(normal[axis]) < 1e-12 {
		return nil, fmt.Errorf("CDT: degenerate ring")
	}
	u, v := (axis+1)%3, (axis+2)%3
	points := make([]cdt.Point2, n)
	edges := make([]cdt.Edge, n)
	lookup := make(map[cdt.Point2]uint32, n)
	for i := 0; i < n; i++ {
		p := cdt.Point2{X: float64(pos[i][u]) - float64(pos[0][u]), Y: float64(pos[i][v]) - float64(pos[0][v])}
		if old, ok := lookup[p]; ok {
			return nil, fmt.Errorf("CDT: vertices %d and %d overlap in projection", old, i)
		}
		points[i] = p
		lookup[p] = uint32(i)
		edges[i] = cdt.Edge{A: int32(i), B: int32((i + 1) % n)}
	}
	out, triangles, err := cdt.TriangulateInside(points, edges)
	if err != nil {
		return nil, fmt.Errorf("CDT: %w", err)
	}
	remap := make([]uint32, len(out))
	for i, p := range out {
		index, ok := lookup[p]
		if !ok {
			return nil, fmt.Errorf("CDT: unexpected generated vertex; cannot preserve original 3D position")
		}
		remap[i] = index
	}
	indices := make([]uint32, 0, len(triangles)*3)
	for _, tri := range triangles {
		if tri.A < 0 || tri.B < 0 || tri.C < 0 || int(tri.A) >= len(out) || int(tri.B) >= len(out) || int(tri.C) >= len(out) {
			return nil, fmt.Errorf("CDT: invalid triangle index")
		}
		a, b, c := remap[tri.A], remap[tri.B], remap[tri.C]
		p, q, r := points[a], points[b], points[c]
		area := (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X)
		if area == 0 {
			return nil, fmt.Errorf("CDT: degenerate triangle")
		}
		if area*normal[axis] < 0 {
			b, c = c, b
		}
		indices = append(indices, a, b, c)
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("CDT: empty triangulation")
	}
	return indices, nil
}

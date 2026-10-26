package common

import (
	"fmt"
	"math"
)

// Sweep a cosine approximation of the corrugated sheet along the ground line.
// This is a visual approximation, not a manufacturing profile: no thickness,
// rolled edges, bolt holes or overlap. Sixteen segments per wave include extrema.
func buildGuardrailWaveSweep(line LocalLine, direction, waves int, base, height float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 || waves < 1 || height <= 0 || !chainFinite(float64(base)) || !chainFinite(float64(height)) {
		return nil, nil, nil, nil, fmt.Errorf("invalid wave sweep line or dimensions")
	}
	for _, p := range points {
		for _, v := range p {
			if !chainFinite(float64(v)) {
				return nil, nil, nil, nil, fmt.Errorf("nonfinite wave sweep point")
			}
		}
	}
	segments := waves * 16
	stride := segments + 1
	positions := make([][3]float32, 0, len(points)*stride)
	uv := make([][2]float32, 0, cap(positions))
	cumulative := polylineCumLengthsF32(points)
	normals2D := buildSegmentNormals(points)
	closed := len(points) > 3 && points[0] == points[len(points)-1]
	sign := float32(-1)
	if direction == 1 {
		sign = 1
	}
	for i, p := range points {
		previous, next := i-1, i
		if i == 0 {
			previous = 0
		}
		if next >= len(normals2D) {
			next = len(normals2D) - 1
		}
		if closed && (i == 0 || i == len(points)-1) {
			previous = len(normals2D) - 1
			next = 0
		}
		a, b := normals2D[previous], normals2D[next]
		if a[0]*b[0]+a[1]*b[1] < -.95 {
			return nil, nil, nil, nil, fmt.Errorf("wave sweep turns back at vertex %d; split the line", i)
		}
		nx, nz := normalize2(a[0]+b[0], a[1]+b[1])
		// A capped miter keeps consecutive sections joined without huge corner spikes.
		divisor := nx*b[0] + nz*b[1]
		miter := 1.0
		if divisor > 1e-6 {
			miter = math.Min(2.5, 1/divisor)
		}
		for j := 0; j <= segments; j++ {
			t := float64(j) / float64(segments)
			depth := float32(float64(defaultGuardrailWaveDepth) * .5 * (1 - math.Cos(2*math.Pi*float64(waves)*t)))
			positions = append(positions, [3]float32{p[0] + sign*float32(nx*miter)*depth, p[1] + base + height*float32(t), p[2] + sign*float32(nz*miter)*depth})
			uv = append(uv, [2]float32{cumulative[i] / defaultGuardrailRailLengthUV, 1 - float32(t)})
		}
	}
	indices := make([]uint32, 0, (len(points)-1)*segments*6)
	for i := 0; i < len(points)-1; i++ {
		for j := 0; j < segments; j++ {
			a := uint32(i*stride + j)
			b := a + uint32(stride)
			c := b + 1
			d := a + 1
			if direction == 1 {
				indices = append(indices, a, b, c, a, c, d)
			} else {
				indices = append(indices, a, c, b, a, d, c)
			}
		}
	}
	normals := computeVertexNormals(positions, indices)
	if closed {
		for j := 0; j < stride; j++ {
			k := (len(points)-1)*stride + j
			n := normalize3(add3(normals[j], normals[k]))
			normals[j] = n
			normals[k] = n
		}
	}
	return positions, normals, uv, indices, nil
}

package common

import "fmt"

// Configure circular posts in metres; zero fields use the defaults below.
// Height overrides the automatic board-center-plus-clearance height when positive.
type GuardrailPostOptions struct {
	Diameter            float32
	TopAboveBoardCenter float32
	Height              float32
	Spacing             float32
	LateralOffset       float32
	RadialSegments      int
}

// Return the visible post defaults; only the height depends on board type.
func DefaultGuardrailPostOptions() GuardrailPostOptions {
	return GuardrailPostOptions{Diameter: .114, TopAboveBoardCenter: .150, Spacing: 4, LateralOffset: .230, RadialSegments: 12}
}

// Resolve and validate the visible post dimensions without altering the input.
func resolveGuardrailPosts(input *GuardrailPostOptions, base, panel float32) (GuardrailPostOptions, error) {
	p := DefaultGuardrailPostOptions()
	if input != nil {
		for _, v := range []float32{input.Diameter, input.TopAboveBoardCenter, input.Height, input.Spacing, input.LateralOffset} {
			if !chainFinite(float64(v)) || v < 0 {
				return p, fmt.Errorf("guardrail post parameters must be finite and nonnegative")
			}
		}
		if input.Diameter != 0 {
			p.Diameter = input.Diameter
		}
		if input.TopAboveBoardCenter != 0 {
			p.TopAboveBoardCenter = input.TopAboveBoardCenter
		}
		if input.Height != 0 {
			p.Height = input.Height
		}
		if input.Spacing != 0 {
			p.Spacing = input.Spacing
		}
		if input.LateralOffset != 0 {
			p.LateralOffset = input.LateralOffset
		}
		if input.RadialSegments != 0 {
			p.RadialSegments = input.RadialSegments
		}
	}
	if p.Diameter < .114 {
		return p, fmt.Errorf("guardrail post diameter must be at least 0.114 m")
	}
	if p.RadialSegments < 3 || p.RadialSegments > 256 {
		return p, fmt.Errorf("guardrail post radial segments must be 3..256")
	}
	if p.Height == 0 {
		p.Height = base + panel/2 + p.TopAboveBoardCenter
	}
	if !chainFinite(float64(p.Height)) || p.Height <= 0 {
		return p, fmt.Errorf("invalid guardrail post height")
	}
	return p, nil
}

// Build only the circular post, retaining the established local-side convention.
func buildGuardrailPostMesh(direction int, p GuardrailPostOptions) ([][3]float32, [][3]float32, [][2]float32, []uint32) {
	sign := float32(1)
	if direction == 1 {
		sign = -1
	}
	var pos [][3]float32
	var normals [][3]float32
	var uv [][2]float32
	var indices []uint32
	appendCylinderGeometry(&pos, &normals, &uv, &indices, [3]float32{sign * p.LateralOffset, p.Height / 2, 0}, p.Diameter, p.Height, p.Diameter, p.RadialSegments)
	return pos, normals, uv, indices
}

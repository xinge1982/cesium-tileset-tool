package mapmodel

import (
	"errors"
	"fmt"
	"math"
)

const eps = 1e-12

// Mat4 uses column-major layout, same as glTF/OpenGL.
// Element access: m[col*4 + row]
type Mat4 [16]float64

type Vec3 struct {
	X, Y, Z float64
}

type Quat struct {
	X, Y, Z, W float64
}

// -----------------------------
// Basic matrix helpers
// -----------------------------

func Identity4() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

func TranslationMatrix(t Vec3) Mat4 {
	m := Identity4()
	m[12] = t.X
	m[13] = t.Y
	m[14] = t.Z
	return m
}

func Mul4(a, b Mat4) Mat4 {
	var out Mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			out[col*4+row] =
				a[0*4+row]*b[col*4+0] +
					a[1*4+row]*b[col*4+1] +
					a[2*4+row]*b[col*4+2] +
					a[3*4+row]*b[col*4+3]
		}
	}
	return out
}

// InverseAffine inverts a 4x4 affine matrix:
// [ A t ]
// [ 0 1 ]
// where A is any invertible 3x3 (can include rotation+scale).
func InverseAffine(m Mat4) (Mat4, error) {
	// Top-left 3x3
	a00, a01, a02 := m[0], m[4], m[8]
	a10, a11, a12 := m[1], m[5], m[9]
	a20, a21, a22 := m[2], m[6], m[10]

	det :=
		a00*(a11*a22-a12*a21) -
			a01*(a10*a22-a12*a20) +
			a02*(a10*a21-a11*a20)

	if math.Abs(det) < eps {
		return Mat4{}, errors.New("affine matrix is not invertible")
	}
	invDet := 1.0 / det

	// Inverse of 3x3
	i00 := (a11*a22 - a12*a21) * invDet
	i01 := (a02*a21 - a01*a22) * invDet
	i02 := (a01*a12 - a02*a11) * invDet

	i10 := (a12*a20 - a10*a22) * invDet
	i11 := (a00*a22 - a02*a20) * invDet
	i12 := (a02*a10 - a00*a12) * invDet

	i20 := (a10*a21 - a11*a20) * invDet
	i21 := (a01*a20 - a00*a21) * invDet
	i22 := (a00*a11 - a01*a10) * invDet

	tx, ty, tz := m[12], m[13], m[14]

	// t' = -A^-1 * t
	itx := -(i00*tx + i01*ty + i02*tz)
	ity := -(i10*tx + i11*ty + i12*tz)
	itz := -(i20*tx + i21*ty + i22*tz)

	return Mat4{
		i00, i10, i20, 0,
		i01, i11, i21, 0,
		i02, i12, i22, 0,
		itx, ity, itz, 1,
	}, nil
}

// -----------------------------
// Vector helpers
// -----------------------------

func vecLen(x, y, z float64) float64 {
	return math.Sqrt(x*x + y*y + z*z)
}

func dot(ax, ay, az, bx, by, bz float64) float64 {
	return ax*bx + ay*by + az*bz
}

func cross(ax, ay, az, bx, by, bz float64) (float64, float64, float64) {
	return ay*bz - az*by,
		az*bx - ax*bz,
		ax*by - ay*bx
}

func normalize3(x, y, z float64) (float64, float64, float64, error) {
	l := vecLen(x, y, z)
	if l < eps {
		return 0, 0, 0, errors.New("zero-length vector")
	}
	return x / l, y / l, z / l, nil
}

// -----------------------------
// Decompose TRS
// -----------------------------

// DecomposeTRS decomposes an affine matrix into translation, rotation(quaternion), scale.
// Assumes the matrix has no shear.
// Column-major + column-vector convention.
//
// For glTF-style TRS:
//   - translation is the last column
//   - scale is the length of the first 3 columns
//   - rotation is extracted from the normalized basis columns
func DecomposeTRS(m Mat4) (Vec3, Quat, Vec3, error) {
	// Translation
	t := Vec3{
		X: m[12],
		Y: m[13],
		Z: m[14],
	}

	// Columns of upper-left 3x3
	c0x, c0y, c0z := m[0], m[1], m[2]
	c1x, c1y, c1z := m[4], m[5], m[6]
	c2x, c2y, c2z := m[8], m[9], m[10]

	// Scale = column lengths
	sx := vecLen(c0x, c0y, c0z)
	sy := vecLen(c1x, c1y, c1z)
	sz := vecLen(c2x, c2y, c2z)

	if sx < eps || sy < eps || sz < eps {
		return Vec3{}, Quat{}, Vec3{}, errors.New("scale contains zero; cannot extract rotation")
	}

	// Normalize columns => rotation basis candidate
	r0x, r0y, r0z := c0x/sx, c0y/sx, c0z/sx
	r1x, r1y, r1z := c1x/sy, c1y/sy, c1z/sy
	r2x, r2y, r2z := c2x/sz, c2y/sz, c2z/sz

	// Handle reflection / negative determinant.
	// If det(R) < 0, flip one axis and one scale sign.
	cx, cy, cz := cross(r0x, r0y, r0z, r1x, r1y, r1z)
	d := dot(cx, cy, cz, r2x, r2y, r2z)
	if d < 0 {
		// Flip Z axis by convention
		sz = -sz
		r2x, r2y, r2z = -r2x, -r2y, -r2z
	}

	q := quatFromRotationColumns(
		r0x, r0y, r0z,
		r1x, r1y, r1z,
		r2x, r2y, r2z,
	)

	s := Vec3{X: sx, Y: sy, Z: sz}
	return t, q, s, nil
}

// quatFromRotationColumns builds a quaternion from a 3x3 rotation matrix
// whose columns are r0, r1, r2.
// Matrix entries in row/col form:
// [ m00 m01 m02 ]
// [ m10 m11 m12 ]
// [ m20 m21 m22 ]
func quatFromRotationColumns(
	r0x, r0y, r0z float64,
	r1x, r1y, r1z float64,
	r2x, r2y, r2z float64,
) Quat {
	m00, m10, m20 := r0x, r0y, r0z
	m01, m11, m21 := r1x, r1y, r1z
	m02, m12, m22 := r2x, r2y, r2z

	trace := m00 + m11 + m22

	var q Quat
	if trace > 0 {
		s := math.Sqrt(trace+1.0) * 2.0
		q.W = 0.25 * s
		q.X = (m21 - m12) / s
		q.Y = (m02 - m20) / s
		q.Z = (m10 - m01) / s
	} else if m00 > m11 && m00 > m22 {
		s := math.Sqrt(1.0+m00-m11-m22) * 2.0
		q.W = (m21 - m12) / s
		q.X = 0.25 * s
		q.Y = (m01 + m10) / s
		q.Z = (m02 + m20) / s
	} else if m11 > m22 {
		s := math.Sqrt(1.0+m11-m00-m22) * 2.0
		q.W = (m02 - m20) / s
		q.X = (m01 + m10) / s
		q.Y = 0.25 * s
		q.Z = (m12 + m21) / s
	} else {
		s := math.Sqrt(1.0+m22-m00-m11) * 2.0
		q.W = (m10 - m01) / s
		q.X = (m02 + m20) / s
		q.Y = (m12 + m21) / s
		q.Z = 0.25 * s
	}

	return normalizeQuat(q)
}

func normalizeQuat(q Quat) Quat {
	l := math.Sqrt(q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W)
	if l < eps {
		return Quat{W: 1}
	}
	return Quat{
		X: q.X / l,
		Y: q.Y / l,
		Z: q.Z / l,
		W: q.W / l,
	}
}

// -----------------------------
// Main API you need
// -----------------------------

func ComputeInstanceLocalTRS(MTargetWorld, MTile Mat4) (Mat4, Vec3, Quat, Vec3, error) {
	invTile, err := InverseAffine(MTile)
	if err != nil {
		return Mat4{}, Vec3{}, Quat{}, Vec3{}, fmt.Errorf("inverse tile failed: %w", err)
	}

	MInstanceLocal := Mul4(invTile, MTargetWorld)

	t, r, s, err := DecomposeTRS(MInstanceLocal)
	if err != nil {
		return Mat4{}, Vec3{}, Quat{}, Vec3{}, fmt.Errorf("decompose instance local failed: %w", err)
	}

	return MInstanceLocal, t, r, s, nil
}

package service

import (
	"math"
	"testing"
)

func TestRegistrationRigidTransform(t *testing.T) {
	angle := 7.5 * math.Pi / 180
	wantRotation := [3][3]float64{
		{math.Cos(angle), -math.Sin(angle), 0},
		{math.Sin(angle), math.Cos(angle), 0},
		{0, 0, 1},
	}
	wantTranslation := registrationVector{1.25, -2.5, 0.75}
	sources := []registrationVector{
		{0, 0, 0},
		{30, 0, 0},
		{0, 12, 0},
		{18, 7, 4},
		{-4, 9, 2},
	}
	targets := make([]registrationVector, len(sources))
	for index, source := range sources {
		rotated := registrationMulMatrixVector(wantRotation, source)
		targets[index] = registrationVector{
			rotated[0] + wantTranslation[0],
			rotated[1] + wantTranslation[1],
			rotated[2] + wantTranslation[2],
		}
	}

	rotation, quaternion, translation := registrationRigidTransform(sources, targets)
	for index := range sources {
		got := registrationMulMatrixVector(rotation, sources[index])
		got[0] += translation[0]
		got[1] += translation[1]
		got[2] += translation[2]
		if distance := registrationNorm(registrationSubtract(got, targets[index])); distance > 1e-8 {
			t.Fatalf("point %d residual is %g", index, distance)
		}
	}
	if quaternion[3] < 0 {
		t.Fatalf("quaternion sign is not normalized: %v", quaternion)
	}
}


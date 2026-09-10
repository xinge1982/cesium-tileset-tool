package mapmodel

import (
	"fmt"
	"testing"
)

func TestComputeInstanceLocalTRS(tt *testing.T) {

	// Example:
	// Suppose tile only has translation.
	MTile := [16]float64{
		-0.8880958982833015,
		-0.4596582159087073,
		0,
		0,
		0.29564361090886726,
		-0.5712067556168219,
		0.7657138484228104,
		0,
		-0.3519666614626194,
		0.6800273280430196,
		0.6431813914701897,
		0,
		-2248006.4987994987,
		4343325.718547701,
		4080490.745770137,
		1,
	}

	// Replace this with your real M_target_world.
	MTargetWorld := Mat4{-0.1934448877401275, 0.33434855523082546, -0.4612302066534683, 0,
		-0.9248303549156425, -0.5211596980018732, 0.010091949847847252, 0,
		-0.4451222608647662, 0.804809725958132, 0.7701004612655364, 0,
		-2248014.3820858723, 4343325.922585557, 4080502.5893500615, 1}

	mLocal, t, r, s, err := ComputeInstanceLocalTRS(MTargetWorld, MTile)
	if err != nil {
		panic(err)
	}

	fmt.Println("M_instance_local =", mLocal)
	fmt.Printf("T = %+v\n", t)
	fmt.Printf("R = %+v\n", r) // glTF quaternion order is [x, y, z, w]
	fmt.Printf("S = %+v\n", s)
}

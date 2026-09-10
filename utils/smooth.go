package utils

import (
	"math"
)

func MakeSmooth(path [][]float64, numIterations float64) [][]float64 {
	numIterations = math.Min(math.Max(float64(numIterations), 1), 10.0)
	for numIterations > 0 {
		path = smooth(path)
		numIterations--
	}
	return path
}

func smooth(input [][]float64) (output [][]float64) {
	output = make([][]float64, 0)
	if len(input) > 0 {
		output = append(output, input[0])
	}

	for i := 0; i < len(input)-1; i++ {
		var p0 = input[i]
		var p1 = input[i+1]

		if len(p0) != 3 {
			return
		}

		if p0[0] == p1[0] && p0[1] == p1[1] {
			output = append(output, p0)
			continue
		}

		var p0x = p0[0]
		var p0y = p0[1]
		var p1x = p1[0]
		var p1y = p1[1]

		var Q = []float64{0.75*p0x + 0.25*p1x, 0.75*p0y + 0.25*p1y, p0[2]}
		var R = []float64{0.25*p0x + 0.75*p1x, 0.25*p0y + 0.75*p1y, p1[2]}
		output = append(output, Q, R)
	}

	if len(input) > 1 {
		output = append(output, []float64{input[len(input)-1][0], input[len(input)-1][1], input[len(input)-1][2]})
	}

	return
}

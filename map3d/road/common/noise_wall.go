package common

import (
	"fmt"
	"math"
)

const (
	defaultNoiseWallHeight       = float32(3.5)
	defaultNoiseWallBendOffset   = float32(0.35)
	defaultNoiseWallBendStartRat = float32(0.82)
	defaultNoiseWallTileMeters   = float32(2.0)
	defaultNoiseWallPostSpacing  = float32(2.0)
	defaultNoiseWallPostWidth    = float32(0.08)
	defaultNoiseWallPostDepth    = float32(0.08)
)

func buildNoiseWallProfile(height, bendOffset float32) Profile2D {
	if height <= 0 {
		height = defaultNoiseWallHeight
	}
	if bendOffset <= 0 {
		bendOffset = defaultNoiseWallBendOffset
	}
	bendStart := height * defaultNoiseWallBendStartRat
	if bendStart < height*0.5 {
		bendStart = height * 0.5
	}
	if bendStart > height {
		bendStart = height
	}

	return Profile2D{
		Name: "noise_wall_bent",
		Points: [][2]float32{
			{0, 0},
			{0, bendStart},
			{-bendOffset, height},
		},
		Closed: false,
	}
}

func buildNoiseWallMesh(line LocalLine, direction int, height, bendOffset, repeatU float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("noise wall line requires at least 2 distinct points")
	}
	if repeatU <= 0 {
		repeatU = defaultNoiseWallTileMeters
	}

	profile := buildNoiseWallProfile(height, bendOffset)
	sign := float32(1)
	if direction == 2 {
		sign = -1
	}

	profiles := NewProfileFactory()
	pos, normals, uv, indices, err := profiles.SweepProfileMesh(profile, LocalLine{Points: points}, SweepMeshOptions{
		LateralSign: sign,
		RepeatU:     repeatU,
		CloseCaps:   false,
		HardEdges:   true,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	var maxV float32
	for _, p := range uv {
		if p[1] > maxV {
			maxV = p[1]
		}
	}
	if maxV > 0 {
		for i := range uv {
			uv[i][1] /= maxV
			uv[i][1] = 1 - uv[i][1]
		}
	}
	return pos, normals, uv, indices, nil
}

func noiseWallPostTransforms(line LocalLine, direction int, height, spacing, width, depth float32) ([]instancedTransform, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, fmt.Errorf("noise wall line requires at least 2 distinct points")
	}
	if height <= 0 {
		height = defaultNoiseWallHeight
	}
	if spacing <= 0 {
		spacing = defaultNoiseWallPostSpacing
	}
	if width <= 0 {
		width = defaultNoiseWallPostWidth
	}
	if depth <= 0 {
		depth = defaultNoiseWallPostDepth
	}

	samples := samplePolylineForPosts(points, spacing)
	posts := make([]instancedTransform, 0, len(samples))
	scaleX := float32(-1)
	if direction == 2 {
		scaleX = 1
	}
	for _, s := range samples {
		posts = append(posts, instancedTransform{
			Translation: s.Point,
			Rotation:    quaternionFromForward(s.Forward),
			Scale:       [3]float32{scaleX, 1, 1},
		})
	}
	return posts, nil
}

func buildNoiseWallBentPostMesh(height, width, depth, bendOffset float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	if height <= 0 {
		height = defaultNoiseWallHeight
	}
	if width <= 0 {
		width = defaultNoiseWallPostWidth
	}
	if depth <= 0 {
		depth = defaultNoiseWallPostDepth
	}
	if bendOffset <= 0 {
		bendOffset = defaultNoiseWallBendOffset
	}

	bendStart := height * defaultNoiseWallBendStartRat
	if bendStart < height*0.5 {
		bendStart = height * 0.5
	}
	center := [][3]float32{
		{0, 0, 0},
		{0, bendStart, 0},
		{-bendOffset, height, 0},
	}
	tangents := make([][3]float32, len(center))
	normals2D := make([][3]float32, len(center))
	for i := range center {
		var dx, dy float32
		switch {
		case i == 0:
			dx = center[1][0] - center[0][0]
			dy = center[1][1] - center[0][1]
		case i == len(center)-1:
			dx = center[i][0] - center[i-1][0]
			dy = center[i][1] - center[i-1][1]
		default:
			dx = center[i+1][0] - center[i-1][0]
			dy = center[i+1][1] - center[i-1][1]
		}
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l <= 1e-6 {
			tangents[i] = [3]float32{0, 1, 0}
			normals2D[i] = [3]float32{1, 0, 0}
			continue
		}
		tx, ty := dx/l, dy/l
		tangents[i] = [3]float32{tx, ty, 0}
		normals2D[i] = [3]float32{-ty, tx, 0}
	}

	frame := make([][4][3]float32, len(center))
	halfD := depth * 0.5
	for i, c := range center {
		n := normals2D[i]
		b := [3]float32{0, 0, 1}
		frame[i][0] = [3]float32{c[0] + b[0]*halfD, c[1] + b[1]*halfD, c[2] + b[2]*halfD}
		frame[i][1] = [3]float32{c[0] - n[0]*width + b[0]*halfD, c[1] - n[1]*width + b[1]*halfD, c[2] + b[2]*halfD}
		frame[i][2] = [3]float32{c[0] - n[0]*width - b[0]*halfD, c[1] - n[1]*width - b[1]*halfD, c[2] - b[2]*halfD}
		frame[i][3] = [3]float32{c[0] - b[0]*halfD, c[1] - b[1]*halfD, c[2] - b[2]*halfD}
	}

	var pos [][3]float32
	var normals [][3]float32
	var uv [][2]float32
	var indices []uint32
	for i := 0; i < len(frame)-1; i++ {
		u0 := float32(i) / float32(len(frame)-1)
		u1 := float32(i+1) / float32(len(frame)-1)
		for j := 0; j < 4; j++ {
			k := (j + 1) % 4
			a := frame[i][j]
			b := frame[i+1][j]
			c := frame[i+1][k]
			d := frame[i][k]
			n := normalize3(triangleNormal(a, b, c))
			appendQuadFlat(&pos, &normals, &uv, &indices, a, b, c, d, [2]float32{u0, 0}, [2]float32{u1, 0}, [2]float32{u1, 1}, [2]float32{u0, 1})
			last := len(normals)
			for m := last - 4; m < last; m++ {
				normals[m] = n
			}
		}
	}
	appendProfileCap(&pos, &normals, &uv, &indices, frame[0][:], true, nil)
	appendProfileCap(&pos, &normals, &uv, &indices, frame[len(frame)-1][:], false, nil)
	return pos, normals, uv, indices, nil
}

package common

import (
	"math"

	poly2tri "github.com/ByteArena/poly2tri-go"
)

// TriangulateSubgradeExperimental builds a corridor mesh by sampling cross-sections
// along a curved reference line and directly triangulating adjacent section quads.
func TriangulateSubgradeExperimental(ring LocalRings) ([][3]float32, []uint32, bool) {
	return triangulateSubgradeStripCandidates(ring)
}

func TriangulateSubgradeExperimentalAdaptive(ring LocalRings, roadID int64) ([][3]float32, []uint32, bool) {
	_ = roadID
	return triangulateSubgradeStripCandidates(ring)
}

func triangulateSubgradeStripCandidates(ring LocalRings) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 {
		return nil, nil, false
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 6 {
		return nil, nil, false
	}
	var (
		bestPos   [][3]float32
		bestIdx   []uint32
		bestScore = math.MaxFloat64
		bestTier  = 99
	)
	polyArea := polygonAreaXZ(outer)

	eval := func(pos [][3]float32, idx []uint32, ok bool, requireFit bool, tier int) {
		if !ok || len(idx) < 3 {
			return
		}
		triArea := trianglesAreaXZ(pos, idx)
		if polyArea <= 0 || triArea <= 0 {
			return
		}
		ratio := triArea / polyArea
		if ratio < 0.90 || ratio > 1.10 {
			return
		}
		if requireFit && !trianglesFitPolygonXZ(outer, pos, idx) {
			return
		}
		score := math.Abs(1-ratio)*2.0 + 1.0/float64(len(idx)/3)
		if tier < bestTier || (tier == bestTier && score < bestScore) {
			bestTier = tier
			bestScore = score
			bestPos = pos
			bestIdx = idx
		}
	}

	if pos, idx, ok := triangulateSubgradeByPoly2Tri(ring); ok {
		eval(pos, idx, true, true, -2)
	}

	if pos, idx, ok := triangulateSubgradeByBoundaryChains(ring); ok {
		eval(pos, idx, true, true, -1)
	}

	centerlines := make([]LocalLine, 0, 3)
	if cl, ok := estimateDenseCurvedCenterlineFromOuter(outer); ok && len(cl.Points) >= 4 {
		centerlines = append(centerlines, cl)
	}
	if cl, ok := estimateLocalCenterlineFromOuterWithMinRatio(outer, 1.0); ok && len(cl.Points) >= 3 {
		centerlines = append(centerlines, cl)
	}
	if cl, ok := estimateLocalCenterlineFromOuter(outer); ok && len(cl.Points) >= 3 {
		centerlines = append(centerlines, cl)
	}
	for _, cl := range centerlines {
		for _, spacing := range []float64{0.5, 0.75, 1.0, 1.5, 2.0, 3.0} {
			left, right, center, ok := buildSubgradeSectionPairsAlongCurve(outer, cl.Points, spacing)
			if !ok {
				continue
			}
			smoothStripElevations(left, right)
			left, right = fitSectionWidthsToArea(left, right, center, polyArea)
			pos, idx, ok := triangulateSectionQuads(left, right, center)
			eval(pos, idx, ok, false, 0)
		}
		for _, sliceLen := range []float64{8.0, 12.0, 16.0, 24.0} {
			if pos, idx, ok := triangulateSubgradeByCenterlineSlices(ring, cl, sliceLen); ok {
				eval(pos, idx, true, true, 1)
			}
		}
	}

	if pos, idx, ok := triangulateSubgradeDirectionalByPCA(ring); ok {
		eval(pos, idx, true, true, 2)
	}
	for _, chunk := range []float64{20, 40, 60} {
		if pos, idx, ok := triangulateSubgradeByAxisChunks(ring, chunk); ok {
			eval(pos, idx, true, true, 3)
		}
	}
	for _, cell := range []float64{20, 30, 35} {
		if pos, idx, ok := triangulateSubgradeByGridCells(ring, cell, cell); ok {
			eval(pos, idx, true, true, 4)
		}
	}
	if len(bestIdx) < 3 {
		return nil, nil, false
	}
	return bestPos, bestIdx, true
}

func triangulateSubgradeByPoly2Tri(ring LocalRings) ([][3]float32, []uint32, bool) {
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 3 {
		return nil, nil, false
	}
	if hasSelfIntersectionXZ(outer) {
		return nil, nil, false
	}

	pos := make([][3]float32, 0, len(outer))
	indexOf := make(map[*poly2tri.Point]uint32, len(outer))
	contour := make([]*poly2tri.Point, 0, len(outer))
	if polygonSignedAreaXZ(outer) < 0 {
		outer = reversePoints(outer)
	}
	for _, p := range outer {
		if len(pos) > 0 && segmentLengthXZ(pos[len(pos)-1], p) < 1e-4 {
			continue
		}
		pos = append(pos, p)
		pt := poly2tri.NewPoint(float64(p[0]), float64(p[2]))
		indexOf[pt] = uint32(len(pos) - 1)
		contour = append(contour, pt)
	}
	if len(contour) < 3 {
		return nil, nil, false
	}

	ctx := poly2tri.NewSweepContext(contour, false)
	for _, sp := range subgradePoly2TriSteinerPoints(outer) {
		pt := poly2tri.NewPoint(float64(sp[0]), float64(sp[2]))
		pos = append(pos, sp)
		indexOf[pt] = uint32(len(pos) - 1)
		ctx.AddPoint(pt)
	}
	for _, hole := range ring.Holes {
		pts, holePos, ok := poly2triHolePoints(hole)
		if !ok {
			return nil, nil, false
		}
		for i, p := range pts {
			pos = append(pos, holePos[i])
			indexOf[p] = uint32(len(pos) - 1)
		}
		ctx.AddHole(pts)
	}

	ok := true
	func() {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		ctx.Triangulate()
	}()
	if !ok {
		return nil, nil, false
	}

	tris := ctx.GetTriangles()
	if len(tris) == 0 {
		return nil, nil, false
	}
	idx := make([]uint32, 0, len(tris)*3)
	for _, tri := range tris {
		for i := 0; i < 3; i++ {
			p := tri.GetPoint(i)
			v, exists := indexOf[p]
			if !exists {
				return nil, nil, false
			}
			idx = append(idx, v)
		}
	}
	return pos, idx, true
}

func subgradePoly2TriSteinerPoints(outer [][3]float32) [][3]float32 {
	local2 := make([][2]float64, len(outer))
	for i, p := range outer {
		local2[i] = [2]float64{float64(p[0]), float64(p[2])}
	}
	cx, cz, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok {
		return nil
	}
	minT, maxT, ok := projectionSpan(local2, cx, cz, axisX, axisZ)
	if !ok {
		return nil
	}
	perpX, perpZ := -axisZ, axisX
	minV, maxV, ok := projectionSpan(local2, cx, cz, perpX, perpZ)
	if !ok {
		return nil
	}
	longSpan := maxT - minT
	shortSpan := maxV - minV
	if shortSpan <= 1e-6 || longSpan/shortSpan < 3.0 {
		return nil
	}

	cl, ok := estimateDenseCurvedCenterlineFromOuter(outer)
	if !ok || len(cl.Points) < 3 {
		return nil
	}
	step := 1.0
	switch {
	case longSpan/shortSpan >= 10:
		step = 0.5
	case longSpan/shortSpan >= 6:
		step = 0.75
	case shortSpan < 8:
		step = 0.75
	}
	left, right, center, ok := buildSubgradeSectionPairsAlongCurve(outer, cl.Points, step)
	if !ok || len(center) < 3 {
		points := samplePolylineBySpacing(cl.Points, step)
		return dedupePoly2TriSteiner(points[1 : len(points)-1])
	}
	rowFactors := []float32{0.50}
	switch {
	case longSpan/shortSpan >= 10:
		rowFactors = []float32{0.12, 0.24, 0.36, 0.50, 0.64, 0.76, 0.88}
	case longSpan/shortSpan >= 6:
		rowFactors = []float32{0.16, 0.32, 0.50, 0.68, 0.84}
	default:
		rowFactors = []float32{0.25, 0.50, 0.75}
	}
	out := make([][3]float32, 0, len(center)*(len(rowFactors)+2))
	for i := 1; i < len(center)-1; i++ {
		for _, f := range rowFactors {
			p := lerpPoint3(left[i], right[i], f)
			if pointInPolygonXZ(outer, p[0], p[2]) {
				out = append(out, p)
			}
		}
		if i > 1 && i < len(center)-2 {
			prevMid := lerpPoint3(left[i-1], right[i-1], 0.5)
			nextMid := lerpPoint3(left[i+1], right[i+1], 0.5)
			diagMid := lerpPoint3(prevMid, nextMid, 0.5)
			if pointInPolygonXZ(outer, diagMid[0], diagMid[2]) {
				out = append(out, diagMid)
			}
		}
	}
	return dedupePoly2TriSteiner(out)
}

func dedupePoly2TriSteiner(points [][3]float32) [][3]float32 {
	if len(points) == 0 {
		return nil
	}
	out := make([][3]float32, 0, len(points))
	for _, p := range points {
		dup := false
		for i := range out {
			if segmentLengthXZ(out[i], p) < 0.15 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

func lerpPoint3(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

func poly2triHolePoints(hole [][3]float32) ([]*poly2tri.Point, [][3]float32, bool) {
	hole = dedupeClosedRing(hole)
	if len(hole) < 3 || hasSelfIntersectionXZ(hole) {
		return nil, nil, false
	}
	if polygonSignedAreaXZ(hole) > 0 {
		hole = reversePoints(hole)
	}
	pts := make([]*poly2tri.Point, 0, len(hole))
	pos := make([][3]float32, 0, len(hole))
	for _, p := range hole {
		if len(pos) > 0 && segmentLengthXZ(pos[len(pos)-1], p) < 1e-4 {
			continue
		}
		pos = append(pos, p)
		pts = append(pts, poly2tri.NewPoint(float64(p[0]), float64(p[2])))
	}
	if len(pts) < 3 {
		return nil, nil, false
	}
	return pts, pos, true
}

func triangulateSubgradeByBoundaryChains(ring LocalRings) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 {
		return nil, nil, false
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 8 {
		return nil, nil, false
	}
	left, right, center, ok := inferBoundaryChainsFromRing(outer)
	if !ok {
		return nil, nil, false
	}
	smoothStripElevations(left, right)
	return triangulateBoundaryChains(left, right, center)
}

func inferBoundaryChainsFromRing(outer [][3]float32) ([][3]float32, [][3]float32, [][3]float32, bool) {
	local2 := make([][2]float64, len(outer))
	var cx, cz float64
	for i, p := range outer {
		x := float64(p[0])
		z := float64(p[2])
		local2[i] = [2]float64{x, z}
		cx += x
		cz += z
	}
	cx /= float64(len(outer))
	cz /= float64(len(outer))
	_, _, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok {
		return nil, nil, nil, false
	}

	proj := make([]float64, len(outer))
	minT := math.MaxFloat64
	maxT := -math.MaxFloat64
	for i, p := range outer {
		t := projectionValue(p, cx, cz, axisX, axisZ)
		proj[i] = t
		if t < minT {
			minT = t
		}
		if t > maxT {
			maxT = t
		}
	}
	if maxT-minT < 1 {
		return nil, nil, nil, false
	}

	minCands := topProjectionIndexes(proj, true, 12)
	maxCands := topProjectionIndexes(proj, false, 12)
	if len(minCands) == 0 || len(maxCands) == 0 {
		return nil, nil, nil, false
	}

	bestScore := math.MaxFloat64
	var bestLeft, bestRight, bestCenter [][3]float32
	for _, si := range minCands {
		for _, ei := range maxCands {
			if si == ei {
				continue
			}
			pathA := extractRingPath(outer, si, ei)
			pathB := extractRingPath(outer, ei, si)
			if len(pathA) < 4 || len(pathB) < 4 {
				continue
			}
			center, left, right, ok := buildBoundaryChainCenterline(pathA, pathB)
			if !ok {
				continue
			}
			if !stripChainsAreUsable(left, right, center) {
				continue
			}
			leftLen := polylineLengthXZ(left)
			rightLen := polylineLengthXZ(right)
			if leftLen <= 0 || rightLen <= 0 {
				continue
			}
			shorter := math.Min(leftLen, rightLen)
			longer := math.Max(leftLen, rightLen)
			score := longer / shorter
			score += segmentLengthXZ(pathA[0], pathB[0]) * 0.02
			score += segmentLengthXZ(pathA[len(pathA)-1], pathB[len(pathB)-1]) * 0.02
			score += segmentLengthXZ(center[0], center[len(center)-1]) / math.Max(polylineLengthXZ(center), 1.0)
			if score < bestScore {
				bestScore = score
				bestLeft = left
				bestRight = right
				bestCenter = center
			}
		}
	}
	if len(bestCenter) < 4 {
		return nil, nil, nil, false
	}
	return bestLeft, bestRight, bestCenter, true
}

func topProjectionIndexes(proj []float64, wantMin bool, limit int) []int {
	if len(proj) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(proj) {
		limit = len(proj)
	}
	out := make([]int, 0, limit)
	for i := range proj {
		out = append(out, i)
	}
	for i := 1; i < len(out); i++ {
		v := out[i]
		j := i - 1
		for j >= 0 {
			better := proj[v] < proj[out[j]]
			if !wantMin {
				better = proj[v] > proj[out[j]]
			}
			if !better {
				break
			}
			out[j+1] = out[j]
			j--
		}
		out[j+1] = v
	}
	return out[:limit]
}

func buildBoundaryChainCenterline(pathA, pathB [][3]float32) ([][3]float32, [][3]float32, [][3]float32, bool) {
	pathA = dedupeLinePoints(pathA)
	pathB = dedupeLinePoints(pathB)
	if len(pathA) < 4 || len(pathB) < 4 {
		return nil, nil, nil, false
	}
	forwardGap := segmentLengthXZ(pathA[0], pathB[0]) + segmentLengthXZ(pathA[len(pathA)-1], pathB[len(pathB)-1])
	reverseGap := segmentLengthXZ(pathA[0], pathB[len(pathB)-1]) + segmentLengthXZ(pathA[len(pathA)-1], pathB[0])
	if reverseGap < forwardGap {
		pathB = reversePoints(pathB)
	}
	aCum := polylineCumLengths(pathA)
	bCum := polylineCumLengths(pathB)
	aParams, ok := normalizedPolylineParams(aCum)
	if !ok {
		return nil, nil, nil, false
	}
	bParams, ok := normalizedPolylineParams(bCum)
	if !ok {
		return nil, nil, nil, false
	}
	params := mergedBoundaryParams(aParams, bParams)
	if len(params) < 4 {
		return nil, nil, nil, false
	}
	aSamples, ok := sampleBoundaryByParam(pathA, aParams, params)
	if !ok {
		return nil, nil, nil, false
	}
	bSamples, ok := sampleBoundaryByParam(pathB, bParams, params)
	if !ok {
		return nil, nil, nil, false
	}
	center := make([][3]float32, len(aSamples))
	for i := range aSamples {
		center[i] = [3]float32{
			(aSamples[i][0] + bSamples[i][0]) * 0.5,
			(aSamples[i][1] + bSamples[i][1]) * 0.5,
			(aSamples[i][2] + bSamples[i][2]) * 0.5,
		}
	}
	center = dedupeLinePoints(center)
	if len(center) < 4 {
		return nil, nil, nil, false
	}
	startTan, ok := centerlineTangent(center, 0)
	if !ok {
		return nil, nil, nil, false
	}
	sideA := boundarySideSign(pathA, center[0], startTan)
	sideB := boundarySideSign(pathB, center[0], startTan)
	left := pathA
	right := pathB
	if sideA < sideB {
		left, right = pathB, pathA
	}
	left = orientBoundaryAlongCenterline(left, center)
	right = orientBoundaryAlongCenterline(right, center)
	return center, left, right, true
}

func fitSectionWidthsToArea(left, right, center [][3]float32, targetArea float64) ([][3]float32, [][3]float32) {
	if len(left) < 2 || len(left) != len(right) || len(left) != len(center) || targetArea <= 0 {
		return left, right
	}
	outL := append([][3]float32(nil), left...)
	outR := append([][3]float32(nil), right...)
	for iter := 0; iter < 3; iter++ {
		pos, idx, ok := triangulateSectionQuads(outL, outR, center)
		if !ok || len(idx) < 3 {
			break
		}
		area := trianglesAreaXZ(pos, idx)
		if area <= 0 {
			break
		}
		scale := math.Sqrt(targetArea / area)
		if math.Abs(1-scale) < 0.01 {
			break
		}
		if scale < 0.80 {
			scale = 0.80
		} else if scale > 1.20 {
			scale = 1.20
		}
		for i := range outL {
			outL[i] = scaleSectionPoint(center[i], outL[i], float32(scale))
			outR[i] = scaleSectionPoint(center[i], outR[i], float32(scale))
		}
	}
	return outL, outR
}

func scaleSectionPoint(center, p [3]float32, scale float32) [3]float32 {
	return [3]float32{
		center[0] + (p[0]-center[0])*scale,
		p[1],
		center[2] + (p[2]-center[2])*scale,
	}
}

func triangulateSubgradeByCenterlineSlices(ring LocalRings, centerline LocalLine, sliceLength float64) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 {
		return nil, nil, false
	}
	outer := dedupeClosedRing(ring.Outer)
	cl := samplePolylineBySpacing(dedupeLinePoints(centerline.Points), sliceLength)
	if len(outer) < 6 || len(cl) < 2 {
		return nil, nil, false
	}
	if sliceLength <= 0 {
		sliceLength = 12.0
	}

	var allPos [][3]float32
	var allIdx []uint32
	for i := 0; i < len(cl)-1; i++ {
		t0, ok0 := centerlineTangent(cl, i)
		t1, ok1 := centerlineTangent(cl, i+1)
		if !ok0 || !ok1 {
			continue
		}
		start := cl[i]
		end := cl[i+1]
		slice := clipRingBetweenStations(outer, start, t0, end, t1, i == 0, i == len(cl)-2)
		if len(slice) > 0 {
			slice = sanitizeSubgradeRing(append(append([][3]float32(nil), slice...), slice[0]), 0.01, 0.02, true)
		}
		slice = dedupeClosedRing(slice)
		if len(slice) < 3 {
			continue
		}
		r := LocalRings{Outer: append(append([][3]float32(nil), slice...), slice[0])}
		pos, idx, err := triangulatePlainSurfaceRing(r)
		if err != nil || len(idx) < 3 {
			continue
		}
		base := uint32(len(allPos))
		allPos = append(allPos, pos...)
		for _, v := range idx {
			allIdx = append(allIdx, base+v)
		}
	}
	if len(allIdx) < 3 {
		return nil, nil, false
	}
	return allPos, allIdx, true
}

func estimateDenseCurvedCenterlineFromOuter(outer [][3]float32) (LocalLine, bool) {
	outer = dedupeClosedRing(outer)
	if len(outer) < 6 {
		return LocalLine{}, false
	}
	local2 := make([][2]float64, len(outer))
	var cx, cz float64
	for i, p := range outer {
		x := float64(p[0])
		z := float64(p[2])
		local2[i] = [2]float64{x, z}
		cx += x
		cz += z
	}
	cx /= float64(len(outer))
	cz /= float64(len(outer))
	_, _, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok {
		return LocalLine{}, false
	}
	minT, maxT, ok := projectionSpan(local2, cx, cz, axisX, axisZ)
	if !ok || maxT-minT < 1 {
		return LocalLine{}, false
	}
	perp := [2]float64{-axisZ, axisX}
	span := maxT - minT
	count := int(math.Ceil(span / 1.0))
	if count < 24 {
		count = 24
	}
	if count > 640 {
		count = 640
	}
	points := make([][3]float32, 0, count+1)
	for i := 0; i <= count; i++ {
		t := minT + span*float64(i)/float64(count)
		base := [2]float64{cx + axisX*t, cz + axisZ*t}
		if p, ok := centerlinePointAtSampleLocal(outer, base, perp); ok {
			points = append(points, p)
		}
	}
	points = dedupeLinePoints(points)
	if len(points) < 4 {
		return LocalLine{}, false
	}
	smoothLocalCenterline(points, 1, 1)
	points = samplePolylineBySpacing(points, 1.0)
	for iter := 0; iter < 3; iter++ {
		_, _, center, ok := buildSubgradeSectionPairsAlongCurve(outer, points, 1.0)
		if !ok || len(center) < 4 {
			break
		}
		points = center
		smoothLocalCenterline(points, 1, 1)
		points = samplePolylineBySpacing(points, 0.75)
	}
	points = dedupeLinePoints(points)
	if len(points) < 4 {
		return LocalLine{}, false
	}
	return LocalLine{Points: points}, true
}

func triangulateSubgradeByAxisChunks(ring LocalRings, chunkLength float64) ([][3]float32, []uint32, bool) {
	pos, idx, ok, _, _ := triangulateSubgradeByAxisChunksDetailed(ring, chunkLength)
	return pos, idx, ok
}

func triangulateSubgradeByAxisChunksDetailed(ring LocalRings, chunkLength float64) ([][3]float32, []uint32, bool, float64, float64) {
	if len(ring.Holes) > 0 {
		return nil, nil, false, 0, 0
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 6 {
		return nil, nil, false, 0, 0
	}
	if chunkLength <= 0 {
		chunkLength = 40.0
	}

	local2 := make([][2]float64, len(outer))
	var cx, cz float64
	for i, p := range outer {
		x := float64(p[0])
		z := float64(p[2])
		local2[i] = [2]float64{x, z}
		cx += x
		cz += z
	}
	cx /= float64(len(outer))
	cz /= float64(len(outer))

	_, _, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok {
		return nil, nil, false, 0, 0
	}
	minT, maxT, ok := projectionSpan(local2, cx, cz, axisX, axisZ)
	if !ok || maxT-minT < chunkLength*0.5 {
		return nil, nil, false, 0, 0
	}

	var allPos [][3]float32
	var allIdx []uint32
	for start := minT; start < maxT-1e-6; start += chunkLength {
		end := start + chunkLength
		if end > maxT {
			end = maxT
		}
		startBound := start
		endBound := end
		if start > minT {
			startBound += 0.001
		}
		if end < maxT {
			endBound -= 0.001
		}
		chunk := clipRingToProjectionRange(outer, cx, cz, axisX, axisZ, startBound, endBound)
		if len(chunk) > 0 {
			chunk = sanitizeSubgradeRing(append(append([][3]float32(nil), chunk...), chunk[0]), 0.01, 0.02, true)
		}
		chunk = dedupeClosedRing(chunk)
		if len(chunk) < 3 {
			continue
		}
		r := LocalRings{Outer: append(append([][3]float32(nil), chunk...), chunk[0])}
		pos, idx, err := triangulatePlainSurfaceRing(r)
		if err != nil || len(idx) < 3 {
			continue
		}
		base := uint32(len(allPos))
		allPos = append(allPos, pos...)
		for _, v := range idx {
			allIdx = append(allIdx, base+v)
		}
	}
	if len(allIdx) < 3 {
		return nil, nil, false, 0, 0
	}
	polyArea := polygonAreaXZ(outer)
	triArea := trianglesAreaXZ(allPos, allIdx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false, polyArea, triArea
	}
	ratio := triArea / polyArea
	if ratio < 0.90 || ratio > 1.08 {
		return allPos, allIdx, false, polyArea, triArea
	}
	return allPos, allIdx, true, polyArea, triArea
}

func triangulateSubgradeByGridCells(ring LocalRings, cellU, cellV float64) ([][3]float32, []uint32, bool) {
	pos, idx, ok, _, _ := triangulateSubgradeByGridCellsDetailed(ring, cellU, cellV)
	return pos, idx, ok
}

func triangulateSubgradeByGridCellsDetailed(ring LocalRings, cellU, cellV float64) ([][3]float32, []uint32, bool, float64, float64) {
	if len(ring.Holes) > 0 {
		return nil, nil, false, 0, 0
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 6 {
		return nil, nil, false, 0, 0
	}
	if cellU <= 0 {
		cellU = 35
	}
	if cellV <= 0 {
		cellV = cellU
	}

	local2 := make([][2]float64, len(outer))
	var cx, cz float64
	for i, p := range outer {
		x := float64(p[0])
		z := float64(p[2])
		local2[i] = [2]float64{x, z}
		cx += x
		cz += z
	}
	cx /= float64(len(outer))
	cz /= float64(len(outer))
	_, _, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok {
		return nil, nil, false, 0, 0
	}
	perpX, perpZ := -axisZ, axisX
	minU, maxU, okU := projectionSpan(local2, cx, cz, axisX, axisZ)
	minV, maxV, okV := projectionSpan(local2, cx, cz, perpX, perpZ)
	if !okU || !okV {
		return nil, nil, false, 0, 0
	}

	var allPos [][3]float32
	var allIdx []uint32
	for u0 := minU; u0 < maxU-1e-6; u0 += cellU {
		u1 := u0 + cellU
		if u1 > maxU {
			u1 = maxU
		}
		for v0 := minV; v0 < maxV-1e-6; v0 += cellV {
			v1 := v0 + cellV
			if v1 > maxV {
				v1 = maxV
			}
			u0b := u0
			u1b := u1
			v0b := v0
			v1b := v1
			if u0 > minU {
				u0b += 0.001
			}
			if u1 < maxU {
				u1b -= 0.001
			}
			if v0 > minV {
				v0b += 0.001
			}
			if v1 < maxV {
				v1b -= 0.001
			}
			chunk := clipRingToProjectionRange(outer, cx, cz, axisX, axisZ, u0b, u1b)
			chunk = clipRingToProjectionRangeWithAxis(chunk, cx, cz, perpX, perpZ, v0b, v1b)
			if len(chunk) > 0 {
				chunk = sanitizeSubgradeRing(append(append([][3]float32(nil), chunk...), chunk[0]), 0.01, 0.02, true)
			}
			chunk = dedupeClosedRing(chunk)
			if len(chunk) < 3 {
				continue
			}
			r := LocalRings{Outer: append(append([][3]float32(nil), chunk...), chunk[0])}
			pos, idx, err := triangulatePlainSurfaceRing(r)
			if err != nil || len(idx) < 3 {
				continue
			}
			base := uint32(len(allPos))
			allPos = append(allPos, pos...)
			for _, v := range idx {
				allIdx = append(allIdx, base+v)
			}
		}
	}
	if len(allIdx) < 3 {
		return nil, nil, false, 0, 0
	}
	polyArea := polygonAreaXZ(outer)
	triArea := trianglesAreaXZ(allPos, allIdx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false, polyArea, triArea
	}
	ratio := triArea / polyArea
	if ratio < 0.90 || ratio > 1.08 {
		return allPos, allIdx, false, polyArea, triArea
	}
	return allPos, allIdx, true, polyArea, triArea
}

func clipRingToProjectionRange(ring [][3]float32, cx, cz, axisX, axisZ, minT, maxT float64) [][3]float32 {
	return clipRingToProjectionRangeWithAxis(ring, cx, cz, axisX, axisZ, minT, maxT)
}

func clipRingToProjectionRangeWithAxis(ring [][3]float32, cx, cz, axisX, axisZ, minT, maxT float64) [][3]float32 {
	out := clipRingByProjectionHalfPlane(ring, cx, cz, axisX, axisZ, minT, true)
	if len(out) < 3 {
		return nil
	}
	out = clipRingByProjectionHalfPlane(out, cx, cz, axisX, axisZ, maxT, false)
	return out
}

func clipRingBetweenStations(ring [][3]float32, start [3]float32, startTan [2]float64, end [3]float32, endTan [2]float64, includeStart, includeEnd bool) [][3]float32 {
	out := ring
	startEps := 0.0
	endEps := 0.0
	if !includeStart {
		startEps = 0.001
	}
	if !includeEnd {
		endEps = 0.001
	}
	out = clipRingByStationHalfPlane(out, start, startTan, startEps, true)
	if len(out) < 3 {
		return nil
	}
	out = clipRingByStationHalfPlane(out, end, endTan, endEps, false)
	return out
}

func clipRingByStationHalfPlane(ring [][3]float32, station [3]float32, tangent [2]float64, eps float64, keepForward bool) [][3]float32 {
	if len(ring) < 3 {
		return nil
	}
	out := make([][3]float32, 0, len(ring)+4)
	prev := ring[len(ring)-1]
	prevD := stationSignedDistance(prev, station, tangent)
	prevInside := prevD >= eps
	if !keepForward {
		prevInside = prevD <= -eps
	}
	for _, cur := range ring {
		curD := stationSignedDistance(cur, station, tangent)
		curInside := curD >= eps
		if !keepForward {
			curInside = curD <= -eps
		}
		if curInside != prevInside {
			den := curD - prevD
			if math.Abs(den) > 1e-9 {
				t := float32((0 - prevD) / den)
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				out = append(out, interpolatePoint3(prev, cur, t))
			}
		}
		if curInside {
			out = append(out, cur)
		}
		prev = cur
		prevD = curD
		prevInside = curInside
	}
	return dedupeLinePoints(out)
}

func stationSignedDistance(p, station [3]float32, tangent [2]float64) float64 {
	return (float64(p[0])-float64(station[0]))*tangent[0] + (float64(p[2])-float64(station[2]))*tangent[1]
}

func clipRingByProjectionHalfPlane(ring [][3]float32, cx, cz, axisX, axisZ, bound float64, keepGreater bool) [][3]float32 {
	if len(ring) < 3 {
		return nil
	}
	out := make([][3]float32, 0, len(ring)+4)
	prev := ring[len(ring)-1]
	prevT := projectionValue(prev, cx, cz, axisX, axisZ)
	prevInside := prevT >= bound
	if !keepGreater {
		prevInside = prevT <= bound
	}
	for _, cur := range ring {
		curT := projectionValue(cur, cx, cz, axisX, axisZ)
		curInside := curT >= bound
		if !keepGreater {
			curInside = curT <= bound
		}
		if curInside != prevInside {
			den := curT - prevT
			if math.Abs(den) > 1e-9 {
				t := float32((bound - prevT) / den)
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				out = append(out, interpolatePoint3(prev, cur, t))
			}
		}
		if curInside {
			out = append(out, cur)
		}
		prev = cur
		prevT = curT
		prevInside = curInside
	}
	return dedupeLinePoints(out)
}

func projectionValue(p [3]float32, cx, cz, axisX, axisZ float64) float64 {
	return (float64(p[0])-cx)*axisX + (float64(p[2])-cz)*axisZ
}

func buildSubgradeSectionPairsAlongCurve(outer [][3]float32, centerline [][3]float32, spacing float64) ([][3]float32, [][3]float32, [][3]float32, bool) {
	if spacing <= 0 {
		spacing = 2.0
	}
	guidePts := samplePolylineBySpacing(dedupeLinePoints(centerline), spacing)
	if len(guidePts) < 4 {
		return nil, nil, nil, false
	}

	stations := make([][]sectionPairCandidate, 0, len(guidePts))
	usableGuides := make([][3]float32, 0, len(guidePts))
	for i := range guidePts {
		tan, ok := centerlineTangent(guidePts, i)
		if !ok {
			continue
		}
		dir := [2]float64{-tan[1], tan[0]}
		cands := collectSectionPairCandidates(outer, guidePts[i], dir)
		if len(cands) == 0 {
			continue
		}
		stations = append(stations, cands)
		usableGuides = append(usableGuides, guidePts[i])
	}
	if len(stations) < 4 {
		return nil, nil, nil, false
	}

	path, ok := chooseSectionPairPath(stations, usableGuides)
	if !ok {
		return nil, nil, nil, false
	}
	left := make([][3]float32, 0, len(path))
	right := make([][3]float32, 0, len(path))
	center := make([][3]float32, 0, len(path))
	for i, candIdx := range path {
		cand := stations[i][candIdx]
		left = append(left, cand.left)
		right = append(right, cand.right)
		center = append(center, cand.center)
	}
	left, right, center = dedupeSectionTriplets(left, right, center, 0.25)
	if len(left) < 4 || len(left) != len(right) || len(left) != len(center) {
		return nil, nil, nil, false
	}
	return left, right, center, true
}

type sectionPairCandidate struct {
	left   [3]float32
	right  [3]float32
	width  float64
	center [3]float32
}

func selectSectionPairWithContinuity(outer [][3]float32, guide [3]float32, dir [2]float64, prevL, prevR [3]float32, prevW float64, hasPrev bool) ([3]float32, [3]float32, bool) {
	cands := collectSectionPairCandidates(outer, guide, dir)
	if len(cands) == 0 {
		return [3]float32{}, [3]float32{}, false
	}
	bestIdx := -1
	bestCost := math.MaxFloat64
	for i, cand := range cands {
		cost := segmentLengthXZ(cand.center, guide)
		if hasPrev {
			cost += segmentLengthXZ(cand.left, prevL) * 0.9
			cost += segmentLengthXZ(cand.right, prevR) * 0.9
			cost += math.Abs(cand.width-prevW) * 0.75
			if cand.width > prevW*2.5 || cand.width < prevW*0.4 {
				cost += 1000
			}
		} else {
			// Start with the section centered near the guide and prefer a wider local span.
			cost -= cand.width * 0.25
		}
		if cost < bestCost {
			bestCost = cost
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return [3]float32{}, [3]float32{}, false
	}
	return cands[bestIdx].left, cands[bestIdx].right, true
}

func chooseSectionPairPath(stations [][]sectionPairCandidate, guides [][3]float32) ([]int, bool) {
	if len(stations) == 0 || len(stations) != len(guides) {
		return nil, false
	}
	dp := make([][]float64, len(stations))
	prevIdx := make([][]int, len(stations))
	for i := range stations {
		dp[i] = make([]float64, len(stations[i]))
		prevIdx[i] = make([]int, len(stations[i]))
		for j := range stations[i] {
			dp[i][j] = math.MaxFloat64
			prevIdx[i][j] = -1
		}
	}
	for j, cand := range stations[0] {
		dp[0][j] = sectionUnaryCost(cand, guides[0], true)
	}
	for i := 1; i < len(stations); i++ {
		for j, cand := range stations[i] {
			unary := sectionUnaryCost(cand, guides[i], false)
			bestCost := math.MaxFloat64
			bestPrev := -1
			for k, prev := range stations[i-1] {
				if dp[i-1][k] >= math.MaxFloat64*0.5 {
					continue
				}
				cost := dp[i-1][k] + unary + sectionTransitionCost(prev, cand, guides[i-1], guides[i])
				if cost < bestCost {
					bestCost = cost
					bestPrev = k
				}
			}
			dp[i][j] = bestCost
			prevIdx[i][j] = bestPrev
		}
	}
	last := len(stations) - 1
	bestLast := -1
	best := math.MaxFloat64
	for j := range stations[last] {
		if dp[last][j] < best {
			best = dp[last][j]
			bestLast = j
		}
	}
	if bestLast < 0 {
		return nil, false
	}
	path := make([]int, len(stations))
	cur := bestLast
	for i := last; i >= 0; i-- {
		if cur < 0 {
			return nil, false
		}
		path[i] = cur
		cur = prevIdx[i][cur]
	}
	return path, true
}

func sectionUnaryCost(cand sectionPairCandidate, guide [3]float32, first bool) float64 {
	cost := segmentLengthXZ(cand.center, guide) * 2.0
	if first {
		cost -= cand.width * 0.25
	}
	return cost
}

func sectionTransitionCost(prev, cur sectionPairCandidate, prevGuide, curGuide [3]float32) float64 {
	stepGuide := segmentLengthXZ(prevGuide, curGuide)
	stepCenter := segmentLengthXZ(prev.center, cur.center)
	leftMove := segmentLengthXZ(prev.left, cur.left)
	rightMove := segmentLengthXZ(prev.right, cur.right)
	widthDelta := math.Abs(cur.width - prev.width)
	cost := leftMove + rightMove + widthDelta*0.75 + math.Abs(stepCenter-stepGuide)*1.5
	if cur.width > prev.width*2.5 || cur.width < prev.width*0.4 {
		cost += 1000
	}
	if stepCenter > math.Max(stepGuide*2.5, 12.0) {
		cost += 1000
	}
	return cost
}

func collectSectionPairCandidates(outer [][3]float32, center [3]float32, dir [2]float64) []sectionPairCandidate {
	hits := make([]sampleHit, 0, 8)
	hitPts := make([][3]float32, 0, 8)
	base := [2]float64{float64(center[0]), float64(center[2])}
	for i := 0; i < len(outer); i++ {
		a := outer[i]
		b := outer[(i+1)%len(outer)]
		u, v, ok := infiniteLineSegmentIntersectionUV(base, dir, [2]float64{float64(a[0]), float64(a[2])}, [2]float64{float64(b[0]), float64(b[2])})
		if !ok {
			continue
		}
		hits = append(hits, sampleHit{u: u})
		hitPts = append(hitPts, interpolatePoint3(a, b, float32(v)))
	}
	if len(hits) < 2 {
		return nil
	}
	order := make([]int, len(hits))
	for i := range order {
		order[i] = i
	}
	sortSampleHitIndexesExperimental(order, hits)
	out := make([]sectionPairCandidate, 0, 16)
	for _, li := range order {
		if hits[li].u >= 0 {
			break
		}
		for rj := len(order) - 1; rj >= 0; rj-- {
			ri := order[rj]
			if hits[ri].u <= 0 {
				break
			}
			l := hitPts[li]
			r := hitPts[ri]
			width := segmentLengthXZ(l, r)
			if width < 0.5 {
				continue
			}
			out = append(out, sectionPairCandidate{
				left:  l,
				right: r,
				width: width,
				center: [3]float32{
					(l[0] + r[0]) * 0.5,
					(l[1] + r[1]) * 0.5,
					(l[2] + r[2]) * 0.5,
				},
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	sortSectionCandidatesByLocality(out, center)
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func sortSampleHitIndexesExperimental(order []int, hits []sampleHit) {
	for i := 1; i < len(order); i++ {
		v := order[i]
		j := i - 1
		for j >= 0 && hits[order[j]].u > hits[v].u {
			order[j+1] = order[j]
			j--
		}
		order[j+1] = v
	}
}

func sortSectionCandidatesByLocality(cands []sectionPairCandidate, guide [3]float32) {
	for i := 1; i < len(cands); i++ {
		v := cands[i]
		j := i - 1
		for j >= 0 && sectionCandidateRank(cands[j], guide) > sectionCandidateRank(v, guide) {
			cands[j+1] = cands[j]
			j--
		}
		cands[j+1] = v
	}
}

func sectionCandidateRank(cand sectionPairCandidate, guide [3]float32) float64 {
	return segmentLengthXZ(cand.center, guide)*2.0 + cand.width*0.25
}

func dedupeSectionTriplets(left, right, center [][3]float32, minCenterStep float64) ([][3]float32, [][3]float32, [][3]float32) {
	if len(left) == 0 || len(left) != len(right) || len(left) != len(center) {
		return nil, nil, nil
	}
	if minCenterStep <= 0 {
		minCenterStep = 0.5
	}
	outL := make([][3]float32, 0, len(left))
	outR := make([][3]float32, 0, len(right))
	outC := make([][3]float32, 0, len(center))
	outL = append(outL, left[0])
	outR = append(outR, right[0])
	outC = append(outC, center[0])
	for i := 1; i < len(center); i++ {
		if segmentLengthXZ(center[i], outC[len(outC)-1]) < minCenterStep {
			continue
		}
		outL = append(outL, left[i])
		outR = append(outR, right[i])
		outC = append(outC, center[i])
	}
	return outL, outR, outC
}

func samplePolylineBySpacing(points [][3]float32, spacing float64) [][3]float32 {
	if len(points) < 2 {
		return points
	}
	if spacing <= 0 {
		spacing = 3.0
	}
	cum := polylineCumLengths(points)
	total := cum[len(cum)-1]
	if total <= 1e-6 {
		return points
	}

	distances := make([]float64, 0, int(total/spacing)+2)
	distances = append(distances, 0)
	for d := spacing; d < total; d += spacing {
		distances = append(distances, d)
	}
	distances = append(distances, total)

	out := make([][3]float32, 0, len(distances))
	seg := 0
	for _, d := range distances {
		for seg < len(cum)-2 && cum[seg+1] < d {
			seg++
		}
		next := seg + 1
		den := cum[next] - cum[seg]
		if den <= 1e-6 {
			out = append(out, points[next])
			continue
		}
		t := float32((d - cum[seg]) / den)
		out = append(out, interpolatePoint3(points[seg], points[next], t))
	}
	return dedupeLinePoints(out)
}

func triangulateSectionQuads(left, right, center [][3]float32) ([][3]float32, []uint32, bool) {
	if len(left) < 2 || len(left) != len(right) || len(left) != len(center) {
		return nil, nil, false
	}
	pos := make([][3]float32, 0, len(left)*2)
	pos = append(pos, left...)
	rightBase := uint32(len(pos))
	pos = append(pos, right...)
	indices := make([]uint32, 0, (len(left)-1)*6)

	for i := 0; i < len(left)-1; i++ {
		l0 := uint32(i)
		l1 := uint32(i + 1)
		r0 := rightBase + uint32(i)
		r1 := rightBase + uint32(i+1)
		if chooseLeftRightDiagonal(pos[l0], pos[l1], pos[r1], pos[r0], center[i], center[i+1]) {
			appendTriangleUp(&indices, pos, l0, l1, r1)
			appendTriangleUp(&indices, pos, l0, r1, r0)
		} else {
			appendTriangleUp(&indices, pos, l0, l1, r0)
			appendTriangleUp(&indices, pos, l1, r1, r0)
		}
	}
	return pos, indices, len(indices) >= 3
}

func chooseLeftRightDiagonal(l0, l1, r1, r0, c0, c1 [3]float32) bool {
	scoreA := minTriangleAnglePairXZ(l0, l1, r1, r0)
	scoreB := minTriangleAnglePairXZ(l0, l1, r0, r1)
	if math.Abs(scoreA-scoreB) > 1e-4 {
		return scoreA >= scoreB
	}
	// Tie-breaker: prefer the diagonal that aligns better with section direction.
	segDX := float64(c1[0] - c0[0])
	segDZ := float64(c1[2] - c0[2])
	aDX := float64(r1[0] - l0[0])
	aDZ := float64(r1[2] - l0[2])
	bDX := float64(r0[0] - l1[0])
	bDZ := float64(r0[2] - l1[2])
	return math.Abs(segDX*aDX+segDZ*aDZ) <= math.Abs(segDX*bDX+segDZ*bDZ)
}

func appendTriangleUp(indices *[]uint32, pos [][3]float32, a, b, c uint32) {
	if a == b || b == c || a == c {
		return
	}
	n := triangleNormal(pos[a], pos[b], pos[c])
	if math.Abs(float64(n[0])) < 1e-6 && math.Abs(float64(n[1])) < 1e-6 && math.Abs(float64(n[2])) < 1e-6 {
		return
	}
	if n[1] < 0 {
		b, c = c, b
	}
	*indices = append(*indices, a, b, c)
}

func rebuildCenterlineFromBounds(left, right [][3]float32) (LocalLine, bool) {
	if len(left) < 2 || len(left) != len(right) {
		return LocalLine{}, false
	}
	points := make([][3]float32, 0, len(left))
	for i := range left {
		points = append(points, [3]float32{
			(left[i][0] + right[i][0]) * 0.5,
			(left[i][1] + right[i][1]) * 0.5,
			(left[i][2] + right[i][2]) * 0.5,
		})
	}
	points = dedupeLinePoints(points)
	if len(points) < 3 {
		return LocalLine{}, false
	}
	smoothLocalCenterline(points, 2, 2)
	return LocalLine{Points: points}, true
}

func smoothLocalCenterline(points [][3]float32, radius, passes int) {
	if len(points) < 3 || radius <= 0 || passes <= 0 {
		return
	}
	buf := make([][3]float32, len(points))
	for p := 0; p < passes; p++ {
		copy(buf, points)
		for i := 1; i < len(points)-1; i++ {
			start := i - radius
			if start < 0 {
				start = 0
			}
			end := i + radius
			if end >= len(points) {
				end = len(points) - 1
			}
			var sx, sy, sz float32
			var count float32
			for j := start; j <= end; j++ {
				sx += buf[j][0]
				sy += buf[j][1]
				sz += buf[j][2]
				count += 1
			}
			points[i][0] = sx / count
			points[i][1] = sy / count
			points[i][2] = sz / count
		}
	}
}

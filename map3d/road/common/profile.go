package common

import (
	"fmt"
	"math"
)

// ProfileFactory 璐熻矗瀹氫箟鍜岀敓鎴愮嚎鐘舵瀯绛戠墿鐨勬í鏂潰銆?//
// 绾跨姸楂樿捣鐗╅€氬父涓嶆槸绠€鍗曗€滃睍瀹芥垚闈⑩€濓紝鑰屾槸闇€瑕佷竴涓簩缁?profile
// 娌夸笁缁寸嚎杩涜鎵帬銆傚洜姝?profile 鏄繖绫诲璞＄殑鏍稿績杈撳叆涔嬩竴銆?//
// 渚嬪锛?// - 璺紭鐭虫湁鑷繁鐨勬柇闈紱
// - 鏂版辰瑗块槻鎶ゆ爮鏈夎嚜宸辩殑鏂潰锛?// - 鏅€氬浣撱€侀殧闊冲涔熸湁涓嶅悓鏂潰銆?
type ProfileFactory struct{}

type SweepMeshOptions struct {
	LateralSign float32
	RepeatU     float32
	CloseCaps   bool
	SkipEdges   map[int]bool
	HardEdges   bool
}

// NewProfileFactory 鍒涘缓妯柇闈㈠伐鍘傘€?
func NewProfileFactory() *ProfileFactory {
	return &ProfileFactory{}
}

// BuildRoadMarkingProfile 鏋勫缓閬撹矾鏍囩嚎浣跨敤鐨勭畝鍗曠煩褰㈡柇闈€?//
// 铏界劧鏍囩嚎閫氬父鍙渶瑕佸睍瀹芥垚闈紝浣嗕负浜嗙粺涓€鎺ュ彛锛屼篃鍙互瑙嗕綔涓€绉嶆瀬钖?profile銆?
func (f *ProfileFactory) BuildRoadMarkingProfile(width float32) (Profile2D, error) {
	return Profile2D{}, fmt.Errorf("build road marking profile: not implemented")
}

// BuildBarrierProfile 鏍规嵁鐢熸垚绫诲瀷鐢熸垚涓嶅悓鐨勬í鏂潰銆?//
// 鍚庣画瀹炵幇鏃讹紝buildType 搴旇繘涓€姝ョ粏鍒嗗埌 barrier 鐨勫叿浣撳瓙绫诲瀷锛?// 渚嬪鎶ゆ爮銆佸銆佸洿鏍忋€佹按椹€佽矾缂樼煶绛夈€?
func (f *ProfileFactory) BuildBarrierProfile(buildType BuildType, width, height float32) (Profile2D, error) {
	return Profile2D{}, fmt.Errorf("build barrier profile: not implemented")
}

// ExpandLineToSurface 灏嗕腑蹇冪嚎鎸夊搴︽墿灞曚负涓€涓潰銆?//
// 璇ユ柟娉曚富瑕佺敤浜庯細
// - 瀹炵嚎銆佽櫄绾裤€佽竟绾匡紱
// - 瀵兼祦甯﹀熀纭€闈紱
// - 涓績绾跨敓鎴愮獎鏉￠潰銆?
func (f *ProfileFactory) ExpandLineToSurface(line LocalLine, width float32) (LocalRings, error) {
	if width <= 0 {
		return LocalRings{}, fmt.Errorf("expand line to surface: width must be > 0")
	}

	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return LocalRings{}, fmt.Errorf("expand line to surface: line needs at least 2 distinct points")
	}

	half := width / 2
	left := make([][3]float32, len(points))
	right := make([][3]float32, len(points))
	segNormals := buildSegmentNormals(points)

	for i := range points {
		nx, nz := lineJoinNormal(points, segNormals, i)
		scale := half
		if i > 0 && i < len(points)-1 {
			prevNX, prevNZ := segNormals[i-1][0], segNormals[i-1][1]
			dot := float32(nx*prevNX + nz*prevNZ)
			if dot < 0.25 {
				dot = 0.25
			}
			scale = half / dot
		}
		if scale > half*4 {
			scale = half * 4
		}

		left[i] = [3]float32{
			points[i][0] + float32(nx)*scale,
			points[i][1],
			points[i][2] + float32(nz)*scale,
		}
		right[i] = [3]float32{
			points[i][0] - float32(nx)*scale,
			points[i][1],
			points[i][2] - float32(nz)*scale,
		}
	}

	ring := make([][3]float32, 0, len(left)+len(right)+1)
	ring = append(ring, left...)
	for i := len(right) - 1; i >= 0; i-- {
		ring = append(ring, right[i])
	}
	if len(ring) < 3 {
		return LocalRings{}, fmt.Errorf("expand line to surface: generated ring is too small")
	}
	ring = append(ring, ring[0])

	return LocalRings{Outer: ring}, nil
}

// SweepProfileAlongLine 灏嗕簩缁存í鏂潰娌夸笁缁寸嚎鎵帬鎴愪笁缁寸綉鏍肩偣闆嗐€?//
// 鍚庣画瀹炵幇鏃讹紝闇€瑕侀噸鐐瑰鐞嗭細
// - 鎶樼嚎鎷愮偣杩炴帴锛?// - 娉曞悜/鍒囧悜绋冲畾鎬э紱
// - 闂悎绾匡紱
// - UV 娌跨嚎杩炵画鎬с€?
func (f *ProfileFactory) SweepProfileAlongLine(profile Profile2D, line LocalLine) ([][3]float32, error) {
	return nil, fmt.Errorf("sweep profile along line: not implemented")
}

func (f *ProfileFactory) SweepProfileMesh(profile Profile2D, line LocalLine, opt SweepMeshOptions) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("sweep profile mesh: line needs at least 2 distinct points")
	}
	if len(profile.Points) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("sweep profile mesh: profile needs at least 2 points")
	}
	if opt.LateralSign == 0 {
		opt.LateralSign = 1
	}
	if opt.RepeatU <= 0 {
		opt.RepeatU = 1
	}

	closed := profile.Closed
	profilePts := append([][2]float32(nil), profile.Points...)
	if closed && profilePts[0] == profilePts[len(profilePts)-1] {
		profilePts = profilePts[:len(profilePts)-1]
	}
	if len(profilePts) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("sweep profile mesh: profile is too small")
	}

	segNormals := buildSegmentNormals(points)
	cum := polylineCumLengthsF32(points)
	vCoords := profilePathCoords(profilePts, closed)
	profileNormals := profileVertexNormals(profilePts, closed)
	frames := make([][][3]float32, len(points))
	frameNormals := make([][][3]float32, len(points))
	for i := range points {
		nx, nz := lineJoinNormal(points, segNormals, i)
		right := [2]float32{float32(nx) * opt.LateralSign, float32(nz) * opt.LateralSign}
		frame := make([][3]float32, len(profilePts))
		frameN := make([][3]float32, len(profilePts))
		for j, p := range profilePts {
			frame[j] = [3]float32{
				points[i][0] + right[0]*p[0],
				points[i][1] + p[1],
				points[i][2] + right[1]*p[0],
			}
			pn := profileNormals[j]
			fn := normalize3([3]float32{
				right[0] * pn[0],
				pn[1],
				right[1] * pn[0],
			})
			switch {
			case p[0] <= 1e-6:
				fn = normalize3([3]float32{-right[0], 0, -right[1]})
			case j > 0 && j < len(profilePts)-1 && p[1] >= profilePts[j-1][1] && p[1] >= profilePts[j+1][1]:
				fn = [3]float32{0, 1, 0}
			}
			frameN[j] = fn
		}
		frames[i] = frame
		frameNormals[i] = frameN
	}

	var pos [][3]float32
	var normals [][3]float32
	var uv [][2]float32
	var indices []uint32
	edgeCount := len(profilePts) - 1
	if closed {
		edgeCount = len(profilePts)
	}
	if !opt.HardEdges {
		pos = make([][3]float32, 0, len(frames)*len(profilePts))
		normals = make([][3]float32, 0, len(frames)*len(profilePts))
		uv = make([][2]float32, 0, len(frames)*len(profilePts))
		indices = make([]uint32, 0, (len(frames)-1)*edgeCount*6)
		for i := 0; i < len(frames); i++ {
			u := cum[i] / opt.RepeatU
			for j := 0; j < len(profilePts); j++ {
				pos = append(pos, frames[i][j])
				normals = append(normals, frameNormals[i][j])
				uv = append(uv, [2]float32{u, vCoords[j]})
			}
		}
		for i := 0; i < len(frames)-1; i++ {
			row0 := uint32(i * len(profilePts))
			row1 := uint32((i + 1) * len(profilePts))
			for j := 0; j < edgeCount; j++ {
				if opt.SkipEdges != nil && opt.SkipEdges[j] {
					continue
				}
				j2 := j + 1
				if closed {
					j2 = (j + 1) % len(profilePts)
				}
				a := row0 + uint32(j)
				b := row1 + uint32(j)
				c := row1 + uint32(j2)
				d := row0 + uint32(j2)
				indices = append(indices, a, b, c, a, c, d)
			}
		}
	} else {
		for i := 0; i < len(frames)-1; i++ {
			u0 := cum[i] / opt.RepeatU
			u1 := cum[i+1] / opt.RepeatU
			for j := 0; j < edgeCount; j++ {
				if opt.SkipEdges != nil && opt.SkipEdges[j] {
					continue
				}
				j2 := j + 1
				if closed {
					j2 = (j + 1) % len(profilePts)
				}
				a := frames[i][j]
				b := frames[i+1][j]
				c := frames[i+1][j2]
				d := frames[i][j2]
				if opt.HardEdges {
					appendQuadFlat(&pos, &normals, &uv, &indices,
						a, b, c, d,
						[2]float32{u0, vCoords[j]}, [2]float32{u1, vCoords[j]}, [2]float32{u1, vCoords[j2]}, [2]float32{u0, vCoords[j2]},
					)
				} else {
					normalA := frameNormals[i][j]
					normalB := frameNormals[i+1][j]
					normalC := frameNormals[i+1][j2]
					normalD := frameNormals[i][j2]
					appendQuadVertexNormals(&pos, &normals, &uv, &indices,
						a, b, c, d,
						normalA, normalB, normalC, normalD,
						[2]float32{u0, vCoords[j]}, [2]float32{u1, vCoords[j]}, [2]float32{u1, vCoords[j2]}, [2]float32{u0, vCoords[j2]},
					)
				}
			}
		}
	}

	if opt.CloseCaps && len(profilePts) >= 3 {
		appendProfileCap(&pos, &normals, &uv, &indices, frames[0], true, opt.SkipEdges)
		appendProfileCap(&pos, &normals, &uv, &indices, frames[len(frames)-1], false, opt.SkipEdges)
	}

	return pos, normals, uv, indices, nil
}

func profilePathCoords(profile [][2]float32, closed bool) []float32 {
	out := make([]float32, len(profile))
	for i := 1; i < len(profile); i++ {
		dx := profile[i][0] - profile[i-1][0]
		dy := profile[i][1] - profile[i-1][1]
		out[i] = out[i-1] + float32(math.Hypot(float64(dx), float64(dy)))
	}
	if closed && len(profile) > 1 {
		dx := profile[0][0] - profile[len(profile)-1][0]
		dy := profile[0][1] - profile[len(profile)-1][1]
		out = append(out, out[len(out)-1]+float32(math.Hypot(float64(dx), float64(dy))))
		return out[:len(profile)]
	}
	return out
}

func profileVertexNormals(profile [][2]float32, closed bool) [][2]float32 {
	out := make([][2]float32, len(profile))
	edgeNormals := make([][2]float32, 0, len(profile))
	edgeCount := len(profile) - 1
	if closed {
		edgeCount = len(profile)
	}
	for i := 0; i < edgeCount; i++ {
		j := i + 1
		if closed {
			j = (i + 1) % len(profile)
		}
		dx := profile[j][0] - profile[i][0]
		dy := profile[j][1] - profile[i][1]
		nx, ny := normalize2(float64(-dy), float64(dx))
		edgeNormals = append(edgeNormals, [2]float32{float32(nx), float32(ny)})
	}
	for i := range profile {
		if closed {
			prev := edgeNormals[(i-1+len(edgeNormals))%len(edgeNormals)]
			next := edgeNormals[i%len(edgeNormals)]
			out[i] = normalize2f(prev[0]+next[0], prev[1]+next[1])
			continue
		}
		switch {
		case i == 0:
			out[i] = edgeNormals[0]
		case i == len(profile)-1:
			out[i] = edgeNormals[len(edgeNormals)-1]
		default:
			prev := edgeNormals[i-1]
			next := edgeNormals[i]
			out[i] = normalize2f(prev[0]+next[0], prev[1]+next[1])
		}
	}
	return out
}

func appendProfileCap(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, frame [][3]float32, reverse bool, skipEdges map[int]bool) {
	if len(frame) < 3 {
		return
	}
	capPts := profileCapHull(frame, skipEdges)
	if len(capPts) < 3 {
		return
	}
	base := uint32(len(*pos))
	var center [3]float32
	for _, p := range capPts {
		center[0] += p[0]
		center[1] += p[1]
		center[2] += p[2]
	}
	inv := 1 / float32(len(capPts))
	center[0] *= inv
	center[1] *= inv
	center[2] *= inv
	normal := triangleNormal(capPts[0], capPts[1], capPts[2])
	if reverse {
		normal = [3]float32{-normal[0], -normal[1], -normal[2]}
	}
	normal = normalize3(normal)
	*pos = append(*pos, center)
	*normals = append(*normals, normal)
	*uv = append(*uv, [2]float32{0.5, 0.5})
	for _, p := range capPts {
		*pos = append(*pos, p)
		*normals = append(*normals, normal)
		*uv = append(*uv, [2]float32{0, 0})
	}
	for i := 0; i < len(capPts); i++ {
		j := (i + 1) % len(capPts)
		if reverse {
			*indices = append(*indices, base, base+uint32(j+1), base+uint32(i+1))
		} else {
			*indices = append(*indices, base, base+uint32(i+1), base+uint32(j+1))
		}
	}
}

func profileCapHull(frame [][3]float32, skipEdges map[int]bool) [][3]float32 {
	if len(frame) == 0 {
		return nil
	}
	keep := make([][3]float32, 0, len(frame))
	for i := 0; i < len(frame); i++ {
		if skipEdges != nil {
			if _, ok := skipEdges[i]; ok {
				continue
			}
		}
		keep = append(keep, frame[i])
	}
	return keep
}

func appendQuadVertexNormals(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, a, b, c, d [3]float32, na, nb, nc, nd [3]float32, ua, ub, uc, ud [2]float32) {
	avg := normalize3([3]float32{
		na[0] + nb[0] + nc[0] + nd[0],
		na[1] + nb[1] + nc[1] + nd[1],
		na[2] + nb[2] + nc[2] + nd[2],
	})
	if dot3(triangleNormal(a, b, c), avg) < 0 {
		b, d = d, b
		nb, nd = nd, nb
		ub, ud = ud, ub
	}
	base := uint32(len(*pos))
	*pos = append(*pos, a, b, c, d)
	*normals = append(*normals, na, nb, nc, nd)
	*uv = append(*uv, ua, ub, uc, ud)
	*indices = append(*indices,
		base, base+1, base+2,
		base, base+2, base+3,
	)
}

func appendQuadFlat(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, a, b, c, d [3]float32, ua, ub, uc, ud [2]float32) {
	n := normalize3(triangleNormal(a, b, c))
	if dot3(n, triangleNormal(a, c, d)) < 0 {
		b, d = d, b
		ub, ud = ud, ub
		n = normalize3(triangleNormal(a, b, c))
	}
	appendQuad(pos, normals, uv, indices, a, b, c, d, n, ua, ub, uc, ud)
}

func dedupeLinePoints(points [][3]float32) [][3]float32 {
	if len(points) == 0 {
		return nil
	}
	out := make([][3]float32, 0, len(points))
	out = append(out, points[0])
	for i := 1; i < len(points); i++ {
		if points[i] == points[i-1] {
			continue
		}
		out = append(out, points[i])
	}
	return out
}

func buildSegmentNormals(points [][3]float32) [][2]float64 {
	normals := make([][2]float64, 0, len(points)-1)
	for i := 0; i < len(points)-1; i++ {
		dx := float64(points[i+1][0] - points[i][0])
		dz := float64(points[i+1][2] - points[i][2])
		length := math.Hypot(dx, dz)
		if length == 0 {
			normals = append(normals, [2]float64{0, 0})
			continue
		}
		normals = append(normals, [2]float64{-dz / length, dx / length})
	}
	return normals
}

func lineJoinNormal(points [][3]float32, segNormals [][2]float64, idx int) (float64, float64) {
	switch {
	case idx <= 0:
		return normalize2(segNormals[0][0], segNormals[0][1])
	case idx >= len(points)-1:
		return normalize2(segNormals[len(segNormals)-1][0], segNormals[len(segNormals)-1][1])
	default:
		nx := segNormals[idx-1][0] + segNormals[idx][0]
		nz := segNormals[idx-1][1] + segNormals[idx][1]
		if nx == 0 && nz == 0 {
			return normalize2(segNormals[idx][0], segNormals[idx][1])
		}
		return normalize2(nx, nz)
	}
}

func normalize2(x, z float64) (float64, float64) {
	length := math.Hypot(x, z)
	if length == 0 {
		return 0, 0
	}
	return x / length, z / length
}

func normalize2f(x, y float32) [2]float32 {
	l := float32(math.Hypot(float64(x), float64(y)))
	if l == 0 {
		return [2]float32{0, 0}
	}
	return [2]float32{x / l, y / l}
}

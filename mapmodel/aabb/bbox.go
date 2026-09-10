package aabb

import (
	"fmt"
	"math"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

type Box struct {
	Min [3]float64
	Max [3]float64
}

func NewEmptyBox() Box {
	return Box{
		Min: [3]float64{math.MaxFloat64, math.MaxFloat64, math.MaxFloat64},
		Max: [3]float64{-math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64},
	}
}
func (b *Box) Expand(p [3]float64) {
	for i := 0; i < 3; i++ {
		if p[i] < b.Min[i] {
			b.Min[i] = p[i]
		}
		if p[i] > b.Max[i] {
			b.Max[i] = p[i]
		}
	}
}
func (b *Box) Valid() bool {
	return b.Min[0] <= b.Max[0] && b.Min[1] <= b.Max[1] && b.Min[2] <= b.Max[2]
}
func (b *Box) String() string { return fmt.Sprintf("Min=%v Max=%v", b.Min, b.Max) }

// 列主序 4x4 * 点
func mulPoint(m [16]float64, p [3]float64) [3]float64 {
	x := m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12]
	y := m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13]
	z := m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14]
	return [3]float64{x, y, z}
}

func localMatrix(nd *gltf.Node) [16]float64 {
	if nd == nil {
		return gltf.DefaultMatrix
	}
	// 若 node.Matrix 非默认，直接用它
	if nd.Matrix != gltf.DefaultMatrix {
		return nd.Matrix
	}
	// 用 TRS 组装
	T := nd.Translation
	R := nd.Rotation
	S := nd.Scale
	if T == gltf.DefaultTranslation {
		T = [3]float64{0, 0, 0}
	}
	if R == gltf.DefaultRotation {
		R = [4]float64{0, 0, 0, 1}
	}
	if S == gltf.DefaultScale {
		S = [3]float64{1, 1, 1}
	}
	return composeTRS64(T, R, S)
}

//
//func composeTRS64(T [3]float64, Q [4]float64, S [3]float64) [16]float64 {
//	x, y, z, w := Q[0], Q[1], Q[2], Q[3]
//	xx, yy, zz := x*x, y*y, z*z
//	xy, xz, yz := x*y, x*z, y*z
//	wx, wy, wz := w*x, w*y, w*z
//
//	// 旋转矩阵（无缩放），列主序
//	r00 := 1 - 2*(yy+zz)
//	r01 := 2 * (xy + wz)
//	r02 := 2 * (xz - wy)
//	r10 := 2 * (xy - wz)
//	r11 := 1 - 2*(xx+zz)
//	r12 := 2 * (yz + wx)
//	r20 := 2 * (xz + wy)
//	r21 := 2 * (yz - wx)
//	r22 := 1 - 2*(xx+yy)
//
//	// 列缩放：整列分别乘以 Sx / Sy / Sz
//	sx, sy, sz := S[0], S[1], S[2]
//	return [16]float64{
//		r00 * sx, r10 * sx, r20 * sx, 0,
//		r01 * sy, r11 * sy, r21 * sy, 0,
//		r02 * sz, r12 * sz, r22 * sz, 0,
//		T[0], T[1], T[2], 1,
//	}
//}

// 列主序 4x4 相乘
func mul4x4(a, b [16]float64) [16]float64 {
	var r [16]float64
	for c := 0; c < 4; c++ {
		for r0 := 0; r0 < 4; r0++ {
			r[c*4+r0] = a[0*4+r0]*b[c*4+0] + a[1*4+r0]*b[c*4+1] + a[2*4+r0]*b[c*4+2] + a[3*4+r0]*b[c*4+3]
		}
	}
	return r
}

// 计算每个节点的世界矩阵
func worldMatrices(doc *gltf.Document) [][16]float64 {
	n := len(doc.Nodes)
	world := make([][16]float64, n)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = -1
	}
	for pi, p := range doc.Nodes {
		if p == nil {
			continue
		}
		for _, c := range p.Children {
			parent[c] = pi
		}
	}
	vis := make([]bool, n)
	var dfs func(i int) [16]float64
	dfs = func(i int) [16]float64 {
		if vis[i] {
			return world[i]
		}
		l := localMatrix(doc.Nodes[i])
		if parent[i] >= 0 {
			world[i] = mul4x4(dfs(parent[i]), l)
		} else {
			world[i] = l
		}
		vis[i] = true
		return world[i]
	}
	for i := range doc.Nodes {
		if doc.Nodes[i] != nil {
			dfs(i)
		}
	}
	return world
}

// 计算 Primitive 的 AABB（应用某个 4x4 矩阵，传世界或局部都行）
func PrimitiveBox(doc *gltf.Document, pr *gltf.Primitive, mat [16]float64) (Box, error) {
	b := NewEmptyBox()
	posIdx, ok := pr.Attributes[gltf.POSITION]
	if !ok {
		return b, fmt.Errorf("primitive has no POSITION")
	}
	ac := doc.Accessors[posIdx]
	if ac == nil {
		return b, fmt.Errorf("accessor POSITION nil")
	}

	// 1) 快路径：直接用 Min/Max
	if ac.Min != nil && ac.Max != nil && len(ac.Min) >= 3 && len(ac.Max) >= 3 {
		local := Box{
			Min: [3]float64{ac.Min[0], ac.Min[1], ac.Min[2]},
			Max: [3]float64{ac.Max[0], ac.Max[1], ac.Max[2]},
		}
		// 变换 8 角点
		min, max := local.Min, local.Max
		corners := [][3]float64{
			{min[0], min[1], min[2]}, {max[0], min[1], min[2]},
			{min[0], max[1], min[2]}, {max[0], max[1], min[2]},
			{min[0], min[1], max[2]}, {max[0], min[1], max[2]},
			{min[0], max[1], max[2]}, {max[0], max[1], max[2]},
		}
		for _, c := range corners {
			wp := mulPoint(mat, c)
			b.Expand(wp)
		}
		return b, nil
	}

	// 2) 慢路径：逐点读取
	positions, err := modeler.ReadPosition(doc, ac, nil)
	if err != nil {
		return b, fmt.Errorf("read positions: %w", err)
	}
	for _, v := range positions {
		p := [3]float64{float64(v[0]), float64(v[1]), float64(v[2])}
		wp := mulPoint(mat, p)
		b.Expand(wp)
	}
	return b, nil
}

// 计算 Mesh 的 AABB（聚合其所有 primitive）
func MeshBox(doc *gltf.Document, mesh *gltf.Mesh, mat [16]float64) (Box, error) {
	b := NewEmptyBox()
	for _, pr := range mesh.Primitives {
		if pr == nil {
			continue
		}
		pb, err := PrimitiveBox(doc, pr, mat)
		if err != nil {
			return b, err
		}
		if pb.Valid() {
			b.Expand(pb.Min)
			b.Expand(pb.Max)
		}
	}
	return b, nil
}

func NodeBoxW(doc *gltf.Document, nodeIndex int, wm [][16]float64) (Box, error) {
	world := wm[nodeIndex]
	b := NewEmptyBox()

	nd := doc.Nodes[nodeIndex]
	if nd.Mesh != nil {
		mi := *nd.Mesh
		if mi >= 0 && mi < len(doc.Meshes) && doc.Meshes[mi] != nil {
			mb, err := MeshBox(doc, doc.Meshes[mi], world)
			if err != nil {
				return b, err
			}
			if mb.Valid() {
				b.Expand(mb.Min)
				b.Expand(mb.Max)
			}
		}
	}
	for _, c := range nd.Children {
		cb, err := NodeBoxW(doc, c, wm) // 直接把同一个 wm 传下去
		if err != nil {
			return b, err
		}
		if cb.Valid() {
			b.Expand(cb.Min)
			b.Expand(cb.Max)
		}
	}
	return b, nil
}

// 计算“整体模型”的 AABB（遍历 scene[0] 的顶层节点）
// 修改：SceneBox 里只算一次 worldMatrices，然后传给 NodeBoxW
func SceneBox(doc *gltf.Document, sceneIdx int) (Box, [][16]float64, error) {
	if sceneIdx < 0 || sceneIdx >= len(doc.Scenes) || doc.Scenes[sceneIdx] == nil {
		return NewEmptyBox(), nil, fmt.Errorf("invalid scene index")
	}
	wm := worldMatrices(doc) // 只算这一次
	b := NewEmptyBox()
	for _, ni := range doc.Scenes[sceneIdx].Nodes {
		nb, err := NodeBoxW(doc, ni, wm)
		if err != nil {
			return b, nil, err
		}
		if nb.Valid() {
			b.Expand(nb.Min)
			b.Expand(nb.Max)
		}
	}
	return b, wm, nil
}

package cdt

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"unsafe"
)

const epsilon = 1e-9

// Point2 表示二维点。
type Point2 struct {
	X float64
	Y float64
}

// Edge 表示点索引之间的一条约束边。
type Edge struct {
	A int32
	B int32
}

// Triangle 表示三角形顶点索引。
type Triangle struct {
	A int32
	B int32
	C int32
}

type nativeResult struct {
	Points        *Point2
	PointCount    int32
	Triangles     *Triangle
	TriangleCount int32
	ErrorMessage  *byte
}

type triangulationMode int32

const (
	modeKeepInterior triangulationMode = iota
	modeKeepInteriorAndHoles
	modeConvexHull
)

type api struct {
	triangulate func(points *Point2, pointCount int32, edges *Edge, edgeCount int32, mode int32, outResult *nativeResult) int32
	freeResult  func(result *nativeResult)
	handle      uintptr
}

var globalAPI *api

// CleanResult 是输入预处理结果。
type CleanResult struct {
	Points []Point2
	Edges  []Edge
}

// libraryPath 根据操作系统选择动态库。
func libraryPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return filepath.FromSlash("bin/cdt_bridge.dll"), nil
	case "linux":
		return filepath.FromSlash("bin/cdt_bridge.so"), nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func cString(ptr *byte) string {
	if ptr == nil {
		return ""
	}
	buf := make([]byte, 0, 128)
	for p := uintptr(unsafe.Pointer(ptr)); ; p++ {
		b := *(*byte)(unsafe.Pointer(p))
		if b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}

func sliceFromPtr[T any](ptr *T, n int32) []T {
	if ptr == nil || n <= 0 {
		return nil
	}
	return unsafe.Slice(ptr, n)
}

// TriangulateInside 对封闭边界内部做三角剖分。
// 约束边外部会被删除，适合单个封闭 patch。
func TriangulateInside(points []Point2, edges []Edge) ([]Point2, []Triangle, error) {
	return triangulateWithMode(points, edges, modeKeepInterior)
}

// TriangulateWithHoles 对包含孔洞的约束区域做三角剖分。
// 外部区域和孔洞区域都会被删除。
func TriangulateWithHoles(points []Point2, edges []Edge) ([]Point2, []Triangle, error) {
	return triangulateWithMode(points, edges, modeKeepInteriorAndHoles)
}

// ConvexHullTriangulate 仅基于点集做凸包范围内的三角剖分。
// 适合调试点集，不依赖约束边。
func ConvexHullTriangulate(points []Point2) ([]Point2, []Triangle, error) {
	cleanPoints, err := deduplicatePoints(points)
	if err != nil {
		return nil, nil, err
	}
	if len(cleanPoints) < 3 {
		return nil, nil, fmt.Errorf("need at least 3 unique points")
	}
	return triangulateNative(cleanPoints, nil, modeConvexHull)
}

// CleanInput 对输入做预处理：去重点、重映射边、移除重复边、检查非法相交。
func CleanInput(points []Point2, edges []Edge) (CleanResult, error) {
	cleanPoints, remap, err := deduplicatePointsWithRemap(points)
	if err != nil {
		return CleanResult{}, err
	}

	remappedEdges, err := remapAndNormalizeEdges(edges, remap, len(cleanPoints))
	if err != nil {
		return CleanResult{}, err
	}

	if err := validateEdges(cleanPoints, remappedEdges); err != nil {
		return CleanResult{}, err
	}

	return CleanResult{
		Points: cleanPoints,
		Edges:  remappedEdges,
	}, nil
}

func triangulateWithMode(points []Point2, edges []Edge, mode triangulationMode) ([]Point2, []Triangle, error) {
	if mode == modeConvexHull {
		return nil, nil, fmt.Errorf("use ConvexHullTriangulate for convex hull mode")
	}

	clean, err := CleanInput(points, edges)
	if err != nil {
		return nil, nil, err
	}

	if len(clean.Points) < 3 {
		return nil, nil, fmt.Errorf("need at least 3 unique points")
	}
	if len(clean.Edges) == 0 {
		return nil, nil, fmt.Errorf("edges is empty after cleaning")
	}

	return triangulateNative(clean.Points, clean.Edges, mode)
}

func triangulateNative(points []Point2, edges []Edge, mode triangulationMode) ([]Point2, []Triangle, error) {
	a, err := load()
	if err != nil {
		return nil, nil, err
	}

	var pointPtr *Point2
	if len(points) > 0 {
		pointPtr = &points[0]
	}

	var edgePtr *Edge
	if len(edges) > 0 {
		edgePtr = &edges[0]
	}

	var res nativeResult
	rc := a.triangulate(pointPtr, int32(len(points)), edgePtr, int32(len(edges)), int32(mode), &res)
	defer a.freeResult(&res)

	if rc != 0 {
		if res.ErrorMessage != nil {
			return nil, nil, fmt.Errorf("native error: %s", cString(res.ErrorMessage))
		}
		return nil, nil, fmt.Errorf("native error: triangulation failed")
	}

	outPoints := append([]Point2(nil), sliceFromPtr(res.Points, res.PointCount)...)
	outTriangles := append([]Triangle(nil), sliceFromPtr(res.Triangles, res.TriangleCount)...)
	return outPoints, outTriangles, nil
}

// deduplicatePoints 对点去重。
func deduplicatePoints(points []Point2) ([]Point2, error) {
	clean, _, err := deduplicatePointsWithRemap(points)
	return clean, err
}

// deduplicatePointsWithRemap 去重并返回旧索引到新索引的映射。
func deduplicatePointsWithRemap(points []Point2) ([]Point2, []int32, error) {
	if len(points) == 0 {
		return nil, nil, fmt.Errorf("points is empty")
	}

	type key struct {
		X int64
		Y int64
	}

	quantize := func(v float64) int64 {
		return int64(math.Round(v / epsilon))
	}

	seen := make(map[key]int32, len(points))
	clean := make([]Point2, 0, len(points))
	remap := make([]int32, len(points))

	for i, p := range points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return nil, nil, fmt.Errorf("invalid point at index %d", i)
		}

		k := key{X: quantize(p.X), Y: quantize(p.Y)}
		if idx, ok := seen[k]; ok {
			remap[i] = idx
			continue
		}

		newIdx := int32(len(clean))
		seen[k] = newIdx
		remap[i] = newIdx
		clean = append(clean, p)
	}

	return clean, remap, nil
}

// remapAndNormalizeEdges 重映射边索引，移除零长度边和重复边。
func remapAndNormalizeEdges(edges []Edge, remap []int32, pointCount int) ([]Edge, error) {
	if len(edges) == 0 {
		return nil, fmt.Errorf("edges is empty")
	}

	type edgeKey struct {
		A int32
		B int32
	}

	seen := make(map[edgeKey]struct{}, len(edges))
	out := make([]Edge, 0, len(edges))

	for i, e := range edges {
		if e.A < 0 || e.B < 0 || int(e.A) >= len(remap) || int(e.B) >= len(remap) {
			return nil, fmt.Errorf("edge index out of range at edge %d", i)
		}

		a := remap[e.A]
		b := remap[e.B]

		if a < 0 || b < 0 || int(a) >= pointCount || int(b) >= pointCount {
			return nil, fmt.Errorf("remapped edge index out of range at edge %d", i)
		}
		if a == b {
			continue
		}

		if a > b {
			a, b = b, a
		}

		k := edgeKey{A: a, B: b}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, Edge{A: a, B: b})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("all edges were removed during cleaning")
	}

	return out, nil
}

// validateEdges 检查边是否越界、是否存在非法相交。
func validateEdges(points []Point2, edges []Edge) error {
	for i, e := range edges {
		if e.A < 0 || e.B < 0 || int(e.A) >= len(points) || int(e.B) >= len(points) {
			return fmt.Errorf("edge index out of range at edge %d", i)
		}
		if e.A == e.B {
			return fmt.Errorf("zero-length edge at edge %d", i)
		}
	}

	for i := 0; i < len(edges); i++ {
		for j := i + 1; j < len(edges); j++ {
			e1 := edges[i]
			e2 := edges[j]

			if shareEndpoint(e1, e2) {
				continue
			}

			p1 := points[e1.A]
			q1 := points[e1.B]
			p2 := points[e2.A]
			q2 := points[e2.B]

			if segmentsIntersectStrict(p1, q1, p2, q2) {
				return fmt.Errorf("edges %d and %d intersect", i, j)
			}
		}
	}

	return nil
}

func shareEndpoint(a, b Edge) bool {
	return a.A == b.A || a.A == b.B || a.B == b.A || a.B == b.B
}

func segmentsIntersectStrict(p1, q1, p2, q2 Point2) bool {
	o1 := orientation(p1, q1, p2)
	o2 := orientation(p1, q1, q2)
	o3 := orientation(p2, q2, p1)
	o4 := orientation(p2, q2, q1)

	if o1 == 0 && onSegment(p1, p2, q1) {
		return true
	}
	if o2 == 0 && onSegment(p1, q2, q1) {
		return true
	}
	if o3 == 0 && onSegment(p2, p1, q2) {
		return true
	}
	if o4 == 0 && onSegment(p2, q1, q2) {
		return true
	}

	return (o1 > 0) != (o2 > 0) && (o3 > 0) != (o4 > 0)
}

func orientation(a, b, c Point2) int {
	v := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
	switch {
	case math.Abs(v) <= epsilon:
		return 0
	case v > 0:
		return 1
	default:
		return -1
	}
}

func onSegment(a, b, c Point2) bool {
	return b.X <= math.Max(a.X, c.X)+epsilon &&
		b.X >= math.Min(a.X, c.X)-epsilon &&
		b.Y <= math.Max(a.Y, c.Y)+epsilon &&
		b.Y >= math.Min(a.Y, c.Y)-epsilon
}

// BoundaryEdges 从三角形集合里提取边界边。
// 边界边是在所有三角形中只出现一次的边。
func BoundaryEdges(tris []Triangle) []Edge {
	type edgeKey struct {
		A int32
		B int32
	}

	counts := make(map[edgeKey]int, len(tris)*3)

	add := func(a, b int32) {
		if a > b {
			a, b = b, a
		}
		counts[edgeKey{A: a, B: b}]++
	}

	for _, t := range tris {
		add(t.A, t.B)
		add(t.B, t.C)
		add(t.C, t.A)
	}

	out := make([]Edge, 0)
	for k, n := range counts {
		if n == 1 {
			out = append(out, Edge{A: k.A, B: k.B})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})

	return out
}

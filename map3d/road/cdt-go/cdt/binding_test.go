// File: cdt/binding_test.go
package cdt

import "testing"

func TestTriangulateInside_Square(t *testing.T) {
	points := []Point2{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
	}

	edges := []Edge{
		{A: 0, B: 1},
		{A: 1, B: 2},
		{A: 2, B: 3},
		{A: 3, B: 0},
	}

	outPoints, outTriangles, err := TriangulateInside(points, edges)
	if err != nil {
		t.Fatalf("TriangulateInside failed: %v", err)
	}

	if len(outPoints) < 4 {
		t.Fatalf("expected at least 4 output points, got %d", len(outPoints))
	}

	if len(outTriangles) == 0 {
		t.Fatalf("expected non-empty triangles")
	}

	if len(outTriangles) != 2 {
		t.Fatalf("expected 2 triangles for a square, got %d", len(outTriangles))
	}
}

func TestCleanInput_DeduplicatePointsAndEdges(t *testing.T) {
	points := []Point2{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
		{X: 0, Y: 0},
		{X: 10, Y: 0},
	}

	edges := []Edge{
		{A: 0, B: 1},
		{A: 1, B: 2},
		{A: 2, B: 3},
		{A: 3, B: 4},
		{A: 4, B: 5},
		{A: 0, B: 1},
	}

	clean, err := CleanInput(points, edges)
	if err != nil {
		t.Fatalf("CleanInput failed: %v", err)
	}

	if len(clean.Points) != 4 {
		t.Fatalf("expected 4 unique points, got %d", len(clean.Points))
	}

	if len(clean.Edges) == 0 {
		t.Fatalf("expected non-empty edges after cleaning")
	}
}

func TestCleanInput_IntersectingEdges(t *testing.T) {
	points := []Point2{
		{X: 0, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
		{X: 10, Y: 0},
	}

	edges := []Edge{
		{A: 0, B: 1},
		{A: 2, B: 3},
	}

	_, err := CleanInput(points, edges)
	if err == nil {
		t.Fatalf("expected intersection error, got nil")
	}
}

func TestConvexHullTriangulate_Rectangle(t *testing.T) {
	points := []Point2{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
	}

	outPoints, outTriangles, err := ConvexHullTriangulate(points)
	if err != nil {
		t.Fatalf("ConvexHullTriangulate failed: %v", err)
	}

	if len(outPoints) < 4 {
		t.Fatalf("expected at least 4 output points, got %d", len(outPoints))
	}

	if len(outTriangles) == 0 {
		t.Fatalf("expected non-empty triangles")
	}
}

func TestBoundaryEdges_FromTwoTriangles(t *testing.T) {
	tris := []Triangle{
		{A: 0, B: 1, C: 2},
		{A: 0, B: 2, C: 3},
	}

	boundary := BoundaryEdges(tris)
	if len(boundary) != 4 {
		t.Fatalf("expected 4 boundary edges, got %d", len(boundary))
	}
}

func TestTriangulateWithHoles_OuterAndInnerSquare(t *testing.T) {
	points := []Point2{
		// outer ring
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
		// inner ring
		{X: 3, Y: 3},
		{X: 7, Y: 3},
		{X: 7, Y: 7},
		{X: 3, Y: 7},
	}

	edges := []Edge{
		// outer ring
		{A: 0, B: 1},
		{A: 1, B: 2},
		{A: 2, B: 3},
		{A: 3, B: 0},
		// inner ring
		{A: 4, B: 5},
		{A: 5, B: 6},
		{A: 6, B: 7},
		{A: 7, B: 4},
	}

	outPoints, outTriangles, err := TriangulateWithHoles(points, edges)
	if err != nil {
		t.Fatalf("TriangulateWithHoles failed: %v", err)
	}

	if len(outPoints) < 8 {
		t.Fatalf("expected at least 8 output points, got %d", len(outPoints))
	}

	if len(outTriangles) == 0 {
		t.Fatalf("expected non-empty triangles")
	}
}

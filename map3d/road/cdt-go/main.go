package main

import (
	"fmt"
	"log"

	"cdt-go/cdt"
)

func main() {
	defer func() {
		if err := cdt.Close(); err != nil {
			log.Printf("close native library failed: %v", err)
		}
	}()

	points := []cdt.Point2{
		{X: 0, Y: 0},
		{X: 10, Y: 0},
		{X: 10, Y: 10},
		{X: 0, Y: 10},
	}

	edges := []cdt.Edge{
		{A: 0, B: 1},
		{A: 1, B: 2},
		{A: 2, B: 3},
		{A: 3, B: 0},
	}

	outPoints, outTriangles, err := cdt.TriangulateInside(points, edges)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("points:")
	for i, p := range outPoints {
		fmt.Printf("  %d: (%.3f, %.3f)\n", i, p.X, p.Y)
	}

	fmt.Println("triangles:")
	for i, t := range outTriangles {
		fmt.Printf("  %d: [%d %d %d]\n", i, t.A, t.B, t.C)
	}
}

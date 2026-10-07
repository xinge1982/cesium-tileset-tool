package mgltf

import (
	"testing"

	"github.com/qmuntal/gltf"
)

func TestExtrudePolygonSolidPrimitives_Square(t *testing.T) {
	doc := gltf.NewDocument()
	ring := [][3]float32{
		{0, 0, 0},
		{10, 0, 0},
		{10, 0, 5},
		{0, 0, 5},
		{0, 0, 0}, // closed
	}
	prs, err := ExtrudePolygonSolidPrimitives(doc, ring, 0.2, 0, UvsParams{RepeatX: 1, RepeatY: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 {
		t.Fatalf("expected 3 primitives, got %d", len(prs))
	}
	if len(doc.Accessors) == 0 {
		t.Fatalf("expected accessors to be written")
	}
}

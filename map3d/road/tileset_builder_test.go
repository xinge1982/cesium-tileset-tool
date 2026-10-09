package road

import (
	"math"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func TestTilesetBoxContainsCesiumTransformedGLB(t *testing.T) {
	for _, z := range [][2]float32{{-280, -243}, {22, 356}, {-356, 22}} {
		doc := gltf.NewDocument()
		positions := [][3]float32{{-537, 12, z[0]}, {298, 19, z[1]}}
		doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{{
			Attributes: gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)},
		}}}}
		box := buildTilesetBox([3]float64{-537, 12, float64(z[0])}, [3]float64{298, 19, float64(z[1])}, TilesetBuildOptions{})
		assertRoadGLBInsideTileBox(t, doc, box)
	}
}

// These generated tile boxes are axis-aligned in the shared local ENU frame.
// The root ECEF transform applies equally to the box and the rotated content.
func assertRoadGLBInsideTileBox(t *testing.T, doc *gltf.Document, box []float64) {
	t.Helper()
	if len(box) != 12 {
		t.Fatalf("expected a tile box, got %v", box)
	}
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			positions, err := modeler.ReadPosition(doc, doc.Accessors[primitive.Attributes[gltf.POSITION]], nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range positions {
				// Standard glTF Y-up -> 3D Tiles Z-up rotation about X by +90 degrees.
				local := [3]float64{float64(p[0]), -float64(p[2]), float64(p[1])}
				for axis, value := range local {
					if math.Abs(value-box[axis]) > box[3+4*axis]+0.005 {
						t.Fatalf("Cesium-transformed vertex %v outside tile box %v", local, box)
					}
				}
			}
		}
	}
}

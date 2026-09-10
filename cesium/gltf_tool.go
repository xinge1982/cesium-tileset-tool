package cesium

import (
	"bytes"
	"fmt"
	"math"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

type Dimension struct {
	Min    Vec3
	Max    Vec3
	Width  float64
	Height float64
	Depth  float64
}

func CalculateDimensions(data []byte) (*Dimension, error) {
	doc := new(gltf.Document)

	decoder := gltf.NewDecoder(bytes.NewReader(data))
	if errD := decoder.Decode(doc); errD != nil {
		return nil, fmt.Errorf("Failed to decode glb: %s", errD.Error())
	}

	return calculateDocumentDimensions(doc)
}

// CalculateDimensions loads a .gltf or .glb file
// and returns the model dimensions.
//
// NOTE:
// This version supports Translation + Scale transforms.
// Rotation and Matrix transforms are ignored for simplicity.
func calculateDocumentDimensions(doc *gltf.Document) (*Dimension, error) {
	min := Vec3{
		X: math.Inf(1),
		Y: math.Inf(1),
		Z: math.Inf(1),
	}

	max := Vec3{
		X: math.Inf(-1),
		Y: math.Inf(-1),
		Z: math.Inf(-1),
	}

	var walkNode func(nodeIndex int)

	walkNode = func(nodeIndex int) {
		node := doc.Nodes[nodeIndex]

		translation := node.TranslationOrDefault()
		scale := node.ScaleOrDefault()

		if node.Mesh != nil {
			mesh := doc.Meshes[*node.Mesh]

			for _, primitive := range mesh.Primitives {

				posAccessorIndex, ok := primitive.Attributes[gltf.POSITION]
				if !ok {
					continue
				}

				accessor := doc.Accessors[posAccessorIndex]

				positions, err := modeler.ReadPosition(doc, accessor, nil)
				if err != nil {
					continue
				}

				for _, p := range positions {

					x := float64(p[0])*float64(scale[0]) + float64(translation[0])
					y := float64(p[1])*float64(scale[1]) + float64(translation[1])
					z := float64(p[2])*float64(scale[2]) + float64(translation[2])

					if x < min.X {
						min.X = x
					}
					if y < min.Y {
						min.Y = y
					}
					if z < min.Z {
						min.Z = z
					}

					if x > max.X {
						max.X = x
					}
					if y > max.Y {
						max.Y = y
					}
					if z > max.Z {
						max.Z = z
					}
				}
			}
		}

		for _, child := range node.Children {
			walkNode(child)
		}
	}

	sceneIndex := 0
	if doc.Scene != nil {
		sceneIndex = int(*doc.Scene)
	}

	scene := doc.Scenes[sceneIndex]

	for _, nodeIndex := range scene.Nodes {
		walkNode(nodeIndex)
	}

	return &Dimension{
		Min:    min,
		Max:    max,
		Width:  max.X - min.X,
		Height: max.Y - min.Y,
		Depth:  max.Z - min.Z,
	}, nil
}

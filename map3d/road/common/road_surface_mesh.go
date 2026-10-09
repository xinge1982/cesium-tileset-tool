package common

import (
	"fmt"
	"math"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// RoadSurfaceMesh separates road triangulation from document/material creation.
// Positions, normals and UVs belong to the same shared source coordinate frame.
type RoadSurfaceMesh struct {
	Positions [][3]float32
	Normals   [][3]float32
	UVs       [][2]float32
	Indices   []uint32
}

// BuildRoadSurfaceMeshes triangulates original road polygons once, before tiling.
// Input centerlines must already be registered in this builder's projector.
func (b *SurfaceBuilder) BuildRoadSurfaceMeshes(feature SurfaceFeature, rings []LocalRings) ([]RoadSurfaceMesh, error) {
	if len(rings) == 0 {
		return nil, fmt.Errorf("road surface rings are empty")
	}
	var centerline LocalLine
	var hasCenterline bool
	if b.projector != nil {
		centerline, hasCenterline = b.projector.RoadCenterline(featureFieldInt64(feature.Fields, "road_id"))
	}
	meterX, meterY := feature.UV.RepeatX, feature.UV.RepeatY
	if meterX <= 0 {
		meterX = 8
	}
	if meterY <= 0 {
		meterY = meterX
	}
	meshes := make([]RoadSurfaceMesh, 0, len(rings))
	for _, ring := range rings {
		pos, idx, err := triangulateRoadSurfaceRing(ring, centerline, hasCenterline)
		if err != nil {
			return nil, err
		}
		meshes = append(meshes, RoadSurfaceMesh{Positions: pos, Indices: idx, Normals: computeVertexNormals(pos, idx), UVs: fixedMeterUVs(pos, meterX, meterY, feature.UV.FlipV)})
	}
	return meshes, nil
}

// WriteRoadSurfaceMeshes writes original or clipped meshes without regenerating UVs
// or normals. Feature identity is attached afresh in each destination document.
func (b *SurfaceBuilder) WriteRoadSurfaceMeshes(feature SurfaceFeature, meshes []RoadSurfaceMesh) ([]*gltf.Primitive, error) {
	if len(meshes) == 0 {
		return nil, nil
	}
	set := feature.Material
	set.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(set)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road surface top material is invalid")
	}
	out := make([]*gltf.Primitive, 0, len(meshes))
	for _, mesh := range meshes {
		if err := mesh.Validate(); err != nil {
			return nil, err
		}
		if len(mesh.Indices) == 0 {
			continue
		}
		primitive := &gltf.Primitive{Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, mesh.Positions),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, mesh.Normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, mesh.UVs),
		}, Indices: gltf.Index(modeler.WriteIndices(b.doc, mesh.Indices)), Material: gltf.Index(materials.Top)}
		b.attachFeatureMetadata(primitive, len(mesh.Positions), feature.FeatureInput)
		out = append(out, primitive)
	}
	return out, nil
}

func (m RoadSurfaceMesh) Validate() error {
	if len(m.Indices)%3 != 0 || len(m.Positions) != len(m.Normals) || len(m.Positions) != len(m.UVs) {
		return fmt.Errorf("inconsistent road mesh attributes")
	}
	for _, idx := range m.Indices {
		if int(idx) >= len(m.Positions) {
			return fmt.Errorf("road mesh index out of range: %d", idx)
		}
	}
	for i, p := range m.Positions {
		for _, v := range []float32{p[0], p[1], p[2], m.Normals[i][0], m.Normals[i][1], m.Normals[i][2], m.UVs[i][0], m.UVs[i][1]} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("non-finite road mesh vertex %d", i)
			}
		}
	}
	return nil
}

// ClipRoadSurfaceMesh clips triangles against a convex XZ polygon. The polygon
// and mesh must use the same source frame; either polygon winding is accepted.
// Intersections use float64 and interpolate height, normal and UV from the source
// triangle. It never retriangulates the original road boundary or smooths Z again.
func ClipRoadSurfaceMesh(mesh RoadSurfaceMesh, boundary [][2]float64) (RoadSurfaceMesh, error) {
	var out RoadSurfaceMesh
	if err := mesh.Validate(); err != nil {
		return out, err
	}
	if len(boundary) < 3 {
		return out, fmt.Errorf("clip boundary requires at least three points")
	}
	area := 0.0
	for i, a := range boundary {
		if math.IsNaN(a[0]) || math.IsNaN(a[1]) || math.IsInf(a[0], 0) || math.IsInf(a[1], 0) {
			return out, fmt.Errorf("non-finite clip boundary")
		}
		b := boundary[(i+1)%len(boundary)]
		area += a[0]*b[1] - b[0]*a[1]
	}
	if math.Abs(area) < 1e-12 {
		return out, fmt.Errorf("degenerate clip boundary")
	}
	sign := 1.0
	if area < 0 {
		sign = -1
	}
	for i, a := range boundary {
		b, c := boundary[(i+1)%len(boundary)], boundary[(i+2)%len(boundary)]
		if sign*((b[0]-a[0])*(c[1]-b[1])-(b[1]-a[1])*(c[0]-b[0])) < -1e-9 {
			return out, fmt.Errorf("clip boundary must be convex")
		}
	}
	for i := 0; i < len(mesh.Indices); i += 3 {
		polygon := make([]roadClipVertex, 3)
		for j := 0; j < 3; j++ {
			idx := mesh.Indices[i+j]
			polygon[j] = roadClipVertexFromMesh(mesh, int(idx))
		}
		for e, a := range boundary {
			b := boundary[(e+1)%len(boundary)]
			distance := func(v roadClipVertex) float64 {
				return sign * ((b[0]-a[0])*(v.pos[2]-a[1]) - (b[1]-a[1])*(v.pos[0]-a[0]))
			}
			if len(polygon) == 0 {
				break
			}
			next := make([]roadClipVertex, 0, len(polygon)+1)
			previous := polygon[len(polygon)-1]
			pd := distance(previous)
			for _, current := range polygon {
				cd := distance(current)
				if (pd >= 0) != (cd >= 0) {
					next = append(next, interpolateRoadClipVertex(previous, current, pd/(pd-cd)))
				}
				if cd >= 0 {
					next = append(next, current)
				}
				previous, pd = current, cd
			}
			polygon = next
		}
		// A fan preserves source winding. Skip boundary-only and zero-area fragments.
		for j := 1; j+1 < len(polygon); j++ {
			a, b, c := polygon[0], polygon[j], polygon[j+1]
			cross := (b.pos[0]-a.pos[0])*(c.pos[2]-a.pos[2]) - (b.pos[2]-a.pos[2])*(c.pos[0]-a.pos[0])
			if math.Abs(cross) < 1e-10 {
				continue
			}
			for _, v := range []roadClipVertex{a, b, c} {
				out.Indices = append(out.Indices, uint32(len(out.Positions)))
				out.Positions = append(out.Positions, [3]float32{float32(v.pos[0]), float32(v.pos[1]), float32(v.pos[2])})
				length := math.Sqrt(v.normal[0]*v.normal[0] + v.normal[1]*v.normal[1] + v.normal[2]*v.normal[2])
				if length == 0 {
					length = 1
				}
				out.Normals = append(out.Normals, [3]float32{float32(v.normal[0] / length), float32(v.normal[1] / length), float32(v.normal[2] / length)})
				out.UVs = append(out.UVs, [2]float32{float32(v.uv[0]), float32(v.uv[1])})
			}
		}
	}
	return out, out.Validate()
}

type roadClipVertex struct {
	pos, normal [3]float64
	uv          [2]float64
}

func roadClipVertexFromMesh(m RoadSurfaceMesh, i int) roadClipVertex {
	v := roadClipVertex{}
	for j := 0; j < 3; j++ {
		v.pos[j] = float64(m.Positions[i][j])
		v.normal[j] = float64(m.Normals[i][j])
	}
	for j := 0; j < 2; j++ {
		v.uv[j] = float64(m.UVs[i][j])
	}
	return v
}
func interpolateRoadClipVertex(a, b roadClipVertex, t float64) roadClipVertex {
	v := roadClipVertex{}
	for j := 0; j < 3; j++ {
		v.pos[j] = a.pos[j] + t*(b.pos[j]-a.pos[j])
		v.normal[j] = a.normal[j] + t*(b.normal[j]-a.normal[j])
	}
	for j := 0; j < 2; j++ {
		v.uv[j] = a.uv[j] + t*(b.uv[j]-a.uv[j])
	}
	return v
}

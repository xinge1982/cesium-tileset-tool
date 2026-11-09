package road

import (
	"math"
	"sort"

	"cesium-tileset-tool/map3d/road/common"
)

// Keep mesh bounds in the same projected XZ frame as the exact clipping polygons.
type roadMeshBounds struct{ minX, minZ, maxX, maxZ float64 }
type roadMeshRef struct {
	feature, mesh int
	bounds        roadMeshBounds
}
type roadMeshIndexNode struct {
	bounds                  roadMeshBounds
	start, end, left, right int
}
type roadMeshIndex struct {
	items []roadMeshRef
	nodes []roadMeshIndexNode
}

func (b roadMeshBounds) intersects(other roadMeshBounds) bool {
	return b.minX <= other.maxX && b.maxX >= other.minX && b.minZ <= other.maxZ && b.maxZ >= other.minZ
}
func (b roadMeshBounds) union(other roadMeshBounds) roadMeshBounds {
	return roadMeshBounds{math.Min(b.minX, other.minX), math.Min(b.minZ, other.minZ), math.Max(b.maxX, other.maxX), math.Max(b.maxZ, other.maxZ)}
}

// Build once from validated source meshes instead of testing every road against every tile.
func newRoadMeshIndex(meshes [][]common.RoadSurfaceMesh) (*roadMeshIndex, error) {
	index := &roadMeshIndex{}
	for feature, parts := range meshes {
		for part, mesh := range parts {
			if err := mesh.Validate(); err != nil {
				return nil, err
			}
			if len(mesh.Indices) == 0 {
				continue
			}
			p := mesh.Positions[mesh.Indices[0]]
			bounds := roadMeshBounds{float64(p[0]), float64(p[2]), float64(p[0]), float64(p[2])}
			for _, id := range mesh.Indices {
				p := mesh.Positions[id]
				bounds = bounds.union(roadMeshBounds{float64(p[0]), float64(p[2]), float64(p[0]), float64(p[2])})
			}
			index.items = append(index.items, roadMeshRef{feature: feature, mesh: part, bounds: bounds})
		}
	}
	if len(index.items) > 0 {
		index.build(0, len(index.items))
	}
	return index, nil
}

func (index *roadMeshIndex) build(start, end int) int {
	bounds := index.items[start].bounds
	for _, item := range index.items[start+1 : end] {
		bounds = bounds.union(item.bounds)
	}
	node := len(index.nodes)
	index.nodes = append(index.nodes, roadMeshIndexNode{bounds: bounds, start: start, end: end, left: -1, right: -1})
	if end-start <= 8 {
		return node
	}
	xAxis := bounds.maxX-bounds.minX >= bounds.maxZ-bounds.minZ
	sort.Slice(index.items[start:end], func(i, j int) bool {
		a, b := index.items[start+i], index.items[start+j]
		ca, cb := a.bounds.minZ+a.bounds.maxZ, b.bounds.minZ+b.bounds.maxZ
		if xAxis {
			ca, cb = a.bounds.minX+a.bounds.maxX, b.bounds.minX+b.bounds.maxX
		}
		if ca != cb {
			return ca < cb
		}
		if a.feature != b.feature {
			return a.feature < b.feature
		}
		return a.mesh < b.mesh
	})
	middle := start + (end-start)/2
	left, right := index.build(start, middle), index.build(middle, end)
	index.nodes[node].left, index.nodes[node].right = left, right
	return node
}

// Broad-phase hits are conservative; exact clipping still decides whether faces survive.
// Restore source order so property IDs and primitive merging remain deterministic.
func (index *roadMeshIndex) query(boundary [][2]float64) []roadMeshRef {
	if len(index.nodes) == 0 || len(boundary) == 0 {
		return nil
	}
	box := roadMeshBounds{boundary[0][0], boundary[0][1], boundary[0][0], boundary[0][1]}
	for _, point := range boundary[1:] {
		box = box.union(roadMeshBounds{point[0], point[1], point[0], point[1]})
	}
	// Include touching faces and float32 coordinates at projected cell boundaries.
	box.minX -= 1e-6
	box.minZ -= 1e-6
	box.maxX += 1e-6
	box.maxZ += 1e-6
	var result []roadMeshRef
	var visit func(int)
	visit = func(id int) {
		node := index.nodes[id]
		if !node.bounds.intersects(box) {
			return
		}
		if node.left >= 0 {
			visit(node.left)
			visit(node.right)
			return
		}
		for _, item := range index.items[node.start:node.end] {
			if item.bounds.intersects(box) {
				result = append(result, item)
			}
		}
	}
	visit(0)
	sort.Slice(result, func(i, j int) bool {
		if result[i].feature != result[j].feature {
			return result[i].feature < result[j].feature
		}
		return result[i].mesh < result[j].mesh
	})
	return result
}

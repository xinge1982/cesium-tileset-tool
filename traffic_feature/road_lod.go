package traffic_feature

import (
	"fmt"
	"math"

	"cesium-tileset-tool/config"
)

// Keep a content-free road spatial tree traversable when no positive LOD error exists.
const defaultRoadGeometricErrorBase = 128.0

// Validate road thresholds without looking for existing local or MinIO LOD models.
func configuredRoadLODLevels(lod config.TilesetLODConfig) ([]tileLODLevel, error) {
	levels := configuredTileLODLevels(lod)
	previous := math.Inf(1)
	for _, level := range levels {
		value := level.GeometricError
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fmt.Errorf("road LOD%d geometricError must be finite and nonnegative", level.Level)
		}
		if value >= previous {
			return nil, fmt.Errorf("road LOD%d geometricError must be lower than the preceding LOD error", level.Level)
		}
		if level.Level == 3 && value != 0 {
			return nil, fmt.Errorf("road LOD3 geometricError must be 0")
		}
		previous = value
	}
	return levels, nil
}

// Use the configured coarse error for spatial grouping, independently of mesh generation.
func roadGeometricErrorBase(levels []tileLODLevel) float64 {
	for _, level := range levels {
		if level.GeometricError > 0 {
			return level.GeometricError
		}
	}
	return defaultRoadGeometricErrorBase
}

// Reuse generated geometry across configured replacement levels without duplicating GLBs.
// The outer box is world-space until the existing hierarchy refresh converts it once.
func buildGeneratedRoadLODChain(uri string, localBox [12]float64, transform [16]float64, levels []tileLODLevel, hash string, sourceIndex int) *TileNode {
	var root, parent *TileNode
	for i, level := range levels {
		box := localBox
		if i == 0 {
			box = roadWorldBox(localBox, transform)
		}
		node := &TileNode{
			BoundingVolume: BoundingVolume{Box: box},
			Content:        &TileContent{Uri: uri},
			GeometricError: level.GeometricError,
			Refine:         "REPLACE",
			Level:          len(hash),
			Geohash:        fmt.Sprintf("%s#road%d#lod%d", hash, sourceIndex, level.Level),
		}
		if root == nil {
			root = node
			node.Transform = &transform
		} else {
			parent.Children = []*TileNode{node}
		}
		parent = node
	}
	return root
}

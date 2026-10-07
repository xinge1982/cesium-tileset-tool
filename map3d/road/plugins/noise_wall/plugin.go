package noise_wall

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type NoiseWallPlugin struct{}

func NewNoiseWallPlugin() NoiseWallPlugin {
	return NoiseWallPlugin{}
}

func (p NoiseWallPlugin) BuildType() common.BuildType {
	return common.BuildTypeBarrierNoiseWall
}

func (p NoiseWallPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for noise wall: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip noise wall line feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped noise wall line %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildNoiseWall(clipped[i], lines)
		if err != nil {
			return nil, fmt.Errorf("build noise wall from clipped line %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

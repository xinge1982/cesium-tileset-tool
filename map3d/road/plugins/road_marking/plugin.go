package roadmarking

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type RoadMarkingPlugin struct{}

func NewRoadMarkingPlugin() RoadMarkingPlugin {
	return RoadMarkingPlugin{}
}

func (p RoadMarkingPlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadMarking
}

func (p RoadMarkingPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road marking: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip line feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped line %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildRoadMarking(clipped[i], lines)
		if err != nil {
			return nil, fmt.Errorf("build road marking from clipped line %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}

	return primitives, nil
}

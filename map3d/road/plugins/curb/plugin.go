package curb

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type RoadCurbPlugin struct{}

func NewRoadCurbPlugin() RoadCurbPlugin {
	return RoadCurbPlugin{}
}

func (p RoadCurbPlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadCurb
}

func (p RoadCurbPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road curb: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip curb line feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped curb line %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildRoadCurb(clipped[i], lines)
		if err != nil {
			return nil, fmt.Errorf("build road curb from clipped line %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

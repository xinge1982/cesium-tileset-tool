package roadexpansionjoint

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type RoadExpansionJointPlugin struct{}

func NewRoadExpansionJointPlugin() RoadExpansionJointPlugin {
	return RoadExpansionJointPlugin{}
}

func (p RoadExpansionJointPlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadExpansionJoint
}

func (p RoadExpansionJointPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road expansion joint: %s", feature.BuildType)
	}

	if feature.Width <= 0 {
		feature.Width = 0.5
	}
	if feature.Height <= 0 {
		feature.Height = 0.01
	}
	feature.Material.Top.DoubleSided = true

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
		prs, err := runtime.Meshes.BuildRoadExpansionJoint(clipped[i], lines)
		if err != nil {
			return nil, fmt.Errorf("build road expansion joint from clipped line %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}

	return primitives, nil
}

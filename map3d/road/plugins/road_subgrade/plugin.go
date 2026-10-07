package roadsubgrade

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type RoadSubgradePlugin struct{}

func NewRoadSubgradePlugin() RoadSubgradePlugin {
	return RoadSubgradePlugin{}
}

func (p RoadSubgradePlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadSubgrade
}

func (p RoadSubgradePlugin) BuildSurface(runtime common.BuildRuntime, feature common.SurfaceFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road subgrade: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipSurfaceFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip subgrade surface feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		rings, err := runtime.Coordinates.ToLocalSurface(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped subgrade surface %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildRoadSubgrade(clipped[i], rings)
		if err != nil {
			return nil, fmt.Errorf("build subgrade surface from clipped surface %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

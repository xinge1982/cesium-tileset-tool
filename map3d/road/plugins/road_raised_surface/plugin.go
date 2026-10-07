package roadraisedsurface

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type RoadRaisedSurfacePlugin struct{}

func NewRoadRaisedSurfacePlugin() RoadRaisedSurfacePlugin {
	return RoadRaisedSurfacePlugin{}
}

func (p RoadRaisedSurfacePlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadRaisedSurface
}

func (p RoadRaisedSurfacePlugin) BuildSurface(runtime common.BuildRuntime, feature common.SurfaceFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road raised surface: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipSurfaceFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip raised surface feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		rings, err := runtime.Coordinates.ToLocalSurface(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped raised surface %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildRoadGreenbelt(clipped[i], rings)
		if err != nil {
			return nil, fmt.Errorf("build raised surface from clipped surface %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

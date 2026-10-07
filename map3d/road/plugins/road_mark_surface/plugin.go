package roadmarksurface

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type RoadMarkSurfacePlugin struct{}

func NewRoadMarkSurfacePlugin() RoadMarkSurfacePlugin {
	return RoadMarkSurfacePlugin{}
}

func (p RoadMarkSurfacePlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadMarkSurface
}

func (p RoadMarkSurfacePlugin) BuildSurface(runtime common.BuildRuntime, feature common.SurfaceFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road mark surface: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipSurfaceFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip road mark surface feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		rings, err := runtime.Coordinates.ToLocalSurface(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped road mark surface %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildRoadMarkSurface(clipped[i], rings)
		if err != nil {
			return nil, fmt.Errorf("build road mark surface from clipped surface %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

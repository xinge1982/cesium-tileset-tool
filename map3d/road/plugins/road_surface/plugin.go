package roadsurface

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

// RoadSurfaceExtrudePlugin 负责道路面构建。
// 输入为 4326 Polygon/MultiPolygon，裁剪后转局部坐标，最终生成带贴图的道路面。
type RoadSurfaceExtrudePlugin struct{}

func NewRoadSurfaceExtrudePlugin() RoadSurfaceExtrudePlugin {
	return RoadSurfaceExtrudePlugin{}
}

func (p RoadSurfaceExtrudePlugin) BuildType() common.BuildType {
	return common.BuildTypeRoadSurface
}

func (p RoadSurfaceExtrudePlugin) BuildSurface(runtime common.BuildRuntime, feature common.SurfaceFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for road surface extrude: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipSurfaceFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip surface feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		rings, err := runtime.Coordinates.ToLocalSurface(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped surface %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildRoadSurface(clipped[i], rings)
		if err != nil {
			return nil, fmt.Errorf("build road surface from clipped surface %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}

	return primitives, nil
}

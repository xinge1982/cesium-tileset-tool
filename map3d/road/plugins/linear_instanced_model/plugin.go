package linearinstancedmodel

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"
	"strings"

	"github.com/qmuntal/gltf"
)

type LinearInstancedModelPlugin struct{}

func NewLinearInstancedModelPlugin() LinearInstancedModelPlugin {
	return LinearInstancedModelPlugin{}
}

func (p LinearInstancedModelPlugin) BuildType() common.BuildType {
	return common.BuildTypeLinearInstancedModel
}

func (p LinearInstancedModelPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for linear instanced model: %s", feature.BuildType)
	}

	switch linearInstancedModelFeatureType(feature.Fields) {
	case "", "anti_glare_board":
		clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
		if err != nil {
			return nil, fmt.Errorf("clip linear instanced model feature: %w", err)
		}
		primitives := make([]*gltf.Primitive, 0)
		for i := range clipped {
			lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
			if err != nil {
				return nil, fmt.Errorf("convert clipped linear instanced model line %d to local: %w", i, err)
			}
			prs, err := runtime.Meshes.BuildAntiGlareBoard(clipped[i], lines)
			if err != nil {
				return nil, fmt.Errorf("build linear instanced model from clipped line %d: %w", i, err)
			}
			primitives = append(primitives, prs...)
		}
		return primitives, nil
	default:
		return nil, fmt.Errorf("unsupported linear instanced model feature_type: %s", linearInstancedModelFeatureType(feature.Fields))
	}
}

func linearInstancedModelFeatureType(fields common.FeatureFields) string {
	if fields == nil {
		return ""
	}
	v, ok := fields["feature_type"]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

package barrierwave

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"
	"strings"

	"github.com/qmuntal/gltf"
)

type BarrierWavePlugin struct{}

func NewBarrierWavePlugin() BarrierWavePlugin {
	return BarrierWavePlugin{}
}

func (p BarrierWavePlugin) BuildType() common.BuildType {
	return common.BuildTypeBarrierWave
}

func (p BarrierWavePlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for wave barrier: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip wave barrier line feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped wave barrier line %d to local: %w", i, err)
		}
		var prs []*gltf.Primitive
		switch barrierWaveFeatureType(clipped[i].Fields) {
		case "", "wave_two":
			prs, err = runtime.Meshes.BuildGuardrailTwoWave(clipped[i], lines)
		case "wave_three":
			prs, err = runtime.Meshes.BuildGuardrailThreeWave(clipped[i], lines)
		case "wave_nose_end":
			prs, err = runtime.Meshes.BuildGuardrailNoseEnd(clipped[i], lines)
		default:
			err = fmt.Errorf("unsupported barrier wave feature_type: %s", barrierWaveFeatureType(clipped[i].Fields))
		}
		if err != nil {
			return nil, fmt.Errorf("build wave barrier from clipped line %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

func barrierWaveFeatureType(fields common.FeatureFields) string {
	if fields == nil {
		return ""
	}
	v, ok := fields["feature_type"]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

package barrierrigid

import (
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

type BarrierRigidPlugin struct{}

func NewBarrierRigidPlugin() BarrierRigidPlugin {
	return BarrierRigidPlugin{}
}

func (p BarrierRigidPlugin) BuildType() common.BuildType {
	return common.BuildTypeBarrierRigid
}

func (p BarrierRigidPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	if feature.BuildType != p.BuildType() {
		return nil, fmt.Errorf("unexpected build type for rigid barrier: %s", feature.BuildType)
	}

	clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
	if err != nil {
		return nil, fmt.Errorf("clip rigid barrier line feature: %w", err)
	}

	primitives := make([]*gltf.Primitive, 0)
	for i := range clipped {
		lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
		if err != nil {
			return nil, fmt.Errorf("convert clipped rigid barrier line %d to local: %w", i, err)
		}
		prs, err := runtime.Meshes.BuildNewJerseyBarrier(clipped[i], lines)
		if err != nil {
			return nil, fmt.Errorf("build rigid barrier from clipped line %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}
	return primitives, nil
}

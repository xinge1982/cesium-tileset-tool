package roadtollisland

import (
	"cesium-tileset-tool/map3d/road/common"

	"github.com/qmuntal/gltf"
)

type Plugin struct{}

func NewRoadTollIslandPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) BuildType() common.BuildType {
	return common.BuildTypeRoadTollIsland
}

func (p *Plugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
	lines, err := runtime.Coordinates.ToLocalLine(feature)
	if err != nil {
		return nil, err
	}
	return runtime.Meshes.BuildRoadTollIsland(feature, lines)
}

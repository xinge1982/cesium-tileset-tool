package common

import (
	"fmt"

	"github.com/qmuntal/gltf"
)

// BuildRuntime 表示一次瓦片构建过程中的运行时依赖集合。
type BuildRuntime struct {
	Context      TileContext
	Options      BuildOptions
	Document     *gltf.Document
	Coordinates  *CoordinatePipeline
	Materials    *MaterialResolver
	Metadata     *FeatureMetadataBuilder
	Meshes       *SurfaceBuilder
	Projector    *RoadSurfaceProjector
	Profiles     *ProfileFactory
	UVs          *UVBuilder
	Triangulator *Triangulator
}

// LineBuilderPlugin 定义线类对象插件接口。
type LineBuilderPlugin interface {
	BuildType() BuildType
	BuildLine(runtime BuildRuntime, feature LineFeature) ([]*gltf.Primitive, error)
}

// SurfaceBuilderPlugin 定义面类对象插件接口。
type SurfaceBuilderPlugin interface {
	BuildType() BuildType
	BuildSurface(runtime BuildRuntime, feature SurfaceFeature) ([]*gltf.Primitive, error)
}

// BuilderRegistry 是 BuildType 到插件实现的注册表。
type BuilderRegistry struct {
	lineBuilders    map[BuildType]LineBuilderPlugin
	surfaceBuilders map[BuildType]SurfaceBuilderPlugin
}

func NewBuilderRegistry() *BuilderRegistry {
	return &BuilderRegistry{
		lineBuilders:    make(map[BuildType]LineBuilderPlugin),
		surfaceBuilders: make(map[BuildType]SurfaceBuilderPlugin),
	}
}

func (r *BuilderRegistry) RegisterLine(plugin LineBuilderPlugin) error {
	if plugin == nil {
		return fmt.Errorf("line builder plugin is nil")
	}
	bt := plugin.BuildType()
	if bt == "" {
		return fmt.Errorf("line builder plugin build type is empty")
	}
	if _, ok := r.lineBuilders[bt]; ok {
		return fmt.Errorf("line builder plugin already registered: %s", bt)
	}
	r.lineBuilders[bt] = plugin
	return nil
}

func (r *BuilderRegistry) MustRegisterLine(plugin LineBuilderPlugin) {
	if err := r.RegisterLine(plugin); err != nil {
		panic(err)
	}
}

func (r *BuilderRegistry) RegisterSurface(plugin SurfaceBuilderPlugin) error {
	if plugin == nil {
		return fmt.Errorf("surface builder plugin is nil")
	}
	bt := plugin.BuildType()
	if bt == "" {
		return fmt.Errorf("surface builder plugin build type is empty")
	}
	if _, ok := r.surfaceBuilders[bt]; ok {
		return fmt.Errorf("surface builder plugin already registered: %s", bt)
	}
	r.surfaceBuilders[bt] = plugin
	return nil
}

func (r *BuilderRegistry) MustRegisterSurface(plugin SurfaceBuilderPlugin) {
	if err := r.RegisterSurface(plugin); err != nil {
		panic(err)
	}
}

func (r *BuilderRegistry) Line(buildType BuildType) (LineBuilderPlugin, bool) {
	p, ok := r.lineBuilders[buildType]
	return p, ok
}

func (r *BuilderRegistry) Surface(buildType BuildType) (SurfaceBuilderPlugin, bool) {
	p, ok := r.surfaceBuilders[buildType]
	return p, ok
}

type PlaceholderLinePlugin struct {
	buildType BuildType
}

func NewPlaceholderLinePlugin(buildType BuildType) PlaceholderLinePlugin {
	return PlaceholderLinePlugin{buildType: buildType}
}

func (p PlaceholderLinePlugin) BuildType() BuildType {
	return p.buildType
}

func (p PlaceholderLinePlugin) BuildLine(runtime BuildRuntime, feature LineFeature) ([]*gltf.Primitive, error) {
	return nil, fmt.Errorf("line builder plugin not implemented: %s", p.buildType)
}

type PlaceholderSurfacePlugin struct {
	buildType BuildType
}

func NewPlaceholderSurfacePlugin(buildType BuildType) PlaceholderSurfacePlugin {
	return PlaceholderSurfacePlugin{buildType: buildType}
}

func (p PlaceholderSurfacePlugin) BuildType() BuildType {
	return p.buildType
}

func (p PlaceholderSurfacePlugin) BuildSurface(runtime BuildRuntime, feature SurfaceFeature) ([]*gltf.Primitive, error) {
	return nil, fmt.Errorf("surface builder plugin not implemented: %s", p.buildType)
}

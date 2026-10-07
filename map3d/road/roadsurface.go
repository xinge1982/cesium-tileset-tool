package road

import (
	"bytes"
	"cesium-tileset-tool/map3d/road/common"
	"fmt"

	"github.com/qmuntal/gltf"
)

// RoadTileBuilder 是道路类瓦片构建的总入口。
//
// 它的职责不是直接处理某一种具体几何，而是负责组织整条生产链：
// 1. 接收当前瓦片内的线类、面类对象；
// 2. 结合 TileContext 把所有对象统一到同一个局部坐标系；
// 3. 调用不同的子构建器生成 primitive；
// 4. 最终汇总为一个 glTF/GLB 文档，供 3D Tiles 瓦片输出。
//
// 这个结构体后续应当作为 road 包中“公共道路瓦片构建入口”，
// 上层业务只需要准备好当前瓦片的上下文和标准化后的 Feature 列表。
type RoadTileBuilder struct {
	ctx         TileContext
	opt         BuildOptions
	registry    *BuilderRegistry
	centerlines []CenterlineFeature
	lines       []LineFeature
	surfaces    []SurfaceFeature
	lastStats   BuildStats
}

// NewRoadTileBuilder 创建一个新的道路瓦片构建器。
//
// 参数说明：
// - ctx: 当前瓦片的上下文，例如瓦片中心点、范围、局部坐标原点等；
// - opt: 构建选项，可为 nil，nil 时会自动使用默认值。
//
// 这里仅做基础校验和默认值填充，不做具体几何构建。
func NewRoadTileBuilder(ctx TileContext, opt *BuildOptions) (*RoadTileBuilder, error) {
	out := &RoadTileBuilder{ctx: ctx}
	if opt != nil {
		out.opt = *opt
	} else {
		out.opt = DefaultBuildOptions()
	}
	out.registry = DefaultBuilderRegistry()
	return out, nil
}

// SetRegistry 设置自定义插件注册表。
//
// 当业务方希望替换默认插件，或者在运行时注入新的 BuildType 实现时，
// 可以通过该方法覆盖默认 registry。
func (b *RoadTileBuilder) SetRegistry(registry *BuilderRegistry) error {
	if registry == nil {
		return fmt.Errorf("builder registry is nil")
	}
	b.registry = registry
	return nil
}

// AddLineFeatures 向当前瓦片构建器中追加线类对象。
//
// 线类对象包括但不限于：
// - 道路标线；
// - 车道边线；
// - 导流带中心线；
// - 路缘石中心线；
// - 护栏、隔音墙、围栏等沿线扫掠对象。
func (b *RoadTileBuilder) AddLineFeatures(features ...LineFeature) {
	b.lines = append(b.lines, features...)
}

// AddCenterlineFeatures 添加道路中心线。
//
// 中心线不会直接生成模型，而是用于辅助道路面的条带三角剖分。
func (b *RoadTileBuilder) AddCenterlineFeatures(features ...CenterlineFeature) {
	b.centerlines = append(b.centerlines, features...)
}

// AddSurfaceFeatures 向当前瓦片构建器中追加面类对象。
//
// 面类对象包括但不限于：
// - 道路面；
// - 建筑物底面；
// - 顶棚面；
// - 其它需要由 Polygon/MultiPolygon 构建实体的对象。
func (b *RoadTileBuilder) AddSurfaceFeatures(features ...SurfaceFeature) {
	b.surfaces = append(b.surfaces, features...)
}

// BuildDocument 构建当前瓦片的 glTF 文档和 primitive 列表。
//
// 预期后续流程：
// 1. 先创建 glTF Document；
// 2. 初始化材质解析器、坐标处理管线、UV/三角剖分工具；
// 3. 处理所有线类对象；
// 4. 处理所有面类对象；
// 5. 汇总生成 primitive 并返回。
//
// 注意：
// 当前阶段仅保留方法骨架，后续逐步实现。
func (b *RoadTileBuilder) BuildDocument() (*gltf.Document, []*gltf.Primitive, error) {
	if err := b.validateInputs(); err != nil {
		return nil, nil, err
	}

	doc := gltf.NewDocument()
	runtime, err := b.newRuntime(doc)
	if err != nil {
		return nil, nil, err
	}
	if err := b.preloadCenterlines(runtime); err != nil {
		return nil, nil, err
	}

	primitives := make([]*gltf.Primitive, 0)

	for i := range b.surfaces {
		plugin, ok := b.registry.Surface(b.surfaces[i].BuildType)
		if !ok {
			return nil, nil, fmt.Errorf("surface build type not registered: %s", b.surfaces[i].BuildType)
		}
		prs, err := plugin.BuildSurface(runtime, b.surfaces[i])
		if err != nil {
			return nil, nil, fmt.Errorf("build surface feature %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}

	for i := range b.lines {
		plugin, ok := b.registry.Line(b.lines[i].BuildType)
		if !ok {
			return nil, nil, fmt.Errorf("line build type not registered: %s", b.lines[i].BuildType)
		}
		prs, err := plugin.BuildLine(runtime, b.lines[i])
		if err != nil {
			return nil, nil, fmt.Errorf("build line feature %d: %w", i, err)
		}
		primitives = append(primitives, prs...)
	}

	if runtime.Metadata != nil {
		if err := runtime.Metadata.Finalize(); err != nil {
			return nil, nil, fmt.Errorf("finalize feature metadata: %w", err)
		}
	}
	if err := mergeInstancedNodesByMeshAndName(doc); err != nil {
		return nil, nil, fmt.Errorf("merge instanced nodes: %w", err)
	}
	if b.opt.MergePrimitives {
		primitives, err = mergePrimitivesByBatch(doc, primitives)
		if err != nil {
			return nil, nil, fmt.Errorf("merge primitives: %w", err)
		}
	}

	b.lastStats = BuildStats{
		PrimitiveCount: len(primitives),
		MaterialCount:  len(doc.Materials),
		MeshCount:      len(doc.Meshes),
		NodeCount:      len(doc.Nodes),
		FeatureCount:   countFeatureRows(b.lines, b.surfaces),
	}

	return doc, primitives, nil
}

// BuildBinary 构建当前瓦片的二进制 GLB。
//
// 该方法是上层最常用的输出入口，最终返回的字节流可直接作为
// 3D Tiles 1.1 中 content 的 glb 内容。
func (b *RoadTileBuilder) BuildBinary() ([]byte, error) {
	doc, primitives, err := b.BuildDocument()
	if err != nil {
		return nil, err
	}
	if len(primitives) == 0 {
		return nil, fmt.Errorf("road tile build produced no primitives")
	}
	appendPrimitivesAsMesh(doc, primitives)
	if err := CompactDocument(doc); err != nil {
		return nil, err
	}
	buff := new(bytes.Buffer)
	enc := gltf.NewEncoder(buff)
	enc.AsBinary = true
	err = enc.Encode(doc)
	if err != nil {
		return nil, err
	}
	glb := buff.Bytes()
	b.lastStats.GLBBytes = len(glb)
	return glb, nil
}

func (b *RoadTileBuilder) LastBuildStats() BuildStats {
	return b.lastStats
}

// validateInputs 校验当前构建器的输入状态。
//
// 这里应只做“输入是否合法”的校验，不应做实际构建工作。
// 例如：
// - TileContext 是否完整；
// - BasePoint 是否可用；
// - 宽度/高度/厚度是否为非法值；
// - Feature 的材质配置是否缺失。
func (b *RoadTileBuilder) validateInputs() error {
	resolved, err := ResolveTileContextAuto(b.ctx, b.centerlines, b.lines, b.surfaces)
	if err != nil {
		return err
	}
	b.ctx = resolved

	if err := b.ctx.Validate(); err != nil {
		return err
	}
	for i := range b.lines {
		if err := b.lines[i].Validate(); err != nil {
			return fmt.Errorf("line feature %d: %w", i, err)
		}
	}
	for i := range b.centerlines {
		if err := b.centerlines[i].Validate(); err != nil {
			return fmt.Errorf("centerline feature %d: %w", i, err)
		}
	}
	for i := range b.surfaces {
		if err := b.surfaces[i].Validate(); err != nil {
			return fmt.Errorf("surface feature %d: %w", i, err)
		}
	}
	return nil
}

func (b *RoadTileBuilder) preloadCenterlines(runtime BuildRuntime) error {
	if runtime.Projector == nil || runtime.Coordinates == nil {
		return nil
	}
	for i := range b.surfaces {
		estimated, err := EstimateCenterlineFeatureFromSurface(b.surfaces[i])
		if err != nil {
			continue
		}
		if err := b.addCenterlineToProjector(runtime, estimated); err != nil {
			continue
		}
	}
	if !b.opt.UseInputCenterline {
		return nil
	}
	for i := range b.centerlines {
		if err := b.addCenterlineToProjector(runtime, b.centerlines[i]); err != nil {
			return fmt.Errorf("load input centerline %d: %w", i, err)
		}
	}
	return nil
}

func (b *RoadTileBuilder) addCenterlineToProjector(runtime BuildRuntime, feature CenterlineFeature) error {
	lineFeature := LineFeature{
		FeatureInput: FeatureInput{
			Geom:   feature.Geom,
			Fields: feature.Fields,
		},
	}
	lines, err := runtime.Coordinates.ToLocalLine(lineFeature)
	if err != nil {
		return fmt.Errorf("convert centerline to local: %w", err)
	}
	roadID := featureFieldInt64(feature.Fields, "hroad_id")
	if roadID == 0 {
		roadID = featureFieldInt64(feature.Fields, "road_id")
	}
	if roadID == 0 {
		roadID = featureFieldInt64(feature.Fields, "id")
	}
	if roadID == 0 {
		return nil
	}
	runtime.Projector.AddRoadCenterline(roadID, lines)
	return nil
}

func (b *RoadTileBuilder) newRuntime(doc *gltf.Document) (BuildRuntime, error) {
	coords, err := NewCoordinatePipeline(b.ctx)
	if err != nil {
		return BuildRuntime{}, err
	}
	materials := NewMaterialResolver(doc)
	projector := NewRoadSurfaceProjector()
	projector.Configure(b.opt.ProjectSearchDist, b.opt.ProjectSmoothMeter)
	metadata := common.NewFeatureMetadataBuilder(doc)
	return BuildRuntime{
		Context:      b.ctx,
		Options:      b.opt,
		Document:     doc,
		Coordinates:  coords,
		Materials:    materials,
		Metadata:     metadata,
		Meshes:       NewSurfaceBuilder(doc, materials, metadata, projector, b.resolveLineProjectionMode(), b.resolveLineSampleStep()),
		Projector:    projector,
		Profiles:     NewProfileFactory(),
		UVs:          NewUVBuilder(),
		Triangulator: NewTriangulator(),
	}, nil
}

func (b *RoadTileBuilder) resolveLineProjectionMode() string {
	if b.opt.LineProjectionMode != "" {
		return b.opt.LineProjectionMode
	}
	return "fast"
}

func (b *RoadTileBuilder) resolveLineSampleStep() float32 {
	if b.opt.LineSampleStep > 0 {
		return b.opt.LineSampleStep
	}
	if b.opt.MarkingSampleStep > 0 {
		return b.opt.MarkingSampleStep
	}
	return 6.0
}

func countFeatureRows(lines []LineFeature, surfaces []SurfaceFeature) int {
	total := 0
	for _, feature := range lines {
		if len(feature.Features) > 0 {
			total += len(feature.Features)
		} else {
			total++
		}
	}
	for _, feature := range surfaces {
		if len(feature.Features) > 0 {
			total += len(feature.Features)
		} else {
			total++
		}
	}
	return total
}

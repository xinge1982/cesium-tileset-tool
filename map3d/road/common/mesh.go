package common

import (
	"cesium-tileset-tool/map3d/mgltf"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// 负责把局部几何真正写�?glTF primitive�1�7?
type SurfaceBuilder struct {
	doc             *gltf.Document
	materials       *MaterialResolver
	metadata        *FeatureMetadataBuilder
	projector       *RoadSurfaceProjector
	profiles        *ProfileFactory
	lineProjectMode string
	sampleStep      float32
	instancedModels map[string]int
}

// 根据道路标线中心线生成可渲染的 glTF 面片。
// 负责保存 glTF 文档、材质解析器、要素元数据、道路投影器等依赖，同时初始化线型生成参数；当采样步长或投影模式未配置时，会使用默认值。
func NewSurfaceBuilder(doc *gltf.Document, materials *MaterialResolver, metadata *FeatureMetadataBuilder, projector *RoadSurfaceProjector, lineProjectMode string, sampleStep float32) *SurfaceBuilder {
	if sampleStep <= 0 {
		sampleStep = 0.5
	}
	if lineProjectMode == "" {
		lineProjectMode = "fast"
	}
	return &SurfaceBuilder{
		doc:             doc,
		materials:       materials,
		metadata:        metadata,
		projector:       projector,
		profiles:        NewProfileFactory(),
		lineProjectMode: lineProjectMode,
		sampleStep:      sampleStep,
		instancedModels: make(map[string]int),
	}
}

// 根据道路标线中心线生成可渲染的 glTF 面片。
// 会解析标线宽度、颜色、虚实线样式和关联道路 ID；优先将标线投影到道路三角网表面，再扩展成带宽度的条带、生成 UV/法线/索引，并附加要素元数据。
func (b *SurfaceBuilder) BuildRoadMarking(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("road marking lines are empty")
	}

	width := feature.Width
	if width <= 0 {
		width = featureFieldFloat32(feature.Fields, "bxkd")
	}
	if width <= 0 {
		width = 0.15
	}

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	if isZeroMaterialRef(materialSet.Top) {
		color := featureFieldString(feature.Fields, "ys")
		if color == "" {
			color = "#FFFFFF"
		}
		materialSet.Top = MaterialRef{
			Name:        "road-marking-color",
			Color:       strings.TrimSpace(color),
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}

	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road marking top material is invalid")
	}

	uv := mgltf.UvsParams{
		RepeatX: feature.UV.RepeatX,
		RepeatY: feature.UV.RepeatY,
		FlipV:   feature.UV.FlipV,
	}
	if uv.RepeatX == 0 {
		uv.RepeatX = 1
	}
	if uv.RepeatY == 0 {
		uv.RepeatY = 1
	}

	roadID := featureFieldInt64(feature.Fields, "ldid")
	dashPattern := strings.TrimSpace(featureFieldString(feature.Fields, "bxbl"))
	startOffset := featureFieldFloat64(feature.Fields, "startoffset")
	effectiveSampleStep := b.sampleStep
	projectionMode := b.resolveLineProjectionMode(feature.Fields)
	// 默认不做固定步长插点，改为仅在穿越道路三角边界时插点。
	// 若上层显式给出 LineSampleStep/MarkingSampleStep，则再额外 densify。
	if featureFieldBool(feature.Fields, "low_cost") {
		if effectiveSampleStep > 0 && effectiveSampleStep < 6.0 {
			effectiveSampleStep = 6.0
		}
	}
	clearance := feature.Height
	if clearance <= 0 {
		clearance = 0.05
	}
	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32
	accumulatedOffset := startOffset

	for _, line := range lines {
		working := line
		renderLift := float32(0)
		if b.projector != nil && roadID != 0 {
			projected, ok := b.projector.ProjectLine(roadID, line, effectiveSampleStep, projectionMode)
			if ok {
				working = projected
			} else {
				b.projector.WarnMissingRoad(feature)
				renderLift = clearance
			}
		} else if b.projector != nil && roadID == 0 {
			b.projector.WarnMissingRoad(feature)
			renderLift = clearance
		} else {
			renderLift = clearance
		}

		renderLines := []LocalLine{working}
		if dashLen, gapLen, ok := ParseDashPattern(dashPattern); ok {
			renderLines = splitDashedLines(working, dashLen, gapLen, accumulatedOffset)
		}

		for _, renderLine := range renderLines {
			points := dedupeLinePoints(renderLine.Points)
			if len(points) < 2 {
				continue
			}
			rings, err := b.profiles.ExpandLineToSurface(LocalLine{Points: points, Closed: false}, width)
			if err != nil {
				return nil, err
			}
			if b.projector != nil {
				projectedOuter, ok := b.projector.ProjectPoints(roadID, rings.Outer)
				if ok {
					rings.Outer = projectedOuter
				}
			}
			if renderLift != 0 || (b.projector != nil && roadID != 0) {
				lift := renderLift
				if lift == 0 {
					lift = clearance
				}
				for i := range rings.Outer {
					rings.Outer[i][1] += lift
				}
			}
			pos, indices, err := triangulateExpandedLineSurface(points, rings.Outer)
			if err != nil {
				return nil, err
			}
			normals := computeVertexNormals(pos, indices)
			segUV := fixedMeterUVs(pos, width, width, uv.FlipV)
			base := uint32(len(batchPos))
			batchPos = append(batchPos, pos...)
			batchNormals = append(batchNormals, normals...)
			batchUV = append(batchUV, segUV...)
			for _, idx := range indices {
				batchIdx = append(batchIdx, base+idx)
			}
		}

		accumulatedOffset += polylineLengthXZ(working.Points)
	}

	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)
	return []*gltf.Primitive{primitive}, nil
}

// 根据线要素生成道路伸缩缝表面。
// 处理默认宽度、材质、道路投影和离地高度，然后将中心线扩展为窄条面，并按线方向连续生成 UV，最终合并为一个 glTF Primitive。
func (b *SurfaceBuilder) BuildRoadExpansionJoint(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("road expansion joint lines are empty")
	}

	width := feature.Width
	if width <= 0 {
		width = 0.5
	}

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road expansion joint top material is invalid")
	}

	roadID := featureFieldInt64(feature.Fields, "ldid")
	effectiveSampleStep := b.sampleStep
	projectionMode := b.resolveLineProjectionMode(feature.Fields)
	// Ĭ�ϲ����̶�������㣬��Ϊ���ڴ�Խ��·���Ǳ߽�ʱ��㡣
	if featureFieldBool(feature.Fields, "low_cost") {
		if effectiveSampleStep > 0 && effectiveSampleStep < 6.0 {
			effectiveSampleStep = 6.0
		}
	}
	clearance := feature.Height
	if clearance <= 0 {
		clearance = 0.01
	}

	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32

	for _, line := range lines {
		working := line
		renderLift := float32(0)
		if b.projector != nil && roadID != 0 {
			projected, ok := b.projector.ProjectLine(roadID, line, effectiveSampleStep, projectionMode)
			if ok {
				working = projected
			} else {
				b.projector.WarnMissingRoad(feature)
				renderLift = clearance
			}
		} else if b.projector != nil && roadID == 0 {
			b.projector.WarnMissingRoad(feature)
			renderLift = clearance
		} else {
			renderLift = clearance
		}

		points := dedupeLinePoints(working.Points)
		if len(points) < 2 {
			continue
		}
		rings, err := b.profiles.ExpandLineToSurface(LocalLine{Points: points, Closed: false}, width)
		if err != nil {
			return nil, err
		}
		if b.projector != nil {
			projectedOuter, ok := b.projector.ProjectPoints(roadID, rings.Outer)
			if ok {
				rings.Outer = projectedOuter
			}
		}
		if renderLift != 0 || (b.projector != nil && roadID != 0) {
			lift := renderLift
			if lift == 0 {
				lift = clearance
			}
			for i := range rings.Outer {
				rings.Outer[i][1] += lift
			}
		}

		pos, indices, err := triangulateExpandedLineSurface(points, rings.Outer)
		if err != nil {
			return nil, err
		}
		normals := computeVertexNormals(pos, indices)
		segUV := expandedLineStripUVs(points, feature.UV)
		if len(segUV) != len(pos) {
			return nil, fmt.Errorf("road expansion joint uv count mismatch")
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, segUV...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
	}

	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)
	return []*gltf.Primitive{primitive}, nil
}

// 解析当前线要素使用的道路投影模式。
// 优先读取 feature.Fields 中的 projection_mode；为空时使用 SurfaceBuilder 的默认配置，只接受 boundary_split 和 fast，非法值统一回退到 fast。
func (b *SurfaceBuilder) resolveLineProjectionMode(fields FeatureFields) string {
	mode := strings.ToLower(strings.TrimSpace(featureFieldString(fields, "projection_mode")))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(b.lineProjectMode))
	}
	switch mode {
	case "boundary_split", "fast":
		return mode
	default:
		return "fast"
	}
}

// 将“中心线 + 扩展后的外轮廓”转换为连续三角带。
// 从扩展环中拆出左右两侧顶点，并按中心线顺序连接相邻左右点，生成稳定的三角形索引；输入几何不足时返回错误。
func triangulateExpandedLineSurface(centerline [][3]float32, ring [][3]float32) ([][3]float32, []uint32, error) {
	n := len(centerline)
	if n < 2 {
		return nil, nil, fmt.Errorf("expanded line surface requires at least 2 points")
	}
	if len(ring) < 2*n {
		return nil, nil, fmt.Errorf("expanded line ring is too small")
	}

	left := append([][3]float32(nil), ring[:n]...)
	rightRev := ring[n : 2*n]
	right := make([][3]float32, n)
	for i := 0; i < n; i++ {
		right[i] = rightRev[n-1-i]
	}

	pos := make([][3]float32, 0, 2*n)
	pos = append(pos, left...)
	rightBase := uint32(len(pos))
	pos = append(pos, right...)

	indices := make([]uint32, 0, (n-1)*6)
	for i := 0; i < n-1; i++ {
		l0 := uint32(i)
		l1 := uint32(i + 1)
		r0 := rightBase + uint32(i)
		r1 := rightBase + uint32(i+1)
		appendStripTriangle(&indices, pos, l0, r0, l1)
		appendStripTriangle(&indices, pos, l1, r0, r1)
	}
	if len(indices) == 0 {
		return nil, nil, fmt.Errorf("expanded line surface generated no triangles")
	}
	return pos, indices, nil
}

// 为扩展后的线状条带生成连续 UV。
// U 方向按照中心线累计长度进行归一化和重复，V 方向区分条带左右两侧，并支持 RepeatX、RepeatY 和 FlipV 配置。
func expandedLineStripUVs(centerline [][3]float32, opt UVOptions) [][2]float32 {
	n := len(centerline)
	if n == 0 {
		return nil
	}
	repeatX := opt.RepeatX
	if repeatX == 0 {
		repeatX = 1
	}
	repeatY := opt.RepeatY
	if repeatY == 0 {
		repeatY = 1
	}

	uvals := make([]float32, n)
	total := float32(0)
	for i := 1; i < n; i++ {
		total += float32(segmentLengthXZ(centerline[i-1], centerline[i]))
		uvals[i] = total
	}
	if total > 0 {
		for i := range uvals {
			uvals[i] = (uvals[i] / total) * repeatX
		}
	}

	uvs := make([][2]float32, 0, n*2)
	v0 := float32(0)
	v1 := repeatY
	if opt.FlipV {
		v0, v1 = v1, v0
	}
	for i := 0; i < n; i++ {
		uvs = append(uvs, [2]float32{v0, 1 - uvals[i]})
	}
	for i := 0; i < n; i++ {
		uvs = append(uvs, [2]float32{v1, 1 - uvals[i]})
	}
	return uvs
}

// 构建普通线性隔离设施。
// 当前函数仅保留接口，尚未实现实际几何生成逻辑，调用时会直接返回 not implemented 错误。
func (b *SurfaceBuilder) BuildLinearBarrier(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	return nil, fmt.Errorf("build linear barrier: not implemented")
}

// 根据道路边线生成路缘石几何。
// 自动补充默认宽度和高度，解析路缘石方向，并逐条调用路缘石 Mesh 构建逻辑，最后合并顶点、法线、UV 和索引，并写入要素元数据。
func (b *SurfaceBuilder) BuildRoadCurb(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("road curb lines are empty")
	}

	width := feature.Width
	if width <= 0 {
		width = 0.13
	}
	height := feature.Height
	if height <= 0 {
		height = 0.13
	}

	materials, err := b.materials.ResolveMaterialSet(feature.Material)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road curb material is invalid")
	}

	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32

	direction := curbDirection(feature.Fields)
	for _, line := range lines {
		pos, normals, uv, indices, err := buildRoadCurbMesh(line, width, height, direction)
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 || len(indices) == 0 {
			continue
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, uv...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
	}

	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)
	return []*gltf.Primitive{primitive}, nil
}

// 根据输入线生成 New Jersey 型混凝土防撞墙。
// 会从 feature 或 Fields 中读取宽度、高度并提供默认尺寸，根据方向生成对应截面几何，然后将所有线段批量合并成一个 glTF Primitive。
func (b *SurfaceBuilder) BuildNewJerseyBarrier(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("new jersey barrier lines are empty")
	}

	width := feature.Width
	if width <= 0 {
		width = featureFieldFloat32(feature.Fields, "width")
	}
	if width <= 0 {
		width = featureFieldFloat32(feature.Fields, "barrier_width")
	}
	if width <= 0 {
		width = 0.72
	}
	height := feature.Height
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "height")
	}
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "hight")
	}
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "barrier_height")
	}
	if height <= 0 {
		height = 0.81
	}

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	materialSet.Side.DoubleSided = true
	materialSet.Bottom.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("new jersey barrier material is invalid")
	}

	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32

	direction := curbDirection(feature.Fields)
	for _, line := range lines {
		pos, normals, uv, indices, err := buildNewJerseyBarrierMesh(line, width, height, direction)
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 || len(indices) == 0 {
			continue
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, uv...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
	}
	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)
	return []*gltf.Primitive{primitive}, nil
}

// 构建道路声屏障主体以及立柱实例。
// 主体按照线方向、高度、顶部折弯距离和纹理重复距离生成连续面；立柱则根据 spacing 等参数计算实例变换，通过 GPU Instancing 方式加入场景。
func (b *SurfaceBuilder) BuildNoiseWall(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("noise wall lines are empty")
	}

	height := feature.Height
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "height")
	}
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "hight")
	}
	if height <= 0 {
		height = defaultNoiseWallHeight
	}

	bendOffset := featureFieldFloat32(feature.Fields, "bend_offset")
	if bendOffset <= 0 {
		bendOffset = featureFieldFloat32(feature.Fields, "bend")
	}
	if bendOffset <= 0 {
		bendOffset = featureFieldFloat32(feature.Fields, "bendOffset")
	}
	if bendOffset <= 0 {
		bendOffset = defaultNoiseWallBendOffset
	}

	repeatU := feature.UV.RepeatX
	if repeatU <= 0 {
		repeatU = defaultNoiseWallTileMeters
	}

	materialSet := feature.Material
	if isZeroMaterialRef(materialSet.Top) {
		materialSet.Top = MaterialRef{
			Name:        "noise-wall-panel",
			Color:       "#AFC8D6",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	if isZeroMaterialRef(materialSet.Side) {
		materialSet.Side = MaterialRef{
			Name:        "noise-wall-post",
			Color:       "#7A8088",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	materialSet.Top.DoubleSided = true
	materialSet.Side.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("noise wall material is invalid")
	}

	direction := guardrailDirection(feature.Fields)
	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32
	var postTransforms []instancedTransform
	postSpacing := featureFieldFloat32(feature.Fields, "spacing")
	if postSpacing <= 0 {
		postSpacing = featureFieldFloat32(feature.Fields, "interval")
	}
	if postSpacing <= 0 {
		postSpacing = defaultNoiseWallPostSpacing
	}
	postWidth := featureFieldFloat32(feature.Fields, "post_width")
	if postWidth <= 0 {
		postWidth = defaultNoiseWallPostWidth
	}
	postDepth := featureFieldFloat32(feature.Fields, "post_depth")
	if postDepth <= 0 {
		postDepth = defaultNoiseWallPostDepth
	}
	for _, line := range lines {
		pos, normals, uv, indices, err := buildNoiseWallMesh(line, direction, height, bendOffset, repeatU)
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 || len(indices) == 0 {
			continue
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, uv...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
		posts, err := noiseWallPostTransforms(line, direction, height, postSpacing, postWidth, postDepth)
		if err != nil {
			return nil, err
		}
		postTransforms = append(postTransforms, posts...)
	}
	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)
	if len(postTransforms) > 0 {
		postPos, postNormals, postUV, postIdx, err := buildNoiseWallBentPostMesh(height, postWidth, postDepth, bendOffset)
		if err != nil {
			return nil, err
		}
		postPrimitive := &gltf.Primitive{
			Attributes: gltf.PrimitiveAttributes{
				gltf.POSITION:   modeler.WritePosition(b.doc, postPos),
				gltf.NORMAL:     modeler.WriteNormal(b.doc, postNormals),
				gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, postUV),
			},
			Indices:  gltf.Index(modeler.WriteIndices(b.doc, postIdx)),
			Material: gltf.Index(materials.Side),
		}
		featureID := uint32(0)
		if b.metadata != nil {
			ids := b.metadata.RegisterFeatureRows(feature.Features, feature.Fields)
			if len(ids) > 0 {
				featureID = ids[0]
			}
		}
		postMaterial := materials.Side
		if postMaterial < 0 {
			postMaterial = materials.Top
		}
		postPrimitive.Material = gltf.Index(postMaterial)
		addInstancedPrimitiveNode(b.doc, len(b.doc.Meshes), postPrimitive, "noise-wall-posts", postTransforms, featureID)
	}
	return []*gltf.Primitive{primitive}, nil
}

// 根据道路边线生成双波形护栏。
// 负责生成连续双波形护栏板，并沿线路径按固定规则布置支撑立柱；护栏主体采用普通 Primitive，重复立柱使用实例化方式降低数据量。
func (b *SurfaceBuilder) BuildGuardrailTwoWave(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("guardrail two wave lines are empty")
	}

	materialSet := feature.Material
	if isZeroMaterialRef(materialSet.Top) {
		materialSet.Top = MaterialRef{
			Name:        "guardrail-two-wave-rail",
			Color:       "#B8BDC5",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	if isZeroMaterialRef(materialSet.Side) {
		materialSet.Side = MaterialRef{
			Name:        "guardrail-two-wave-support",
			Color:       "#8D949C",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	materialSet.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("guardrail two wave material is invalid")
	}

	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32
	var supportTransforms []instancedTransform

	direction := guardrailDirection(feature.Fields)
	railHeight, railHeightAll := guardrailRailHeights(feature.Fields, defaultGuardrailRailHeight, defaultGuardrailRailHeightAll)
	for _, line := range lines {
		pos, normals, uv, indices, err := buildGuardrailTwoWaveMesh(line, direction, railHeight, railHeightAll)
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 || len(indices) == 0 {
			continue
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, uv...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
		supports, err := guardrailSupportTransforms(line)
		if err != nil {
			return nil, err
		}
		supportTransforms = append(supportTransforms, supports...)
	}
	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)

	featureID := uint32(0)
	if b.metadata != nil {
		ids := b.metadata.RegisterFeatureRows(feature.Features, feature.Fields)
		if len(ids) > 0 {
			featureID = ids[0]
		}
	}
	instanceMaterial := materials.Side
	if instanceMaterial < 0 {
		instanceMaterial = materials.Top
	}
	if len(supportTransforms) > 0 {
		supportPos, supportNormals, supportUV, supportIdx := buildGuardrailSupportMesh(direction, railHeight, railHeightAll)
		supportPrimitive := buildPrimitiveFromGeometry(b.doc, instanceMaterial, supportPos, supportNormals, supportUV, supportIdx)
		addInstancedPrimitiveNode(b.doc, len(b.doc.Meshes), supportPrimitive, "guardrail-supports", supportTransforms, featureID)
	}
	return []*gltf.Primitive{primitive}, nil
}

// 根据道路边线生成三波形护栏。
// 整体处理方式与双波护栏类似，但使用三波护栏对应的高度、截面和支架参数，并为支架创建独立的实例化 Mesh 节点。
func (b *SurfaceBuilder) BuildGuardrailThreeWave(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("guardrail three wave lines are empty")
	}

	materialSet := feature.Material
	if isZeroMaterialRef(materialSet.Top) {
		materialSet.Top = MaterialRef{
			Name:        "guardrail-three-wave-rail",
			Color:       "#B8BDC5",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	if isZeroMaterialRef(materialSet.Side) {
		materialSet.Side = MaterialRef{
			Name:        "guardrail-three-wave-support",
			Color:       "#8D949C",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	materialSet.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("guardrail three wave material is invalid")
	}

	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32
	var supportTransforms []instancedTransform

	direction := guardrailDirection(feature.Fields)
	railHeight, railHeightAll := guardrailRailHeights(feature.Fields, defaultGuardrailThreeWaveRailHeight, defaultGuardrailThreeWaveRailHeightAll)
	for _, line := range lines {
		pos, normals, uv, indices, err := buildGuardrailThreeWaveMesh(line, direction, railHeight, railHeightAll)
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 || len(indices) == 0 {
			continue
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, uv...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
		supports, err := guardrailSupportTransforms(line)
		if err != nil {
			return nil, err
		}
		supportTransforms = append(supportTransforms, supports...)
	}
	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)

	featureID := uint32(0)
	if b.metadata != nil {
		ids := b.metadata.RegisterFeatureRows(feature.Features, feature.Fields)
		if len(ids) > 0 {
			featureID = ids[0]
		}
	}
	instanceMaterial := materials.Side
	if instanceMaterial < 0 {
		instanceMaterial = materials.Top
	}
	if len(supportTransforms) > 0 {
		supportPos, supportNormals, supportUV, supportIdx := buildGuardrailSupportMesh(direction, railHeight, railHeightAll)
		supportPrimitive := buildPrimitiveFromGeometry(b.doc, instanceMaterial, supportPos, supportNormals, supportUV, supportIdx)
		addInstancedPrimitiveNode(b.doc, len(b.doc.Meshes), supportPrimitive, "guardrail-three-wave-supports", supportTransforms, featureID)
	}
	return []*gltf.Primitive{primitive}, nil
}

// 构建护栏端头/鼻端区域。
// 根据护栏方向生成单侧三波形平面，并读取 UV 旋转角度，使端头纹理可以按指定方向旋转后铺设。
func (b *SurfaceBuilder) BuildGuardrailNoseEnd(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("guardrail nose end lines are empty")
	}

	materialSet := feature.Material
	if isZeroMaterialRef(materialSet.Top) {
		materialSet.Top = MaterialRef{
			Name:        "guardrail-three-wave-rail",
			Color:       "#B8BDC5",
			Mode:        MaterialModeColor,
			DoubleSided: true,
		}
	}
	materialSet.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("guardrail nose end material is invalid")
	}

	var batchPos [][3]float32
	var batchNormals [][3]float32
	var batchUV [][2]float32
	var batchIdx []uint32

	direction := guardrailDirection(feature.Fields)
	uvRotateDeg := float64(resolveFeatureUVRotateDeg(feature.UV, feature.Fields, 45))
	for _, line := range lines {
		pos, normals, uv, indices, err := buildGuardrailThreeWaveSingleSidedPlaneMeshTiled(line, direction, uvRotateDeg)
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 || len(indices) == 0 {
			continue
		}
		base := uint32(len(batchPos))
		batchPos = append(batchPos, pos...)
		batchNormals = append(batchNormals, normals...)
		batchUV = append(batchUV, uv...)
		for _, idx := range indices {
			batchIdx = append(batchIdx, base+idx)
		}
	}
	if len(batchPos) == 0 || len(batchIdx) == 0 {
		return nil, nil
	}

	primitive := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(b.doc, batchPos),
			gltf.NORMAL:     modeler.WriteNormal(b.doc, batchNormals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(b.doc, batchUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(b.doc, batchIdx)),
		Material: gltf.Index(materials.Top),
	}
	b.attachFeatureMetadata(primitive, len(batchPos), feature.FeatureInput)
	return []*gltf.Primitive{primitive}, nil
}

// 获取某个要素最终使用的 UV 旋转角度。
// 按优先级读取 UVOptions.RotateDeg、uv_rotate_deg、texture_rotate_deg、uv_rotate，全部未配置时使用调用方提供的默认角度。
func resolveFeatureUVRotateDeg(opt UVOptions, fields FeatureFields, defaultDeg float32) float32 {
	if opt.RotateDeg != 0 {
		return opt.RotateDeg
	}
	if deg := featureFieldFloat32(fields, "uv_rotate_deg"); deg != 0 {
		return deg
	}
	if deg := featureFieldFloat32(fields, "texture_rotate_deg"); deg != 0 {
		return deg
	}
	if deg := featureFieldFloat32(fields, "uv_rotate"); deg != 0 {
		return deg
	}
	return defaultDeg
}

// 沿道路线路径批量布置防眩板模型。
// 读取外部模型路径并确保模型 Mesh 已加载，然后根据 spacing、interval 和 startoffset 计算实例位置、旋转和缩放，最后使用 GPU Instancing 创建节点。
func (b *SurfaceBuilder) BuildAntiGlareBoard(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("anti glare board lines are empty")
	}

	modelPath := strings.TrimSpace(featureFieldString(feature.Fields, "model_path"))
	if modelPath == "" {
		return nil, fmt.Errorf("anti glare board model_path is empty")
	}
	meshIndex, err := b.ensureInstancedModelMesh(modelPath)
	if err != nil {
		return nil, fmt.Errorf("import anti glare board model: %w", err)
	}

	spacing := featureFieldFloat32(feature.Fields, "spacing")
	if spacing <= 0 {
		spacing = featureFieldFloat32(feature.Fields, "interval")
	}
	if spacing <= 0 {
		spacing = defaultAntiGlareBoardSpacing
	}
	startOffset := featureFieldFloat32(feature.Fields, "startoffset")

	featureID := uint32(0)
	if b.metadata != nil {
		ids := b.metadata.RegisterFeatureRows(feature.Features, feature.Fields)
		if len(ids) > 0 {
			featureID = ids[0]
		}
	}

	nodeCount := 0
	for i := range lines {
		transforms, err := buildAntiGlareBoardTransforms(lines[i], spacing, startOffset)
		if err != nil {
			return nil, fmt.Errorf("build anti glare board transforms for line %d: %w", i, err)
		}
		if len(transforms) == 0 {
			continue
		}
		addInstancedMeshNode(b.doc, meshIndex, "anti-glare-board", transforms, featureID)
		nodeCount++
	}
	if nodeCount == 0 {
		return nil, nil
	}
	return nil, nil
}

// 创建一个单位 Box，并以实例化方式添加到 glTF 场景。
// 适用于大量尺寸和位置不同、但基础几何完全相同的盒状设施，可避免为每个对象重复保存 Mesh 数据。
func addInstancedBoxNode(doc *gltf.Document, material int, name string, transforms []instancedTransform, featureID uint32) {
	if doc == nil || len(transforms) == 0 || material < 0 {
		return
	}
	meshIndex := len(doc.Meshes)
	primitive := buildUnitBoxPrimitive(doc, material)
	addInstancedPrimitiveNode(doc, meshIndex, primitive, name, transforms, featureID)
}

// 创建一个单位圆柱体并批量实例化。
// 使用固定分段数构造基础圆柱 Primitive，再根据 transforms 中的位移、旋转和缩放生成多个实例。
func addInstancedCylinderNode(doc *gltf.Document, material int, name string, transforms []instancedTransform, featureID uint32) {
	if doc == nil || len(transforms) == 0 || material < 0 {
		return
	}
	meshIndex := len(doc.Meshes)
	primitive := buildUnitCylinderPrimitive(doc, material, 10)
	addInstancedPrimitiveNode(doc, meshIndex, primitive, name, transforms, featureID)
}

// 将给定 Primitive 包装成 Mesh，并创建对应的实例化节点。
// 负责把 Mesh 加入 glTF 文档，然后调用 addInstancedMeshNode 写入实例变换和 Feature ID。
func addInstancedPrimitiveNode(doc *gltf.Document, meshIndex int, primitive *gltf.Primitive, name string, transforms []instancedTransform, featureID uint32) {
	if doc == nil || primitive == nil || len(transforms) == 0 {
		return
	}
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       name,
		Primitives: []*gltf.Primitive{primitive},
	})
	addInstancedMeshNode(doc, meshIndex, name, transforms, featureID)
}

// 根据已有顶点、法线、UV 和三角形索引创建 glTF Primitive。
// 会将几何数组写入 glTF Accessor，并绑定指定材质；若没有有效顶点或索引，则返回 nil。
func buildPrimitiveFromGeometry(doc *gltf.Document, material int, pos [][3]float32, normals [][3]float32, uv [][2]float32, indices []uint32) *gltf.Primitive {
	if len(pos) == 0 || len(indices) == 0 {
		return nil
	}
	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uv),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}
}

// 将一个指定尺寸和中心位置的长方体追加到现有几何缓存。
// 会计算 8 个角点，再为六个面分别追加顶点、法线、UV 和三角形索引，适合批量组合复杂设施模型。
func appendBoxGeometry(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, center [3]float32, sx, sy, sz float32) {
	corners := [][3]float32{
		{-0.5 * sx, -0.5 * sy, -0.5 * sz}, {-0.5 * sx, 0.5 * sy, -0.5 * sz}, {0.5 * sx, 0.5 * sy, -0.5 * sz}, {0.5 * sx, -0.5 * sy, -0.5 * sz},
		{-0.5 * sx, -0.5 * sy, 0.5 * sz}, {-0.5 * sx, 0.5 * sy, 0.5 * sz}, {0.5 * sx, 0.5 * sy, 0.5 * sz}, {0.5 * sx, -0.5 * sy, 0.5 * sz},
	}
	for i := range corners {
		corners[i][0] += center[0]
		corners[i][1] += center[1]
		corners[i][2] += center[2]
	}
	appendQuad(pos, normals, uv, indices, corners[0], corners[1], corners[2], corners[3], [3]float32{0, 0, -1}, [2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0})
	appendQuad(pos, normals, uv, indices, corners[4], corners[7], corners[6], corners[5], [3]float32{0, 0, 1}, [2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0})
	appendQuad(pos, normals, uv, indices, corners[1], corners[5], corners[6], corners[2], [3]float32{0, 1, 0}, [2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0})
	appendQuad(pos, normals, uv, indices, corners[0], corners[3], corners[7], corners[4], [3]float32{0, -1, 0}, [2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0})
	appendQuad(pos, normals, uv, indices, corners[0], corners[4], corners[5], corners[1], [3]float32{-1, 0, 0}, [2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0})
	appendQuad(pos, normals, uv, indices, corners[3], corners[2], corners[6], corners[7], [3]float32{1, 0, 0}, [2]float32{0, 0}, [2]float32{0, 1}, [2]float32{1, 1}, [2]float32{1, 0})
}

// 将一个圆柱体追加到现有几何缓存。
// 根据 segments 生成圆周顶点，同时构建侧面、顶盖和底盖的法线、UV 以及索引，并支持 XYZ 三个方向独立缩放。
func appendCylinderGeometry(pos *[][3]float32, normals *[][3]float32, uv *[][2]float32, indices *[]uint32, center [3]float32, sx, sy, sz float32, segments int) {
	if segments < 6 {
		segments = 6
	}
	base := uint32(len(*pos))
	for i := 0; i < segments; i++ {
		ang := float64(i) * 2 * math.Pi / float64(segments)
		x := float32(math.Cos(ang)) * 0.5 * sx
		z := float32(math.Sin(ang)) * 0.5 * sz
		n := normalize3([3]float32{x, 0, z})
		u := float32(i) / float32(segments)
		*pos = append(*pos,
			[3]float32{center[0] + x, center[1] - 0.5*sy, center[2] + z},
			[3]float32{center[0] + x, center[1] + 0.5*sy, center[2] + z},
		)
		*normals = append(*normals, n, n)
		*uv = append(*uv, [2]float32{u, 1}, [2]float32{u, 0})
	}
	for i := 0; i < segments; i++ {
		a := base + uint32(i*2)
		b := base + uint32((i*2+2)%(segments*2))
		c := a + 1
		d := base + uint32((i*2+3)%(segments*2))
		*indices = append(*indices, a, b, c, c, b, d)
	}

	topCenter := uint32(len(*pos))
	*pos = append(*pos, [3]float32{center[0], center[1] + 0.5*sy, center[2]})
	*normals = append(*normals, [3]float32{0, 1, 0})
	*uv = append(*uv, [2]float32{0.5, 0.5})
	topStart := uint32(len(*pos))
	for i := 0; i < segments; i++ {
		ang := float64(i) * 2 * math.Pi / float64(segments)
		x := float32(math.Cos(ang)) * 0.5 * sx
		z := float32(math.Sin(ang)) * 0.5 * sz
		*pos = append(*pos, [3]float32{center[0] + x, center[1] + 0.5*sy, center[2] + z})
		*normals = append(*normals, [3]float32{0, 1, 0})
		*uv = append(*uv, [2]float32{x/sx + 0.5, z/sz + 0.5})
	}
	for i := 0; i < segments; i++ {
		a := topStart + uint32(i)
		b := topStart + uint32((i+1)%segments)
		*indices = append(*indices, topCenter, a, b)
	}

	bottomCenter := uint32(len(*pos))
	*pos = append(*pos, [3]float32{center[0], center[1] - 0.5*sy, center[2]})
	*normals = append(*normals, [3]float32{0, -1, 0})
	*uv = append(*uv, [2]float32{0.5, 0.5})
	bottomStart := uint32(len(*pos))
	for i := 0; i < segments; i++ {
		ang := float64(i) * 2 * math.Pi / float64(segments)
		x := float32(math.Cos(ang)) * 0.5 * sx
		z := float32(math.Sin(ang)) * 0.5 * sz
		*pos = append(*pos, [3]float32{center[0] + x, center[1] - 0.5*sy, center[2] + z})
		*normals = append(*normals, [3]float32{0, -1, 0})
		*uv = append(*uv, [2]float32{x/sx + 0.5, z/sz + 0.5})
	}
	for i := 0; i < segments; i++ {
		a := bottomStart + uint32(i)
		b := bottomStart + uint32((i+1)%segments)
		*indices = append(*indices, bottomCenter, b, a)
	}
}

// 为现有 Mesh 创建 EXT_mesh_gpu_instancing 实例节点。
// 将每个实例的 TRANSLATION、ROTATION、SCALE 和 _FEATURE_ID_0 写入 Accessor，并挂载 EXT_instance_features 元数据扩展。
func addInstancedMeshNode(doc *gltf.Document, meshIndex int, name string, transforms []instancedTransform, featureID uint32) {
	if doc == nil || len(transforms) == 0 || meshIndex < 0 || meshIndex >= len(doc.Meshes) {
		return
	}
	translations := make([][3]float32, len(transforms))
	rotations := make([][4]float32, len(transforms))
	scales := make([][3]float32, len(transforms))
	featureIDs := make([]uint32, len(transforms))
	for i, tr := range transforms {
		translations[i] = tr.Translation
		rotations[i] = tr.Rotation
		scales[i] = tr.Scale
		featureIDs[i] = featureID
	}

	accT := modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, translations)
	accR := modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, rotations)
	accS := modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, scales)
	accF := modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, featureIDs)

	node := &gltf.Node{
		Name: name,
		Mesh: gltf.Index(meshIndex),
		Extensions: map[string]any{
			"EXT_mesh_gpu_instancing": map[string]any{
				"attributes": map[string]any{
					"TRANSLATION":   accT,
					"ROTATION":      accR,
					"SCALE":         accS,
					"_FEATURE_ID_0": accF,
				},
			},
			"EXT_instance_features": map[string]any{
				"featureIds": []map[string]any{
					{
						"attribute":     0,
						"propertyTable": 0,
					},
				},
			},
		},
	}
	doc.Nodes = append(doc.Nodes, node)
	if len(doc.Scenes) == 0 {
		doc.Scenes = []*gltf.Scene{{Nodes: []int{len(doc.Nodes) - 1}}}
		doc.Scene = gltf.Index(0)
	} else {
		doc.Scenes[0].Nodes = append(doc.Scenes[0].Nodes, len(doc.Nodes)-1)
	}
	ensureExtUsed(doc, "EXT_mesh_gpu_instancing")
	ensureExtUsed(doc, "EXT_instance_features")
}

// 创建中心位于原点、边长为 1 的标准立方体 Primitive。
// 六个面使用独立顶点，以确保每个面的法线和 UV 可以正确表达，之后可通过实例 Scale 得到不同尺寸。
func buildUnitBoxPrimitive(doc *gltf.Document, material int) *gltf.Primitive {
	pos := [][3]float32{
		{-0.5, -0.5, -0.5}, {-0.5, 0.5, -0.5}, {0.5, 0.5, -0.5}, {0.5, -0.5, -0.5},
		{-0.5, -0.5, 0.5}, {-0.5, 0.5, 0.5}, {0.5, 0.5, 0.5}, {0.5, -0.5, 0.5},
		{-0.5, 0.5, -0.5}, {-0.5, 0.5, 0.5}, {0.5, 0.5, 0.5}, {0.5, 0.5, -0.5},
		{-0.5, -0.5, -0.5}, {0.5, -0.5, -0.5}, {0.5, -0.5, 0.5}, {-0.5, -0.5, 0.5},
		{-0.5, -0.5, -0.5}, {-0.5, -0.5, 0.5}, {-0.5, 0.5, 0.5}, {-0.5, 0.5, -0.5},
		{0.5, -0.5, -0.5}, {0.5, 0.5, -0.5}, {0.5, 0.5, 0.5}, {0.5, -0.5, 0.5},
	}
	normals := [][3]float32{
		{0, 0, -1}, {0, 0, -1}, {0, 0, -1}, {0, 0, -1},
		{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1},
		{0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {0, 1, 0},
		{0, -1, 0}, {0, -1, 0}, {0, -1, 0}, {0, -1, 0},
		{-1, 0, 0}, {-1, 0, 0}, {-1, 0, 0}, {-1, 0, 0},
		{1, 0, 0}, {1, 0, 0}, {1, 0, 0}, {1, 0, 0},
	}
	uv := [][2]float32{
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
	}
	indices := []uint32{
		0, 1, 2, 0, 2, 3,
		4, 7, 6, 4, 6, 5,
		8, 9, 10, 8, 10, 11,
		12, 15, 14, 12, 14, 13,
		16, 17, 18, 16, 18, 19,
		20, 23, 22, 20, 22, 21,
	}
	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uv),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}
}

// 创建标准单位圆柱 Primitive。
// 根据 segments 控制圆周精度，同时生成圆柱侧壁、顶盖和底盖；通常用于后续 GPU Instancing。
func buildUnitCylinderPrimitive(doc *gltf.Document, material int, segments int) *gltf.Primitive {
	if segments < 6 {
		segments = 6
	}
	pos := make([][3]float32, 0, segments*4+2)
	normals := make([][3]float32, 0, segments*4+2)
	uv := make([][2]float32, 0, segments*4+2)
	indices := make([]uint32, 0, segments*12)

	for i := 0; i < segments; i++ {
		ang := float64(i) * 2 * math.Pi / float64(segments)
		x := float32(math.Cos(ang) * 0.5)
		z := float32(math.Sin(ang) * 0.5)
		n := normalize3([3]float32{x, 0, z})
		u := float32(i) / float32(segments)
		pos = append(pos, [3]float32{x, -0.5, z}, [3]float32{x, 0.5, z})
		normals = append(normals, n, n)
		uv = append(uv, [2]float32{u, 1}, [2]float32{u, 0})
	}
	for i := 0; i < segments; i++ {
		a := uint32(i * 2)
		b := uint32((i*2 + 2) % (segments * 2))
		c := a + 1
		d := uint32((i*2 + 3) % (segments * 2))
		indices = append(indices, a, b, c, c, b, d)
	}

	topCenter := uint32(len(pos))
	pos = append(pos, [3]float32{0, 0.5, 0})
	normals = append(normals, [3]float32{0, 1, 0})
	uv = append(uv, [2]float32{0.5, 0.5})
	bottomCenter := uint32(len(pos))
	pos = append(pos, [3]float32{0, -0.5, 0})
	normals = append(normals, [3]float32{0, -1, 0})
	uv = append(uv, [2]float32{0.5, 0.5})

	topStart := uint32(len(pos))
	for i := 0; i < segments; i++ {
		ang := float64(i) * 2 * math.Pi / float64(segments)
		x := float32(math.Cos(ang) * 0.5)
		z := float32(math.Sin(ang) * 0.5)
		pos = append(pos, [3]float32{x, 0.5, z})
		normals = append(normals, [3]float32{0, 1, 0})
		uv = append(uv, [2]float32{x + 0.5, z + 0.5})
	}
	for i := 0; i < segments; i++ {
		a := topStart + uint32(i)
		b := topStart + uint32((i+1)%segments)
		indices = append(indices, topCenter, a, b)
	}

	bottomStart := uint32(len(pos))
	for i := 0; i < segments; i++ {
		ang := float64(i) * 2 * math.Pi / float64(segments)
		x := float32(math.Cos(ang) * 0.5)
		z := float32(math.Sin(ang) * 0.5)
		pos = append(pos, [3]float32{x, -0.5, z})
		normals = append(normals, [3]float32{0, -1, 0})
		uv = append(uv, [2]float32{x + 0.5, z + 0.5})
	}
	for i := 0; i < segments; i++ {
		a := bottomStart + uint32(i)
		b := bottomStart + uint32((i+1)%segments)
		indices = append(indices, bottomCenter, b, a)
	}

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uv),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}
}

// 构建道路顶面并将其缓存到 RoadSurfaceProjector。
// 会根据道路 ID 获取中心线，选择合适的三角化策略生成道路 Mesh，同时按米制尺度生成 UV，并保存三角网供后续标线、路面符号投影使用。
func (b *SurfaceBuilder) BuildRoadSurface(feature SurfaceFeature, rings []LocalRings) ([]*gltf.Primitive, error) {
	meshes, err := b.BuildRoadSurfaceMeshes(feature, rings)
	if err != nil {
		return nil, err
	}
	roadID := featureFieldInt64(feature.Fields, "road_id")
	for _, mesh := range meshes {
		if b.projector != nil {
			b.projector.AddRoadSurface(roadID, mesh.Positions, mesh.Indices)
		}
	}
	return b.WriteRoadSurfaceMeshes(feature, meshes)
}

// 构建道路路基表面。
// 对每个路基多边形优先进行经过清洗和验证的三角化，再生成平面材质 Primitive，并根据 RepeatX/RepeatY 使用米制纹理坐标。
func (b *SurfaceBuilder) BuildRoadSubgrade(feature SurfaceFeature, rings []LocalRings) ([]*gltf.Primitive, error) {
	if len(rings) == 0 {
		return nil, fmt.Errorf("road subgrade rings are empty")
	}

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}

	repeatMetersX := feature.UV.RepeatX
	repeatMetersY := feature.UV.RepeatY
	if repeatMetersX <= 0 {
		repeatMetersX = 8
	}
	if repeatMetersY <= 0 {
		repeatMetersY = repeatMetersX
	}

	primitives := make([]*gltf.Primitive, 0, len(rings))
	for _, rawRing := range rings {
		pos, indices, err := triangulatePreferredSubgradeSurfaceRing(rawRing)
		if err != nil {
			return nil, err
		}
		top, err := buildPlanarSurfacePrimitiveMeters(b.doc, pos, indices, materials.Top, repeatMetersX, repeatMetersY, feature.UV.FlipV)
		if err != nil {
			return nil, err
		}
		b.attachFeatureMetadata(top, len(pos), feature.FeatureInput)
		primitives = append(primitives, top)
	}
	return primitives, nil
}

// 为路基选择更可靠的三角化方案。
// 如果原始外环有效且没有自交，会优先直接三角化；失败后再清洗重复点、异常点和几何问题，然后重新尝试。
func triangulatePreferredSubgradeSurfaceRing(rawRing LocalRings) ([][3]float32, []uint32, error) {
	rawOuter := dedupeClosedRing(rawRing.Outer)
	if len(rawOuter) >= 3 && !hasSelfIntersectionXZ(rawOuter) {
		if pos, idx, err := triangulateSubgradeSurfaceRing(rawRing); err == nil && len(idx) >= 3 {
			return pos, idx, nil
		}
	}
	cleanRing := cleanSubgradeLocalRings(rawRing, 0.01)
	return triangulateSubgradeSurfaceRing(cleanRing)
}

// 对路基外环和所有孔洞进行统一清洗。
// 外环要求逆时针方向，孔洞要求相反方向，并调用 sanitizeSubgradeRing 处理重复点、共线点、异常高程和几何方向。
func cleanSubgradeLocalRings(rings LocalRings, minEdge float64) LocalRings {
	const collinearTol = 0.02 // meters
	out := LocalRings{
		Outer: sanitizeSubgradeRing(rings.Outer, minEdge, collinearTol, true),
		Holes: make([][][3]float32, 0, len(rings.Holes)),
	}
	for _, h := range rings.Holes {
		out.Holes = append(out.Holes, sanitizeSubgradeRing(h, minEdge, collinearTol, false))
	}
	return out
}

// 对单个路基 Ring 进行几何预处理。
// 包括去除近距离重复点、安全删除近共线点、控制简化前后面积变化、统一环方向，并尽量避免产生新的自相交。
func sanitizeSubgradeRing(points [][3]float32, minEdge, collinearTol float64, wantCCW bool) [][3]float32 {
	if len(points) < 4 {
		return points
	}
	closed := len(points) > 1 && points[0][0] == points[len(points)-1][0] && points[0][2] == points[len(points)-1][2]
	ring := points
	if closed {
		ring = points[:len(points)-1]
	}
	if len(ring) < 3 {
		return points
	}

	// 1) De-dup exact/near points
	dedup := make([][3]float32, 0, len(ring))
	dedup = append(dedup, ring[0])
	for i := 1; i < len(ring); i++ {
		if segmentLengthXZ(dedup[len(dedup)-1], ring[i]) < minEdge {
			continue
		}
		dedup = append(dedup, ring[i])
	}
	if len(dedup) < 3 {
		dedup = append([][3]float32(nil), ring...)
	}

	// 2) Remove nearly collinear points, but never accept a simplification that
	// creates self-intersections or blows up the polygon area. Long, narrow roadbed
	// rings are very sensitive to over-simplification here.
	simplified := chooseSafeSubgradeSimplification(dedup, minEdge, collinearTol)

	// 4) Ensure ring orientation
	area := polygonSignedAreaXZ(simplified)
	if wantCCW && area < 0 {
		simplified = reversePoints(simplified)
	}
	if !wantCCW && area > 0 {
		simplified = reversePoints(simplified)
	}

	// 5) Self-intersection check; if dirty, keep simplified but avoid extra edits.
	if hasSelfIntersectionXZ(simplified) {
		// Keep geometry as-is after sanitation steps; downstream triangulation fallback will handle it.
	}

	if closed {
		simplified = append(simplified, simplified[0])
	}
	return simplified
}

// 在多个容差级别中寻找安全的路基简化结果。
// 会逐级降低共线点删除阈值，并检查顶点数量、自交情况和面积变化，只接受不会明显改变原始几何形状的候选结果。
func chooseSafeSubgradeSimplification(points [][3]float32, minEdge, collinearTol float64) [][3]float32 {
	if len(points) < 3 {
		return points
	}
	base := append([][3]float32(nil), points...)
	base = removeSubgradeOutlierPoints(base, minEdge)
	if len(base) < 3 {
		base = append([][3]float32(nil), points...)
	}
	baseArea := math.Abs(polygonSignedAreaXZ(base))
	if baseArea <= 1e-6 {
		baseArea = math.Abs(polygonSignedAreaXZ(points))
	}

	tols := []float64{collinearTol, collinearTol * 0.5, 0.005, 0.002, 0.001, 0.0005, 0.0002, 0}
	seen := map[int]bool{}
	for _, tol := range tols {
		key := int(math.Round(tol * 1e6))
		if seen[key] {
			continue
		}
		seen[key] = true
		candidate := simplifySubgradeRingCollinear(base, tol)
		if len(candidate) < 3 {
			continue
		}
		candidate = removeSubgradeOutlierPoints(candidate, minEdge)
		if len(candidate) < 3 {
			continue
		}
		if hasSelfIntersectionXZ(candidate) {
			continue
		}
		area := math.Abs(polygonSignedAreaXZ(candidate))
		if baseArea > 1e-6 {
			ratio := area / baseArea
			if ratio < 0.70 || ratio > 1.30 {
				continue
			}
		}
		return candidate
	}
	return base
}

// 删除 Ring 中近似共线的中间点。
// 通过前一点、当前点和后一点在 XZ 平面的偏离程度判断是否可删除，从而降低不必要的顶点数量。
func simplifySubgradeRingCollinear(points [][3]float32, tol float64) [][3]float32 {
	if len(points) < 3 || tol <= 0 {
		return append([][3]float32(nil), points...)
	}
	out := make([][3]float32, 0, len(points))
	for i := 0; i < len(points); i++ {
		prev := points[(i-1+len(points))%len(points)]
		cur := points[i]
		next := points[(i+1)%len(points)]
		if nearlyCollinearXZ(prev, cur, next, tol) && len(points) > 3 {
			continue
		}
		out = append(out, cur)
	}
	if len(out) < 3 {
		return append([][3]float32(nil), points...)
	}
	return out
}

// 删除路基 Ring 中可能存在的异常高程采样点。
// 重点检测短距离内出现过大纵坡且明显偏离相邻点线性插值的尖峰，并通过多轮处理逐步剔除异常值。
func removeSubgradeOutlierPoints(points [][3]float32, minEdge float64) [][3]float32 {
	if len(points) < 6 {
		return points
	}
	// Short segments with steep local grade are usually bad samples.
	shortEdge := math.Max(minEdge*2.0, 0.08)
	gradeThresh := 0.30
	deviationThresh := 0.10

	cur := append([][3]float32(nil), points...)
	for pass := 0; pass < 3; pass++ {
		if len(cur) < 6 {
			break
		}
		changed := false
		out := make([][3]float32, 0, len(cur))
		for i := 0; i < len(cur); i++ {
			a := cur[(i-1+len(cur))%len(cur)]
			b := cur[i]
			c := cur[(i+1)%len(cur)]
			l1 := segmentLengthXZ(a, b)
			l2 := segmentLengthXZ(b, c)
			lac := segmentLengthXZ(a, c)
			if l1 <= 0 || l2 <= 0 || lac <= 0 {
				continue
			}
			g1 := math.Abs(float64(b[1]-a[1])) / l1
			g2 := math.Abs(float64(c[1]-b[1])) / l2
			// Compare current point elevation to linear interpolation on AC.
			t := l1 / (l1 + l2)
			interpY := float64(a[1]) + (float64(c[1])-float64(a[1]))*t
			dev := math.Abs(float64(b[1]) - interpY)

			isSpike := ((l1 < shortEdge && g1 > gradeThresh) || (l2 < shortEdge && g2 > gradeThresh)) && dev > deviationThresh
			if isSpike && len(cur)-1 >= 3 {
				changed = true
				continue
			}
			out = append(out, b)
		}
		if len(out) < 3 {
			return points
		}
		cur = out
		if !changed {
			break
		}
	}
	return cur
}

// 判断三个三维点投影到 XZ 平面后是否近似共线。
// 使用二维叉积相对于两段长度的比例与指定容差比较，极短线段会直接认为是共线情况。
func nearlyCollinearXZ(a, b, c [3]float32, tol float64) bool {
	abx := float64(b[0] - a[0])
	abz := float64(b[2] - a[2])
	bcx := float64(c[0] - b[0])
	bcz := float64(c[2] - b[2])
	l1 := math.Hypot(abx, abz)
	l2 := math.Hypot(bcx, bcz)
	if l1 < 1e-9 || l2 < 1e-9 {
		return true
	}
	cross := math.Abs(abx*bcz - abz*bcx)
	return cross/(l1+l2) < tol
}

// 计算多边形在 XZ 平面的有符号面积。
// 面积绝对值表示多边形大小，符号可用于判断环的顺时针/逆时针方向。
func polygonSignedAreaXZ(points [][3]float32) float64 {
	if len(points) < 3 {
		return 0
	}
	var area float64
	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		area += float64(points[i][0])*float64(points[j][2]) - float64(points[j][0])*float64(points[i][2])
	}
	return area * 0.5
}

// 检查一个闭合多边形是否存在自相交。
// 遍历所有非相邻边，并使用二维线段相交测试判断 Ring 是否出现交叉。
func hasSelfIntersectionXZ(points [][3]float32) bool {
	n := len(points)
	if n < 4 {
		return false
	}
	for i := 0; i < n; i++ {
		a1 := points[i]
		a2 := points[(i+1)%n]
		for j := i + 1; j < n; j++ {
			if j == i || j == (i+1)%n || (i == 0 && j == n-1) {
				continue
			}
			b1 := points[j]
			b2 := points[(j+1)%n]
			if segmentsIntersectXZ(a1, a2, b1, b2) {
				return true
			}
		}
	}
	return false
}

// 判断两条线段在 XZ 平面是否相交。
// 同时处理普通交叉以及共线端点落在线段上的特殊情况。
func segmentsIntersectXZ(a1, a2, b1, b2 [3]float32) bool {
	o1 := orientXZ(a1, a2, b1)
	o2 := orientXZ(a1, a2, b2)
	o3 := orientXZ(b1, b2, a1)
	o4 := orientXZ(b1, b2, a2)
	const eps = 1e-9
	if math.Abs(o1) < eps && onSegmentXZ(a1, a2, b1) {
		return true
	}
	if math.Abs(o2) < eps && onSegmentXZ(a1, a2, b2) {
		return true
	}
	if math.Abs(o3) < eps && onSegmentXZ(b1, b2, a1) {
		return true
	}
	if math.Abs(o4) < eps && onSegmentXZ(b1, b2, a2) {
		return true
	}
	return (o1 > 0) != (o2 > 0) && (o3 > 0) != (o4 > 0)
}

// 计算三个点在 XZ 平面上的有向叉积。
// 结果的正负可以判断第三点位于有向线段的哪一侧，也是线段相交和方向判断的基础函数。
func orientXZ(a, b, c [3]float32) float64 {
	return float64(b[0]-a[0])*float64(c[2]-a[2]) - float64(b[2]-a[2])*float64(c[0]-a[0])
}

// 判断点 p 是否位于线段 a-b 的 XZ 包围范围内。
// 通常与 orientXZ 的共线判断一起使用，用于处理线段端点接触和共线重叠。
func onSegmentXZ(a, b, p [3]float32) bool {
	return float64(minFloat32(a[0], b[0])) <= float64(p[0]) &&
		float64(p[0]) <= float64(maxFloat32(a[0], b[0])) &&
		float64(minFloat32(a[2], b[2])) <= float64(p[2]) &&
		float64(p[2]) <= float64(maxFloat32(a[2], b[2]))
}

// 返回两个 float32 数值中的较大值。
func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// 构建道路绿化带的顶部和侧壁几何。
// 先按照指定高度整体抬升原始多边形，再生成顶部三角网以及上下环之间的垂直侧壁，并分别应用顶部和侧面材质。
func (b *SurfaceBuilder) BuildRoadGreenbelt(feature SurfaceFeature, rings []LocalRings) ([]*gltf.Primitive, error) {
	if len(rings) == 0 {
		return nil, fmt.Errorf("road greenbelt rings are empty")
	}

	height := feature.Height
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "hight")
	}
	if height <= 0 {
		height = 0.3
	}

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	materialSet.Side.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road greenbelt top material is invalid")
	}
	sideMaterial := materials.Side
	if sideMaterial < 0 {
		sideMaterial = materials.Top
	}

	repeatMetersX := feature.UV.RepeatX
	repeatMetersY := feature.UV.RepeatY
	if repeatMetersX <= 0 {
		repeatMetersX = 4
	}
	if repeatMetersY <= 0 {
		repeatMetersY = repeatMetersX
	}

	out := make([]*gltf.Primitive, 0, len(rings)*2)
	for _, ring := range rings {
		raised := raiseLocalRings(ring, height)
		prs, err := buildGreenbeltPrimitivesMeters(b.doc, raised, height, materials.Top, sideMaterial, repeatMetersX, repeatMetersY, feature.UV.FlipV)
		if err != nil {
			return nil, err
		}
		for _, pr := range prs {
			if pr == nil {
				continue
			}
			if materials.Top >= 0 {
				vertexCount := primitiveVertexCount(b.doc, pr)
				b.attachFeatureMetadata(pr, vertexCount, feature.FeatureInput)
			}
			out = append(out, pr)
		}
	}
	return out, nil
}

// 构建面状道路标记，例如箭头、文字底面或其他路面符号。
// 首先将多边形三角化，然后尝试投影到关联道路表面并略微抬高，避免 Z-Fighting，最后生成带 UV 和元数据的 Primitive。
func (b *SurfaceBuilder) BuildRoadMarkSurface(feature SurfaceFeature, rings []LocalRings) ([]*gltf.Primitive, error) {
	if len(rings) == 0 {
		return nil, fmt.Errorf("road mark rings are empty")
	}

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road mark top material is invalid")
	}

	roadID := featureFieldInt64(feature.Fields, "road_id")
	repeatMetersX := feature.UV.RepeatX
	repeatMetersY := feature.UV.RepeatY
	if repeatMetersX <= 0 {
		repeatMetersX = 1
	}
	if repeatMetersY <= 0 {
		repeatMetersY = repeatMetersX
	}

	out := make([]*gltf.Primitive, 0, len(rings))
	for _, ring := range rings {
		pos, indices, err := triangulatePlanarPolygonSurfaceRing(ring)
		if err != nil {
			return nil, err
		}
		if b.projector != nil {
			projected, ok := b.projector.ProjectPoints(roadID, pos)
			if ok {
				pos = projected
				for i := range pos {
					pos[i][1] += 0.05
				}
			}
		}
		top, err := buildPlanarSurfacePrimitiveMeters(b.doc, pos, indices, materials.Top, repeatMetersX, repeatMetersY, feature.UV.FlipV)
		if err != nil {
			return nil, err
		}
		b.attachFeatureMetadata(top, len(pos), feature.FeatureInput)
		out = append(out, top)
	}
	return out, nil
}

// 将业务 Feature 信息绑定到生成的 glTF Primitive。
// 注册 Feature Row，写入 Feature ID 属性，同时在 Extras 中保存 batchType，使渲染结果可以反查原始业务对象。
func (b *SurfaceBuilder) attachFeatureMetadata(primitive *gltf.Primitive, vertexCount int, feature FeatureInput) {
	if b == nil || b.metadata == nil || primitive == nil || vertexCount <= 0 {
		return
	}
	featureIDs := b.metadata.RegisterFeatureRows(feature.Features, feature.Fields)
	if len(featureIDs) == 0 {
		return
	}
	b.metadata.AttachPrimitiveFeatureID(primitive, vertexCount, featureIDs[0])
	extras, _ := primitive.Extras.(map[string]any)
	if extras == nil {
		extras = map[string]any{}
	}
	extras["batchType"] = featureBatchType(feature)
	primitive.Extras = extras
}

// 获取 Feature 对应的业务类型。
// 优先读取 Features 第一条记录中的 type，其次读取 Fields["type"]，仍不存在时回退为 BuildType。
func featureBatchType(feature FeatureInput) string {
	if len(feature.Features) > 0 {
		if v, ok := feature.Features[0]["type"]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	if feature.Fields != nil {
		if v, ok := feature.Fields["type"]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return string(feature.BuildType)
}

// 为道路多边形选择最终的三角化算法。
// 优先尝试经过验证的 CDT；如果存在中心线则尝试中心线三角化，没有中心线时先平滑高程；之后依次尝试估算中心线、Poly2Tri 和 Earcut 作为回退。
func triangulateRoadSurfaceRing(ring LocalRings, centerline LocalLine, hasCenterline bool) ([][3]float32, []uint32, error) {
	if pos, indices, ok := triangulateRoadSurfaceCDTCandidate(ring); ok {
		return pos, indices, nil
	}
	//fmt.Println("hasCenterline!!!")

	if hasCenterline {
		if pos, indices, ok := triangulateRoadSurfaceByCenterline(ring, centerline); ok {
			return pos, indices, nil
		}
	} else {
		smoothed := smoothRoadSurfaceRingHeights(ring)
		if pos, indices, ok := triangulateRoadSurfaceCDTCandidate(smoothed); ok {
			return pos, indices, nil
		}
	}
	if pos, indices, ok := triangulateRoadSurfaceWithEstimatedCenterline(ring); ok {
		return pos, indices, nil
	}
	if pos, indices, ok := triangulateSubgradeByPoly2Tri(ring); ok {
		return pos, indices, nil
	}
	polygon := make([][][3]float32, 0, 1+len(ring.Holes))
	polygon = append(polygon, ring.Outer)
	polygon = append(polygon, ring.Holes...)
	return mgltf.EarcutXZRings(polygon)
}

// 尝试使用 CDT 对道路表面进行三角化并验证结果。
// 除了检查算法是否成功，还会比较三角形总面积与原多边形面积，并确保生成三角形没有明显超出道路边界。
func triangulateRoadSurfaceCDTCandidate(ring LocalRings) ([][3]float32, []uint32, bool) {
	pos, idx, ok, err := triangulateSurfaceByCDTExperimental(ring)
	if err != nil || !ok || len(idx) < 3 {
		return nil, nil, false
	}
	polyArea := polygonAreaXZ(ring.Outer)
	triArea := trianglesAreaXZ(pos, idx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false
	}
	ratio := triArea / polyArea
	if ratio < 0.95 || ratio > 1.05 {
		return nil, nil, false
	}
	if !trianglesFitPolygonXZ(ring.Outer, pos, idx) {
		return nil, nil, false
	}
	return pos, idx, true
}

// 在没有可靠道路中心线时自动估计中心线并进行三角化。
// 仅针对没有孔洞的道路多边形，先从外轮廓估计中心轴，再尝试基于中心线采样的道路网格生成方法。
func triangulateRoadSurfaceWithEstimatedCenterline(ring LocalRings) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 {
		return nil, nil, false
	}
	cl, ok := estimateLocalCenterlineFromOuterWithMinRatio(ring.Outer, 1.2)
	if !ok || len(cl.Points) < 3 {
		return nil, nil, false
	}
	if pos, idx, ok := triangulateRoadSurfaceEstimatedCandidate(ring, cl); ok {
		return pos, idx, true
	}
	return triangulateRoadSurfaceByCenterline(ring, cl)
}

// 验证使用估算中心线生成的道路三角网。
// 检查生成是否成功、面积误差是否在允许范围内，以及所有三角形是否合理落在道路多边形内部。
func triangulateRoadSurfaceEstimatedCandidate(ring LocalRings, centerline LocalLine) ([][3]float32, []uint32, bool) {
	pos, idx, ok := triangulateSurfaceByCenterlineSampling(ring, centerline)
	if !ok {
		return nil, nil, false
	}
	polyArea := polygonAreaXZ(ring.Outer)
	triArea := trianglesAreaXZ(pos, idx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false
	}
	ratio := triArea / polyArea
	if ratio < 0.95 || ratio > 1.05 {
		return nil, nil, false
	}
	if !trianglesFitPolygonXZ(ring.Outer, pos, idx) {
		return nil, nil, false
	}
	return pos, idx, true
}

// 使用通用 Earcut 方法三角化多边形。
// 将 Outer 和 Holes 组织成 polygon rings 后直接交给 mgltf.EarcutXZRings 处理，是多个高级算法失败后的基础回退方案。
func triangulatePlainSurfaceRing(ring LocalRings) ([][3]float32, []uint32, error) {
	polygon := make([][][3]float32, 0, 1+len(ring.Holes))
	polygon = append(polygon, ring.Outer)
	polygon = append(polygon, ring.Holes...)
	return mgltf.EarcutXZRings(polygon)
}

// 对道路边界的 Y 高程进行平滑。
// 外环采用稍大的平滑窗口，孔洞采用较小窗口，主要用于减少原始道路边界高程抖动对三角网的影响。
func smoothRoadSurfaceRingHeights(ring LocalRings) LocalRings {
	out := LocalRings{
		Outer: smoothClosedRingHeights(ring.Outer, 2, 1),
		Holes: make([][][3]float32, 0, len(ring.Holes)),
	}
	for _, hole := range ring.Holes {
		out.Holes = append(out.Holes, smoothClosedRingHeights(hole, 1, 1))
	}
	return out
}

// 对闭合 Ring 的高程值执行循环平滑。
// 只修改 Y 坐标，X/Z 平面形状保持不变；处理结束后重新补上首尾闭合点。
func smoothClosedRingHeights(points [][3]float32, radius, passes int) [][3]float32 {
	ring := dedupeClosedRing(points)
	if len(ring) < 4 || radius <= 0 || passes <= 0 {
		return points
	}
	ys := make([]float32, len(ring))
	for i := range ring {
		ys[i] = ring[i][1]
	}
	smoothCircularScalar(ys, radius, passes)
	out := make([][3]float32, len(ring)+1)
	for i := range ring {
		out[i] = [3]float32{ring[i][0], ys[i], ring[i][2]}
	}
	out[len(ring)] = out[0]
	return out
}

// 对循环数组执行指定次数的滑动平均。
// 索引在数组首尾之间循环连接，因此适用于闭合多边形的高程等周期数据。
func smoothCircularScalar(values []float32, radius, passes int) {
	if len(values) < 3 || radius <= 0 || passes <= 0 {
		return
	}
	tmp := make([]float32, len(values))
	n := len(values)
	for p := 0; p < passes; p++ {
		copy(tmp, values)
		for i := 0; i < n; i++ {
			var sum float32
			var cnt float32
			for j := -radius; j <= radius; j++ {
				idx := (i + j) % n
				if idx < 0 {
					idx += n
				}
				sum += tmp[idx]
				cnt++
			}
			values[i] = sum / cnt
		}
	}
}

// 暴露普通多边形三角化函数供单元测试调用。
// 实际逻辑完全委托给 triangulatePlainSurfaceRing。
func TriangulatePlainSurfaceForTest(ring LocalRings) ([][3]float32, []uint32, error) {
	return triangulatePlainSurfaceRing(ring)
}

// 暴露路基 Ring 清洗逻辑供测试使用。
// 便于直接验证异常点、共线点和环方向处理结果。
func CleanSubgradeLocalRingsForTest(rings LocalRings, minEdge float64) LocalRings {
	return cleanSubgradeLocalRings(rings, minEdge)
}

// 为路基面选择三角化策略。
// 首先尝试经过面积和边界验证的 CDT，然后尝试 Poly2Tri，最后使用普通 Earcut 作为兜底。
func triangulateSubgradeSurfaceRing(ring LocalRings) ([][3]float32, []uint32, error) {
	if pos, idx, ok := triangulateSubgradeSurfaceCDTCandidate(ring); ok {
		return pos, idx, nil
	}
	if pos, idx, ok := triangulateSubgradeByPoly2Tri(ring); ok {
		return pos, idx, nil
	}
	return triangulatePlainSurfaceRing(ring)
}

// 使用 CDT 尝试生成路基三角网。
// 会检查算法结果、面积比例以及三角形是否落在外多边形范围内，不满足质量要求则返回失败。
func triangulateSubgradeSurfaceCDTCandidate(ring LocalRings) ([][3]float32, []uint32, bool) {
	pos, idx, ok, err := triangulateSurfaceByCDTExperimental(ring)
	if err != nil || !ok || len(idx) < 3 {
		return nil, nil, false
	}
	polyArea := polygonAreaXZ(ring.Outer)
	triArea := trianglesAreaXZ(pos, idx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false
	}
	ratio := triArea / polyArea
	if ratio < 0.90 || ratio > 1.10 {
		return nil, nil, false
	}
	if !trianglesFitPolygonXZ(ring.Outer, pos, idx) {
		return nil, nil, false
	}
	return pos, idx, true
}

// 为普通平面多边形选择三角化算法。
// 优先使用验证后的 CDT，其次尝试 Poly2Tri，仍失败时回退到 Earcut。
func triangulatePlanarPolygonSurfaceRing(ring LocalRings) ([][3]float32, []uint32, error) {
	if pos, idx, ok := triangulatePlanarPolygonSurfaceCDTCandidate(ring); ok {
		return pos, idx, nil
	}
	if pos, idx, ok := triangulateSubgradeByPoly2Tri(ring); ok {
		return pos, idx, nil
	}
	return triangulatePlainSurfaceRing(ring)
}

// 尝试用 CDT 三角化普通平面多边形并校验结果。
// 会检查三角形面积和原多边形面积的比例；对于没有孔洞的多边形，还会额外检查三角形是否越界。
func triangulatePlanarPolygonSurfaceCDTCandidate(ring LocalRings) ([][3]float32, []uint32, bool) {
	pos, idx, ok, err := triangulateSurfaceByCDTExperimental(ring)
	if err != nil || !ok || len(idx) < 3 {
		return nil, nil, false
	}
	polyArea := polygonAreaXZ(ring.Outer)
	triArea := trianglesAreaXZ(pos, idx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false
	}
	ratio := triArea / polyArea
	if ratio < 0.90 || ratio > 1.10 {
		return nil, nil, false
	}
	if len(ring.Holes) == 0 && !trianglesFitPolygonXZ(ring.Outer, pos, idx) {
		return nil, nil, false
	}
	return pos, idx, true
}

// 尝试利用给定中心线对路基进行定向三角化。
// 如果中心线方案未通过验证，则直接回退到普通多边形三角化。
func triangulateSubgradeSurfaceWithCenterline(ring LocalRings, centerline LocalLine) ([][3]float32, []uint32, error) {
	if pos, idx, ok := triangulateSubgradeSurfaceCandidate(ring, centerline); ok {
		return pos, idx, nil
	}
	return triangulatePlainSurfaceRing(ring)
}

// 基于中心线采样生成路基三角网候选。
// 对结果进行面积比例和边界适配检查，只有满足质量要求时才认为候选结果有效。
func triangulateSubgradeSurfaceCandidate(ring LocalRings, centerline LocalLine) ([][3]float32, []uint32, bool) {
	pos, idx, ok := triangulateSurfaceByCenterlineSampling(ring, centerline)
	if !ok {
		return nil, nil, false
	}
	polyArea := polygonAreaXZ(ring.Outer)
	triArea := trianglesAreaXZ(pos, idx)
	if polyArea <= 0 || triArea <= 0 {
		return nil, nil, false
	}
	ratio := triArea / polyArea
	if ratio < 0.9 || ratio > 1.1 {
		return nil, nil, false
	}
	if !trianglesFitPolygonXZ(ring.Outer, pos, idx) {
		return nil, nil, false
	}
	return pos, idx, true
}

// 根据路基外轮廓估算主方向和中心线后进行定向三角化。
// 会拆分中心线两侧边界并构建对应三角带，之后再通过边翻转优化三角形质量。
func triangulateSubgradeDirectionalByPCA(ring LocalRings) ([][3]float32, []uint32, bool) {
	if len(ring.Holes) > 0 {
		return nil, nil, false
	}
	outer := dedupeClosedRing(ring.Outer)
	if len(outer) < 6 {
		return nil, nil, false
	}
	cl, ok := estimateLocalCenterlineFromOuterWithMinRatio(outer, 1.2)
	if !ok || len(cl.Points) < 3 {
		return nil, nil, false
	}
	left, right, ok := splitRoadBoundaryByCenterline(outer, cl)
	if !ok {
		return nil, nil, false
	}
	pos, idx, ok := triangulateBoundaryChains(left, right, cl.Points)
	if !ok || len(idx) < 3 {
		return nil, nil, false
	}
	idx = optimizeTrianglesXZ(pos, idx)
	return pos, idx, true
}

// 根据最大允许线段长度对折线进行加密。
// 当两个相邻点距离超过 maxSeg 时，在两者之间均匀插入额外点，最终再去除重复点。
func densifyPolylineByMaxSeg(points [][3]float32, maxSeg float64) [][3]float32 {
	if len(points) < 2 || maxSeg <= 0 {
		return points
	}
	out := make([][3]float32, 0, len(points)*2)
	for i := 0; i < len(points)-1; i++ {
		a := points[i]
		b := points[i+1]
		out = append(out, a)
		dist := segmentLengthXZ(a, b)
		if dist <= maxSeg {
			continue
		}
		n := int(math.Ceil(dist/maxSeg)) - 1
		for k := 1; k <= n; k++ {
			t := float32(float64(k) / float64(n+1))
			out = append(out, interpolatePoint3(a, b, t))
		}
	}
	out = append(out, points[len(points)-1])
	return dedupeLinePoints(out)
}

// 暴露米制 UV 平面 Primitive 构建逻辑供测试代码直接调用。
func BuildPlanarSurfacePrimitiveMetersForTest(doc *gltf.Document, pos [][3]float32, indices []uint32, material int, meterX, meterY float32, flipV bool) (*gltf.Primitive, error) {
	return buildPlanarSurfacePrimitiveMeters(doc, pos, indices, material, meterX, meterY, flipV)
}

// 通过局部边翻转改善三角网质量。
// 对拥有两个相邻三角形的公共边进行评估，如果翻转后四边形仍凸且最小三角形角度增大，则使用新的对角线重新连接。
func optimizeTrianglesXZ(pos [][3]float32, indices []uint32) []uint32 {
	if len(indices) < 6 {
		return indices
	}
	out := append([]uint32(nil), indices...)
	maxIter := len(out)
	if maxIter < 16 {
		maxIter = 16
	}
	for iter := 0; iter < maxIter; iter++ {
		changed := false
		edgeMap := buildTriangleEdgeMap(out)
		for edge, tris := range edgeMap {
			if len(tris) != 2 {
				continue
			}
			t1 := tris[0]
			t2 := tris[1]
			a, b, ok1 := triangleThirdVertex(out, t1, edge[0], edge[1])
			c, d, ok2 := triangleThirdVertex(out, t2, edge[0], edge[1])
			if !ok1 || !ok2 {
				continue
			}
			_ = b
			_ = d
			if a == c || a == edge[0] || a == edge[1] || c == edge[0] || c == edge[1] {
				continue
			}
			if !isConvexQuadXZ(pos[a], pos[edge[0]], pos[c], pos[edge[1]]) {
				continue
			}

			before := minTriangleAnglePairXZ(pos[a], pos[edge[0]], pos[edge[1]], pos[c])
			after := minTriangleAnglePairXZ(pos[a], pos[c], pos[edge[0]], pos[edge[1]])
			if after <= before+1e-4 {
				continue
			}

			writeTrianglePreserveWinding(out, t1, a, c, edge[0], pos)
			writeTrianglePreserveWinding(out, t2, c, a, edge[1], pos)
			changed = true
		}
		if !changed {
			break
		}
	}
	return out
}

// 建立“三角网边 -> 使用该边的三角形”映射。
// 将每条边按较小顶点索引在前的方式标准化，可快速找出恰好被两个三角形共享的内部边。
func buildTriangleEdgeMap(indices []uint32) map[[2]uint32][]int {
	edgeMap := make(map[[2]uint32][]int, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := indices[i], indices[i+1], indices[i+2]
		for _, e := range [][2]uint32{{a, b}, {b, c}, {c, a}} {
			if e[0] > e[1] {
				e[0], e[1] = e[1], e[0]
			}
			edgeMap[e] = append(edgeMap[e], i)
		}
	}
	return edgeMap
}

// 在指定三角形中查找不属于给定公共边的第三个顶点。
// 同时确认该三角形确实包含公共边的两个端点。
func triangleThirdVertex(indices []uint32, triStart int, e0, e1 uint32) (uint32, uint32, bool) {
	a, b, c := indices[triStart], indices[triStart+1], indices[triStart+2]
	verts := [3]uint32{a, b, c}
	found0, found1 := false, false
	var other uint32
	for _, v := range verts {
		if v == e0 {
			found0 = true
			continue
		}
		if v == e1 {
			found1 = true
			continue
		}
		other = v
	}
	return other, 0, found0 && found1
}

// 判断四个顶点在 XZ 平面组成的四边形是否凸。
// 比较四个连续转角的叉积符号，如果同时出现明显正值和负值，则认为四边形不是凸多边形。
func isConvexQuadXZ(a, b, c, d [3]float32) bool {
	signs := [4]float64{
		cross2DXZ(a, b, c),
		cross2DXZ(b, c, d),
		cross2DXZ(c, d, a),
		cross2DXZ(d, a, b),
	}
	hasPos, hasNeg := false, false
	for _, s := range signs {
		if s > 1e-6 {
			hasPos = true
		}
		if s < -1e-6 {
			hasNeg = true
		}
	}
	return !(hasPos && hasNeg)
}

// 计算由四个点形成的一对三角形中的最小角。
// 常用于比较边翻转之前和之后的三角形质量。
func minTriangleAnglePairXZ(a, b, c, d [3]float32) float64 {
	ang1 := minTriangleAngleXZ(a, b, c)
	ang2 := minTriangleAngleXZ(a, c, d)
	if ang1 < ang2 {
		return ang1
	}
	return ang2
}

// 返回一个三角形三个内角中的最小值。
// 最小角越大通常意味着三角形越均匀，可用于避免极细长三角形。
func minTriangleAngleXZ(a, b, c [3]float32) float64 {
	angles := [3]float64{
		triangleAngleXZ(b, a, c),
		triangleAngleXZ(a, b, c),
		triangleAngleXZ(a, c, b),
	}
	minv := angles[0]
	for i := 1; i < len(angles); i++ {
		if angles[i] < minv {
			minv = angles[i]
		}
	}
	return minv
}

// 计算三点在 XZ 平面构成的夹角。
// 使用向量点积和长度计算 cos 值，并限制到 [-1,1] 后通过 acos 得到弧度角。
func triangleAngleXZ(a, b, c [3]float32) float64 {
	abx := float64(a[0] - b[0])
	abz := float64(a[2] - b[2])
	cbx := float64(c[0] - b[0])
	cbz := float64(c[2] - b[2])
	l1 := math.Hypot(abx, abz)
	l2 := math.Hypot(cbx, cbz)
	if l1 == 0 || l2 == 0 {
		return 0
	}
	cosv := (abx*cbx + abz*cbz) / (l1 * l2)
	if cosv < -1 {
		cosv = -1
	} else if cosv > 1 {
		cosv = 1
	}
	return math.Acos(cosv)
}

// 计算三个点投影到 XZ 平面后的二维叉积。
// 常用于判断转向、三角形绕序、凸性以及点在线段哪一侧。
func cross2DXZ(a, b, c [3]float32) float64 {
	abx := float64(b[0] - a[0])
	abz := float64(b[2] - a[2])
	acx := float64(c[0] - a[0])
	acz := float64(c[2] - a[2])
	return abx*acz - abz*acx
}

// 用新的三个顶点覆盖指定三角形，同时尽量保持原始绕序。
// 比较修改前后的二维叉积符号，如果方向反转，则交换两个顶点避免正反面发生变化。
func writeTrianglePreserveWinding(indices []uint32, triStart int, a, b, c uint32, pos [][3]float32) {
	origA, origB, origC := indices[triStart], indices[triStart+1], indices[triStart+2]
	origSign := cross2DXZ(pos[origA], pos[origB], pos[origC])
	newSign := cross2DXZ(pos[a], pos[b], pos[c])
	if origSign == 0 || newSign == 0 || (origSign > 0) == (newSign > 0) {
		indices[triStart], indices[triStart+1], indices[triStart+2] = a, b, c
		return
	}
	indices[triStart], indices[triStart+1], indices[triStart+2] = a, c, b
}

// 构建建筑物表面几何。
// 当前仅预留接口，实际建筑 Mesh 生成逻辑尚未实现。
func (b *SurfaceBuilder) BuildBuilding(feature SurfaceFeature, rings []LocalRings) ([]*gltf.Primitive, error) {
	return nil, fmt.Errorf("build building: not implemented")
}

// 构建棚体或雨棚类面状设施。
// 当前仅预留接口，调用时返回 not implemented。
func (b *SurfaceBuilder) BuildCanopy(feature SurfaceFeature, rings []LocalRings) ([]*gltf.Primitive, error) {
	return nil, fmt.Errorf("build canopy: not implemented")
}

// 根据一组位置、半径和高度生成柱体。
// 当前函数尚未实现，未来可用于桥墩、立柱或其他点状柱体设施。
func (b *SurfaceBuilder) BuildColumnsFromPoints(points [][3]float32, radius, height float32, materials MaterialIndices) ([]*gltf.Primitive, error) {
	return nil, fmt.Errorf("build columns from points: not implemented")
}

// 从 FeatureFields 中读取指定字段并转换成 float32。
// 支持 float32、float64、整数、json.Number 和字符串；字段不存在或转换失败时返回 0。
func featureFieldFloat32(fields FeatureFields, key string) float32 {
	if fields == nil {
		return 0
	}
	raw, ok := fields[key]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case float32:
		return v
	case float64:
		return float32(v)
	case int:
		return float32(v)
	case int32:
		return float32(v)
	case int64:
		return float32(v)
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		if err == nil {
			return float32(f)
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return float32(f)
		}
	}
	return 0
}

// 从 FeatureFields 中读取指定字段并转换成 float64。
// 支持多种数字类型和数字字符串，用于统一处理来源类型不固定的业务属性。
func featureFieldFloat64(fields FeatureFields, key string) float64 {
	if fields == nil {
		return 0
	}
	raw, ok := fields[key]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		if err == nil {
			return f
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return f
		}
	}
	return 0
}

// 从 FeatureFields 中读取指定字段并转换成 int64。
// 可处理整数、浮点数、json.Number 和字符串；无法解析时返回 0。
func featureFieldInt64(fields FeatureFields, key string) int64 {
	if fields == nil {
		return 0
	}
	raw, ok := fields[key]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err == nil {
			return n
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return n
		}
	}
	return 0
}

// 从 FeatureFields 中读取布尔类型配置。
// 除 bool 外，也支持 "1"、"true"、"yes" 等字符串以及非零数字，无法识别时返回 false。
func featureFieldBool(fields FeatureFields, key string) bool {
	if fields == nil {
		return false
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "1", "true", "t", "yes", "y":
			return true
		default:
			return false
		}
	case float32:
		return val != 0
	case float64:
		return val != 0
	case int:
		return val != 0
	case int32:
		return val != 0
	case int64:
		return val != 0
	default:
		return false
	}
}

// 将 FeatureFields 中指定字段转换成字符串。
// 原值为 string 时直接返回，实现 fmt.Stringer 时调用 String，其余类型使用 fmt.Sprintf 统一格式化。
func featureFieldString(fields FeatureFields, key string) string {
	if fields == nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", raw)
	}
}

// 根据三角网创建带米制 UV 的平面 glTF Primitive。
// UV 直接使用顶点 X/Z 坐标除以纹理米制周期生成，可控制 V 翻转，并自动计算顶点法线。
func buildPlanarSurfacePrimitiveMeters(doc *gltf.Document, pos [][3]float32, indices []uint32, material int, meterX, meterY float32, flipV bool) (*gltf.Primitive, error) {
	if meterX <= 0 {
		meterX = 8
	}
	if meterY <= 0 {
		meterY = meterX
	}

	uvs := make([][2]float32, len(pos))
	for i := range pos {
		u := pos[i][0] / meterX
		v := pos[i][2] / meterY
		if flipV {
			v = -v
		}
		uvs[i] = [2]float32{u, v}
	}
	normals := computeVertexNormals(pos, indices)

	return &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, pos),
			gltf.NORMAL:     modeler.WriteNormal(doc, normals),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uvs),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, indices)),
		Material: gltf.Index(material),
	}, nil
}

// 根据世界坐标生成固定米制重复 UV。
// X 坐标控制 U、Z 坐标控制 V，通过 meterX/meterY 决定纹理每多少米重复一次，并支持 FlipV。
func fixedMeterUVs(pos [][3]float32, meterX, meterY float32, flipV bool) [][2]float32 {
	if meterX <= 0 {
		meterX = 1
	}
	if meterY <= 0 {
		meterY = meterX
	}
	uvs := make([][2]float32, len(pos))
	for i := range pos {
		u := pos[i][0] / meterX
		v := pos[i][2] / meterY
		if flipV {
			v = -v
		}
		uvs[i] = [2]float32{u, v}
	}
	return uvs
}

// 根据索引三角形累计并计算每个顶点的平均法线。
// 会忽略非法索引，先将相邻三角形法线累加到顶点，再对最终结果进行单位化。
func computeVertexNormals(pos [][3]float32, indices []uint32) [][3]float32 {
	normals := make([][3]float32, len(pos))
	for i := 0; i+2 < len(indices); i += 3 {
		ia, ib, ic := indices[i], indices[i+1], indices[i+2]
		if int(ia) >= len(pos) || int(ib) >= len(pos) || int(ic) >= len(pos) {
			continue
		}
		a, b, c := pos[ia], pos[ib], pos[ic]
		n := triangleNormal(a, b, c)
		normals[ia][0] += n[0]
		normals[ia][1] += n[1]
		normals[ia][2] += n[2]
		normals[ib][0] += n[0]
		normals[ib][1] += n[1]
		normals[ib][2] += n[2]
		normals[ic][0] += n[0]
		normals[ic][1] += n[1]
		normals[ic][2] += n[2]
	}
	for i := range normals {
		normals[i] = normalize3(normals[i])
	}
	return normals
}

// 通过两个三角形边向量的叉积计算三角形法线。
// 返回值未主动单位化，因此长度同时包含三角形面积相关信息。
func triangleNormal(a, b, c [3]float32) [3]float32 {
	ux := b[0] - a[0]
	uy := b[1] - a[1]
	uz := b[2] - a[2]
	vx := c[0] - a[0]
	vy := c[1] - a[1]
	vz := c[2] - a[2]
	return [3]float32{
		uy*vz - uz*vy,
		uz*vx - ux*vz,
		ux*vy - uy*vx,
	}
}

// 将三维向量归一化成单位向量。
// 当输入向量长度为 0 时返回默认向上的法线 {0,1,0}，避免除零。
func normalize3(v [3]float32) [3]float32 {
	l := math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]))
	if l == 0 {
		return [3]float32{0, 1, 0}
	}
	return [3]float32{float32(float64(v[0]) / l), float32(float64(v[1]) / l), float32(float64(v[2]) / l)}
}

// 根据虚线长度、间隔和起始偏移将完整折线拆分成多个可见线段。
// 先建立沿折线的累计距离，再计算每一个 dash 区间，最后通过 extractSubLine 提取对应几何。
func splitDashedLines(line LocalLine, dashLen, gapLen, startOffset float64) []LocalLine {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 || dashLen <= 0 {
		return nil
	}
	cycle := dashLen + gapLen
	if cycle <= 0 {
		return []LocalLine{{Points: points, Closed: false}}
	}

	startOffset = math.Mod(startOffset, cycle)
	if startOffset < 0 {
		startOffset += cycle
	}

	cum := make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		cum[i] = cum[i-1] + segmentLengthXZ(points[i-1], points[i])
	}
	totalLen := cum[len(cum)-1]
	if totalLen <= 0 {
		return nil
	}

	var intervals [][2]float64
	pos := -startOffset
	for pos < totalLen {
		dashStart := pos
		dashEnd := pos + dashLen
		if dashEnd > 0 && dashStart < totalLen {
			if dashStart < 0 {
				dashStart = 0
			}
			if dashEnd > totalLen {
				dashEnd = totalLen
			}
			if dashEnd > dashStart {
				intervals = append(intervals, [2]float64{dashStart, dashEnd})
			}
		}
		pos += cycle
	}

	out := make([]LocalLine, 0, len(intervals))
	for _, iv := range intervals {
		seg := extractSubLine(points, cum, iv[0], iv[1])
		if len(seg) >= 2 {
			out = append(out, LocalLine{Points: seg})
		}
	}
	return out
}

// 在两个三维点之间进行线性插值。
// t=0 返回起点，t=1 返回终点，中间值用于线段加密、虚线截取等场景。
func interpolatePoint3(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

// 返回两个 float32 数值中的较小值。
func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// 解析道路标线的虚线配置字符串。
// 优先支持 "dash,gap" 格式，同时兼容特定历史数据中的 "3.3" 类写法；成功时返回虚线长度、空白长度和 true。
func ParseDashPattern(pattern string) (float64, float64, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return 0, 0, false
	}

	parts := strings.Split(pattern, ",")
	if len(parts) == 2 {
		dash, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		gap, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 == nil && err2 == nil && dash > 0 && gap >= 0 {
			return dash, gap, true
		}
	}

	// 兼容类似 "3.3" 这种写法：�1�7?3,3 处理�?
	if strings.Count(pattern, ".") == 1 && !strings.Contains(pattern, ",") {
		parts = strings.Split(pattern, ".")
		if len(parts) == 2 {
			dash, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			gap, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 == nil && err2 == nil && dash > 0 && gap >= 0 {
				return dash, gap, true
			}
		}
	}

	return 0, 0, false
}

// 计算两个三维点在 XZ 水平平面上的距离。
// 不考虑 Y 高程差，适用于道路里程、纹理长度和二维几何计算。
func segmentLengthXZ(a, b [3]float32) float64 {
	return math.Hypot(float64(b[0]-a[0]), float64(b[2]-a[2]))
}

// 按累计距离从一条折线中截取指定区间。
// 会定位所有与 [from,to] 相交的原始线段，并在边界位置进行插值，从而得到精确的子折线。
func extractSubLine(points [][3]float32, cum []float64, from, to float64) [][3]float32 {
	if len(points) < 2 || to <= from {
		return nil
	}
	var out [][3]float32
	for i := 0; i < len(points)-1; i++ {
		segStart := cum[i]
		segEnd := cum[i+1]
		if segEnd <= from || segStart >= to {
			continue
		}
		segLen := segEnd - segStart
		if segLen <= 0 {
			continue
		}
		localStart := float32(0)
		if from > segStart {
			localStart = float32((from - segStart) / segLen)
		}
		localEnd := float32(1)
		if to < segEnd {
			localEnd = float32((to - segStart) / segLen)
		}
		p0 := interpolatePoint3(points[i], points[i+1], localStart)
		p1 := interpolatePoint3(points[i], points[i+1], localEnd)
		if len(out) == 0 || out[len(out)-1] != p0 {
			out = append(out, p0)
		}
		if p1 != p0 {
			out = append(out, p1)
		}
	}
	return out
}

// 计算整条折线在 XZ 平面的累计长度。
// 对所有相邻点调用 segmentLengthXZ 后求和。
func polylineLengthXZ(points [][3]float32) float64 {
	if len(points) < 2 {
		return 0
	}
	var total float64
	for i := 0; i < len(points)-1; i++ {
		total += segmentLengthXZ(points[i], points[i+1])
	}
	return total
}

// 将 LocalRings 的外环和所有孔洞整体向上抬升指定高度。
// 仅修改 Y 坐标，不改变 X/Z 平面形状。
func raiseLocalRings(rings LocalRings, height float32) LocalRings {
	out := LocalRings{
		Outer: raisePolyline(rings.Outer, height),
		Holes: make([][][3]float32, 0, len(rings.Holes)),
	}
	for _, hole := range rings.Holes {
		out.Holes = append(out.Holes, raisePolyline(hole, height))
	}
	return out
}

// 将一组折线点整体向下移动指定高度。
// 常用于从绿化带顶部 Ring 推导对应的底部侧壁 Ring。
func lowerPolyline(points [][3]float32, height float32) [][3]float32 {
	out := make([][3]float32, len(points))
	for i := range points {
		out[i] = [3]float32{points[i][0], points[i][1] - height, points[i][2]}
	}
	return out
}

// 将 LocalRings 转换为普通 Ring 切片列表。
// 输出顺序为 Outer 在前，随后依次追加所有 Holes，方便统一遍历外环和孔洞。
func localRingsToSlices(rings LocalRings) [][][3]float32 {
	out := make([][][3]float32, 0, 1+len(rings.Holes))
	if len(rings.Outer) > 0 {
		out = append(out, rings.Outer)
	}
	out = append(out, rings.Holes...)
	return out
}

// 从 glTF Primitive 的 POSITION Accessor 中取得顶点数量。
// 如果 Document、Primitive 或 POSITION Accessor 无效，则安全返回 0。
func primitiveVertexCount(doc *gltf.Document, primitive *gltf.Primitive) int {
	if doc == nil || primitive == nil {
		return 0
	}
	accIdx, ok := primitive.Attributes[gltf.POSITION]
	if !ok || int(accIdx) < 0 || int(accIdx) >= len(doc.Accessors) || doc.Accessors[accIdx] == nil {
		return 0
	}
	return doc.Accessors[accIdx].Count
}

// 生成绿化带完整的顶部和侧壁 Primitive。
// 顶部首先三角化并生成法线和米制 UV；随后将每个 Ring 与向下偏移后的 Ring 连接形成墙面，并分别应用顶部材质和侧壁材质。
func buildGreenbeltPrimitivesMeters(doc *gltf.Document, rings LocalRings, height float32, topMaterial, sideMaterial int, meterX, meterY float32, flipV bool) ([]*gltf.Primitive, error) {
	allRings := localRingsToSlices(rings)
	if len(allRings) == 0 {
		return nil, nil
	}
	topPos, topIdx, err := triangulateGreenbeltTopRing(rings)
	if err != nil {
		return nil, err
	}
	top := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, topPos),
			gltf.NORMAL:     modeler.WriteNormal(doc, computeVertexNormals(topPos, topIdx)),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, fixedMeterUVs(topPos, meterX, meterY, flipV)),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, topIdx)),
		Material: gltf.Index(topMaterial),
	}

	wallPos := make([][3]float32, 0)
	wallUV := make([][2]float32, 0)
	wallIdx := make([]uint32, 0)
	for _, ring := range allRings {
		r := normalizeRingForWalls(ring)
		if len(r) < 3 {
			continue
		}
		base := uint32(len(wallPos))
		bottomRing := lowerPolyline(r, height)
		wallPos = append(wallPos, r...)
		wallPos = append(wallPos, bottomRing...)
		wallUV = append(wallUV, wallUVsMeters(r, height, meterX, meterY, flipV)...)
		wallIdx = append(wallIdx, wallIndicesOffsetLocal(len(r), base)...)
	}
	if len(wallPos) == 0 {
		return []*gltf.Primitive{top}, nil
	}
	walls := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   modeler.WritePosition(doc, wallPos),
			gltf.NORMAL:     modeler.WriteNormal(doc, computeVertexNormals(wallPos, wallIdx)),
			gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, wallUV),
		},
		Indices:  gltf.Index(modeler.WriteIndices(doc, wallIdx)),
		Material: gltf.Index(sideMaterial),
	}
	return []*gltf.Primitive{top, walls}, nil
}

// 对绿化带顶面执行三角化。
// 当前作为统一入口，实际策略交由 triangulatePreferredGreenbeltTopRing 决定。
func triangulateGreenbeltTopRing(rings LocalRings) ([][3]float32, []uint32, error) {
	return triangulatePreferredGreenbeltTopRing(rings)
}

// 为绿化带顶面选择尽量保真的三角化方案。
// 优先直接使用原始合法 Ring；失败后仅进行较轻量的清洗，再失败才使用路基级别的更强清洗方式作为最终兜底。
func triangulatePreferredGreenbeltTopRing(rawRing LocalRings) ([][3]float32, []uint32, error) {
	rawOuter := dedupeClosedRing(rawRing.Outer)
	if len(rawOuter) >= 3 && !hasSelfIntersectionXZ(rawOuter) {
		if pos, idx, err := triangulateGreenbeltTopRingCandidates(rawRing); err == nil && len(idx) >= 3 {
			return pos, idx, nil
		}
	}

	cleanRing := cleanGreenbeltTopLocalRings(rawRing, 0.001)
	if pos, idx, err := triangulateGreenbeltTopRingCandidates(cleanRing); err == nil && len(idx) >= 3 {
		return pos, idx, nil
	}

	fallbackRing := cleanSubgradeLocalRings(rawRing, 0.01)
	return triangulateGreenbeltTopRingCandidates(fallbackRing)
}

// 尝试生成绿化带顶部三角网。
// 没有孔洞时优先从轮廓估算中心线并采用道路式中心线三角化；无法使用中心线时回退到普通平面多边形三角化。
func triangulateGreenbeltTopRingCandidates(rings LocalRings) ([][3]float32, []uint32, error) {
	if len(rings.Holes) == 0 {
		if cl, ok := estimateLocalCenterlineFromOuter(rings.Outer); ok {
			if pos, idx, ok := triangulateRoadSurfaceByCenterline(rings, cl); ok {
				return pos, idx, nil
			}
		}
	}
	return triangulatePlanarPolygonSurfaceRing(rings)
}

// 对绿化带顶面 Ring 做轻量清洗。
// 主要处理重复/短边和环方向，不主动使用较强的共线简化，以尽可能保留原始绿化带边界形状。
func cleanGreenbeltTopLocalRings(rings LocalRings, minEdge float64) LocalRings {
	out := LocalRings{
		Outer: sanitizeSubgradeRing(rings.Outer, minEdge, 0, true),
		Holes: make([][][3]float32, 0, len(rings.Holes)),
	}
	for _, h := range rings.Holes {
		out.Holes = append(out.Holes, sanitizeSubgradeRing(h, minEdge, 0, false))
	}
	return out
}

// 根据长条状多边形外轮廓估算局部中心线。
// 内部使用默认长宽比阈值 2.0 调用 estimateLocalCenterlineFromOuterWithMinRatio。
func estimateLocalCenterlineFromOuter(outer [][3]float32) (LocalLine, bool) {
	return estimateLocalCenterlineFromOuterWithMinRatio(outer, 2.0)
}

// 利用外轮廓主轴方向估算长条多边形的中心线。
// 先计算包围范围和长宽比，再通过 PCA 得到主方向；沿主轴进行多次横截面采样，取左右边界交点中点形成中心线。
func estimateLocalCenterlineFromOuterWithMinRatio(outer [][3]float32, minRatio float64) (LocalLine, bool) {
	outer = dedupeClosedRing(outer)
	if len(outer) < 6 {
		return LocalLine{}, false
	}
	var cx, cz float64
	minX, maxX := float64(outer[0][0]), float64(outer[0][0])
	minZ, maxZ := float64(outer[0][2]), float64(outer[0][2])
	for _, p := range outer {
		x := float64(p[0])
		z := float64(p[2])
		cx += x
		cz += z
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if z < minZ {
			minZ = z
		}
		if z > maxZ {
			maxZ = z
		}
	}
	n := float64(len(outer))
	cx /= n
	cz /= n
	spanX := maxX - minX
	spanZ := maxZ - minZ
	longSpan := math.Max(spanX, spanZ)
	shortSpan := math.Min(spanX, spanZ)
	if longSpan < 3 || shortSpan <= 0 || longSpan/shortSpan < minRatio {
		return LocalLine{}, false
	}

	local2 := make([][2]float64, len(outer))
	for i, p := range outer {
		local2[i] = [2]float64{float64(p[0]), float64(p[2])}
	}
	_, _, axisX, axisZ, ok := principalAxis2D(local2)
	if !ok {
		return LocalLine{}, false
	}
	perp := [2]float64{-axisZ, axisX}
	minT, maxT, ok := projectionSpan(local2, cx, cz, axisX, axisZ)
	if !ok || maxT-minT < 1 {
		return LocalLine{}, false
	}
	count := int(math.Ceil((maxT - minT) / 4.0))
	if count < 6 {
		count = 6
	}
	if count > 32 {
		count = 32
	}
	points := make([][3]float32, 0, count+1)
	for i := 0; i <= count; i++ {
		t := minT + (maxT-minT)*float64(i)/float64(count)
		base := [2]float64{cx + axisX*t, cz + axisZ*t}
		if p, ok := centerlinePointAtSampleLocal(outer, base, perp); ok {
			points = append(points, p)
		}
	}
	points = dedupeLinePoints(points)
	if len(points) < 3 {
		return LocalLine{}, false
	}
	return LocalLine{Points: points}, true
}

// 在给定横截面采样线上计算多边形内部中心点。
// 求采样无限直线与所有多边形边的交点，排序去重后寻找包围中心位置的左右交点，并取两者的位置和高程中值。
func centerlinePointAtSampleLocal(poly [][3]float32, base, dir [2]float64) ([3]float32, bool) {
	hits := make([]sampleHit, 0, 8)
	for i := 0; i < len(poly); i++ {
		a := poly[i]
		b := poly[(i+1)%len(poly)]
		u, v, ok := infiniteLineSegmentIntersectionUV(base, dir, [2]float64{float64(a[0]), float64(a[2])}, [2]float64{float64(b[0]), float64(b[2])})
		if !ok {
			continue
		}
		y := float64(a[1]) + float64(b[1]-a[1])*v
		hits = append(hits, sampleHit{u: u, y: y})
	}
	if len(hits) < 2 {
		return [3]float32{}, false
	}
	sortSampleHitsByU(hits)
	hits = dedupeSampleHits(hits, 0.1)
	if len(hits) < 2 {
		return [3]float32{}, false
	}
	left, right, ok := nearestBracketSampleHits(hits)
	if !ok {
		return [3]float32{}, false
	}
	midu := (left.u + right.u) * 0.5
	return [3]float32{
		float32(base[0] + dir[0]*midu),
		float32((left.y + right.y) * 0.5),
		float32(base[1] + dir[1]*midu),
	}, true
}

// 将闭合 Ring 转换为适合生成墙面的顶点序列。
// 如果最后一个点与第一个点重复，则移除末尾闭合点，避免侧壁生成时重复创建一段零长度边。
func normalizeRingForWalls(ring [][3]float32) [][3]float32 {
	if len(ring) >= 2 && ring[0][0] == ring[len(ring)-1][0] && ring[0][2] == ring[len(ring)-1][2] {
		return ring[:len(ring)-1]
	}
	return ring
}

// 为一对上下 Ring 生成连续侧壁三角形索引。
// 每一条边对应一个四边形，再拆成两个三角形，并通过 base 支持将结果追加到已有批量几何缓存中。
func wallIndicesOffsetLocal(n int, base uint32) []uint32 {
	indices := make([]uint32, 0, n*6)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a := uint32(i) + base
		b := uint32(j) + base
		c := uint32(j+n) + base
		d := uint32(i+n) + base
		indices = append(indices, a, b, c, a, c, d)
	}
	return indices
}

// 为垂直墙面生成按真实距离连续展开的 UV。
// U 根据顶部 Ring 的累计水平长度生成，V 根据墙体高度生成，并分别应用 meterU、meterV 的纹理重复尺度，同时支持 FlipV。
func wallUVsMeters(top [][3]float32, height, meterU, meterV float32, flipV bool) [][2]float32 {
	if meterU <= 0 {
		meterU = 1
	}
	if meterV <= 0 {
		meterV = meterU
	}
	uvs := make([][2]float32, 0, len(top)*2)
	var total float32
	cum := make([]float32, len(top))
	for i := 1; i < len(top); i++ {
		total += float32(segmentLengthXZ(top[i-1], top[i]))
		cum[i] = total
	}
	for i := range top {
		u := cum[i] / meterU
		v := float32(0)
		if flipV {
			v = -v
		}
		uvs = append(uvs, [2]float32{u, v})
	}
	for i := range top {
		u := cum[i] / meterU
		v := height / meterV
		if flipV {
			v = -v
		}
		uvs = append(uvs, [2]float32{u, v})
	}
	return uvs
}

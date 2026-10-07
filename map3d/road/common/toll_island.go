package common

import (
	"fmt"
	"math"
	"strings"

	"github.com/qmuntal/gltf"
)

const defaultTollIslandTopRepeatMeters = float32(4.0)
const defaultTollIslandHeadLength = float32(3.5)
const defaultTollIslandHeadWidth = float32(2.0)
const defaultTollIslandTailLength = float32(3.0)
const defaultTollIslandTailWidth = float32(2.0)
const defaultTollIslandHeadModelNativeLength = float32(2.3516378)
const defaultTollIslandTailModelNativeLength = float32(3.0)

// BuildTollIslandTopMesh 生成收费岛顶面。
//
// 输入线被视为收费岛长轴。岛身始终按长方体处理：
// - 线长度决定岛身长度
// - width 决定左右展宽
// - height 只用于抬高返回的顶面顶点
//
// 返回的 rings 保持在基础高程，供侧墙继续复用。
func BuildTollIslandTopMesh(line LocalLine, width, height float32) (LocalRings, [][3]float32, []uint32, error) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return LocalRings{}, nil, nil, fmt.Errorf("toll island line needs at least 2 distinct points")
	}

	rings, corners, err := buildTollIslandRectRings(points[0], points[len(points)-1], width)
	if err != nil {
		return LocalRings{}, nil, nil, err
	}

	pos := make([][3]float32, len(corners))
	copy(pos, corners)
	for i := range pos {
		pos[i][1] += height
	}

	indices := []uint32{0, 1, 2, 0, 2, 3}
	return rings, pos, indices, nil
}

// buildTollIslandRectRings 将两点线扩展为基础矩形。
//
// 返回：
// - 供侧墙构建使用的 LocalRings
// - 按顺序排列的四个矩形角点，供顶面构网使用
func buildTollIslandRectRings(start, end [3]float32, width float32) (LocalRings, [][3]float32, error) {
	if width <= 0 {
		return LocalRings{}, nil, fmt.Errorf("toll island width must be > 0")
	}

	forward := normalize3([3]float32{end[0] - start[0], 0, end[2] - start[2]})
	if forward[0] == 0 && forward[1] == 1 && forward[2] == 0 {
		return LocalRings{}, nil, fmt.Errorf("toll island line direction is invalid")
	}

	right := [3]float32{forward[2], 0, -forward[0]}
	half := width / 2

	startLeft := [3]float32{start[0] - right[0]*half, start[1], start[2] - right[2]*half}
	endLeft := [3]float32{end[0] - right[0]*half, end[1], end[2] - right[2]*half}
	endRight := [3]float32{end[0] + right[0]*half, end[1], end[2] + right[2]*half}
	startRight := [3]float32{start[0] + right[0]*half, start[1], start[2] + right[2]*half}

	return LocalRings{
			Outer: [][3]float32{startLeft, endLeft, endRight, startRight, startLeft},
		},
		[][3]float32{startLeft, startRight, endRight, endLeft},
		nil
}

// BuildTollIslandSideMesh 生成收费岛侧墙网格。
func BuildTollIslandSideMesh(line LocalLine, width, height float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	if height <= 0 {
		return nil, nil, nil, nil, fmt.Errorf("toll island height must be > 0")
	}
	rings, _, _, err := BuildTollIslandTopMesh(line, width, 0)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return buildTollIslandSideMeshFromRings(rings, height, width, height, false)
}

// buildTollIslandSideMeshFromRings 为每个 ring 生成一圈带索引的侧墙条带。
//
// 顶点布局：
// - 先追加抬高后的 top ring
// - 再追加基础高程的 bottom ring
//
// 这样可以与 wallIndicesOffsetLocal 对齐，并让相邻侧墙共享顶点。
func buildTollIslandSideMeshFromRings(rings LocalRings, height, meterU, meterV float32, flipV bool) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	allRings := localRingsToSlices(rings)
	wallPos := make([][3]float32, 0)
	wallUV := make([][2]float32, 0)
	wallIdx := make([]uint32, 0)

	for _, ring := range allRings {
		r := normalizeRingForWalls(ring)
		if len(r) < 3 {
			continue
		}

		base := uint32(len(wallPos))
		topRing := raisePolyline(r, height)

		wallPos = append(wallPos, topRing...)
		wallPos = append(wallPos, r...)
		wallUV = append(wallUV, wallUVsMeters(topRing, height, meterU, meterV, flipV)...)
		wallIdx = append(wallIdx, wallIndicesOffsetLocal(len(r), base)...)
	}

	if len(wallPos) == 0 || len(wallIdx) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("toll island walls are empty")
	}

	return wallPos, computeVertexNormals(wallPos, wallIdx), wallUV, wallIdx, nil
}

// BuildRoadTollIsland 生成收费岛主体，以及可选的岛头/岛尾模型。
func (b *SurfaceBuilder) BuildRoadTollIsland(feature LineFeature, lines []LocalLine) ([]*gltf.Primitive, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("road toll island lines are empty")
	}

	width, height := resolveTollIslandDimensions(feature)

	materialSet := feature.Material
	materialSet.Top.DoubleSided = true
	materialSet.Side.DoubleSided = true
	materials, err := b.materials.ResolveMaterialSet(materialSet)
	if err != nil {
		return nil, err
	}
	if materials.Top < 0 {
		return nil, fmt.Errorf("road toll island top material is invalid")
	}

	sideMaterial := materials.Side
	if sideMaterial < 0 {
		sideMaterial = materials.Top
	}

	repeatMetersX := feature.UV.RepeatX
	repeatMetersY := feature.UV.RepeatY
	if repeatMetersX <= 0 {
		repeatMetersX = defaultTollIslandTopRepeatMeters
	}
	if repeatMetersY <= 0 {
		repeatMetersY = repeatMetersX
	}

	headModelPath, tailModelPath, headOffset, tailOffset, headYawDeg, tailYawDeg := resolveTollIslandModelOptions(feature.Fields)
	headTransforms := make([]instancedTransform, 0, len(lines))
	tailTransforms := make([]instancedTransform, 0, len(lines))

	out := make([]*gltf.Primitive, 0, len(lines)*2)
	for _, line := range lines {
		rings, topPos, topIdx, err := BuildTollIslandTopMesh(line, width, height)
		if err != nil {
			return nil, err
		}

		top, err := buildPlanarSurfacePrimitiveMeters(b.doc, topPos, topIdx, materials.Top, repeatMetersX, repeatMetersY, feature.UV.FlipV)
		if err != nil {
			return nil, err
		}
		b.attachFeatureMetadata(top, primitiveVertexCount(b.doc, top), feature.FeatureInput)
		out = append(out, top)

		sidePos, sideNormals, sideUV, sideIdx, err := buildTollIslandSideMeshFromRings(rings, height, repeatMetersX, repeatMetersY, feature.UV.FlipV)
		if err != nil {
			return nil, err
		}
		side := buildPrimitiveFromGeometry(b.doc, sideMaterial, sidePos, sideNormals, sideUV, sideIdx)
		if side != nil {
			b.attachFeatureMetadata(side, primitiveVertexCount(b.doc, side), feature.FeatureInput)
			out = append(out, side)
		}

		headTransform, tailTransform, ok := buildTollIslandEndModelTransforms(line, width, headOffset, tailOffset, headYawDeg, tailYawDeg)
		if !ok {
			continue
		}
		if headModelPath != "" {
			headTransforms = append(headTransforms, headTransform)
		}
		if tailModelPath != "" {
			tailTransforms = append(tailTransforms, tailTransform)
		}
	}

	featureID := uint32(0)
	if b.metadata != nil {
		ids := b.metadata.RegisterFeatureRows(feature.Features, feature.Fields)
		if len(ids) > 0 {
			featureID = ids[0]
		}
	}
	if headModelPath != "" && len(headTransforms) > 0 {
		meshIndex, err := b.ensureInstancedModelMesh(headModelPath)
		if err != nil {
			return nil, fmt.Errorf("import toll island head model: %w", err)
		}
		addInstancedMeshNode(b.doc, meshIndex, "toll-island-head", headTransforms, featureID)
	}
	if tailModelPath != "" && len(tailTransforms) > 0 {
		meshIndex, err := b.ensureInstancedModelMesh(tailModelPath)
		if err != nil {
			return nil, fmt.Errorf("import toll island tail model: %w", err)
		}
		addInstancedMeshNode(b.doc, meshIndex, "toll-island-tail", tailTransforms, featureID)
	}
	return out, nil
}

func resolveTollIslandDimensions(feature LineFeature) (float32, float32) {
	width := feature.Width
	if width <= 0 {
		width = featureFieldFloat32(feature.Fields, "width")
	}
	if width <= 0 {
		width = featureFieldFloat32(feature.Fields, "widths")
	}
	if width <= 0 {
		width = 2.0
	}

	height := feature.Height
	if height <= 0 {
		height = featureFieldFloat32(feature.Fields, "height")
	}
	if height <= 0 {
		height = 0.3
	}

	return width, height
}

func resolveTollIslandModelOptions(fields FeatureFields) (headModelPath, tailModelPath string, headOffset, tailOffset, headYawDeg, tailYawDeg float32) {
	headModelPath = strings.TrimSpace(featureFieldString(fields, "head_model_path"))
	tailModelPath = strings.TrimSpace(featureFieldString(fields, "tail_model_path"))
	headOffset = featureFieldFloat32(fields, "head_offset")
	tailOffset = featureFieldFloat32(fields, "tail_offset")
	headYawDeg = featureFieldFloat32(fields, "head_yaw_deg")
	tailYawDeg = featureFieldFloat32(fields, "tail_yaw_deg")
	return
}

// buildTollIslandEndModelTransforms 计算岛头/岛尾预制模型的端点变换。
//
// 当前放置规则：
// - 岛头模型锚点严格放在线起点
// - 岛尾模型锚点严格放在线终点
// - 岛头朝向岛身外侧，因此使用线反方向
// - 岛尾沿线方向摆放
//
// offset 字段目前只保留接口兼容性，当前实现故意不使用，
// 以保证模型严格锚定在线两端。
func buildTollIslandEndModelTransforms(line LocalLine, width, headOffset, tailOffset, headYawDeg, tailYawDeg float32) (instancedTransform, instancedTransform, bool) {
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return instancedTransform{}, instancedTransform{}, false
	}

	start := points[0]
	end := points[len(points)-1]
	forward := normalize3([3]float32{end[0] - start[0], 0, end[2] - start[2]})
	if forward[0] == 0 && forward[1] == 1 && forward[2] == 0 {
		return instancedTransform{}, instancedTransform{}, false
	}
	reverse := [3]float32{-forward[0], 0, -forward[2]}

	headWidthRatio := width / defaultTollIslandHeadWidth
	if headWidthRatio <= 0 {
		headWidthRatio = 1
	}
	tailWidthRatio := width / defaultTollIslandTailWidth
	if tailWidthRatio <= 0 {
		tailWidthRatio = 1
	}

	// 预留给后续扩展；当前严格按端点锚定，不应用偏移。
	_ = headOffset
	_ = tailOffset

	head := instancedTransform{
		Translation: start,
		Rotation:    applyTollIslandYawOffset(quaternionFromForward(reverse), headYawDeg),
		Scale:       tollIslandModelScale(headWidthRatio, defaultTollIslandHeadLength/defaultTollIslandHeadModelNativeLength),
	}
	tail := instancedTransform{
		Translation: end,
		Rotation:    applyTollIslandYawOffset(quaternionFromForward(forward), tailYawDeg),
		Scale:       tollIslandModelScale(tailWidthRatio, defaultTollIslandTailLength/defaultTollIslandTailModelNativeLength),
	}
	return head, tail, true
}

// tollIslandModelScale 计算收费岛头尾模型缩放。
// - X/Z：按宽度比例缩放
// - Y：按目标长度比例缩放
func tollIslandModelScale(widthRatio, lengthRatio float32) [3]float32 {
	if widthRatio <= 0 {
		widthRatio = 1
	}
	if lengthRatio <= 0 {
		lengthRatio = 1
	}
	return [3]float32{widthRatio, lengthRatio, widthRatio}
}

func applyTollIslandYawOffset(q [4]float32, deg float32) [4]float32 {
	if deg == 0 {
		return q
	}
	rad := float64(deg) * math.Pi / 180
	s := float32(math.Sin(rad / 2))
	c := float32(math.Cos(rad / 2))
	return normalizedQuat4(quatMul4(q, [4]float32{0, s, 0, c}))
}

// importTollIslandModelPrimitives 将预制模型按固定变换直接导入当前文档。
//
// 收费岛头尾模型故意不走 instancing：
// - 数量固定只有两个
// - 直接导入更容易核对端点位置和朝向
// - 调试成本更低
func importTollIslandModelPrimitives(dstDoc *gltf.Document, modelPath string, tr instancedTransform) ([]*gltf.Primitive, error) {
	srcDoc, err := gltf.Open(modelPath)
	if err != nil {
		return nil, err
	}
	if len(srcDoc.Meshes) == 0 {
		return nil, fmt.Errorf("toll island model has no meshes: %s", modelPath)
	}

	sceneNodes := []int{}
	if srcDoc.Scene != nil && *srcDoc.Scene < len(srcDoc.Scenes) && srcDoc.Scenes[*srcDoc.Scene] != nil {
		sceneNodes = append(sceneNodes, srcDoc.Scenes[*srcDoc.Scene].Nodes...)
	}
	if len(sceneNodes) == 0 {
		for i := range srcDoc.Nodes {
			sceneNodes = append(sceneNodes, i)
		}
	}

	images := make(map[int]int)
	samplers := make(map[int]int)
	textures := make(map[int]int)
	materials := make(map[int]int)
	out := make([]*gltf.Primitive, 0)
	world := modelNodeTransform{
		Translation: tr.Translation,
		Rotation:    tr.Rotation,
		Scale:       tr.Scale,
	}
	for _, nodeIndex := range sceneNodes {
		if err := appendImportedNodePrimitives(dstDoc, srcDoc, nodeIndex, world, &out, images, samplers, textures, materials); err != nil {
			return nil, err
		}
	}
	return out, nil
}

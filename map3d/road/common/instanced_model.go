package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

//整体覆盖：
//
//glTF 模型导入流程
//Mesh 合并
//Node 层级变换
//Material / Texture / Image 资源复制
//顶点坐标转换
//四元数和向量数学辅助函数
//深复制工具函数

// 表示 glTF 节点的空间变换信息。
//
// 用于保存模型节点的：
// - 平移（Translation）
// - 旋转四元数（Rotation）
// - 缩放（Scale）
//
// 在导入嵌套 glTF 节点结构时，用于累计父子节点变换。
type modelNodeTransform struct {
	Translation [3]float32
	Rotation    [4]float32
	Scale       [3]float32
}

// 创建单位模型变换。
//
// 返回：
// - 无平移。
// - 无旋转。
// - 缩放比例为1。
//
// 用于作为节点变换计算的初始状态。
func identityModelNodeTransform() modelNodeTransform {
	return modelNodeTransform{
		Rotation: [4]float32{0, 0, 0, 1},
		Scale:    [3]float32{1, 1, 1},
	}
}

// 确保指定模型已经导入到当前文档中。
//
// 功能：
// 1. 检查 SurfaceBuilder 和 glTF 文档是否有效。
// 2. 根据模型路径缓存已经导入的 mesh。
// 3. 如果模型未加载，则调用 importInstancedModelMesh 导入。
//
// 返回：
// 已存在或新创建的 mesh 索引。
//
// 该函数用于避免同一个实例化模型被重复导入。
func (b *SurfaceBuilder) ensureInstancedModelMesh(modelPath string) (int, error) {
	if b == nil || b.doc == nil {
		return -1, fmt.Errorf("surface builder document is nil")
	}
	if b.instancedModels == nil {
		b.instancedModels = make(map[string]int)
	}
	path := filepath.Clean(modelPath)
	if idx, ok := b.instancedModels[path]; ok {
		return idx, nil
	}
	idx, err := importInstancedModelMesh(b.doc, path)
	if err != nil {
		return -1, err
	}
	b.instancedModels[path] = idx
	return idx, nil
}

// 导入外部 glTF/glb 模型并合并为一个 Mesh。
//
// 功能：
// 1. 打开外部 glTF 文件。
// 2. 遍历模型场景节点。
// 3. 递归读取节点中的 Primitive。
// 4. 复制几何、材质、纹理资源到目标文档。
// 5. 合并生成新的 Mesh。
//
// 参数：
// dstDoc      目标 glTF 文档。
// modelPath   外部模型路径。
//
// 返回：
// 新生成 mesh 在目标文档中的索引。
func importInstancedModelMesh(dstDoc *gltf.Document, modelPath string) (int, error) {
	srcDoc, err := gltf.Open(modelPath)
	if err != nil {
		return -1, fmt.Errorf("open instanced model %s: %w", modelPath, err)
	}
	if len(srcDoc.Meshes) == 0 {
		return -1, fmt.Errorf("instanced model has no meshes: %s", modelPath)
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
	primitives := make([]*gltf.Primitive, 0)
	for _, nodeIndex := range sceneNodes {
		if err := appendImportedNodePrimitives(dstDoc, srcDoc, nodeIndex, identityModelNodeTransform(), &primitives, images, samplers, textures, materials); err != nil {
			return -1, err
		}
	}
	if len(primitives) == 0 {
		return -1, fmt.Errorf("instanced model produced no primitives: %s", modelPath)
	}

	meshIndex := len(dstDoc.Meshes)
	dstDoc.Meshes = append(dstDoc.Meshes, &gltf.Mesh{
		Name:       filepath.Base(modelPath),
		Primitives: primitives,
	})
	return meshIndex, nil
}

// 递归遍历 glTF 节点并提取 Primitive。
//
// 功能：
// 1. 计算当前节点相对于世界坐标的变换。
// 2. 如果节点包含 Mesh，则复制其中所有 Primitive。
// 3. 递归处理所有子节点。
//
// 参数：
// dstDoc      目标文档。
// srcDoc      源模型文档。
// nodeIndex   当前节点索引。
// parent      父节点累计变换。
// out         输出 Primitive 列表。
//
// 用于保持原始 glTF 节点层级结构中的空间关系。
func appendImportedNodePrimitives(dstDoc, srcDoc *gltf.Document, nodeIndex int, parent modelNodeTransform, out *[]*gltf.Primitive, images, samplers, textures, materials map[int]int) error {
	if nodeIndex < 0 || nodeIndex >= len(srcDoc.Nodes) || srcDoc.Nodes[nodeIndex] == nil {
		return nil
	}
	node := srcDoc.Nodes[nodeIndex]
	world := composeModelNodeTransform(parent, node)

	if node.Mesh != nil && *node.Mesh >= 0 && *node.Mesh < len(srcDoc.Meshes) && srcDoc.Meshes[*node.Mesh] != nil {
		mesh := srcDoc.Meshes[*node.Mesh]
		for _, srcPrim := range mesh.Primitives {
			prim, err := cloneImportedPrimitive(dstDoc, srcDoc, srcPrim, world, images, samplers, textures, materials)
			if err != nil {
				return err
			}
			*out = append(*out, prim)
		}
	}
	for _, child := range node.Children {
		if err := appendImportedNodePrimitives(dstDoc, srcDoc, child, world, out, images, samplers, textures, materials); err != nil {
			return err
		}
	}
	return nil
}

// 合并父节点和当前节点的变换。
//
// 根据 glTF 节点规则计算：
// - 平移叠加。
// - 四元数旋转组合。
// - 缩放累乘。
//
// 返回当前节点在世界坐标系中的最终变换。
func composeModelNodeTransform(parent modelNodeTransform, node *gltf.Node) modelNodeTransform {
	local := identityModelNodeTransform()
	if node != nil {
		local.Translation = [3]float32{float32(node.Translation[0]), float32(node.Translation[1]), float32(node.Translation[2])}
		local.Rotation = normalizedQuat4([4]float32{float32(node.Rotation[0]), float32(node.Rotation[1]), float32(node.Rotation[2]), float32(node.Rotation[3])})
		local.Scale = [3]float32{float32(node.Scale[0]), float32(node.Scale[1]), float32(node.Scale[2])}
		if isZeroScale(local.Scale) {
			local.Scale = [3]float32{1, 1, 1}
		}
	}

	scaledT := [3]float32{
		local.Translation[0] * parent.Scale[0],
		local.Translation[1] * parent.Scale[1],
		local.Translation[2] * parent.Scale[2],
	}
	rotatedT := rotateVec3(parent.Rotation, scaledT)
	return modelNodeTransform{
		Translation: [3]float32{
			parent.Translation[0] + rotatedT[0],
			parent.Translation[1] + rotatedT[1],
			parent.Translation[2] + rotatedT[2],
		},
		Rotation: normalizedQuat4(quatMul4(parent.Rotation, local.Rotation)),
		Scale: [3]float32{
			parent.Scale[0] * local.Scale[0],
			parent.Scale[1] * local.Scale[1],
			parent.Scale[2] * local.Scale[2],
		},
	}
}

// 复制一个 glTF Primitive 到目标文档。
//
// 功能：
// 1. 读取顶点位置。
// 2. 应用节点世界变换。
// 3. 转换法线方向。
// 4. 复制UV坐标。
// 5. 复制索引。
// 6. 复制关联材质。
//
// 如果缩放导致坐标系发生镜像，会自动调整三角形绕序。
//
// 返回：
// 目标文档中的新 Primitive。
func cloneImportedPrimitive(dstDoc, srcDoc *gltf.Document, srcPrim *gltf.Primitive, world modelNodeTransform, images, samplers, textures, materials map[int]int) (*gltf.Primitive, error) {
	if srcPrim == nil {
		return nil, fmt.Errorf("source primitive is nil")
	}
	posIndex, ok := srcPrim.Attributes[gltf.POSITION]
	if !ok || posIndex < 0 || posIndex >= len(srcDoc.Accessors) {
		return nil, fmt.Errorf("source primitive has no POSITION accessor")
	}
	pos, err := modeler.ReadPosition(srcDoc, srcDoc.Accessors[posIndex], nil)
	if err != nil {
		return nil, fmt.Errorf("read source positions: %w", err)
	}
	for i := range pos {
		pos[i] = transformImportedPosition(world, pos[i])
	}

	var normals [][3]float32
	if normalIndex, ok := srcPrim.Attributes[gltf.NORMAL]; ok && normalIndex >= 0 && normalIndex < len(srcDoc.Accessors) {
		normals, err = modeler.ReadNormal(srcDoc, srcDoc.Accessors[normalIndex], nil)
		if err != nil {
			return nil, fmt.Errorf("read source normals: %w", err)
		}
		for i := range normals {
			normals[i] = transformImportedNormal(world, normals[i])
		}
	}

	var uv [][2]float32
	if uvIndex, ok := srcPrim.Attributes[gltf.TEXCOORD_0]; ok && uvIndex >= 0 && uvIndex < len(srcDoc.Accessors) {
		uv, err = modeler.ReadTextureCoord(srcDoc, srcDoc.Accessors[uvIndex], nil)
		if err != nil {
			return nil, fmt.Errorf("read source uv: %w", err)
		}
	}

	var indices []uint32
	if srcPrim.Indices != nil && *srcPrim.Indices >= 0 && *srcPrim.Indices < len(srcDoc.Accessors) {
		indices, err = modeler.ReadIndices(srcDoc, srcDoc.Accessors[*srcPrim.Indices], nil)
		if err != nil {
			return nil, fmt.Errorf("read source indices: %w", err)
		}
	}
	if determinantSign(world.Scale) < 0 {
		reverseTriangleWinding(indices)
	}

	attrs := gltf.PrimitiveAttributes{
		gltf.POSITION: modeler.WritePosition(dstDoc, pos),
	}
	if len(normals) == len(pos) {
		attrs[gltf.NORMAL] = modeler.WriteNormal(dstDoc, normals)
	}
	if len(uv) == len(pos) {
		attrs[gltf.TEXCOORD_0] = modeler.WriteTextureCoord(dstDoc, uv)
	}

	var matIndex *int
	if srcPrim.Material != nil {
		idx, err := copyImportedMaterial(dstDoc, srcDoc, *srcPrim.Material, images, samplers, textures, materials)
		if err != nil {
			return nil, err
		}
		matIndex = gltf.Index(idx)
	}

	prim := &gltf.Primitive{
		Mode:       srcPrim.Mode,
		Attributes: attrs,
		Material:   matIndex,
	}
	if len(indices) > 0 {
		prim.Indices = gltf.Index(modeler.WriteIndices(dstDoc, indices))
	}
	return prim, nil
}

// 复制外部 glTF 材质到目标文档。
//
// 功能：
// 1. 检查材质是否已经复制，避免重复创建。
// 2. 深复制原始材质结构。
// 3. 递归复制材质引用的纹理资源。
// 4. 更新纹理索引到目标文档中的新索引。
//
// 支持复制：
// - 基础颜色纹理。
// - 金属度/粗糙度纹理。
// - 法线纹理。
// - 环境遮挡纹理。
// - 自发光纹理。
//
// 返回：
// 新材质在目标 glTF 文档中的索引。
func copyImportedMaterial(dstDoc, srcDoc *gltf.Document, srcIndex int, images, samplers, textures, materials map[int]int) (int, error) {
	if idx, ok := materials[srcIndex]; ok {
		return idx, nil
	}
	if srcIndex < 0 || srcIndex >= len(srcDoc.Materials) || srcDoc.Materials[srcIndex] == nil {
		return -1, fmt.Errorf("invalid source material index: %d", srcIndex)
	}
	cloned, err := deepCopyMaterial(srcDoc.Materials[srcIndex])
	if err != nil {
		return -1, err
	}
	if cloned.PBRMetallicRoughness != nil {
		if cloned.PBRMetallicRoughness.BaseColorTexture != nil {
			idx, err := copyImportedTexture(dstDoc, srcDoc, cloned.PBRMetallicRoughness.BaseColorTexture.Index, images, samplers, textures)
			if err != nil {
				return -1, err
			}
			cloned.PBRMetallicRoughness.BaseColorTexture.Index = idx
		}
		if cloned.PBRMetallicRoughness.MetallicRoughnessTexture != nil {
			idx, err := copyImportedTexture(dstDoc, srcDoc, cloned.PBRMetallicRoughness.MetallicRoughnessTexture.Index, images, samplers, textures)
			if err != nil {
				return -1, err
			}
			cloned.PBRMetallicRoughness.MetallicRoughnessTexture.Index = idx
		}
	}
	if cloned.NormalTexture != nil {
		if cloned.NormalTexture.Index == nil {
			return -1, fmt.Errorf("source normal texture index is nil")
		}
		idx, err := copyImportedTexture(dstDoc, srcDoc, *cloned.NormalTexture.Index, images, samplers, textures)
		if err != nil {
			return -1, err
		}
		cloned.NormalTexture.Index = gltf.Index(idx)
	}
	if cloned.OcclusionTexture != nil {
		if cloned.OcclusionTexture.Index == nil {
			return -1, fmt.Errorf("source occlusion texture index is nil")
		}
		idx, err := copyImportedTexture(dstDoc, srcDoc, *cloned.OcclusionTexture.Index, images, samplers, textures)
		if err != nil {
			return -1, err
		}
		cloned.OcclusionTexture.Index = gltf.Index(idx)
	}
	if cloned.EmissiveTexture != nil {
		idx, err := copyImportedTexture(dstDoc, srcDoc, cloned.EmissiveTexture.Index, images, samplers, textures)
		if err != nil {
			return -1, err
		}
		cloned.EmissiveTexture.Index = idx
	}
	idx := len(dstDoc.Materials)
	dstDoc.Materials = append(dstDoc.Materials, cloned)
	materials[srcIndex] = idx
	return idx, nil
}

// 复制外部 glTF Texture 到目标文档。
//
// 功能：
// 1. 检查纹理缓存，避免重复复制。
// 2. 深复制纹理对象。
// 3. 复制关联 Image。
// 4. 复制关联 Sampler。
// 5. 更新纹理内部引用关系。
//
// 返回：
// 新纹理在目标文档中的索引。
func copyImportedTexture(dstDoc, srcDoc *gltf.Document, srcIndex int, images, samplers, textures map[int]int) (int, error) {
	if idx, ok := textures[srcIndex]; ok {
		return idx, nil
	}
	if srcIndex < 0 || srcIndex >= len(srcDoc.Textures) || srcDoc.Textures[srcIndex] == nil {
		return -1, fmt.Errorf("invalid source texture index: %d", srcIndex)
	}
	src := srcDoc.Textures[srcIndex]
	cloned, err := deepCopyTexture(src)
	if err != nil {
		return -1, err
	}
	if src.Source != nil {
		idx, err := copyImportedImage(dstDoc, srcDoc, *src.Source, images)
		if err != nil {
			return -1, err
		}
		cloned.Source = gltf.Index(idx)
	}
	if src.Sampler != nil {
		idx, err := copyImportedSampler(dstDoc, srcDoc, *src.Sampler, samplers)
		if err != nil {
			return -1, err
		}
		cloned.Sampler = gltf.Index(idx)
	}
	idx := len(dstDoc.Textures)
	dstDoc.Textures = append(dstDoc.Textures, cloned)
	textures[srcIndex] = idx
	return idx, nil
}

// 复制外部 glTF Image 到目标文档。
//
// 功能：
// 1. 检查图片缓存。
// 2. 如果图片存储在 BufferView 中，则读取二进制数据重新写入。
// 3. 如果图片为外部引用结构，则直接深复制。
// 4. 保存源索引和目标索引映射。
//
// 用于保证导入模型后的纹理资源完整。
func copyImportedImage(dstDoc, srcDoc *gltf.Document, srcIndex int, images map[int]int) (int, error) {
	if idx, ok := images[srcIndex]; ok {
		return idx, nil
	}
	if srcIndex < 0 || srcIndex >= len(srcDoc.Images) || srcDoc.Images[srcIndex] == nil {
		return -1, fmt.Errorf("invalid source image index: %d", srcIndex)
	}
	src := srcDoc.Images[srcIndex]
	if src.BufferView != nil && *src.BufferView >= 0 && *src.BufferView < len(srcDoc.BufferViews) {
		data, err := modeler.ReadBufferView(srcDoc, srcDoc.BufferViews[*src.BufferView])
		if err != nil {
			return -1, fmt.Errorf("read source image buffer view: %w", err)
		}
		idx, err := modeler.WriteImage(dstDoc, src.Name, src.MimeType, bytes.NewReader(data))
		if err != nil {
			return -1, fmt.Errorf("write source image to target: %w", err)
		}
		images[srcIndex] = idx
		return idx, nil
	}
	cloned, err := deepCopyImage(src)
	if err != nil {
		return -1, err
	}
	idx := len(dstDoc.Images)
	dstDoc.Images = append(dstDoc.Images, cloned)
	images[srcIndex] = idx
	return idx, nil
}

// 复制外部 glTF Sampler 到目标文档。
//
// 功能：
// 1. 检查缓存，避免重复创建。
// 2. 深复制采样器参数。
// 3. 添加到目标 glTF 文档。
//
// 返回：
// 新 Sampler 的索引。
func copyImportedSampler(dstDoc, srcDoc *gltf.Document, srcIndex int, samplers map[int]int) (int, error) {
	if idx, ok := samplers[srcIndex]; ok {
		return idx, nil
	}
	if srcIndex < 0 || srcIndex >= len(srcDoc.Samplers) || srcDoc.Samplers[srcIndex] == nil {
		return -1, fmt.Errorf("invalid source sampler index: %d", srcIndex)
	}
	cloned, err := deepCopySampler(srcDoc.Samplers[srcIndex])
	if err != nil {
		return -1, err
	}
	idx := len(dstDoc.Samplers)
	dstDoc.Samplers = append(dstDoc.Samplers, cloned)
	samplers[srcIndex] = idx
	return idx, nil
}

// 对导入模型顶点位置进行世界坐标变换。
//
// 变换顺序：
// 1. 根据节点 Scale 缩放。
// 2. 根据 Rotation 旋转。
// 3. 根据 Translation 平移。
//
// 用于将外部模型顶点转换到目标 glTF 文档统一坐标系。
func transformImportedPosition(world modelNodeTransform, p [3]float32) [3]float32 {
	scaled := [3]float32{
		p[0] * world.Scale[0],
		p[1] * world.Scale[1],
		p[2] * world.Scale[2],
	}
	rotated := rotateVec3(world.Rotation, scaled)
	return [3]float32{
		rotated[0] + world.Translation[0],
		rotated[1] + world.Translation[1],
		rotated[2] + world.Translation[2],
	}
}

// 对导入模型法线进行变换。
//
// 功能：
// 1. 根据缩放修正法线方向。
// 2. 应用旋转。
// 3. 重新归一化法线长度。
//
// 用于保证非均匀缩放后的光照计算正确。
func transformImportedNormal(world modelNodeTransform, n [3]float32) [3]float32 {
	scaled := [3]float32{
		safeNormalScale(n[0], world.Scale[0]),
		safeNormalScale(n[1], world.Scale[1]),
		safeNormalScale(n[2], world.Scale[2]),
	}
	return normalize3(rotateVec3(world.Rotation, scaled))
}

// 对法线缩放因子进行安全处理。
//
// 当缩放值接近0时，避免除零错误。
// 正常情况下返回法线分量除以缩放值后的结果。
func safeNormalScale(v, scale float32) float32 {
	if math.Abs(float64(scale)) < 1e-6 {
		return v
	}
	return v / scale
}

// 反转三角形顶点绕序。
//
// 将每个三角面的第二、第三个顶点交换，
// 用于处理模型镜像缩放导致的法线方向反转问题。
//
// 参数：
// indices 三角形索引数组。
func reverseTriangleWinding(indices []uint32) {
	for i := 0; i+2 < len(indices); i += 3 {
		indices[i+1], indices[i+2] = indices[i+2], indices[i+1]
	}
}

// 计算缩放矩阵的行列式符号。
//
// 通过三个轴向缩放值相乘判断模型是否发生镜像：
// - 正值表示保持原方向。
// - 负值表示坐标系发生翻转。
//
// 用于判断是否需要调整三角面绕序。
func determinantSign(scale [3]float32) float32 {
	return scale[0] * scale[1] * scale[2]
}

// 判断缩放向量是否为零缩放。
//
// 当三个方向缩放值均为0时返回 true。
// 用于检测无效 glTF 节点缩放数据。
func isZeroScale(scale [3]float32) bool {
	return scale[0] == 0 && scale[1] == 0 && scale[2] == 0
}

// 对四元数进行归一化处理。
//
// 功能：
// 1. 计算四元数长度。
// 2. 将四元数缩放到单位长度。
// 3. 当长度接近0时返回单位旋转四元数。
//
// 用于保证 glTF 节点旋转数据符合规范。
func normalizedQuat4(q [4]float32) [4]float32 {
	l := math.Sqrt(float64(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3]))
	if l < 1e-6 {
		return [4]float32{0, 0, 0, 1}
	}
	inv := float32(1 / l)
	return [4]float32{q[0] * inv, q[1] * inv, q[2] * inv, q[3] * inv}
}

// 计算两个四元数的乘积。
//
// 用于组合父节点和子节点的旋转变换。
// 返回结果表示两个旋转依次作用后的新旋转。
//
// 参数：
// a 第一个旋转四元数。
// b 第二个旋转四元数。
//
// 返回：
// 组合后的旋转四元数。
func quatMul4(a, b [4]float32) [4]float32 {
	return [4]float32{
		a[3]*b[0] + a[0]*b[3] + a[1]*b[2] - a[2]*b[1],
		a[3]*b[1] - a[0]*b[2] + a[1]*b[3] + a[2]*b[0],
		a[3]*b[2] + a[0]*b[1] - a[1]*b[0] + a[2]*b[3],
		a[3]*b[3] - a[0]*b[0] - a[1]*b[1] - a[2]*b[2],
	}
}

// 使用四元数旋转三维向量。
//
// 通过四元数计算向量旋转结果。
// 用于 glTF 节点变换过程中对顶点位置和方向进行旋转。
//
// 参数：
// q 旋转四元数。
// v 待旋转三维向量。
//
// 返回：
// 旋转后的三维向量。
func rotateVec3(q [4]float32, v [3]float32) [3]float32 {
	u := [3]float32{q[0], q[1], q[2]}
	s := q[3]
	uv := cross3(u, v)
	uuv := cross3(u, uv)
	return [3]float32{
		v[0] + 2*(s*uv[0]+uuv[0]),
		v[1] + 2*(s*uv[1]+uuv[1]),
		v[2] + 2*(s*uv[2]+uuv[2]),
	}
}

// 计算两个三维向量的叉积。
//
// 返回结果为垂直于两个输入向量所在平面的向量。
// 常用于计算法线、旋转以及空间方向关系。
func cross3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

// 深复制 glTF 材质对象。
//
// 通过通用结构复制方法创建独立副本。
// 用于导入外部模型时避免修改源文档中的材质数据。
func deepCopyMaterial(src *gltf.Material) (*gltf.Material, error) {
	return deepCopyStruct(src)
}

// 深复制 glTF 纹理对象。
//
// 创建纹理结构的独立副本。
// 用于模型合并过程中复制外部 Texture 数据。
func deepCopyTexture(src *gltf.Texture) (*gltf.Texture, error) {
	return deepCopyStruct(src)
}

// 深复制 glTF 图片对象。
//
// 创建图片资源结构的独立副本。
// 用于复制模型中的纹理图片信息。
func deepCopyImage(src *gltf.Image) (*gltf.Image, error) {
	return deepCopyStruct(src)
}

// 深复制 glTF 纹理采样器对象。
//
// 创建 Sampler 的独立副本。
// 用于复制纹理过滤方式和寻址模式等参数。
func deepCopySampler(src *gltf.Sampler) (*gltf.Sampler, error) {
	return deepCopyStruct(src)
}

// 使用 JSON 序列化方式深复制任意结构体。
//
// 泛型实现通用深复制功能：
// 1. 将对象序列化为 JSON。
// 2. 创建新的目标对象。
// 3. 反序列化恢复数据。
//
// 主要用于复制 glTF 中结构简单、无需共享引用的资源对象。
//
// 参数：
// src 原始对象指针。
//
// 返回：
// 新创建的对象副本。
func deepCopyStruct[T any](src *T) (*T, error) {
	if src == nil {
		return nil, nil
	}
	buf, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("deep copy marshal: %w", err)
	}
	var dst T
	if err := json.Unmarshal(buf, &dst); err != nil && err != io.EOF {
		return nil, fmt.Errorf("deep copy unmarshal: %w", err)
	}
	return &dst, nil
}

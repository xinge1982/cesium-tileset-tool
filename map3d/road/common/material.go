package common

import (
	"bytes"
	"cesium-tileset-tool/map3d/mgltf"
	"cesium-tileset-tool/map3d/texture"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// 负责将业务侧 MaterialRef / MaterialSet
// 转换为 glTF 中可以直接引用的材质索引。
//
// 主要职责：
// 1. 根据材质类型加载纹理或颜色材质。
// 2. 管理材质缓存，避免重复创建。
// 3. 设置材质渲染参数。
// 4. 处理双面显示等材质属性。
//
// 将材质解析逻辑从模型构建流程中独立出来，
// 方便不同几何 Builder 共享统一材质处理逻辑。
type MaterialResolver struct {
	doc   *gltf.Document
	cache map[string]int
}

// 创建材质解析器。
//
// 参数：
// doc glTF 文档对象，用于保存生成的材质、纹理资源。
//
// 返回：
// 初始化后的 MaterialResolver。
func NewMaterialResolver(doc *gltf.Document) *MaterialResolver {
	return &MaterialResolver{
		doc:   doc,
		cache: make(map[string]int),
	}
}

// 表示一个几何对象实际使用的多个面材质索引。
//
// 不同方向的面可以使用不同材质：
// - Top    顶面材质。
// - Side   侧面材质。
// - Bottom 底面材质。
// - Front  前侧材质。
// - Back   后侧材质。
//
// 未使用的材质索引约定为 -1。
type MaterialIndices struct {
	Top    int
	Side   int
	Bottom int
	Front  int
	Back   int
}

// 解析多面材质集合。
//
// 根据输入 MaterialSet 中配置的材质引用，
// 创建或获取对应 glTF Material 索引。
//
// 支持场景：
// - 道路面：顶面、侧面、底面。
// - 建筑模型：顶部和侧面。
// - 护栏模型：前后两个方向。
//
// 当没有单独配置 Side 或 Bottom 时，
// 默认复用 Top 材质作为兜底。
//
// 返回：
// 各面的 glTF 材质索引集合。
func (r *MaterialResolver) ResolveMaterialSet(set MaterialSet) (MaterialIndices, error) {
	out := MaterialIndices{
		Top:    -1,
		Side:   -1,
		Bottom: -1,
		Front:  -1,
		Back:   -1,
	}

	var err error
	if !isZeroMaterialRef(set.Top) {
		out.Top, err = r.ResolveMaterial(set.Top)
		if err != nil {
			return out, fmt.Errorf("resolve top material: %w", err)
		}
	}
	if !isZeroMaterialRef(set.Side) {
		out.Side, err = r.ResolveMaterial(set.Side)
		if err != nil {
			return out, fmt.Errorf("resolve side material: %w", err)
		}
	}
	if !isZeroMaterialRef(set.Bottom) {
		out.Bottom, err = r.ResolveMaterial(set.Bottom)
		if err != nil {
			return out, fmt.Errorf("resolve bottom material: %w", err)
		}
	}
	if !isZeroMaterialRef(set.Front) {
		out.Front, err = r.ResolveMaterial(set.Front)
		if err != nil {
			return out, fmt.Errorf("resolve front material: %w", err)
		}
	}
	if !isZeroMaterialRef(set.Back) {
		out.Back, err = r.ResolveMaterial(set.Back)
		if err != nil {
			return out, fmt.Errorf("resolve back material: %w", err)
		}
	}

	// 甯歌鍏滃簳锛氭湭鍗曠嫭閰嶇疆 side/bottom 鏃讹紝娌跨敤 top銆?
	if out.Top >= 0 {
		if out.Side < 0 {
			out.Side = out.Top
		}
		if out.Bottom < 0 {
			out.Bottom = out.Top
		}
	}
	return out, nil
}

// 解析单个材质引用。
//
// 根据 MaterialRef 的模式选择不同处理方式：
//
// MaterialModeTexture:
//
//	加载指定纹理文件。
//
// MaterialModeColor:
//
//	根据颜色值创建纯色材质。
//
// 默认模式:
//
//	优先使用纹理路径。
//	其次使用颜色。
//	如果都不存在则返回错误。
//
// 同时：
// 1. 使用缓存避免重复创建材质。
// 2. 调整 PBR 光照参数。
// 3. 设置材质双面渲染属性。
//
// 返回：
// glTF Material 索引。
func (r *MaterialResolver) ResolveMaterial(ref MaterialRef) (int, error) {
	if r == nil || r.doc == nil {
		return 0, fmt.Errorf("material resolver document is nil")
	}
	key := materialCacheKey(ref)
	if idx, ok := r.cache[key]; ok {
		return idx, nil
	}

	var (
		idx int
		err error
	)
	switch ref.Mode {
	case MaterialModeTexture:
		if ref.Path == "" {
			return 0, fmt.Errorf("texture material path is empty")
		}
		idx, err = r.resolveTextureMaterial(ref)
	case MaterialModeColor:
		if ref.Color == "" {
			return 0, fmt.Errorf("color material value is empty")
		}
		idx, err = mgltf.TextureMaterialForColor(r.doc, ref.Color)
	default:
		if ref.Path != "" {
			idx, err = mgltf.TextureMaterial(r.doc, ref.Path)
			break
		}
		if ref.Color != "" {
			idx, err = mgltf.TextureMaterialForColor(r.doc, ref.Color)
			break
		}
		return 0, fmt.Errorf("material ref is empty")
	}
	if err != nil {
		return 0, err
	}
	if idx >= 0 && idx < len(r.doc.Materials) && r.doc.Materials[idx] != nil {
		tuneMaterialLighting(r.doc.Materials[idx], ref)
		r.doc.Materials[idx].DoubleSided = ref.DoubleSided
	}
	r.cache[key] = idx
	return idx, nil
}

// 加载纹理材质。
//
// 功能：
// 1. 根据纹理路径创建基础材质。
// 2. 自动查找并关联同名 PBR 贴图：
//   - Normal
//   - Roughness
//   - Metalness
//
// 3. 返回材质索引。
//
// 用于支持完整 PBR 材质工作流。
func (r *MaterialResolver) resolveTextureMaterial(ref MaterialRef) (int, error) {
	idx, err := mgltf.TextureMaterial(r.doc, ref.Path)
	if err != nil {
		return 0, err
	}
	if idx < 0 || idx >= len(r.doc.Materials) || r.doc.Materials[idx] == nil {
		return idx, nil
	}
	attachSiblingPBRMaps(r.doc, r.doc.Materials[idx], ref.Path)
	return idx, nil
}

// 判断材质引用是否为空。
//
// 当材质名称、路径、颜色以及模式均为空时，
// 认为该 MaterialRef 未配置。
//
// 用于判断 MaterialSet 中是否需要处理某个面。
func isZeroMaterialRef(ref MaterialRef) bool {
	return ref.Name == "" && ref.Path == "" && ref.Color == "" && ref.Mode == ""
}

// 根据 MaterialRef 生成缓存 Key。
//
// 将材质关键属性组合成字符串，
// 用于 MaterialResolver 内部缓存判断。
//
// 相同 Key 的材质会复用已有 glTF Material。
func materialCacheKey(ref MaterialRef) string {
	return fmt.Sprintf("%s|%s|%s|%t|%s", ref.Mode, ref.Path, ref.Color, ref.DoubleSided, ref.Name)
}

// 调整 glTF 材质的光照和 PBR 参数。
//
// 根据材质类型和名称特征，对材质进行针对性优化。
//
// 主要调整内容：
// 1. MetallicFactor（金属度）。
// 2. RoughnessFactor（粗糙度）。
// 3. EmissiveFactor（自发光）。
// 4. AlphaMode（透明模式）。
// 5. KHR_materials_emissive_strength 扩展。
//
// 针对不同类型材质进行特殊处理：
// - curb       路缘石材质。
// - guardrail  护栏材质。
// - noise-wall 隔音墙材质。
//
// 目的：
// 使生成的 glTF 模型在 Cesium / PBR 渲染环境中具有更合理的视觉效果。
func tuneMaterialLighting(mat *gltf.Material, ref MaterialRef) {
	if mat == nil {
		return
	}
	if mat.PBRMetallicRoughness == nil {
		mat.PBRMetallicRoughness = &gltf.PBRMetallicRoughness{}
	}
	mat.PBRMetallicRoughness.MetallicFactor = gltf.Float(0.0)
	if mat.PBRMetallicRoughness.RoughnessFactor == nil {
		mat.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.75)
	}
	if mat.Extensions == nil {
		mat.Extensions = map[string]interface{}{}
	}

	emissive := 0.08
	if strings.Contains(strings.ToLower(ref.Name), "curb") {
		emissive = 0.02
		if mat.PBRMetallicRoughness.RoughnessFactor == nil || *mat.PBRMetallicRoughness.RoughnessFactor < 0.9 {
			mat.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.95)
		}
	}
	if strings.Contains(strings.ToLower(ref.Name), "guardrail") {
		emissive = 0.04
		mat.PBRMetallicRoughness.MetallicFactor = gltf.Float(0.18)
		mat.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.72)
	}
	if strings.Contains(strings.ToLower(ref.Name), "noise-wall") {
		emissive = 0.01
		mat.PBRMetallicRoughness.MetallicFactor = gltf.Float(0.0)
		mat.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.18)
		mat.AlphaMode = gltf.AlphaBlend
	}
	if ref.Mode == MaterialModeTexture {
		if emissive < 0.08 {
			emissive = 0.02
		} else {
			emissive = 0.12
		}
		if strings.Contains(strings.ToLower(ref.Name), "guardrail") {
			emissive = 0.03
			mat.PBRMetallicRoughness.MetallicFactor = gltf.Float(0.10)
			mat.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.82)
		}
		if strings.Contains(strings.ToLower(ref.Name), "noise-wall") {
			emissive = 0.01
			mat.PBRMetallicRoughness.MetallicFactor = gltf.Float(0.0)
			mat.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.18)
			mat.AlphaMode = gltf.AlphaBlend
		}
		if mat.PBRMetallicRoughness.BaseColorTexture != nil {
			ti := *mat.PBRMetallicRoughness.BaseColorTexture
			mat.EmissiveTexture = &gltf.TextureInfo{
				Index:    ti.Index,
				TexCoord: ti.TexCoord,
			}
		}
	}
	mat.EmissiveFactor = [3]float64{emissive, emissive, emissive}
	mat.Extensions["KHR_materials_emissive_strength"] = map[string]interface{}{
		"emissiveStrength": 1.0,
	}
}

// 根据颜色纹理路径查找并关联同目录下的 PBR 贴图。
//
// 根据基础颜色纹理命名规则自动寻找：
// - Normal 法线贴图。
// - Roughness 粗糙度贴图。
// - Metalness 金属度贴图。
//
// 例如：
// xxx_Color.jpg
//
// 自动尝试加载：
// xxx_NormalGL.jpg
// xxx_Roughness.jpg
// xxx_Metalness.jpg
//
// 并将其绑定到 glTF Material 对应的 PBR 属性中。
func attachSiblingPBRMaps(doc *gltf.Document, mat *gltf.Material, colorPath string) {
	if doc == nil || mat == nil || colorPath == "" {
		return
	}
	base := strings.TrimSuffix(colorPath, filepath.Ext(colorPath))
	if strings.HasSuffix(base, "_Color") {
		prefix := strings.TrimSuffix(base, "_Color")
		if normalPath := existingPath(prefix + "_NormalGL.jpg"); normalPath != "" {
			if texIdx, err := appendTexture(doc, normalPath); err == nil {
				mat.NormalTexture = &gltf.NormalTexture{
					Index: gltf.Index(texIdx),
					Scale: gltf.Float(1),
				}
			}
		}
		roughnessPath := existingPath(prefix + "_Roughness.jpg")
		metalnessPath := existingPath(prefix + "_Metalness.jpg")
		if roughnessPath != "" || metalnessPath != "" {
			if texIdx, err := appendMetallicRoughnessTexture(doc, roughnessPath, metalnessPath); err == nil {
				mat.PBRMetallicRoughness.MetallicRoughnessTexture = &gltf.TextureInfo{
					Index: texIdx,
				}
			}
		}
	}
}

// 判断文件路径是否存在。
//
// 如果文件存在返回原路径。
// 如果文件不存在返回空字符串。
//
// 用于自动查找可选材质贴图资源。
func existingPath(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// 将图片文件添加到 glTF 文档纹理资源中。
//
// 功能：
// 1. 读取纹理文件。
// 2. 将图片数据写入 glTF Image。
// 3. 创建对应 Texture。
// 4. 添加到目标文档。
//
// 返回：
// 新创建 Texture 在 glTF 文档中的索引。
func appendTexture(doc *gltf.Document, path string) (int, error) {
	tre, err := texture.NewTexture(path)
	if err != nil {
		return 0, err
	}
	imgIdx, err := modeler.WriteImage(doc, tre.Name(), tre.Format(), bytes.NewReader(tre.GetBuf()))
	if err != nil {
		return 0, err
	}
	doc.Textures = append(doc.Textures, &gltf.Texture{
		Source: gltf.Index(imgIdx),
	})
	return len(doc.Textures) - 1, nil
}

// 创建并添加 PBR 金属度/粗糙度纹理。
//
// glTF PBR Metallic-Roughness 工作流要求：
// - R 通道：保留。
// - G 通道：粗糙度（Roughness）。
// - B 通道：金属度（Metallic）。
//
// 本函数：
// 1. 分别读取 Roughness 和 Metalness 灰度图。
// 2. 合成为一张 RGB PNG。
// 3. 写入 glTF Image。
// 4. 创建 Texture。
//
// 返回：
// 新生成 MetallicRoughness Texture 索引。
func appendMetallicRoughnessTexture(doc *gltf.Document, roughnessPath, metalnessPath string) (int, error) {
	roughness, err := decodeGrayImage(roughnessPath)
	if err != nil {
		return 0, err
	}
	metalness, err := decodeGrayImage(metalnessPath)
	if err != nil {
		return 0, err
	}
	bounds := roughness.Bounds()
	if !bounds.Eq(metalness.Bounds()) {
		return 0, fmt.Errorf("metallic/roughness texture size mismatch")
	}
	img := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r := uint8(0)
			g := roughness.GrayAt(x, y).Y
			b := metalness.GrayAt(x, y).Y
			img.Set(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return 0, err
	}
	name := "metallic_roughness_" + filepath.Base(strings.TrimSuffix(roughnessPath, filepath.Ext(roughnessPath))) + ".png"
	imgIdx, err := modeler.WriteImage(doc, name, "image/png", bytes.NewReader(buf.Bytes()))
	if err != nil {
		return 0, err
	}
	doc.Textures = append(doc.Textures, &gltf.Texture{
		Source: gltf.Index(imgIdx),
	})
	return len(doc.Textures) - 1, nil
}

// 读取图片并转换为灰度图像。
//
// 功能：
// 1. 打开指定图片文件。
// 2. 使用 Go image 库解码图片。
// 3. 将任意格式图片转换为 image.Gray 类型。
//
// 用于处理 PBR 灰度贴图，例如：
// - Roughness 粗糙度。
// - Metalness 金属度。
//
// 参数：
// path 图片文件路径。
//
// 返回：
// 转换后的灰度图对象。
func decodeGrayImage(path string) (*image.Gray, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	_ = format
	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.Set(x, y, color.GrayModel.Convert(img.At(x, y)))
		}
	}
	return gray, nil
}

// 注册支持的图片格式解析器。
//
// 注册格式：
// - jpeg。
// - jpg。
// - png。
//
// 确保 Go image 包能够正确识别材质纹理图片格式。
func init() {
	image.RegisterFormat("jpeg", "jpeg", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("jpg", "jpg", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
}

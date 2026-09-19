package mapmodel

import (
	"bytes"
	"cesium-tileset-tool/mapmodel/aabb"
	"cesium-tileset-tool/mapmodel/mergeone"
	"cesium-tileset-tool/mapmodel/transform"
	"encoding/binary"
	"fmt"

	"github.com/qmuntal/gltf"
)

type BmOption struct {
	RootNode bool //在GLB中写入transform
	Draco    bool //启用Draco
	Zip      bool //启用gzip压缩
	Factor   float64
}

var DefaultOption = BmOption{
	RootNode: false,
	Draco:    false,
	Zip:      false,
	Factor:   1,
}

type BuildModels struct {
	models []*Model
	aabb   aabb.Box
	fields map[string][]string
	doc    *gltf.Document
	center [3]float64
	region [6]float64
	//defaultRegion [6]float64
	centerENU  *aabb.ENUFrame //ECEF 原点坐标
	scenesNode []*gltf.Node
	opt        *BmOption
	// imageDeduper is scoped to one output tile and reuses identical images
	// embedded by different source GLBs.
	imageDeduper *mergeone.ImageDeduper
}

// NewBuildModels 新建BuildModels,一定要做
func NewBuildModels(center []float64, region [4]float64, opt ...*BmOption) (*BuildModels, error) {
	var bm = &BuildModels{}
	if len(opt) > 0 {
		bm.opt = opt[0]
	} else {
		bm.opt = &DefaultOption
	}

	bm.fields = make(map[string][]string)
	bm.aabb = aabb.NewEmptyBox()
	bm.doc = gltf.NewDocument()
	bm.imageDeduper = mergeone.NewImageDeduper()
	bm.doc.Asset.Generator = "mapabc/gltf"
	bm.region = aabb.WGS84BoxToRegionDegBuf(region[0], region[1], region[2], region[3], 0, 100)

	if len(center) == 3 {
		bm.center = [3]float64{center[0], center[1], center[2]}
	} else if len(center) == 2 {
		bm.center = [3]float64{center[0], center[1], 0}
	} else {
		//TODO
		//判断输入数据的合法性
	}
	var err error
	bm.centerENU, err = aabb.NewENUFrame(bm.center[0], bm.center[1], bm.center[2])
	if err != nil {
		return nil, err
	}

	//增加扩展插件
	bm.doc.ExtensionsUsed = appendUnique(bm.doc.ExtensionsUsed, "EXT_structural_metadata")
	bm.doc.ExtensionsUsed = appendUnique(bm.doc.ExtensionsUsed, "EXT_mesh_features")
	bm.doc.ExtensionsUsed = appendUnique(bm.doc.ExtensionsUsed, "EXT_mesh_gpu_instancing")
	bm.doc.ExtensionsUsed = appendUnique(bm.doc.ExtensionsUsed, "EXT_instance_features")
	bm.doc.ExtensionsUsed = appendUnique(bm.doc.ExtensionsUsed, "KHR_materials_ior")
	bm.doc.ExtensionsUsed = appendUnique(bm.doc.ExtensionsUsed, "KHR_materials_specular")
	return bm, nil
}

func (bm *BuildModels) AddModel(model *Model) error {
	if model.Doc == nil || len(model.Doc.Scenes) == 0 || len(model.Doc.Nodes) == 0 {
		return fmt.Errorf("thid model no scenes or nodes")
	}
	//合并区域
	region, err := model.Region()
	if err != nil {
		return err
	}

	bm.region = mergeRegion(region, bm.region)
	fidx := 0
	for k, _ := range bm.fields {
		fidx = len(bm.fields[k])
		break
	}
	for k, _ := range model.Fields {
		bm.fields[k] = append(bm.fields[k], model.Fields[k]...)
	}
	//fmt.Println("fidx:", fidx, bm.fields)
	return bm.mergeGlb(model, fidx)
}

func (bm *BuildModels) AABB() *aabb.Box {
	return nil
}
func (bm *BuildModels) Transform() [16]float64 {
	return transform.GenerateTransformMatrixUP(bm.center[0], bm.center[1], bm.center[2], 0)
}
func (bm *BuildModels) Region() [6]float64 {
	return bm.region
}

func appendUnique(a []string, s string) []string {
	for _, x := range a {
		if x == s {
			return a
		}
	}
	return append(a, s)
}

func (bm *BuildModels) BuildBinary() ([]byte, error) {

	err := writeEXTStructuralMetadataStrings(bm.doc, bm.fields)
	if err != nil {
		return nil, err
	}
	//加亮
	//SwitchToPBRWithGlowTextureAware(bm.doc, 0.35, 0.9)
	// 3) buffers[0].URI 置空；byteLength = BIN chunk 实际长度

	//if bm.opt.RootNode {
	//	wrapUnderRootWithMatrix(bm.doc, bm.Transform())
	//}

	//if errM := normalizeMaterialAlphaModes(bm.doc); errM != nil {
	//	return nil, errM
	//}

	buff := new(bytes.Buffer)
	e := gltf.NewEncoder(buff)
	e.AsBinary = true
	err = e.Encode(bm.doc)
	if err != nil {
		return nil, err
	}
	return buff.Bytes(), err

}

func (bm *BuildModels) BuildGLTF() (*gltf.Document, error) {

	err := writeEXTStructuralMetadataStrings(bm.doc, bm.fields)
	return bm.doc, err
}

// fields: 每个字段是一列字符串；要求各列行数一致
func writeEXTStructuralMetadataStrings(doc *gltf.Document, fields map[string][]string) error {
	if doc == nil {
		return fmt.Errorf("nil glTF document")
	}
	if len(fields) == 0 {
		return fmt.Errorf("no fields provided")
	}

	// 1) 行数校验
	rowCount := -1
	for name, vals := range fields {
		if name == "" {
			return fmt.Errorf("empty field name")
		}
		if rowCount < 0 {
			rowCount = len(vals)
		} else if len(vals) != rowCount {
			return fmt.Errorf("inconsistent row count: field %q has %d, expected %d", name, len(vals), rowCount)
		}
	}
	if rowCount < 0 {
		return fmt.Errorf("empty fields")
	}

	ensureSingleBuffer(doc)

	// 2) 为每个字段生成 values(字节串) 与 stringOffsets(uint32[len=rowCount+1]) 两个 bufferView
	schemaProps := map[string]interface{}{} // schema.classes.Feature.properties
	tableProps := map[string]interface{}{}  // propertyTables[0].properties

	for name, vals := range fields {
		// 2.1 生成字符串拼接与偏移
		valBytes, offU32 := buildStringTable(vals)
		if len(offU32) != rowCount+1 {
			return fmt.Errorf("stringOffsets length %d != rowCount+1 (%d)", len(offU32), rowCount+1)
		}

		// 2.2 写 values -> bufferView
		_, off1 := appendBytesAligned(doc, valBytes)
		doc.BufferViews = append(doc.BufferViews, &gltf.BufferView{
			Buffer:     0,
			ByteOffset: off1,
			ByteLength: len(valBytes),
		})
		bvValues := len(doc.BufferViews) - 1

		// 2.3 写 stringOffsets(uint32 小端) -> bufferView
		offBytes := make([]byte, 4*len(offU32))
		for i, v := range offU32 {
			binary.LittleEndian.PutUint32(offBytes[4*i:], v)
		}
		_, off2 := appendBytesAligned(doc, offBytes)
		doc.BufferViews = append(doc.BufferViews, &gltf.BufferView{
			Buffer:     0,
			ByteOffset: off2,
			ByteLength: len(offBytes),
		})
		bvOffsets := len(doc.BufferViews) - 1

		// 2.4 schema 与 propertyTables 绑定（注意这里是 bufferView 索引，不是 accessor）
		schemaProps[name] = map[string]interface{}{"type": "STRING"}
		tableProps[name] = map[string]interface{}{
			"values":        bvValues,
			"stringOffsets": bvOffsets,
		}
	}

	// 3) 组装/覆盖 EXT_structural_metadata
	ext := map[string]interface{}{
		"schema": map[string]interface{}{
			"classes": map[string]interface{}{
				"Feature": map[string]interface{}{
					"properties": schemaProps,
				},
			},
		},
		"propertyTables": []map[string]interface{}{
			{
				"class":      "Feature",
				"count":      rowCount,
				"properties": tableProps,
			},
		},
	}

	if doc.Extensions == nil {
		doc.Extensions = make(map[string]interface{})
	}
	doc.Extensions["EXT_structural_metadata"] = ext

	// 4) 补充 extensionsUsed 声明
	ensureExtUsed(doc, "EXT_structural_metadata")
	//增加亮度
	return nil
}

// 把若干字符串拼接为 values 字节流 + UINT32 偏移表(长度 = len(ss)+1)
func buildStringTable(ss []string) ([]byte, []uint32) {
	var blob []byte
	offs := make([]uint32, 0, len(ss)+1)
	cur := uint32(0)
	offs = append(offs, cur) // 第一个偏移为 0
	for _, s := range ss {
		b := []byte(s) // UTF-8
		blob = append(blob, b...)
		cur += uint32(len(b))
		offs = append(offs, cur)
	}
	return blob, offs
}

func ensureExtUsed(doc *gltf.Document, name string) {
	for _, s := range doc.ExtensionsUsed {
		if s == name {
			return
		}
	}
	doc.ExtensionsUsed = append(doc.ExtensionsUsed, name)
}

func removeExtIfUnused(doc *gltf.Document, name string) {
	used := false
	for _, m := range doc.Materials {
		if m != nil && m.Extensions != nil {
			if _, ok := m.Extensions[name]; ok {
				used = true
				break
			}
		}
	}
	if !used {
		out := make([]string, 0, len(doc.ExtensionsUsed))
		for _, s := range doc.ExtensionsUsed {
			if s != name {
				out = append(out, s)
			}
		}
		doc.ExtensionsUsed = out
	}
}

func SwitchToPBRWithGlowTextureAware(doc *gltf.Document, emissiveLevel, baseColorScale float64) {
	if doc == nil {
		return
	}

	// 声明扩展（可选：用不到也没关系）
	ensure := func(name string) {
		for _, s := range doc.ExtensionsUsed {
			if s == name {
				return
			}
		}
		doc.ExtensionsUsed = append(doc.ExtensionsUsed, name)
	}
	ensure("KHR_materials_emissive_strength")

	clamp01 := func(x float64) float64 {
		if x < 0 {
			return 0
		}
		if x > 1 {
			return 1
		}
		return x
	}

	for _, m := range doc.Materials {
		if m == nil {
			continue
		}

		// 取消 Unlit，强制不透明，避免发灰/半透明
		if m.Extensions != nil {
			delete(m.Extensions, "KHR_materials_unlit")
		}

		// 不要修改 AlphaMode 和 AlphaCutoff。
		// 保留原模型的 OPAQUE、MASK 或 BLEND。
		//
		//m.AlphaMode = gltf.AlphaOpaque

		// 基础 PBR 设置
		if m.PBRMetallicRoughness == nil {
			m.PBRMetallicRoughness = &gltf.PBRMetallicRoughness{}
		}

		// 轻微调整基色亮度（只调 RGB，不动 A）
		c := m.PBRMetallicRoughness.BaseColorFactor
		if c == nil {
			c = &[4]float64{1, 1, 1, 1}
		}
		//c[0], c[1], c[2] = clamp01(c[0]*baseColorScale), clamp01(c[1]*baseColorScale), clamp01(c[2]*baseColorScale)
		//if c[3] == 0 {
		//	c[3] = 1
		//}

		// 只调整 RGB，绝对不修改 Alpha。
		c[0] = clamp01(c[0] * baseColorScale)
		c[1] = clamp01(c[1] * baseColorScale)
		c[2] = clamp01(c[2] * baseColorScale)

		m.PBRMetallicRoughness.BaseColorFactor = c

		// 非金属 & 适中粗糙，避免场景无 IBL 时过黑
		m.PBRMetallicRoughness.MetallicFactor = gltf.Float(0.0)
		if m.PBRMetallicRoughness.RoughnessFactor == nil {
			m.PBRMetallicRoughness.RoughnessFactor = gltf.Float(0.6)
		}

		// ——关键：发光跟贴图走，强度很小——
		// 有 baseColorTexture 时，共用它作为 emissiveTexture，这样发光保留原图细节，不会洗白。
		if m.PBRMetallicRoughness.BaseColorTexture != nil {
			ti := *m.PBRMetallicRoughness.BaseColorTexture
			m.EmissiveTexture = &gltf.TextureInfo{
				Index:    ti.Index,
				TexCoord: ti.TexCoord, // 跟随同一 UV
			}
			// 小幅发光（建议 0.05~0.25），避免盖住贴图
			v := clamp01(emissiveLevel)
			m.EmissiveFactor = [3]float64{v, v, v}
			if m.Extensions == nil {
				m.Extensions = map[string]interface{}{}
			}
			// emissiveStrength 用 1 就行，把强度放在 EmissiveFactor（更直观）
			m.Extensions["KHR_materials_emissive_strength"] = map[string]interface{}{"emissiveStrength": 1.0}
		} else {
			// 没贴图的就少量常量发光或不发光（看需求）
			if emissiveLevel > 0 {
				v := clamp01(emissiveLevel * 0.5) // 稍小一点
				m.EmissiveFactor = [3]float64{v, v, v}
				if m.Extensions == nil {
					m.Extensions = map[string]interface{}{}
				}
				m.Extensions["KHR_materials_emissive_strength"] = map[string]interface{}{"emissiveStrength": 1.0}
			}
		}
	}
}

/************* 目标文档基础：单一 Buffer + 4字节对齐写入 *************/

func ensureSingleBuffer(doc *gltf.Document) int {
	if len(doc.Buffers) == 0 || doc.Buffers[0] == nil {
		doc.Buffers = []*gltf.Buffer{
			{
				Data:       []byte{},
				ByteLength: 0,
			},
		}
	}
	return 0
}
func appendBytesAligned(dst *gltf.Document, b []byte) (bufIndex int, byteOffset int) {
	bufIndex = ensureSingleBuffer(dst)
	buf := dst.Buffers[bufIndex]
	off := len(buf.Data)
	if pad := (4 - (off % 4)) & 3; pad > 0 {
		buf.Data = append(buf.Data, make([]byte, pad)...)
		off += pad
	}
	buf.Data = append(buf.Data, b...)
	buf.ByteLength = len(buf.Data)
	return bufIndex, off
}

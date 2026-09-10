package mapmodel

//
//import (
//	"errors"
//	"fmt"
//	"math"
//	"path/filepath"
//	"strings"
//
//	"github.com/qmuntal/gltf"
//)
//
///************* 目标文档基础：单一 Buffer + 4字节对齐写入 *************/
//
//func ensureSingleBuffer(doc *gltf.Document) int {
//	if len(doc.Buffers) == 0 || doc.Buffers[0] == nil {
//		doc.Buffers = []*gltf.Buffer{
//			{
//				Data:       []byte{},
//				ByteLength: 0,
//			},
//		}
//	}
//	return 0
//}
//
//func appendBytesAligned(dst *gltf.Document, b []byte) (bufIndex int, byteOffset int) {
//	bufIndex = ensureSingleBuffer(dst)
//	buf := dst.Buffers[bufIndex]
//	off := len(buf.Data)
//	if pad := (4 - (off % 4)) & 3; pad > 0 {
//		buf.Data = append(buf.Data, make([]byte, pad)...)
//		off += pad
//	}
//	buf.Data = append(buf.Data, b...)
//	buf.ByteLength = len(buf.Data)
//	return bufIndex, off
//}
//
///************* 通用小工具 *************/
////
////func appendUnique(list []string, s string) []string {
////	for _, v := range list {
////		if v == s {
////			return list
////		}
////	}
////	return append(list, s)
////}
//
//func mergeExtensionsDecl(dst, src *gltf.Document) {
//	if src == nil || dst == nil {
//		return
//	}
//	for _, e := range src.ExtensionsUsed {
//		dst.ExtensionsUsed = appendUnique(dst.ExtensionsUsed, e)
//	}
//	for _, e := range src.ExtensionsRequired {
//		dst.ExtensionsRequired = appendUnique(dst.ExtensionsRequired, e)
//	}
//}
//
//func copyStringAnyMap(in map[string]interface{}) map[string]interface{} {
//	if in == nil {
//		return nil
//	}
//	out := make(map[string]interface{}, len(in))
//	for k, v := range in {
//		out[k] = v
//	}
//	return out
//}
//
//func cloneFloat64Slice(in []float64) []float64 {
//	if in == nil {
//		return nil
//	}
//	out := make([]float64, len(in))
//	copy(out, in)
//	return out
//}
//
//func inferMimeTypeByURI(uri string) string {
//	ext := strings.ToLower(filepath.Ext(uri))
//	switch ext {
//	case ".jpg", ".jpeg":
//		return "image/jpeg"
//	case ".png":
//		return "image/png"
//	case ".webp":
//		return "image/webp"
//	case ".ktx2":
//		return "image/ktx2"
//	default:
//		return ""
//	}
//}
//
////
////func decodeDataURI(uri string) ([]byte, string, error) {
////	// 仅支持：data:<mime>;base64,<data>
////	if !strings.HasPrefix(uri, "data:") {
////		return nil, "", fmt.Errorf("not data uri")
////	}
////	comma := strings.Index(uri, ",")
////	if comma < 0 {
////		return nil, "", fmt.Errorf("invalid data uri")
////	}
////	meta := uri[:comma]
////	payload := uri[comma+1:]
////
////	mime := ""
////	if strings.HasPrefix(meta, "data:") {
////		meta2 := strings.TrimPrefix(meta, "data:")
////		parts := strings.Split(meta2, ";")
////		if len(parts) > 0 {
////			mime = parts[0]
////		}
////	}
////
////	if !strings.HasSuffix(meta, ";base64") {
////		return nil, "", fmt.Errorf("only base64 data uri is supported")
////	}
////
////	b, err := base64.StdEncoding.DecodeString(payload)
////	if err != nil {
////		return nil, "", err
////	}
////	return b, mime, nil
////}
//
///************* 原始 accessor / bufferView / bytes 复制 *************/
//
//type rawCloneContext struct {
//	src, dst *gltf.Document
//
//	accessorMap   map[int]int
//	bufferViewMap map[int]int
//}
//
//func newRawCloneContext(src, dst *gltf.Document) *rawCloneContext {
//	return &rawCloneContext{
//		src:           src,
//		dst:           dst,
//		accessorMap:   map[int]int{},
//		bufferViewMap: map[int]int{},
//	}
//}
//
//func (c *rawCloneContext) cloneBufferViewRaw(srcBVIndex int) (int, error) {
//	if ni, ok := c.bufferViewMap[srcBVIndex]; ok {
//		return ni, nil
//	}
//	if srcBVIndex < 0 || srcBVIndex >= len(c.src.BufferViews) || c.src.BufferViews[srcBVIndex] == nil {
//		return -1, fmt.Errorf("invalid bufferView index %d", srcBVIndex)
//	}
//
//	srcBV := c.src.BufferViews[srcBVIndex]
//	if srcBV.Buffer < 0 || srcBV.Buffer >= len(c.src.Buffers) || c.src.Buffers[srcBV.Buffer] == nil {
//		return -1, fmt.Errorf("invalid buffer index %d for bufferView %d", srcBV.Buffer, srcBVIndex)
//	}
//
//	srcBuf := c.src.Buffers[srcBV.Buffer]
//	if len(srcBuf.Data) == 0 {
//		return -1, fmt.Errorf("source buffer %d has empty data", srcBV.Buffer)
//	}
//
//	start := srcBV.ByteOffset
//	end := start + srcBV.ByteLength
//	if start < 0 || end > len(srcBuf.Data) || start > end {
//		return -1, fmt.Errorf("bufferView %d out of range", srcBVIndex)
//	}
//
//	raw := append([]byte(nil), srcBuf.Data[start:end]...)
//	bufIdx, off := appendBytesAligned(c.dst, raw)
//
//	cp := &gltf.BufferView{
//		Buffer:     bufIdx,
//		ByteOffset: off,
//		ByteLength: len(raw),
//		ByteStride: srcBV.ByteStride,
//		Target:     srcBV.Target,
//		Name:       srcBV.Name,
//		Extensions: copyStringAnyMap(srcBV.Extensions),
//		Extras:     srcBV.Extras,
//	}
//	c.dst.BufferViews = append(c.dst.BufferViews, cp)
//	newIdx := len(c.dst.BufferViews) - 1
//	c.bufferViewMap[srcBVIndex] = newIdx
//	return newIdx, nil
//}
//
//func (c *rawCloneContext) cloneAccessorRaw(srcAccIndex int) (int, error) {
//	if ni, ok := c.accessorMap[srcAccIndex]; ok {
//		return ni, nil
//	}
//	if srcAccIndex < 0 || srcAccIndex >= len(c.src.Accessors) || c.src.Accessors[srcAccIndex] == nil {
//		return -1, fmt.Errorf("invalid accessor index %d", srcAccIndex)
//	}
//
//	srcAcc := c.src.Accessors[srcAccIndex]
//	cp := *srcAcc
//
//	if srcAcc.BufferView != nil {
//		newBV, err := c.cloneBufferViewRaw(int(*srcAcc.BufferView))
//		if err != nil {
//			return -1, err
//		}
//		cp.BufferView = gltf.Index(newBV)
//	}
//
//	// Min/Max 做深拷贝
//	if srcAcc.Min != nil {
//		cp.Min = cloneFloat64Slice(srcAcc.Min)
//	}
//	if srcAcc.Max != nil {
//		cp.Max = cloneFloat64Slice(srcAcc.Max)
//	}
//
//	cp.Extensions = copyStringAnyMap(srcAcc.Extensions)
//	cp.Extras = srcAcc.Extras
//
//	c.dst.Accessors = append(c.dst.Accessors, &cp)
//	newIdx := len(c.dst.Accessors) - 1
//	c.accessorMap[srcAccIndex] = newIdx
//	return newIdx, nil
//}
//
///************* 贴图链克隆（Image/Sampler/Texture/Material） *************/
//
//type matCopier struct {
//	src, dst *gltf.Document
//	srcDir   string
//
//	imgMap, samplerMap map[int]int
//	texMap, matMap     map[int]int
//}
//
//func newMatCopier(src, dst *gltf.Document, srcDir string) *matCopier {
//	return &matCopier{
//		src: src, dst: dst, srcDir: srcDir,
//		imgMap:     map[int]int{},
//		samplerMap: map[int]int{},
//		texMap:     map[int]int{},
//		matMap:     map[int]int{},
//	}
//}
//
//func (m *matCopier) cloneImage(i int) (int, error) {
//	if ni, ok := m.imgMap[i]; ok {
//		return ni, nil
//	}
//	if i < 0 || i >= len(m.src.Images) || m.src.Images[i] == nil {
//		return -1, fmt.Errorf("invalid image %d", i)
//	}
//
//	img := m.src.Images[i]
//	if img.BufferView == nil {
//		return -1, fmt.Errorf("image %d has no BufferView, this code only supports standard GLB", i)
//	}
//
//	bvIdx := int(*img.BufferView)
//	if bvIdx < 0 || bvIdx >= len(m.src.BufferViews) || m.src.BufferViews[bvIdx] == nil {
//		return -1, fmt.Errorf("invalid image bufferview %d", bvIdx)
//	}
//	bv := m.src.BufferViews[bvIdx]
//
//	if bv.Buffer < 0 || bv.Buffer >= len(m.src.Buffers) || m.src.Buffers[bv.Buffer] == nil {
//		return -1, fmt.Errorf("invalid image buffer %d", bv.Buffer)
//	}
//	buf := m.src.Buffers[bv.Buffer]
//	if len(buf.Data) == 0 {
//		return -1, fmt.Errorf("empty image buffer %d", bv.Buffer)
//	}
//
//	start := bv.ByteOffset
//	end := start + bv.ByteLength
//	if start < 0 || end > len(buf.Data) || start > end {
//		return -1, fmt.Errorf("image %d out of range", i)
//	}
//
//	data := append([]byte(nil), buf.Data[start:end]...)
//
//	mime := img.MimeType
//	if mime == "" {
//		return -1, fmt.Errorf("image %d mimeType is empty", i)
//	}
//
//	bufIdx, off := appendBytesAligned(m.dst, data)
//	m.dst.BufferViews = append(m.dst.BufferViews, &gltf.BufferView{
//		Buffer:     bufIdx,
//		ByteOffset: off,
//		ByteLength: len(data),
//	})
//	newBV := len(m.dst.BufferViews) - 1
//
//	cp := &gltf.Image{
//		Name:       img.Name,
//		MimeType:   mime,
//		BufferView: gltf.Index(newBV),
//		Extensions: copyStringAnyMap(img.Extensions),
//		Extras:     img.Extras,
//	}
//
//	m.dst.Images = append(m.dst.Images, cp)
//	newIdx := len(m.dst.Images) - 1
//	m.imgMap[i] = newIdx
//	return newIdx, nil
//}
//
//func (m *matCopier) cloneSampler(i int) (int, error) {
//	if ni, ok := m.samplerMap[i]; ok {
//		return ni, nil
//	}
//	if i < 0 || i >= len(m.src.Samplers) || m.src.Samplers[i] == nil {
//		return -1, fmt.Errorf("invalid sampler %d", i)
//	}
//	s := m.src.Samplers[i]
//	cp := *s
//	cp.Extensions = copyStringAnyMap(s.Extensions)
//	cp.Extras = s.Extras
//
//	m.dst.Samplers = append(m.dst.Samplers, &cp)
//	newIdx := len(m.dst.Samplers) - 1
//	m.samplerMap[i] = newIdx
//	return newIdx, nil
//}
//
//func (m *matCopier) cloneTexture(i int) (int, error) {
//	if ni, ok := m.texMap[i]; ok {
//		return ni, nil
//	}
//	if i < 0 || i >= len(m.src.Textures) || m.src.Textures[i] == nil {
//		return -1, fmt.Errorf("invalid texture %d", i)
//	}
//	tx := m.src.Textures[i]
//	cp := *tx
//
//	if tx.Source != nil {
//		ii, err := m.cloneImage(int(*tx.Source))
//		if err != nil {
//			return -1, err
//		}
//		cp.Source = gltf.Index(ii)
//	}
//	if tx.Sampler != nil {
//		si, err := m.cloneSampler(int(*tx.Sampler))
//		if err != nil {
//			return -1, err
//		}
//		cp.Sampler = gltf.Index(si)
//	}
//	cp.Extensions = copyStringAnyMap(tx.Extensions)
//	cp.Extras = tx.Extras
//
//	m.dst.Textures = append(m.dst.Textures, &cp)
//	newIdx := len(m.dst.Textures) - 1
//	m.texMap[i] = newIdx
//	return newIdx, nil
//}
//
//func (m *matCopier) cloneMaterial(i int) (int, error) {
//	if ni, ok := m.matMap[i]; ok {
//		return ni, nil
//	}
//	if i < 0 || i >= len(m.src.Materials) || m.src.Materials[i] == nil {
//		return -1, fmt.Errorf("invalid material %d", i)
//	}
//
//	mat := m.src.Materials[i]
//	cp := *mat
//	cp.Extensions = copyStringAnyMap(mat.Extensions)
//	cp.Extras = mat.Extras
//
//	if mat.PBRMetallicRoughness != nil {
//		mr := *mat.PBRMetallicRoughness
//
//		// 这里直接赋值，不做指针拷贝
//		mr.BaseColorFactor = mat.PBRMetallicRoughness.BaseColorFactor
//
//		if mr.BaseColorTexture != nil {
//			ti, err := m.cloneTexture(mr.BaseColorTexture.Index)
//			if err != nil {
//				return -1, err
//			}
//			t := *mr.BaseColorTexture
//			t.Index = ti
//			t.Extensions = copyStringAnyMap(mr.BaseColorTexture.Extensions)
//			t.Extras = mr.BaseColorTexture.Extras
//			mr.BaseColorTexture = &t
//		}
//
//		if mr.MetallicRoughnessTexture != nil {
//			ti, err := m.cloneTexture(mr.MetallicRoughnessTexture.Index)
//			if err != nil {
//				return -1, err
//			}
//			t := *mr.MetallicRoughnessTexture
//			t.Index = ti
//			t.Extensions = copyStringAnyMap(mr.MetallicRoughnessTexture.Extensions)
//			t.Extras = mr.MetallicRoughnessTexture.Extras
//			mr.MetallicRoughnessTexture = &t
//		}
//
//		cp.PBRMetallicRoughness = &mr
//	}
//
//	if mat.NormalTexture != nil && mat.NormalTexture.Index != nil {
//		ti, err := m.cloneTexture(int(*mat.NormalTexture.Index))
//		if err != nil {
//			return -1, err
//		}
//		t := *mat.NormalTexture
//		t.Index = gltf.Index(ti)
//		t.Extensions = copyStringAnyMap(mat.NormalTexture.Extensions)
//		t.Extras = mat.NormalTexture.Extras
//		cp.NormalTexture = &t
//	}
//
//	if mat.OcclusionTexture != nil && mat.OcclusionTexture.Index != nil {
//		ti, err := m.cloneTexture(int(*mat.OcclusionTexture.Index))
//		if err != nil {
//			return -1, err
//		}
//		t := *mat.OcclusionTexture
//		t.Index = gltf.Index(ti)
//		t.Extensions = copyStringAnyMap(mat.OcclusionTexture.Extensions)
//		t.Extras = mat.OcclusionTexture.Extras
//		cp.OcclusionTexture = &t
//	}
//
//	if mat.EmissiveTexture != nil {
//		ti, err := m.cloneTexture(mat.EmissiveTexture.Index)
//		if err != nil {
//			return -1, err
//		}
//		t := *mat.EmissiveTexture
//		t.Index = ti
//		t.Extensions = copyStringAnyMap(mat.EmissiveTexture.Extensions)
//		t.Extras = mat.EmissiveTexture.Extras
//		cp.EmissiveTexture = &t
//	}
//
//	// EmissiveFactor 是 [3]float64，直接赋值
//	cp.EmissiveFactor = mat.EmissiveFactor
//
//	m.dst.Materials = append(m.dst.Materials, &cp)
//	newIdx := len(m.dst.Materials) - 1
//	m.matMap[i] = newIdx
//	return newIdx, nil
//}
//
///************* Primitive 原样克隆：属性 accessor 直接复制 *************/
//
//func clonePrimitiveRaw(
//	src *gltf.Document,
//	p *gltf.Primitive,
//	dst *gltf.Document,
//	rawCtx *rawCloneContext,
//	mc *matCopier,
//	fidx int,
//) (*gltf.Primitive, error) {
//	if p == nil {
//		return nil, errors.New("nil primitive")
//	}
//	if rawCtx == nil {
//		return nil, errors.New("nil raw clone context")
//	}
//	if mc == nil {
//		return nil, errors.New("nil material copier")
//	}
//
//	out := &gltf.Primitive{
//		Mode:       p.Mode,
//		Attributes: make(gltf.PrimitiveAttributes),
//		Targets:    nil,
//		Extensions: copyStringAnyMap(p.Extensions),
//		Extras:     p.Extras,
//	}
//
//	// 原样复制所有 attributes
//	for semantic, accIndex := range p.Attributes {
//		newAcc, err := rawCtx.cloneAccessorRaw(accIndex)
//		if err != nil {
//			return nil, fmt.Errorf("clone attribute %s accessor %d: %w", semantic, accIndex, err)
//		}
//		out.Attributes[semantic] = newAcc
//	}
//
//	// indices
//	if p.Indices != nil {
//		newIdxAcc, err := rawCtx.cloneAccessorRaw(int(*p.Indices))
//		if err != nil {
//			return nil, fmt.Errorf("clone indices accessor %d: %w", int(*p.Indices), err)
//		}
//		out.Indices = gltf.Index(newIdxAcc)
//	}
//
//	// morph targets
//	if len(p.Targets) > 0 {
//		out.Targets = make([]gltf.PrimitiveAttributes, len(p.Targets))
//		for i, target := range p.Targets {
//			dstTarget := make(gltf.PrimitiveAttributes, len(target))
//			for semantic, accIndex := range target {
//				newAcc, err := rawCtx.cloneAccessorRaw(accIndex)
//				if err != nil {
//					return nil, fmt.Errorf("clone morph target %d semantic %s accessor %d: %w", i, semantic, accIndex, err)
//				}
//				dstTarget[semantic] = newAcc
//			}
//			out.Targets[i] = dstTarget
//		}
//	}
//
//	// material
//	if p.Material != nil {
//		newMat, err := mc.cloneMaterial(int(*p.Material))
//		if err != nil {
//			return nil, fmt.Errorf("clone material %d: %w", int(*p.Material), err)
//		}
//		out.Material = gltf.Index(newMat)
//	}
//
//	// 可选：附加 feature id
//	if fidx != -1 {
//		// 找 POSITION 的 accessor count 作为顶点数
//		posAccIdx, ok := out.Attributes[gltf.POSITION]
//		if !ok {
//			return nil, errors.New("POSITION attribute missing while adding _FEATURE_ID_0")
//		}
//		if posAccIdx < 0 || posAccIdx >= len(dst.Accessors) || dst.Accessors[posAccIdx] == nil {
//			return nil, fmt.Errorf("invalid cloned POSITION accessor %d", posAccIdx)
//		}
//		vertexCount := dst.Accessors[posAccIdx].Count
//		if vertexCount < 0 {
//			return nil, fmt.Errorf("invalid vertex count %d", vertexCount)
//		}
//		if fidx < 0 || fidx > math.MaxUint16 {
//			return nil, fmt.Errorf("feature id %d overflow uint16", fidx)
//		}
//
//		feat := make([]uint16, vertexCount)
//		for i := range feat {
//			feat[i] = uint16(fidx)
//		}
//		featAcc := writeUint16Accessor(dst, feat)
//		out.Attributes["_FEATURE_ID_0"] = featAcc
//
//		if out.Extensions == nil {
//			out.Extensions = map[string]interface{}{}
//		}
//		out.Extensions["EXT_mesh_features"] = map[string]interface{}{
//			"featureIds": []map[string]interface{}{
//				{
//					"attribute":     0,
//					"propertyTable": 0,
//				},
//			},
//		}
//	}
//
//	return out, nil
//}
//
//func writeUint16Accessor(dst *gltf.Document, data []uint16) int {
//	raw := make([]byte, len(data)*2)
//	for i, v := range data {
//		raw[i*2+0] = byte(v)
//		raw[i*2+1] = byte(v >> 8)
//	}
//	bufIdx, off := appendBytesAligned(dst, raw)
//	dst.BufferViews = append(dst.BufferViews, &gltf.BufferView{
//		Buffer:     bufIdx,
//		ByteOffset: off,
//		ByteLength: len(raw),
//		Target:     gltf.TargetArrayBuffer,
//	})
//	bvIdx := len(dst.BufferViews) - 1
//
//	dst.Accessors = append(dst.Accessors, &gltf.Accessor{
//		BufferView:    gltf.Index(bvIdx),
//		ByteOffset:    0,
//		ComponentType: gltf.ComponentUshort,
//		Count:         len(data),
//		Type:          gltf.AccessorScalar,
//	})
//	return len(dst.Accessors) - 1
//}
//
///************* Mesh 克隆 *************/
//
//func cloneMeshRaw(
//	src *gltf.Document,
//	meshIndex int,
//	dst *gltf.Document,
//	rawCtx *rawCloneContext,
//	mc *matCopier,
//	fidx int,
//) (int, error) {
//	if src == nil || dst == nil {
//		return -1, errors.New("nil doc")
//	}
//	if rawCtx == nil {
//		return -1, errors.New("nil raw clone context")
//	}
//	if mc == nil {
//		return -1, errors.New("nil material copier")
//	}
//	if meshIndex < 0 || meshIndex >= len(src.Meshes) || src.Meshes[meshIndex] == nil {
//		return -1, fmt.Errorf("invalid mesh index %d", meshIndex)
//	}
//
//	srcMesh := src.Meshes[meshIndex]
//	out := &gltf.Mesh{
//		Name:       srcMesh.Name,
//		Weights:    cloneFloat64Slice(srcMesh.Weights),
//		Extensions: copyStringAnyMap(srcMesh.Extensions),
//		Extras:     srcMesh.Extras,
//	}
//	out.Primitives = make([]*gltf.Primitive, 0, len(srcMesh.Primitives))
//
//	for i, p := range srcMesh.Primitives {
//		if p == nil {
//			continue
//		}
//		np, err := clonePrimitiveRaw(src, p, dst, rawCtx, mc, fidx)
//		if err != nil {
//			return -1, fmt.Errorf("primitive %d: %w", i, err)
//		}
//		out.Primitives = append(out.Primitives, np)
//	}
//
//	dst.Meshes = append(dst.Meshes, out)
//	return len(dst.Meshes) - 1, nil
//}
//
///************* 对外主函数：克隆单个 mesh *************/
//
//// srcDir 仅在源图片是 uri / data uri 时需要；
//// 标准 GLB 可传 ""。
//func CloneMeshRaw(
//	src *gltf.Document,
//	meshIndex int,
//	dst *gltf.Document,
//	srcDir string,
//	fidx int,
//) (int, error) {
//	if src == nil || dst == nil {
//		return -1, errors.New("nil doc")
//	}
//	ensureSingleBuffer(dst)
//	mergeExtensionsDecl(dst, src)
//
//	rawCtx := newRawCloneContext(src, dst)
//	mc := newMatCopier(src, dst, srcDir)
//
//	// 如果用了 feature extension，补根声明
//	if fidx != -1 {
//		dst.ExtensionsUsed = appendUnique(dst.ExtensionsUsed, "EXT_mesh_features")
//	}
//
//	return cloneMeshRaw(src, meshIndex, dst, rawCtx, mc, fidx)
//}
//
///************* 批量克隆多个 mesh：推荐用这个，便于共享映射 *************/
//
//// 返回 source mesh index -> dst mesh index
//func CloneMeshesRaw(
//	src *gltf.Document,
//	meshIndices []int,
//	dst *gltf.Document,
//	srcDir string,
//	fidx int,
//) (map[int]int, error) {
//	if src == nil || dst == nil {
//		return nil, errors.New("nil doc")
//	}
//	ensureSingleBuffer(dst)
//	mergeExtensionsDecl(dst, src)
//
//	rawCtx := newRawCloneContext(src, dst)
//	mc := newMatCopier(src, dst, srcDir)
//
//	if fidx != -1 {
//		dst.ExtensionsUsed = appendUnique(dst.ExtensionsUsed, "EXT_mesh_features")
//	}
//
//	outMap := make(map[int]int, len(meshIndices))
//	for _, mi := range meshIndices {
//		newIdx, err := cloneMeshRaw(src, mi, dst, rawCtx, mc, fidx)
//		if err != nil {
//			return nil, fmt.Errorf("clone mesh %d: %w", mi, err)
//		}
//		outMap[mi] = newIdx
//	}
//	return outMap, nil
//}
//
///************* 一个最小装配示例 *************/
//
//// 用法示例：
////  srcDoc, _ := gltf.OpenBinary("a.glb")
////  dstDoc := &gltf.Document{}
////  newMeshIdx, _ := CloneMeshRaw(srcDoc, 0, dstDoc, "", -1)
////  dstDoc.Nodes = append(dstDoc.Nodes, &gltf.Node{Mesh: gltf.Index(newMeshIdx)})
////  dstDoc.Scenes = append(dstDoc.Scenes, &gltf.Scene{Nodes: []uint32{0}})
////  dstDoc.Scene = gltf.Index(0)
////  _ = gltf.SaveBinary(dstDoc, "out.glb")

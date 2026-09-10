package mergeone

import (
	"errors"
	"fmt"
	"math"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

/*** -------------- 基础：单 Buffer + 4 字节对齐 -------------- ***/
func ensureSingleBuffer(doc *gltf.Document) int {
	if len(doc.Buffers) == 0 || doc.Buffers[0] == nil {
		doc.Buffers = append(doc.Buffers, &gltf.Buffer{Data: []byte{}})
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

/*** -------------- 原样复制 accessor / bufferView（给 UV / indices 用） -------------- ***/
type rawCloneContext struct {
	src, dst *gltf.Document

	accessorMap   map[int]int
	bufferViewMap map[int]int
}

func newRawCloneContext(src, dst *gltf.Document) *rawCloneContext {
	return &rawCloneContext{
		src:           src,
		dst:           dst,
		accessorMap:   map[int]int{},
		bufferViewMap: map[int]int{},
	}
}

func (c *rawCloneContext) cloneBufferViewRaw(srcBVIndex int) (int, error) {
	if ni, ok := c.bufferViewMap[srcBVIndex]; ok {
		return ni, nil
	}
	if srcBVIndex < 0 || srcBVIndex >= len(c.src.BufferViews) || c.src.BufferViews[srcBVIndex] == nil {
		return -1, fmt.Errorf("invalid bufferView index %d", srcBVIndex)
	}

	srcBV := c.src.BufferViews[srcBVIndex]
	if srcBV.Buffer < 0 || srcBV.Buffer >= len(c.src.Buffers) || c.src.Buffers[srcBV.Buffer] == nil {
		return -1, fmt.Errorf("invalid buffer index %d for bufferView %d", srcBV.Buffer, srcBVIndex)
	}

	srcBuf := c.src.Buffers[srcBV.Buffer]
	if len(srcBuf.Data) == 0 {
		return -1, fmt.Errorf("source buffer %d has empty data", srcBV.Buffer)
	}

	start := srcBV.ByteOffset
	end := start + srcBV.ByteLength
	if start < 0 || end > len(srcBuf.Data) || start > end {
		return -1, fmt.Errorf("bufferView %d out of range", srcBVIndex)
	}

	raw := append([]byte(nil), srcBuf.Data[start:end]...)
	bufIdx, off := appendBytesAligned(c.dst, raw)

	cp := *srcBV
	cp.Buffer = bufIdx
	cp.ByteOffset = off
	cp.ByteLength = len(raw)

	c.dst.BufferViews = append(c.dst.BufferViews, &cp)
	newIdx := len(c.dst.BufferViews) - 1
	c.bufferViewMap[srcBVIndex] = newIdx
	return newIdx, nil
}

func (c *rawCloneContext) cloneAccessorRaw(srcAccIndex int) (int, error) {
	if ni, ok := c.accessorMap[srcAccIndex]; ok {
		return ni, nil
	}
	if srcAccIndex < 0 || srcAccIndex >= len(c.src.Accessors) || c.src.Accessors[srcAccIndex] == nil {
		return -1, fmt.Errorf("invalid accessor index %d", srcAccIndex)
	}

	srcAcc := c.src.Accessors[srcAccIndex]
	cp := *srcAcc

	if srcAcc.BufferView != nil {
		newBV, err := c.cloneBufferViewRaw(int(*srcAcc.BufferView))
		if err != nil {
			return -1, err
		}
		cp.BufferView = gltf.Index(newBV)
	}

	c.dst.Accessors = append(c.dst.Accessors, &cp)
	newIdx := len(c.dst.Accessors) - 1
	c.accessorMap[srcAccIndex] = newIdx
	return newIdx, nil
}

func copyUVAndIndicesRaw(src *gltf.Document, dst *gltf.Document, rawCtx *rawCloneContext, p *gltf.Primitive, np *gltf.Primitive) error {
	if ai, ok := p.Attributes[gltf.TEXCOORD_0]; ok {
		newAcc, err := rawCtx.cloneAccessorRaw(ai)
		if err != nil {
			return fmt.Errorf("clone TEXCOORD_0 accessor %d: %w", ai, err)
		}
		np.Attributes[gltf.TEXCOORD_0] = newAcc
	}
	if p.Indices != nil {
		newAcc, err := rawCtx.cloneAccessorRaw(int(*p.Indices))
		if err != nil {
			return fmt.Errorf("clone indices accessor %d: %w", int(*p.Indices), err)
		}
		np.Indices = gltf.Index(newAcc)
	}
	return nil
}

/*** -------------- 简单 4x4 矩阵工具（列主序） -------------- ***/
type Mat4 [16]float64 // 列主序（glTF/GLM 风格）

func Identity() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}
func Mul(a, b Mat4) Mat4 {
	var r Mat4
	for c := 0; c < 4; c++ {
		for r0 := 0; r0 < 4; r0++ {
			sum := 0.0
			for k := 0; k < 4; k++ {
				sum += a[k*4+r0] * b[c*4+k]
			}
			r[c*4+r0] = sum
		}
	}
	return r
}
func TransformPos(m Mat4, v [3]float32) [3]float32 {
	x := float64(v[0])
	y := float64(v[1])
	z := float64(v[2])
	return [3]float32{
		float32(m[0]*x + m[4]*y + m[8]*z + m[12]),
		float32(m[1]*x + m[5]*y + m[9]*z + m[13]),
		float32(m[2]*x + m[6]*y + m[10]*z + m[14]),
	}
}
func TransformDirApprox(m Mat4, v [3]float32) [3]float32 {
	// 仅旋转+均匀缩放情况下可用（多数场景够用）。
	x := float64(v[0])
	y := float64(v[1])
	z := float64(v[2])
	out := [3]float32{
		float32(m[0]*x + m[4]*y + m[8]*z),
		float32(m[1]*x + m[5]*y + m[9]*z),
		float32(m[2]*x + m[6]*y + m[10]*z),
	}
	// 归一化
	l := math.Sqrt(float64(out[0]*out[0] + out[1]*out[1] + out[2]*out[2]))
	if l > 1e-8 {
		out[0] /= float32(l)
		out[1] /= float32(l)
		out[2] /= float32(l)
	}
	return out
}

/*** -------------- 扩展合并 + 贴图/材质拷贝（含 NPOT 安全 Sampler） -------------- ***/
func mergeExtDecl(dst, src *gltf.Document, names ...string) {
	has := func(list []string, v string) bool {
		for _, s := range list {
			if s == v {
				return true
			}
		}
		return false
	}
	// bring over from src
	for _, v := range src.ExtensionsUsed {
		if !has(dst.ExtensionsUsed, v) {
			dst.ExtensionsUsed = append(dst.ExtensionsUsed, v)
		}
	}
	for _, v := range src.ExtensionsRequired {
		if !has(dst.ExtensionsRequired, v) {
			dst.ExtensionsRequired = append(dst.ExtensionsRequired, v)
		}
	}
	// add custom used
	for _, v := range names {
		if !has(dst.ExtensionsUsed, v) {
			dst.ExtensionsUsed = append(dst.ExtensionsUsed, v)
		}
	}
}

type matCopier struct {
	src, dst           *gltf.Document
	imgMap, samplerMap map[int]int
	texMap, matMap     map[int]int
}

func newMatCopier(src, dst *gltf.Document) *matCopier {
	return &matCopier{
		src: src, dst: dst,
		imgMap:     map[int]int{},
		samplerMap: map[int]int{},
		texMap:     map[int]int{},
		matMap:     map[int]int{},
	}
}

func (m *matCopier) cloneImage(i int) (int, error) {
	if ni, ok := m.imgMap[i]; ok {
		return ni, nil
	}
	if i < 0 || i >= len(m.src.Images) || m.src.Images[i] == nil {
		return -1, fmt.Errorf("invalid image %d", i)
	}

	img := m.src.Images[i]
	if img.BufferView == nil {
		return -1, fmt.Errorf("image %d has no BufferView, only standard GLB is supported", i)
	}

	bvIdx := int(*img.BufferView)
	if bvIdx < 0 || bvIdx >= len(m.src.BufferViews) || m.src.BufferViews[bvIdx] == nil {
		return -1, fmt.Errorf("invalid image bufferview %d", bvIdx)
	}
	bv := m.src.BufferViews[bvIdx]

	if bv.Buffer < 0 || bv.Buffer >= len(m.src.Buffers) || m.src.Buffers[bv.Buffer] == nil {
		return -1, fmt.Errorf("invalid image buffer %d", bv.Buffer)
	}
	buf := m.src.Buffers[bv.Buffer]
	if len(buf.Data) == 0 {
		return -1, fmt.Errorf("empty image buffer %d", bv.Buffer)
	}

	start := bv.ByteOffset
	end := start + bv.ByteLength
	if start < 0 || end > len(buf.Data) || start > end {
		return -1, fmt.Errorf("image %d out of range", i)
	}

	data := append([]byte(nil), buf.Data[start:end]...)
	if img.MimeType == "" {
		return -1, fmt.Errorf("image %d mimeType is empty", i)
	}

	bufIdx, off := appendBytesAligned(m.dst, data)
	m.dst.BufferViews = append(m.dst.BufferViews, &gltf.BufferView{
		Buffer:     bufIdx,
		ByteOffset: off,
		ByteLength: len(data),
	})
	newBV := len(m.dst.BufferViews) - 1

	cp := *img
	cp.BufferView = gltf.Index(newBV)
	cp.URI = ""
	m.dst.Images = append(m.dst.Images, &cp)

	newIdx := len(m.dst.Images) - 1
	m.imgMap[i] = newIdx
	return newIdx, nil
}

func (m *matCopier) cloneSampler(i int) (int, error) {
	if ni, ok := m.samplerMap[i]; ok {
		return ni, nil
	}
	if i < 0 || i >= len(m.src.Samplers) || m.src.Samplers[i] == nil {
		return -1, fmt.Errorf("nil sampler %d", i)
	}
	s := m.src.Samplers[i]
	cp := *s // 原样拷贝，不做任何 Wrap / Filter 修改

	m.dst.Samplers = append(m.dst.Samplers, &cp)
	newIdx := len(m.dst.Samplers) - 1
	m.samplerMap[i] = newIdx
	return newIdx, nil
}

func (m *matCopier) cloneTexture(i int) (int, error) {
	if ni, ok := m.texMap[i]; ok {
		return ni, nil
	}
	if i < 0 || i >= len(m.src.Textures) || m.src.Textures[i] == nil {
		return -1, fmt.Errorf("nil texture %d", i)
	}

	tx := m.src.Textures[i]
	cp := *tx

	if tx.Source != nil {
		ii, err := m.cloneImage(int(*tx.Source))
		if err != nil {
			return -1, err
		}
		cp.Source = gltf.Index(ii)
	}

	if tx.Sampler != nil {
		si, err := m.cloneSampler(int(*tx.Sampler))
		if err != nil {
			return -1, err
		}
		cp.Sampler = gltf.Index(si)
	} else {
		// 保持 nil，不要强行补 sampler
		cp.Sampler = nil
	}

	m.dst.Textures = append(m.dst.Textures, &cp)
	newIdx := len(m.dst.Textures) - 1
	m.texMap[i] = newIdx
	return newIdx, nil
}

func (m *matCopier) cloneMaterial(i int) (int, error) {
	if ni, ok := m.matMap[i]; ok {
		return ni, nil
	}
	mat := m.src.Materials[i]
	if mat == nil {
		return -1, fmt.Errorf("nil material %d", i)
	}
	cp := *mat

	if mat.PBRMetallicRoughness != nil {
		mr := *mat.PBRMetallicRoughness

		if mr.BaseColorTexture != nil {
			ti, err := m.cloneTexture(mr.BaseColorTexture.Index)
			if err != nil {
				return -1, err
			}
			t := *mr.BaseColorTexture
			t.Index = ti
			mr.BaseColorTexture = &t
		}
		if mr.MetallicRoughnessTexture != nil {
			ti, err := m.cloneTexture(mr.MetallicRoughnessTexture.Index)
			if err != nil {
				return -1, err
			}
			t := *mr.MetallicRoughnessTexture
			t.Index = ti
			mr.MetallicRoughnessTexture = &t
		}
		cp.PBRMetallicRoughness = &mr
	}
	if mat.NormalTexture != nil && mat.NormalTexture.Index != nil {
		ti, err := m.cloneTexture(int(*mat.NormalTexture.Index))
		if err != nil {
			return -1, err
		}
		t := *mat.NormalTexture
		t.Index = gltf.Index(ti)
		cp.NormalTexture = &t
	}
	if mat.OcclusionTexture != nil && mat.OcclusionTexture.Index != nil {
		ti, err := m.cloneTexture(int(*mat.OcclusionTexture.Index))
		if err != nil {
			return -1, err
		}
		t := *mat.OcclusionTexture
		t.Index = gltf.Index(ti)
		cp.OcclusionTexture = &t
	}
	if mat.EmissiveTexture != nil {
		ti, err := m.cloneTexture(mat.EmissiveTexture.Index)
		if err != nil {
			return -1, err
		}
		t := *mat.EmissiveTexture
		t.Index = ti
		cp.EmissiveTexture = &t
	}

	// EmissiveFactor 是值类型，直接拷贝
	cp.EmissiveFactor = mat.EmissiveFactor

	m.dst.Materials = append(m.dst.Materials, &cp)
	newIdx := len(m.dst.Materials) - 1
	m.matMap[i] = newIdx
	return newIdx, nil
}

/*** -------------- 计算世界矩阵 -------------- ***/
func getNodeMatrix(n *gltf.Node) Mat4 {
	if n == nil {
		return Identity()
	}
	// 优先 Matrix
	if n.Matrix != (gltf.DefaultMatrix) {
		var m Mat4
		copy(m[:], n.Matrix[:])
		return m
	}
	// TRS -> Matrix
	T := n.Translation
	if T == (gltf.DefaultTranslation) {
		T = [3]float64{0, 0, 0}
	}
	S := n.Scale
	if S == (gltf.DefaultScale) {
		S = [3]float64{1, 1, 1}
	}
	R := n.Rotation
	if R == (gltf.DefaultRotation) {
		R = [4]float64{0, 0, 0, 1}
	}

	// 生成 4x4（列主）
	mt := Identity()
	mt[12], mt[13], mt[14] = T[0], T[1], T[2]

	x, y, z, w := R[0], R[1], R[2], R[3]
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	mr := Identity()
	mr[0] = 1 - 2*(yy+zz)
	mr[4] = 2 * (xy - wz)
	mr[8] = 2 * (xz + wy)
	mr[1] = 2 * (xy + wz)
	mr[5] = 1 - 2*(xx+zz)
	mr[9] = 2 * (yz - wx)
	mr[2] = 2 * (xz - wy)
	mr[6] = 2 * (yz + wx)
	mr[10] = 1 - 2*(xx+yy)

	ms := Identity()
	ms[0], ms[5], ms[10] = S[0], S[1], S[2]
	return Mul(mt, Mul(mr, ms))
}

func computeWorldMatrices(doc *gltf.Document) []Mat4 {
	n := len(doc.Nodes)
	world := make([]Mat4, n)
	vis := make([]bool, n)

	var dfs func(i int, parent Mat4)
	dfs = func(i int, parent Mat4) {
		if i < 0 || i >= n || doc.Nodes[i] == nil {
			return
		}
		local := getNodeMatrix(doc.Nodes[i])
		cur := Mul(parent, local)
		world[i] = cur
		vis[i] = true
		for _, c := range doc.Nodes[i].Children {
			dfs(c, cur)
		}
	}

	if len(doc.Scenes) == 0 {
		for i := range doc.Nodes {
			if !vis[i] {
				dfs(i, Identity())
			}
		}
	} else {
		for _, sc := range doc.Scenes {
			if sc == nil {
				continue
			}
			for _, root := range sc.Nodes {
				dfs(root, Identity())
			}
		}
	}
	return world
}

/*** -------------- 小工具：写 feature id -------------- ***/
func attachFeatureID(dst *gltf.Document, np *gltf.Primitive, vertexCount int, fidx int) {
	feat := make([]uint16, vertexCount)
	for i := range feat {
		feat[i] = uint16(fidx)
	}
	featAcc := modeler.WriteAccessor(dst, gltf.TargetArrayBuffer, feat)
	np.Attributes["_FEATURE_ID_0"] = featAcc

	if np.Extensions == nil {
		np.Extensions = map[string]any{}
	}
	np.Extensions["EXT_mesh_features"] = map[string]any{
		"featureIds": []any{
			map[string]any{
				"attribute":     0,
				"propertyTable": 0,
			},
		},
	}
}

/*** -------------- 核心：把所有 Mesh 合到一个 Mesh，再挂一个 Node -------------- ***/
type MergeOptions struct {
	SkipSkinned bool
	Fidx        int
}

func MergeAllToSingleMeshNode(src *gltf.Document, opt MergeOptions) (*gltf.Document, error) {
	if src == nil {
		return nil, errors.New("nil src")
	}
	dst := &gltf.Document{}
	if opt.Fidx >= 0 {
		mergeExtDecl(dst, src, "EXT_mesh_features")
	} else {
		mergeExtDecl(dst, src)
	}

	world := computeWorldMatrices(src)
	mc := newMatCopier(src, dst)
	rawCtx := newRawCloneContext(src, dst)
	ensureSingleBuffer(dst)

	outMesh := &gltf.Mesh{Name: "Merged"}
	for ni, nd := range src.Nodes {
		if nd == nil || nd.Mesh == nil {
			continue
		}
		if opt.SkipSkinned && nd.Skin != nil {
			continue
		}
		m := src.Meshes[*nd.Mesh]
		if m == nil {
			continue
		}

		wm := world[ni]

		for _, p := range m.Primitives {
			if p == nil {
				continue
			}

			posAcc, hasPos := p.Attributes[gltf.POSITION]
			if !hasPos {
				continue
			}
			pos, err := modeler.ReadPosition(src, src.Accessors[posAcc], nil)
			if err != nil {
				return nil, fmt.Errorf("read pos: %w", err)
			}

			var nrm [][3]float32
			if ai, ok := p.Attributes[gltf.NORMAL]; ok {
				nrm, err = modeler.ReadNormal(src, src.Accessors[ai], nil)
				if err != nil {
					return nil, fmt.Errorf("read normal: %w", err)
				}
			}

			for i := range pos {
				pos[i] = TransformPos(wm, pos[i])
			}
			if len(nrm) > 0 {
				for i := range nrm {
					nrm[i] = TransformDirApprox(wm, nrm[i])
				}
			}

			np := &gltf.Primitive{Mode: p.Mode}
			np.Attributes = make(gltf.PrimitiveAttributes)
			np.Attributes[gltf.POSITION] = modeler.WritePosition(dst, pos)
			if len(nrm) > 0 {
				np.Attributes[gltf.NORMAL] = modeler.WriteNormal(dst, nrm)
			}

			if err := copyUVAndIndicesRaw(src, dst, rawCtx, p, np); err != nil {
				return nil, err
			}

			if p.Material != nil {
				newMat, err := mc.cloneMaterial(int(*p.Material))
				if err != nil {
					return nil, fmt.Errorf("material %d: %w", *p.Material, err)
				}
				np.Material = gltf.Index(newMat)
			}

			if opt.Fidx >= 0 {
				attachFeatureID(dst, np, len(pos), opt.Fidx)
			}

			outMesh.Primitives = append(outMesh.Primitives, np)
		}
	}

	if len(outMesh.Primitives) == 0 {
		return nil, errors.New("no primitives collected; maybe all skinned or empty")
	}

	dst.Meshes = append(dst.Meshes, outMesh)
	newMeshIdx := len(dst.Meshes) - 1

	dst.Nodes = append(dst.Nodes, &gltf.Node{
		Name:        "MergedNode",
		Mesh:        gltf.Index(newMeshIdx),
		Matrix:      gltf.DefaultMatrix,
		Translation: gltf.DefaultTranslation,
		Rotation:    gltf.DefaultRotation,
		Scale:       gltf.DefaultScale,
	})
	root := len(dst.Nodes) - 1

	dst.Scenes = append(dst.Scenes, &gltf.Scene{
		Name:  "MergedScene",
		Nodes: []int{root},
	})
	dst.Scene = gltf.Index(len(dst.Scenes) - 1)

	return dst, nil
}

// MergeTwoToSingleMeshNode 将 docA 与 docB 压平并合并成一个 Mesh + 一个 Node
func MergeTwoToSingleMeshNode(docA, docB *gltf.Document, opt MergeOptions) (*gltf.Document, error) {
	if docA == nil || docB == nil {
		return nil, errors.New("nil src")
	}
	dst := &gltf.Document{}
	if opt.Fidx >= 0 {
		mergeExtDecl(dst, docA, "EXT_mesh_features")
		mergeExtDecl(dst, docB, "EXT_mesh_features")
	} else {
		mergeExtDecl(dst, docA)
		mergeExtDecl(dst, docB)
	}
	ensureSingleBuffer(dst)

	outMesh := &gltf.Mesh{Name: "Merged"}

	appendFrom := func(src *gltf.Document, label string) error {
		world := computeWorldMatrices(src)
		mc := newMatCopier(src, dst)
		rawCtx := newRawCloneContext(src, dst)

		for ni, nd := range src.Nodes {
			if nd == nil || nd.Mesh == nil {
				continue
			}
			if opt.SkipSkinned && nd.Skin != nil {
				continue
			}
			m := src.Meshes[*nd.Mesh]
			if m == nil {
				continue
			}
			wm := world[ni]

			for _, p := range m.Primitives {
				if p == nil {
					continue
				}

				posAcc, hasPos := p.Attributes[gltf.POSITION]
				if !hasPos {
					continue
				}
				pos, err := modeler.ReadPosition(src, src.Accessors[posAcc], nil)
				if err != nil {
					return fmt.Errorf("(%s) read pos: %w", label, err)
				}

				var nrm [][3]float32
				if ai, ok := p.Attributes[gltf.NORMAL]; ok {
					nrm, err = modeler.ReadNormal(src, src.Accessors[ai], nil)
					if err != nil {
						return fmt.Errorf("(%s) read normal: %w", label, err)
					}
				}

				for i := range pos {
					pos[i] = TransformPos(wm, pos[i])
				}
				if len(nrm) > 0 {
					for i := range nrm {
						nrm[i] = TransformDirApprox(wm, nrm[i])
					}
				}

				np := &gltf.Primitive{Mode: p.Mode}
				np.Attributes = make(gltf.PrimitiveAttributes)
				np.Attributes[gltf.POSITION] = modeler.WritePosition(dst, pos)
				if len(nrm) > 0 {
					np.Attributes[gltf.NORMAL] = modeler.WriteNormal(dst, nrm)
				}

				if err := copyUVAndIndicesRaw(src, dst, rawCtx, p, np); err != nil {
					return fmt.Errorf("(%s) %w", label, err)
				}

				if opt.Fidx >= 0 {
					attachFeatureID(dst, np, len(pos), opt.Fidx)
				}

				if p.Material != nil {
					newMat, err := mc.cloneMaterial(int(*p.Material))
					if err != nil {
						return fmt.Errorf("(%s) material %d: %w", label, *p.Material, err)
					}
					np.Material = gltf.Index(newMat)
				}

				np.Extras = map[string]any{
					"source":         label,
					"sourceNodeName": nd.Name,
					"sourceNodeIdx":  ni,
				}

				outMesh.Primitives = append(outMesh.Primitives, np)
			}
		}
		return nil
	}

	if err := appendFrom(docA, "A"); err != nil {
		return nil, err
	}
	if err := appendFrom(docB, "B"); err != nil {
		return nil, err
	}

	if len(outMesh.Primitives) == 0 {
		return nil, errors.New("no primitives collected from both documents")
	}

	dst.Meshes = append(dst.Meshes, outMesh)
	mid := len(dst.Meshes) - 1
	dst.Nodes = append(dst.Nodes, &gltf.Node{
		Name:        "MergedNode_AB",
		Mesh:        gltf.Index(mid),
		Matrix:      gltf.DefaultMatrix,
		Translation: gltf.DefaultTranslation,
		Rotation:    gltf.DefaultRotation,
		Scale:       gltf.DefaultScale,
	})
	root := len(dst.Nodes) - 1
	dst.Scenes = append(dst.Scenes, &gltf.Scene{
		Name:  "MergedScene_AB",
		Nodes: []int{root},
	})
	dst.Scene = gltf.Index(len(dst.Scenes) - 1)
	return dst, nil
}

// 把 docB 压平为 1 个 Mesh + 1 个 Node，直接追加进 docA 的场景；docA 其余内容不变。
func AppendDocBFlattenedIntoDocA(docA, docB *gltf.Document, newNode *gltf.Node, opt MergeOptions) (int, int, error) {
	if docA == nil || docB == nil {
		return -1, -1, errors.New("nil doc")
	}

	if opt.Fidx >= 0 {
		mergeExtDecl(docA, docB, "EXT_mesh_features")
	} else {
		mergeExtDecl(docA, docB)
	}
	ensureSingleBuffer(docA)

	worldB := computeWorldMatrices(docB)
	mc := newMatCopier(docB, docA)
	rawCtx := newRawCloneContext(docB, docA)

	outMesh := &gltf.Mesh{Name: fmt.Sprintf("mesh-%d", opt.Fidx)}
	for ni, nd := range docB.Nodes {
		if nd == nil || nd.Mesh == nil {
			continue
		}
		if opt.SkipSkinned && nd.Skin != nil {
			continue
		}
		srcMesh := docB.Meshes[*nd.Mesh]
		if srcMesh == nil {
			continue
		}

		wm := worldB[ni]

		for _, p := range srcMesh.Primitives {
			if p == nil {
				continue
			}

			posAcc, hasPos := p.Attributes[gltf.POSITION]
			if !hasPos {
				continue
			}
			pos, err := modeler.ReadPosition(docB, docB.Accessors[posAcc], nil)
			if err != nil {
				return -1, -1, fmt.Errorf("(B) read pos: %w", err)
			}

			var nrm [][3]float32
			if ai, ok := p.Attributes[gltf.NORMAL]; ok {
				nrm, err = modeler.ReadNormal(docB, docB.Accessors[ai], nil)
				if err != nil {
					return -1, -1, fmt.Errorf("(B) read normal: %w", err)
				}
			}

			for i := range pos {
				pos[i] = TransformPos(wm, pos[i])
			}
			if len(nrm) > 0 {
				for i := range nrm {
					nrm[i] = TransformDirApprox(wm, nrm[i])
				}
			}

			np := &gltf.Primitive{Mode: p.Mode}
			np.Attributes = make(gltf.PrimitiveAttributes)
			np.Attributes[gltf.POSITION] = modeler.WritePosition(docA, pos)
			if len(nrm) > 0 {
				np.Attributes[gltf.NORMAL] = modeler.WriteNormal(docA, nrm)
			}

			if err := copyUVAndIndicesRaw(docB, docA, rawCtx, p, np); err != nil {
				return -1, -1, fmt.Errorf("(B) %w", err)
			}

			if opt.Fidx >= 0 {
				attachFeatureID(docA, np, len(pos), opt.Fidx)
			}

			if p.Material != nil {
				newMat, err := mc.cloneMaterial(int(*p.Material))
				if err != nil {
					return -1, -1, fmt.Errorf("(B) material %d: %w", *p.Material, err)
				}
				np.Material = gltf.Index(newMat)
			}

			np.Extras = map[string]any{
				"source":         "B",
				"sourceNodeName": nd.Name,
				"sourceNodeIdx":  ni,
			}

			outMesh.Primitives = append(outMesh.Primitives, np)
		}
	}

	if len(outMesh.Primitives) == 0 {
		return -1, -1, errors.New("docB produced no primitives (maybe all skinned or empty)")
	}

	docA.Meshes = append(docA.Meshes, outMesh)
	newMeshIdx := len(docA.Meshes) - 1
	newNode.Mesh = &newMeshIdx
	docA.Nodes = append(docA.Nodes, newNode)
	newNodeIdx := len(docA.Nodes) - 1

	if len(docA.Scenes) == 0 {
		docA.Scenes = []*gltf.Scene{{
			Name:  "Default",
			Nodes: []int{newNodeIdx},
		}}
		docA.Scene = gltf.Index(0)
	} else {
		targetScene := 0
		if docA.Scene != nil {
			targetScene = int(*docA.Scene)
		}
		if targetScene < 0 || targetScene >= len(docA.Scenes) || docA.Scenes[targetScene] == nil {
			targetScene = 0
		}
		docA.Scenes[targetScene].Nodes = append(docA.Scenes[targetScene].Nodes, newNodeIdx)
	}

	return newNodeIdx, newMeshIdx, nil
}

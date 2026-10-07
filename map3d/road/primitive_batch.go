package road

import (
	"encoding/json"
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

type primitiveBatchKey struct {
	Material     int
	Mode         gltf.PrimitiveMode
	HasNormal    bool
	HasUV        bool
	HasFeatureID bool
}

type primitiveBatch struct {
	key        primitiveBatchKey
	pos        [][3]float32
	normals    [][3]float32
	uv         [][2]float32
	featureIDs []uint32
	indices    []uint32
}

type instancedNodeBatchKey struct {
	Mesh int
	Name string
}

type instancedNodeBatch struct {
	KeepNode     int
	Translations [][3]float32
	Rotations    [][4]float32
	Scales       [][3]float32
	FeatureIDs   []uint32
	RemoveNodes  []int
}

func mergeInstancedNodesByMeshAndName(doc *gltf.Document) error {
	if doc == nil || len(doc.Nodes) <= 1 {
		return nil
	}

	order := make([]instancedNodeBatchKey, 0)
	batches := map[instancedNodeBatchKey]*instancedNodeBatch{}
	for nodeIndex, node := range doc.Nodes {
		if node == nil || node.Mesh == nil || node.Extensions == nil {
			continue
		}
		translations, rotations, scales, featureIDs, ok, err := readInstancedNodeAttributes(doc, node)
		if err != nil {
			return err
		}
		if !ok || len(translations) == 0 {
			continue
		}

		key := instancedNodeBatchKey{Mesh: *node.Mesh, Name: node.Name}
		batch := batches[key]
		if batch == nil {
			batch = &instancedNodeBatch{KeepNode: nodeIndex}
			batches[key] = batch
			order = append(order, key)
		} else {
			batch.RemoveNodes = append(batch.RemoveNodes, nodeIndex)
		}
		batch.Translations = append(batch.Translations, translations...)
		batch.Rotations = append(batch.Rotations, rotations...)
		batch.Scales = append(batch.Scales, scales...)
		batch.FeatureIDs = append(batch.FeatureIDs, featureIDs...)
	}

	remove := map[int]bool{}
	for _, key := range order {
		batch := batches[key]
		if batch == nil || len(batch.RemoveNodes) == 0 {
			continue
		}
		node := doc.Nodes[batch.KeepNode]
		attrs := node.Extensions["EXT_mesh_gpu_instancing"].(map[string]any)["attributes"].(map[string]any)
		attrs["TRANSLATION"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, batch.Translations)
		attrs["ROTATION"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, batch.Rotations)
		attrs["SCALE"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, batch.Scales)
		attrs["_FEATURE_ID_0"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, batch.FeatureIDs)
		for _, nodeIndex := range batch.RemoveNodes {
			remove[nodeIndex] = true
		}
	}
	removeNodesAndRemapRefs(doc, remove)
	return nil
}

func readInstancedNodeAttributes(doc *gltf.Document, node *gltf.Node) ([][3]float32, [][4]float32, [][3]float32, []uint32, bool, error) {
	ext, ok := node.Extensions["EXT_mesh_gpu_instancing"].(map[string]any)
	if !ok {
		return nil, nil, nil, nil, false, nil
	}
	attrs, ok := ext["attributes"].(map[string]any)
	if !ok {
		return nil, nil, nil, nil, false, nil
	}

	tIndex, ok := instancingAttributeIndex(attrs, "TRANSLATION")
	if !ok {
		return nil, nil, nil, nil, false, nil
	}
	rIndex, ok := instancingAttributeIndex(attrs, "ROTATION")
	if !ok {
		return nil, nil, nil, nil, false, nil
	}
	sIndex, ok := instancingAttributeIndex(attrs, "SCALE")
	if !ok {
		return nil, nil, nil, nil, false, nil
	}
	fIndex, ok := instancingAttributeIndex(attrs, "_FEATURE_ID_0")
	if !ok {
		return nil, nil, nil, nil, false, nil
	}

	translations, err := readAccessorVec3(doc, tIndex)
	if err != nil {
		return nil, nil, nil, nil, false, fmt.Errorf("read instanced translations: %w", err)
	}
	rotations, err := readAccessorVec4(doc, rIndex)
	if err != nil {
		return nil, nil, nil, nil, false, fmt.Errorf("read instanced rotations: %w", err)
	}
	scales, err := readAccessorVec3(doc, sIndex)
	if err != nil {
		return nil, nil, nil, nil, false, fmt.Errorf("read instanced scales: %w", err)
	}
	featureIDs, err := readAccessorUint32(doc, fIndex)
	if err != nil {
		return nil, nil, nil, nil, false, fmt.Errorf("read instanced feature ids: %w", err)
	}
	if len(translations) != len(rotations) || len(translations) != len(scales) || len(translations) != len(featureIDs) {
		return nil, nil, nil, nil, false, fmt.Errorf("instanced node %q has mismatched attribute counts", node.Name)
	}
	return translations, rotations, scales, featureIDs, true, nil
}

func instancingAttributeIndex(attrs map[string]any, name string) (int, bool) {
	switch v := attrs[name].(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func readAccessorVec3(doc *gltf.Document, index int) ([][3]float32, error) {
	if index < 0 || index >= len(doc.Accessors) || doc.Accessors[index] == nil {
		return nil, fmt.Errorf("invalid accessor index %d", index)
	}
	raw, err := modeler.ReadAccessor(doc, doc.Accessors[index], nil)
	if err != nil {
		return nil, err
	}
	out, ok := raw.([][3]float32)
	if !ok {
		return nil, fmt.Errorf("accessor %d is %T, expected [][3]float32", index, raw)
	}
	return out, nil
}

func readAccessorVec4(doc *gltf.Document, index int) ([][4]float32, error) {
	if index < 0 || index >= len(doc.Accessors) || doc.Accessors[index] == nil {
		return nil, fmt.Errorf("invalid accessor index %d", index)
	}
	raw, err := modeler.ReadAccessor(doc, doc.Accessors[index], nil)
	if err != nil {
		return nil, err
	}
	out, ok := raw.([][4]float32)
	if !ok {
		return nil, fmt.Errorf("accessor %d is %T, expected [][4]float32", index, raw)
	}
	return out, nil
}

func readAccessorUint32(doc *gltf.Document, index int) ([]uint32, error) {
	if index < 0 || index >= len(doc.Accessors) || doc.Accessors[index] == nil {
		return nil, fmt.Errorf("invalid accessor index %d", index)
	}
	raw, err := modeler.ReadAccessor(doc, doc.Accessors[index], nil)
	if err != nil {
		return nil, err
	}
	out, ok := raw.([]uint32)
	if !ok {
		return nil, fmt.Errorf("accessor %d is %T, expected []uint32", index, raw)
	}
	return out, nil
}

func removeNodesAndRemapRefs(doc *gltf.Document, remove map[int]bool) {
	if len(remove) == 0 {
		return
	}
	indexMap := make(map[int]int, len(doc.Nodes)-len(remove))
	newNodes := make([]*gltf.Node, 0, len(doc.Nodes)-len(remove))
	for oldIndex, node := range doc.Nodes {
		if remove[oldIndex] {
			continue
		}
		indexMap[oldIndex] = len(newNodes)
		newNodes = append(newNodes, node)
	}

	for _, node := range newNodes {
		if node == nil || len(node.Children) == 0 {
			continue
		}
		kept := node.Children[:0]
		for _, child := range node.Children {
			if newChild, ok := indexMap[child]; ok {
				kept = append(kept, newChild)
			}
		}
		node.Children = kept
	}
	for _, scene := range doc.Scenes {
		if scene == nil || len(scene.Nodes) == 0 {
			continue
		}
		kept := scene.Nodes[:0]
		for _, nodeIndex := range scene.Nodes {
			if newIndex, ok := indexMap[nodeIndex]; ok {
				kept = append(kept, newIndex)
			}
		}
		scene.Nodes = kept
	}
	doc.Nodes = newNodes
}

func mergePrimitivesByBatch(doc *gltf.Document, primitives []*gltf.Primitive) ([]*gltf.Primitive, error) {
	if doc == nil || len(primitives) <= 1 {
		return primitives, nil
	}

	order := make([]primitiveBatchKey, 0)
	batches := make(map[primitiveBatchKey]*primitiveBatch, len(primitives))
	mergedCount := 0

	for _, primitive := range primitives {
		if primitive == nil {
			continue
		}
		batch, key, ok, err := readPrimitiveIntoBatch(doc, primitive)
		if err != nil {
			return nil, err
		}
		if !ok {
			order = append(order, primitiveBatchKey{
				Material:     -1000000 - len(order),
				Mode:         primitive.Mode,
				HasNormal:    false,
				HasUV:        false,
				HasFeatureID: false,
			})
			batches[order[len(order)-1]] = &primitiveBatch{
				key:     primitiveBatchKey{Material: -1},
				pos:     nil,
				indices: nil,
			}
			continue
		}
		dst, exists := batches[key]
		if !exists {
			order = append(order, key)
			dst = &primitiveBatch{key: key}
			batches[key] = dst
		} else {
			mergedCount++
		}
		appendPrimitiveBatch(dst, batch)
	}

	if mergedCount == 0 {
		return primitives, nil
	}

	out := make([]*gltf.Primitive, 0, len(order))
	for _, key := range order {
		batch := batches[key]
		if batch == nil || len(batch.pos) == 0 || len(batch.indices) == 0 {
			continue
		}
		primitive, err := buildPrimitiveFromBatch(doc, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, primitive)
	}
	return out, nil
}

func readPrimitiveIntoBatch(doc *gltf.Document, primitive *gltf.Primitive) (*primitiveBatch, primitiveBatchKey, bool, error) {
	if primitive == nil || primitive.Attributes == nil {
		return nil, primitiveBatchKey{}, false, nil
	}
	if primitive.Mode != 0 && primitive.Mode != gltf.PrimitiveTriangles {
		return nil, primitiveBatchKey{}, false, nil
	}

	posIndex, ok := primitive.Attributes[gltf.POSITION]
	if !ok || posIndex < 0 || posIndex >= len(doc.Accessors) {
		return nil, primitiveBatchKey{}, false, nil
	}
	pos, err := modeler.ReadPosition(doc, doc.Accessors[posIndex], nil)
	if err != nil {
		return nil, primitiveBatchKey{}, false, fmt.Errorf("read primitive position: %w", err)
	}
	if len(pos) == 0 {
		return nil, primitiveBatchKey{}, false, nil
	}

	var normals [][3]float32
	normalIndex, hasNormal := primitive.Attributes[gltf.NORMAL]
	if hasNormal {
		if normalIndex < 0 || normalIndex >= len(doc.Accessors) {
			return nil, primitiveBatchKey{}, false, nil
		}
		normals, err = modeler.ReadNormal(doc, doc.Accessors[normalIndex], nil)
		if err != nil {
			return nil, primitiveBatchKey{}, false, fmt.Errorf("read primitive normal: %w", err)
		}
		if len(normals) != len(pos) {
			return nil, primitiveBatchKey{}, false, nil
		}
	}

	var uv [][2]float32
	uvIndex, hasUV := primitive.Attributes[gltf.TEXCOORD_0]
	if hasUV {
		if uvIndex < 0 || uvIndex >= len(doc.Accessors) {
			return nil, primitiveBatchKey{}, false, nil
		}
		uv, err = modeler.ReadTextureCoord(doc, doc.Accessors[uvIndex], nil)
		if err != nil {
			return nil, primitiveBatchKey{}, false, fmt.Errorf("read primitive uv: %w", err)
		}
		if len(uv) != len(pos) {
			return nil, primitiveBatchKey{}, false, nil
		}
	}

	var featureIDs []uint32
	featureIndex, hasFeatureID := primitive.Attributes["_FEATURE_ID_0"]
	if hasFeatureID {
		if featureIndex < 0 || featureIndex >= len(doc.Accessors) {
			return nil, primitiveBatchKey{}, false, nil
		}
		featureIDs, err = modeler.ReadIndices(doc, doc.Accessors[featureIndex], nil)
		if err != nil {
			return nil, primitiveBatchKey{}, false, fmt.Errorf("read primitive feature ids: %w", err)
		}
		if len(featureIDs) != len(pos) {
			return nil, primitiveBatchKey{}, false, nil
		}
	}

	var indices []uint32
	if primitive.Indices != nil {
		if *primitive.Indices < 0 || *primitive.Indices >= len(doc.Accessors) {
			return nil, primitiveBatchKey{}, false, nil
		}
		indices, err = modeler.ReadIndices(doc, doc.Accessors[*primitive.Indices], nil)
		if err != nil {
			return nil, primitiveBatchKey{}, false, fmt.Errorf("read primitive indices: %w", err)
		}
	} else {
		indices = make([]uint32, len(pos))
		for i := range pos {
			indices[i] = uint32(i)
		}
	}
	if len(indices) == 0 {
		return nil, primitiveBatchKey{}, false, nil
	}

	mat := -1
	if primitive.Material != nil {
		mat = *primitive.Material
	}
	key := primitiveBatchKey{
		Material:     mat,
		Mode:         primitive.Mode,
		HasNormal:    hasNormal,
		HasUV:        hasUV,
		HasFeatureID: hasFeatureID,
	}
	return &primitiveBatch{
		key:        key,
		pos:        pos,
		normals:    normals,
		uv:         uv,
		featureIDs: featureIDs,
		indices:    indices,
	}, key, true, nil
}

func appendPrimitiveBatch(dst, src *primitiveBatch) {
	if dst == nil || src == nil || len(src.pos) == 0 {
		return
	}
	base := uint32(len(dst.pos))
	dst.pos = append(dst.pos, src.pos...)
	if dst.key.HasNormal {
		dst.normals = append(dst.normals, src.normals...)
	}
	if dst.key.HasUV {
		dst.uv = append(dst.uv, src.uv...)
	}
	if dst.key.HasFeatureID {
		dst.featureIDs = append(dst.featureIDs, src.featureIDs...)
	}
	for _, idx := range src.indices {
		dst.indices = append(dst.indices, base+idx)
	}
}

func buildPrimitiveFromBatch(doc *gltf.Document, batch *primitiveBatch) (*gltf.Primitive, error) {
	attrs := gltf.PrimitiveAttributes{
		gltf.POSITION: modeler.WritePosition(doc, batch.pos),
	}
	if batch.key.HasNormal {
		attrs[gltf.NORMAL] = modeler.WriteNormal(doc, batch.normals)
	}
	if batch.key.HasUV {
		attrs[gltf.TEXCOORD_0] = modeler.WriteTextureCoord(doc, batch.uv)
	}
	if batch.key.HasFeatureID {
		attrs["_FEATURE_ID_0"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, batch.featureIDs)
	}

	primitive := &gltf.Primitive{
		Mode:       batch.key.Mode,
		Attributes: attrs,
		Indices:    gltf.Index(modeler.WriteIndices(doc, batch.indices)),
	}
	if batch.key.Material >= 0 {
		primitive.Material = gltf.Index(batch.key.Material)
	}
	if batch.key.HasFeatureID {
		primitive.Extensions = map[string]any{
			"EXT_mesh_features": map[string]any{
				"featureIds": []map[string]any{
					{
						"attribute":     0,
						"propertyTable": 0,
					},
				},
			},
		}
	}
	return primitive, nil
}

func appendPrimitivesAsMesh(doc *gltf.Document, primitives []*gltf.Primitive) {
	mesh := &gltf.Mesh{Primitives: primitives}
	meshIndex := len(doc.Meshes)
	doc.Meshes = append(doc.Meshes, mesh)
	node := &gltf.Node{Mesh: gltf.Index(meshIndex)}
	nodeIndex := len(doc.Nodes)
	doc.Nodes = append(doc.Nodes, node)
	if len(doc.Scenes) == 0 {
		doc.Scenes = []*gltf.Scene{{Nodes: []int{nodeIndex}}}
		doc.Scene = gltf.Index(0)
		return
	}
	doc.Scenes[0].Nodes = append(doc.Scenes[0].Nodes, nodeIndex)
	if doc.Scene == nil {
		doc.Scene = gltf.Index(0)
	}
}

func CompactDocument(doc *gltf.Document) error {
	if doc == nil || len(doc.Buffers) == 0 || doc.Buffers[0] == nil {
		return nil
	}
	meshRefs := collectReferencedMeshes(doc)
	accessorRefs := collectReferencedAccessors(doc, meshRefs)
	bufferViewRefs := collectReferencedBufferViews(doc, accessorRefs)

	accessorMap := make(map[int]int, len(accessorRefs))
	newAccessors := make([]*gltf.Accessor, 0, len(accessorRefs))
	for _, oldIdx := range accessorRefs {
		acc, err := cloneAccessor(doc.Accessors[oldIdx])
		if err != nil {
			return err
		}
		accessorMap[oldIdx] = len(newAccessors)
		newAccessors = append(newAccessors, acc)
	}

	bufferViewMap := make(map[int]int, len(bufferViewRefs))
	newBufferViews := make([]*gltf.BufferView, 0, len(bufferViewRefs))
	newBufferData := make([]byte, 0)
	for _, oldIdx := range bufferViewRefs {
		bv := doc.BufferViews[oldIdx]
		if bv == nil {
			continue
		}
		raw, err := modeler.ReadBufferView(doc, bv)
		if err != nil {
			return fmt.Errorf("read buffer view %d: %w", oldIdx, err)
		}
		offset := len(newBufferData)
		if pad := (4 - (offset % 4)) & 3; pad > 0 {
			newBufferData = append(newBufferData, make([]byte, pad)...)
			offset += pad
		}
		newBufferData = append(newBufferData, raw...)
		clone, err := cloneBufferView(bv)
		if err != nil {
			return err
		}
		clone.Buffer = 0
		clone.ByteOffset = offset
		clone.ByteLength = len(raw)
		bufferViewMap[oldIdx] = len(newBufferViews)
		newBufferViews = append(newBufferViews, clone)
	}

	for _, acc := range newAccessors {
		if acc != nil && acc.BufferView != nil {
			if newIdx, ok := bufferViewMap[*acc.BufferView]; ok {
				acc.BufferView = gltf.Index(newIdx)
			} else {
				acc.BufferView = nil
			}
		}
	}

	remapMeshAccessors(doc, accessorMap)
	remapNodeInstancingAccessors(doc, accessorMap)
	remapImageBufferViews(doc, bufferViewMap)
	remapStructuralMetadataBufferViews(doc, bufferViewMap)

	doc.Accessors = newAccessors
	doc.BufferViews = newBufferViews
	doc.Buffers = []*gltf.Buffer{{ByteLength: len(newBufferData), Data: newBufferData}}
	return nil
}

func collectReferencedMeshes(doc *gltf.Document) []int {
	seen := map[int]bool{}
	order := make([]int, 0)
	if doc.Scene == nil || *doc.Scene >= len(doc.Scenes) || doc.Scenes[*doc.Scene] == nil {
		for i, node := range doc.Nodes {
			if node != nil {
				collectMeshesFromNode(doc, i, seen, &order)
			}
		}
		return order
	}
	for _, nodeIndex := range doc.Scenes[*doc.Scene].Nodes {
		collectMeshesFromNode(doc, nodeIndex, seen, &order)
	}
	return order
}

func collectMeshesFromNode(doc *gltf.Document, nodeIndex int, seen map[int]bool, order *[]int) {
	if nodeIndex < 0 || nodeIndex >= len(doc.Nodes) || doc.Nodes[nodeIndex] == nil {
		return
	}
	node := doc.Nodes[nodeIndex]
	if node.Mesh != nil && !seen[*node.Mesh] {
		seen[*node.Mesh] = true
		*order = append(*order, *node.Mesh)
	}
	for _, child := range node.Children {
		collectMeshesFromNode(doc, child, seen, order)
	}
}

func collectReferencedAccessors(doc *gltf.Document, meshRefs []int) []int {
	seen := map[int]bool{}
	order := make([]int, 0)
	add := func(idx int) {
		if idx < 0 || idx >= len(doc.Accessors) || seen[idx] {
			return
		}
		seen[idx] = true
		order = append(order, idx)
	}
	for _, meshIndex := range meshRefs {
		if meshIndex < 0 || meshIndex >= len(doc.Meshes) || doc.Meshes[meshIndex] == nil {
			continue
		}
		for _, primitive := range doc.Meshes[meshIndex].Primitives {
			if primitive == nil {
				continue
			}
			for _, idx := range primitive.Attributes {
				add(idx)
			}
			if primitive.Indices != nil {
				add(*primitive.Indices)
			}
		}
	}
	for _, node := range doc.Nodes {
		if node == nil || node.Extensions == nil {
			continue
		}
		ext, ok := node.Extensions["EXT_mesh_gpu_instancing"].(map[string]any)
		if !ok {
			continue
		}
		attrs, ok := ext["attributes"].(map[string]any)
		if !ok {
			continue
		}
		for _, v := range attrs {
			switch x := v.(type) {
			case int:
				add(x)
			case float64:
				add(int(x))
			}
		}
	}
	return order
}

func collectReferencedBufferViews(doc *gltf.Document, accessorRefs []int) []int {
	seen := map[int]bool{}
	order := make([]int, 0)
	add := func(idx int) {
		if idx < 0 || idx >= len(doc.BufferViews) || seen[idx] {
			return
		}
		seen[idx] = true
		order = append(order, idx)
	}
	for _, accIndex := range accessorRefs {
		acc := doc.Accessors[accIndex]
		if acc != nil && acc.BufferView != nil {
			add(*acc.BufferView)
		}
	}
	for _, img := range doc.Images {
		if img != nil && img.BufferView != nil {
			add(*img.BufferView)
		}
	}
	if doc.Extensions != nil {
		if ext, ok := doc.Extensions["EXT_structural_metadata"].(map[string]any); ok {
			if tables, ok := ext["propertyTables"].([]map[string]any); ok {
				for _, tbl := range tables {
					props, _ := tbl["properties"].(map[string]any)
					for _, raw := range props {
						prop, _ := raw.(map[string]any)
						switch v := prop["values"].(type) {
						case int:
							add(v)
						case float64:
							add(int(v))
						}
						switch v := prop["stringOffsets"].(type) {
						case int:
							add(v)
						case float64:
							add(int(v))
						}
					}
				}
			}
		}
	}
	return order
}

func remapMeshAccessors(doc *gltf.Document, accessorMap map[int]int) {
	for _, mesh := range doc.Meshes {
		if mesh == nil {
			continue
		}
		for _, primitive := range mesh.Primitives {
			if primitive == nil {
				continue
			}
			for key, idx := range primitive.Attributes {
				if newIdx, ok := accessorMap[idx]; ok {
					primitive.Attributes[key] = newIdx
				}
			}
			if primitive.Indices != nil {
				if newIdx, ok := accessorMap[*primitive.Indices]; ok {
					primitive.Indices = gltf.Index(newIdx)
				}
			}
		}
	}
}

func remapNodeInstancingAccessors(doc *gltf.Document, accessorMap map[int]int) {
	for _, node := range doc.Nodes {
		if node == nil || node.Extensions == nil {
			continue
		}
		ext, ok := node.Extensions["EXT_mesh_gpu_instancing"].(map[string]any)
		if !ok {
			continue
		}
		attrs, ok := ext["attributes"].(map[string]any)
		if !ok {
			continue
		}
		for key, raw := range attrs {
			switch v := raw.(type) {
			case int:
				if newIdx, ok := accessorMap[v]; ok {
					attrs[key] = newIdx
				}
			case float64:
				if newIdx, ok := accessorMap[int(v)]; ok {
					attrs[key] = newIdx
				}
			}
		}
	}
}

func remapImageBufferViews(doc *gltf.Document, bufferViewMap map[int]int) {
	for _, img := range doc.Images {
		if img == nil || img.BufferView == nil {
			continue
		}
		if newIdx, ok := bufferViewMap[*img.BufferView]; ok {
			img.BufferView = gltf.Index(newIdx)
		}
	}
}

func remapStructuralMetadataBufferViews(doc *gltf.Document, bufferViewMap map[int]int) {
	if doc.Extensions == nil {
		return
	}
	ext, ok := doc.Extensions["EXT_structural_metadata"].(map[string]any)
	if !ok {
		return
	}
	tables, ok := ext["propertyTables"].([]map[string]any)
	if !ok {
		return
	}
	for _, tbl := range tables {
		props, _ := tbl["properties"].(map[string]any)
		for _, raw := range props {
			prop, _ := raw.(map[string]any)
			if v, ok := prop["values"].(int); ok {
				if newIdx, ok := bufferViewMap[v]; ok {
					prop["values"] = newIdx
				}
			} else if v, ok := prop["values"].(float64); ok {
				if newIdx, ok := bufferViewMap[int(v)]; ok {
					prop["values"] = newIdx
				}
			}
			if v, ok := prop["stringOffsets"].(int); ok {
				if newIdx, ok := bufferViewMap[v]; ok {
					prop["stringOffsets"] = newIdx
				}
			} else if v, ok := prop["stringOffsets"].(float64); ok {
				if newIdx, ok := bufferViewMap[int(v)]; ok {
					prop["stringOffsets"] = newIdx
				}
			}
		}
	}
}

func cloneAccessor(src *gltf.Accessor) (*gltf.Accessor, error) {
	return cloneStruct(src)
}

func cloneBufferView(src *gltf.BufferView) (*gltf.BufferView, error) {
	return cloneStruct(src)
}

func cloneStruct[T any](src *T) (*T, error) {
	if src == nil {
		return nil, nil
	}
	buf, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst T
	if err := json.Unmarshal(buf, &dst); err != nil {
		return nil, err
	}
	return &dst, nil
}

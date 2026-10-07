package common

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

type FeatureMetadataBuilder struct {
	doc      *gltf.Document
	rows     []FeatureFields
	rowIndex map[string]uint32
}

func NewFeatureMetadataBuilder(doc *gltf.Document) *FeatureMetadataBuilder {
	return &FeatureMetadataBuilder{
		doc:      doc,
		rowIndex: map[string]uint32{},
	}
}

func (b *FeatureMetadataBuilder) RegisterFeatureRows(rows []FeatureFields, fallback FeatureFields) []uint32 {
	normalized := normalizeFeatureRows(rows, fallback)
	ids := make([]uint32, 0, len(normalized))
	for _, row := range normalized {
		normalizedRow := normalizeFeatureRow(row)
		keyBytes, _ := json.Marshal(normalizedRow)
		key := string(keyBytes)
		if id, ok := b.rowIndex[key]; ok {
			ids = append(ids, id)
			continue
		}
		b.rows = append(b.rows, normalizedRow)
		id := uint32(len(b.rows) - 1)
		b.rowIndex[key] = id
		ids = append(ids, id)
	}
	return ids
}

func (b *FeatureMetadataBuilder) AttachPrimitiveFeatureID(primitive *gltf.Primitive, vertexCount int, featureID uint32) {
	if b == nil || b.doc == nil || primitive == nil || vertexCount <= 0 {
		return
	}
	ids := make([]uint32, vertexCount)
	for i := range ids {
		ids[i] = featureID
	}
	acc := modeler.WriteAccessor(b.doc, gltf.TargetArrayBuffer, ids)
	if primitive.Attributes == nil {
		primitive.Attributes = gltf.PrimitiveAttributes{}
	}
	primitive.Attributes["_FEATURE_ID_0"] = acc
	if primitive.Extensions == nil {
		primitive.Extensions = map[string]any{}
	}
	primitive.Extensions["EXT_mesh_features"] = map[string]any{
		"featureIds": []map[string]any{
			{
				"attribute":     0,
				"propertyTable": 0,
			},
		},
	}
	ensureExtUsed(b.doc, "EXT_mesh_features")
}

func (b *FeatureMetadataBuilder) Finalize() error {
	if b == nil || b.doc == nil || len(b.rows) == 0 {
		return nil
	}

	keys := collectMetadataKeys(b.rows)
	if len(keys) == 0 {
		return nil
	}

	ensureSingleBuffer(b.doc)
	schemaProps := make(map[string]any, len(keys))
	tableProps := make(map[string]any, len(keys))
	for _, key := range keys {
		values := make([]string, len(b.rows))
		for i, row := range b.rows {
			values[i] = metadataValueString(row[key])
		}

		valueBlob, offsets := buildStringTable(values)
		_, valueOffset := appendBytesAligned(b.doc, valueBlob)
		b.doc.BufferViews = append(b.doc.BufferViews, &gltf.BufferView{
			Buffer:     0,
			ByteOffset: valueOffset,
			ByteLength: len(valueBlob),
		})
		valueView := len(b.doc.BufferViews) - 1

		offsetBytes := make([]byte, 4*len(offsets))
		for i, off := range offsets {
			binary.LittleEndian.PutUint32(offsetBytes[i*4:], off)
		}
		_, offsetViewOffset := appendBytesAligned(b.doc, offsetBytes)
		b.doc.BufferViews = append(b.doc.BufferViews, &gltf.BufferView{
			Buffer:     0,
			ByteOffset: offsetViewOffset,
			ByteLength: len(offsetBytes),
		})
		offsetView := len(b.doc.BufferViews) - 1

		schemaProps[key] = map[string]any{"type": "STRING"}
		tableProps[key] = map[string]any{
			"values":        valueView,
			"stringOffsets": offsetView,
		}
	}

	if b.doc.Extensions == nil {
		b.doc.Extensions = map[string]any{}
	}
	b.doc.Extensions["EXT_structural_metadata"] = map[string]any{
		"schema": map[string]any{
			"classes": map[string]any{
				"Feature": map[string]any{
					"properties": schemaProps,
				},
			},
		},
		"propertyTables": []map[string]any{
			{
				"class":      "Feature",
				"count":      len(b.rows),
				"properties": tableProps,
			},
		},
	}
	ensureExtUsed(b.doc, "EXT_structural_metadata")
	return nil
}

func normalizeFeatureRows(rows []FeatureFields, fallback FeatureFields) []FeatureFields {
	if len(rows) > 0 {
		out := make([]FeatureFields, 0, len(rows))
		for _, row := range rows {
			out = append(out, normalizeFeatureRow(row))
		}
		return out
	}
	return []FeatureFields{normalizeFeatureRow(fallback)}
}

func normalizeFeatureRow(row FeatureFields) FeatureFields {
	out := FeatureFields{}
	for k, v := range row {
		out[k] = v
	}
	if _, ok := out["id"]; !ok {
		out["id"] = ""
	}
	return out
}

func collectMetadataKeys(rows []FeatureFields) []string {
	keySet := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			keySet[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func metadataValueString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	case fmt.Stringer:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int8, int16, int32, int64:
		return fmt.Sprintf("%d", x)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func ensureSingleBuffer(doc *gltf.Document) int {
	if len(doc.Buffers) == 0 || doc.Buffers[0] == nil {
		doc.Buffers = append(doc.Buffers, &gltf.Buffer{Data: []byte{}})
	}
	return 0
}

func appendBytesAligned(doc *gltf.Document, data []byte) (int, int) {
	bufIndex := ensureSingleBuffer(doc)
	buf := doc.Buffers[bufIndex]
	offset := len(buf.Data)
	if pad := (4 - (offset % 4)) & 3; pad > 0 {
		buf.Data = append(buf.Data, bytes.Repeat([]byte{0}, pad)...)
		offset += pad
	}
	buf.Data = append(buf.Data, data...)
	buf.ByteLength = len(buf.Data)
	return bufIndex, offset
}

func buildStringTable(values []string) ([]byte, []uint32) {
	var blob []byte
	offsets := make([]uint32, 0, len(values)+1)
	var current uint32
	offsets = append(offsets, 0)
	for _, value := range values {
		bytesValue := []byte(value)
		blob = append(blob, bytesValue...)
		current += uint32(len(bytesValue))
		offsets = append(offsets, current)
	}
	return blob, offsets
}

func ensureExtUsed(doc *gltf.Document, name string) {
	for _, v := range doc.ExtensionsUsed {
		if v == name {
			return
		}
	}
	doc.ExtensionsUsed = append(doc.ExtensionsUsed, name)
}

package draco

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"github.com/qmuntal/gltf"
)

// -------- 保存下来的结构 --------

type SavedStructuralMeta struct {
	Schema any
	Tables []SavedTable
}
type SavedTable struct {
	Class string
	Count int
	Props []SavedProperty
}
type SavedProperty struct {
	Name               string
	Template           map[string]any // 除了 values/arrayOffsets/stringOffsets 的其他键
	HasValues          bool
	ValuesBytes        []byte
	HasArrayOffsets    bool
	ArrayOffsetsBytes  []byte
	HasStringOffsets   bool
	StringOffsetsBytes []byte
}

// 注回：把 bytes 追加成新的 bufferView，并把 propertyTables[*].properties[*] 的
// values/arrayOffsets/stringOffsets 改为 “实际新建的 bufferView 索引”。
func InjectStructuralMetadata(doc *gltf.Document, saved *SavedStructuralMeta) error {
	if doc == nil || saved == nil {
		return fmt.Errorf("nil doc/saved")
	}

	root, err := ExtEnsureMap(doc, "EXT_structural_metadata")
	if err != nil {
		return err
	}

	// 1) schema 回写（可选）
	if saved.Schema != nil {
		root["schema"] = deepCopy(saved.Schema)
	}

	// 2) propertyTables 重建
	var ptArr []any
	for ti := range saved.Tables {
		t := &saved.Tables[ti]

		props := map[string]any{}
		for _, sp := range t.Props {
			// 2.1 属性对象 = Template 的浅拷贝
			pm := map[string]any{}
			for k, v := range sp.Template {
				pm[k] = v
			}

			// 2.2 真实写入 values
			if sp.HasValues && len(sp.ValuesBytes) > 0 {
				bvIdx, err := appendBytesAsBufferViewAligned(doc, sp.ValuesBytes, 0)
				if err != nil {
					return fmt.Errorf("table %d prop %q append values: %w", ti, sp.Name, err)
				}
				pm["values"] = bvIdx // ★★ 用真实的新下标
			} else {
				delete(pm, "values") // 保守处理
			}

			// 2.3 真实写入 arrayOffsets
			if sp.HasArrayOffsets && len(sp.ArrayOffsetsBytes) > 0 {
				bvIdx, err := appendBytesAsBufferViewAligned(doc, sp.ArrayOffsetsBytes, 0)
				if err != nil {
					return fmt.Errorf("table %d prop %q append arrayOffsets: %w", ti, sp.Name, err)
				}
				pm["arrayOffsets"] = bvIdx
			} else {
				delete(pm, "arrayOffsets")
			}

			// 2.4 真实写入 stringOffsets
			if sp.HasStringOffsets && len(sp.StringOffsetsBytes) > 0 {
				bvIdx, err := appendBytesAsBufferViewAligned(doc, sp.StringOffsetsBytes, 0)
				if err != nil {
					return fmt.Errorf("table %d prop %q append stringOffsets: %w", ti, sp.Name, err)
				}
				pm["stringOffsets"] = bvIdx
			} else {
				delete(pm, "stringOffsets")
			}

			props[sp.Name] = pm
		}

		// 2.5 组装 table
		tm := map[string]any{
			"class":      t.Class,
			"count":      t.Count,
			"properties": props,
		}
		ptArr = append(ptArr, tm)
	}
	root["propertyTables"] = ptArr

	// 3) 填 extensionsUsed / Required（保守确保存在）
	ensureExtUsage(doc, "EXT_structural_metadata", false)

	// 4) 校验：所有写回的下标必须在范围内
	if err := validateMetaRefs(doc); err != nil {
		return err
	}
	return nil
}

func ensureExtUsage(doc *gltf.Document, ext string, required bool) {
	has := func(list []string, x string) bool {
		for _, s := range list {
			if s == x {
				return true
			}
		}
		return false
	}
	if !has(doc.ExtensionsUsed, ext) {
		doc.ExtensionsUsed = append(doc.ExtensionsUsed, ext)
	}
	if required && !has(doc.ExtensionsRequired, ext) {
		doc.ExtensionsRequired = append(doc.ExtensionsRequired, ext)
	}
}

// 简单校验：EXT_structural_metadata 中的各个 bufferView 下标必须 < len(bufferViews)
func validateMetaRefs(doc *gltf.Document) error {
	root, ok, _ := ExtGetMap(doc, "EXT_structural_metadata")
	if !ok {
		return fmt.Errorf("EXT_structural_metadata missing after inject")
	}
	nBV := len(doc.BufferViews)
	rawPT, _ := root["propertyTables"]
	ptArr, _ := rawPT.([]any)
	for ti, it := range ptArr {
		tm, _ := it.(map[string]any)
		props, _ := tm["properties"].(map[string]any)
		for pname, pit := range props {
			pm, _ := pit.(map[string]any)
			for _, key := range []string{"values", "arrayOffsets", "stringOffsets"} {
				if v, ok := pm[key]; ok {
					if idx, ok2 := toInt(v); ok2 {
						if idx < 0 || idx >= nBV {
							return fmt.Errorf("propertyTables[%d].properties[%s].%s = %d out of range (bufferViews=%d)",
								ti, pname, key, idx, nBV)
						}
					}
				}
			}
		}
	}
	return nil
}

// ExtractStructuralMetadata 从 doc 中提取 EXT_structural_metadata：
//  1. 深拷贝 schema；2) 对每个 propertyTables[*].properties[*]，把
//     values / arrayOffsets / stringOffsets 指向的 bufferView 原始字节读出来，
//     作为 SavedProperty 的 *_Bytes 保存，其他字段原样存入 Template。
func ExtractStructuralMetadata(doc *gltf.Document, baseDir string) (*SavedStructuralMeta, error) {
	if doc == nil {
		return nil, fmt.Errorf("nil doc")
	}

	// ★ 关键改动：兼容 Raw JSON / []byte / string 的扩展取法
	root, ok, err := ExtGetMap(doc, "EXT_structural_metadata")
	if err != nil {
		return nil, fmt.Errorf("parse EXT_structural_metadata: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("EXT_structural_metadata not found")
	}

	out := &SavedStructuralMeta{}

	// 1) schema 深拷贝（若存在）
	if sc, has := root["schema"]; has && sc != nil {
		out.Schema = deepCopy(sc)
	}

	// 2) 遍历 propertyTables 数组
	rawPT, _ := root["propertyTables"]
	ptArr, _ := rawPT.([]any)
	if len(ptArr) == 0 {
		return nil, fmt.Errorf("no propertyTables in EXT_structural_metadata")
	}

	for ti, it := range ptArr {
		tm, _ := it.(map[string]any)
		if tm == nil {
			return nil, fmt.Errorf("propertyTables[%d] is not an object", ti)
		}

		st := SavedTable{}
		if cls, ok := tm["class"].(string); ok {
			st.Class = cls
		}
		if cnt, ok := toInt(tm["count"]); ok {
			st.Count = cnt
		}

		propsObj, _ := tm["properties"].(map[string]any)
		if len(propsObj) == 0 {
			// 允许空，但给个提示
			// return nil, fmt.Errorf("propertyTables[%d] has empty properties", ti)
		}

		for pname, pit := range propsObj {
			pm, _ := pit.(map[string]any)
			if pm == nil {
				return nil, fmt.Errorf("propertyTables[%d].properties[%q] not an object", ti, pname)
			}

			sp := SavedProperty{
				Name:     pname,
				Template: shallowCopyWithout(pm, "values", "arrayOffsets", "stringOffsets"),
			}

			// values
			if vi, ok := toInt(pm["values"]); ok {
				b, err := readBufferViewBytes(doc, vi, baseDir)
				if err != nil {
					return nil, fmt.Errorf("table %d prop %q read values bv=%d: %w", ti, pname, vi, err)
				}
				sp.HasValues = true
				sp.ValuesBytes = b
			}

			// arrayOffsets
			if ai, ok := toInt(pm["arrayOffsets"]); ok {
				b, err := readBufferViewBytes(doc, ai, baseDir)
				if err != nil {
					return nil, fmt.Errorf("table %d prop %q read arrayOffsets bv=%d: %w", ti, pname, ai, err)
				}
				sp.HasArrayOffsets = true
				sp.ArrayOffsetsBytes = b
			}

			// stringOffsets
			if si, ok := toInt(pm["stringOffsets"]); ok {
				b, err := readBufferViewBytes(doc, si, baseDir)
				if err != nil {
					return nil, fmt.Errorf("table %d prop %q read stringOffsets bv=%d: %w", ti, pname, si, err)
				}
				sp.HasStringOffsets = true
				sp.StringOffsetsBytes = b
			}

			st.Props = append(st.Props, sp)
		}

		out.Tables = append(out.Tables, st)
	}

	return out, nil
}

// -------------------- 辅助：bufferView 读/写与工具 --------------------

func readBufferViewBytes(doc *gltf.Document, bvIdx int, baseDir string) ([]byte, error) {
	if bvIdx < 0 || bvIdx >= len(doc.BufferViews) || doc.BufferViews[bvIdx] == nil {
		return nil, fmt.Errorf("invalid bufferview %d", bvIdx)
	}
	bv := doc.BufferViews[bvIdx]
	if bv == nil {
		return nil, fmt.Errorf("bufferview %d missing buffer index", bvIdx)
	}
	bi := bv.Buffer
	if bi < 0 || bi >= len(doc.Buffers) || doc.Buffers[bi] == nil {
		return nil, fmt.Errorf("invalid buffer %d", bi)
	}
	buf := doc.Buffers[bi]

	// 确保 Data 已加载
	if len(buf.Data) == 0 {
		if buf.URI == "" {
			return nil, fmt.Errorf("buffer %d: empty data and empty URI", bi)
		}
		data, err := loadBufferURI(buf.URI, baseDir)
		if err != nil {
			return nil, err
		}
		buf.Data = data
		doc.Buffers[bi] = buf
	}

	start := int(bv.ByteOffset)
	end := start + int(bv.ByteLength)
	if start < 0 || end > len(buf.Data) {
		return nil, fmt.Errorf("bufferview %d out of range", bvIdx)
	}
	out := make([]byte, int(bv.ByteLength))
	copy(out, buf.Data[start:end])
	return out, nil
}

func appendBufferView(doc *gltf.Document, raw []byte) (int, error) {
	bufIdx := ensureSingleBuffer(doc)
	// 对齐到 4 字节
	pad := (4 - (len(doc.Buffers[bufIdx].Data))%4) % 4
	if pad > 0 {
		doc.Buffers[bufIdx].Data = append(doc.Buffers[bufIdx].Data, make([]byte, pad)...)
	}
	byteOffset := len(doc.Buffers[bufIdx].Data)
	doc.Buffers[bufIdx].Data = append(doc.Buffers[bufIdx].Data, raw...)
	byteLength := len(raw)
	doc.Buffers[bufIdx].ByteLength = len(doc.Buffers[bufIdx].Data)

	bv := &gltf.BufferView{
		Buffer:     bufIdx,
		ByteOffset: byteOffset,
		ByteLength: byteLength,
	}
	doc.BufferViews = append(doc.BufferViews, bv)
	return len(doc.BufferViews) - 1, nil
}

func ensureSingleBuffer(doc *gltf.Document) int {
	if len(doc.Buffers) == 0 || doc.Buffers[0] == nil {
		doc.Buffers = []*gltf.Buffer{{}}
	}
	if doc.Buffers[0].Data == nil {
		doc.Buffers[0].Data = make([]byte, 0)
	}
	return 0
}

func loadBufferURI(uri, baseDir string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		// data:...;base64,XXXX
		comma := strings.Index(uri, ",")
		if comma < 0 {
			return nil, fmt.Errorf("invalid data URI")
		}
		dataPart := uri[comma+1:]
		return base64.StdEncoding.DecodeString(dataPart)
	}
	path := uri
	if !filepath.IsAbs(path) && baseDir != "" {
		path = filepath.Join(baseDir, path)
	}
	return ioutil.ReadFile(path)
}

func u32(v uint32) *uint32 { return &v }

// 取 ext map

// ExtGetMap 只读获取扩展为 map。存在则返回 map 和 true；不存在返回 nil, false。
// 若是 Raw JSON，会自动解码为 map，但不会回写到 doc（避免无意修改）。
func ExtGetMap(doc *gltf.Document, name string) (map[string]any, bool, error) {
	if doc == nil || doc.Extensions == nil {
		return nil, false, nil
	}
	v, ok := doc.Extensions[name]
	if !ok || v == nil {
		return nil, false, nil
	}
	switch t := v.(type) {
	case map[string]any:
		return t, true, nil
	case []byte:
		var m map[string]any
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, false, fmt.Errorf("unmarshal %s: %w", name, err)
		}
		return m, true, nil
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, false, fmt.Errorf("unmarshal %s: %w", name, err)
		}
		return m, true, nil
	case string:
		// 偶尔也会是 JSON 字符串
		var m map[string]any
		if err := json.Unmarshal([]byte(t), &m); err != nil {
			return nil, false, fmt.Errorf("unmarshal string %s: %w", name, err)
		}
		return m, true, nil
	default:
		return nil, false, fmt.Errorf("unexpected %s type: %T", name, v)
	}
}

// ExtEnsureMap 读出扩展为 map；如是 Raw JSON 会“解码并回写”；不存在则创建空 map 并回写。
// 用在“要修改扩展并保存”的场景。
func ExtEnsureMap(doc *gltf.Document, name string) (map[string]any, error) {
	if doc == nil {
		return nil, fmt.Errorf("nil doc")
	}
	if doc.Extensions == nil {
		doc.Extensions = map[string]any{}
	}
	if v, ok := doc.Extensions[name]; ok && v != nil {
		switch t := v.(type) {
		case map[string]any:
			return t, nil
		case []byte:
			var m map[string]any
			if err := json.Unmarshal(t, &m); err != nil {
				return nil, fmt.Errorf("unmarshal %s: %w", name, err)
			}
			doc.Extensions[name] = m // 回写为 map，后续可直接编辑
			return m, nil
		case json.RawMessage:
			var m map[string]any
			if err := json.Unmarshal(t, &m); err != nil {
				return nil, fmt.Errorf("unmarshal %s: %w", name, err)
			}
			doc.Extensions[name] = m
			return m, nil
		case string:
			var m map[string]any
			if err := json.Unmarshal([]byte(t), &m); err != nil {
				return nil, fmt.Errorf("unmarshal string %s: %w", name, err)
			}
			doc.Extensions[name] = m
			return m, nil
		default:
			return nil, fmt.Errorf("unexpected %s type: %T", name, v)
		}
	}
	// 不存在则新建
	m := map[string]any{}
	doc.Extensions[name] = m
	return m, nil
}
func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		// JSON number
		return int(t), true
	case string:
		if t == "" {
			return 0, false
		}
		if n, err := strconv.Atoi(t); err == nil {
			return n, true
		}
	}
	return 0, false
}

func shallowCopy(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	n := make(map[string]any, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}

func shallowCopyWithout(m map[string]any, keys ...string) map[string]any {
	if m == nil {
		return nil
	}

	n := make(map[string]any, len(m))
	for k, v := range m {
		if slices.Contains(keys, k) {
			continue
		}
		n[k] = v
	}
	return n
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		n := make(map[string]any, len(t))
		for k, v2 := range t {
			n[k] = deepCopy(v2)
		}
		return n
	case []any:
		n := make([]any, len(t))
		for i, v2 := range t {
			n[i] = deepCopy(v2)
		}
		return n
	default:
		return t
	}
}

// 为了快速取 float32 位
func f32bits(f float32) uint32 { return *(*uint32)(unsafe.Pointer(&f)) }

func addExtensionsUsed(doc *gltf.Document, name string) {
	if !slices.Contains(doc.ExtensionsUsed, name) {
		doc.ExtensionsUsed = append(doc.ExtensionsUsed, name)
	}
}

// 追加 data 到 buffers[0]，创建一个新的 bufferView，返回其下标。
// 统一做到 4 字节对齐，避免某些运行时的对齐问题。
func appendBytesAsBufferViewAligned(doc *gltf.Document, data []byte, byteStride uint32) (int, error) {
	if doc == nil {
		return -1, fmt.Errorf("nil doc")
	}
	// 4 字节对齐
	if pad := (4 - (len(data) % 4)) % 4; pad != 0 {
		data = append(data, make([]byte, pad)...)
	}
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(data)

	buf := &gltf.Buffer{
		URI:        uri,
		ByteLength: len(data),
	}
	doc.Buffers = append(doc.Buffers, buf)
	bIndex := len(doc.Buffers) - 1

	bv := &gltf.BufferView{
		Buffer:     bIndex,
		ByteOffset: 0,
		ByteLength: len(data),
	}
	if byteStride != 0 {
		bv.ByteStride = int(byteStride)
	}
	doc.BufferViews = append(doc.BufferViews, bv)
	return len(doc.BufferViews) - 1, nil
}

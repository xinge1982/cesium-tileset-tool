package mapmodel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/qmuntal/gltf"
)

/********** data: URI 解码（同时返回 MIME，便于给 images 设置 mimeType） **********/
func decodeDataURI(s string) (mime string, data []byte, err error) {
	if !strings.HasPrefix(s, "data:") {
		return "", nil, fmt.Errorf("not a data URI")
	}
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return "", nil, fmt.Errorf("bad data URI: missing comma")
	}
	meta := s[len("data:"):comma]
	payload := strings.TrimSpace(s[comma+1:])

	mime = meta
	if i := strings.IndexByte(meta, ';'); i >= 0 {
		mime = meta[:i]
	}
	if strings.Contains(meta, ";base64") {
		// 容忍换行/空白
		payload = strings.Map(func(r rune) rune {
			switch r {
			case '\n', '\r', '\t', ' ':
				return -1
			default:
				return r
			}
		}, payload)
		if b, err := base64.StdEncoding.DecodeString(payload); err == nil {
			return mime, b, nil
		}
		if b, err := base64.RawStdEncoding.DecodeString(payload); err == nil {
			return mime, b, nil
		}
		return "", nil, fmt.Errorf("base64 decode failed")
	}
	u, err := url.PathUnescape(payload)
	if err != nil {
		return "", nil, fmt.Errorf("percent decode: %w", err)
	}
	return mime, []byte(u), nil
}

/********** 把 buffers 的 URI 转成内存 Data（支持 data: / 相对路径） **********/
func HydrateBuffers(doc *gltf.Document, baseDir string) error {
	for i, buf := range doc.Buffers {
		if buf == nil {
			continue
		}
		if len(buf.Data) > 0 {
			continue // GLB 已经有 Data
		}
		uri := strings.TrimSpace(buf.URI)
		if uri == "" {
			// 用 JSON 方式解析 GLB 时可能出现；如果 Data 也空，说明读法不对
			return fmt.Errorf("buffer[%d]: empty URI and no Data", i)
		}
		var data []byte
		var err error
		if strings.HasPrefix(uri, "data:") {
			_, data, err = decodeDataURI(uri)
		} else {
			path := uri
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDir, filepath.FromSlash(path))
			}
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return fmt.Errorf("buffer[%d] load failed: %w", i, err)
		}
		buf.URI = ""
		buf.Data = data
		buf.ByteLength = len(data)
		doc.Buffers[i] = buf

	}
	return nil
}

/********** 把 images 的 URI 也收编进主 BufferView（便于后续 SaveBinary 产 GLB） **********/
func HydrateImages(doc *gltf.Document, baseDir string) error {
	for i, im := range doc.Images {
		if im == nil || im.BufferView != nil {
			continue // 已经是内嵌
		}
		uri := strings.TrimSpace(im.URI)
		if uri == "" {
			continue
		}
		var data []byte
		var mime string
		var err error
		if strings.HasPrefix(uri, "data:") {
			mime, data, err = decodeDataURI(uri)
		} else {
			path := uri
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDir, filepath.FromSlash(path))
			}
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return fmt.Errorf("image[%d] load failed: %w", i, err)
		}

		// 写到 doc.Buffers[0]，4 字节对齐
		bufIdx := ensureSingleBuffer(doc)
		buf := doc.Buffers[bufIdx]
		off := len(buf.Data)
		if pad := (4 - (off % 4)) & 3; pad > 0 {
			buf.Data = append(buf.Data, make([]byte, pad)...)
			off += pad
		}
		buf.Data = append(buf.Data, data...)
		buf.ByteLength = len(buf.Data)
		doc.Buffers[bufIdx] = buf

		doc.BufferViews = append(doc.BufferViews, &gltf.BufferView{
			Buffer:     bufIdx,
			ByteOffset: off,
			ByteLength: len(data),
		})
		bv := len(doc.BufferViews) - 1

		im.BufferView = gltf.Index(bv)
		if mime != "" {
			im.MimeType = mime // glTF 的 image.mimeType
		}
		im.URI = "" // 变成内嵌
		doc.Images[i] = im
	}
	return nil
}

func RepairGLTF(buf []byte, basepath ...string) (*gltf.Document, error) {
	base, _ := os.Getwd()
	var doc gltf.Document
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, err
	}
	if len(basepath) > 0 {
		base = filepath.Dir(basepath[0])
	}
	if err := HydrateBuffers(&doc, base); err != nil {
		return nil, err
	}
	if err := HydrateImages(&doc, base); err != nil {
		return nil, err
	}
	return &doc, nil
}
func wrapUnderRootWithMatrix(doc *gltf.Document, M [16]float64) {

	// 收集所有 scene 的根节点（去重）
	seen := make(map[int]bool)
	var oldRoots []int
	for _, sc := range doc.Scenes {
		if sc == nil {
			continue
		}
		for _, r := range sc.Nodes {
			if !seen[r] {
				seen[r] = true
				oldRoots = append(oldRoots, r)
			}
		}
	}
	// 如果没有 scene，认为所有未被其他节点引用的节点是根（可按需补充）
	// 这里假设至少有一个根。
	rootIdx := len(doc.Nodes)
	doc.Nodes = append(doc.Nodes, &gltf.Node{
		Name:        "TilesetRoot",
		Matrix:      M, // 只设 Matrix，TRS 用默认值
		Translation: gltf.DefaultTranslation,
		Rotation:    gltf.DefaultRotation,
		Scale:       gltf.DefaultScale,
		Children:    oldRoots,
	})
	// 所有 scene 的根改成唯一的 root
	if len(doc.Scenes) == 0 {
		doc.Scenes = append(doc.Scenes, &gltf.Scene{Nodes: []int{rootIdx}})
		doc.Scene = gltf.Index(0)
	} else {
		for _, sc := range doc.Scenes {
			if sc == nil {
				continue
			}
			sc.Nodes = []int{rootIdx}
		}
		// 默认场景不变或指向第一个
		if doc.Scene == gltf.Index(0) || int(*doc.Scene) >= len(doc.Scenes) {
			doc.Scene = gltf.Index(0)
		}
	}
}

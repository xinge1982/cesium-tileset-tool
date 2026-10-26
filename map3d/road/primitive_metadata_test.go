package road

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Preserve all metadata bytes when unused views are removed after a GLB round trip.
func TestCompactDocumentMetadataRoundTrip(t *testing.T) {
	for _, representation := range []string{"builder", "decoded", "raw"} {
		t.Run(representation, func(t *testing.T) {
			doc := gltf.NewDocument()
			// Unused leading data guarantees every surviving view receives a new index.
			doc.Buffers = []*gltf.Buffer{{Data: []byte("unused__")}}
			doc.BufferViews = []*gltf.BufferView{{Buffer: 0, ByteLength: 8}}
			add := func(data []byte) int {
				offset := len(doc.Buffers[0].Data)
				doc.Buffers[0].Data = append(doc.Buffers[0].Data, data...)
				doc.BufferViews = append(doc.BufferViews, &gltf.BufferView{Buffer: 0, ByteOffset: offset, ByteLength: len(data)})
				return len(doc.BufferViews) - 1
			}
			values := add([]byte("护栏A"))
			offsets := add([]byte{0, 0, 0, 0, 7, 0, 0, 0})
			arrays := add([]byte{0, 0, 0, 0, 1, 0, 0, 0})
			doc.Buffers[0].ByteLength = len(doc.Buffers[0].Data)
			doc.Extensions = gltf.Extensions{"EXT_structural_metadata": map[string]any{
				"schema":         map[string]any{"classes": map[string]any{"Feature": map[string]any{"properties": map[string]any{"name": map[string]any{"type": "STRING", "array": true}}}}},
				"propertyTables": []map[string]any{{"class": "Feature", "count": 1, "properties": map[string]any{"name": map[string]any{"values": values, "stringOffsets": offsets, "arrayOffsets": arrays}}}},
			}}
			doc.ExtensionsUsed = []string{"EXT_structural_metadata"}
			if representation == "decoded" {
				data, _ := json.Marshal(doc.Extensions["EXT_structural_metadata"])
				var ext map[string]any
				if err := json.Unmarshal(data, &ext); err != nil {
					t.Fatal(err)
				}
				doc.Extensions["EXT_structural_metadata"] = ext
			} else if representation == "raw" {
				encoded, err := waveEncode(doc)
				if err != nil {
					t.Fatal(err)
				}
				doc = gltf.NewDocument()
				if err = gltf.NewDecoder(bytes.NewReader(encoded)).Decode(doc); err != nil {
					t.Fatal(err)
				}
			}
			want := metadataBytesForTest(t, doc)
			for pass := 0; pass < 2; pass++ {
				if err := CompactDocument(doc); err != nil {
					t.Fatal(err)
				}
				if len(doc.BufferViews) != 3 {
					t.Fatalf("metadata views discarded or unused views retained: %d", len(doc.BufferViews))
				}
				got := metadataBytesForTest(t, doc)
				for key, data := range want {
					if !bytes.Equal(data, got[key]) {
						t.Fatalf("%s bytes changed", key)
					}
				}
				encoded, err := waveEncode(doc)
				if err != nil {
					t.Fatal(err)
				}
				doc = gltf.NewDocument()
				if err = gltf.NewDecoder(bytes.NewReader(encoded)).Decode(doc); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// Return readable byte copies for each reference, including array offsets.
func metadataBytesForTest(t *testing.T, doc *gltf.Document) map[string][]byte {
	t.Helper()
	if err := ValidateStructuralMetadataBufferViews(doc); err != nil {
		t.Fatal(err)
	}
	refs, err := structuralMetadataBufferViewRefs(doc)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, ref := range refs {
		data, err := modeler.ReadBufferView(doc, doc.BufferViews[ref.index])
		if err != nil {
			t.Fatal(err)
		}
		out[ref.key] = append([]byte(nil), data...)
	}
	return out
}

// Fail before discarding data when a property contains a dangling or invalid index.
func TestCompactDocumentRejectsBrokenMetadata(t *testing.T) {
	for _, index := range []any{1774, -1, 1.5, "0"} {
		doc := gltf.NewDocument()
		doc.Buffers = []*gltf.Buffer{{ByteLength: 4, Data: make([]byte, 4)}}
		doc.BufferViews = []*gltf.BufferView{{Buffer: 0, ByteLength: 4}}
		doc.Extensions = gltf.Extensions{"EXT_structural_metadata": map[string]any{"propertyTables": []any{map[string]any{"properties": map[string]any{"name": map[string]any{"values": index}}}}}}
		if err := CompactDocument(doc); err == nil {
			t.Fatalf("accepted invalid reference %v", index)
		}
		if len(doc.BufferViews) != 1 || len(doc.Buffers[0].Data) != 4 {
			t.Fatal("modified input before validation")
		}
	}
}

package mergeone

import (
	"bytes"
	"testing"

	"github.com/qmuntal/gltf"
)

func TestImageDeduperReusesImageAcrossSourceDocuments(t *testing.T) {
	imageData := []byte("same embedded image")
	newSource := func() *gltf.Document {
		return &gltf.Document{
			Buffers: []*gltf.Buffer{{
				ByteLength: len(imageData),
				Data:       append([]byte(nil), imageData...),
			}},
			BufferViews: []*gltf.BufferView{{
				Buffer:     0,
				ByteLength: len(imageData),
			}},
			Images: []*gltf.Image{{
				BufferView: gltf.Index(0),
				MimeType:   "image/png",
			}},
		}
	}

	dst := gltf.NewDocument()
	deduper := NewImageDeduper()
	first, err := newMatCopier(newSource(), dst, deduper).cloneImage(0)
	if err != nil {
		t.Fatalf("clone first image: %v", err)
	}
	second, err := newMatCopier(newSource(), dst, deduper).cloneImage(0)
	if err != nil {
		t.Fatalf("clone duplicate image: %v", err)
	}

	if first != second {
		t.Fatalf("duplicate image indexes differ: first=%d second=%d", first, second)
	}
	if len(dst.Images) != 1 {
		t.Fatalf("got %d output images, want 1", len(dst.Images))
	}
	if len(dst.BufferViews) != 1 {
		t.Fatalf("got %d output image bufferViews, want 1", len(dst.BufferViews))
	}
	if !bytes.Equal(dst.Buffers[0].Data, imageData) {
		t.Fatalf("output buffer contains duplicated or changed image data")
	}
}

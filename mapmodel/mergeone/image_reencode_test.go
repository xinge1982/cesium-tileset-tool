package mergeone

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestReencodeEmbeddedImageKeepsTransparentPNG(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 20, G: 100, B: 200, A: uint8(x * 17)})
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatalf("encode source PNG: %v", err)
	}

	encoded, mimeType := reencodeEmbeddedImage(input.Bytes(), "image/png")
	if mimeType != "image/png" {
		t.Fatalf("transparent PNG became %q", mimeType)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode output PNG: %v", err)
	}
	_, _, _, alpha := decoded.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatalf("transparent pixel alpha changed to %d", alpha)
	}
	if decoded.Bounds() != source.Bounds() {
		t.Fatalf("image dimensions changed: got %v want %v", decoded.Bounds(), source.Bounds())
	}
}

func TestReencodeEmbeddedImageConvertsOpaquePNGToJPEG(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			source.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*31 + y*17) & 0xff),
				G: uint8((x*13 + y*29) & 0xff),
				B: uint8((x*7 + y*19) & 0xff),
				A: 255,
			})
		}
	}
	var input bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&input, source); err != nil {
		t.Fatalf("encode source PNG: %v", err)
	}

	encoded, mimeType := reencodeEmbeddedImage(input.Bytes(), "image/png")
	if mimeType != "image/jpeg" {
		t.Fatalf("opaque PNG output MIME = %q, want image/jpeg", mimeType)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode output JPEG config: %v", err)
	}
	if config.Width != 128 || config.Height != 128 {
		t.Fatalf("image dimensions changed: got %dx%d", config.Width, config.Height)
	}
	if len(encoded) >= input.Len() {
		t.Fatalf("reencoded image did not shrink: input=%d output=%d", input.Len(), len(encoded))
	}
}

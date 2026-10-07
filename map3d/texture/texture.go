package texture

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path"
	"strings"
)

var (
	BODY_TEXTURE     TextureType = "body.png"
	WINDOW_TEXTURE   TextureType = "window.png"
	ROOF_TEXTURE     TextureType = "roof.png"
	ROOF_TOP_TEXTURE TextureType = "roof_top.png"
)

type TextureType string

type BuddingTexture struct {
	Path     string
	Textures map[TextureType]*Texture
}

type Texture struct {
	path   string
	buf    []byte
	format string
	width  int
	height int
	name   string
}

func (tt Texture) GetPath() string {
	return tt.path
}
func (tt Texture) GetBuf() []byte {
	return tt.buf
}
func (tt Texture) Format() string {
	return tt.format
}
func (tt Texture) Width() int {
	return tt.width
}
func (tt Texture) Height() int {
	return tt.height
}
func (tt Texture) Name() string {
	return tt.name
}

func imageType(buffer []byte) (string, error) {
	switch {
	case buffer[0] == 0xFF && buffer[1] == 0xD8:
		return "image/jpeg", nil
	case buffer[0] == 0x89 && buffer[1] == 0x50:
		return "image/png", nil
	default:
		return "", errors.New("image type error")
	}

}

func NewTexture(path string) (*Texture, error) {
	txu := &Texture{}

	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewBuffer(buf))
	if err != nil {
		return nil, err
	}
	txu.path = path
	txu.buf = buf
	txu.width = img.Bounds().Dx()
	txu.height = img.Bounds().Dy()
	txu.format, err = imageType(buf)
	txu.name = path
	return txu, err
}
func NewTextureFromBase64(b64 string) (*Texture, error) {

	txu := &Texture{}
	var err error
	// 去除前缀（如果有 data:image/png;base64,...）
	if strings.Contains(b64, ",") {
		b64 = strings.Split(b64, ",")[1]
	}

	txu.buf, err = base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(strings.NewReader(string(txu.buf)))
	if err != nil {
		return nil, err
	}

	txu.width = img.Bounds().Dx()
	txu.height = img.Bounds().Dy()
	txu.format, err = imageType(txu.buf)
	txu.name = "face.png"
	return txu, err
}

func NewBuddingTexture(stylePath string) (*BuddingTexture, error) {
	bt := &BuddingTexture{}
	content := []string{string(BODY_TEXTURE), string(WINDOW_TEXTURE), string(ROOF_TEXTURE), string(ROOF_TOP_TEXTURE)}

	bt.Textures = make(map[TextureType]*Texture)
	for _, name := range content {
		tx, err := NewTexture(path.Join(stylePath, name))
		if err != nil {
			return nil, err
		}
		bt.Textures[TextureType(name)] = tx
	}
	bt.Path = stylePath
	return bt, nil
}

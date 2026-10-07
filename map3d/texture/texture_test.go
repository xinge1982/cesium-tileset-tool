package texture

import (
	"testing"
)

func Test_openImage(t *testing.T) {
	path := `C:\MapABC\code\roadgltf\resources\style1`
	texture, err := NewBuddingTexture(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(texture)

}

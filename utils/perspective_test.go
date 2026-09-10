package utils

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestPerspectiveWarpBase64(t *testing.T) {
	buf, err := os.ReadFile("输入.png")
	if err != nil {
		t.Fatal(err)
	}

	base64Str := base64.StdEncoding.EncodeToString(buf)

	srcPoints := [4]Point{{220, 21}, {596, 83}, {596, 487}, {221, 493}}
	dstPoints := [4]Point{{0, 0}, {300, 0}, {300, 450}, {0, 450}}
	//srcPoints := [4]Point{{0, 0}, {200, 50}, {200, 250}, {0, 200}}
	//dstPoints := [4]Point{{0, 0}, {300, 0}, {300, 300}, {0, 300}}
	cropImage, err := PerspectiveWarpBase64(base64Str, srcPoints, dstPoints, 300, 450)
	if err != nil {
		t.Fatal(err)
	}

	if idx := strings.Index(cropImage, "base64,"); idx >= 0 {
		cropImage = cropImage[idx+7:]
	}
	buf, err = base64.StdEncoding.DecodeString(cropImage)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile("剪切.png", buf, 0644)
	if err != nil {
		t.Fatal(err)
	}
}

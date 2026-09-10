package traffic_feature

import (
	"cesium-tileset-tool/cesium"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func Test_getRegionFromBound(t *testing.T) {
	var bound = "121.45116075, 31.04727620, 121.56881049, 31.07995470"
	box := cesium.GetBoxFromBound(bound)
	transform := cesium.GenerateTransformMatrixUPScale(121.50998562, 31.06361545, 0, 0, 1.0, 1.0, 1.0)

	root := &TileNode{
		BoundingVolume: BoundingVolume{
			Box: box,
		},
		GeometricError: 1000,
		Refine:         "ADD",
		Transform:      &transform,
	}
	data, _ := json.MarshalIndent(root, "", "  ")
	absPath := filepath.Join("./", "tileset.json")
	_ = os.WriteFile(absPath, data, 0644)

	fmt.Printf("✅ tileset.json 生成完成 %s", absPath)
}

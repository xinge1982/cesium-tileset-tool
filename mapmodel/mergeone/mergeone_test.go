package mergeone

import (
	"fmt"
	"testing"

	"github.com/qmuntal/gltf"
)

func TestMergeAllToSingleMeshNode(t *testing.T) {
	src, err := gltf.Open("C:\\Users\\lujie\\Desktop\\tool\\poc\\html\\qpnew\\pole_test\\pole_test\\qingfu_4051000124.glb") // 或 gltf.OpenBinary
	if err != nil {
		panic(err)
	}

	dst, err := MergeAllToSingleMeshNode(src, MergeOptions{SkipSkinned: true})
	if err != nil {
		panic(err)
	}

	fmt.Println(len(dst.Meshes), len(dst.Nodes), dst.Scenes[0])

	if err := gltf.SaveBinary(dst, "C:\\MapABC\\web\\shanghai\\r.glb"); err != nil {
		panic(err)
	}

}
func TestMergeOneToSingleMeshNode(t *testing.T) {

	aDoc, err := gltf.Open("C:\\Users\\lujie\\Desktop\\tool\\poc\\html\\qpnew\\pole_test\\pole_test\\qingfu_4051000124.glb")
	bDoc, err := gltf.Open("C:\\MapABC\\code\\map-tool\\mapmodel\\mergeone\\kxq_01_07.glb")
	aDoc = gltf.NewDocument()
	newDoc, err := MergeTwoToSingleMeshNode(aDoc, bDoc, MergeOptions{SkipSkinned: true, Fidx: -1})
	if err != nil {
		panic(err)
	}

	if err := gltf.SaveBinary(newDoc, "C:\\MapABC\\web\\shanghai\\r.glb"); err != nil {
		panic(err)
	}

}

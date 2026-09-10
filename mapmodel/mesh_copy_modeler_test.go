package mapmodel

//
//import (
//	"fmt"
//	"testing"
//
//	"github.com/qmuntal/gltf"
//)
//
//func TestCloneMeshWithModeler(t *testing.T) {
//	doc, err := gltf.Open(`glb/lmj_01_03.glb`)
//	if err != nil {
//		t.Fatal("open gltf file error")
//	}
//	fmt.Println(len(doc.Scenes))
//	fmt.Println(len(doc.Nodes))
//	fmt.Println(len(doc.Meshes))
//
//	doc = gltf.NewDocument()
//
//	newDoc, _ := gltf.Open("glb/sdbj_01_01.glb")
//	fmt.Println("newScenes:", len(newDoc.Scenes))
//
//	for _, nd := range doc.Nodes {
//		if nd.Matrix != gltf.DefaultMatrix || nd.Translation != gltf.DefaultTranslation || nd.Rotation != gltf.DefaultRotation || nd.Scale != gltf.DefaultScale {
//			//TODO
//			fmt.Println("矩阵不一致")
//		}
//
//		if nd.Mesh != nil {
//			meshIdx, err := (doc, *nd.Mesh, newDoc, -1)
//			if err != nil {
//				t.Fatal(err)
//			}
//			var node = &gltf.Node{}
//			node.Mesh = gltf.Index(meshIdx)
//			newDoc.Nodes = append(newDoc.Nodes, node)
//			nodeIdx := len(newDoc.Nodes) - 1
//			newDoc.Scenes[0].Nodes = append(newDoc.Scenes[0].Nodes, nodeIdx)
//			//TODO 处理属性数据
//		}
//	}
//
//	gltf.SaveBinary(newDoc, "C:\\MapABC\\web\\shanghai\\r.glb")
//}

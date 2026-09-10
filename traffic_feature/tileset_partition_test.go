package traffic_feature

import (
	"cesium-tileset-tool/mapmodel/aabb"
	"fmt"
	"testing"
)

func Test_generateGeohashesInBounds(t *testing.T) {
	hash := "w"
	parentHash := hash[:len(hash)-1]
	hashes := generateGeohashesInBounds(111.11794051437313, 30.556832074133, 111.13222780946803, 30.558676928732268, 3)
	fmt.Println(hashes)
	print(parentHash)
}

func Test_generateQuaternion(t *testing.T) {
	qua := aabb.QuatFromHPRDeg(45, 0, 0)
	fmt.Println(qua)

}

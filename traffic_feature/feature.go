package traffic_feature

import (
	"bytes"
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/pg"
	"context"
	"sync"
)

type FeatureMan struct {
	tables    map[string]bool
	cancelFun context.CancelFunc
	locker    sync.Mutex
	cfg       *config.Config
	dbC       *pg.PGConn
}

const EmptyFieldReplace = "-"

var instantiated *FeatureMan
var once sync.Once

func NewPubInstance(cfg *config.Config) *FeatureMan {
	once.Do(func() {
		instantiated = &FeatureMan{
			cfg: cfg,
			dbC: pg.GetConn(cfg.Name),
		}
	})
	return instantiated
}

func detectGltfFormat(data []byte) string {
	// Check if it's a binary GLB file (first 4 bytes should be "glTF")
	if bytes.HasPrefix(data, []byte("glTF")) {
		return "model/gltf-binary"
	}

	// Check if it's a JSON file (should start with '{' and end with '}')
	if len(data) > 0 && data[0] == '{' && data[len(data)-1] == '}' {
		return "model/gltf+json"
	}

	return "application/octet-stream"
}

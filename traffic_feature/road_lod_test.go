package traffic_feature

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"cesium-tileset-tool/cesium"
	"cesium-tileset-tool/config"
	"gopkg.in/yaml.v3"
)

func shanghaiRoadLODForTest(t *testing.T) config.TilesetLODConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "networks", "config_shanghai.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Tilesets map[string]struct {
			LOD config.TilesetLODConfig `yaml:"lod"`
		} `yaml:"Tilesets"`
	}
	if err = yaml.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	source, ok := project.Tilesets["roadsurface"]
	if !ok {
		t.Fatal("roadsurface configuration is missing")
	}
	return source.LOD
}

func roadLODForTest() config.TilesetLODConfig {
	return config.TilesetLODConfig{
		Enabled: true,
		LOD0:    config.TilesetLODLevelConfig{GeometricError: 120},
		LOD1:    config.TilesetLODLevelConfig{GeometricError: 35},
		LOD2:    config.TilesetLODLevelConfig{GeometricError: 7},
		LOD3:    config.TilesetLODLevelConfig{GeometricError: 0},
	}
}

// Preserve configured thresholds and reject values that would prevent refinement.
func TestRoadLODConfiguration(t *testing.T) {
	lod := roadLODForTest()
	levels, err := configuredRoadLODLevels(lod)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 4 {
		t.Fatal("project LOD configuration was discarded", levels)
	}
	for i, want := range []float64{120, 35, 7, 0} {
		if levels[i].GeometricError != want {
			t.Fatalf("LOD%d error = %v, want %v", i, levels[i].GeometricError, want)
		}
	}
	if base := roadGeometricErrorBase(levels); base != lod.LOD0.GeometricError {
		t.Fatal(base)
	}
	disabled, err := configuredRoadLODLevels(config.TilesetLODConfig{})
	if err != nil || len(disabled) != 1 || disabled[0].Level != 3 || roadGeometricErrorBase(disabled) != 128 {
		t.Fatal(disabled, err)
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		invalid := lod
		invalid.LOD1.GeometricError = value
		if _, err = configuredRoadLODLevels(invalid); err == nil {
			t.Fatal("invalid threshold accepted", value)
		}
	}
	invalid := lod
	invalid.LOD1.GeometricError = invalid.LOD0.GeometricError
	if _, err = configuredRoadLODLevels(invalid); err == nil {
		t.Fatal("nondecreasing thresholds accepted")
	}
	invalid = lod
	invalid.LOD3.GeometricError = 1
	if _, err = configuredRoadLODLevels(invalid); err == nil {
		t.Fatal("nonzero terminal error accepted")
	}
}

// Preserve the configured replacement thresholds and apply the ENU transform once.
func TestGeneratedRoadLODChainBounds(t *testing.T) {
	levels, err := configuredRoadLODLevels(roadLODForTest())
	if err != nil {
		t.Fatal(err)
	}
	local := [12]float64{1000, -500, 20, 50, 0, 0, 0, 100, 0, 0, 0, 6}
	transform := cesium.GenerateTransformMatrixUP(121.77, 31.12, 0, 0)
	uri := "road-models/run-test/source-0/tiles/wtw94u/surface.glb"
	chain := buildGeneratedRoadLODChain(uri, local, transform, levels, "wtw94u", 0)
	root := &TileNode{Children: []*TileNode{chain}}
	refreshGeoHashTileBound(root, roadGeometricErrorBase(levels))
	if root.GeometricError != 240 {
		t.Fatal("spatial parent did not use LOD0", root.GeometricError)
	}
	node := chain
	for i, level := range levels {
		if node == nil || node.GeometricError != level.GeometricError || node.Refine != "REPLACE" || node.Content.Uri != uri {
			t.Fatal("incorrect replacement chain", node)
		}
		if i == 0 {
			if node.Transform == nil {
				t.Fatal("missing outer transform")
			}
			world := roadWorldBox(node.BoundingVolume.Box, *node.Transform)
			want := roadWorldBox(local, transform)
			for axis := range world {
				if math.Abs(world[axis]-want[axis]) > 1e-6 {
					t.Fatal("bounds transformed incorrectly", world, want)
				}
			}
		} else if node.Transform != nil || node.BoundingVolume.Box != local {
			t.Fatal("nested transform/bounds incorrect", node)
		}
		if i == len(levels)-1 {
			if len(node.Children) != 0 {
				t.Fatal("unexpected terminal children")
			}
		} else {
			if len(node.Children) != 1 {
				t.Fatal("incorrect LOD child count")
			}
			node = node.Children[0]
		}
	}
}

package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"fmt"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"math"
)

type TunnelPlatformOptions struct {
	Centerline      *tunnelCenterline
	SourceFeatures  []int
	RepairTolerance float64
	OnRepair        func(int)
	// TileSize 是 platform_top 一张完整方砖贴图对应的边长，单位米。
	TileSize float64
}

// 对每个独立平台面执行 CDT，按中线里程和横向距离生成连续的米制 UV。
// 顶面不会按单个多边形宽度归一化，因此不同长度和宽度的片段保持同一方砖尺寸。
func TunnelPlatformMultiPolygonPrimitive(doc *gltf.Document, polygons [][][3]float32, surfaceType string, material int, params mgltf.UvsParams, options ...TunnelPlatformOptions) (*gltf.Primitive, error) {
	opt := TunnelPlatformOptions{TileSize: 0.5}
	if len(options) > 0 {
		opt = options[0]
		if opt.TileSize == 0 {
			opt.TileSize = 0.5
		}
	}
	if opt.RepairTolerance == 0 {
		opt.RepairTolerance = 0.005
	}
	if !isFinite(opt.RepairTolerance) || opt.RepairTolerance <= 0 {
		return nil, fmt.Errorf("platform repair tolerance must be positive meters")
	}
	if !isFinite(opt.TileSize) || opt.TileSize <= 0 {
		return nil, fmt.Errorf("platform tile size must be positive meters")
	}
	var positions [][3]float32
	var indices []uint32
	var uv [][2]float32
	for polygonIndex, ring := range polygons {
		pos := normalizeClosedRing3D(ring)
		if len(pos) < 3 {
			continue
		}
		var local []uint32
		var err error
		feature := polygonIndex
		if polygonIndex < len(opt.SourceFeatures) {
			feature = opt.SourceFeatures[polygonIndex]
		}
		repaired := false
		if surfaceType == "platform_top" {
			pos, local, repaired, err = repairPlatformTopCrossingWithTolerance(pos, opt.RepairTolerance)
			if err != nil {
				return nil, fmt.Errorf("platform polygon %d (source feature %d): %w", polygonIndex, feature, err)
			}
			if repaired && opt.OnRepair != nil {
				opt.OnRepair(feature)
			}
		}
		if repaired {
			// 交叉顶面已拆成简单面并完成 CDT。
		} else if surfaceType == "platform_inner_wall" || surfaceType == "platform_outer_wall" {
			local, err = triangulateTunnelWallCDT(pos)
		} else if surfaceType == "platform_top" && opt.Centerline != nil {
			local, err = triangulatePlatformStripCDT(pos, opt.Centerline)
		} else {
			local, err = mgltf.CDTRingKeepWinding(pos)
		}
		if err != nil {
			return nil, fmt.Errorf("platform polygon %d (source feature %d): %w", polygonIndex, feature, err)
		}
		base := uint32(len(positions))
		positions = append(positions, pos...)
		for _, index := range local {
			indices = append(indices, base+index)
		}
		if surfaceType == "platform_top" {
			for _, p := range pos {
				s, l := float64(p[0]), float64(p[2])
				if opt.Centerline != nil {
					s, l = opt.Centerline.coordinates(p)
				}
				u, v := float32(s/opt.TileSize), float32(l/opt.TileSize)
				if params.FlipU {
					u = -u
				}
				if params.FlipV {
					v = -v
				}
				uv = append(uv, [2]float32{u, v})
			}
		} else if opt.Centerline != nil {
			coords := tunnelWallUVs(pos, opt.Centerline, 1, 1)
			for _, pair := range coords {
				if params.FlipU {
					pair[0] = -pair[0]
				}
				if params.FlipV {
					pair[1] = -pair[1]
				}
				uv = append(uv, pair)
			}
		}
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("empty platform primitive: %s", surfaceType)
	}
	normals := GenerateSmoothNormalsByIndices(positions, indices)
	if len(uv) == 0 {
		return mgltf.PrimitiveWithNormalsStrip(doc, positions, indices, normals, material, 1, params)
	}
	return &gltf.Primitive{Attributes: gltf.PrimitiveAttributes{
		gltf.POSITION: modeler.WritePosition(doc, positions), gltf.NORMAL: modeler.WriteNormal(doc, normals), gltf.TEXCOORD_0: modeler.WriteTextureCoord(doc, uv),
	}, Indices: gltf.Index(modeler.WriteIndices(doc, indices)), Material: gltf.Index(material)}, nil
}

// 对由两条反向边界组成的平台长条按相邻断面剖分，无法确认配对时使用通用 CDT。
func triangulatePlatformStripCDT(pos [][3]float32, line *tunnelCenterline) ([]uint32, error) {
	n := len(pos)
	if n < 4 || n%2 != 0 {
		return mgltf.CDTRingKeepWinding(pos)
	}
	half := n / 2
	for i := 0; i < half; i++ {
		a, b := pos[i], pos[n-1-i]
		sa, _ := line.coordinates(a)
		sb, _ := line.coordinates(b)
		width := math.Hypot(float64(a[0])-float64(b[0]), float64(a[2])-float64(b[2]))
		if width < 1e-6 || width > 20 || math.Abs(sa-sb) > math.Max(0.1, width*0.1) {
			return mgltf.CDTRingKeepWinding(pos)
		}
	}
	indices := make([]uint32, 0, (half-1)*6)
	for i := 0; i+1 < half; i++ {
		source := [4]uint32{uint32(i), uint32(i + 1), uint32(n - 2 - i), uint32(n - 1 - i)}
		local, err := mgltf.CDTRingKeepWinding([][3]float32{pos[source[0]], pos[source[1]], pos[source[2]], pos[source[3]]})
		if err != nil {
			return nil, fmt.Errorf("platform strip segment %d: %w", i, err)
		}
		for _, index := range local {
			indices = append(indices, source[index])
		}
	}
	return indices, nil
}

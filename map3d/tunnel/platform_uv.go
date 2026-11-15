package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"fmt"
	"math"
	"sort"
)

type platformTexturePoint struct{ station, lateral float64 }
type platformTextureEdge struct {
	a, b     platformTexturePoint
	min, max float64
}

// 在同一平台组的所有片段上建立左边界，按隧道前进方向向右累计横向纹理距离。
// 使用公共里程断面的最小有符号横距，避免每片单独归零造成接缝错位。
func platformTopUVs(positions [][3]float32, indices []uint32, opt TunnelPlatformOptions, params mgltf.UvsParams) ([][2]float32, error) {
	points := make([]platformTexturePoint, len(positions))
	for i, p := range positions {
		s, l := float64(p[0]), float64(p[2])
		if opt.Centerline != nil {
			s, l = opt.Centerline.coordinates(p)
		}
		points[i] = platformTexturePoint{s, l}
	}
	edges := make([]platformTextureEdge, 0, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		for k := 0; k < 3; k++ {
			a, b := points[indices[i+k]], points[indices[i+(k+1)%3]]
			edges = append(edges, platformTextureEdge{a, b, math.Min(a.station, b.station), math.Max(a.station, b.station)})
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].min < edges[j].min })
	order := make([]int, len(points))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return points[order[i]].station < points[order[j]].station })
	uv := make([][2]float32, len(points))
	active := make([]platformTextureEdge, 0)
	cursor := 0
	const epsilon = 1e-8
	for _, index := range order {
		p := points[index]
		for cursor < len(edges) && edges[cursor].min <= p.station+epsilon {
			active = append(active, edges[cursor])
			cursor++
		}
		left := math.Inf(1)
		remaining := active[:0]
		for _, edge := range active {
			if edge.max < p.station-epsilon {
				continue
			}
			remaining = append(remaining, edge)
			lateral := math.Min(edge.a.lateral, edge.b.lateral)
			delta := edge.b.station - edge.a.station
			if math.Abs(delta) > epsilon {
				t := math.Max(0, math.Min(1, (p.station-edge.a.station)/delta))
				lateral = edge.a.lateral + t*(edge.b.lateral-edge.a.lateral)
			}
			left = math.Min(left, lateral)
		}
		active = remaining
		if math.IsInf(left, 1) {
			return nil, fmt.Errorf("platform vertex %d has no left boundary at station %.6f", index, p.station)
		}
		u, v := float32(p.station/opt.TileSize), float32(math.Max(0, p.lateral-left)/opt.TileSize)
		if params.FlipU {
			u = -u
		}
		if params.FlipV {
			v = -v
		}
		uv[index] = [2]float32{u, v}
	}
	return uv, nil
}

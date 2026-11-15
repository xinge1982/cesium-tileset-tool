package tunnel

import (
	"fmt"
	"math"
)

type roofSectionGraph map[uint32]map[uint32]float64

// 从屋顶四边形中识别横断面边，用三维边长累计横向贴图坐标。
func (graph roofSectionGraph) addQuad(ids []uint32, positions [][3]float32, line *tunnelCenterline, orderedSections bool) error {
	if len(ids) != 4 {
		return fmt.Errorf("centerline roof UV requires quadrilateral roof patches, got %d vertices", len(ids))
	}
	var s [4]float64
	for i, id := range ids {
		s[i], _ = line.coordinates(positions[id])
	}
	length := func(a, b int) float64 {
		p, q := positions[ids[a]], positions[ids[b]]
		return math.Sqrt(math.Pow(float64(p[0])-float64(q[0]), 2) + math.Pow(float64(p[1])-float64(q[1]), 2) + math.Pow(float64(p[2])-float64(q[2]), 2))
	}
	score := func(a, b int) float64 {
		d := length(a, b)
		if d < 1e-9 {
			return 0
		}
		return math.Abs(s[a]-s[b]) / d
	}
	edges := [][2]int{{0, 1}, {2, 3}}
	if orderedSections || score(0, 1)+score(2, 3) > score(1, 2)+score(3, 0) {
		edges = [][2]int{{1, 2}, {3, 0}}
	}
	for _, edge := range edges {
		a, b := ids[edge[0]], ids[edge[1]]
		if a == b {
			continue
		}
		if graph[a] == nil {
			graph[a] = make(map[uint32]float64)
		}
		if graph[b] == nil {
			graph[b] = make(map[uint32]float64)
		}
		graph[a][b] = length(edge[0], edge[1])
		graph[b][a] = graph[a][b]
	}
	return nil
}

// 每条横断面从有符号横向坐标较小的一端起算，保留拱顶立面过渡带的纹理长度。
func (graph roofSectionGraph) apply(uv [][2]float32, repeatV float64) error {
	seen := make(map[uint32]bool)
	for seed := range graph {
		if seen[seed] {
			continue
		}
		stack := []uint32{seed}
		seen[seed] = true
		var component, ends []uint32
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			component = append(component, node)
			if len(graph[node]) == 1 {
				ends = append(ends, node)
			}
			for next := range graph[node] {
				if !seen[next] {
					seen[next] = true
					stack = append(stack, next)
				}
			}
		}
		distance := make(map[uint32]float64, len(component))
		done := make(map[uint32]bool, len(component))
		minimum := seed
		for _, node := range component {
			distance[node] = math.Inf(1)
			if uv[node][1] < uv[minimum][1] {
				minimum = node
			}
		}
		// 近邻焊接可使极短末端断面合并；从每个左侧端点同时起算，避免纹理绕回。
		sources := 0
		for _, node := range ends {
			if uv[node][1] <= 0 {
				distance[node] = 0
				sources++
			}
		}
		if sources == 0 {
			distance[minimum] = 0
		}
		for range component {
			best := math.Inf(1)
			node := seed
			for _, candidate := range component {
				if !done[candidate] && distance[candidate] < best {
					best = distance[candidate]
					node = candidate
				}
			}
			if math.IsInf(best, 1) {
				return fmt.Errorf("unreachable roof section vertex")
			}
			done[node] = true
			for next, length := range graph[node] {
				if best+length < distance[next] {
					distance[next] = best + length
				}
			}
		}
		for _, node := range component {
			uv[node][1] = float32(distance[node] / repeatV)
		}
	}
	return nil
}

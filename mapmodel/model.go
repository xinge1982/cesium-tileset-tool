package mapmodel

import "github.com/qmuntal/gltf"
import "cesium-tileset-tool/mapmodel/aabb"

type Model struct {
	Doc    *gltf.Document
	Fields map[string][]string
	Coords [][3]float64 //wgs84 坐标
	R      [][3]float64
	S      [][3]float64
	Q      [][4]float64
	WT     [][16]float64 //直接使用的世界系transform
	UseWT  []bool
	wm     [][16]float64
}

func (m *Model) AABB() (aabb.Box, error) {
	box, world, err := aabb.SceneBox(m.Doc, 0)
	m.wm = world
	return box, err
}
func (m *Model) Region() ([6]float64, error) {
	var region [6]float64

	box, err := m.AABB()
	if err != nil {
		return region, err
	}

	for i := range m.Coords {
		if i == 0 {
			region = aabb.RegionFromBoxPoseWGS84(box, m.Coords[i][0], m.Coords[i][1], m.Coords[i][2],
				aabb.QuatFromHPRDeg(m.R[i][0], m.R[i][1], m.R[i][2]), m.S[i], 0)
		} else {
			region = mergeRegion(region, aabb.RegionFromBoxPoseWGS84(box, m.Coords[i][0], m.Coords[i][1], m.Coords[i][2],
				aabb.QuatFromHPRDeg(m.R[i][0], m.R[i][1], m.R[i][2]), m.S[i], 0))
		}
	}
	return region, nil
}

func (m *Model) ModelNumber() int {
	return len(m.Coords)
}

func mergeRegion(r1 [6]float64, r2 ...[6]float64) [6]float64 {
	res := r1
	// 保证 r1 边界有序（防输入颠倒）
	if res[0] > res[2] {
		res[0], res[2] = res[2], res[0]
	}
	if res[1] > res[3] {
		res[1], res[3] = res[3], res[1]
	}

	for _, r := range r2 {
		w, s, e, n, hmin, hmax := r[0], r[1], r[2], r[3], r[4], r[5]
		// 保证当前 r 边界有序
		if w > e {
			w, e = e, w
		}
		if s > n {
			s, n = n, s
		}

		if w < res[0] {
			res[0] = w
		}
		if s < res[1] {
			res[1] = s
		}
		if e > res[2] {
			res[2] = e
		}
		if n > res[3] {
			res[3] = n
		}
		if hmin < res[4] {
			res[4] = hmin
		}
		if hmax > res[5] {
			res[5] = hmax
		}
	}
	return res
}

////把这个方法改为 map[uint32]*gltf.node,*gltf.node 里面矩阵、或T、R、S是压平后的
//
//func (m *Model) meshGroup() map[uint32][]int {
//	meshToNodes := map[uint32][]int{}
//	for ni, nd := range m.Doc.Models {
//		if nd == nil || nd.Mesh == nil {
//			continue
//		}
//		midx := *nd.Mesh
//		meshToNodes[uint32(midx)] = append(meshToNodes[uint32(midx)], ni)
//	}
//	return meshToNodes
//}

func (m *Model) MeshGroupFlattened(useMatrix ...bool) map[int][]*gltf.Node {
	out := make(map[int][]*gltf.Node)
	if m == nil || m.Doc == nil || len(m.Doc.Nodes) == 0 {
		return out
	}
	for ni, nd := range m.Doc.Nodes {
		if nd == nil || nd.Mesh == nil {
			continue
		}
		mid := *nd.Mesh // gltf.Index 的底层就是 uint32
		world := m.wm[ni]

		// 构造一个“压平后的副本”节点（仅携带 Mesh 与烘焙后的变换；不保留层级）
		cp := &gltf.Node{
			Name: nd.Name, // 名字保留可追踪
		}
		use := false
		if len(useMatrix) > 0 {
			use = useMatrix[0]
		}

		if use {
			// 直接写矩阵（float64 列主序）
			cp.Matrix = world
			// 清 TRS，保持语义一致
			cp.Translation = gltf.DefaultTranslation
			cp.Rotation = gltf.DefaultRotation
			cp.Scale = gltf.DefaultScale
		} else {
			// 分解为 T/R/S（推荐做法，兼容性更好）
			T, R, S := aabb.DecomposeTRS(world)
			cp.Matrix = gltf.DefaultMatrix
			cp.Translation = T
			cp.Rotation = R
			cp.Scale = S
		}

		// 继续挂同一个 mesh
		meshIdx := gltf.Index(mid)
		cp.Mesh = meshIdx
		// 这个“压平副本”不再保留任何层级/皮肤信息
		cp.Children = nil
		cp.Skin = nil
		cp.Camera = nil
		out[mid] = append(out[mid], cp)
	}
	return out
}

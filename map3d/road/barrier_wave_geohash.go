package road

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"cesium-tileset-tool/cesium"
	"cesium-tileset-tool/map3d/road/common"
	"cesium-tileset-tool/mapmodel/aabb"
	"github.com/mmcloughlin/geohash"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Fixed-precision, shared-frame slicing options.
// Features must have WGS84 LineString/MultiLineString geometry. OutputDir is the
// final tileset directory. Source supports are sampled once, then assigned to
// exactly one projected cell by anchor; their intact geometry may cross an edge.
// This prototype has no LOD, per-tile rebasing or stale-output cleanup.
type BarrierWaveGeohashOptions struct {
	OutputDir string
	Precision int
	// Posts supplies global post defaults; a feature-level pointer takes precedence.
	Posts common.GuardrailPostOptions
	// Chains configures full-source endpoint connectivity; zero uses safe defaults.
	Chains common.SidewallChainOptions
	// DisableChains restores legacy per-part sampling.
	DisableChains bool
	// Bound keeps complete features AFTER computing full-source chain offsets.
	Bound *[4]float64
}

type BarrierWaveGeohashResult struct {
	TilesetPath                      string
	Tiles                            []RoadSurfaceGeohashTile
	SourceArea, SlicedArea           float64
	SourceInstances, SlicedInstances int
}

type waveSliceCell struct {
	hash     string
	boundary [][2]float64
}

// Preserve source rail UVs, normals, feature IDs
// and GPU support instance transforms rather than rebuilding cut line fragments.
func BuildBarrierWaveGeohashSlices(features []LineFeature, opt BarrierWaveGeohashOptions) (*BarrierWaveGeohashResult, error) {
	if opt.Precision == 0 {
		opt.Precision = 6
	}
	if opt.Precision < 4 || opt.Precision > 7 || opt.OutputDir == "" {
		return nil, fmt.Errorf("require output directory and precision 4..7")
	}
	if len(features) == 0 {
		return nil, fmt.Errorf("no barrier wave features")
	}
	for _, f := range features {
		if f.BuildType != BuildTypeBarrierWave {
			return nil, fmt.Errorf("unexpected build type %q", f.BuildType)
		}
	}
	ctx, err := ResolveTileContextAuto(TileContext{TileID: "barrier-wave-source", SRID: 4326, UseENU: true}, nil, features, nil)
	if err != nil {
		return nil, err
	}
	// Copy the feature slice before assigning per-build options.
	features = append([]LineFeature(nil), features...)
	for i := range features {
		if features[i].GuardrailPosts == nil {
			posts := opt.Posts
			features[i].GuardrailPosts = &posts
		}
	}
	if !opt.DisableChains {
		coordinates, err := common.NewCoordinatePipeline(ctx)
		if err != nil {
			return nil, err
		}
		features, err = common.PrepareSidewallChains(features, coordinates, opt.Chains)
		if err != nil {
			return nil, err
		}
	}
	if opt.Bound != nil {
		b := *opt.Bound
		for _, v := range b {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("invalid bound")
			}
		}
		if b[0] > b[2] || b[1] > b[3] {
			return nil, fmt.Errorf("invalid bound")
		}
		selected := []LineFeature{}
		for _, f := range features {
			if roadSliceGeometryInsideBound(f.Geom, b) {
				selected = append(selected, f)
			}
		}
		features = selected
		if len(features) == 0 {
			return nil, fmt.Errorf("no barrier features inside bound")
		}
	}
	options := DefaultBuildOptions()
	options.MergePrimitives = false
	builder, err := NewRoadTileBuilder(ctx, &options)
	if err != nil {
		return nil, err
	}
	builder.AddLineFeatures(features...)
	source, primitives, err := builder.BuildDocument()
	if err != nil {
		return nil, err
	}
	appendPrimitivesAsMesh(source, primitives)
	// Every rail primitive still belongs to one input feature at this point.
	result := &BarrierWaveGeohashResult{}
	for _, p := range primitives {
		batch, _, ok, err := readPrimitiveIntoBatch(source, p)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("invalid rail primitive")
		}
		result.SourceArea += waveMeshArea(common.RoadSurfaceMesh{Positions: batch.pos, Indices: batch.indices})
	}
	for _, n := range source.Nodes {
		tr, _, _, _, ok, err := readInstancedNodeAttributes(source, n)
		if err != nil {
			return nil, err
		}
		if ok {
			result.SourceInstances += len(tr)
		}
	}
	frame, err := aabb.NewENUFrame(ctx.BasePoint[0], ctx.BasePoint[1], ctx.BasePoint[2])
	if err != nil {
		return nil, err
	}
	cells := []waveSliceCell{}
	for _, hash := range roadSliceGeohashes(ctx.Region, opt.Precision) {
		cells = append(cells, waveSliceCell{hash, roadSliceProjectedBoundary(frame, geohash.BoundingBox(hash), ctx.BasePoint[2])})
	}
	// Compute ownership once; do not rescan every cell for every source instance
	// while building each output tile.
	owners := make(map[int][]int)
	for nodeIndex, node := range source.Nodes {
		translations, _, _, _, instanced, err := readInstancedNodeAttributes(source, node)
		if err != nil {
			return nil, err
		}
		if !instanced {
			continue
		}
		owners[nodeIndex] = make([]int, len(translations))
		for i, p := range translations {
			owners[nodeIndex][i] = waveCellOwner(p, cells)
			if owners[nodeIndex][i] < 0 {
				return nil, fmt.Errorf("support anchor outside projected cells: %v", p)
			}
		}
	}
	encoded, err := waveEncode(source)
	if err != nil {
		return nil, err
	}
	root := tilesetJSON{Asset: tilesetAsset{Version: "1.1"}, GeometricError: 100, Root: tilesetRoot{GeometricError: 100, Refine: "ADD"}}
	transform := cesium.GenerateTransformMatrixUP(ctx.BasePoint[0], ctx.BasePoint[1], ctx.BasePoint[2], 0)
	root.Root.Transform = append([]float64(nil), transform[:]...)
	var lo, hi [3]float64
	haveBounds := false
	for cellIndex, cell := range cells {
		doc := gltf.NewDocument()
		if err := gltf.NewDecoder(bytes.NewReader(encoded)).Decode(doc); err != nil {
			return nil, err
		}
		if err := waveNormalizeExtensions(doc); err != nil {
			return nil, err
		}
		remove := map[int]bool{}
		for nodeIndex, node := range doc.Nodes {
			if node.Mesh == nil {
				continue
			}
			tr, rot, scale, ids, instanced, err := readInstancedNodeAttributes(doc, node)
			if err != nil {
				return nil, err
			}
			if instanced {
				keepT := [][3]float32{}
				keepR := [][4]float32{}
				keepS := [][3]float32{}
				keepIDs := []uint32{}
				for i, p := range tr {
					if owners[nodeIndex][i] == cellIndex {
						keepT = append(keepT, p)
						keepR = append(keepR, rot[i])
						keepS = append(keepS, scale[i])
						keepIDs = append(keepIDs, ids[i])
					}
				}
				if len(keepT) == 0 {
					remove[nodeIndex] = true
					continue
				}
				result.SlicedInstances += len(keepT)
				attrs := node.Extensions["EXT_mesh_gpu_instancing"].(map[string]any)["attributes"].(map[string]any)
				attrs["TRANSLATION"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, keepT)
				attrs["ROTATION"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, keepR)
				attrs["SCALE"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, keepS)
				attrs["_FEATURE_ID_0"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, keepIDs)
				continue
			}
			clippedPrimitives := []*gltf.Primitive{}
			for _, p := range doc.Meshes[*node.Mesh].Primitives {
				batch, _, ok, err := readPrimitiveIntoBatch(doc, p)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("invalid rail primitive")
				}
				mesh := common.RoadSurfaceMesh{Positions: batch.pos, Normals: batch.normals, UVs: batch.uv, Indices: batch.indices}
				cut, err := common.ClipRoadSurfaceMesh(mesh, cell.boundary)
				if err != nil {
					return nil, err
				}
				cut = waveDropOtherSeamFaces(cut, cells, cellIndex)
				if len(cut.Indices) == 0 {
					continue
				}
				if err := roadSliceCheckInside(cut, cell.boundary); err != nil {
					return nil, err
				}
				result.SlicedArea += waveMeshArea(cut)
				// Recreate constant IDs for the newly interpolated vertices.
				if len(batch.featureIDs) == 0 {
					return nil, fmt.Errorf("missing rail feature IDs")
				}
				for _, id := range batch.featureIDs {
					if id != batch.featureIDs[0] {
						return nil, fmt.Errorf("rail primitive contains multiple features")
					}
				}
				featureIDs := make([]uint32, len(cut.Positions))
				for i := range featureIDs {
					featureIDs[i] = batch.featureIDs[0]
				}
				p.Attributes[gltf.POSITION] = modeler.WritePosition(doc, cut.Positions)
				p.Attributes[gltf.NORMAL] = modeler.WriteNormal(doc, cut.Normals)
				p.Attributes[gltf.TEXCOORD_0] = modeler.WriteTextureCoord(doc, cut.UVs)
				p.Attributes["_FEATURE_ID_0"] = modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, featureIDs)
				p.Indices = gltf.Index(modeler.WriteIndices(doc, cut.Indices))
				clippedPrimitives = append(clippedPrimitives, p)
			}
			if len(clippedPrimitives) == 0 {
				remove[nodeIndex] = true
			} else {
				doc.Meshes[*node.Mesh].Primitives = clippedPrimitives
			}
		}
		removeNodesAndRemapRefs(doc, remove)
		if len(doc.Nodes) == 0 {
			continue
		}
		wavePruneMeshes(doc)
		if err := CompactDocument(doc); err != nil {
			return nil, err
		}
		minv, maxv, err := waveRenderedBounds(doc)
		if err != nil {
			return nil, err
		}
		box := buildTilesetBox(minv, maxv, TilesetBuildOptions{PaddingX: 5, PaddingZ: 5, PaddingY: 2, MinPaddingXZ: 12, MinPaddingY: 6, PaddingRatioXZ: .03, PaddingRatioY: .2})
		for axis := 0; axis < 3; axis++ {
			a, b := box[axis]-box[3+4*axis], box[axis]+box[3+4*axis]
			if !haveBounds || a < lo[axis] {
				lo[axis] = a
			}
			if !haveBounds || b > hi[axis] {
				hi[axis] = b
			}
		}
		haveBounds = true
		tileDir := filepath.Join(opt.OutputDir, "tiles", cell.hash)
		if err := os.MkdirAll(tileDir, 0755); err != nil {
			return nil, err
		}
		glb, err := waveEncode(doc)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(tileDir, "barrier.glb")
		if err := os.WriteFile(path, glb, 0644); err != nil {
			return nil, err
		}
		count := 0
		for _, m := range doc.Meshes {
			count += len(m.Primitives)
		}
		result.Tiles = append(result.Tiles, RoadSurfaceGeohashTile{Geohash: cell.hash, GLBPath: path, PrimitiveCount: count, GLBBytes: len(glb)})
		root.Root.Children = append(root.Root.Children, tilesetRoot{BoundingVolume: tilesetBoundingVolume{Box: box}, Refine: "ADD", Content: &tilesetContent{URI: "tiles/" + cell.hash + "/barrier.glb"}})
	}
	if len(result.Tiles) == 0 {
		return nil, fmt.Errorf("no nonempty barrier tiles")
	}
	if result.SourceInstances != result.SlicedInstances {
		return nil, fmt.Errorf("support instance count changed: %d -> %d", result.SourceInstances, result.SlicedInstances)
	}
	if result.SourceArea <= 0 || math.Abs(result.SourceArea-result.SlicedArea)/result.SourceArea > 1e-5 {
		return nil, fmt.Errorf("rail area changed: %f -> %f", result.SourceArea, result.SlicedArea)
	}
	box := make([]float64, 12)
	for axis := 0; axis < 3; axis++ {
		box[axis] = (lo[axis] + hi[axis]) / 2
		box[3+4*axis] = (hi[axis] - lo[axis]) / 2
	}
	root.Root.BoundingVolume.Box = box
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	result.TilesetPath = filepath.Join(opt.OutputDir, "tileset.json")
	if err := os.WriteFile(result.TilesetPath, data, 0644); err != nil {
		return nil, err
	}
	return result, nil
}

func waveEncode(doc *gltf.Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := gltf.NewEncoder(&buf)
	enc.AsBinary = true
	err := enc.Encode(doc)
	return buf.Bytes(), err
}

// glTF decodes unregistered extensions as RawMessage; compaction expects maps.
func waveNormalizeExtensions(doc *gltf.Document) error {
	normalize := func(ext gltf.Extensions) error {
		for key, value := range ext {
			if _, ok := value.(map[string]any); ok {
				continue
			}
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			var obj map[string]any
			if err := json.Unmarshal(data, &obj); err != nil {
				return err
			}
			ext[key] = obj
		}
		return nil
	}
	if err := normalize(doc.Extensions); err != nil {
		return err
	}
	for _, n := range doc.Nodes {
		if err := normalize(n.Extensions); err != nil {
			return err
		}
	}
	for _, m := range doc.Meshes {
		for _, p := range m.Primitives {
			if err := normalize(p.Extensions); err != nil {
				return err
			}
		}
	}
	return nil
}

func waveCellOwner(p [3]float32, cells []waveSliceCell) int {
	for i, c := range cells {
		area := 0.
		for j, a := range c.boundary {
			b := c.boundary[(j+1)%len(c.boundary)]
			area += a[0]*b[1] - b[0]*a[1]
		}
		sign := 1.
		if area < 0 {
			sign = -1
		}
		inside := true
		for j, a := range c.boundary {
			b := c.boundary[(j+1)%len(c.boundary)]
			distance := sign * ((b[0]-a[0])*(float64(p[2])-a[1]) - (b[1]-a[1])*(float64(p[0])-a[0])) / math.Hypot(b[0]-a[0], b[1]-a[1])
			if distance < -0.00001 {
				inside = false
				break
			}
		}
		if inside {
			return i
		}
	}
	return -1
}

// A vertical face coincident with a shared cell edge has nonzero 3D area.
// Keep it only in the deterministic first cell, avoiding duplicate seam faces.
func waveDropOtherSeamFaces(mesh common.RoadSurfaceMesh, cells []waveSliceCell, index int) common.RoadSurfaceMesh {
	indices := []uint32{}
	for i := 0; i < len(mesh.Indices); i += 3 {
		ids := mesh.Indices[i : i+3]
		seam := false
		for j, a := range cells[index].boundary {
			b := cells[index].boundary[(j+1)%4]
			onEdge := true
			for _, id := range ids {
				p := mesh.Positions[id]
				d := ((b[0]-a[0])*(float64(p[2])-a[1]) - (b[1]-a[1])*(float64(p[0])-a[0])) / math.Hypot(b[0]-a[0], b[1]-a[1])
				if math.Abs(d) > 0.00001 {
					onEdge = false
				}
			}
			if onEdge {
				seam = true
				break
			}
		}
		if seam {
			a, b, c := mesh.Positions[ids[0]], mesh.Positions[ids[1]], mesh.Positions[ids[2]]
			center := [3]float32{(a[0] + b[0] + c[0]) / 3, (a[1] + b[1] + c[1]) / 3, (a[2] + b[2] + c[2]) / 3}
			if waveCellOwner(center, cells) != index {
				continue
			}
		}
		indices = append(indices, ids...)
	}
	mesh.Indices = indices
	return mesh
}

func waveMeshArea(mesh common.RoadSurfaceMesh) float64 {
	area := 0.
	for i := 0; i < len(mesh.Indices); i += 3 {
		a, b, c := mesh.Positions[mesh.Indices[i]], mesh.Positions[mesh.Indices[i+1]], mesh.Positions[mesh.Indices[i+2]]
		u := [3]float64{float64(b[0]) - float64(a[0]), float64(b[1]) - float64(a[1]), float64(b[2]) - float64(a[2])}
		v := [3]float64{float64(c[0]) - float64(a[0]), float64(c[1]) - float64(a[1]), float64(c[2]) - float64(a[2])}
		x, y, z := u[1]*v[2]-u[2]*v[1], u[2]*v[0]-u[0]*v[2], u[0]*v[1]-u[1]*v[0]
		area += math.Sqrt(x*x+y*y+z*z) / 2
	}
	return area
}

func waveRenderedBounds(doc *gltf.Document) ([3]float64, [3]float64, error) {
	var minv, maxv [3]float64
	found := false
	err := waveVisitPositions(doc, func(p [3]float64) {
		for axis := 0; axis < 3; axis++ {
			if !found || p[axis] < minv[axis] {
				minv[axis] = p[axis]
			}
			if !found || p[axis] > maxv[axis] {
				maxv[axis] = p[axis]
			}
		}
		found = true
	})
	if err != nil {
		return minv, maxv, err
	}
	if !found {
		return minv, maxv, fmt.Errorf("no rendered positions")
	}
	return minv, maxv, nil
}

// Visit actual support vertices after instance TRS, not the support template at
// the origin. Generated nodes are flat and have no additional node transforms.
func waveVisitPositions(doc *gltf.Document, visit func([3]float64)) error {
	for _, node := range doc.Nodes {
		if node.Mesh == nil {
			continue
		}
		tr, rot, scale, _, instanced, err := readInstancedNodeAttributes(doc, node)
		if err != nil {
			return err
		}
		for _, prim := range doc.Meshes[*node.Mesh].Primitives {
			pos, err := modeler.ReadPosition(doc, doc.Accessors[prim.Attributes[gltf.POSITION]], nil)
			if err != nil {
				return err
			}
			if !instanced {
				for _, p := range pos {
					visit([3]float64{float64(p[0]), float64(p[1]), float64(p[2])})
				}
				continue
			}
			for i, t := range tr {
				q := rot[i]
				for _, p := range pos {
					v := [3]float64{float64(p[0] * scale[i][0]), float64(p[1] * scale[i][1]), float64(p[2] * scale[i][2])}
					qx, qy, qz, qw := float64(q[0]), float64(q[1]), float64(q[2]), float64(q[3])
					tx, ty, tz := 2*(qy*v[2]-qz*v[1]), 2*(qz*v[0]-qx*v[2]), 2*(qx*v[1]-qy*v[0])
					visit([3]float64{v[0] + qw*tx + qy*tz - qz*ty + float64(t[0]), v[1] + qw*ty + qz*tx - qx*tz + float64(t[1]), v[2] + qw*tz + qx*ty - qy*tx + float64(t[2])})
				}
			}
		}
	}
	return nil
}

// Remove orphan templates before accessor compaction; otherwise unreferenced
// meshes can retain accessor indices that no longer exist in the output GLB.
func wavePruneMeshes(doc *gltf.Document) {
	refs := collectReferencedMeshes(doc)
	remap := make(map[int]int, len(refs))
	meshes := make([]*gltf.Mesh, 0, len(refs))
	for _, old := range refs {
		remap[old] = len(meshes)
		meshes = append(meshes, doc.Meshes[old])
	}
	for _, node := range doc.Nodes {
		if node.Mesh != nil {
			node.Mesh = gltf.Index(remap[*node.Mesh])
		}
	}
	doc.Meshes = meshes
}

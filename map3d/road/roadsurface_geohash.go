package road

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cesium-tileset-tool/map3d/road/common"
	"cesium-tileset-tool/mapmodel/aabb"

	"github.com/mmcloughlin/geohash"
	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// RoadSurfaceGeohashOptions configures fixed-precision slicing in one shared frame.
// OutputDir is the tileset directory itself. Precision zero defaults to six.
// Bound keeps complete WGS84 features; crossing features are excluded.
// BuildOptions defaults to DefaultBuildOptions; input centerlines are enabled.
type RoadSurfaceGeohashOptions struct {
	OutputDir    string
	Precision    int
	Bound        string
	TexturePath  string
	BuildOptions *BuildOptions
}

type RoadSurfaceGeohashTile struct {
	Geohash        string
	GLBPath        string
	PrimitiveCount int
	GLBBytes       int
}

type RoadSurfaceGeohashResult struct {
	TilesetPath                string
	Tiles                      []RoadSurfaceGeohashTile
	RoadCount, CenterlineCount int
	SourceArea, SlicedArea     float64
	SplitRoad                  bool
}

// BuildRoadSurfaceGeohashSlices triangulates original polygons once, clips their
// meshes into projected geohash cells, and writes GLBs plus a root tileset.
// Inputs are WGS84 GeoJSON collections and are not mutated. No environment
// variables, test fixtures or testing.T are required. Existing output files are
// not removed; use a fresh directory when changing inputs or precision.
// This remains a fixed-precision prototype, without LOD or per-tile rebasing.
func BuildRoadSurfaceGeohashSlices(roads, centerlines *geojson.FeatureCollection, opt RoadSurfaceGeohashOptions) (*RoadSurfaceGeohashResult, error) {
	precision := opt.Precision
	if precision == 0 {
		precision = 6
	}
	if precision < 4 || precision > 7 {
		return nil, fmt.Errorf("precision must be 4..7")
	}
	if opt.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	if roads == nil {
		return nil, fmt.Errorf("road surface collection is required")
	}
	if centerlines == nil {
		centerlines = &geojson.FeatureCollection{}
	}
	if opt.Bound != "" {
		bound, err := ParseRoadSurfaceBound(opt.Bound)
		if err != nil {
			return nil, fmt.Errorf("input bound: %w", err)
		}
		roads = FilterRoadSurfaceInputBound(roads, bound)
		centerlines = FilterRoadSurfaceInputBound(centerlines, bound)
	}
	if len(roads.Features) == 0 {
		return nil, fmt.Errorf("no complete road surface features")
	}
	result := &RoadSurfaceGeohashResult{}
	base, region, err := calcTileContextFromFeatureCollections(roads, centerlines)
	if err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	ctx := TileContext{TileID: "road-surface-geohash-source", Region: region, BasePoint: base, SRID: 4326, UseENU: true}
	options := DefaultBuildOptions()
	if opt.BuildOptions != nil {
		options = *opt.BuildOptions
	}
	options.UseInputCenterline = true
	builder, err := NewRoadTileBuilder(ctx, &options)
	if err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	builder.AddCenterlineFeatures(RoadCenterlineFeaturesFromGeoJSON(centerlines)...)
	sourceRuntime, err := builder.newRuntime(gltf.NewDocument())
	if err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	if err := builder.preloadCenterlines(sourceRuntime); err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	features := RoadSurfaceFeaturesFromGeoJSON(roads, opt.TexturePath)
	sourceMeshes := make([][]common.RoadSurfaceMesh, len(features))
	originalArea := 0.0
	for i := range features {
		// Keep business fields, including fields beyond id/type.
		fields := featureFieldsFromGeoJSON(features[i].Fields)
		fields["type"] = "road_surface"
		features[i].Features = []FeatureFields{fields}
		rings, err := sourceRuntime.Coordinates.ToLocalSurface(features[i])
		if err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		sourceMeshes[i], err = sourceRuntime.Meshes.BuildRoadSurfaceMeshes(features[i], rings)
		if err != nil {
			return nil, fmt.Errorf("road %d: %v", i, err)
		}
		for _, mesh := range sourceMeshes[i] {
			originalArea += roadSliceMeshArea(mesh)
		}
	}
	if originalArea <= 0 {
		return nil, fmt.Errorf("source road mesh is empty")
	}
	hashes := roadSliceGeohashes(region, precision)
	frame, err := aabb.NewENUFrame(base[0], base[1], base[2])
	if err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	outDir := opt.OutputDir
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	root := tilesetJSON{Asset: tilesetAsset{Version: "1.1"}, GeometricError: 100, Root: tilesetRoot{Refine: "ADD", GeometricError: 100}}
	var outputMin, outputMax [3]float64
	haveBounds := false
	clippedArea := 0.0
	tileCount := 0
	splitRoad := false
	roadTileCounts := make([]int, len(features))
	for _, hash := range hashes {
		box := geohash.BoundingBox(hash)
		boundary := roadSliceProjectedBoundary(frame, box, base[2])
		doc := gltf.NewDocument()
		tileRuntime, err := builder.newRuntime(doc)
		if err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		primitives := []*gltf.Primitive{}
		for i, meshes := range sourceMeshes {
			clipped := []common.RoadSurfaceMesh{}
			for _, mesh := range meshes {
				cut, err := common.ClipRoadSurfaceMesh(mesh, boundary)
				if err != nil {
					return nil, fmt.Errorf("road surface geohash slices: %w", err)
				}
				if len(cut.Indices) == 0 {
					continue
				}
				if err := roadSliceCheckInside(cut, boundary); err != nil {
					return nil, err
				}
				clippedArea += roadSliceMeshArea(cut)
				clipped = append(clipped, cut)
			}
			if len(clipped) == 0 {
				continue
			}
			roadTileCounts[i]++
			if roadTileCounts[i] > 1 {
				splitRoad = true
			}
			prs, err := tileRuntime.Meshes.WriteRoadSurfaceMeshes(features[i], clipped)
			if err != nil {
				return nil, fmt.Errorf("road surface geohash slices: %w", err)
			}
			primitives = append(primitives, prs...)
		}
		if len(primitives) == 0 {
			continue
		}
		if err := tileRuntime.Metadata.Finalize(); err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		primitives, err = mergePrimitivesByBatch(doc, primitives)
		if err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		appendPrimitivesAsMesh(doc, primitives)
		if err := CompactDocument(doc); err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		var buffer bytes.Buffer
		encoder := gltf.NewEncoder(&buffer)
		encoder.AsBinary = true
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		tileDir := filepath.Join(outDir, "tiles", hash)
		if err := os.MkdirAll(tileDir, 0755); err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		glbPath := filepath.Join(tileDir, "surface.glb")
		if err := os.WriteFile(glbPath, buffer.Bytes(), 0644); err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		decoded, err := gltf.Open(glbPath)
		if err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		if len(decoded.Meshes) == 0 || decoded.Extensions["EXT_structural_metadata"] == nil {
			return nil, fmt.Errorf("missing geometry or feature metadata")
		}
		contentURI := "tiles/" + hash + "/surface.glb"
		leafJSON, err := BuildTilesetJSONForGLB(TilesetBuildOptions{GLBPath: glbPath, ContentURI: contentURI, BasePoint: base})
		if err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		var leaf tilesetJSON
		if err := json.Unmarshal(leafJSON, &leaf); err != nil {
			return nil, fmt.Errorf("road surface geohash slices: %w", err)
		}
		root.Root.Transform = leaf.Root.Transform
		leaf.Root.Transform = nil
		root.Root.Children = append(root.Root.Children, leaf.Root)
		b := leaf.Root.BoundingVolume.Box
		for axis := 0; axis < 3; axis++ {
			lo, hi := b[axis]-b[3+axis*4], b[axis]+b[3+axis*4]
			if !haveBounds || lo < outputMin[axis] {
				outputMin[axis] = lo
			}
			if !haveBounds || hi > outputMax[axis] {
				outputMax[axis] = hi
			}
		}
		haveBounds = true
		tileCount++
		result.Tiles = append(result.Tiles, RoadSurfaceGeohashTile{Geohash: hash, GLBPath: glbPath, PrimitiveCount: len(primitives), GLBBytes: buffer.Len()})
	}
	if tileCount == 0 {
		return nil, fmt.Errorf("no nonempty road surface tiles generated")
	}
	// The projected cells must partition the source mesh without area loss/overlap.
	if math.Abs(clippedArea-originalArea)/originalArea > 1e-5 {
		return nil, fmt.Errorf("area mismatch: source=%f sliced=%f", originalArea, clippedArea)
	}
	b := make([]float64, 12)
	for i := 0; i < 3; i++ {
		b[i] = (outputMin[i] + outputMax[i]) / 2
		b[3+i*4] = (outputMax[i] - outputMin[i]) / 2
	}
	root.Root.BoundingVolume.Box = b
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	path := filepath.Join(outDir, "tileset.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, fmt.Errorf("road surface geohash slices: %w", err)
	}
	result.TilesetPath = path
	result.RoadCount = len(features)
	result.CenterlineCount = len(centerlines.Features)
	result.SourceArea, result.SlicedArea = originalArea, clippedArea
	result.SplitRoad = splitRoad
	return result, nil
}

func roadSliceGeohashes(region [6]float64, precision int) []string {
	cell := geohash.BoundingBox(geohash.EncodeWithPrecision((region[1]+region[3])/2, (region[0]+region[2])/2, uint(precision)))
	dx, dy := cell.MaxLng-cell.MinLng, cell.MaxLat-cell.MinLat
	// Include one neighboring row/column: projection and mesh interpolation can
	// move extreme vertices slightly outside the input geographic bounding box.
	startX := math.Floor((region[0]+180)/dx)*dx - 180 - dx
	startY := math.Floor((region[1]+90)/dy)*dy - 90 - dy
	seen := map[string]bool{}
	for y := startY; y <= region[3]+dy; y += dy {
		for x := startX; x <= region[2]+dx; x += dx {
			seen[geohash.EncodeWithPrecision(y+dy/2, x+dx/2, uint(precision))] = true
		}
	}
	out := make([]string, 0, len(seen))
	for hash := range seen {
		out = append(out, hash)
	}
	sort.Strings(out)
	return out
}
func roadSliceProjectedBoundary(frame *aabb.ENUFrame, box geohash.Box, height float64) [][2]float64 {
	corners := [][2]float64{{box.MinLng, box.MinLat}, {box.MaxLng, box.MinLat}, {box.MaxLng, box.MaxLat}, {box.MinLng, box.MaxLat}}
	out := make([][2]float64, 4)
	for i, p := range corners {
		v := frame.Offset(p[0], p[1], height)
		out[i] = [2]float64{v[0], v[1]}
	}
	return out
}
func roadSliceMeshArea(mesh common.RoadSurfaceMesh) float64 {
	area := 0.0
	for i := 0; i < len(mesh.Indices); i += 3 {
		a, b, c := mesh.Positions[mesh.Indices[i]], mesh.Positions[mesh.Indices[i+1]], mesh.Positions[mesh.Indices[i+2]]
		area += math.Abs((float64(b[0])-float64(a[0]))*(float64(c[2])-float64(a[2]))-(float64(b[2])-float64(a[2]))*(float64(c[0])-float64(a[0]))) / 2
	}
	return area
}
func roadSliceCheckInside(mesh common.RoadSurfaceMesh, boundary [][2]float64) error {
	area := 0.0
	for i, a := range boundary {
		b := boundary[(i+1)%len(boundary)]
		area += a[0]*b[1] - b[0]*a[1]
	}
	sign := 1.0
	if area < 0 {
		sign = -1
	}
	for _, p := range mesh.Positions {
		for i, a := range boundary {
			b := boundary[(i+1)%len(boundary)]
			dx, dz := b[0]-a[0], b[1]-a[1]
			distance := sign * (dx*(float64(p[2])-a[1]) - dz*(float64(p[0])-a[0])) / math.Hypot(dx, dz)
			if distance < -0.005 {
				return fmt.Errorf("clipped vertex outside tile: %v distance=%f", p, distance)
			}
		}
	}
	return nil
}

// ParseRoadSurfaceBound accepts WGS84 degrees in west,south,east,north order.
func ParseRoadSurfaceBound(value string) ([4]float64, error) {
	var bound [4]float64
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return bound, fmt.Errorf("expected west,south,east,north")
	}
	for i, part := range parts {
		number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return bound, fmt.Errorf("coordinate %d must be finite", i+1)
		}
		bound[i] = number
	}
	if bound[0] < -180 || bound[2] > 180 || bound[1] < -90 || bound[3] > 90 || bound[0] >= bound[2] || bound[1] >= bound[3] {
		return bound, fmt.Errorf("require -180 <= west < east <= 180 and -90 <= south < north <= 90")
	}
	return bound, nil
}

// FilterRoadSurfaceInputBound keeps entire features, never clipped fragments.
// Boundary contact is included; holes and all multipart components must fit.
func FilterRoadSurfaceInputBound(fc *geojson.FeatureCollection, bound [4]float64) *geojson.FeatureCollection {
	if fc == nil {
		return &geojson.FeatureCollection{}
	}
	filtered := *fc
	filtered.Features = nil
	for _, feature := range fc.Features {
		if feature != nil && roadSliceGeometryInsideBound(feature.Geometry, bound) {
			filtered.Features = append(filtered.Features, feature)
		}
	}
	return &filtered
}

func roadSliceGeometryInsideBound(g geom.T, bound [4]float64) bool {
	if g == nil {
		return false
	}
	if collection, ok := g.(*geom.GeometryCollection); ok {
		if collection.NumGeoms() == 0 {
			return false
		}
		for i := 0; i < collection.NumGeoms(); i++ {
			if !roadSliceGeometryInsideBound(collection.Geom(i), bound) {
				return false
			}
		}
		return true
	}
	flat, stride := g.FlatCoords(), g.Stride()
	if stride < 2 || len(flat) == 0 || len(flat)%stride != 0 {
		return false
	}
	for i := 0; i < len(flat); i += stride {
		x, y := flat[i], flat[i+1]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < bound[0] || x > bound[2] || y < bound[1] || y > bound[3] {
			return false
		}
	}
	return true
}

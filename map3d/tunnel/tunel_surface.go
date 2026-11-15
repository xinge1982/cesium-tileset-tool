package tunnel

import (
	"cesium-tileset-tool/map3d/mgltf"
	"cesium-tileset-tool/map3d/road"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/qmuntal/gltf"
	log "github.com/sirupsen/logrus"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

const (
	wgs84A  = 6378137.0
	wgs84F  = 1.0 / 298.257223563
	wgs84E2 = wgs84F * (2 - wgs84F)
)

func TunnelWallPrimitive(
	doc *gltf.Document,
	pos [][3]float32,
	surfaceType string,
	material int,
	params mgltf.UvsParams,
) (*gltf.Primitive, error) {

	indices, err := triangulateTunnelWallCDT(pos)
	if err != nil {
		return nil, err
	}

	var normals [][3]float32
	normals = GenerateSmoothNormalsByIndices(pos, indices)
	return mgltf.PrimitiveWithNormals(doc, pos, indices, normals, material, params)
}

func TunnelPortalPrimitive(
	doc *gltf.Document,
	pos [][3]float32,
	material int,
	params mgltf.UvsParams,
) (*gltf.Primitive, error) {

	indices, err := mgltf.CDTRingKeepWinding(pos)
	if err != nil {
		return nil, err
	}

	var normals [][3]float32
	normals = GenerateWallNormals(pos, indices)
	return mgltf.PrimitiveWithNormalsTile(doc, pos, indices, normals, material, params.FlipU, params.FlipV)
}

func TunnelSignPrimitive(
	doc *gltf.Document,
	pos [][3]float32,
	material int,
	params mgltf.UvsParams,
) (*gltf.Primitive, error) {

	indices, err := mgltf.CDTRingKeepWinding(pos)
	if err != nil {
		return nil, err
	}

	var normals [][3]float32
	normals = GenerateWallNormals(pos, indices)
	return mgltf.SignNamePrimitiveWithNormals(doc, pos, indices, normals, material, params.FlipU, params.FlipV)
}

func TunnelPlatformPrimitive(
	doc *gltf.Document,
	pos [][3]float32,
	surfaceType string,
	material int,
	params mgltf.UvsParams,
) (*gltf.Primitive, error) {

	indices, err := mgltf.CDTRingKeepWinding(pos)
	if err != nil {
		return nil, err
	}

	var normals [][3]float32

	switch surfaceType {
	case "platform_inner_wall", "platform_outer_wall":
		normals = GenerateWallNormals(pos, indices)
	default:
		normals = GenerateSmoothNormals(pos, indices)
	}

	return mgltf.PrimitiveWithNormals(doc, pos, indices, normals, material, params)
}

func deg2rad(d float64) float64 { return d * math.Pi / 180 }

func wgs84ToECEF(lonDeg, latDeg, h float64) (x, y, z float64) {
	lon := deg2rad(lonDeg)
	lat := deg2rad(latDeg)

	sinLat, cosLat := math.Sin(lat), math.Cos(lat)
	sinLon, cosLon := math.Sin(lon), math.Cos(lon)

	N := wgs84A / math.Sqrt(1-wgs84E2*sinLat*sinLat)

	x = (N + h) * cosLat * cosLon
	y = (N + h) * cosLat * sinLon
	z = (N*(1-wgs84E2) + h) * sinLat
	return
}

func ecefDeltaToENU(dx, dy, dz, lon0Deg, lat0Deg float64) (east, north, up float64) {
	lon0 := deg2rad(lon0Deg)
	lat0 := deg2rad(lat0Deg)

	sinLat0, cosLat0 := math.Sin(lat0), math.Cos(lat0)
	sinLon0, cosLon0 := math.Sin(lon0), math.Cos(lon0)

	east = -sinLon0*dx + cosLon0*dy
	north = -sinLat0*cosLon0*dx - sinLat0*sinLon0*dy + cosLat0*dz
	up = cosLat0*cosLon0*dx + cosLat0*sinLon0*dy + sinLat0*dz
	return
}

// Convert WGS84 coordinates to the local tangent ENU frame.
// We first convert WGS84 -> ECEF, then normalize using the origin, then rotate into ENU.
func wgs84ToLocal(lon, lat, h, originLon, originLat, originH float64) (east, north, up float64) {
	x, y, z := wgs84ToECEF(lon, lat, h)
	ox, oy, oz := wgs84ToECEF(originLon, originLat, originH)
	return ecefDeltaToENU(x-ox, y-oy, z-oz, originLon, originLat)
}

func outerRingFlatCoordsFromWGS84Geom(g geom.T) ([]float64, int, bool) {
	switch gg := g.(type) {
	case *geom.Polygon:
		flat := gg.FlatCoords()
		ends := gg.Ends()
		if len(ends) > 0 && ends[0] <= len(flat) {
			return flat[:ends[0]], gg.Stride(), true
		}
		return flat, gg.Stride(), len(flat) > 0
	case *geom.MultiPolygon:
		if gg.NumPolygons() == 0 {
			return nil, 0, false
		}
		p := gg.Polygon(0)
		flat := p.FlatCoords()
		ends := p.Ends()
		if len(ends) > 0 && ends[0] <= len(flat) {
			return flat[:ends[0]], p.Stride(), true
		}
		return flat, p.Stride(), len(flat) > 0
	default:
		return nil, 0, false
	}
}

func normalizeClosedRingXZ(pos [][3]float32) [][3]float32 {
	if len(pos) < 2 {
		return pos
	}
	first := pos[0]
	last := pos[len(pos)-1]
	// compare in ground plane (X,Z)
	dx := float64(first[0] - last[0])
	dz := float64(first[2] - last[2])
	if dx*dx+dz*dz < 1e-8 {
		return pos[:len(pos)-1]
	}
	return pos
}

func isFinite(v float64) bool {
	return !math.IsInf(v, 0) && !math.IsNaN(v)
}

func writeRoadTileOutputs(outDir, baseName string, glb []byte, basePoint []float64, writeDefault bool) error {
	glbPath := filepath.Join(outDir, baseName+".glb")
	if err := os.WriteFile(glbPath, glb, 0o644); err != nil {
		return fmt.Errorf("write glb: %v", err)
	}
	st, err := os.Stat(glbPath)
	if err != nil {
		return fmt.Errorf("stat glb: %v", err)
	}
	if st.Size() == 0 {
		return fmt.Errorf("glb is empty: %s", glbPath)
	}

	tilesetPath := filepath.Join(outDir, baseName+".tileset.json")
	if err := road.WriteTilesetJSONForGLB(road.TilesetBuildOptions{
		GLBPath:        glbPath,
		OutputPath:     tilesetPath,
		ContentURI:     filepath.Base(glbPath),
		BasePoint:      basePoint,
		GeometricError: 0,
		PaddingXY:      12,
		PaddingY:       6,
		MinPaddingXZ:   12,
		MinPaddingY:    6,
		PaddingRatioXZ: 0.03,
		PaddingRatioY:  0.2,
	}); err != nil {
		return fmt.Errorf("write tileset json: %v", err)
	}
	if _, err := os.Stat(tilesetPath); err != nil {
		return fmt.Errorf("stat tileset json: %v", err)
	}
	defaultTilesetPath := filepath.Join(outDir, "tileset.json")
	if writeDefault {
		if err := road.WriteTilesetJSONForGLB(road.TilesetBuildOptions{
			GLBPath:        glbPath,
			OutputPath:     defaultTilesetPath,
			ContentURI:     filepath.Base(glbPath),
			BasePoint:      basePoint,
			GeometricError: 0,
			PaddingXY:      12,
			PaddingY:       6,
			MinPaddingXZ:   12,
			MinPaddingY:    6,
			PaddingRatioXZ: 0.03,
			PaddingRatioY:  0.2,
		}); err != nil {
			return fmt.Errorf("write default tileset json: %v", err)
		}
		if _, err := os.Stat(defaultTilesetPath); err != nil {
			return fmt.Errorf("stat default tileset json: %v", err)
		}
	}

	log.Infof("generated glb: %s (%d bytes)", glbPath, st.Size())
	log.Infof("generated tileset: %s", tilesetPath)
	if writeDefault {
		log.Infof("generated default tileset: %s", defaultTilesetPath)
	}

	return nil
}

func calcTileContextFromFeatureCollections(collections ...*geojson.FeatureCollection) ([]float64, [6]float64, error) {
	var region [6]float64
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)

	for _, fc := range collections {
		if fc == nil {
			continue
		}
		for i := range fc.Features {
			g := fc.Features[i].Geometry
			if g == nil {
				continue
			}
			flat := g.FlatCoords()
			stride := g.Stride()
			if stride < 2 {
				continue
			}
			for j := 0; j+stride-1 < len(flat); j += stride {
				lon := flat[j]
				lat := flat[j+1]
				if lon < minLon {
					minLon = lon
				}
				if lon > maxLon {
					maxLon = lon
				}
				if lat < minLat {
					minLat = lat
				}
				if lat > maxLat {
					maxLat = lat
				}
			}
		}
	}
	if math.IsInf(minLon, 0) || math.IsInf(minLat, 0) || math.IsInf(maxLon, 0) || math.IsInf(maxLat, 0) {
		return nil, region, os.ErrInvalid
	}

	basePoint := []float64{(minLon + maxLon) / 2, (minLat + maxLat) / 2, 0}
	region = [6]float64{minLon, minLat, maxLon, maxLat, 0, 0}
	return basePoint, region, nil
}

type VertexWelder struct {
	scale float32
	m     map[string]uint32
}

func NewVertexWelder(epsilon float32) *VertexWelder {
	if epsilon <= 0 {
		epsilon = 0.001
	}
	return &VertexWelder{
		scale: 1.0 / epsilon,
		m:     make(map[string]uint32),
	}
}

func (w *VertexWelder) Key(p [3]float32) string {
	x := int32(math.Round(float64(p[0] * w.scale)))
	y := int32(math.Round(float64(p[1] * w.scale)))
	z := int32(math.Round(float64(p[2] * w.scale)))
	return fmt.Sprintf("%d_%d_%d", x, y, z)
}

func (w *VertexWelder) Add(p [3]float32, positions *[][3]float32) uint32 {
	key := w.Key(p)

	if idx, ok := w.m[key]; ok {
		return idx
	}

	idx := uint32(len(*positions))
	*positions = append(*positions, p)
	w.m[key] = idx
	return idx
}

func normalizeClosedRing3D(pos [][3]float32) [][3]float32 {
	clean := make([][3]float32, 0, len(pos))
	for _, p := range pos {
		if len(clean) == 0 || p != clean[len(clean)-1] {
			clean = append(clean, p)
		}
	}
	pos = clean
	if len(pos) < 2 {
		return pos
	}

	first := pos[0]
	last := pos[len(pos)-1]

	dx := float64(first[0] - last[0])
	dy := float64(first[1] - last[1])
	dz := float64(first[2] - last[2])

	if dx*dx+dy*dy+dz*dz < 1e-8 {
		return pos[:len(pos)-1]
	}
	return pos
}

func GenerateSmoothNormals(pos [][3]float32, indices []uint32) [][3]float32 {
	normals := make([][3]float32, len(pos))

	for i := 0; i+2 < len(indices); i += 3 {
		i0 := indices[i]
		i1 := indices[i+1]
		i2 := indices[i+2]

		v0 := pos[i0]
		v1 := pos[i1]
		v2 := pos[i2]

		e1 := sub3(v1, v0)
		e2 := sub3(v2, v0)

		// 不先 normalize，直接累加 cross，等价于面积加权
		fn := cross3(e1, e2)

		normals[i0] = add3(normals[i0], fn)
		normals[i1] = add3(normals[i1], fn)
		normals[i2] = add3(normals[i2], fn)
	}

	for i := range normals {
		normals[i] = normalize3(normals[i])
	}

	return normals
}

func GenerateWallNormals(pos [][3]float32, indices []uint32) [][3]float32 {
	n := CalcAverageFaceNormal(pos, indices)

	normals := make([][3]float32, len(pos))
	for i := range normals {
		normals[i] = n
	}
	return normals
}

func GenerateArchedRoofNormals(pos [][3]float32) [][3]float32 {
	center := AveragePoint(pos)

	normals := make([][3]float32, len(pos))
	for i, p := range pos {
		n := sub3(center, p) // 指向内部
		normals[i] = normalize3(n)
	}

	return normals
}

func AveragePoint(pos [][3]float32) [3]float32 {
	var c [3]float32
	for _, p := range pos {
		c[0] += p[0]
		c[1] += p[1]
		c[2] += p[2]
	}
	if len(pos) > 0 {
		inv := float32(1.0 / float32(len(pos)))
		c[0] *= inv
		c[1] *= inv
		c[2] *= inv
	}
	return c
}

func CalcAverageFaceNormal(pos [][3]float32, indices []uint32) [3]float32 {
	var n [3]float32

	for i := 0; i+2 < len(indices); i += 3 {
		v0 := pos[indices[i]]
		v1 := pos[indices[i+1]]
		v2 := pos[indices[i+2]]

		c := cross3(sub3(v1, v0), sub3(v2, v0))
		n = add3(n, c)
	}

	return normalize3(n)
}

func dot3(a, b [3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func sub3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[0] - b[0],
		a[1] - b[1],
		a[2] - b[2],
	}
}

func add3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[0] + b[0],
		a[1] + b[1],
		a[2] + b[2],
	}
}

func cross3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func normalize3(v [3]float32) [3]float32 {
	l := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	if l == 0 {
		return [3]float32{0, 0, 1}
	}
	return [3]float32{
		v[0] / l,
		v[1] / l,
		v[2] / l,
	}
}

func GenerateRoofInnerNormalsByCenter(pos [][3]float32) [][3]float32 {
	center := AveragePoint(pos)

	normals := make([][3]float32, len(pos))
	for i, p := range pos {
		n := sub3(center, p) // 顶点 -> 中心，朝隧道内部
		normals[i] = normalize3(n)
	}
	return normals
}

func GenerateRoofUVsByZY(pos [][3]float32, repeatU, repeatV float32) [][2]float32 {
	minZ, maxZ := float32(math.Inf(1)), float32(math.Inf(-1))
	minY, maxY := float32(math.Inf(1)), float32(math.Inf(-1))

	for _, p := range pos {
		if p[2] < minZ {
			minZ = p[2]
		}
		if p[2] > maxZ {
			maxZ = p[2]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}

	dz := maxZ - minZ
	dy := maxY - minY
	if dz == 0 {
		dz = 1
	}
	if dy == 0 {
		dy = 1
	}

	uvs := make([][2]float32, len(pos))
	for i, p := range pos {
		u := (p[2] - minZ) / dz * repeatU
		v := (p[1] - minY) / dy * repeatV
		uvs[i] = [2]float32{u, v}
	}
	return uvs
}

func GenerateSmoothNormalsByIndices(
	positions [][3]float32,
	indices []uint32,
) [][3]float32 {
	normals := make([][3]float32, len(positions))

	for i := 0; i+2 < len(indices); i += 3 {
		i0 := indices[i]
		i1 := indices[i+1]
		i2 := indices[i+2]

		if int(i0) >= len(positions) ||
			int(i1) >= len(positions) ||
			int(i2) >= len(positions) {
			continue
		}

		p0 := positions[i0]
		p1 := positions[i1]
		p2 := positions[i2]

		ux := p1[0] - p0[0]
		uy := p1[1] - p0[1]
		uz := p1[2] - p0[2]

		vx := p2[0] - p0[0]
		vy := p2[1] - p0[1]
		vz := p2[2] - p0[2]

		// 按三角面绕序计算法线：cross(p1-p0, p2-p0)
		nx := uy*vz - uz*vy
		ny := uz*vx - ux*vz
		nz := ux*vy - uy*vx

		normals[i0][0] += nx
		normals[i0][1] += ny
		normals[i0][2] += nz

		normals[i1][0] += nx
		normals[i1][1] += ny
		normals[i1][2] += nz

		normals[i2][0] += nx
		normals[i2][1] += ny
		normals[i2][2] += nz
	}

	for i := range normals {
		x := normals[i][0]
		y := normals[i][1]
		z := normals[i][2]

		l := float32(math.Sqrt(float64(x*x + y*y + z*z)))
		if l < 1e-8 {
			normals[i] = [3]float32{0, 1, 0}
			continue
		}

		normals[i][0] = x / l
		normals[i][1] = y / l
		normals[i][2] = z / l
	}

	return normals
}

func TriangulateRingKeepWinding(pos [][3]float32) []uint32 {
	if len(pos) < 3 {
		return nil
	}

	indices := make([]uint32, 0, (len(pos)-2)*3)

	for i := 1; i+1 < len(pos); i++ {
		indices = append(indices, 0, uint32(i), uint32(i+1))
	}

	return indices
}

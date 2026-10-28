package main

import (
	"encoding/json"
	"fmt"
	"map-tool/config"
	"math"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type Tileset struct {
	Asset          Asset   `json:"asset"`
	GeometricError float64 `json:"geometricError"`
	Root           Tile    `json:"root"`
}

type Asset struct {
	Version string `json:"version"`
}

type Tile struct {
	BoundingVolume BoundingVolume `json:"boundingVolume"`
	GeometricError float64        `json:"geometricError"`
	Refine         string         `json:"refine,omitempty"`
	Transform      *[]float64     `json:"transform,omitempty"`
	Content        *Content       `json:"content,omitempty"`
	Children       []Tile         `json:"children,omitempty"`
}

type BoundingVolume struct {
	Sphere *[]float64 `json:"sphere,omitempty"`
	Region *[]float64 `json:"region,omitempty"`
	Box    *[]float64 `json:"box,omitempty"`
}

type Content struct {
	URI string `json:"uri"`
}

type Sphere struct {
	X, Y, Z float64
	R       float64
}

type Region struct {
	West, South, East, North, MinHeight, MaxHeight float64
}

type Point3 struct {
	X, Y, Z float64
}

type Box struct {
	Center Point3
	HalfX  Point3
	HalfY  Point3
	HalfZ  Point3
}

type AABB struct {
	Min Point3
	Max Point3
}

const (
	wgs84A  = 6378137.0
	wgs84F  = 1.0 / 298.257223563
	wgs84E2 = wgs84F * (2.0 - wgs84F)
)

func ecefToCartographic(p Point3) (lon, lat, height float64) {
	// Converts WGS84 ECEF XYZ meters to cartographic longitude/latitude radians
	// and height meters. The iterative latitude solve is stable for tileset bounds.
	lon = math.Atan2(p.Y, p.X)
	r := math.Sqrt(p.X*p.X + p.Y*p.Y)
	lat = math.Atan2(p.Z, r*(1.0-wgs84E2))

	for i := 0; i < 8; i++ {
		sinLat := math.Sin(lat)
		n := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
		height = r/math.Cos(lat) - n
		lat = math.Atan2(p.Z, r*(1.0-wgs84E2*n/(n+height)))
	}

	sinLat := math.Sin(lat)
	n := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
	height = r/math.Cos(lat) - n
	return lon, lat, height
}

func regionFromPoint(p Point3) Region {
	lon, lat, height := ecefToCartographic(p)
	return Region{
		West:      lon,
		South:     lat,
		East:      lon,
		North:     lat,
		MinHeight: height,
		MaxHeight: height,
	}
}

func regionFromBoxWithTransform(b Box, transform *[]float64) Region {
	corners := boxCorners(b)
	region := regionFromPoint(transformPoint(corners[0], transform))
	for _, corner := range corners[1:] {
		region = mergeRegion(region, regionFromPoint(transformPoint(corner, transform)))
	}
	return region
}

func mergeSphere(a, b Sphere) Sphere {
	dx := b.X - a.X
	dy := b.Y - a.Y
	dz := b.Z - a.Z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)

	if a.R >= dist+b.R {
		return a
	}
	if b.R >= dist+a.R {
		return b
	}
	if dist == 0 {
		if a.R >= b.R {
			return a
		}
		return b
	}

	newR := (dist + a.R + b.R) / 2.0
	t := (newR - a.R) / dist

	return Sphere{
		X: a.X + dx*t,
		Y: a.Y + dy*t,
		Z: a.Z + dz*t,
		R: newR,
	}
}

func mergeRegion(a, b Region) Region {
	return Region{
		West:      math.Min(a.West, b.West),
		South:     math.Min(a.South, b.South),
		East:      math.Max(a.East, b.East),
		North:     math.Max(a.North, b.North),
		MinHeight: math.Min(a.MinHeight, b.MinHeight),
		MaxHeight: math.Max(a.MaxHeight, b.MaxHeight),
	}
}

func padRegion(r Region) Region {
	// Padding protects against numerical conversion error and overly tight
	// horizon/frustum culling at the root. About 0.0001 degrees is ~11 m
	// latitude; height padding is deliberately larger for tunnel/underground data.
	const angularPad = 0.0001 * math.Pi / 180.0
	const heightPad = 2500.0
	return Region{
		West:      r.West - angularPad,
		South:     r.South - angularPad,
		East:      r.East + angularPad,
		North:     r.North + angularPad,
		MinHeight: r.MinHeight - heightPad,
		MaxHeight: r.MaxHeight + heightPad,
	}
}

func boxToRegion(box []float64) []float64 {
	b := boxFromArray(box)
	r := regionFromBoxWithTransform(b, nil)

	return []float64{
		r.West,
		r.South,
		r.East,
		r.North,
		r.MinHeight,
		r.MaxHeight,
	}
}

func boxFromArray(v []float64) Box {
	return Box{
		Center: Point3{v[0], v[1], v[2]},
		HalfX:  Point3{v[3], v[4], v[5]},
		HalfY:  Point3{v[6], v[7], v[8]},
		HalfZ:  Point3{v[9], v[10], v[11]},
	}
}

func boxToArray(b Box) []float64 {
	return []float64{
		b.Center.X, b.Center.Y, b.Center.Z,
		b.HalfX.X, b.HalfX.Y, b.HalfX.Z,
		b.HalfY.X, b.HalfY.Y, b.HalfY.Z,
		b.HalfZ.X, b.HalfZ.Y, b.HalfZ.Z,
	}
}

func boxCorners(b Box) []Point3 {
	corners := make([]Point3, 0, 8)
	for _, sx := range []float64{-1, 1} {
		for _, sy := range []float64{-1, 1} {
			for _, sz := range []float64{-1, 1} {
				corners = append(corners, Point3{
					X: b.Center.X + sx*b.HalfX.X + sy*b.HalfY.X + sz*b.HalfZ.X,
					Y: b.Center.Y + sx*b.HalfX.Y + sy*b.HalfY.Y + sz*b.HalfZ.Y,
					Z: b.Center.Z + sx*b.HalfX.Z + sy*b.HalfY.Z + sz*b.HalfZ.Z,
				})
			}
		}
	}
	return corners
}

func aabbFromBox(b Box) AABB {
	return aabbFromBoxWithTransform(b, nil)
}

func transformPoint(p Point3, transform *[]float64) Point3 {
	if transform == nil {
		return p
	}
	m := *transform
	return Point3{
		X: m[0]*p.X + m[4]*p.Y + m[8]*p.Z + m[12],
		Y: m[1]*p.X + m[5]*p.Y + m[9]*p.Z + m[13],
		Z: m[2]*p.X + m[6]*p.Y + m[10]*p.Z + m[14],
	}
}

func aabbFromBoxWithTransform(b Box, transform *[]float64) AABB {
	corners := boxCorners(b)
	first := transformPoint(corners[0], transform)
	aabb := AABB{Min: first, Max: first}
	for _, corner := range corners[1:] {
		p := transformPoint(corner, transform)
		aabb.Min.X = math.Min(aabb.Min.X, p.X)
		aabb.Min.Y = math.Min(aabb.Min.Y, p.Y)
		aabb.Min.Z = math.Min(aabb.Min.Z, p.Z)
		aabb.Max.X = math.Max(aabb.Max.X, p.X)
		aabb.Max.Y = math.Max(aabb.Max.Y, p.Y)
		aabb.Max.Z = math.Max(aabb.Max.Z, p.Z)
	}
	return aabb
}

func mergeAABB(a, b AABB) AABB {
	return AABB{
		Min: Point3{
			X: math.Min(a.Min.X, b.Min.X),
			Y: math.Min(a.Min.Y, b.Min.Y),
			Z: math.Min(a.Min.Z, b.Min.Z),
		},
		Max: Point3{
			X: math.Max(a.Max.X, b.Max.X),
			Y: math.Max(a.Max.Y, b.Max.Y),
			Z: math.Max(a.Max.Z, b.Max.Z),
		},
	}
}

func boxFromAABB(aabb AABB) Box {
	halfX := (aabb.Max.X - aabb.Min.X) / 2.0
	halfY := (aabb.Max.Y - aabb.Min.Y) / 2.0
	halfZ := (aabb.Max.Z - aabb.Min.Z) / 2.0

	return Box{
		Center: Point3{
			X: (aabb.Min.X + aabb.Max.X) / 2.0,
			Y: (aabb.Min.Y + aabb.Max.Y) / 2.0,
			Z: (aabb.Min.Z + aabb.Max.Z) / 2.0,
		},
		HalfX: Point3{halfX, 0, 0},
		HalfY: Point3{0, halfY, 0},
		HalfZ: Point3{0, 0, halfZ},
	}
}

func boundingVolumeType(bv BoundingVolume) string {
	switch {
	case bv.Sphere != nil:
		return "sphere"
	case bv.Region != nil:
		return "region"
	case bv.Box != nil:
		return "box"
	default:
		return ""
	}
}

// childVersion is returned too
func loadTileset(path string) (BoundingVolume, float64, string, *[]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BoundingVolume{}, 0, "", nil, err
	}

	var ts Tileset
	if err := json.Unmarshal(data, &ts); err != nil {
		return BoundingVolume{}, 0, "", nil, err
	}

	// support child asset.version 1.0 and 1.1
	switch ts.Asset.Version {
	case "1.0", "1.1":
	default:
		return BoundingVolume{}, 0, "", nil, fmt.Errorf("unsupported asset.version %q in %s", ts.Asset.Version, path)
	}

	bv := ts.Root.BoundingVolume
	transform := ts.Root.Transform
	if transform != nil && len(*transform) != 16 {
		return BoundingVolume{}, 0, "", nil, fmt.Errorf("invalid transform in %s", path)
	}

	if bv.Sphere != nil {
		s := *bv.Sphere
		if len(s) != 4 {
			return BoundingVolume{}, 0, "", nil, fmt.Errorf("invalid sphere in %s", path)
		}
		return bv, ts.GeometricError, ts.Asset.Version, transform, nil
	}

	if bv.Region != nil {
		r := *bv.Region
		if len(r) != 6 {
			return BoundingVolume{}, 0, "", nil, fmt.Errorf("invalid region in %s", path)
		}
		return bv, ts.GeometricError, ts.Asset.Version, transform, nil
	}

	if bv.Box != nil {
		b := *bv.Box
		if len(b) != 12 {
			return BoundingVolume{}, 0, "", nil, fmt.Errorf("invalid box in %s", path)
		}
		return bv, ts.GeometricError, ts.Asset.Version, transform, nil
	}

	return BoundingVolume{}, 0, "", nil, fmt.Errorf("unsupported boundingVolume in %s: only sphere, region and box are supported", path)
}

func mergeTilesets() {
	log.Infof("开始合并切片目录：%s", config.Instance().LocalTileFolder)

	rootDir := config.Instance().LocalTileFolder

	var children []Tile
	var globalSphere *Sphere
	var globalRegion *Region
	var globalBoxRegion *Region
	var mergeMode string
	var maxError float64

	// default merged root version
	rootAssetVersion := "1.0"

	var childCount int
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "tileset.json" {
			return nil
		}
		if filepath.Dir(path) == rootDir {
			return nil
		}
		childCount += 1
		return nil
	})

	if childCount > 0 {
		log.Infof("found %d children", childCount)
		fn := filepath.Join(rootDir, "tileset.json")
		if _, errS := os.Stat(fn); errS == nil {
			if errR := os.Remove(fn); errR != nil {
				panic(errR)
			}
		}
	}
	if err != nil {
		panic(err)
	}

	err = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "tileset.json" {
			return nil
		}
		if filepath.Dir(path) == rootDir {
			return nil
		}

		log.Info("Found:", path)

		bv, geoErr, childAssetVersion, transform, err := loadTileset(path)
		if err != nil {
			return err
		}
		geoErr = math.Min(geoErr, 200)

		// if any child is 1.1, merged root becomes 1.1 too
		if childAssetVersion == "1.1" {
			rootAssetVersion = "1.1"
		}

		rel, _ := filepath.Rel(rootDir, path)
		rel = filepath.ToSlash(rel)

		bvType := boundingVolumeType(bv)

		childBV := bv
		//var trans *[]float64
		var bakedChildBox *Box
		if bvType == "box" {
			bvBox := boxFromArray(*bv.Box)
			// Bake the child root transform into an ECEF AABB. Use this exact
			// baked box both for the child boundingVolume and for the root region.
			// This keeps parent/child culling volumes consistent in Cesium.
			newChildAABB := aabbFromBoxWithTransform(bvBox, transform)
			baked := boxFromAABB(newChildAABB)
			bakedChildBox = &baked
			box := boxToArray(baked)
			region := boxToRegion(box)
			childBV = BoundingVolume{Region: &region}
			//trans = transform
		}

		child := Tile{
			BoundingVolume: childBV,
			GeometricError: geoErr,
			Content: &Content{
				URI: rel,
			},
		}
		//if trans != nil {
		//	child.Transform = trans
		//}
		children = append(children, child)

		if mergeMode == "" {
			mergeMode = bvType
		} else if mergeMode != bvType {
			return fmt.Errorf("mixed boundingVolume types are not supported: found both %s and %s", mergeMode, bvType)
		}

		switch bvType {
		case "sphere":
			s := *bv.Sphere
			sphere := Sphere{s[0], s[1], s[2], s[3]}
			if globalSphere == nil {
				globalSphere = &sphere
			} else {
				merged := mergeSphere(*globalSphere, sphere)
				globalSphere = &merged
			}
		case "region":
			r := *bv.Region
			region := Region{
				West:      r[0],
				South:     r[1],
				East:      r[2],
				North:     r[3],
				MinHeight: r[4],
				MaxHeight: r[5],
			}
			if globalRegion == nil {
				globalRegion = &region
			} else {
				merged := mergeRegion(*globalRegion, region)
				globalRegion = &merged
			}
		case "box":
			if bakedChildBox == nil {
				return fmt.Errorf("internal error: missing baked child box for %s", path)
			}
			// Build the merged root as a region from the same baked ECEF AABB
			// that is written to the child. Do not use the original oriented box
			// here, otherwise the root region can be tighter/different than the
			// actual child boundingVolume and Cesium may cull all tiles.
			region := regionFromBoxWithTransform(*bakedChildBox, nil)
			if globalBoxRegion == nil {
				globalBoxRegion = &region
			} else {
				merged := mergeRegion(*globalBoxRegion, region)
				globalBoxRegion = &merged
			}
		default:
			return fmt.Errorf("child tileset %s has no supported boundingVolume", path)
		}

		if geoErr > maxError {
			maxError = geoErr
		}

		return nil
	})

	if err != nil {
		panic(err)
	}

	if len(children) == 0 {
		log.Info("No sub tilesets found.")
		return
	}

	rootBV := BoundingVolume{}
	switch mergeMode {
	case "sphere":
		s := []float64{
			globalSphere.X,
			globalSphere.Y,
			globalSphere.Z,
			globalSphere.R,
		}
		rootBV.Sphere = &s
	case "region":
		r := []float64{
			globalRegion.West,
			globalRegion.South,
			globalRegion.East,
			globalRegion.North,
			globalRegion.MinHeight,
			globalRegion.MaxHeight,
		}
		rootBV.Region = &r
	case "box":
		padded := padRegion(*globalBoxRegion)
		log.Infof("Root region degrees: west=%.9f south=%.9f east=%.9f north=%.9f minH=%.3f maxH=%.3f",
			padded.West*180.0/math.Pi, padded.South*180.0/math.Pi,
			padded.East*180.0/math.Pi, padded.North*180.0/math.Pi,
			padded.MinHeight, padded.MaxHeight)
		r := []float64{
			padded.West,
			padded.South,
			padded.East,
			padded.North,
			padded.MinHeight,
			padded.MaxHeight,
		}
		rootBV.Region = &r
	default:
		panic("no valid boundingVolume type found")
	}

	root := Tileset{
		Asset: Asset{
			Version: rootAssetVersion,
		},
		GeometricError: maxError,
		Root: Tile{
			BoundingVolume: rootBV,
			GeometricError: maxError * 10,
			Refine:         "ADD",
			Children:       children,
		},
	}

	outPath := filepath.Join(rootDir, "tileset.json")
	outData, _ := json.MarshalIndent(root, "", "  ")
	if errW := os.WriteFile(outPath, outData, 0644); errW != nil {
		log.Error(errW)
	}

	log.Infof("✅ 完成 Root tileset written to: %s (asset.version=%s)", outPath, rootAssetVersion)
}

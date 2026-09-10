package cesium

import (
	"cesium-tileset-tool/utils"
	"fmt"
	"math"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	wgs84A = 6378137.0           // semi-major axis
	wgs84F = 1.0 / 298.257223563 // flattening
)

var (
	wgs84B  = wgs84A * (1.0 - wgs84F)
	wgs84E2 = 1.0 - (wgs84B*wgs84B)/(wgs84A*wgs84A)
)

type Vec3 struct {
	X, Y, Z float64
}

func (a Vec3) Add(b Vec3) Vec3 {
	return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func (a Vec3) Sub(b Vec3) Vec3 {
	return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z}
}

func (a Vec3) Mul(s float64) Vec3 {
	return Vec3{a.X * s, a.Y * s, a.Z * s}
}

func Dot(a, b Vec3) float64 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

func Normalize(v Vec3) Vec3 {
	n := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if n == 0 {
		return Vec3{}
	}
	return Vec3{v.X / n, v.Y / n, v.Z / n}
}

// Convert lon/lat/height (radians, radians, meters) to ECEF Cartesian.
func cartesianFromRadians(lon, lat, h float64) Vec3 {
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)
	sinLon := math.Sin(lon)
	cosLon := math.Cos(lon)

	N := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)

	x := (N + h) * cosLat * cosLon
	y := (N + h) * cosLat * sinLon
	z := (N*(1.0-wgs84E2) + h) * sinLat

	return Vec3{x, y, z}
}

// Build ENU basis vectors at lon/lat.
// east, north, up
func enuAxes(lon, lat float64) (Vec3, Vec3, Vec3) {
	sinLon := math.Sin(lon)
	cosLon := math.Cos(lon)
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)

	east := Vec3{-sinLon, cosLon, 0}
	north := Vec3{-sinLat * cosLon, -sinLat * sinLon, cosLat}
	up := Vec3{cosLat * cosLon, cosLat * sinLon, sinLat}

	return Normalize(east), Normalize(north), Normalize(up)
}

func WorldBoxToLocalBox(box [12]float64, transform [16]float64) [12]float64 {
	// world box center
	cx, cy, cz := box[0], box[1], box[2]

	// world half-axis vectors
	hx := [3]float64{box[3], box[4], box[5]}
	hy := [3]float64{box[6], box[7], box[8]}
	hz := [3]float64{box[9], box[10], box[11]}

	// transform layout (Cesium / glTF style column-major)
	//
	// [ 0  4  8 12 ]
	// [ 1  5  9 13 ]
	// [ 2  6 10 14 ]
	// [ 3  7 11 15 ]
	//
	// upper-left 3x3
	m00, m01, m02 := transform[0], transform[4], transform[8]
	m10, m11, m12 := transform[1], transform[5], transform[9]
	m20, m21, m22 := transform[2], transform[6], transform[10]

	// translation
	tx, ty, tz := transform[12], transform[13], transform[14]

	// inverse of 3x3 matrix
	det := m00*(m11*m22-m12*m21) -
		m01*(m10*m22-m12*m20) +
		m02*(m10*m21-m11*m20)

	if math.Abs(det) < 1e-15 {
		return [12]float64{}
	}

	invDet := 1.0 / det

	r00 := (m11*m22 - m12*m21) * invDet
	r01 := (m02*m21 - m01*m22) * invDet
	r02 := (m01*m12 - m02*m11) * invDet

	r10 := (m12*m20 - m10*m22) * invDet
	r11 := (m00*m22 - m02*m20) * invDet
	r12 := (m02*m10 - m00*m12) * invDet

	r20 := (m10*m21 - m11*m20) * invDet
	r21 := (m01*m20 - m00*m21) * invDet
	r22 := (m00*m11 - m01*m10) * invDet

	// helper: multiply inverse-3x3 by vector
	mulInv3 := func(v [3]float64) [3]float64 {
		return [3]float64{
			r00*v[0] + r01*v[1] + r02*v[2],
			r10*v[0] + r11*v[1] + r12*v[2],
			r20*v[0] + r21*v[1] + r22*v[2],
		}
	}

	// local center = inv(M3) * (worldCenter - translation)
	localCenter := mulInv3([3]float64{cx - tx, cy - ty, cz - tz})

	// local half axes
	localHx := mulInv3(hx)
	localHy := mulInv3(hy)
	localHz := mulInv3(hz)

	// convert to simple local box by keeping only half lengths
	halfX := math.Sqrt(localHx[0]*localHx[0] + localHx[1]*localHx[1] + localHx[2]*localHx[2])
	halfY := math.Sqrt(localHy[0]*localHy[0] + localHy[1]*localHy[1] + localHy[2]*localHy[2])
	halfZ := math.Sqrt(localHz[0]*localHz[0] + localHz[1]*localHz[1] + localHz[2]*localHz[2])

	return [12]float64{
		localCenter[0], localCenter[1], localCenter[2],
		halfX, 0, 0,
		0, halfY, 0,
		0, 0, halfZ,
	}
}

// Convert region [west, south, east, north, minH, maxH] to 3D Tiles box[12].
func RegionToBox(region [6]float64, expand float64) [12]float64 {
	west := region[0]
	south := region[1]
	east := region[2]
	north := region[3]
	minH := region[4]
	maxH := region[5]

	centerLon := (west + east) * 0.5
	centerLat := (south + north) * 0.5
	centerH := (minH + maxH) * 0.5

	centerECEF := cartesianFromRadians(centerLon, centerLat, centerH)
	eastAxis, northAxis, upAxis := enuAxes(centerLon, centerLat)

	// 8 region corner points
	lons := []float64{west, east}
	lats := []float64{south, north}
	hs := []float64{minH, maxH}

	minE, maxE := math.Inf(1), math.Inf(-1)
	minN, maxN := math.Inf(1), math.Inf(-1)
	minU, maxU := math.Inf(1), math.Inf(-1)

	for _, lon := range lons {
		for _, lat := range lats {
			for _, h := range hs {
				p := cartesianFromRadians(lon, lat, h)
				d := p.Sub(centerECEF)

				e := Dot(d, eastAxis)
				n := Dot(d, northAxis)
				u := Dot(d, upAxis)

				if e < minE {
					minE = e
				}
				if e > maxE {
					maxE = e
				}
				if n < minN {
					minN = n
				}
				if n > maxN {
					maxN = n
				}
				if u < minU {
					minU = u
				}
				if u > maxU {
					maxU = u
				}
			}
		}
	}

	// Local center in ENU, then back to ECEF
	localCenterE := (minE + maxE) * 0.5
	localCenterN := (minN + maxN) * 0.5
	localCenterU := (minU + maxU) * 0.5

	boxCenter := centerECEF.
		Add(eastAxis.Mul(localCenterE)).
		Add(northAxis.Mul(localCenterN)).
		Add(upAxis.Mul(localCenterU))

	halfE := (maxE - minE) * 0.5 * expand
	halfN := (maxN - minN) * 0.5 * expand
	halfU := (maxU - minU) * 0.5 * expand

	hx := eastAxis.Mul(halfE)
	hy := northAxis.Mul(halfN)
	hz := upAxis.Mul(halfU)

	return [12]float64{
		boxCenter.X, boxCenter.Y, boxCenter.Z,
		hx.X, hx.Y, hx.Z,
		hy.X, hy.Y, hy.Z,
		hz.X, hz.Y, hz.Z,
	}
}

func degToRad(d float64) float64 {
	return d * math.Pi / 180.0
}

// GetBoundingVolumeBox returns a Cesium 3D Tiles boundingVolume.box
//
// Inputs:
//   - lngDeg: center longitude degree
//   - latDeg: center latitude degree
//   - heightMeters: center height in meters
//   - marginMeters: half-size of the box in east/north/up directions
//
// Output format:
//
//	[cx, cy, cz, hx1, hx2, hx3, hy1, hy2, hy3, hz1, hz2, hz3]
func GetBoundingVolumeBoxDeg(lngDeg, latDeg, heightMeters, marginMeters float64) [12]float64 {

	lon := degToRad(lngDeg)
	lat := degToRad(latDeg)

	center := cartesianFromRadians(lon, lat, heightMeters)
	east, north, up := enuAxes(lon, lat)

	hx := Vec3{east.X * marginMeters, east.Y * marginMeters, east.Z * marginMeters}
	hy := Vec3{north.X * marginMeters, north.Y * marginMeters, north.Z * marginMeters}
	hz := Vec3{up.X * marginMeters, up.Y * marginMeters, up.Z * marginMeters}

	return [12]float64{
		center.X, center.Y, center.Z,
		hx.X, hx.Y, hx.Z,
		hy.X, hy.Y, hy.Z,
		hz.X, hz.Y, hz.Z,
	}
}

func GetBoxFromBoundHeight(bound string, minHeight, maxHeight float64) [12]float64 {

	/*
		input bound format:
		"west,south,east,north"  (degrees)

		output Cesium box:
		[
		  cx, cy, cz,
		  hx1, hx2, hx3,
		  hy1, hy2, hy3,
		  hz1, hz2, hz3
		]
	*/

	if len(bound) == 0 {
		return [12]float64{}
	}

	values := strings.Split(strings.ReplaceAll(bound, " ", ""), ",")
	if len(values) != 4 {
		return [12]float64{}
	}

	west := utils.ToFloat64(values[0])
	south := utils.ToFloat64(values[1])
	east := utils.ToFloat64(values[2])
	north := utils.ToFloat64(values[3])

	// center
	lng := (west + east) / 2
	lat := (south + north) / 2

	// convert to radians
	lngRad := CesiumMathToRadians(lng)
	latRad := CesiumMathToRadians(lat)

	// approximate horizontal margin in meters
	// 1 degree lat ~= 111km
	dLat := (north - south) * 111000 / 2
	dLng := (east - west) * 111000 * math.Cos(latRad) / 2

	margin := math.Max(dLat, dLng)

	centerHeight := (minHeight + maxHeight) / 2

	// compute center ECEF
	center := cartesianFromRadians(lngRad, latRad, centerHeight)

	eastAxis, northAxis, upAxis := enuAxes(lngRad, latRad)

	hx := eastAxis.Mul(margin)
	hy := northAxis.Mul(margin)
	hz := upAxis.Mul((maxHeight - minHeight) / 2)

	return [12]float64{
		center.X, center.Y, center.Z,
		hx.X, hx.Y, hx.Z,
		hy.X, hy.Y, hy.Z,
		hz.X, hz.Y, hz.Z,
	}
}

func GetBoxFromBound(bound string) [12]float64 {
	return GetBoxFromBoundHeight(bound, -20, 300)
}

func GetRegionFromBound(bound string) [6]float64 {
	/*
		"region": [
		  west,  // 经度最小值（弧度）
		  south, // 纬度最小值（弧度）
		  east,  // 经度最大值（弧度）
		  north, // 纬度最大值（弧度）
		  minHeight, // 最低高度（米）
		  maxHeight  // 最高高度（米）
		]
	*/
	var region []float64
	if len(bound) > 0 {
		values := strings.Split(strings.ReplaceAll(bound, " ", ""), ",")
		if len(values) == 4 {
			region = append(region, CesiumMathToRadians(utils.ToFloat64(values[0])))
			region = append(region, CesiumMathToRadians(utils.ToFloat64(values[1])))
			region = append(region, CesiumMathToRadians(utils.ToFloat64(values[2])))
			region = append(region, CesiumMathToRadians(utils.ToFloat64(values[3])))
			region = append(region, -20)
			region = append(region, 200)
		}
	}
	slice, err := SliceToArray6(region)
	if err != nil {
		logrus.Error(err)
		return [6]float64{}
	}
	return slice
}

func SliceToArray6(s []float64) ([6]float64, error) {
	var arr [6]float64
	if len(s) < 6 {
		return arr, fmt.Errorf("slice 长度不足 6")
	}
	copy(arr[:], s[:6])
	return arr, nil
}

func Cross(a, b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

// 3D Tiles box:
// [cx, cy, cz, hx1, hx2, hx3, hy1, hy2, hy3, hz1, hz2, hz3]
type Box12 [12]float64

type OBB struct {
	Center Vec3
	HX     Vec3
	HY     Vec3
	HZ     Vec3
}

func Box12ToOBB(b Box12) OBB {
	return OBB{
		Center: Vec3{b[0], b[1], b[2]},
		HX:     Vec3{b[3], b[4], b[5]},
		HY:     Vec3{b[6], b[7], b[8]},
		HZ:     Vec3{b[9], b[10], b[11]},
	}
}

func OBBToBox12(o OBB) Box12 {
	return Box12{
		o.Center.X, o.Center.Y, o.Center.Z,
		o.HX.X, o.HX.Y, o.HX.Z,
		o.HY.X, o.HY.Y, o.HY.Z,
		o.HZ.X, o.HZ.Y, o.HZ.Z,
	}
}

func (o OBB) Corners() [8]Vec3 {
	axes := []Vec3{o.HX, o.HY, o.HZ}
	signs := [8][3]float64{
		{-1, -1, -1},
		{-1, -1, +1},
		{-1, +1, -1},
		{-1, +1, +1},
		{+1, -1, -1},
		{+1, -1, +1},
		{+1, +1, -1},
		{+1, +1, +1},
	}

	var corners [8]Vec3
	for i, s := range signs {
		p := o.Center
		p = p.Add(axes[0].Mul(s[0]))
		p = p.Add(axes[1].Mul(s[1]))
		p = p.Add(axes[2].Mul(s[2]))
		corners[i] = p
	}
	return corners
}

// MergeBoxes merges child 3D Tiles boxes into one root box.
// The merged root box uses the first child's orientation as the root frame.
// This is simple and stable, and usually good enough for Cesium tilesets.
func MergeBoxes(children []Box12, expand float64) Box12 {
	if len(children) == 0 {
		return Box12{}
	}
	if len(children) == 1 {
		return children[0]
	}

	first := Box12ToOBB(children[0])

	// Use first child orientation as root frame
	ux := Normalize(first.HX)
	uy := Normalize(first.HY)
	uz := Normalize(first.HZ)

	// Re-orthogonalize a bit in case input is slightly noisy
	uz = Normalize(Cross(ux, uy))
	uy = Normalize(Cross(uz, ux))

	origin := first.Center

	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	minZ, maxZ := math.Inf(1), math.Inf(-1)

	for _, child := range children {
		obb := Box12ToOBB(child)
		corners := obb.Corners()

		for _, p := range corners {
			d := p.Sub(origin)
			x := Dot(d, ux)
			y := Dot(d, uy)
			z := Dot(d, uz)

			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
			if z < minZ {
				minZ = z
			}
			if z > maxZ {
				maxZ = z
			}
		}
	}

	cx := (minX + maxX) * 0.5
	cy := (minY + maxY) * 0.5
	cz := (minZ + maxZ) * 0.5

	hxLen := (maxX - minX) * 0.5 * expand
	hyLen := (maxY - minY) * 0.5 * expand
	hzLen := (maxZ - minZ) * 0.5 * expand

	center := origin.
		Add(ux.Mul(cx)).
		Add(uy.Mul(cy)).
		Add(uz.Mul(cz))

	root := OBB{
		Center: center,
		HX:     ux.Mul(hxLen),
		HY:     uy.Mul(hyLen),
		HZ:     uz.Mul(hzLen),
	}

	return OBBToBox12(root)
}

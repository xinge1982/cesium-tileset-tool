package road

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"cesium-tileset-tool/map3d/mgltf"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

const (
	wgs84A  = 6378137.0
	wgs84F  = 1.0 / 298.257223563
	wgs84E2 = wgs84F * (2 - wgs84F)
)

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

// wgs84ToLocal returns a local tangent ENU coordinate at the origin.
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

func TestRoadWGS84SurfaceToGLB(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	geojsonPath, _ := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "./test/road_wgs84.geojson"))
	buf, err := os.ReadFile(geojsonPath)
	if err != nil {
		t.Fatalf("read geojson %s: %v", geojsonPath, err)
	}

	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal geojson: %v", err)
	}
	if len(fc.Features) == 0 {
		t.Fatalf("geojson has no features: %s", geojsonPath)
	}

	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)
	var count int

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
			count++
		}
	}
	if !isFinite(minLon) || !isFinite(minLat) || !isFinite(maxLon) || !isFinite(maxLat) {
		t.Fatalf("failed to compute bounds for %s", geojsonPath)
	}

	originLon := (minLon + maxLon) / 2
	originLat := (minLat + maxLat) / 2
	originH := 0.0

	t.Logf("box: %.8f, %.8f,%.8f, %.8f", minLon, minLat, maxLon, maxLat)

	t.Logf("origin lon/lat/h: %.8f, %.8f, %.3f", originLon, originLat, originH)

	doc := gltf.NewDocument()
	material, err := mgltf.TextureMaterialForColor(doc, "#808080")
	if err != nil {
		t.Fatalf("material: %v", err)
	}

	var primitives []*gltf.Primitive
	for i := range fc.Features {
		g := fc.Features[i].Geometry
		flat, stride, ok := outerRingFlatCoordsFromWGS84Geom(g)
		if !ok || stride < 2 {
			continue
		}

		pos := make([][3]float32, 0, len(flat)/stride)
		for j := 0; j+stride-1 < len(flat); j += stride {
			lon := flat[j]
			lat := flat[j+1]
			h := 0.0
			if stride >= 3 {
				h = flat[j+2]
			}
			e, n, u := wgs84ToLocal(lon, lat, h, originLon, originLat, originH)
			// map to this repo's convention: X=east, Y=up, Z=north
			pos = append(pos, [3]float32{float32(e), float32(u), float32(n)})
		}
		pos = normalizeClosedRingXZ(pos)
		if len(pos) < 3 {
			continue
		}

		face, err := mgltf.Primitive(doc, pos, material, mgltf.UvsParams{RepeatX: 1, RepeatY: 1, FlipV: true})
		if err != nil {
			t.Fatalf("primitive: %v", err)
		}
		primitives = append(primitives, face)
	}
	if len(primitives) == 0 {
		t.Fatalf("no primitives built from %s", geojsonPath)
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = "./test"
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}

	out := filepath.Join(outDir, "road-wgs84-surface.glb")
	if err := mgltf.SaveGlbForPrimitive(primitives, doc, out); err != nil {
		t.Fatalf("save glb: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat glb: %v", err)
	}
	if st.Size() == 0 {
		t.Fatalf("glb is empty: %s", out)
	}
	t.Logf("generated glb: %s (%d bytes)", out, st.Size())
}

func isFinite(v float64) bool {
	return !math.IsInf(v, 0) && !math.IsNaN(v)
}

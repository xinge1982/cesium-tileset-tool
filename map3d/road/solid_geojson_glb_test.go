package road

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"cesium-tileset-tool/map3d/mgltf"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func expandFlatCoordsToXYZ(flat []float64, stride int) []float64 {
	if stride == 3 {
		return flat
	}
	out := make([]float64, 0, (len(flat)/stride)*3)
	for i := 0; i+stride-1 < len(flat); i += stride {
		x := flat[i]
		y := flat[i+1]
		z := 0.0
		if stride >= 3 {
			z = flat[i+2]
		}
		out = append(out, x, y, z)
	}
	return out
}

func outerRingFlatCoordsFromGeom(g geom.T) ([]float64, int, bool) {
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

func TestRoadSolidFromGeoJSONToGLB(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}

	geojsonPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "lj32651.geojson"))
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

	// Use a single base point for the whole FeatureCollection so
	// relative positions between features remain correct.
	var bounds *geom.Bounds
	for i := range fc.Features {
		g := fc.Features[i].Geometry
		if g == nil {
			continue
		}
		if bounds == nil {
			bounds = g.Bounds().Clone()
		} else {
			bounds.Extend(g)
		}
	}
	if bounds == nil || bounds.IsEmpty() {
		t.Fatalf("failed to compute bounds for %s", geojsonPath)
	}
	basePoint := []float64{bounds.Min(0), bounds.Min(1), 0}
	t.Logf("basePoint: %.6f, %.6f", basePoint[0], basePoint[1])
	t.Logf("bounds XY: min(%.6f, %.6f) max(%.6f, %.6f)", bounds.Min(0), bounds.Min(1), bounds.Max(0), bounds.Max(1))

	doc := gltf.NewDocument()
	material, err := mgltf.TextureMaterialForColor(doc, "#808080")
	if err != nil {
		t.Fatalf("material: %v", err)
	}

	var primitives []*gltf.Primitive
	thickness := float32(0.2)
	for i := range fc.Features {
		g := fc.Features[i].Geometry
		flat, stride, ok := outerRingFlatCoordsFromGeom(g)
		if !ok {
			continue
		}

		flat = expandFlatCoordsToXYZ(flat, stride)
		if len(flat) < 6 {
			continue
		}

		pos := mgltf.ConvertToRelativeCoordinates(flat, basePoint)
		solids, err := mgltf.ExtrudePolygonSolidPrimitives(doc, pos, thickness, material, mgltf.UvsParams{RepeatX: 1, RepeatY: 1})
		if err != nil {
			t.Fatalf("extrude: %v", err)
		}
		primitives = append(primitives, solids...)
	}

	if len(primitives) == 0 {
		t.Fatalf("no primitives built from %s", geojsonPath)
	}

	outDir := os.Getenv("MAP3D_TEST_OUTPUT_DIR")
	if outDir == "" {
		outDir = `C:\MapABC\code\map-tool\map3d\road`
	} else {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir output dir: %v", err)
		}
	}
	out := filepath.Join(outDir, "road-solid.glb")
	if err := mgltf.SaveGlbForPrimitive(primitives, doc, out); err != nil {
		t.Fatalf("save glb: %v", err)
	}
	t.Logf("generated glb: %s", out)

	st, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat glb: %v", err)
	}
	if st.Size() == 0 {
		t.Fatalf("glb is empty: %s", out)
	}
}

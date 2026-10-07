package common

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

func TestTriangulateRoadSurfaceByCenterline_UsesStripTriangulation(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_face_wgs84.geojson"))
	roadFC := readFeatureCollectionForStripTest(t, roadPath)

	const roadID = int64(14241000111)
	var surfaceFeature SurfaceFeature
	found := false
	for _, f := range roadFC.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		surfaceFeature = SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("road_id=%d not found", roadID)
	}

	centerlineFeature, err := EstimateCenterlineFeatureFromSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("estimate centerline road_id=%d: %v", roadID, err)
	}

	basePoint, region := calcTileContextForGeom(surfaceFeature.Geom)
	pipeline, err := NewCoordinatePipeline(TileContext{
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}

	localRings, err := pipeline.ToLocalSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("localize surface: %v", err)
	}
	localLines, err := pipeline.ToLocalLine(LineFeature{FeatureInput: FeatureInput{Geom: centerlineFeature.Geom, Fields: centerlineFeature.Fields}})
	if err != nil {
		t.Fatalf("localize centerline: %v", err)
	}
	if len(localRings) == 0 || len(localLines) == 0 {
		t.Fatal("expected localized surface and centerline")
	}

	pos, indices, ok := triangulateRoadSurfaceByCenterline(localRings[0], localLines[0])
	if !ok {
		t.Fatal("expected centerline strip triangulation to succeed")
	}
	if len(pos) < 6 {
		t.Fatalf("expected enough strip vertices, got %d", len(pos))
	}
	if len(indices) < 6 || len(indices)%3 != 0 {
		t.Fatalf("expected triangle indices, got %d", len(indices))
	}
	for _, idx := range indices {
		if int(idx) >= len(pos) {
			t.Fatalf("index %d out of range for %d vertices", idx, len(pos))
		}
	}
}

func calcTileContextForGeom(g geom.T) ([]float64, [6]float64) {
	var region [6]float64
	flat := g.FlatCoords()
	stride := g.Stride()
	minLon, minLat := math.MaxFloat64, math.MaxFloat64
	maxLon, maxLat := -math.MaxFloat64, -math.MaxFloat64
	for i := 0; i+stride-1 < len(flat); i += stride {
		lon := flat[i]
		lat := flat[i+1]
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
	region = [6]float64{minLon, minLat, maxLon, maxLat, 0, 0}
	return []float64{(minLon + maxLon) / 2, (minLat + maxLat) / 2, 0}, region
}

func TestTriangulateRoadSurfaceRing_FallsBackWithoutCenterline(t *testing.T) {
	ring := LocalRings{
		Outer: [][3]float32{
			{0, 0, 0},
			{8, 0, 0},
			{8, 0, 4},
			{0, 0, 4},
			{0, 0, 0},
		},
	}

	pos, indices, err := triangulateRoadSurfaceRing(ring, LocalLine{}, false)
	if err != nil {
		t.Fatalf("expected fallback triangulation to succeed, got error: %v", err)
	}
	if len(pos) != 4 {
		t.Fatalf("expected 4 unique polygon vertices after fallback, got %d", len(pos))
	}
	if len(indices) != 6 {
		t.Fatalf("expected 2 triangles after fallback, got %d indices", len(indices))
	}
}

func TestTriangulateRoadSurfaceRing_UsesEstimatedCenterlineFallback(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_face_wgs84.geojson"))
	roadFC := readFeatureCollectionForStripTest(t, roadPath)

	const roadID = int64(14241000124)
	var surfaceFeature SurfaceFeature
	found := false
	for _, f := range roadFC.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		surfaceFeature = SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("road_id=%d not found", roadID)
	}

	basePoint, region := calcTileContextForGeom(surfaceFeature.Geom)
	pipeline, err := NewCoordinatePipeline(TileContext{
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}

	localRings, err := pipeline.ToLocalSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("localize surface: %v", err)
	}
	if len(localRings) == 0 {
		t.Fatal("expected localized surface ring")
	}

	pos, indices, err := triangulateRoadSurfaceRing(localRings[0], LocalLine{}, false)
	if err != nil {
		t.Fatalf("triangulate road surface ring: %v", err)
	}
	if len(indices) < 6 {
		t.Fatalf("expected estimated-centerline fallback to produce triangles, got %d indices", len(indices))
	}
	if !trianglesFitPolygonXZ(dedupeClosedRing(localRings[0].Outer), pos, indices) {
		t.Fatalf("estimated-centerline fallback does not fit polygon for road_id=%d", roadID)
	}
}

func TestBuildRoadSurfaceWithoutInputCenterline_UsesUnifiedFallback(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_face_wgs84.geojson"))
	roadFC := readFeatureCollectionForStripTest(t, roadPath)

	const roadID = int64(14241000124)
	var surfaceFeature SurfaceFeature
	found := false
	for _, f := range roadFC.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		surfaceFeature = SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
			Material: MaterialSet{
				Top: MaterialRef{Name: "test-road", Mode: MaterialModeColor, Color: "#FFFFFF", DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 8, RepeatY: 8, FlipV: false},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("road_id=%d not found", roadID)
	}

	basePoint, region := calcTileContextForGeom(surfaceFeature.Geom)
	pipeline, err := NewCoordinatePipeline(TileContext{
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}
	rings, err := pipeline.ToLocalSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("localize surface: %v", err)
	}
	doc := gltf.NewDocument()
	builder := NewSurfaceBuilder(doc, NewMaterialResolver(doc), nil, nil, "fast", 0.5)
	prims, err := builder.BuildRoadSurface(surfaceFeature, rings)
	if err != nil {
		t.Fatalf("build road surface: %v", err)
	}
	if len(prims) == 0 {
		t.Fatal("expected road surface primitive")
	}
}

func TestTriangulateRoadSurfaceEstimatedCandidate_RejectsBadFit(t *testing.T) {
	ring := LocalRings{
		Outer: [][3]float32{
			{0, 0, 0},
			{8, 0, 0},
			{8, 0, 2},
			{5, 0, 2},
			{5, 0, 6},
			{0, 0, 6},
			{0, 0, 0},
		},
	}
	cl := LocalLine{
		Points: [][3]float32{
			{0, 0, 1},
			{8, 0, 1},
		},
	}
	if _, _, ok := triangulateRoadSurfaceEstimatedCandidate(ring, cl); ok {
		t.Fatal("expected estimated candidate to reject bad fit")
	}
}

func TestTriangulateRoadSurfaceCDTCandidate_UsesRoad14241000124(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_face_wgs84.geojson"))
	roadFC := readFeatureCollectionForStripTest(t, roadPath)

	const roadID = int64(14241000124)
	var surfaceFeature SurfaceFeature
	found := false
	for _, f := range roadFC.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		surfaceFeature = SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("road_id=%d not found", roadID)
	}

	basePoint, region := calcTileContextForGeom(surfaceFeature.Geom)
	pipeline, err := NewCoordinatePipeline(TileContext{
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}
	localRings, err := pipeline.ToLocalSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("localize surface: %v", err)
	}
	if len(localRings) == 0 {
		t.Fatal("expected localized surface ring")
	}
	if _, _, ok := triangulateRoadSurfaceCDTCandidate(localRings[0]); !ok {
		t.Fatalf("expected cdt candidate to succeed for road_id=%d", roadID)
	}
}

func TestTriangulateRoadSurfaceByCenterline_RejectsOutOfPolygonResult(t *testing.T) {
	roadPath := filepath.Clean(filepath.Join("..", "road_face_wgs84.geojson"))
	roadFC := readFeatureCollectionForStripTest(t, roadPath)

	const roadID = int64(14241000008)
	var surfaceFeature SurfaceFeature
	found := false
	for _, f := range roadFC.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		surfaceFeature = SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      f.Geometry,
				Fields:    FeatureFields{"road_id": roadID},
			},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("road_id=%d not found", roadID)
	}

	centerlineFeature, err := EstimateCenterlineFeatureFromSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("estimate centerline road_id=%d: %v", roadID, err)
	}

	basePoint, region := calcTileContextForGeom(surfaceFeature.Geom)
	pipeline, err := NewCoordinatePipeline(TileContext{
		Region:    region,
		Center:    [3]float64{basePoint[0], basePoint[1], 0},
		BasePoint: basePoint,
		SRID:      4326,
		UseENU:    true,
	})
	if err != nil {
		t.Fatalf("new coordinate pipeline: %v", err)
	}

	localRings, err := pipeline.ToLocalSurface(surfaceFeature)
	if err != nil {
		t.Fatalf("localize surface: %v", err)
	}
	localLines, err := pipeline.ToLocalLine(LineFeature{FeatureInput: FeatureInput{Geom: centerlineFeature.Geom, Fields: centerlineFeature.Fields}})
	if err != nil {
		t.Fatalf("localize centerline: %v", err)
	}
	if len(localRings) == 0 || len(localLines) == 0 {
		t.Fatal("expected localized surface and centerline")
	}

	if _, _, ok := triangulateRoadSurfaceByCenterline(localRings[0], localLines[0]); ok {
		t.Fatalf("expected centerline triangulation to be rejected for road_id=%d", roadID)
	}
}

func readFeatureCollectionForStripTest(t *testing.T, path string) *geojson.FeatureCollection {
	t.Helper()
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fc := &geojson.FeatureCollection{}
	if err := json.Unmarshal(buf, fc); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return fc
}

func loadRoadRingForStripTest(t *testing.T, fc *geojson.FeatureCollection, roadID int64) LocalRings {
	t.Helper()
	for _, f := range fc.Features {
		if int64FromAny(f.Properties["road_id"]) != roadID || f.Geometry == nil {
			continue
		}
		switch g := f.Geometry.(type) {
		case *geom.MultiPolygon:
			poly := g.Polygon(0)
			return polygonToLocalRingsForStripTest(t, poly)
		case *geom.Polygon:
			return polygonToLocalRingsForStripTest(t, g)
		}
	}
	t.Fatalf("road_id=%d not found", roadID)
	return LocalRings{}
}

func polygonToLocalRingsForStripTest(t *testing.T, poly *geom.Polygon) LocalRings {
	t.Helper()
	flat := poly.FlatCoords()
	ends := poly.Ends()
	stride := poly.Stride()
	start := 0
	var out LocalRings
	for i, end := range ends {
		points := make([][3]float32, 0, (end-start)/stride)
		for j := start; j < end; j += stride {
			p := [3]float32{float32(flat[j]), 0, float32(flat[j+1])}
			if stride >= 3 {
				p[1] = float32(flat[j+2])
			}
			points = append(points, p)
		}
		if i == 0 {
			out.Outer = points
		} else {
			out.Holes = append(out.Holes, points)
		}
		start = end
	}
	return out
}

func localizeRingsForStripTest(rings LocalRings, origin [3]float32) LocalRings {
	out := LocalRings{
		Outer: localizePointsForStripTest(rings.Outer, origin),
		Holes: make([][][3]float32, 0, len(rings.Holes)),
	}
	for _, hole := range rings.Holes {
		out.Holes = append(out.Holes, localizePointsForStripTest(hole, origin))
	}
	return out
}

func loadCenterlineForStripTest(t *testing.T, fc *geojson.FeatureCollection, roadID int64) LocalLine {
	t.Helper()
	for _, f := range fc.Features {
		if int64FromAny(f.Properties["hroad_id"]) != roadID || f.Geometry == nil {
			continue
		}
		switch g := f.Geometry.(type) {
		case *geom.LineString:
			return lineToLocalForStripTest(g)
		case *geom.MultiLineString:
			return lineToLocalForStripTest(g.LineString(0))
		}
	}
	t.Fatalf("centerline hroad_id=%d not found", roadID)
	return LocalLine{}
}

func lineToLocalForStripTest(line *geom.LineString) LocalLine {
	flat := line.FlatCoords()
	stride := line.Stride()
	points := make([][3]float32, 0, len(flat)/stride)
	for i := 0; i < len(flat); i += stride {
		p := [3]float32{float32(flat[i]), 0, float32(flat[i+1])}
		if stride >= 3 {
			p[1] = float32(flat[i+2])
		}
		points = append(points, p)
	}
	return LocalLine{Points: points}
}

func localizeLineForStripTest(line LocalLine, origin [3]float32) LocalLine {
	return LocalLine{Points: localizePointsForStripTest(line.Points, origin)}
}

func localizePointsForStripTest(points [][3]float32, origin [3]float32) [][3]float32 {
	out := make([][3]float32, len(points))
	cosLat := math32CosDeg(origin[2])
	for i, p := range points {
		out[i] = [3]float32{
			(p[0] - origin[0]) * 111000 * cosLat,
			p[1] - origin[1],
			(p[2] - origin[2]) * 111000,
		}
	}
	return out
}

func math32CosDeg(lat float32) float32 {
	return float32(cosDeg(float64(lat)))
}

func cosDeg(v float64) float64 {
	return math.Cos(v * math.Pi / 180.0)
}

func int64FromAny(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		return 0
	}
}

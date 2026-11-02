package road

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"cesium-tileset-tool/map3d/road/common"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
)

// Supply original WGS84 geometries and business properties, not model paths.
// Barrier input must include the complete chain context before Bound filtering.
type RoadModelInput struct {
	Features    *geojson.FeatureCollection
	Centerlines *geojson.FeatureCollection
}

// Select a road generator independently of pre-existing device/building GLBs.
// Bound currently retains whole features; it does not cut at the project border.
type RoadModelOptions struct {
	BuildType          BuildType
	OutputDir          string
	Precision          int
	Bound              string
	TexturePath        string
	WaveSubtype        string
	WaveSubtypeField   string
	WaveSubtypeMapping map[string]string
	Posts              common.GuardrailPostOptions
	Chains             common.SidewallChainOptions
	BuildOptions       *BuildOptions
}

// Return a generated child tileset with a world-space box for parent attachment.
// Its internal ENU transform remains in the child JSON and must not be applied twice.
type RoadModelResult struct {
	TilesetPath     string
	Tiles           []RoadSurfaceGeohashTile
	WorldBox        [12]float64
	GeometricError  float64
	SourceInstances int
}

// Generate road geometry and cut it into GLBs during tileset production.
// New road facility types can be dispatched here without changing model loaders.
func BuildRoadModelSlices(ctx context.Context, input RoadModelInput, opt RoadModelOptions) (*RoadModelResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("road generation context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.Features == nil || len(input.Features.Features) == 0 {
		return nil, fmt.Errorf("road generation requires source features")
	}
	var path string
	var tiles []RoadSurfaceGeohashTile
	instances := 0
	switch opt.BuildType {
	case BuildTypeRoadSurface:
		if err := validateRoadModelGeometry(input.Features, true); err != nil {
			return nil, err
		}
		result, err := BuildRoadSurfaceGeohashSlices(input.Features, input.Centerlines, RoadSurfaceGeohashOptions{OutputDir: opt.OutputDir, Precision: opt.Precision, Bound: opt.Bound, TexturePath: opt.TexturePath, BuildOptions: opt.BuildOptions})
		if err != nil {
			return nil, err
		}
		path, tiles = result.TilesetPath, result.Tiles
	case BuildTypeBarrierWave:
		features, err := BarrierWaveFeaturesFromGeoJSON(input.Features, opt)
		if err != nil {
			return nil, err
		}
		options := BarrierWaveGeohashOptions{OutputDir: opt.OutputDir, Precision: opt.Precision, Posts: opt.Posts, Chains: opt.Chains}
		if opt.Bound != "" {
			bound, err := ParseRoadSurfaceBound(opt.Bound)
			if err != nil {
				return nil, err
			}
			options.Bound = &bound
		}
		result, err := BuildBarrierWaveGeohashSlices(features, options)
		if err != nil {
			return nil, err
		}
		path, tiles, instances = result.TilesetPath, result.Tiles, result.SourceInstances
	default:
		return nil, fmt.Errorf("unsupported generated road type %q", opt.BuildType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var generated tilesetJSON
	if err = json.Unmarshal(data, &generated); err != nil {
		return nil, err
	}
	box := generated.Root.BoundingVolume.Box
	m := generated.Root.Transform
	if len(box) != 12 || len(m) != 16 {
		return nil, fmt.Errorf("generated road tileset requires a box and a root transform")
	}
	var world [12]float64
	for part := 0; part < 4; part++ {
		for axis := 0; axis < 3; axis++ {
			world[part*3+axis] = m[axis]*box[part*3] + m[4+axis]*box[part*3+1] + m[8+axis]*box[part*3+2]
			if part == 0 {
				world[axis] += m[12+axis]
			}
			if math.IsNaN(world[part*3+axis]) || math.IsInf(world[part*3+axis], 0) {
				return nil, fmt.Errorf("invalid generated road bounds")
			}
		}
	}
	return &RoadModelResult{TilesetPath: path, Tiles: tiles, WorldBox: world, GeometricError: generated.Root.GeometricError, SourceInstances: instances}, nil
}

// Convert wave lines outside test code while preserving complete property rows.
// A subtype mapping deliberately skips unmatched source categories, e.g. other walls.
func BarrierWaveFeaturesFromGeoJSON(fc *geojson.FeatureCollection, opt RoadModelOptions) ([]LineFeature, error) {
	if err := validateRoadModelGeometry(fc, false); err != nil {
		return nil, err
	}
	features := []LineFeature{}
	for i, f := range fc.Features {
		fields := featureFieldsFromGeoJSON(f.Properties)
		kind := opt.WaveSubtype
		if opt.WaveSubtypeField != "" {
			raw := fmt.Sprint(fields[opt.WaveSubtypeField])
			if len(opt.WaveSubtypeMapping) > 0 {
				mapped, ok := opt.WaveSubtypeMapping[raw]
				if !ok {
					continue
				}
				kind = mapped
			} else {
				kind = raw
			}
		} else if kind == "" {
			if value, ok := fields["feature_type"]; ok {
				kind = fmt.Sprint(value)
			}
		}
		if kind == "" {
			kind = "wave_two"
		}
		if kind != "wave_two" && kind != "wave_three" && kind != "wave_nose_end" {
			return nil, fmt.Errorf("wave feature %d has unsupported subtype %q", i, kind)
		}
		fields["feature_type"] = kind
		material := MaterialRef{Color: "#B8BDC5", Mode: MaterialModeColor, DoubleSided: true}
		if opt.TexturePath != "" {
			material = MaterialRef{Path: opt.TexturePath, Mode: MaterialModeTexture, DoubleSided: true}
		}
		row := featureFieldsFromGeoJSON(fields)
		row["type"] = "barrier_" + kind
		features = append(features, LineFeature{FeatureInput: FeatureInput{BuildType: BuildTypeBarrierWave, Geom: f.Geometry, Fields: fields, Features: []FeatureFields{row}}, Material: MaterialSet{Top: material, Side: MaterialRef{Color: "#8D949C", Mode: MaterialModeColor, DoubleSided: true}}, UV: UVOptions{Mapping: UVMappingSweep, RepeatX: 1, RepeatY: 1}})
	}
	if len(features) == 0 {
		return nil, fmt.Errorf("no supported wave barrier features")
	}
	return features, nil
}

// Reject wrong geometry categories before any generated files are written.
func validateRoadModelGeometry(fc *geojson.FeatureCollection, surface bool) error {
	if fc == nil {
		return fmt.Errorf("nil road feature collection")
	}
	for i, f := range fc.Features {
		if f == nil || f.Geometry == nil {
			return fmt.Errorf("road feature %d has no geometry", i)
		}
		valid := false
		if surface {
			switch f.Geometry.(type) {
			case *geom.Polygon, *geom.MultiPolygon:
				valid = true
			}
		} else {
			switch f.Geometry.(type) {
			case *geom.LineString, *geom.MultiLineString:
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("road feature %d has unsupported geometry %T", i, f.Geometry)
		}
	}
	return nil
}

package traffic_feature

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cesium-tileset-tool/cesium"
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/map3d/road"
	roadcommon "cesium-tileset-tool/map3d/road/common"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"gorm.io/gorm"
)

const ModelSourceExisting = "existing"
const ModelSourceGeneratedRoad = "generated-road"

// Distinguish geometry generation from existing GLB acquisition explicitly.
// Table names alone must not reclassify existing facility/building model sources.
func ResolveModelSource(source config.TilesetSourceConfig) (string, error) {
	mode := strings.TrimSpace(source.ModelSource)
	if mode == "" {
		if source.Road != nil {
			mode = ModelSourceGeneratedRoad
		} else {
			mode = ModelSourceExisting
		}
	}
	switch mode {
	case ModelSourceExisting:
		if source.Road != nil {
			return "", fmt.Errorf("source %s combines existing models with road generation", source.ID)
		}
	case ModelSourceGeneratedRoad:
		if source.Road == nil {
			return "", fmt.Errorf("source %s requires road generation settings", source.ID)
		}
		switch source.Road.BuildType {
		case string(road.BuildTypeRoadSurface), string(road.BuildTypeBarrierWave):
		default:
			return "", fmt.Errorf("unsupported road buildType %q", source.Road.BuildType)
		}
		if source.Road.Precision != 0 && (source.Road.Precision < 4 || source.Road.Precision > 7) {
			return "", fmt.Errorf("road precision must be 4..7")
		}
	default:
		return "", fmt.Errorf("unsupported modelSource %q", mode)
	}
	return mode, nil
}

// Keep procedural geometry out of the old point-model partition and LOD path.
func splitRoadModelSources(table GeoTable) (GeoTable, []config.TilesetSourceConfig, error) {
	existing := table
	existing.GeoTableNames = nil
	generated := []config.TilesetSourceConfig{}
	for _, name := range table.GeoTableNames {
		source := table.TilesetSources[name]
		mode, err := ResolveModelSource(source)
		if err != nil {
			return existing, nil, err
		}
		if mode == ModelSourceGeneratedRoad {
			if source.Table.Name == "" {
				return existing, nil, fmt.Errorf("generated source %s has no table", source.ID)
			}
			generated = append(generated, source)
		} else {
			existing.GeoTableNames = append(existing.GeoTableNames, name)
		}
	}
	sort.Slice(generated, func(i, j int) bool { return generated[i].Table.Name < generated[j].Table.Name })
	return existing, generated, nil
}

// Report geometry built in this run rather than GLBs looked up on disk or MinIO.
type GeneratedRoadOutput struct {
	SourceID        string      `json:"sourceID"`
	Table           string      `json:"table"`
	BuildType       string      `json:"buildType"`
	TilesetURI      string      `json:"tilesetURI"`
	Tiles           int         `json:"tiles"`
	SourceInstances int         `json:"sourceInstances"`
	WorldBox        [12]float64 `json:"-"`
	GeometricError  float64     `json:"-"`
}

type roadFeatureLoader func(context.Context, config.TableConfig, *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error)

// Generate road child tilesets from database geometry during the main export.
// Source tables are read before bound filtering to retain full barrier-chain phase.
func GenerateRoadSourceModels(ctx context.Context, db *gorm.DB, table GeoTable, outputDirectory, bound string) ([]GeneratedRoadOutput, error) {
	if db == nil {
		return nil, fmt.Errorf("road generation database is nil")
	}
	return generateRoadSourceModels(ctx, table, outputDirectory, bound, func(ctx context.Context, t config.TableConfig, options *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error) {
		return readRoadFeatureCollection(ctx, db, t, options)
	})
}

// Keep the geometry loader injectable so the complete export path can be tested offline.
func generateRoadSourceModels(ctx context.Context, table GeoTable, outputDirectory, bound string, load roadFeatureLoader) ([]GeneratedRoadOutput, error) {
	_, sources, err := splitRoadModelSources(table)
	if err != nil {
		return nil, err
	}
	outputs := []GeneratedRoadOutput{}
	if len(sources) == 0 {
		return outputs, nil
	}
	if ctx == nil || load == nil {
		return nil, fmt.Errorf("road generation requires context and loader")
	}
	bounds, err := road.ParseRoadSurfaceBound(bound)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		options := source.Road
		collection, err := load(ctx, source.Table, options)
		if err != nil {
			return nil, fmt.Errorf("load generated source %s: %w", source.Table.Name, err)
		}
		if collection == nil || len(collection.Features) == 0 {
			continue
		}
		// Empty bounds are normal, but crossing geometries are not silently pre-clipped.
		// Full original collections are passed to the generator after this emptiness check.
		if len(road.FilterRoadSurfaceInputBound(collection, bounds).Features) == 0 {
			continue
		}
		var centerlines *geojson.FeatureCollection
		if options.CenterlineTable != nil {
			centerlines, err = load(ctx, *options.CenterlineTable, nil)
			if err != nil {
				return nil, err
			}
		}
		// A run-specific directory prevents stale files/cached GLBs from replacing new geometry.
		parent := filepath.Join(outputDirectory, "generated-road")
		if err = os.MkdirAll(parent, 0755); err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp(parent, "source-")
		if err != nil {
			return nil, err
		}
		p := options.Posts
		result, err := road.BuildRoadModelSlices(ctx, road.RoadModelInput{Features: collection, Centerlines: centerlines}, road.RoadModelOptions{
			BuildType: road.BuildType(options.BuildType), OutputDir: dir, Precision: options.Precision, Bound: bound, TexturePath: options.TexturePath,
			WaveSubtype: options.WaveSubtype, WaveSubtypeField: options.WaveSubtypeField, WaveSubtypeMapping: options.WaveSubtypeMapping,
			Posts: roadcommon.GuardrailPostOptions{Diameter: p.Diameter, TopAboveBoardCenter: p.TopAboveBoardCenter, Height: p.Height, Spacing: p.Spacing, LateralOffset: p.LateralOffset, RadialSegments: p.RadialSegments},
		})
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("generate %s geometry: %w", source.Table.Name, err)
		}
		relative, err := filepath.Rel(outputDirectory, result.TilesetPath)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, GeneratedRoadOutput{SourceID: source.ID, Table: source.Table.Name, BuildType: options.BuildType, TilesetURI: filepath.ToSlash(relative), Tiles: len(result.Tiles), SourceInstances: result.SourceInstances, WorldBox: result.WorldBox, GeometricError: result.GeometricError})
	}
	return outputs, nil
}

// Quote identifiers separately and bind category filters rather than accepting raw SQL.
func roadFeatureQuery(table config.TableConfig, options *config.GeneratedRoadConfig) (string, []any, error) {
	if table.Name == "" {
		return "", nil, fmt.Errorf("road table name is empty")
	}
	if strings.Contains(table.Name, ".") {
		parts := strings.Split(table.Name, ".")
		if len(parts) != 2 || table.Schema != "" {
			return "", nil, fmt.Errorf("use separate schema and table name")
		}
		table.Schema, table.Name = parts[0], parts[1]
	}
	name := quoteIdentifier(table.Name)
	if table.Schema != "" {
		name = quoteIdentifier(table.Schema) + "." + name
	}
	geom := table.GeometryField
	if geom == "" {
		geom = "geom"
	}
	pk := table.PrimaryKey
	if pk == "" {
		pk = "id"
	}
	query := fmt.Sprintf(`SELECT ST_AsGeoJSON(ST_Transform(t.%s,4326)) AS geometry, (to_jsonb(t)-?)::text AS properties FROM %s AS t WHERE t.%s IS NOT NULL`, quoteIdentifier(geom), name, quoteIdentifier(geom))
	args := []any{geom}
	if options != nil && options.FilterField != "" {
		if len(options.FilterValues) == 0 {
			return "", nil, fmt.Errorf("road filterField requires filterValues")
		}
		query += " AND t." + quoteIdentifier(options.FilterField) + "::text IN ?"
		args = append(args, options.FilterValues)
	} else if options != nil && len(options.FilterValues) > 0 {
		return "", nil, fmt.Errorf("road filterValues require filterField")
	}
	query += " ORDER BY t." + quoteIdentifier(pk)
	return query, args, nil
}

// Read one row at a time; preserve Z, all business properties and 64-bit IDs.
func readRoadFeatureCollection(ctx context.Context, db *gorm.DB, table config.TableConfig, options *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error) {
	query, args, err := roadFeatureQuery(table, options)
	if err != nil {
		return nil, err
	}
	rows, err := db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fc := &geojson.FeatureCollection{}
	pk := table.PrimaryKey
	if pk == "" {
		pk = "id"
	}
	for rows.Next() {
		var geometry, properties string
		if err = rows.Scan(&geometry, &properties); err != nil {
			return nil, err
		}
		var g geom.T
		err := geojson.Unmarshal([]byte(geometry), &g)
		if err != nil {
			return nil, err
		}
		var fields map[string]any
		decoder := json.NewDecoder(bytes.NewBufferString(properties))
		decoder.UseNumber()
		if err = decoder.Decode(&fields); err != nil {
			return nil, err
		}
		id, ok := fields[pk]
		if !ok || id == nil {
			return nil, fmt.Errorf("road source %s requires primary key %s", table.Name, pk)
		}
		fields["id"] = fmt.Sprint(id)
		fc.Features = append(fc.Features, &geojson.Feature{Geometry: g, Properties: fields})
	}
	return fc, rows.Err()
}

// Attach external road roots in world space while leaving their ENU transforms inside.
// Run after the static hierarchy is split; never pass these nodes to the old GLB merger.
func attachGeneratedRoadModels(tileset *Tileset, outputs []GeneratedRoadOutput) error {
	if len(outputs) == 0 {
		return nil
	}
	if tileset == nil || tileset.Root == nil {
		return fmt.Errorf("cannot attach generated roads to a nil root")
	}
	root := tileset.Root
	if root.Transform != nil {
		return fmt.Errorf("generated road attachment requires a world-space parent root")
	}
	boxes := []cesium.Box12{}
	if len(root.Children) > 0 || root.Content != nil {
		boxes = append(boxes, cesium.Box12(root.BoundingVolume.Box))
	}
	for _, output := range outputs {
		root.Children = append(root.Children, &TileNode{BoundingVolume: BoundingVolume{Box: output.WorldBox}, Content: &TileContent{Uri: output.TilesetURI}, Refine: "ADD", GeometricError: output.GeometricError, Geohash: "generated#" + output.Table})
		boxes = append(boxes, cesium.Box12(output.WorldBox))
		if root.GeometricError < output.GeometricError {
			root.GeometricError = output.GeometricError
		}
	}
	root.BoundingVolume.Box = [12]float64(cesium.MergeBoxes(boxes, 1.05))
	tileset.GeometricError = root.GeometricError
	return nil
}

// Touch every referenced generated resource indirectly by rebuilding on incremental updates.
// Post sampling is chain-wide, so changing one endpoint can move downstream stations.
func regenerateRoadAwareTileset(configName, partitionTable, folder string, table GeoTable) (bool, error) {
	_, sources, err := splitRoadModelSources(table)
	if err != nil {
		return false, err
	}
	if len(sources) == 0 {
		return false, nil
	}
	_, err = GenerateAllGeoHashTile(configName, partitionTable, folder, config.Instance().Bound)
	return true, err
}

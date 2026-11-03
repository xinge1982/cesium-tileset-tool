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

	"cesium-tileset-tool/config"
	"cesium-tileset-tool/map3d/road"
	roadcommon "cesium-tileset-tool/map3d/road/common"
	"cesium-tileset-tool/pg"
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
	default:
		return "", fmt.Errorf("unsupported modelSource %q", mode)
	}
	return mode, nil
}

// A road export owns its partition tree; device/building exports keep their old path.
func roadModelSources(table GeoTable) ([]config.TilesetSourceConfig, error) {
	sources := []config.TilesetSourceConfig{}
	existing := false
	for _, name := range table.GeoTableNames {
		source := table.TilesetSources[name]
		mode, err := ResolveModelSource(source)
		if err != nil {
			return nil, err
		}
		if mode == ModelSourceExisting {
			existing = true
			continue
		}
		if source.Table.Name == "" {
			return nil, fmt.Errorf("generated source %s has no table", source.ID)
		}
		sources = append(sources, source)
	}
	if existing && len(sources) > 0 {
		return nil, fmt.Errorf("road geometry and existing device/building models require separate tileset configurations")
	}
	return sources, nil
}

// Report source geometry generated directly into the database leaf hierarchy.
type GeneratedRoadOutput struct {
	SourceID        string `json:"sourceID"`
	Table           string `json:"table"`
	BuildType       string `json:"buildType"`
	Tiles           int    `json:"tiles"`
	SourceInstances int    `json:"sourceInstances"`
	GLBBytes        int64  `json:"glbBytes"`
}

type roadFeatureLoader func(context.Context, config.TableConfig, *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error)

// Build GLBs for the database leaves selected by the refinement pass.
// The caller owns the hierarchy and writes the single road tileset.json.
func GenerateRoadSourceModels(ctx context.Context, db *gorm.DB, table GeoTable, leaves []*GeoHashTile, outputDirectory, bound string) (map[string]*TileNode, []GeneratedRoadOutput, error) {
	if db == nil {
		return nil, nil, fmt.Errorf("road generation database is nil")
	}
	return generateRoadSourceModels(ctx, table, leaves, outputDirectory, bound, func(ctx context.Context, t config.TableConfig, options *config.GeneratedRoadConfig) (*geojson.FeatureCollection, error) {
		return readRoadFeatureCollection(ctx, db, t, options)
	})
}

// Preserve complete geometry and chain context; clip meshes only at final leaf edges.
func generateRoadSourceModels(ctx context.Context, table GeoTable, leaves []*GeoHashTile, outputDirectory, bound string, load roadFeatureLoader) (map[string]*TileNode, []GeneratedRoadOutput, error) {
	sources, err := roadModelSources(table)
	if err != nil {
		return nil, nil, err
	}
	levels, err := configuredRoadLODLevels(table.LOD)
	if err != nil {
		return nil, nil, err
	}
	nodes := map[string]*TileNode{}
	outputs := []GeneratedRoadOutput{}
	if len(sources) == 0 || len(leaves) == 0 {
		return nodes, outputs, nil
	}
	if ctx == nil || load == nil || outputDirectory == "" {
		return nil, nil, fmt.Errorf("road generation requires context, loader and output directory")
	}
	bounds, err := road.ParseRoadSurfaceBound(bound)
	if err != nil {
		return nil, nil, err
	}
	hashes := make([]string, 0, len(leaves))
	byHash := map[string]*GeoHashTile{}
	for _, leaf := range leaves {
		if leaf == nil || leaf.Level != len(leaf.Geohash) {
			return nil, nil, fmt.Errorf("invalid road database leaf")
		}
		if _, exists := byHash[leaf.Geohash]; exists {
			return nil, nil, fmt.Errorf("duplicate road database leaf %s", leaf.Geohash)
		}
		byHash[leaf.Geohash] = leaf
		hashes = append(hashes, leaf.Geohash)
	}
	parent := filepath.Join(outputDirectory, "road-models")
	if err = os.MkdirAll(parent, 0755); err != nil {
		return nil, nil, err
	}
	runDir, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		return nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(runDir)
		}
	}()
	for i, source := range sources {
		if err = ctx.Err(); err != nil {
			return nil, nil, err
		}
		options := source.Road
		collection, err := load(ctx, source.Table, options)
		if err != nil {
			return nil, nil, fmt.Errorf("load road source %s: %w", source.Table.Name, err)
		}
		if collection == nil || len(collection.Features) == 0 {
			continue
		}
		var centerlines *geojson.FeatureCollection
		if options.CenterlineTable != nil {
			centerlines, err = load(ctx, *options.CenterlineTable, nil)
			if err != nil {
				return nil, nil, err
			}
		}
		p := options.Posts
		result, err := road.BuildRoadModelSlices(ctx, road.RoadModelInput{Features: collection, Centerlines: centerlines}, road.RoadModelOptions{
			BuildType: road.BuildType(options.BuildType), OutputDir: filepath.Join(runDir, fmt.Sprintf("source-%d", i)),
			Geohashes: hashes, ModelsOnly: true, IntersectBound: &bounds, TexturePath: options.TexturePath,
			WaveSubtype: options.WaveSubtype, WaveSubtypeField: options.WaveSubtypeField, WaveSubtypeMapping: options.WaveSubtypeMapping,
			Posts: roadcommon.GuardrailPostOptions{Diameter: p.Diameter, TopAboveBoardCenter: p.TopAboveBoardCenter, Height: p.Height, Spacing: p.Spacing, LateralOffset: p.LateralOffset, RadialSegments: p.RadialSegments},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("generate %s geometry: %w", source.Table.Name, err)
		}
		var glbBytes int64
		for _, built := range result.Tiles {
			glbBytes += int64(built.GLBBytes)
			leaf, ok := byHash[built.Geohash]
			if !ok {
				return nil, nil, fmt.Errorf("generator returned non-database leaf %s", built.Geohash)
			}
			relative, err := filepath.Rel(outputDirectory, built.GLBPath)
			if err != nil {
				return nil, nil, err
			}
			node := nodes[built.Geohash]
			if node == nil {
				node = &TileNode{Geohash: leaf.Geohash, Level: leaf.Level, Refine: "ADD"}
				nodes[built.Geohash] = node
			}
			node.Children = append(node.Children, buildGeneratedRoadLODChain(filepath.ToSlash(relative), built.Box, built.Transform, levels, built.Geohash, i))
		}
		outputs = append(outputs, GeneratedRoadOutput{SourceID: source.ID, Table: source.Table.Name, BuildType: options.BuildType, Tiles: len(result.Tiles), SourceInstances: result.SourceInstances, GLBBytes: glbBytes})
	}
	success = true
	return nodes, outputs, nil
}

// Transform the center as a point and the half axes as vectors into ECEF.
func roadWorldBox(box [12]float64, m [16]float64) (world [12]float64) {
	for part := 0; part < 4; part++ {
		for axis := 0; axis < 3; axis++ {
			world[part*3+axis] = m[axis]*box[part*3] + m[4+axis]*box[part*3+1] + m[8+axis]*box[part*3+2]
			if part == 0 {
				world[axis] += m[12+axis]
			}
		}
	}
	return
}

// Quote schema, table and column names independently for all road database queries.
func roadSourceRelation(table config.TableConfig) (name, geometry, primaryKey string, err error) {
	if table.Name == "" {
		return "", "", "", fmt.Errorf("road table name is empty")
	}
	if strings.Contains(table.Name, ".") {
		parts := strings.Split(table.Name, ".")
		if len(parts) != 2 || table.Schema != "" {
			return "", "", "", fmt.Errorf("use separate schema and table name")
		}
		table.Schema, table.Name = parts[0], parts[1]
	}
	name = quoteIdentifier(table.Name)
	if table.Schema != "" {
		name = quoteIdentifier(table.Schema) + "." + name
	}
	geometry, primaryKey = table.GeometryField, table.PrimaryKey
	if geometry == "" {
		geometry = "geom"
	}
	if primaryKey == "" {
		primaryKey = "id"
	}
	return
}

// Use identical business category selection for refinement and mesh generation.
func roadSourceFilter(options *config.GeneratedRoadConfig) (string, []any, error) {
	predicate := ""
	args := []any{}
	if options == nil {
		return predicate, args, nil
	}
	if options.FilterField != "" {
		if len(options.FilterValues) == 0 {
			return "", nil, fmt.Errorf("road filterField requires filterValues")
		}
		predicate += " AND t." + quoteIdentifier(options.FilterField) + "::text IN ?"
		args = append(args, options.FilterValues)
	} else if len(options.FilterValues) > 0 {
		return "", nil, fmt.Errorf("road filterValues require filterField")
	}
	if len(options.WaveSubtypeMapping) > 0 {
		if options.WaveSubtypeField == "" {
			return "", nil, fmt.Errorf("waveSubtypeMapping requires waveSubtypeField")
		}
		keys := make([]string, 0, len(options.WaveSubtypeMapping))
		for key := range options.WaveSubtypeMapping {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		predicate += " AND t." + quoteIdentifier(options.WaveSubtypeField) + "::text IN ?"
		args = append(args, keys)
	}
	return predicate, args, nil
}

// Bind category values rather than allowing raw source SQL in configuration.
func roadFeatureQuery(table config.TableConfig, options *config.GeneratedRoadConfig) (string, []any, error) {
	name, geometry, pk, err := roadSourceRelation(table)
	if err != nil {
		return "", nil, err
	}
	filter, filterArgs, err := roadSourceFilter(options)
	if err != nil {
		return "", nil, err
	}
	query := fmt.Sprintf(`SELECT ST_AsGeoJSON(ST_Transform(t.%s,4326)) AS geometry, (to_jsonb(t)-?)::text AS properties FROM %s AS t WHERE t.%s IS NOT NULL`, quoteIdentifier(geometry), name, quoteIdentifier(geometry))
	query += filter + " ORDER BY t." + quoteIdentifier(pk)
	return query, append([]any{geometry}, filterArgs...), nil
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

// Re-refine before a complete road export because upstream edits can alter a chain.
func regenerateRoadAwareTileset(configName, partitionTable, folder string, table GeoTable) (bool, error) {
	sources, err := roadModelSources(table)
	if err != nil {
		return false, err
	}
	if len(sources) == 0 {
		return false, nil
	}
	dbc := pg.GetConn(configName)
	if dbc == nil {
		return true, fmt.Errorf("road generation database connection is empty")
	}
	bound := config.Instance().Bound
	if err = InitTileSetTables(dbc.DB, bound); err != nil {
		return true, err
	}
	if err = RefineUntilStable(dbc.DB, bound, folder); err != nil {
		return true, err
	}
	_, err = GenerateAllGeoHashTile(configName, partitionTable, folder, bound)
	return true, err
}

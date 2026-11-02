package traffic_feature

import (
	"fmt"
	"math"
	"strings"
	"time"

	"cesium-tileset-tool/config"
	"gorm.io/gorm"
)

// Count crossing surfaces and lines in every intersected partition, never by centroid.
func roadPartitionCountQuery(table GeoTable, source config.TilesetSourceConfig, parentHash string, level int16, bound projectBound) (string, []any, error) {
	name, geometry, _, err := roadSourceRelation(source.Table)
	if err != nil {
		return "", nil, err
	}
	filter, filterArgs, err := roadSourceFilter(source.Road)
	if err != nil {
		return "", nil, err
	}
	spatial := fmt.Sprintf("ST_Intersects(ST_Transform(t.%s,4326), p.bbox)", quoteIdentifier(geometry))
	args := []any{}
	if source.Road != nil && source.Road.BuildType == "barrier_wave" {
		// Count cells reached by the swept board/support offset as well as the input line.
		// Otherwise deep cells adjacent to a centerline could be deleted as empty.
		diameter, offset := float64(source.Road.Posts.Diameter), float64(source.Road.Posts.LateralOffset)
		if diameter == 0 {
			diameter = .114
		}
		if offset == 0 {
			offset = .230
		}
		padding := math.Max(.5, offset+diameter/2+.1)
		spatial = fmt.Sprintf("ST_DWithin(ST_Transform(t.%s,4326)::geography, p.bbox::geography, ?)", quoteIdentifier(geometry))
		args = append(args, padding)
	}
	query := fmt.Sprintf(`SELECT p.geohash, COUNT(*) AS count FROM %s AS p JOIN %s AS t
        ON %s
        WHERE p.level = ? AND ST_Intersects(p.bbox, ST_MakeEnvelope(?, ?, ?, ?,4326))`, quotePartitionRelation(table.PartitionTableName), name, spatial)
	query += fmt.Sprintf(" AND ST_Intersects(ST_Transform(t.%s,4326), ST_MakeEnvelope(?, ?, ?, ?,4326))", quoteIdentifier(geometry))
	args = append(args, level, bound.MinLng, bound.MinLat, bound.MaxLng, bound.MaxLat, bound.MinLng, bound.MinLat, bound.MaxLng, bound.MaxLat)
	if parentHash != "" {
		query += " AND p.parent_hash = ?"
		args = append(args, parentHash)
	}
	query += filter + " GROUP BY p.geohash"
	return query, append(args, filterArgs...), nil
}

// Handle a schema-qualified partition table without interpreting SQL fragments.
func quotePartitionRelation(name string) string {
	// Table identifiers are supplied by the same trusted configuration as the old path.
	parts := strings.Split(name, ".")
	for i := range parts {
		parts[i] = quoteIdentifier(parts[i])
	}
	return strings.Join(parts, ".")
}

// Preserve legacy point-model statistics and dispatch road-only tables separately.
func updateSourcePartitionCount(db *gorm.DB, table GeoTable, parentHash string, level int16, bound projectBound) error {
	sources, err := roadModelSources(table)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return UpdatePartitionCount(db, table.GeoTableNames, table.PartitionTableName, parentHash, level, table.Threshold, bound)
	}
	if table.Threshold <= 0 {
		return fmt.Errorf("road partition threshold must be positive")
	}
	counts := map[string]int{}
	for _, source := range sources {
		query, args, err := roadPartitionCountQuery(table, source, parentHash, level, bound)
		if err != nil {
			return err
		}
		var rows []GeoCount
		if err = db.Raw(query, args...).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			counts[row.Geohash] += row.Count
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var partitions []TileSetPartition
		query := tx.Table(table.PartitionTableName).Where("level = ? AND ST_Intersects(bbox, ST_MakeEnvelope(?, ?, ?, ?,4326))", append([]any{level}, bound.args()...)...)
		if parentHash != "" {
			query = query.Where("parent_hash = ?", parentHash)
		}
		if err = query.Find(&partitions).Error; err != nil {
			return err
		}
		for _, partition := range partitions {
			count := counts[partition.Geohash]
			refine := count > table.Threshold && level < MAX_GEOHASH_LEVEL
			if err = tx.Table(table.PartitionTableName).Where("id = ?", partition.ID).Updates(map[string]any{
				"total_count": count, "need_refine": refine, "refined": false, "update_time": time.Now(),
			}).Error; err != nil {
				return err
			}
			if !refine {
				// A previous run may have refined this cell; make it a real leaf again.
				// Also remove empty descendants after source deletions or filter changes.
				if err = tx.Table(table.PartitionTableName).Where("geohash LIKE ? AND level > ?", partition.Geohash+"%", level).Delete(&TileSetPartition{}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Keep nonempty road leaves even when they no longer need further subdivision.
func cleanupSourceChildren(db *gorm.DB, table GeoTable, parentHash string, level int16, bound projectBound) error {
	sources, err := roadModelSources(table)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return CleanupChildren(db, table.PartitionTableName, parentHash, level, bound)
	}
	return db.Table(table.PartitionTableName).Where("parent_hash = ? AND level = ? AND total_count = 0", parentHash, level).Delete(&TileSetPartition{}).Error
}

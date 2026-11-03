package traffic_feature

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"cesium-tileset-tool/config"
	"github.com/DATA-DOG/go-sqlmock"
)

const readerGeometry = `{"type":"LineString","coordinates":[[121,31,10],[121.001,31,11]]}`

// Read consecutive bounded pages with exact bigint keys and original XYZ geometries.
func TestReadRoadFeatureCollectionBatches(t *testing.T) {
	db, mock := roadMockDB(t)
	table := config.TableConfig{Schema: "public", Name: "walls", PrimaryKey: "fid", GeometryField: "shape"}
	options := roadFeatureReadOptions{Bound: [4]float64{120, 30, 122, 32}, BatchSize: 2}
	first, _, err := roadFeatureQuery(table, nil, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := roadFeatureQuery(table, nil, options, int64(9007199254740993))
	if err != nil {
		t.Fatal(err)
	}
	// Match PostgreSQL's actual numbered placeholders after GORM binding.
	sqlPattern := func(query string) string {
		for i := 1; strings.Contains(query, "?"); i++ {
			query = strings.Replace(query, "?", fmt.Sprintf("$%d", i), 1)
		}
		return regexp.QuoteMeta(query)
	}
	mock.ExpectQuery(sqlPattern(first)).WithArgs("shape", 120., 30., 122., 32., 2).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties", "cursor_key"}).AddRow(readerGeometry, `{"fid":0,"other":9007199254740993}`, int64(0)).AddRow(readerGeometry, `{"fid":9007199254740993,"other":9007199254740994}`, int64(9007199254740993)))
	mock.ExpectQuery(sqlPattern(next)).WithArgs("shape", 120., 30., 122., 32., int64(9007199254740993), 2).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties", "cursor_key"}).AddRow(readerGeometry, `{"fid":9007199254740994,"other":9007199254740995}`, int64(9007199254740994)))
	fc, err := readRoadFeatureCollection(context.Background(), db, table, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Features) != 3 {
		t.Fatal("missing batch features", len(fc.Features))
	}
	for i, id := range []string{"0", "9007199254740993", "9007199254740994"} {
		feature := fc.Features[i]
		if feature.Properties["id"] != id || fmt.Sprint(feature.Properties["fid"]) != id || feature.Geometry.Stride() != 3 || feature.Geometry.FlatCoords()[2] != 10 {
			t.Fatal("ID, attributes or elevation changed", feature)
		}
	}
	if fmt.Sprint(fc.Features[2].Properties["other"]) != "9007199254740995" {
		t.Fatal("business number lost precision")
	}
	if strings.Contains(first, "OFFSET") || strings.Contains(next, "OFFSET") || !strings.Contains(next, `t."fid" > ?`) {
		t.Fatal("query did not use native keyset pagination", next)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Stop on an empty page after an exact batch, retaining native string key ordering.
func TestReadRoadFeatureCollectionTextKeys(t *testing.T) {
	db, mock := roadMockDB(t)
	table := config.TableConfig{Name: "centers", PrimaryKey: "hroad_id"}
	options := roadFeatureReadOptions{Bound: [4]float64{120, 30, 122, 32}, BatchSize: 2}
	mock.ExpectQuery(`SELECT ST_AsGeoJSON`).WithArgs("geom", 120., 30., 122., 32., 2).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties", "cursor_key"}).AddRow(readerGeometry, `{"hroad_id":"10"}`, "10").AddRow(readerGeometry, `{"hroad_id":"2"}`, "2"))
	mock.ExpectQuery(`AND t."hroad_id" > \$6 ORDER BY t."hroad_id" ASC LIMIT \$7`).WithArgs("geom", 120., 30., 122., 32., "2", 2).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties", "cursor_key"}))
	fc, err := readRoadFeatureCollection(context.Background(), db, table, nil, options)
	if err != nil || len(fc.Features) != 2 || fc.Features[1].Properties["id"] != "2" {
		t.Fatal(fc, err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Return no partial collection on SQL failure, malformed rows or cancellation.
func TestReadRoadFeatureCollectionFailures(t *testing.T) {
	t.Run("sql-error-after-page", func(t *testing.T) {
		db, mock := roadMockDB(t)
		options := roadFeatureReadOptions{Bound: [4]float64{120, 30, 122, 32}, BatchSize: 1}
		mock.ExpectQuery(`SELECT ST_AsGeoJSON`).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties", "cursor_key"}).AddRow(readerGeometry, `{"id":1}`, int64(1)))
		failure := errors.New("database read failed")
		mock.ExpectQuery(`SELECT ST_AsGeoJSON`).WillReturnError(failure)
		fc, err := readRoadFeatureCollection(context.Background(), db, config.TableConfig{Name: "walls"}, nil, options)
		if fc != nil || !errors.Is(err, failure) {
			t.Fatal("returned partial data", fc, err)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		db, mock := roadMockDB(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		fc, err := readRoadFeatureCollection(ctx, db, config.TableConfig{Name: "walls"}, nil, roadFeatureReadOptions{Bound: [4]float64{120, 30, 122, 32}})
		if fc != nil || !errors.Is(err, context.Canceled) {
			t.Fatal(fc, err)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name, geometry, properties string
		key                        any
	}{{"missing-key", readerGeometry, `{"id":1}`, nil}, {"malformed-geometry", `not-json`, `{"id":1}`, int64(1)}, {"missing-property-id", readerGeometry, `{"name":"rail"}`, int64(1)}} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := roadMockDB(t)
			mock.ExpectQuery(`SELECT ST_AsGeoJSON`).WillReturnRows(sqlmock.NewRows([]string{"geometry", "properties", "cursor_key"}).AddRow(tc.geometry, tc.properties, tc.key))
			fc, err := readRoadFeatureCollection(context.Background(), db, config.TableConfig{Name: "walls"}, nil, roadFeatureReadOptions{Bound: [4]float64{120, 30, 122, 32}})
			if err == nil || fc != nil {
				t.Fatal(fc, err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Repeat the spatial and category predicates for every cursor page and apply defaults.
func TestRoadFeatureQueryBatchOptions(t *testing.T) {
	table := config.TableConfig{Name: "walls"}
	options := &config.GeneratedRoadConfig{FilterField: "category", FilterValues: []string{"5"}, WaveSubtypeField: "kind", WaveSubtypeMapping: map[string]string{"3": "wave_three"}}
	read := roadFeatureReadOptions{Bound: [4]float64{120, 30, 122, 32}}
	query, args, err := roadFeatureQuery(table, options, read, int64(0))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`ST_Intersects(ST_Transform(t."geom",4326), ST_MakeEnvelope(?, ?, ?, ?,4326))`, `t."category"::text IN ?`, `t."kind"::text IN ?`, `t."id" > ?`, `ORDER BY t."id" ASC LIMIT ?`} {
		if !strings.Contains(query, part) {
			t.Fatal(query)
		}
	}
	want := []any{"geom", 120., 30., 122., 32., []string{"5"}, []string{"3"}, int64(0), defaultRoadFeatureBatchSize}
	if !reflect.DeepEqual(args, want) {
		t.Fatal("incorrect bound/filter/cursor parameters", args, want)
	}
	for _, invalid := range []roadFeatureReadOptions{{}, {Bound: [4]float64{120, 30, 122, 32}, BatchSize: -1}, {Bound: [4]float64{122, 30, 120, 32}}} {
		if _, _, err = roadFeatureQuery(table, nil, invalid, nil); err == nil {
			t.Fatal("invalid query options accepted", invalid)
		}
	}
}

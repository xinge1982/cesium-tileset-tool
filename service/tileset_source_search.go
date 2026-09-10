package service

import (
	"cesium-tileset-tool/pg"
	"cesium-tileset-tool/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"cesium-tileset-tool/config"
	"gorm.io/gorm"
)

var sqlIdentifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`)

const (
	defaultPageSize = 50
	maxPageSize     = 500
)

var autoCreateColumnTypes = map[string]string{
	"":            "TEXT",
	"text":        "TEXT",
	"string":      "TEXT",
	"varchar":     "VARCHAR",
	"integer":     "INTEGER",
	"int":         "INTEGER",
	"bigint":      "BIGINT",
	"smallint":    "SMALLINT",
	"real":        "REAL",
	"float":       "DOUBLE PRECISION",
	"double":      "DOUBLE PRECISION",
	"numeric":     "NUMERIC",
	"decimal":     "NUMERIC",
	"boolean":     "BOOLEAN",
	"bool":        "BOOLEAN",
	"date":        "DATE",
	"timestamp":   "TIMESTAMP",
	"timestamptz": "TIMESTAMPTZ",
	"json":        "JSON",
	"jsonb":       "JSONB",
}

type TilesetSourceSearchRequest struct {
	TilesetKey string
	SourceID   string
	Keyword    string
	Page       int
	PageSize   int
}

type TilesetSourceDetailRequest struct {
	TilesetKey string
	SourceID   string
	Key        string
}

type TilesetSourceUpdateRequest struct {
	TilesetKey string
	SourceID   string
	Key        string
	Values     map[string]interface{}
	ClientIP   string
	UserAgent  string
}

type SearchResultField struct {
	Name     string         `json:"name"`
	Label    string         `json:"label"`
	Editable bool           `json:"editable"`
	Submit   bool           `json:"submit"`
	Editor   map[string]any `json:"editor,omitempty"`
}

type TilesetSourceSearchResult struct {
	TilesetKey     string                   `json:"tilesetKey"`
	SourceID       string                   `json:"sourceId"`
	PrimaryKey     string                   `json:"primaryKey"`
	FeatureIdField string                   `json:"featureIdField"`
	Fields         []SearchResultField      `json:"fields"`
	Items          []map[string]interface{} `json:"items"`
	Total          int64                    `json:"total"`
	Page           int                      `json:"page"`
	PageSize       int                      `json:"pageSize"`
}

type TilesetSourceDetailResult struct {
	TilesetKey   string              `json:"tilesetKey"`   //tileste名称
	SourceID     string              `json:"sourceId"`     //数据表名
	PrimaryKey   string              `json:"primaryKey"`   //主键字段名称
	FeatureValue string              `json:"featureValue"` //对应tileset中的对应字段
	Fields       []SearchResultField `json:"fields"`       //字段
	Item         interface{}         `json:"item"`         //数据
}

type TilesetSourceSearchService struct {
	conn     *pg.PGConn
	tilesets map[string]config.TilesetConfig
}

func NewTilesetSourceSearchService(
	conn *pg.PGConn,
	tilesets map[string]config.TilesetConfig,
) *TilesetSourceSearchService {
	return &TilesetSourceSearchService{
		conn:     conn,
		tilesets: tilesets,
	}
}

func (service *TilesetSourceSearchService) Search(
	ctx context.Context,
	request TilesetSourceSearchRequest,
) (*TilesetSourceSearchResult, error) {
	tileset, exists := service.tilesets[request.TilesetKey]
	if !exists {
		return nil, fmt.Errorf(
			"tileset %q does not exist",
			request.TilesetKey,
		)
	}

	if !tileset.Enabled {
		return nil, fmt.Errorf(
			"tileset %q is disabled",
			request.TilesetKey,
		)
	}

	source, err := findSource(tileset.Sources, request.SourceID)
	if err != nil {
		return nil, err
	}

	if err := validateSourceConfig(source); err != nil {
		return nil, fmt.Errorf(
			"invalid source configuration: %w",
			err,
		)
	}

	page, pageSize := normalizePagination(
		request.Page,
		request.PageSize,
	)

	selectFields := make([]config.FieldConfig, 0)
	searchFields := make([]config.FieldConfig, 0)

	for _, field := range source.Fields {
		if field.List {
			selectFields = append(selectFields, field)
		}

		if field.Search {
			searchFields = append(searchFields, field)
		}
	}

	if len(selectFields) == 0 {
		return nil, errors.New(
			"source has no fields marked as list=true",
		)
	}

	if strings.TrimSpace(request.Keyword) != "" &&
		len(searchFields) == 0 {
		return nil, errors.New(
			"source has no fields marked as search=true",
		)
	}

	if err := service.ensureAutoCreateColumns(
		ctx,
		source,
		selectFields,
		searchFields,
	); err != nil {
		return nil, err
	}

	tableSQL := qualifiedTableName(
		source.Table.Schema,
		source.Table.Name,
	)

	selectSQL, resultFields, err := buildSelectFields(selectFields)
	if err != nil {
		return nil, err
	}

	whereSQL, whereArgs, err := buildKeywordCondition(
		searchFields,
		request.Keyword,
	)
	if err != nil {
		return nil, err
	}

	countSQL := "SELECT COUNT(*) FROM " + tableSQL + whereSQL

	var total int64
	if err := service.conn.DB.
		WithContext(ctx).
		Raw(countSQL, whereArgs...).
		Scan(&total).
		Error; err != nil {
		return nil, fmt.Errorf(
			"count source records: %w",
			err,
		)
	}

	querySQL := fmt.Sprintf(
		`SELECT %s
		   FROM %s
		     %s
		  ORDER BY %s
		  LIMIT ?
		 OFFSET ?`,
		selectSQL,
		tableSQL,
		whereSQL,
		quoteIdentifier(source.Table.PrimaryKey),
	)

	queryArgs := append(
		append([]interface{}{}, whereArgs...),
		pageSize,
		(page-1)*pageSize,
	)

	rows, err := service.conn.DB.
		WithContext(ctx).
		Raw(querySQL, queryArgs...).
		Rows()
	if err != nil {
		return nil, fmt.Errorf(
			"query source records: %w",
			err,
		)
	}
	defer rows.Close()

	items, err := scanRows(rows)
	if err != nil {
		return nil, err
	}

	featureIdField := tileset.Feature.IDField
	if len(tileset.Feature.FeatureIdField) > 0 {
		featureIdField = tileset.Feature.FeatureIdField
	}
	if len(featureIdField) == 0 {
		featureIdField = "id"
	}

	return &TilesetSourceSearchResult{
		TilesetKey:     request.TilesetKey,
		SourceID:       request.SourceID,
		PrimaryKey:     source.Table.PrimaryKey,
		FeatureIdField: featureIdField,
		Fields:         resultFields,
		Items:          items,
		Total:          total,
		Page:           page,
		PageSize:       pageSize,
	}, nil
}

func (service *TilesetSourceSearchService) Detail(
	ctx context.Context,
	request TilesetSourceDetailRequest,
) (*TilesetSourceDetailResult, error) {
	tileset, exists := service.tilesets[request.TilesetKey]
	if !exists {
		return nil, fmt.Errorf(
			"tileset %q does not exist",
			request.TilesetKey,
		)
	}

	if !tileset.Enabled {
		return nil, fmt.Errorf(
			"tileset %q is disabled",
			request.TilesetKey,
		)
	}

	source, err := findSource(tileset.Sources, request.SourceID)
	if err != nil {
		return nil, err
	}

	if err := validateSourceConfig(source); err != nil {
		return nil, fmt.Errorf(
			"invalid source configuration: %w",
			err,
		)
	}

	selectFields := make([]config.FieldConfig, 0)
	searchFields := make([]config.FieldConfig, 0)
	for _, field := range source.Fields {
		if field.List {
			selectFields = append(selectFields, field)
		} else if field.Detail {
			selectFields = append(selectFields, field)
		}
		if field.Name == source.Table.PrimaryKey {
			searchFields = append(searchFields, field)
		}
	}

	if len(selectFields) == 0 {
		return nil, errors.New(
			"source has no fields marked as list=true",
		)
	}

	if len(searchFields) != 1 {
		return nil, fmt.Errorf(
			"primary key field %q is not configured",
			source.Table.PrimaryKey,
		)
	}

	tableSQL := qualifiedTableName(
		source.Table.Schema,
		source.Table.Name,
	)

	selectSQL, resultFields, err := buildSelectFields(selectFields)
	if err != nil {
		return nil, err
	}

	whereSQL, whereArgs, err := buildKeyCondition(
		searchFields,
		request.Key,
	)
	if err != nil {
		return nil, err
	}

	querySQL := fmt.Sprintf(
		`SELECT %s
		   FROM %s
		     %s LIMIT 1`,
		selectSQL,
		tableSQL,
		whereSQL,
	)

	queryArgs := append(
		append([]interface{}{}, whereArgs...),
	)

	rows, err := service.conn.DB.
		WithContext(ctx).
		Raw(querySQL, queryArgs...).
		Rows()
	if err != nil {
		return nil, fmt.Errorf(
			"query source records: %w",
			err,
		)
	}
	defer rows.Close()

	items, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("data not found")
	}

	fvalue := buildFeatureId(request.Key, items[0], source.KeyMapping)

	return &TilesetSourceDetailResult{
		TilesetKey:   request.TilesetKey,
		SourceID:     request.SourceID,
		PrimaryKey:   source.Table.PrimaryKey,
		FeatureValue: fvalue,
		Fields:       resultFields,
		Item:         items[0],
	}, nil
}

func (service *TilesetSourceSearchService) Update(
	ctx context.Context,
	request TilesetSourceUpdateRequest,
) error {
	tileset, exists := service.tilesets[request.TilesetKey]
	if !exists {
		return fmt.Errorf("tileset %q does not exist", request.TilesetKey)
	}
	if !tileset.Enabled {
		return fmt.Errorf("tileset %q is disabled", request.TilesetKey)
	}
	if !tileset.Maintainable {
		return fmt.Errorf("tileset %q is not maintainable", request.TilesetKey)
	}

	source, err := findSource(tileset.Sources, request.SourceID)
	if err != nil {
		return err
	}
	if err := validateSourceConfig(source); err != nil {
		return fmt.Errorf("invalid source configuration: %w", err)
	}

	key := strings.TrimSpace(request.Key)
	if key == "" {
		return errors.New("primary key is required")
	}

	fieldsByName := make(map[string]config.FieldConfig, len(source.Fields))
	for _, field := range source.Fields {
		if field.Submit && field.Name != source.Table.PrimaryKey {
			fieldsByName[field.Name] = field
		}
	}

	fieldNames := make([]string, 0, len(request.Values))
	for name := range request.Values {
		if _, allowed := fieldsByName[name]; !allowed {
			return fmt.Errorf("field %q is not allowed to submit", name)
		}
		fieldNames = append(fieldNames, name)
	}
	if len(fieldNames) == 0 {
		return errors.New("no fields to update")
	}
	sort.Strings(fieldNames)

	assignments := make([]string, 0, len(fieldNames))
	args := make([]interface{}, 0, len(fieldNames)*2+1)
	autoCreateColumns := make(map[string]string)

	for _, name := range fieldNames {
		field := fieldsByName[name]
		value := request.Values[name]
		storage := field.EffectiveStorage()

		if storage.AutoCreate {
			columnType, err := autoCreateColumnType(storage.ValueType)
			if err != nil {
				return fmt.Errorf("field %q: %w", name, err)
			}
			autoCreateColumns[storage.Column] = columnType
		}

		switch storage.Type {
		case "", "column", "text", "varchar", "string":
			assignments = append(
				assignments,
				quoteIdentifier(storage.Column)+" = ?",
			)
			args = append(args, value)

		case "json":
			path := "{" + strings.Join(storage.Path, ",") + "}"
			assignments = append(
				assignments,
				fmt.Sprintf(
					"%s = jsonb_set(COALESCE(%s, '{}'::jsonb), ?, to_jsonb(CAST(? AS text)), true)",
					quoteIdentifier(storage.Column),
					quoteIdentifier(storage.Column),
				),
			)
			args = append(args, path, value)

		default:
			return fmt.Errorf(
				"field %q has unsupported editable storage type %q",
				name,
				storage.Type,
			)
		}
	}

	tableSQL := qualifiedTableName(
		source.Table.Schema,
		source.Table.Name,
	)
	args = append(args, key)
	updateSQL := fmt.Sprintf(
		"UPDATE %s SET %s WHERE COALESCE(CAST(%s AS TEXT), '') = ?",
		tableSQL,
		strings.Join(assignments, ", "),
		quoteIdentifier(source.Table.PrimaryKey),
	)

	return service.conn.DB.WithContext(ctx).Transaction(
		func(tx *gorm.DB) error {
			columnNames := make([]string, 0, len(autoCreateColumns))
			for column := range autoCreateColumns {
				columnNames = append(columnNames, column)
			}
			sort.Strings(columnNames)

			for _, column := range columnNames {
				alterSQL := fmt.Sprintf(
					"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
					tableSQL,
					quoteIdentifier(column),
					autoCreateColumns[column],
				)
				if err := tx.Exec(alterSQL).Error; err != nil {
					return fmt.Errorf(
						"auto-create column %q: %w",
						column,
						err,
					)
				}
			}

			oldValues, err := readSourceUpdateValues(
				tx,
				source,
				fieldsByName,
				fieldNames,
				key,
				true,
			)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return errors.New("data not found")
				}
				return fmt.Errorf("read source record before update: %w", err)
			}

			result := tx.Exec(updateSQL, args...)
			if result.Error != nil {
				return fmt.Errorf("update source record: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return errors.New("data not found")
			}

			newValues, err := readSourceUpdateValues(
				tx,
				source,
				fieldsByName,
				fieldNames,
				key,
				false,
			)
			if err != nil {
				return fmt.Errorf("read source record after update: %w", err)
			}

			if err := insertSourceUpdateRecord(
				tx,
				request,
				source,
				oldValues,
				newValues,
			); err != nil {
				return err
			}

			return nil
		},
	)
}

func readSourceUpdateValues(
	tx *gorm.DB,
	source *config.TilesetSourceConfig,
	fieldsByName map[string]config.FieldConfig,
	fieldNames []string,
	key string,
	lockRow bool,
) ([]byte, error) {
	jsonPairs := make([]string, 0, len(fieldNames)*2)
	for _, name := range fieldNames {
		expression, err := buildStorageExpression(fieldsByName[name])
		if err != nil {
			return nil, err
		}
		jsonPairs = append(
			jsonPairs,
			"'"+name+"'",
			expression,
		)
	}

	querySQL := fmt.Sprintf(
		"SELECT jsonb_build_object(%s) FROM %s WHERE COALESCE(CAST(%s AS TEXT), '') = ? LIMIT 1",
		strings.Join(jsonPairs, ", "),
		qualifiedTableName(source.Table.Schema, source.Table.Name),
		quoteIdentifier(source.Table.PrimaryKey),
	)
	if lockRow {
		querySQL += " FOR UPDATE"
	}

	var values []byte
	if err := tx.Raw(querySQL, key).Row().Scan(&values); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return []byte("{}"), nil
	}
	return values, nil
}

func insertSourceUpdateRecord(
	tx *gorm.DB,
	request TilesetSourceUpdateRequest,
	source *config.TilesetSourceConfig,
	oldValues []byte,
	newValues []byte,
) error {
	const insertSQL = `
		INSERT INTO public.tileset_source_update_log (
			tileset_key,
			source_id,
			table_schema,
			table_name,
			record_key,
			old_values,
			new_values,
			client_ip,
			user_agent
		)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, NULLIF(?, '')::inet, ?)
	`

	if err := tx.Exec(
		insertSQL,
		request.TilesetKey,
		request.SourceID,
		source.Table.Schema,
		source.Table.Name,
		request.Key,
		string(oldValues),
		string(newValues),
		request.ClientIP,
		request.UserAgent,
	).Error; err != nil {
		return fmt.Errorf("insert source update record: %w", err)
	}

	return nil
}

func buildFeatureId(key string, row interface{}, mapping *config.KeyMappingConfig) string {
	if mapping == nil {
		return key
	}
	switch mapping.Operator {
	case "eq":
		if len(mapping.FeatureValueField) > 0 {
			if u, ok := row.(map[string]interface{}); ok {
				return utils.ToString(u[mapping.FeatureValueField])
			}
		}
		return key
	case "removePrefix":
		return fmt.Sprintf("%s%s", mapping.Value, key)
	default:
		return key
	}
}

func findSource(
	sources []config.TilesetSourceConfig,
	sourceID string,
) (*config.TilesetSourceConfig, error) {
	for index := range sources {
		if sources[index].ID == sourceID {
			return &sources[index], nil
		}
	}

	return nil, fmt.Errorf(
		"source %q does not exist",
		sourceID,
	)
}

func normalizePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}

	if pageSize < 1 {
		pageSize = defaultPageSize
	}

	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	return page, pageSize
}

func validateSourceConfig(
	source *config.TilesetSourceConfig,
) error {
	if source.ID == "" {
		return errors.New("source id is empty")
	}

	if err := validateIdentifier(
		source.Table.Schema,
		"schema",
	); err != nil {
		return err
	}

	if err := validateIdentifier(
		source.Table.Name,
		"table",
	); err != nil {
		return err
	}

	if err := validateIdentifier(
		source.Table.PrimaryKey,
		"primary key",
	); err != nil {
		return err
	}

	fieldNames := make(map[string]struct{})

	for _, field := range source.Fields {
		if err := validateIdentifier(
			field.Name,
			"field name",
		); err != nil {
			return err
		}

		if _, exists := fieldNames[field.Name]; exists {
			return fmt.Errorf(
				"duplicate field name %q",
				field.Name,
			)
		}
		fieldNames[field.Name] = struct{}{}

		storage := field.EffectiveStorage()

		switch storage.Type {
		case "", "column", "text", "varchar", "string":
			if err := validateIdentifier(
				storage.Column,
				"storage column",
			); err != nil {
				return err
			}
			if storage.AutoCreate {
				if _, err := autoCreateColumnType(
					storage.ValueType,
				); err != nil {
					return fmt.Errorf(
						"field %q: %w",
						field.Name,
						err,
					)
				}
			}

		case "geom":
			if err := validateIdentifier(
				storage.Column,
				"storage column",
			); err != nil {
				return err
			}
			if storage.AutoCreate {
				return fmt.Errorf(
					"field %q cannot auto-create geom storage",
					field.Name,
				)
			}

		case "json":
			if err := validateIdentifier(
				storage.Column,
				"JSON storage column",
			); err != nil {
				return err
			}

			if len(storage.Path) == 0 {
				return fmt.Errorf(
					"field %q has an empty JSON path",
					field.Name,
				)
			}

			for _, pathPart := range storage.Path {
				if err := validateIdentifier(
					pathPart,
					"JSON path",
				); err != nil {
					return err
				}
			}
			if storage.AutoCreate {
				return fmt.Errorf(
					"field %q cannot auto-create JSON path storage",
					field.Name,
				)
			}

		default:
			return fmt.Errorf(
				"field %q has unsupported storage type %q",
				field.Name,
				storage.Type,
			)
		}
	}

	return nil
}

func (service *TilesetSourceSearchService) ensureAutoCreateColumns(
	ctx context.Context,
	source *config.TilesetSourceConfig,
	fieldGroups ...[]config.FieldConfig,
) error {
	checkedColumns := make(map[string]struct{})

	for _, fields := range fieldGroups {
		for _, field := range fields {
			storage := field.EffectiveStorage()
			if !storage.AutoCreate {
				continue
			}
			if _, checked := checkedColumns[storage.Column]; checked {
				continue
			}
			checkedColumns[storage.Column] = struct{}{}

			var exists bool
			if err := service.conn.DB.
				WithContext(ctx).
				Raw(
					`SELECT EXISTS (
						SELECT 1
						FROM information_schema.columns
						WHERE table_schema = ?
						  AND table_name = ?
						  AND column_name = ?
					)`,
					source.Table.Schema,
					source.Table.Name,
					storage.Column,
				).
				Scan(&exists).
				Error; err != nil {
				return fmt.Errorf(
					"check auto-create column %q: %w",
					storage.Column,
					err,
				)
			}
			if exists {
				continue
			}

			columnType, err := autoCreateColumnType(storage.ValueType)
			if err != nil {
				return fmt.Errorf("field %q: %w", field.Name, err)
			}

			// IF NOT EXISTS also protects against concurrent search requests.
			alterSQL := fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
				qualifiedTableName(
					source.Table.Schema,
					source.Table.Name,
				),
				quoteIdentifier(storage.Column),
				columnType,
			)
			if err := service.conn.DB.
				WithContext(ctx).
				Exec(alterSQL).
				Error; err != nil {
				return fmt.Errorf(
					"auto-create column %q: %w",
					storage.Column,
					err,
				)
			}
		}
	}

	return nil
}

func autoCreateColumnType(valueType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(valueType))
	columnType, supported := autoCreateColumnTypes[normalized]
	if !supported {
		return "", fmt.Errorf(
			"unsupported auto-create valueType %q",
			valueType,
		)
	}
	return columnType, nil
}

func validateIdentifier(value, description string) error {
	if !sqlIdentifierPattern.MatchString(value) {
		return fmt.Errorf(
			"invalid %s %q",
			description,
			value,
		)
	}

	return nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func qualifiedTableName(schema, table string) string {
	return quoteIdentifier(schema) +
		"." +
		quoteIdentifier(table)
}

func buildSelectFields(
	fields []config.FieldConfig,
) (
	string,
	[]SearchResultField,
	error,
) {
	expressions := make([]string, 0, len(fields))
	resultFields := make(
		[]SearchResultField,
		0,
		len(fields),
	)

	for _, field := range fields {
		expression, err := buildStorageExpression(field)
		if err != nil {
			return "", nil, err
		}

		expressions = append(
			expressions,
			expression+" AS "+quoteIdentifier(field.Name),
		)

		resultFields = append(
			resultFields,
			SearchResultField{
				Name:     field.Name,
				Label:    field.Label,
				Editable: field.Editable,
				Submit:   field.Submit,
				Editor:   field.Editor,
			},
		)
	}

	return strings.Join(expressions, ", "), resultFields, nil
}

func buildKeyCondition(
	fields []config.FieldConfig,
	key string,
) (string, []interface{}, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil, errors.New("primary key is required")
	}

	conditions := make([]string, 0, len(fields))
	args := make([]interface{}, 0, len(fields))

	for _, field := range fields {
		expression, err := buildStorageExpression(field)
		if err != nil {
			return "", nil, errors.New("primary key is required")
		}

		conditions = append(
			conditions,
			"COALESCE(CAST("+expression+" AS TEXT), '') = ?",
		)

		args = append(args, key)
	}

	return " WHERE (" +
		strings.Join(conditions, " AND ") +
		")", args, nil
}

func buildKeywordCondition(
	fields []config.FieldConfig,
	keyword string,
) (string, []interface{}, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", nil, nil
	}

	conditions := make([]string, 0, len(fields))
	args := make([]interface{}, 0, len(fields))

	for _, field := range fields {
		expression, err := buildStorageExpression(field)
		if err != nil {
			return "", nil, err
		}

		conditions = append(
			conditions,
			"COALESCE(CAST("+expression+" AS TEXT), '') ILIKE ?",
		)

		args = append(args, "%"+keyword+"%")
	}

	return " WHERE (" +
		strings.Join(conditions, " OR ") +
		")", args, nil
}

func buildStorageExpression(
	field config.FieldConfig,
) (string, error) {
	storage := field.EffectiveStorage()

	switch storage.Type {
	case "", "column", "text", "varchar", "string":
		return quoteIdentifier(storage.Column), nil

	case "geom":
		if len(storage.ZColumn) > 0 {
			return fmt.Sprintf(`
			ST_AsText(
				   ST_Force3DZ(
						   ST_Expand(
								   ST_Envelope(
										   ST_Force2D(ST_Transform(%s, 4326))
								   ),
								   0.000001
						   ),
						   %s
				   )
		   ) `,
				quoteIdentifier(storage.Column),
				quoteIdentifier(storage.ZColumn),
			), nil
		} else {
			return fmt.Sprintf(`
			ST_AsText(
				   ST_Force3DZ(
						   ST_Expand(
								   ST_Envelope(
										   ST_Force2D(ST_Transform(%s, 4326))
								   ),
								   0.000001
						   ),
						   ST_ZMin(
								   Box3D(ST_Transform(%s, 4326))
						   )
				   )
		   ) `,
				quoteIdentifier(storage.Column),
				quoteIdentifier(storage.Column),
			), nil
		}

	case "json":
		path := make([]string, 0, len(storage.Path))
		for _, part := range storage.Path {
			if !sqlIdentifierPattern.MatchString(part) {
				return "", fmt.Errorf(
					"invalid JSON path component %q",
					part,
				)
			}

			path = append(path, part)
		}

		return fmt.Sprintf(
			"%s #>> '{%s}'",
			quoteIdentifier(storage.Column),
			strings.Join(path, ","),
		), nil

	default:
		return "", fmt.Errorf(
			"unsupported storage type %q",
			storage.Type,
		)
	}
}

func scanRows(
	rows *sql.Rows,
) ([]map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf(
			"read query columns: %w",
			err,
		)
	}

	items := make([]map[string]interface{}, 0)

	for rows.Next() {
		values := make([]interface{}, len(columns))
		destinations := make([]interface{}, len(columns))

		for index := range values {
			destinations[index] = &values[index]
		}

		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf(
				"scan query row: %w",
				err,
			)
		}

		item := make(map[string]interface{}, len(columns))

		for index, column := range columns {
			item[column] = normalizeDatabaseValue(
				values[index],
			)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate query rows: %w",
			err,
		)
	}

	return items, nil
}

func normalizeDatabaseValue(value interface{}) interface{} {
	switch typedValue := value.(type) {
	case []byte:
		return string(typedValue)

	default:
		return typedValue
	}
}

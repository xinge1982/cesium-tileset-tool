package pg

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

func GetConfigTableName(c *gin.Context) string {
	tableName := GormTableName(c)
	if len(tableName) > 0 {
		return tableName
	} else {
		return ""
	}
}

func MatchesStructure(columns []TableColumns, expected map[string]string) bool {
	var matched = make(map[string]bool)
	for cname, ctype := range expected {
		for _, column := range columns {
			var dataType = column.DataType
			var columnName = column.ColumnName
			if columnName == cname && strings.Contains(ctype, dataType) {
				matched[cname] = true
			}
		}
	}

	if len(expected) == len(matched) {
		return true
	}

	return false
}

func ValidateMapFields(input map[string]interface{}, structType interface{}) bool {
	// 获取结构体的类型
	t := reflect.TypeOf(structType)
	if t.Kind() != reflect.Struct {
		fmt.Println("Provided type is not a struct.")
		return false
	}

	// 递归解析结构体字段
	validFields := extractFields(structType)

	// 检查 map 中的键是否在结构体字段中存在
	for key := range input {
		if !validFields[key] {
			fmt.Printf("Invalid field: %s\n", key)
			return false
		}
	}

	return true
}

// 递归提取所有字段
func extractFields(structType interface{}) map[string]bool {
	fields := make(map[string]bool)
	t := reflect.TypeOf(structType)
	if t.Kind() == reflect.Ptr {
		t = t.Elem() // 获取指针所指向的元素
	}

	if t.Kind() != reflect.Struct {
		return fields
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 如果字段是嵌入结构体，递归提取其字段
		if field.Anonymous && field.Type.Kind() == reflect.Ptr {
			embeddedFields := extractFields(reflect.New(field.Type.Elem()).Elem().Interface())
			for k, v := range embeddedFields {
				fields[k] = v
			}
		} else if field.Anonymous && field.Type.Kind() == reflect.Struct {
			embeddedFields := extractFields(reflect.New(field.Type).Elem().Interface())
			for k, v := range embeddedFields {
				fields[k] = v
			}
		} else {
			jsonName := field.Tag.Get("json")
			if jsonName != "" {
				fields[jsonName] = true
			}
		}
	}
	return fields
}

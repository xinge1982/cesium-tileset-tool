package utils

import (
	"errors"
	"fmt"
	"github.com/xuri/excelize/v2"
	"reflect"
	"strconv"
	"time"
)

// ExportDataToExcel 导出任意结构体切片数据为 Excel 文件
func ExportDataToExcel[T any](data []T, filePath string) error {
	if len(data) == 0 {
		return nil // 没有数据直接返回
	}

	f := excelize.NewFile()
	sheet := "Sheet1"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	// 使用反射获取结构体类型和标签信息
	t := reflect.TypeOf(data[0])
	index := 0
	for i := 0; i < t.NumField(); i++ {
		col := intToExcelColumn(index) + "1"   // A1, B1, ...
		colName := t.Field(i).Tag.Get("excel") // 从 `excel` 标签获取中文名
		if colName == "" {
			continue
		}
		if err := f.SetCellValue(sheet, col, colName); err != nil {
			return err
		}
		index += 1
	}

	// 填充数据
	for i, record := range data {
		index = 0
		v := reflect.ValueOf(record)
		for j := 0; j < t.NumField(); j++ {
			colName := t.Field(j).Tag.Get("excel")
			if colName == "" {
				continue
			}
			col := intToExcelColumn(index) + strconv.Itoa(i+2) // A2, B2, ...
			if err := f.SetCellValue(sheet, col, v.Field(j).Interface()); err != nil {
				return err
			}
			index += 1
		}
	}

	return f.SaveAs(filePath)
}

// intToExcelColumn 将整数索引转换为 Excel 字母列名
func intToExcelColumn(index int) string {
	colName := ""
	for index >= 0 {
		colName = string(rune('A'+(index%26))) + colName
		index = index/26 - 1
	}
	return colName
}

// ImportDataFromExcel 从 Excel 文件导入任意结构体数据
func ImportDataFromExcel[T any](filePath string, sheetName string) ([]T, error) {
	var data []T
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return data, err
	}
	defer func(f *excelize.File) {
		_ = f.Close()
	}(f)

	if sheetName == "" {
		sheetName = "Sheet1"
	}

	rows, err := f.GetRows(sheetName)
	if err != nil || len(rows) < 2 { // 至少需要包含表头和数据
		return data, err
	}

	// 获取结构体的字段类型信息
	var sample T
	t := reflect.TypeOf(sample)
	fieldMap := map[int]string{} // 映射列索引到结构体字段名

	// 初始化字段映射，基于表头行
	headerRow := rows[0]
	for colIdx, colName := range headerRow {
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).Tag.Get("excel") == colName {
				fieldMap[colIdx] = t.Field(i).Name
				break
			}
		}
	}

	// 读取数据行
	for _, row := range rows[1:] {
		var record T
		v := reflect.ValueOf(&record).Elem()

		for colIdx, cellValue := range row {
			fieldName, ok := fieldMap[colIdx]
			if !ok {
				continue
			}

			field := v.FieldByName(fieldName)
			if !field.IsValid() || !field.CanSet() {
				continue
			}

			// 根据字段类型设置值
			switch field.Kind() {
			case reflect.Int64:
				intVal, _ := strconv.ParseInt(cellValue, 10, 64)
				field.SetInt(intVal)
			case reflect.Int32, reflect.Int:
				intVal, _ := strconv.ParseInt(cellValue, 10, 32)
				field.SetInt(intVal)
			case reflect.String:
				field.SetString(cellValue)
			case reflect.Struct: // 处理 time.Time
				if field.Type() == reflect.TypeOf(time.Time{}) {
					timeVal, _ := time.Parse("2006-01-02 15:04:05", cellValue)
					field.Set(reflect.ValueOf(timeVal))
				}
			default:
				return nil, errors.New(fmt.Sprintf("unknown type: %s", fieldName))
			}
		}

		data = append(data, record)
	}

	return data, nil
}

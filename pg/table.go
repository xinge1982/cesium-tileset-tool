package pg

import (
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ConfigDataTable struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Metadata datatypes.JSON `json:"metadata"`
}

type TableColumns struct {
	ColumnName string `gorm:"column:column_name"`
	DataType   string `gorm:"column:data_type"`
}

func RemoveHistoricalDatas[T any](s *PGConn, tableDatas map[string][]T) error {
	for tableName, datas := range tableDatas {
		var value T
		if err := s.DB.Table(tableName).AutoMigrate(&value); err != nil {
			return err
		}
		if result := s.DB.Table(tableName).
			Delete(datas); result.Error != nil {
			if result.Error != gorm.ErrRecordNotFound {
				return result.Error
			}
		}
	}

	return nil
}

func InsertHistoricalDatas[T any](s *PGConn, tableDatas map[string][]T) error {
	for tableName, datas := range tableDatas {
		tableExist := s.ContainsTable(tableName)
		if !tableExist {
			// 使用 AutoMigrate 创建动态表
			var value T
			if err := s.DB.Table(tableName).AutoMigrate(&value); err != nil {
				return err
			}
			s.fetchTables()
		}

		if result := s.DB.Session(&gorm.Session{CreateBatchSize: 100}).
			Table(tableName).
			Create(datas); result.Error != nil {
			if result.Error != gorm.ErrRecordNotFound {
				return result.Error
			}
		}
	}

	return nil
}

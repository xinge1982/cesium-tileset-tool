package pg

import (
	"cesium-tileset-tool/utils"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"time"
)

type JSONTime time.Time

const timeFormat = "2006-01-02 15:04:05"
const dbTimeFormat2 = "2006-01-02T15:04:05.999999Z07:00"

func (jt JSONTime) MarshalJSON() ([]byte, error) {
	formatted := time.Time(jt).Format(timeFormat)
	return []byte(`"` + formatted + `"`), nil
}

func (jt *JSONTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*jt = JSONTime(time.Time{})
		return nil
	}
	parsedTime, err := time.ParseInLocation(`"`+timeFormat+`"`, string(data), time.Local)
	if err != nil {
		parsedTime, err = time.Parse(`"`+dbTimeFormat2+`"`, string(data))
		if err != nil {
			if isNumeric(string(data)) {
				if len(data) == 10 {
					parsedTime = time.UnixMilli(utils.ToInt64(utils.ToString(string(data))) * 1000)
				} else if len(data) == 13 {
					parsedTime = time.UnixMilli(utils.ToInt64(utils.ToString(string(data))))
				}
			} else {
				return err
			}
		}
	}
	*jt = JSONTime(parsedTime)
	return nil
}

func (jt JSONTime) String() string {
	return time.Time(jt).Format(timeFormat)
}

// 实现 sql.Scanner 接口
func (jt *JSONTime) Scan(value interface{}) error {
	if dt, ok := value.(time.Time); ok {
		*jt = JSONTime(dt)
	} else if bytes, ok := value.([]byte); ok {
		parsedTime, err := time.ParseInLocation(`"`+timeFormat+`"`, string(bytes), time.Local)
		if err != nil {
			return err
		}
		*jt = JSONTime(parsedTime)
	} else {
		return errors.New(fmt.Sprintf("Failed to unmarshal JSONTime value:%v", value))
	}

	return nil
}

// 实现 driver.Valuer 接口，Value 返回 json value
func (jt JSONTime) Value() (driver.Value, error) {
	var zeroTime time.Time
	if time.Time(jt).UnixNano() == zeroTime.UnixNano() {
		return nil, nil
	}
	return time.Time(jt), nil
}

func isNumeric(s string) bool {
	match, _ := regexp.MatchString(`^\d+$`, s)
	return match
}

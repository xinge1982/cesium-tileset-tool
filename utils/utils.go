package utils

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/paulmach/orb"
)

func ToBoolean(field interface{}) bool {
	return ToBoolDefault(field, false)
}

func ToBoolDefault(field interface{}, defaultBool bool) bool {
	if field == nil {
		return defaultBool
	} else {
		if v, ok := field.(string); ok {
			return strings.EqualFold(v, "true")
		} else if v, ok := field.(*string); ok {
			return strings.EqualFold(*v, "true")
		} else if v, ok := field.(bool); ok {
			return v
		} else {
			if ToInt16(field) == 1 {
				return true
			}
			return false
		}
	}
}

func ToString(field interface{}) string {
	return ToStringDefault(field, "")
}

func ToStringDefault(v interface{}, defaultString string) string {
	switch val := v.(type) {

	// ===== 整型系列 =====
	case int:
		return strconv.FormatInt(int64(val), 10)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)

	// ===== 无符号整型 =====
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint8:
		return strconv.FormatUint(uint64(val), 10)
	case uint16:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)

	// ===== 浮点类型（防止科学计数法）=====
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)

	// ===== 字符串 =====
	case string:
		return val

	// ===== 字节数组（转为字符串）=====
	case []byte:
		return string(val)

	// ===== 布尔类型 =====
	case bool:
		return strconv.FormatBool(val)

	// ===== nil =====
	case nil:
		return defaultString

	// ===== 其他复杂类型（结构体、map等）=====
	default:
		// 尝试用 JSON 序列化（如 map、struct）
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		// 兜底：使用 fmt.Sprintf("%v")
		return fmt.Sprintf("%v", v)
	}
}

func ToFloat32(field interface{}) float32 {
	return ToFloat32Default(field, 0)
}

func ToFloat32Default(field interface{}, defaultValue float32) float32 {
	if field == nil {
		return defaultValue
	} else {
		if v, ok := field.(float64); ok {
			return float32(v)
		}
		if v, ok := field.(float32); ok {
			return v
		}
		if v, ok := field.(string); ok {
			va, err := strconv.ParseFloat(strings.Trim(v, " "), 8)
			if err == nil {
				return float32(va)
			}
		}
	}
	return defaultValue
}

func ToFloat64(field interface{}) float64 {
	return ToFloat64Default(field, 0)
}

func ToFloat64Default(field interface{}, defaultValue float64) float64 {
	if field == nil {
		return defaultValue
	} else {
		if v, ok := field.(float64); ok {
			return v
		}
		if v, ok := field.(float32); ok {
			return float64(v)
		}
		if v, ok := field.(int); ok {
			return float64(v)
		}
		if v, ok := field.(int32); ok {
			return float64(v)
		}
		if v, ok := field.(int64); ok {
			return float64(v)
		}
		if v, ok := field.(string); ok {
			va, err := strconv.ParseFloat(strings.Trim(v, " "), 8)
			if err == nil {
				return va
			}
		}
	}
	return defaultValue
}

func ToInt64(field interface{}) int64 {
	return ToInt64Default(field, 0)
}

func ToInt64Default(field interface{}, defaultValue int64) int64 {
	if field == nil {
		return defaultValue
	} else {
		if v, ok := field.(uint); ok {
			return int64(v)
		}
		if v, ok := field.(uint8); ok {
			return int64(v)
		}
		if v, ok := field.(int8); ok {
			return int64(v)
		}
		if v, ok := field.(uint16); ok {
			return int64(v)
		}
		if v, ok := field.(int16); ok {
			return int64(v)
		}
		if v, ok := field.(int32); ok {
			return int64(v)
		}
		if v, ok := field.(uint32); ok {
			return int64(v)
		}
		if v, ok := field.(int64); ok {
			return v
		}
		if v, ok := field.(uint64); ok {
			return int64(v)
		}
		if v, ok := field.(int); ok {
			return int64(v)
		}
		if v, ok := field.(float32); ok {
			return int64(v)
		}
		if v, ok := field.(float64); ok {
			return int64(v)
		}
		if v, ok := field.(bool); ok {
			if v == true {
				return int64(1)
			} else {
				return int64(0)
			}
		}
		if v, ok := field.(string); ok {
			va, err := strconv.ParseInt(strings.Trim(v, " "), 10, 64)
			if err == nil {
				return int64(va)
			}
		}
	}

	return defaultValue
}

func ToInt16(field interface{}) int16 {
	return ToInt16Default(field, 0)
}

func ToInt16Default(field interface{}, defaultValue int16) int16 {
	if field == nil {
		return defaultValue
	} else {
		if v, ok := field.(uint); ok {
			return int16(v)
		}
		if v, ok := field.(uint8); ok {
			return int16(v)
		}
		if v, ok := field.(int8); ok {
			return int16(v)
		}
		if v, ok := field.(uint16); ok {
			return int16(v)
		}
		if v, ok := field.(int16); ok {
			return v
		}
		if v, ok := field.(int32); ok {
			return int16(v)
		}
		if v, ok := field.(uint32); ok {
			return int16(v)
		}
		if v, ok := field.(int64); ok {
			return int16(v)
		}
		if v, ok := field.(uint64); ok {
			return int16(v)
		}
		if v, ok := field.(int); ok {
			return int16(v)
		}
		if v, ok := field.(float32); ok {
			return int16(v)
		}
		if v, ok := field.(float64); ok {
			return int16(v)
		}
		if v, ok := field.(bool); ok {
			if v == true {
				return int16(1)
			} else {
				return int16(0)
			}
		}
		if v, ok := field.(string); ok {
			va, err := strconv.ParseFloat(strings.Trim(v, " "), 64)
			if err == nil {
				return int16(va)
			}
		}
	}

	return defaultValue
}

func ToInt32(field interface{}) int32 {
	return ToInt32Default(field, 0)
}

func ToInt32Default(field interface{}, defaultValue int32) int32 {
	if field == nil {
		return defaultValue
	} else {
		if v, ok := field.(uint); ok {
			return int32(v)
		}
		if v, ok := field.(uint8); ok {
			return int32(v)
		}
		if v, ok := field.(int8); ok {
			return int32(v)
		}
		if v, ok := field.(uint16); ok {
			return int32(v)
		}
		if v, ok := field.(int16); ok {
			return int32(v)
		}
		if v, ok := field.(int32); ok {
			return v
		}
		if v, ok := field.(uint32); ok {
			return int32(v)
		}
		if v, ok := field.(int64); ok {
			return int32(v)
		}
		if v, ok := field.(uint64); ok {
			return int32(v)
		}
		if v, ok := field.(int); ok {
			return int32(v)
		}
		if v, ok := field.(float32); ok {
			return int32(v)
		}
		if v, ok := field.(float64); ok {
			return int32(v)
		}
		if v, ok := field.(bool); ok {
			if v == true {
				return int32(1)
			} else {
				return int32(0)
			}
		}
		if v, ok := field.(string); ok {
			va, err := strconv.ParseFloat(strings.Trim(v, " "), 64)
			if err == nil {
				return int32(va)
			}
		}
	}

	return defaultValue
}

// TimestampToTime
// Deprecated: instead of time.UnixMilli in go 1.17
func TimestampToTime(timestamp int64) time.Time {
	return time.UnixMilli(timestamp)
}

func AppendByte(slice []byte, data ...byte) []byte {
	m := len(slice)
	n := m + len(data)
	if n > cap(slice) { // if necessary, reallocate
		// allocate double what's needed, for future growth.
		newSlice := make([]byte, (n+1)*2)
		copy(newSlice, slice)
		slice = newSlice
	}
	slice = slice[0:n]
	copy(slice[m:n], data)
	return slice
}

// Filter returns a new slice holding only
// the elements of s that satisfy fn()
func Filter(s []int, fn func(int) bool) []int {
	var p []int // == nil
	for _, v := range s {
		if fn(v) {
			p = append(p, v)
		}
	}
	return p
}

var digitRegexp = regexp.MustCompile("[0-9]+")

func FindDigits(filename string) []byte {
	b, _ := ioutil.ReadFile(filename)
	return digitRegexp.Find(b)
}

func CopyDigits(filename string) []byte {
	b, _ := ioutil.ReadFile(filename)
	b = digitRegexp.Find(b)
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}

func round32(num float64) int {
	return int(float32(num) + float32(math.Copysign(0.5, num)))
}

func ToFixed32(num float64, precision int) float32 {
	output := math.Pow(10, float64(precision))
	return float32(round32(num*output)) / float32(output)
}

// 矩行是否相交：
func regionIntersection(p1 orb.Point, p2 orb.Point, q1 orb.Point, q2 orb.Point) int {
	if math.Min(p1.X(), p2.X()) <= math.Max(q1.X(), q2.X()) && math.Min(q1.X(), q2.X()) <= math.Max(p1.X(), p2.X()) &&
		math.Min(p1.Y(), p2.Y()) <= math.Max(q1.Y(), q2.Y()) && math.Min(q1.Y(), q2.Y()) <= math.Max(p1.Y(), p2.Y()) {
		return 1
	}
	return 0
}

func chengji(x1, y1, x2, y2 float64) float64 {
	return x1*y2 - y1*x2
}

// 线段是否相交：
func LineIntersection(p1 orb.Point, p2 orb.Point, q1 orb.Point, q2 orb.Point) int {
	if chengji(p2.X()-p1.X(), p2.Y()-p1.Y(), q1.X()-p1.X(), q1.Y()-p1.Y())*chengji(p2.X()-p1.X(), p2.Y()-p1.Y(), q2.X()-p1.X(), q2.Y()-p1.Y()) <= 0 &&
		chengji(q2.X()-q1.X(), q2.Y()-q1.Y(), p1.X()-q1.X(), p1.Y()-q1.Y())*chengji(q2.X()-q1.X(), q2.Y()-q1.Y(), p2.X()-q1.X(), p2.Y()-q1.Y()) <= 0 {
		return 1
	}
	return 0
}

// 跨立判断
func IsLineSegmentCross(P1, P2, Q1, Q2 orb.Point) bool {
	if ((Q1.X()-P1.X())*(Q1.Y()-Q2.Y())-(Q1.Y()-P1.Y())*(Q1.X()-Q2.X()))*((Q1.X()-P2.X())*(Q1.Y()-Q2.Y())-(Q1.Y()-P2.Y())*(Q1.X()-Q2.X())) < 0 ||
		((P1.X()-Q1.X())*(P1.Y()-P2.Y())-(P1.Y()-Q1.Y())*(P1.X()-P2.X()))*((P1.X()-Q2.X())*(P1.Y()-P2.Y())-(P1.Y()-Q2.Y())*(P1.X()-P2.X())) < 0 {
		return true
	} else {
		return false
	}
}

func GetLocalIP(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		log.Println("Error in parse url:" + host)
		return ""
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", u.Hostname(), u.Port()))
	if err != nil {
		fmt.Println(err)
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.TCPAddr)
	return localAddr.IP.String()
}

func TimeTrack(start time.Time, name string) (now time.Time) {
	elapsed := time.Since(start)
	log.Printf("%s took %dns", name, elapsed.Nanoseconds())
	return time.Now()
}

func GetNetworkUInt16(v uint16) []byte {
	var b = make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func GetNetworkUInt32(v uint32) []byte {
	var b = make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func GetNetworkUInt64(v uint64) []byte {
	var b = make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func GetNetworkFloatBytes(v float32) []byte {
	if v < 0 {
		v = 0
	}
	var b = make([]byte, 2)
	n, f := math.Modf(float64(v))
	f, _ = math.Modf(f * 100)
	if n > 254 {
		n = 254
	}
	if f > 254 {
		f = 254
	}
	b[0] = byte(f)
	b[1] = byte(n)
	return b
}

func GetNetworkFloatFromBytes(buffer []byte) float32 {
	return float32(float32(buffer[0])/100) + float32(buffer[1]|0x0000)
}

func ContainsChinese(s string) bool {
	// 定义正则表达式：匹配中文字符的范围
	re := regexp.MustCompile(`[\p{Han}]`)
	return re.MatchString(s)
}

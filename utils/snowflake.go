package utils

import (
	"github.com/sony/sonyflake"
	"time"
)

var sf *sonyflake.Sonyflake

// InitSonyflake 初始化 Sonyflake
func InitSonwflake() {
	var st sonyflake.Settings
	st.StartTime = time.Now().AddDate(-1, 0, 0) // 设置起始时间
	sf = sonyflake.NewSonyflake(st)
	if sf == nil {
		panic("sonyflake not created")
	}
}

// GenerateID 生成唯一的雪花 ID
func GenerateID() (int64, error) {
	if sf == nil {
		InitSonwflake()
	}
	id, err := sf.NextID()
	if err != nil {
		return 0, err
	}
	return int64(id), nil
}

package utils

import (
	"context"
	"time"
)

// TickerDo
// do 返回true的时停止循环
func TickerDo(ctx context.Context, d time.Duration, doAndExit func() bool) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if doAndExit() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// cycleDo
// d 默认的循环时间
// ch 外部通知循环周期改变channel
// do 循环周期需要做的事情
func cycleDo(d time.Duration, do func()) chan<- time.Duration {
	return CycleDoWithCtx(context.Background(), d, do)
}

func CycleDoWithCtx(ctx context.Context, d time.Duration, do func()) chan<- time.Duration {
	ticker := time.NewTicker(d)
	ch := make(chan time.Duration)

	// 监听外部请求
	// 修改定时器时间间隔
	go func() {
		for {
			select {
			case duration, ok := <-ch:
				if !ok {
					return
				}
				ticker.Reset(duration)
			case <-ctx.Done():
				return
			}
		}
	}()

	// 主事件循环
	go func() {
		defer ticker.Stop()
		defer close(ch)
		for {
			select {
			case <-ticker.C:
				do()
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

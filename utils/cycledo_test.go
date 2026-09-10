package utils

import (
	"context"
	"testing"
	"time"
)

func Test_cycleStatistics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	result := 0

	f := func() {
		result++
	}
	ch := CycleDoWithCtx(ctx, 3*time.Second, f)
	go func() {
		time.Sleep(10 * time.Second)
		ch <- 1 * time.Second
		time.Sleep(5500 * time.Millisecond)
		cancel()
	}()

	<-ctx.Done()

	if result != 3+5 {
		t.Error("result != 8", result)
	}
}

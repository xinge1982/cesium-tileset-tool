package draco

import (
	"context"
	"testing"
)

func TestCompressWithPipelineAndRestoreMeta(t *testing.T) {
	ctx := context.Background()
	err := CompressWithPipelineAndRestoreMeta(
		ctx,
		`C:\MapABC\web\shanghai\wmqvyw.glb`,
		`C:\MapABC\web\shanghai\wmqvyw-d.glb`,
		&PipelineParams{
			// Draco 参数（与你现在用的一致）
			CompressionLevel:     10,
			QuantizePositionBits: 14,
			QuantizeNormalBits:   10,
			QuantizeTexcoordBits: 12,
			QuantizeGenericBits:  12,
			UnifiedQuantization:  true,
		},
	)
	if err != nil {
		panic(err)
	}
}

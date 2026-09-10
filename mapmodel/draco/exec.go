package draco

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/qmuntal/gltf"
)

type PipelineParams struct {
	// 用 npx 直接拉取并调用 gltf-pipeline
	NpxPath         string // 默认 "npx"
	PipelineVersion string // gltf-pipeline 版本，默认 "latest"；也可固定 "4.2.9" 等

	// Draco 选项
	CompressionLevel     int
	QuantizePositionBits int
	QuantizeNormalBits   int
	QuantizeTexcoordBits int
	QuantizeGenericBits  int
	UnifiedQuantization  bool

	Timeout time.Duration
}

func (p *PipelineParams) def() {
	if p.NpxPath == "" {
		p.NpxPath = "npx"
	}
	if p.PipelineVersion == "" {
		p.PipelineVersion = "latest" // 可固定具体版本提高可复现性
	}
	if p.Timeout == 0 {
		p.Timeout = 5 * time.Minute
	}
	if p.CompressionLevel == 0 {
		p.CompressionLevel = 10
	}
	if p.QuantizePositionBits == 0 {
		p.QuantizePositionBits = 14
	}
	if p.QuantizeNormalBits == 0 {
		p.QuantizeNormalBits = 10
	}
	if p.QuantizeTexcoordBits == 0 {
		p.QuantizeTexcoordBits = 12
	}
	if p.QuantizeGenericBits == 0 {
		p.QuantizeGenericBits = 12
	}
}

// CompressWithPipelineAndRestoreMeta：读取原始 GLB，提取 EXT_structural_metadata；
// 调 gltf-pipeline -d 压缩；最后把元数据注回压缩产物。
func CompressWithPipelineAndRestoreMeta(ctx context.Context, inPath, outPath string, pp *PipelineParams) error {
	pp.def()

	// 1) 打开原始 GLB 并提取元数据
	orig, err := gltf.Open(inPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	baseDir := filepath.Dir(inPath)
	saved, err := ExtractStructuralMetadata(orig, baseDir)
	if err != nil {
		// 如果原始文件没有该扩展，可按需选择忽略
		return fmt.Errorf("extract structural metadata: %w", err)
	}

	// 2) 调 Node 脚本执行 Draco 压缩
	if err := runPipeline(ctx, inPath, outPath, pp); err != nil {
		return err
	}

	// 3) 打开压缩后的 GLB，注回元数据并保存
	newDoc, err := gltf.Open(outPath)
	if err != nil {
		return fmt.Errorf("open compressed: %w", err)
	}
	if err := InjectStructuralMetadata(newDoc, saved); err != nil {
		return fmt.Errorf("inject structural metadata: %w", err)
	}
	if err := gltf.SaveBinary(newDoc, outPath); err != nil {
		return fmt.Errorf("save compressed with meta: %w", err)
	}
	return nil
}
func runPipeline(ctx context.Context, inPath, outPath string, pp *PipelineParams) error {
	if _, err := os.Stat(inPath); err != nil {
		return fmt.Errorf("input not found: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("prepare out dir: %w", err)
	}

	// npx 参数构造（不写到一行字符串里，逐个参数最稳）
	pkg := fmt.Sprintf("gltf-pipeline@%s", pp.PipelineVersion)
	args := []string{
		"--yes",
		"--package", pkg,
		"gltf-pipeline",
		"-i", inPath,
		"-o", outPath,
		"-d", // 启用 Draco
		"--draco.compressionLevel", fmt.Sprint(pp.CompressionLevel),
		"--draco.quantizePositionBits", fmt.Sprint(pp.QuantizePositionBits),
		"--draco.quantizeNormalBits", fmt.Sprint(pp.QuantizeNormalBits),
		"--draco.quantizeTexcoordBits", fmt.Sprint(pp.QuantizeTexcoordBits),
		"--draco.quantizeGenericBits", fmt.Sprint(pp.QuantizeGenericBits),
	}
	if pp.UnifiedQuantization {
		args = append(args, "--draco.unifiedQuantization", "true")
	}

	// 带超时执行
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, pp.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, pp.NpxPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// 常见报错：npx 不在 PATH / 网络受限 / 公司内网需要代理
		return fmt.Errorf("gltf-pipeline failed: %w\nSTDOUT:\n%s\nSTDERR:\n%s",
			err, stdout.String(), stderr.String())
	}
	return nil
}

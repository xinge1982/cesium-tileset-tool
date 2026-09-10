package utils

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"regexp"
	"strings"

	_ "image/jpeg"
	_ "image/png"
)

// Point 定义
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func ExtractBase64DataURI(b64 string) (mimeType string, pureBase64 string) {
	// 支持形如 data:image/png;base64,xxxxx 的输入
	re := regexp.MustCompile(`^data:(.*?);base64,(.*)$`)
	matches := re.FindStringSubmatch(b64)
	if len(matches) == 3 {
		mimeType = matches[1]
		pureBase64 = matches[2]
	} else {
		// 如果不带说明，默认使用 png
		mimeType = "image/png"
		pureBase64 = b64
	}
	return
}

func BuildDataURI(mimeType, b64 string) string {
	return "data:" + mimeType + ";base64," + b64
}

// PerspectiveWarpBase64 将 srcBase64 的 srcPts（TL,TR,BR,BL） 透视映射到 dstPts（通常是矩形 (0,0)-(w,h)）
// 返回 data:image/png;base64,....
func PerspectiveWarpBase64(srcBase64 string, srcPts [4]Point, dstPts [4]Point, dstW, dstH int) (string, error) {
	// 1. 处理 base64 前缀
	raw := srcBase64
	if idx := strings.Index(raw, "base64,"); idx >= 0 {
		raw = raw[idx+7:]
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
	}

	// 2. 解码图片
	img, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		return "", err
	}
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return "", errors.New("source image has zero size")
	}

	// 3. 计算单应矩阵 H (3x3) 使 srcPts -> dstPts
	H, err := computeHomography([4][2]float64{
		{srcPts[0].X, srcPts[0].Y},
		{srcPts[1].X, srcPts[1].Y},
		{srcPts[2].X, srcPts[2].Y},
		{srcPts[3].X, srcPts[3].Y},
	}, [4][2]float64{
		{dstPts[0].X, dstPts[0].Y},
		{dstPts[1].X, dstPts[1].Y},
		{dstPts[2].X, dstPts[2].Y},
		{dstPts[3].X, dstPts[3].Y},
	})
	if err != nil {
		return "", err
	}

	// 4. 目标图像
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	// 5. 暂存源像素数据访问（使用 img.At）
	// 逆向映射：对每个 (x,y) 在 dst 上，计算源坐标 (sx,sy)
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			// 逆向映射： [sx, sy, w] = H_inv * [x, y, 1]
			// 这里 H 是把 src->dst 求得的矩阵，但我们需要对 dst 上的点用逆 H 映射回 src。
			// 更简单：计算原矩阵 Hsrc2dst，然后按公式用 Hinv (这里通过直接使用求出的矩阵的逆关系做反向映射)
			// 为效率：我们直接用逆映射系数（求 Hinv once），但为了简洁，这里计算 Hinv once:
		}
	}
	// 为避免重复计算 Hinv，在外面先求 Hinv
	Hinv, err := invert3x3(H)
	if err != nil {
		return "", err
	}

	// 主循环（反向映射 + 双线性插值）
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			// 计算源坐标 (sx, sy)
			den := Hinv[2][0]*float64(x) + Hinv[2][1]*float64(y) + Hinv[2][2]
			if math.Abs(den) < 1e-12 {
				// 非常小的分母，跳过（留透明）
				continue
			}
			sx := (Hinv[0][0]*float64(x) + Hinv[0][1]*float64(y) + Hinv[0][2]) / den
			sy := (Hinv[1][0]*float64(x) + Hinv[1][1]*float64(y) + Hinv[1][2]) / den

			// 双线性插值采样并写入 dst
			c := bilinearSampleRGBA(img, sx, sy)
			dst.Set(x, y, c)
		}
	}

	// 6. 编码 PNG 并返回 base64 DataURI
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", err
	}
	outBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	return outBase64, nil
}

// ---------- 辅助函数 ----------

// computeHomography 构建 8x8 线性系统并解出 9 元素 h（最后 h[8]=1）
func computeHomography(src [4][2]float64, dst [4][2]float64) ([3][3]float64, error) {
	// 构造 A (8x8) 和 b (8)
	A := make([][]float64, 8)
	b := make([]float64, 8)
	for i := 0; i < 4; i++ {
		x := src[i][0]
		y := src[i][1]
		u := dst[i][0]
		v := dst[i][1]
		// [ x y 1 0 0 0 -u*x -u*y ] * h(0..7) = u
		// [ 0 0 0 x y 1 -v*x -v*y ] * h(0..7) = v
		A[2*i] = []float64{x, y, 1, 0, 0, 0, -u * x, -u * y}
		A[2*i+1] = []float64{0, 0, 0, x, y, 1, -v * x, -v * y}
		b[2*i] = u
		b[2*i+1] = v
	}

	h8, err := solveLinearSystem(A, b) // 返回长度8的解 (h0..h7)
	if err != nil {
		return [3][3]float64{}, err
	}
	// 组装 H，令 h8 = 1
	H := [3][3]float64{
		{h8[0], h8[1], h8[2]},
		{h8[3], h8[4], h8[5]},
		{h8[6], h8[7], 1.0},
	}
	return H, nil
}

// solveLinearSystem 使用高斯消元（带主元）解 8x8 系统
func solveLinearSystem(A [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	// 增广矩阵
	M := make([][]float64, n)
	for i := 0; i < n; i++ {
		M[i] = make([]float64, n+1)
		for j := 0; j < n; j++ {
			M[i][j] = A[i][j]
		}
		M[i][n] = b[i]
	}

	// 高斯消元带主元
	for i := 0; i < n; i++ {
		// 找主元
		maxRow := i
		maxVal := math.Abs(M[i][i])
		for k := i + 1; k < n; k++ {
			if math.Abs(M[k][i]) > maxVal {
				maxVal = math.Abs(M[k][i])
				maxRow = k
			}
		}
		if maxVal < 1e-12 {
			return nil, errors.New("singular matrix in solveLinearSystem")
		}
		// 交换
		M[i], M[maxRow] = M[maxRow], M[i]

		// 归一化当前行
		pivot := M[i][i]
		for j := i; j <= n; j++ {
			M[i][j] /= pivot
		}

		// 消去其他行
		for r := 0; r < n; r++ {
			if r == i {
				continue
			}
			f := M[r][i]
			if f == 0 {
				continue
			}
			for c := i; c <= n; c++ {
				M[r][c] -= f * M[i][c]
			}
		}
	}

	// 回代（现在矩阵已是单位矩阵）
	x := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = M[i][n]
	}
	return x, nil
}

// invert3x3 计算 3x3 矩阵的逆
func invert3x3(m [3][3]float64) ([3][3]float64, error) {
	a := m
	det := a[0][0]*(a[1][1]*a[2][2]-a[1][2]*a[2][1]) -
		a[0][1]*(a[1][0]*a[2][2]-a[1][2]*a[2][0]) +
		a[0][2]*(a[1][0]*a[2][1]-a[1][1]*a[2][0])
	if math.Abs(det) < 1e-12 {
		return [3][3]float64{}, errors.New("singular homography")
	}
	inv := [3][3]float64{}
	inv[0][0] = (a[1][1]*a[2][2] - a[1][2]*a[2][1]) / det
	inv[0][1] = (a[0][2]*a[2][1] - a[0][1]*a[2][2]) / det
	inv[0][2] = (a[0][1]*a[1][2] - a[0][2]*a[1][1]) / det
	inv[1][0] = (a[1][2]*a[2][0] - a[1][0]*a[2][2]) / det
	inv[1][1] = (a[0][0]*a[2][2] - a[0][2]*a[2][0]) / det
	inv[1][2] = (a[0][2]*a[1][0] - a[0][0]*a[1][2]) / det
	inv[2][0] = (a[1][0]*a[2][1] - a[1][1]*a[2][0]) / det
	inv[2][1] = (a[0][1]*a[2][0] - a[0][0]*a[2][1]) / det
	inv[2][2] = (a[0][0]*a[1][1] - a[0][1]*a[1][0]) / det
	return inv, nil
}

// bilinearSampleRGBA 对 (fx,fy) 做双线性插值，支持边界clamp
func bilinearSampleRGBA(img image.Image, fx, fy float64) color.RGBA {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if math.IsNaN(fx) || math.IsNaN(fy) {
		return color.RGBA{0, 0, 0, 0}
	}

	// clamp 到像素范围 [0, w-1], [0, h-1]
	if fx < 0 {
		fx = 0
	}
	if fy < 0 {
		fy = 0
	}
	if fx > float64(w-1) {
		fx = float64(w - 1)
	}
	if fy > float64(h-1) {
		fy = float64(h - 1)
	}

	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	x1 := x0 + 1
	y1 := y0 + 1
	if x1 >= w {
		x1 = w - 1
	}
	if y1 >= h {
		y1 = h - 1
	}

	wx := fx - float64(x0)
	wy := fy - float64(y0)

	c00 := colorToRGBA(img.At(x0, y0))
	c10 := colorToRGBA(img.At(x1, y0))
	c01 := colorToRGBA(img.At(x0, y1))
	c11 := colorToRGBA(img.At(x1, y1))

	// 双线性插值
	r := (1-wx)*(1-wy)*float64(c00.R) + wx*(1-wy)*float64(c10.R) + (1-wx)*wy*float64(c01.R) + wx*wy*float64(c11.R)
	g := (1-wx)*(1-wy)*float64(c00.G) + wx*(1-wy)*float64(c10.G) + (1-wx)*wy*float64(c01.G) + wx*wy*float64(c11.G)
	b := (1-wx)*(1-wy)*float64(c00.B) + wx*(1-wy)*float64(c10.B) + (1-wx)*wy*float64(c01.B) + wx*wy*float64(c11.B)
	a := (1-wx)*(1-wy)*float64(c00.A) + wx*(1-wy)*float64(c10.A) + (1-wx)*wy*float64(c01.A) + wx*wy*float64(c11.A)

	return color.RGBA{uint8(clamp(r, 0, 255)), uint8(clamp(g, 0, 255)), uint8(clamp(b, 0, 255)), uint8(clamp(a, 0, 255))}
}

func colorToRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

package common

import "fmt"

// UVBuilder 缁熶竴璐熻矗璐村浘鍧愭爣鐢熸垚銆?//
// 鍚庣画鎵€鏈夊嚑浣?builder 閮戒笉搴旇嚜宸变复鏃舵嫾 UV锛岃€屽簲閫氳繃杩欎釜妯″潡缁熶竴澶勭悊锛?// 杩欐牱鍙互纭繚锛?// - 鍚岀被瀵硅薄璐村浘鏂瑰悜涓€鑷达紱
// - 閲嶅鐜囦竴鑷达紱
// - 鎸夌背缂╂斁瑙勫垯涓€鑷淬€?
type UVBuilder struct{}

// NewUVBuilder 鍒涘缓 UV 鐢熸垚鍣ㄣ€?
func NewUVBuilder() *UVBuilder {
	return &UVBuilder{}
}

// BuildTopUV 鐢熸垚椤堕潰 UV銆?//
// 甯哥敤浜庨亾璺潰銆佸缓绛戦《闈€侀《妫氶《闈€?// 涓€鑸噰鐢ㄥ钩闈㈡姇褰辨柟寮忋€?
func (b *UVBuilder) BuildTopUV(vertices [][3]float32, opt UVOptions) ([][2]float32, error) {
	return nil, fmt.Errorf("build top uv: not implemented")
}

// BuildSideUV 鐢熸垚渚ч潰 UV銆?//
// 甯歌瑙勫垯锛?// - U 娌块暱搴︽柟鍚戯紱
// - V 娌块珮搴︽柟鍚戯紱
// - 鍙寜鐪熷疄绫冲埗閲嶅銆?
func (b *UVBuilder) BuildSideUV(vertices [][3]float32, height float32, opt UVOptions) ([][2]float32, error) {
	return nil, fmt.Errorf("build side uv: not implemented")
}

// BuildBottomUV 鐢熸垚搴曢潰 UV銆?//
// 閬撹矾闈€侀《妫氱瓑瀵硅薄濡傛灉闇€瑕佸簳闈㈣创鍥撅紝鍙€氳繃璇ユ柟娉曠粺涓€鐢熸垚銆?
func (b *UVBuilder) BuildBottomUV(vertices [][3]float32, opt UVOptions) ([][2]float32, error) {
	return nil, fmt.Errorf("build bottom uv: not implemented")
}

// BuildSweepUV 涓烘部绾挎壂鎺犲璞＄敓鎴?UV銆?//
// 杩欑被 UV 涓€鑸姹傦細
// - U 娌跨嚎绱闀垮害锛?// - V 娌?profile 鎴潰楂樺害鎴栧搴︼紱
// - 杞澶勫敖閲忎繚鎸佽繛缁€?
func (b *UVBuilder) BuildSweepUV(vertices [][3]float32, path LocalLine, opt UVOptions) ([][2]float32, error) {
	return nil, fmt.Errorf("build sweep uv: not implemented")
}

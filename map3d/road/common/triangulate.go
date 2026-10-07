package common

import "fmt"

// Triangulator 璐熻矗闈㈠墫鍒嗗拰闈㈡尋鍑虹浉鍏崇殑鍑犱綍鍩虹鑳藉姏銆?//
// 瀹冧笉鍏冲績鏌愪釜瀵硅薄鏄亾璺€佸缓绛戣繕鏄《妫氾紝鍙叧娉細
// - 涓€涓潰濡備綍涓夎鍓栧垎锛?// - 涓€涓潰濡備綍鎷夊嚭鍘氬害锛?// - 椤?渚?搴曠綉鏍煎浣曟媶鍒嗐€?
type Triangulator struct{}

// NewTriangulator 鍒涘缓涓夎鍓栧垎宸ュ叿銆?
func NewTriangulator() *Triangulator {
	return &Triangulator{}
}

// TriangulateSurface 瀵逛竴涓甫澶栫幆鍜屽唴鐜殑闈㈣繘琛屼笁瑙掑墫鍒嗐€?//
// 璇ユ柟娉曞簲褰撴槸鍚庣画閬撹矾闈€佸缓绛戦《闈€侀《妫氶《闈㈡瀯寤虹殑閫氱敤鍩虹銆?
func (t *Triangulator) TriangulateSurface(rings LocalRings) ([]uint32, error) {
	return nil, fmt.Errorf("triangulate surface: not implemented")
}

// ExtrudeSurface 瀵逛竴涓潰鎸夊帤搴﹁繘琛屾媺浼搞€?//
// 棰勬湡杈撳嚭澶氫釜 MeshPart锛屼緥濡傦細
// - top锛?// - side锛?// - bottom銆?//
// 杩欐牱鍚庣画鍙互閽堝涓嶅悓 part 鍒嗗埆缁戝畾涓嶅悓鏉愯川鍜?UV銆?
func (t *Triangulator) ExtrudeSurface(rings LocalRings, thickness float32) ([]MeshPart, error) {
	return nil, fmt.Errorf("extrude surface: not implemented")
}

// MeshPart 琛ㄧず涓€涓彲鐙珛璧嬫潗璐ㄣ€佽祴 UV 鐨勭綉鏍煎垎鍧椼€?//
// 渚嬪涓€涓亾璺潰瀹炰綋锛岄€氬父鍙互鎷嗕负 top / side / bottom 涓変釜 part銆?
type MeshPart struct {
	Name     string
	Vertices [][3]float32
	Indices  []uint32
}

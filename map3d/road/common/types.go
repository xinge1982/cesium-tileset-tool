package common

import (
	"fmt"

	"github.com/twpayne/go-geom"
)

// BuildType 琛ㄧず鈥滅敓鎴愮被鍨嬧€濄€?//
// 瀹冧笉鏄暟鎹簱涓殑鍘熷涓氬姟缂栫爜锛岃€屾槸鍑犱綍鏋勫缓闃舵鍐呴儴缁熶竴浣跨敤鐨勭被鍨嬨€?// 姣忎竴涓緭鍏ュ璞￠兘蹇呴』鏄庣‘鑷繁鐨?BuildType锛屽悗缁墠鑳藉喅瀹氾細
// 1. 璇ュ璞¤蛋绾垮鐞嗚繕鏄潰澶勭悊锛?// 2. 璇ュ璞℃槸灞曞銆佹壂鎺犺繕鏄尋鍑猴紱
// 3. 浣跨敤鍝竴濂楅粯璁ゆ潗璐ㄣ€乁V 鍜屽帤搴﹂€昏緫銆?
type BuildType string

const (
	BuildTypeRoadMarking               BuildType = "road_marking"
	BuildTypeRoadMarkingSolid          BuildType = BuildTypeRoadMarking
	BuildTypeRoadMarkingDashed         BuildType = BuildTypeRoadMarking
	BuildTypeRoadExpansionJoint        BuildType = "road_expansion_joint"
	BuildTypeRoadCurb                  BuildType = "road_curb"
	BuildTypeLinearInstancedModel      BuildType = "linear_instanced_model"
	BuildTypeBarrierAntiGlareBoard     BuildType = BuildTypeLinearInstancedModel
	BuildTypeBarrierRigid              BuildType = "barrier_rigid"
	BuildTypeBarrierNewJersey          BuildType = BuildTypeBarrierRigid
	BuildTypeBarrierConcreteWall       BuildType = BuildTypeBarrierRigid
	BuildTypeBarrierWave               BuildType = "barrier_wave"
	BuildTypeBarrierGuardrailTwoWave   BuildType = BuildTypeBarrierWave
	BuildTypeBarrierGuardrailThreeWave BuildType = BuildTypeBarrierWave
	BuildTypeBarrierGuardrailNoseEnd   BuildType = BuildTypeBarrierWave
	BuildTypeBarrierGuardrail          BuildType = BuildTypeBarrierWave
	BuildTypeBarrierNoiseWall          BuildType = "barrier_noise_wall"
	BuildTypeRoadSurface               BuildType = "road_surface"
	BuildTypeRoadSubgrade              BuildType = "road_subgrade"
	BuildTypeRoadRaisedSurface         BuildType = "road_raised_surface"
	BuildTypeRoadGreenbelt             BuildType = BuildTypeRoadRaisedSurface
	BuildTypeRoadMarkSurface           BuildType = "road_mark_surface"
	BuildTypeRoadSurfaceExtrude        BuildType = BuildTypeRoadSurface
	BuildTypeBuildingExtrude           BuildType = "building_extrude"
	BuildTypeCanopyColumns             BuildType = "canopy_columns"
	BuildTypeRoadTollIsland            BuildType = "road_toll_island" //收费岛

)

type MaterialMode string

const (
	MaterialModeTexture MaterialMode = "texture"
	MaterialModeColor   MaterialMode = "color"
)

type UVMapping string

const (
	UVMappingPlanar UVMapping = "planar"
	UVMappingStrip  UVMapping = "strip"
	UVMappingSweep  UVMapping = "sweep"
)

// TileContext 鎻忚堪褰撳墠 3D Tiles 鐡︾墖鐨勬瀯寤轰笂涓嬫枃銆?//
// 寤鸿涓€涓摝鐗囧搴斾竴涓?TileContext銆?// 鍚庣画鎵€鏈夊嚑浣曢兘浼氬熀浜庤繖涓笂涓嬫枃鍋氳鍓€佸潗鏍囪浆鎹㈠拰杈撳嚭銆?
type TileContext struct {
	TileID    string
	Region    [6]float64
	Center    [3]float64
	BasePoint []float64
	SRID      int
	UseENU    bool
}

// Validate 鏍￠獙褰撳墠鐡︾墖涓婁笅鏂囨槸鍚︽弧瓒虫瀯寤鸿姹傘€?//
// 褰撳墠鍙仛鏈€鍩虹鏍￠獙锛屽悗缁彲浠ョ户缁ˉ鍏咃細
// - Region 鏄惁鏈夋晥锛?// - Center 鏄惁鍚堢悊锛?// - SRID 鏄惁涓庤緭鍏ユ暟鎹尮閰嶃€?
func (c TileContext) Validate() error {
	if len(c.BasePoint) < 2 {
		return fmt.Errorf("tile base point requires at least 2 values")
	}
	return nil
}

// BuildOptions 瀹氫箟鏁翠釜鐡︾墖鏋勫缓杩囩▼鐨勫紑鍏冲拰榛樿鍙傛暟銆?//
// 杩欎簺閫夐」灞炰簬鈥滄瀯寤鸿涓烘帶鍒垛€濓紝鑰屼笉鏄煇涓€涓叿浣撳璞＄殑灞炴€с€?
type BuildOptions struct {
	ClipToTile         bool
	BuildNormals       bool
	MergePrimitives    bool
	IncludeBottom      bool
	DefaultThickness   float32
	DefaultHeight      float32
	UseInputCenterline bool
	// LineProjectionMode controls how projected line-like features are refined before
	// projecting onto road triangles. Supported values:
	// - "fast": no boundary-split refinement; only optional active densify by LineSampleStep.
	// - "boundary_split": insert points at road-triangle boundary crossings first.
	LineProjectionMode string
	// LineSampleStep controls optional active densify spacing before road projection,
	// and applies to road_marking and road_expansion_joint.
	// = 0: only insert points at road-triangle boundary crossings.
	// > 0: after boundary-crossing insertion, continue densifying by this step.
	LineSampleStep float32
	// MarkingSampleStep is kept for compatibility; when LineSampleStep is 0,
	// this value is reused as the fallback source.
	MarkingSampleStep  float32
	ProjectSearchDist  float32
	ProjectSmoothMeter float64
}

// DefaultBuildOptions 杩斿洖榛樿鏋勫缓鍙傛暟銆?//
// 杩欎簺鍊间富瑕佺敤浜庝袱绫诲満鏅細
// 1. 涓婃父娌℃湁鏄庣‘浼犲€兼椂鐨勫厹搴曪紱
// 2. 缁熶竴璋冭瘯鏃剁殑榛樿琛屼负銆?
func DefaultBuildOptions() BuildOptions {
	return BuildOptions{
		ClipToTile:         true,
		BuildNormals:       true,
		MergePrimitives:    true,
		IncludeBottom:      true,
		DefaultThickness:   0.15,
		DefaultHeight:      3.0,
		UseInputCenterline: false,
		LineProjectionMode: "fast",
		LineSampleStep:     6.0,
		MarkingSampleStep:  6.0,
		ProjectSearchDist:  2.0,
		ProjectSmoothMeter: 1.5,
	}
}

// MaterialRef 鎻忚堪涓€涓潗璐ㄦ潵婧愩€?//
// 涓€涓潗璐ㄥ彲浠ユ潵婧愪簬璐村浘锛屼篃鍙互鏉ユ簮浜庣函鑹层€?// 鍚庣画鍙户缁墿灞曢€忔槑搴︺€佹硶绾胯创鍥俱€侀噾灞炲害绛夊弬鏁般€?
type MaterialRef struct {
	Name        string
	Path        string
	Color       string
	Mode        MaterialMode
	DoubleSided bool
}

// MaterialSet 鎻忚堪涓€涓璞″彲鐢ㄧ殑澶氶潰鏉愯川闆嗗悎銆?//
// 鍏稿瀷鐢ㄩ€旓細
// - 閬撹矾瀹炰綋锛歍op/Side/Bottom锛?// - 寤虹瓚鐗╋細Top/Side锛?// - 椤舵锛歍op/Side/Bottom锛?// - 鎶ゆ爮锛欶ront/Back/Side銆?
type MaterialSet struct {
	Top    MaterialRef
	Side   MaterialRef
	Bottom MaterialRef
	Front  MaterialRef
	Back   MaterialRef
}

// UVOptions 瀹氫箟璐村浘鍧愭爣鐢熸垚鏂瑰紡銆?//
// 涓嶅悓鍑犱綍绫诲瀷浣跨敤涓嶅悓鐨?UV 鏂规锛?// - 闈㈢被閫氬父浣跨敤骞抽潰鏄犲皠锛?// - 娌跨嚎瀵硅薄閫氬父浣跨敤鏉″甫鏄犲皠鎴栨壂鎺犳槧灏勶紱
// - 鏄惁鎸夌湡瀹炵背鍒剁缉鏀句篃鍦ㄨ繖閲屾帶鍒躲€?
type UVOptions struct {
	Mapping       UVMapping
	RepeatX       float32
	RepeatY       float32
	RotateDeg     float32
	FlipV         bool
	ScaleByMeters bool
}

// FeatureFields 琛ㄧず瑕佸啓鍏?3D Tiles 鍏冩暟鎹腑鐨勫瓧娈甸泦鍚堛€?//
// 渚嬪锛?// - id
// - name
// - road_id
// - kind
// - 浠绘剰鍏跺畠甯屾湜淇濈暀鍒?3D Tiles property / metadata 涓殑涓氬姟瀛楁
//
// 杩欓噷缁熶竴浣跨敤 map[string]any锛屽悗缁湪鐪熸鍐欏叆 3D Tiles 鍏冩暟鎹椂锛?// 鍐嶆牴鎹€肩被鍨嬪仛褰掍竴鍖栧拰搴忓垪鍖栥€?
type FeatureFields map[string]any

// FeatureInput 鏄墍鏈夎緭鍏ュ璞＄殑鍏叡澶撮儴銆?//
// 浣犺姹傜殑涓変釜寮哄埗杈撳叆閮芥斁鍦ㄨ繖閲岋細
// - BuildType: 鍐冲畾鎬庝箞鐢熸垚锛?// - Geom: 鍘熷鍑犱綍锛岀害瀹氳緭鍏ヤ负 4326锛?// - Fields: 闇€瑕佷繚鐣欏埌 3D Tiles 涓殑灞炴€у瓧娈点€?//
// 鍚庣画鎵€鏈夌嚎绫汇€侀潰绫诲璞￠兘蹇呴』鍖呭惈杩欓儴鍒嗗叕鍏变俊鎭€?
type FeatureInput struct {
	BuildType BuildType
	Geom      geom.T
	Fields    FeatureFields
	Features  []FeatureFields
}

// Validate 鏍￠獙鍏叡杈撳叆澶淬€?//
// 鎵€鏈夎緭鍏ュ璞￠兘蹇呴』鍏峰杩欎笁涓熀纭€鍏冪礌锛?// - BuildType: 鍐冲畾鐢熸垚璺緞锛?// - Geom: 鍘熷 4326 鍑犱綍锛?// - Fields: 杩涘叆 3D Tiles 鍏冩暟鎹殑瀛楁闆嗗悎銆?
func (f FeatureInput) Validate() error {
	if f.BuildType == "" {
		return fmt.Errorf("feature build type is required")
	}
	if f.Geom == nil {
		return fmt.Errorf("feature geom is required")
	}
	if f.Fields == nil {
		return fmt.Errorf("feature fields are required")
	}
	return nil
}

// LineFeature 琛ㄧず鐢辩嚎鐢熸垚鐨勮绱犮€?//
// 瀹冨湪 FeatureInput 鐨勫熀纭€涓婏紝鍐嶈ˉ鍏呯嚎绫讳笓鏈夊弬鏁帮細
// - 瀹藉害锛?// - 楂樺害锛?// - 鍘氬害锛?// - 鏄惁闂悎锛?// - 鏉愯川涓?UV銆?
type LineFeature struct {
	FeatureInput
	Width     float32
	Height    float32
	Thickness float32
	Closed    bool
	Material  MaterialSet
	UV        UVOptions
}

func (f LineFeature) Validate() error {
	return f.FeatureInput.Validate()
}

// SurfaceFeature 琛ㄧず鐢遍潰鐢熸垚鐨勮绱犮€?//
// 瀹冨悓鏍风户鎵垮叕鍏辫緭鍏ュご锛屽啀琛ュ厖闈㈢被涓撴湁鍙傛暟锛?// - 楂樺害锛?// - 鍘氬害锛?// - 灞傞珮锛?// - 灞傛暟锛?// - 鏉愯川涓?UV銆?
type SurfaceFeature struct {
	FeatureInput
	Height      float32
	Thickness   float32
	FloorHeight float32
	Levels      int
	Material    MaterialSet
	UV          UVOptions
}

func (f SurfaceFeature) Validate() error {
	return f.FeatureInput.Validate()
}

// CenterlineFeature 表示用于辅助道路面剖分的中心线输入。
//
// 这类数据不直接生成 glTF 几何，而是作为道路面条带剖分的控制线：
// - 用于确定道路主方向；
// - 用于按 road_id / hroad_id 找到对应的道路面；
// - 用于生成横断面，改善匝道、坡道等长条道路面的三角质量。
type CenterlineFeature struct {
	Geom   geom.T
	Fields FeatureFields
}

func (f CenterlineFeature) Validate() error {
	if f.Geom == nil {
		return fmt.Errorf("centerline geom is required")
	}
	if f.Fields == nil {
		return fmt.Errorf("centerline fields are required")
	}
	return nil
}

// Profile2D 琛ㄧず涓€涓簩缁存í鏂潰銆?//
// 璇ョ粨鏋勪富瑕佹湇鍔′簬鈥滄部绾挎壂鎺犫€濈被瀵硅薄锛屼緥濡傦細
// - 璺紭鐭虫í鏂潰锛?// - 鏂版辰瑗挎姢鏍忔í鏂潰锛?// - 鏅€氬浣撴í鏂潰銆?
type Profile2D struct {
	Name   string
	Points [][2]float32
	Closed bool
}

// LocalLine 琛ㄧず宸茬粡杞崲鍒扮摝鐗囧眬閮ㄥ潗鏍囩郴涓殑绾裤€?//
// 鍚庣画鐨勫睍瀹姐€佹壂鎺犮€乁V 璁＄畻閮藉簲璇ュ熀浜庡眬閮ㄥ潗鏍囪繘琛岋紝
// 鑰屼笉鏄洿鎺ュ熀浜庣粡绾害鍧愭爣銆?
type LocalLine struct {
	Points [][3]float32
	Closed bool
}

// LocalRings 琛ㄧず宸茬粡杞崲鍒板眬閮ㄥ潗鏍囩郴鐨勪竴涓潰銆?//
// Outer 涓哄鐜紝Holes 涓哄唴鐜泦鍚堛€?// 璇ョ粨鏋勬槸闈笁瑙掑墫鍒嗗拰闈㈡尋鍑虹殑鏍稿績杈撳叆鏁版嵁銆?
type LocalRings struct {
	Outer [][3]float32
	Holes [][][3]float32
}

package common

import (
	"fmt"

	"cesium-tileset-tool/mapmodel/aabb"

	"github.com/twpayne/go-geom"
)

// 坐标处理管线。
//
// 主要负责 Geometry 数据从 GIS 坐标系转换到
// 3D Tile / Mesh 构建使用的局部坐标系。
//
// 主要功能:
//
//  1. 根据 TileContext 创建当前 Tile 的参考坐标系
//
//  2. 根据需要建立 ENU (East-North-Up) 局部坐标框架
//
//  3. 将输入的 LineString / Polygon 转换为:
//     - LocalLine
//     - LocalRings
//
//  4. 为后续 Mesh 生成提供局部米制坐标
//
// 坐标输出约定:
//
//	X:
//	  东方向 (East)
//
//	Y:
//	  高程方向 (Up)
//
//	Z:
//	  北方向 (North)
//
// 当使用 ENU 模式时:
//
//	WGS84 经纬高坐标会转换为相对于 Tile 中心的局部坐标。
//
// 当不使用 ENU 时:
//
//	使用 BasePoint 做简单局部偏移。
type CoordinatePipeline struct {
	// 当前 Tile 的上下文信息。
	//
	// 包含:
	//   - SRID
	//   - Tile 基准点
	//   - 是否使用 ENU
	ctx TileContext

	// ENU 坐标转换参考框架。
	//
	// 当输入为经纬度坐标时，
	// 使用该对象将地理坐标转换为局部米制坐标。
	centerENU *aabb.ENUFrame
}

// 创建坐标处理管线。
//
// 初始化过程:
//  1. 校验 TileContext 参数
//  2. 根据 SRID 和 UseENU 判断是否创建 ENU 坐标系
//  3. 保存 Tile 中心参考点
//
// 后续所有 Geometry 转换都会通过该 Pipeline
// 转换到局部米制坐标。
//
// 坐标输出约定:
//
//	X: 东方向
//	Y: 高程方向
//	Z: 北方向
func NewCoordinatePipeline(ctx TileContext) (*CoordinatePipeline, error) {
	if err := ctx.Validate(); err != nil {
		return nil, err
	}
	p := &CoordinatePipeline{ctx: ctx}
	if ctx.SRID == 4326 || ctx.UseENU {
		h := 0.0
		if len(ctx.BasePoint) >= 3 {
			h = ctx.BasePoint[2]
		}
		enu, err := aabb.NewENUFrame(ctx.BasePoint[0], ctx.BasePoint[1], h)
		if err != nil {
			return nil, err
		}
		p.centerENU = enu
	}
	return p, nil
}

// 将 Line 类型 Feature 裁剪到当前 Tile 范围。
//
// 典型应用:
//
//  1. 道路中心线跨多个 Tile 时:
//     按 Tile 边界拆分
//
//  2. 护栏、路缘石等线状设施:
//     避免整条线进入单个 Tile
//
//  3. 减少后续坐标转换和 Mesh 生成的数据量
//
// 当前实现:
//
// 暂时直接返回输入 Feature。
//
// 保留该接口用于后续接入:
//   - AABB 裁剪
//   - Polygon clip
//   - Line split
func (p *CoordinatePipeline) ClipLineFeatureToTile(feature LineFeature) ([]LineFeature, error) {
	// 当前阶段不执行真实裁剪，
	// 后续可以在此处增加 Tile 范围裁剪逻辑。
	return []LineFeature{feature}, nil
}

// 将 Surface 类型 Feature 裁剪到当前 Tile 范围。
//
// 典型应用:
//
//  1. 道路面跨 Tile 时进行切割
//
//  2. 建筑物、地面、覆盖物等大面积 Polygon
//     按 Tile 范围拆分
//
//  3. 减少单个 Tile 中无效 Geometry
//
// 支持后续扩展:
//
//	Polygon Clip
//
//	MultiPolygon 拆分
//
//	Tile 边界裁剪
//
// 当前实现:
//
//	暂时直接返回输入 Feature。
func (p *CoordinatePipeline) ClipSurfaceFeatureToTile(feature SurfaceFeature) ([]SurfaceFeature, error) {
	// 当前阶段保持透传，
	// 后续增加 Polygon 裁剪。
	return []SurfaceFeature{feature}, nil
}

// 将 GIS Line 类型 Geometry 转换为局部坐标线。
//
// 输入:
//
// # LineFeature
//
// 支持:
//
//   - LineString
//   - MultiLineString
//
// 输出:
//
// []LocalLine
//
// 原因:
//
// 一个 Geometry 可能包含多条线:
//
//	MultiLineString
//	     |
//	     +--- Line 1
//	     |
//	     +--- Line 2
//
// 因此统一拆分为多个 LocalLine。
//
// 坐标转换:
//
// 输入:
//
//	WGS84 / 投影坐标
//
// 输出:
//
//	Tile Local Coordinate
//
// 输出坐标约定:
//
//	X: East
//	Y: Up(height)
//	Z: North
func (p *CoordinatePipeline) ToLocalLine(feature LineFeature) ([]LocalLine, error) {
	if len(p.ctx.BasePoint) < 2 {
		return nil, fmt.Errorf("tile base point requires at least 2 values")
	}

	switch g := feature.Geom.(type) {
	case *geom.LineString:
		line, err := p.lineStringToLocalLine(g)
		if err != nil {
			return nil, err
		}
		return []LocalLine{line}, nil
	case *geom.MultiLineString:
		if g.NumLineStrings() == 0 {
			return nil, fmt.Errorf("empty multilinestring")
		}
		out := make([]LocalLine, 0, g.NumLineStrings())
		for i := 0; i < g.NumLineStrings(); i++ {
			line, err := p.lineStringToLocalLine(g.LineString(i))
			if err != nil {
				return nil, err
			}
			out = append(out, line)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported line geometry type: %T", feature.Geom)
	}
}

// 将 GIS Surface Geometry 转换为局部坐标面。
//
// 支持:
//
//   - Polygon
//   - MultiPolygon
//
// 输出:
//
// []LocalRings
//
// Polygon 结构:
//
//	Outer Ring
//	     |
//	     +--- Hole Ring
//
// 转换后:
//
// LocalRings:
//
//	Outer
//	Holes
//
// 供后续:
//
//   - Mesh triangulation
//   - glTF primitive generation
//
// 使用。
func (p *CoordinatePipeline) ToLocalSurface(feature SurfaceFeature) ([]LocalRings, error) {
	switch g := feature.Geom.(type) {
	case *geom.Polygon:
		rings, err := p.polygonToLocalRings(g)
		if err != nil {
			return nil, err
		}
		return []LocalRings{rings}, nil
	case *geom.MultiPolygon:
		if g.NumPolygons() == 0 {
			return nil, fmt.Errorf("empty multipolygon")
		}
		out := make([]LocalRings, 0, g.NumPolygons())
		for i := 0; i < g.NumPolygons(); i++ {
			rings, err := p.polygonToLocalRings(g.Polygon(i))
			if err != nil {
				return nil, err
			}
			out = append(out, rings)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported surface geometry type: %T", feature.Geom)
	}
}

// 将单个 geom.LineString 转换为 LocalLine。
//
// 处理内容:
//
//  1. 读取 go-geom FlatCoords
//  2. 根据 stride 判断是否包含 Z
//  3. 转换为局部三维点
//  4. 判断线是否闭合
//
// 输出:
//
// LocalLine:
//
//	Points:
//	    [x,y,z]
//
//	Closed:
//	    是否首尾闭合
//
// 用途:
//
// 后续用于:
//
//   - 道路 Mesh
//   - 护栏生成
//   - 路缘石生成
//   - 线状设施建模
func (p *CoordinatePipeline) lineStringToLocalLine(line *geom.LineString) (LocalLine, error) {
	if line == nil {
		return LocalLine{}, fmt.Errorf("line string is nil")
	}

	points, err := p.flatCoordsToLocalPoints(line.FlatCoords(), line.Stride())
	if err != nil {
		return LocalLine{}, err
	}
	if len(points) < 2 {
		return LocalLine{}, fmt.Errorf("line string needs at least 2 points")
	}

	local := LocalLine{
		Points: points,
	}
	if len(points) > 1 {
		first := points[0]
		last := points[len(points)-1]
		local.Closed = first[0] == last[0] && first[1] == last[1] && first[2] == last[2]
	}
	return local, nil
}

// 将 geom.Polygon 转换为 LocalRings。
//
// Polygon:
//
//	Ring 0:
//	    Outer boundary
//
//	Ring 1..N:
//	    Holes
//
// 转换后:
//
// LocalRings:
//
//	Outer
//
//	Holes[]
//
// 同时完成:
//
//   - FlatCoords解析
//   - 坐标转换
//   - Ring分类
func (p *CoordinatePipeline) polygonToLocalRings(poly *geom.Polygon) (LocalRings, error) {
	if poly == nil {
		return LocalRings{}, fmt.Errorf("polygon is nil")
	}
	flat := poly.FlatCoords()
	ends := poly.Ends()
	stride := poly.Stride()
	if len(ends) == 0 {
		return LocalRings{}, fmt.Errorf("polygon has no rings")
	}

	start := 0
	local := LocalRings{}
	for i, end := range ends {
		if end <= start || end > len(flat) {
			return LocalRings{}, fmt.Errorf("invalid ring end: %d", end)
		}
		points, err := p.flatCoordsToLocalPoints(flat[start:end], stride)
		if err != nil {
			return LocalRings{}, err
		}
		if i == 0 {
			local.Outer = points
		} else {
			local.Holes = append(local.Holes, points)
		}
		start = end
	}
	if len(local.Outer) < 3 {
		return LocalRings{}, fmt.Errorf("polygon outer ring needs at least 3 points")
	}
	return local, nil
}

// 将 go-geom Geometry 的 FlatCoords 转换为内部局部三维坐标点。
//
// 输入:
//
// flat:
//
//	go-geom 存储的一维坐标数组。
//
// stride:
//
//	每个点包含的坐标数量:
//
//	  stride = 2:
//	     X,Y
//
//	  stride = 3:
//	     X,Y,Z
//
// 输出:
//
// [] [3]float32
//
// 坐标格式:
//
//	[X, Y, Z]
//
// 其中:
//
//	X:
//	   东西方向 (East)
//
//	Y:
//	   高程方向 (Up)
//
//	Z:
//	   南北方向 (North)
//
// 转换模式:
//
// 1. ENU 模式:
//
// 输入:
//
//	WGS84:
//	   longitude
//	   latitude
//	   height
//
// 通过 centerENU.Offset:
//
//	经纬高
//	     |
//	     v
//	ENU local coordinate
//
// 2. 非 ENU 模式:
//
// 使用 Tile BasePoint 做简单偏移:
//
//	X = inputX - baseX
//
//	Y = inputZ - baseHeight
//
//	Z = inputY - baseY
//
// 这样可以将全球坐标转换为 Tile 内部局部坐标，
// 避免 glTF 大坐标导致的精度问题。
func (p *CoordinatePipeline) flatCoordsToLocalPoints(flat []float64, stride int) ([][3]float32, error) {
	if stride < 2 {
		return nil, fmt.Errorf("unsupported stride: %d", stride)
	}
	if len(flat)%stride != 0 {
		return nil, fmt.Errorf("invalid flat coords length %d for stride %d", len(flat), stride)
	}

	points := make([][3]float32, 0, len(flat)/stride)
	for i := 0; i < len(flat); i += stride {
		x := flat[i]
		y := flat[i+1]
		z := 0.0
		if stride >= 3 {
			z = flat[i+2]
		}

		if p.centerENU != nil {
			off := p.centerENU.Offset(x, y, z)
			points = append(points, [3]float32{float32(off[0]), float32(off[2]), float32(off[1])})
			continue
		}

		baseZ := 0.0
		if len(p.ctx.BasePoint) >= 3 {
			baseZ = p.ctx.BasePoint[2]
		}
		points = append(points, [3]float32{
			float32(x - p.ctx.BasePoint[0]),
			float32(z - baseZ),
			float32(y - p.ctx.BasePoint[1]),
		})
	}
	return points, nil
}

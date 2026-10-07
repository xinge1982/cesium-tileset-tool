package common

import (
	"fmt"
	"math"

	"github.com/twpayne/go-geom"
)

// CalcBasePointAndRegionFromGeometries 根据输入几何自动计算 BasePoint 和 Region。
//
// 规则：
//  1. 统一按几何的二维范围计算 min/max lon/lat；
//  2. BasePoint 取包围盒中心点，Z 默认置 0；
//  3. Region 返回 [west, south, east, north, minHeight, maxHeight]，
//     当前高度范围默认返回 0,0，由后续业务自行扩展。
func CalcBasePointAndRegionFromGeometries(geometries ...geom.T) ([]float64, [6]float64, error) {
	var region [6]float64
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)

	for _, g := range geometries {
		if g == nil {
			continue
		}
		flat := g.FlatCoords()
		stride := g.Stride()
		if stride < 2 {
			continue
		}
		for i := 0; i+stride-1 < len(flat); i += stride {
			lon := flat[i]
			lat := flat[i+1]
			if lon < minLon {
				minLon = lon
			}
			if lon > maxLon {
				maxLon = lon
			}
			if lat < minLat {
				minLat = lat
			}
			if lat > maxLat {
				maxLat = lat
			}
		}
	}

	if math.IsInf(minLon, 0) || math.IsInf(minLat, 0) || math.IsInf(maxLon, 0) || math.IsInf(maxLat, 0) {
		return nil, region, fmt.Errorf("failed to calculate basepoint and region from geometries")
	}

	basePoint := []float64{
		(minLon + maxLon) / 2,
		(minLat + maxLat) / 2,
		0,
	}
	region = [6]float64{minLon, minLat, maxLon, maxLat, 0, 0}
	return basePoint, region, nil
}

// CalcBasePointAndRegionFromFeatures 根据当前瓦片内全部要素自动计算 BasePoint 和 Region。
//
// 该方法适合在调用方没有传入 TileContext.BasePoint 时使用。
func CalcBasePointAndRegionFromFeatures(centerlines []CenterlineFeature, lines []LineFeature, surfaces []SurfaceFeature) ([]float64, [6]float64, error) {
	geometries := make([]geom.T, 0, len(centerlines)+len(lines)+len(surfaces))
	for i := range centerlines {
		if centerlines[i].Geom != nil {
			geometries = append(geometries, centerlines[i].Geom)
		}
	}
	for i := range lines {
		if lines[i].Geom != nil {
			geometries = append(geometries, lines[i].Geom)
		}
	}
	for i := range surfaces {
		if surfaces[i].Geom != nil {
			geometries = append(geometries, surfaces[i].Geom)
		}
	}
	return CalcBasePointAndRegionFromGeometries(geometries...)
}

// ResolveTileContextAuto 自动补齐 TileContext 中缺失的 BasePoint / Region / Center。
//
// 当前策略：
// - BasePoint 为空时，按当前瓦片全部输入要素范围自动计算；
// - Region 全 0 时，同步回填自动计算结果；
// - Center 全 0 时，默认取 BasePoint 的经纬度中心。
func ResolveTileContextAuto(ctx TileContext, centerlines []CenterlineFeature, lines []LineFeature, surfaces []SurfaceFeature) (TileContext, error) {
	needsBasePoint := len(ctx.BasePoint) < 2
	needsRegion := ctx.Region == [6]float64{}
	if needsBasePoint || needsRegion {
		basePoint, region, err := CalcBasePointAndRegionFromFeatures(centerlines, lines, surfaces)
		if err != nil {
			return ctx, err
		}
		if needsBasePoint {
			ctx.BasePoint = basePoint
		}
		if needsRegion {
			ctx.Region = region
		}
	}
	if ctx.Center == [3]float64{} && len(ctx.BasePoint) >= 2 {
		ctx.Center = [3]float64{ctx.BasePoint[0], ctx.BasePoint[1], 0}
	}
	return ctx, nil
}

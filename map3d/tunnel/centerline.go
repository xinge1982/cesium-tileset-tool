package tunnel

import (
	"encoding/json"
	"fmt"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"math"
	"os"
	"strings"
)

type tunnelCenterline struct {
	points   [][3]float32
	stations []float64
}

// 统一字符串与数值形式的隧道编号，避免数字编号被格式化成科学计数法。
func tunnelGroupID(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%g", v)
	case json.Number:
		return v.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

// 将中线转换到模型局部坐标，按 group_id 建立独立的纵向里程参考。
func loadTunnelCenterlines(path string, origin []float64) (map[string]*tunnelCenterline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fc := new(geojson.FeatureCollection)
	if err = json.Unmarshal(data, fc); err != nil {
		return nil, err
	}
	result := make(map[string]*tunnelCenterline)
	for i, f := range fc.Features {
		id := tunnelGroupID(f.Properties["group_id"])
		if id == "" {
			return nil, fmt.Errorf("centerline feature %d: missing group_id", i)
		}
		if result[id] != nil {
			return nil, fmt.Errorf("duplicate centerline group_id %s; use one continuous LineString per tunnel", id)
		}
		line, ok := f.Geometry.(*geom.LineString)
		if !ok {
			return nil, fmt.Errorf("centerline %s: expected LineString or LineStringZ", id)
		}
		ref := new(tunnelCenterline)
		flat, stride := line.FlatCoords(), line.Stride()
		for j := 0; j < len(flat); j += stride {
			h := 0.0
			if stride >= 3 {
				h = flat[j+2]
			}
			if !isFinite(flat[j]) || !isFinite(flat[j+1]) || !isFinite(h) {
				return nil, fmt.Errorf("centerline %s: nonfinite coordinate", id)
			}
			e, n, u := wgs84ToLocal(flat[j], flat[j+1], h, origin[0], origin[1], origin[2])
			p := [3]float32{float32(e), float32(u), float32(-n)}
			s := 0.0
			if count := len(ref.points); count > 0 {
				q := ref.points[count-1]
				d := math.Hypot(float64(p[0])-float64(q[0]), float64(p[2])-float64(q[2]))
				if d < 1e-6 {
					continue
				}
				s = ref.stations[count-1] + d
			}
			ref.points = append(ref.points, p)
			ref.stations = append(ref.stations, s)
		}
		if len(ref.points) < 2 {
			return nil, fmt.Errorf("centerline %s: need two distinct horizontal points", id)
		}
		result[id] = ref
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("centerline collection is empty")
	}
	return result, nil
}

// 在水平中线的最近线段上求里程和有符号横向距离，中线 Z 不作为墙底高度。
func (line *tunnelCenterline) coordinates(p [3]float32) (station, lateral float64) {
	best := math.Inf(1)
	for i := 0; i+1 < len(line.points); i++ {
		a, b := line.points[i], line.points[i+1]
		dx, dz := float64(b[0])-float64(a[0]), float64(b[2])-float64(a[2])
		px, pz := float64(p[0])-float64(a[0]), float64(p[2])-float64(a[2])
		length := math.Hypot(dx, dz)
		t := math.Max(0, math.Min(1, (px*dx+pz*dz)/(length*length)))
		ex, ez := px-t*dx, pz-t*dz
		dist := ex*ex + ez*ez
		if dist < best {
			best = dist
			station = line.stations[i] + t*length
			lateral = (dx*pz - dz*px) / length
		}
	}
	return
}

// 使用统一中线里程铺设墙面纹理；横向坐标从每对上下边界的低点起算。
func tunnelWallUVs(pos [][3]float32, line *tunnelCenterline, repeatU, repeatV float64) [][2]float32 {
	uv := make([][2]float32, len(pos))
	half := pairedTunnelWallHalf(pos)
	minY := math.Inf(1)
	for _, p := range pos {
		minY = math.Min(minY, float64(p[1]))
	}
	for i, p := range pos {
		s, _ := line.coordinates(p)
		height := float64(p[1]) - minY
		if half > 0 {
			other := 2*half - 1 - i
			if i == 2*half {
				other = 2*half - 1
			}
			height = float64(p[1]) - math.Min(float64(p[1]), float64(pos[other][1]))
		}
		uv[i] = [2]float32{float32(s / repeatU), float32(height / repeatV)}
	}
	return uv
}

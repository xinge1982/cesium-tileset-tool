package common

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/twpayne/go-geom"
)

// Endpoint matching uses metres in the shared source frame. Only logical
// end-to-start endpoints join, never planar interior intersections. Ambiguous
// branches terminate all incident chains rather than selecting an arbitrary arm.
type SidewallChainOptions struct {
	XYTolerance    float64
	ZTolerance     float64
	MaxTurnDegrees float64
}

// Sampling applies to each original LineString part, not each tile. Length and
// StartOffset use the same horizontal XZ metric as the guardrail sampler.
// Reverse affects stationing only: source geometry, normals and support sides
// retain their original orientation. IncludeEnd is false for chain interiors
// and for closed loops.
type LineChainSampling struct {
	StartOffset float64
	Length      float64
	Reverse     bool
	IncludeEnd  bool
	ChainID     string
}

type sidewallChainPart struct {
	feature, part                  int
	key, group                     string
	points                         [][3]float32
	start, end, incoming, outgoing [3]float32
	reverse, hasZ, closed          bool
	length                         float64
	next, prev                     int
}

type sidewallEndpointCell struct {
	group string
	x, z  int64
}

// Return shallow feature copies with independent sampling
// slices. It never modifies geometry, Fields, Features or database startoffset.
// Supply ALL source features before filtering a bound. Currently two-/three-wave
// barriers participate; other sidewall plugins can reuse the sampling contract.
func PrepareSidewallChains(features []LineFeature, coordinates *CoordinatePipeline, opt SidewallChainOptions) ([]LineFeature, error) {
	if coordinates == nil {
		return nil, fmt.Errorf("sidewall chains: nil coordinates")
	}
	if opt.XYTolerance == 0 {
		opt.XYTolerance = .05
	}
	if opt.ZTolerance == 0 {
		opt.ZTolerance = .05
	}
	if opt.MaxTurnDegrees == 0 {
		opt.MaxTurnDegrees = 60
	}
	if !chainFinite(opt.XYTolerance) || !chainFinite(opt.ZTolerance) || !chainFinite(opt.MaxTurnDegrees) || opt.XYTolerance <= 0 || opt.ZTolerance < 0 || opt.MaxTurnDegrees <= 0 || opt.MaxTurnDegrees > 180 {
		return nil, fmt.Errorf("invalid sidewall chain tolerances")
	}
	out := append([]LineFeature(nil), features...)
	parts := []sidewallChainPart{}
	for fi, f := range features {
		if f.BuildType != BuildTypeBarrierWave {
			continue
		}
		kind := fmt.Sprint(f.Fields["feature_type"])
		if kind == "<nil>" || kind == "" {
			kind = "wave_two"
		}
		if kind != "wave_two" && kind != "wave_three" {
			continue
		}
		lines, err := coordinates.ToLocalLine(f)
		if err != nil {
			return nil, fmt.Errorf("sidewall feature %d: %w", fi, err)
		}
		out[fi].ChainSampling = make([]LineChainSampling, len(lines))
		class, exists := f.Fields["bxdl"]
		if !exists || class == nil {
			class = f.Fields["type"]
		}
		// Separate missing classes from explicit values; retain construction dimensions.
		defaultBase, defaultPanel := defaultGuardrailRailHeight, defaultGuardrailRailHeightAll
		if kind == "wave_three" {
			defaultBase, defaultPanel = defaultGuardrailThreeWaveRailHeight, defaultGuardrailThreeWaveRailHeightAll
		}
		base, panel := guardrailRailHeights(f.Fields, defaultBase, defaultPanel)
		posts, err := resolveGuardrailPosts(f.GuardrailPosts, base, panel)
		if err != nil {
			return nil, err
		}
		direction := int64FromFields(f.Fields, "direction")
		group := fmt.Sprintf("%s|%v|%d|%g|%g|%g", kind, class, direction, base, panel, posts.Spacing)
		identity := f.Fields["id"]
		if identity == nil {
			identity = f.Fields["fid"]
		}
		for pi, line := range lines {
			points := dedupeLinePoints(line.Points)
			if len(points) < 2 {
				return nil, fmt.Errorf("sidewall feature %d part %d is degenerate", fi, pi)
			}
			for _, p := range points {
				for _, v := range p {
					if !chainFinite(float64(v)) {
						return nil, fmt.Errorf("sidewall feature %d has nonfinite coordinates", fi)
					}
				}
			}
			cum := polylineCumLengthsF32(points)
			length := float64(cum[len(cum)-1])
			if length <= 0 {
				return nil, fmt.Errorf("sidewall feature %d has zero horizontal length", fi)
			}
			data, _ := json.Marshal(points)
			hash := sha256.Sum256(data)
			p := sidewallChainPart{feature: fi, part: pi, key: fmt.Sprintf("%v/%08d/%x", identity, pi, hash), group: group, points: points, length: length, next: -1, prev: -1, reverse: direction != 2, hasZ: f.Geom.Layout() == geom.XYZ || f.Geom.Layout() == geom.XYZM}
			p.closed = points[0] == points[len(points)-1]
			p.start, p.end = points[0], points[len(points)-1]
			p.incoming = chainTangent(points, true)
			p.outgoing = chainTangent(points, false)
			if p.reverse {
				p.start, p.end = p.end, p.start
				p.incoming, p.outgoing = scale3(p.outgoing, -1), scale3(p.incoming, -1)
			}
			parts = append(parts, p)
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].key < parts[j].key })
	cell := func(p sidewallChainPart, point [3]float32) sidewallEndpointCell {
		return sidewallEndpointCell{p.group, int64(math.Floor(float64(point[0]) / opt.XYTolerance)), int64(math.Floor(float64(point[2]) / opt.XYTolerance))}
	}
	starts := map[sidewallEndpointCell][]int{}
	for i, p := range parts {
		if p.hasZ {
			k := cell(p, p.start)
			starts[k] = append(starts[k], i)
		}
	}
	candidates := make([][]int, len(parts))
	inCount := make([]int, len(parts))
	minDot := math.Cos(opt.MaxTurnDegrees * math.Pi / 180)
	for i, p := range parts {
		if !p.hasZ {
			continue
		}
		k := cell(p, p.end)
		for dx := int64(-1); dx <= 1; dx++ {
			for dz := int64(-1); dz <= 1; dz++ {
				for _, j := range starts[sidewallEndpointCell{k.group, k.x + dx, k.z + dz}] {
					q := parts[j]
					if i == j && !p.closed {
						continue
					}
					if i != j && (p.closed || q.closed) {
						continue
					}
					if math.Hypot(float64(p.end[0])-float64(q.start[0]), float64(p.end[2])-float64(q.start[2])) > opt.XYTolerance || math.Abs(float64(p.end[1])-float64(q.start[1])) > opt.ZTolerance {
						continue
					}
					if i != j && float64(p.outgoing[0])*float64(q.incoming[0])+float64(p.outgoing[2])*float64(q.incoming[2]) < minDot-1e-7 {
						continue
					}
					candidates[i] = append(candidates[i], j)
					inCount[j]++
				}
			}
		}
	}
	for i, cs := range candidates {
		if len(cs) == 1 && inCount[cs[0]] == 1 {
			j := cs[0]
			parts[i].next = j
			parts[j].prev = i
		}
	}
	visited := make([]bool, len(parts))
	walk := func(start int) {
		chain := []int{}
		i := start
		for i >= 0 && !visited[i] {
			visited[i] = true
			chain = append(chain, i)
			i = parts[i].next
		}
		closed := i == start || len(chain) == 1 && parts[start].closed
		offset := 0.0
		for ci, index := range chain {
			p := parts[index]
			out[p.feature].ChainSampling[p.part] = LineChainSampling{StartOffset: offset, Length: p.length, Reverse: p.reverse, IncludeEnd: !closed && ci == len(chain)-1, ChainID: parts[start].key}
			offset += p.length
		}
	}
	for i, p := range parts {
		if p.prev < 0 && !visited[i] {
			walk(i)
		}
	}
	// Remaining cycles begin at the stable smallest source ID/part/geometry key.
	for i := range parts {
		if !visited[i] {
			walk(i)
		}
	}
	return out, nil
}

func chainFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func chainTangent(points [][3]float32, start bool) [3]float32 {
	for j := 0; j < len(points)-1; j++ {
		i := j
		if !start {
			i = len(points) - 2 - j
		}
		dx, dz := float64(points[i+1][0])-float64(points[i][0]), float64(points[i+1][2])-float64(points[i][2])
		length := math.Hypot(dx, dz)
		if length > 1e-10 {
			return [3]float32{float32(dx / length), 0, float32(dz / length)}
		}
	}
	return [3]float32{}
}

func guardrailChainSupportTransforms(line LocalLine, sampling *LineChainSampling, configuredSpacing ...float32) ([]instancedTransform, error) {
	spacing := float64(defaultGuardrailPostSpacing)
	if len(configuredSpacing) > 0 {
		spacing = float64(configuredSpacing[0])
	}
	if !chainFinite(spacing) || spacing <= 0 {
		return nil, fmt.Errorf("invalid post spacing")
	}
	if sampling == nil {
		return guardrailSupportTransformsWithSpacing(line, float32(spacing))
	}
	if !chainFinite(sampling.StartOffset) || !chainFinite(sampling.Length) || sampling.StartOffset < 0 || sampling.Length <= 0 {
		return nil, fmt.Errorf("invalid chain stationing")
	}
	points := dedupeLinePoints(line.Points)
	if len(points) < 2 {
		return nil, fmt.Errorf("guardrail line requires two points")
	}
	cum := polylineCumLengthsF32(points)
	length := float64(cum[len(cum)-1])
	if math.Abs(length-sampling.Length) > 1e-5 {
		return nil, fmt.Errorf("chain geometry changed after preprocessing; prepare before slicing, never reuse on cut fragments")
	}
	// Float32 source positions accumulate small rounding errors. Use a tight
	// metre epsilon only for ownership at exact station boundaries.
	const eps = 1e-6
	first := math.Ceil((sampling.StartOffset-eps)/spacing)*spacing - sampling.StartOffset
	if first < 0 {
		first = 0
	}
	distances := []float64{}
	for k := 0; ; k++ {
		d := first + float64(k)*spacing
		if d >= length-eps {
			break
		}
		distances = append(distances, d)
	}
	if sampling.IncludeEnd {
		distances = append(distances, length)
	}
	out := make([]instancedTransform, 0, len(distances))
	for _, d := range distances {
		if sampling.Reverse {
			d = length - d
		}
		sample := samplePolylineAt(points, cum, float32(d))
		out = append(out, instancedTransform{Translation: sample.Point, Rotation: quaternionFromForward(sample.Forward), Scale: [3]float32{1, 1, 1}})
	}
	return out, nil
}

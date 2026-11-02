package road

import "github.com/twpayne/go-geom"

// Select complete original geometries that intersect the project range.
// Chains are prepared before this selection; clipping remains a mesh operation.
func roadGeometryIntersectsBound(g geom.T, b [4]float64) bool {
	switch v := g.(type) {
	case *geom.LineString:
		return roadLineIntersectsBound(v.Coords(), b)
	case *geom.MultiLineString:
		for _, line := range v.Coords() {
			if roadLineIntersectsBound(line, b) {
				return true
			}
		}
	case *geom.Polygon:
		return roadPolygonIntersectsBound(v.Coords(), b)
	case *geom.MultiPolygon:
		for _, polygon := range v.Coords() {
			if roadPolygonIntersectsBound(polygon, b) {
				return true
			}
		}
	}
	return false
}

func roadLineIntersectsBound(line []geom.Coord, b [4]float64) bool {
	for i, p := range line {
		if p[0] >= b[0] && p[0] <= b[2] && p[1] >= b[1] && p[1] <= b[3] {
			return true
		}
		if i == 0 {
			continue
		}
		a := line[i-1]
		// Slab intersection includes contact with edges and corners.
		lo, hi := 0., 1.
		hit := true
		for axis := 0; axis < 2; axis++ {
			d := p[axis] - a[axis]
			if d == 0 {
				if a[axis] < b[axis] || a[axis] > b[axis+2] {
					hit = false
					break
				}
				continue
			}
			u, v := (b[axis]-a[axis])/d, (b[axis+2]-a[axis])/d
			if u > v {
				u, v = v, u
			}
			if u > lo {
				lo = u
			}
			if v < hi {
				hi = v
			}
			if lo > hi {
				hit = false
				break
			}
		}
		if hit {
			return true
		}
	}
	return false
}

func roadPolygonIntersectsBound(rings [][]geom.Coord, b [4]float64) bool {
	if len(rings) == 0 {
		return false
	}
	for _, ring := range rings {
		if roadLineIntersectsBound(ring, b) {
			return true
		}
	}
	// With no boundary crossing, one rectangle corner determines containment.
	if !roadRingContainsXY(rings[0], b[0], b[1]) {
		return false
	}
	for _, hole := range rings[1:] {
		if roadRingContainsXY(hole, b[0], b[1]) {
			return false
		}
	}
	return true
}

func roadRingContainsXY(ring []geom.Coord, x, y float64) bool {
	inside := false
	for i, a := range ring {
		b := ring[(i+1)%len(ring)]
		if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}

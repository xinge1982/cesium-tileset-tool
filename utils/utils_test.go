package utils

import (
	"github.com/magiconair/properties/assert"
	"github.com/paulmach/orb"
	"math"
	"testing"
)

func TestLineIntersection(t *testing.T) {
	line1 := orb.LineString{orb.Point{0, 0}, orb.Point{2, 2}}
	line2 := orb.LineString{orb.Point{2, 0}, orb.Point{0, 2}}
	inter := LineIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, inter, 1)

	line1 = orb.LineString{orb.Point{0, 0}, orb.Point{2, 2}}
	line2 = orb.LineString{orb.Point{1, 1}, orb.Point{0, 2}}
	inter = LineIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, inter, 1)

	line1 = orb.LineString{orb.Point{0, 0}, orb.Point{5, 5}}
	line2 = orb.LineString{orb.Point{10, 5}, orb.Point{12, 5}}
	inter = LineIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, inter, 0)

	line1 = orb.LineString{orb.Point{519090.5504, 3340536.0226000026}, orb.Point{519090.51571061125, 3340539.804360083}}
	line2 = orb.LineString{orb.Point{519045.3078368272, 3340536.847525914}, orb.Point{519044.42761386035, 3340536.7952644723}}
	region := regionIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, region, 0)
	inter = LineIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, inter, 0)

	line1 = orb.LineString{orb.Point{519090.5504, 3340536.0226000026}, orb.Point{519090.51571061125, 3340539.804360083}}
	line2 = orb.LineString{orb.Point{519080.3078368272, 3340537.0226000026}, orb.Point{519090.9504, 3340537.0226000026}}
	region = regionIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, region, 1)
	inter = LineIntersection(line1[0], line1[1], line2[0], line2[1])
	assert.Equal(t, inter, 1)

}

func TestGetNetworkFloatBytes(t *testing.T) {
	v := -1.0
	var b = make([]byte, 2)
	n, f := math.Modf(float64(v))
	f, _ = math.Modf(f * 100)
	b[0] = byte(f)
	b[1] = byte(n)
	assert.Equal(t, b[0], 0)
	assert.Equal(t, b[1], 256)
}

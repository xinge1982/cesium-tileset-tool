package utm

import (
	"math"

	"github.com/paulmach/orb"
)

var D2R = 0.01745329251994329577

var R2D = 57.29577951308232088

var FORTPI = math.Pi / 4

var TWO_PI = math.Pi * 2

// SPI is slightly greater than Math.PI, so values that exceed the -180..180
// degree range by a tiny amount don't get wrapped. This prevents points that
// have drifted from their original location along the 180th meridian (due to
// floating point error) from changing their sign.
var SPI = 3.14159265359

type CRSWgs84utm struct {
}

type Wgs84utmProjection struct {
	projName     string    `json:"projName"`
	zone         int       `json:"zone"`
	datumCode    string    `json:"datumCode"`
	units        string    `json:"units"`
	no_defs      bool      `json:"no_defs"`
	datum_params []string  `json:"datum_params"`
	ellps        string    `json:"ellps"`
	datumName    string    `json:"datumName"`
	k0           float64   `json:"k0"`
	axis         string    `json:"axis"`
	names        []string  `json:"names"`
	dependsOn    string    `json:"dependsOn"`
	a            float64   `json:"a"`
	b            float64   `json:"b"`
	rf           float64   `json:"rf"`
	es           float64   `json:"es"`
	e            float64   `json:"e"`
	ep2          float64   `json:"ep2"`
	datum        Datum     `json:"datum"`
	lat0         float64   `json:"lat0"`
	long0        float64   `json:"long0"`
	x0           float64   `json:"x0"`
	y0           float64   `json:"y0"`
	cgb          []float64 `json:"cgb"`
	cbg          []float64 `json:"cbg"`
	utg          []float64 `json:"utg"`
	gtu          []float64 `json:"gtu"`
	Qn           float64   `json:"Qn"`
	Zb           float64   `json:"Zb"`
}
type Datum struct {
	datum_type   int     `json:"datum_type"`
	datum_params []int   `json:"datum_params"`
	a            int     `json:"a"`
	b            float64 `json:"b"`
	es           float64 `json:"es"`
	ep2          float64 `json:"ep2"`
}

var Infinity float64 = 0

type Coordinate struct {
	x float64
	y float64
	z float64
}

var projMap = map[string]MyProjection{
	"epsg:32643": Epsg32643,
	"epsg:32644": Epsg32644,
	"epsg:32645": Epsg32645,
	"epsg:32646": Epsg32646,
	"epsg:32647": Epsg32647,
	"epsg:32648": Epsg32648,
	"epsg:32649": Epsg32649,
	"epsg:32650": Epsg32650,
	"epsg:32651": Epsg32651,
	"epsg:32652": Epsg32652,
	"epsg:32653": Epsg32653,
}

func (crs CRSWgs84utm) GetProjection(proj string) MyProjection {
	if m, ok := projMap[proj]; ok {
		return m
	}
	return nil
}

// 转换到wgs84经纬度
func (pj Wgs84utmProjection) ToWgs84(x, y float64) orb.Point {
	point := pj.inverse(Coordinate{x: x, y: y})
	point = Coordinate{
		x: point.x * R2D,
		y: point.y * R2D,
		z: 0,
	}
	return orb.Point{
		point.x,
		point.y,
	}
}

// 从wgs84经纬度转换
func (pj Wgs84utmProjection) FromWgs84(x, y float64) orb.Point {
	convp := Coordinate{
		x: x * D2R,
		y: y * D2R,
		z: 0,
	}
	point := pj.forward(Coordinate{x: convp.x, y: convp.y})
	return orb.Point{
		point.x,
		point.y,
	}
}

func (this *Wgs84utmProjection) inverse(p Coordinate) Coordinate {
	var Ce = (p.x - this.x0) * (1 / this.a)
	var Cn = (p.y - this.y0) * (1 / this.a)
	Cn = (Cn - this.Zb) / this.Qn
	Ce = Ce / this.Qn
	var lon float64
	var lat float64
	if math.Abs(Ce) <= 2.623395162778 {
		var tmp = clens_cmplx(this.utg, 2*Cn, 2*Ce)
		Cn = Cn + tmp[0]
		Ce = Ce + tmp[1]
		Ce = math.Atan(sinh(Ce))
		var sin_Cn = math.Sin(Cn)
		var cos_Cn = math.Cos(Cn)
		var sin_Ce = math.Sin(Ce)
		var cos_Ce = math.Cos(Ce)
		Cn = math.Atan2(sin_Cn*cos_Ce, hypot(sin_Ce, cos_Ce*cos_Cn))
		Ce = math.Atan2(sin_Ce, cos_Ce*cos_Cn)
		lon = adjust_lon(Ce + this.long0)
		lat = gatg(this.cgb, Cn)
	} else {
		lon = Infinity
		lat = Infinity
	}

	p.x = lon
	p.y = lat
	return p
}

func (this *Wgs84utmProjection) forward(p Coordinate) Coordinate {
	var Ce = adjust_lon(p.x - this.long0)
	var Cn = p.y

	Cn = gatg(this.cbg, Cn)
	var sin_Cn = math.Sin(Cn)
	var cos_Cn = math.Cos(Cn)
	var sin_Ce = math.Sin(Ce)
	var cos_Ce = math.Cos(Ce)

	Cn = math.Atan2(sin_Cn, cos_Ce*cos_Cn)
	Ce = math.Atan2(sin_Ce*cos_Cn, hypot(sin_Cn, cos_Cn*cos_Ce))
	Ce = asinhy(math.Tan(Ce))

	var tmp = clens_cmplx(this.gtu, 2*Cn, 2*Ce)

	Cn = Cn + tmp[0]
	Ce = Ce + tmp[1]

	var x float64
	var y float64

	if math.Abs(Ce) <= 2.623395162778 {
		x = this.a*(this.Qn*Ce) + this.x0
		y = this.a*(this.Qn*Cn+this.Zb) + this.y0
	} else {
		x = Infinity
		y = Infinity
	}

	p.x = x
	p.y = y
	return p
}

func asinhy(x float64) float64 {
	var y = math.Abs(x)
	y = log1py(y * (1 + y/(hypot(1, y)+1)))

	if x < 0 {
		return -y
	} else {
		return y
	}
}

func log1py(x float64) float64 {
	var y = 1 + x
	var z = y - 1

	if z == 0 {
		return x
	} else {
		return x * math.Log(y) / z
	}
}

func hypot(x float64, y float64) float64 {
	x = math.Abs(x)
	y = math.Abs(y)
	var a = math.Max(x, y)
	var b = math.Min(x, y)
	if a != 0 {
		b = math.Min(x, y) / a
	}
	return a * math.Sqrt(1+math.Pow(b, 2))
}

func gatg(pp []float64, B float64) float64 {
	var cos_2B = 2 * math.Cos(2*B)
	var i = len(pp) - 1
	var h1 = pp[i]
	var h2 float64 = 0
	var h float64

	i -= 1
	for i >= 0 {
		h = -h2 + cos_2B*h1 + pp[i]
		h2 = h1
		h1 = h
		i -= 1
	}

	return (B + h*math.Sin(2*B))
}

func adjust_lon(x float64) float64 {
	if math.Abs(x) <= SPI {
		return x
	} else {
		return (x - (sign(x) * TWO_PI))
	}
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	} else {
		return 1
	}
}

func clens_cmplx(pp []float64, arg_r, arg_i float64) []float64 {
	var sin_arg_r = math.Sin(arg_r)
	var cos_arg_r = math.Cos(arg_r)
	var sinh_arg_i = sinh(arg_i)
	var cosh_arg_i = cosh(arg_i)
	var r = 2 * cos_arg_r * cosh_arg_i
	var i = -2 * sin_arg_r * sinh_arg_i
	var j = len(pp) - 1
	var hr = pp[j]
	var hi1 float64 = 0
	var hr1 float64 = 0
	var hi float64 = 0
	var hr2 float64
	var hi2 float64
	j -= 1
	for j >= 0 {
		hr2 = hr1
		hi2 = hi1
		hr1 = hr
		hi1 = hi
		hr = -hr2 + r*hr1 - i*hi1 + pp[j]
		hi = -hi2 + i*hr1 + r*hi1
		j -= 1
	}

	r = sin_arg_r * cosh_arg_i
	i = cos_arg_r * sinh_arg_i
	return []float64{r*hr - i*hi, r*hi + i*hr}
}

func sinh(x float64) float64 {
	var r = math.Exp(x)
	r = (r - 1/r) / 2
	return r
}

func cosh(x float64) float64 {
	var r = math.Exp(x)
	r = (r + 1/r) / 2
	return r
}

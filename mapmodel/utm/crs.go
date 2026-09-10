package utm

import "github.com/paulmach/orb"

type MyCRS interface {
	GetProjection(proj string) MyProjection
}

type MyProjection interface {
	ToWgs84(x, y float64) orb.Point
	FromWgs84(x, y float64) orb.Point
}

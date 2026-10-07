package utm

type MyCRS interface {
	GetProjection(proj string) MyProjection
}

type MyProjection interface {
	ToWgs84(x, y float64) []float64
	FromWgs84(x, y float64) []float64
}

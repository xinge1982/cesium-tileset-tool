package main

import (
	"cesium-tileset-tool/map3d/road"
	"flag"
	"log"
)

func main() {
	var (
		glbPath        string
		outPath        string
		contentURI     string
		lon            float64
		lat            float64
		height         float64
		geometricError float64
		paddingXY      float64
		paddingY       float64
	)

	flag.StringVar(&glbPath, "glb", "", "input glb path")
	flag.StringVar(&outPath, "out", "", "output tileset.json path")
	flag.StringVar(&contentURI, "uri", "", "content uri in tileset.json, defaults to glb filename")
	flag.Float64Var(&lon, "lon", 0, "tileset base longitude")
	flag.Float64Var(&lat, "lat", 0, "tileset base latitude")
	flag.Float64Var(&height, "height", 0, "tileset base height")
	flag.Float64Var(&geometricError, "error", 100, "tileset geometric error")
	flag.Float64Var(&paddingXY, "padxy", 5, "local x/z half-axis padding in meters")
	flag.Float64Var(&paddingY, "pady", 2, "local y half-axis padding in meters")
	flag.Parse()

	if err := road.WriteTilesetJSONForGLB(road.TilesetBuildOptions{
		GLBPath:        glbPath,
		OutputPath:     outPath,
		ContentURI:     contentURI,
		BasePoint:      []float64{lon, lat, height},
		GeometricError: geometricError,
		PaddingXY:      paddingXY,
		PaddingY:       paddingY,
	}); err != nil {
		log.Fatal(err)
	}
}

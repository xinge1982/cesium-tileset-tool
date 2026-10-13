package road

import (
	"math"
	"os"

	"github.com/twpayne/go-geom/encoding/geojson"
)

func calcTileContextFromFeatureCollection(fc *geojson.FeatureCollection) ([]float64, [6]float64, error) {
	return calcTileContextFromFeatureCollections(fc)
}

func calcTileContextFromFeatureCollections(collections ...*geojson.FeatureCollection) ([]float64, [6]float64, error) {
	var region [6]float64
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)

	for _, fc := range collections {
		if fc == nil {
			continue
		}
		for i := range fc.Features {
			if fc.Features[i] == nil {
				continue
			}
			g := fc.Features[i].Geometry
			if g == nil {
				continue
			}
			flat := g.FlatCoords()
			stride := g.Stride()
			if stride < 2 {
				continue
			}
			for j := 0; j+stride-1 < len(flat); j += stride {
				lon := flat[j]
				lat := flat[j+1]
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
	}
	if math.IsInf(minLon, 0) || math.IsInf(minLat, 0) || math.IsInf(maxLon, 0) || math.IsInf(maxLat, 0) {
		return nil, region, os.ErrInvalid
	}

	basePoint := []float64{(minLon + maxLon) / 2, (minLat + maxLat) / 2, 0}
	region = [6]float64{minLon, minLat, maxLon, maxLat, 0, 0}
	return basePoint, region, nil
}

// RoadSurfaceFeaturesFromGeoJSON converts polygons with the existing road material and UV defaults.
func RoadSurfaceFeaturesFromGeoJSON(fc *geojson.FeatureCollection, texturePath string) []SurfaceFeature {
	if fc == nil {
		return nil
	}
	features := make([]SurfaceFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i] == nil || fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		fields["feature_type"] = "surface"
		features = append(features, SurfaceFeature{
			FeatureInput: FeatureInput{
				BuildType: BuildTypeRoadSurface,
				Geom:      fc.Features[i].Geometry,
				Fields:    fields,
				Features:  featureRowsFromGeoJSON(fc.Features[i].Properties, "road_surface"),
			},
			Thickness: 0,
			Material: MaterialSet{
				Top:    MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Side:   MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
				Bottom: MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true},
			},
			UV: UVOptions{Mapping: UVMappingPlanar, RepeatX: 8, RepeatY: 8, FlipV: true, ScaleByMeters: true},
		})
	}
	return features
}

// RoadCenterlineFeaturesFromGeoJSON copies properties and normalizes the ldid road reference.
func RoadCenterlineFeaturesFromGeoJSON(fc *geojson.FeatureCollection) []CenterlineFeature {
	if fc == nil {
		return nil
	}
	features := make([]CenterlineFeature, 0, len(fc.Features))
	for i := range fc.Features {
		if fc.Features[i] == nil || fc.Features[i].Geometry == nil {
			continue
		}
		fields := featureFieldsFromGeoJSON(fc.Features[i].Properties)
		// Some line datasets store the related road ID in `ldid`.
		// Normalize it so the projector can actually associate the reference line with the road.
		if featureFieldInt64(fields, "hroad_id") == 0 && featureFieldInt64(fields, "road_id") == 0 {
			if ldid := featureFieldInt64(fields, "ldid"); ldid != 0 {
				fields["hroad_id"] = ldid
			}
		}
		features = append(features, CenterlineFeature{
			Geom:   fc.Features[i].Geometry,
			Fields: fields,
		})
	}
	return features
}

func featureFieldsFromGeoJSON(props map[string]interface{}) FeatureFields {
	fields := make(FeatureFields, len(props))
	for k, v := range props {
		fields[k] = v
	}
	if _, ok := fields["id"]; !ok {
		fields["id"] = ""
	}
	if _, ok := fields["name"]; !ok {
		fields["name"] = ""
	}
	return fields
}

func featureRowsFromGeoJSON(props map[string]interface{}, typeName string) []FeatureFields {
	fields := featureFieldsFromGeoJSON(props)
	return []FeatureFields{
		{
			"id":   fields["id"],
			"type": typeName,
		},
	}
}

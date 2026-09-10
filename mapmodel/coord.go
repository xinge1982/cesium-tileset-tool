package mapmodel

import (
	"math"
)

// ECEF to WGS84 Conversion constants
const WGS84_A = 6378137.0         // Semi-major axis in meters
const WGS84_E = 0.081819190842622 // Eccentricity of the WGS84 ellipsoid

// Extract position (translation) from a model matrix
func ExtractTranslation(modelMatrix Mat4) (x, y, z float64) {
	return modelMatrix[12], modelMatrix[13], modelMatrix[14]
}

// Convert ECEF (x, y, z) to WGS84 latitude, longitude, altitude
func ECEFToWGS84(x, y, z float64) (latitude, longitude, altitude float64) {
	// Calculate longitude
	longitude = math.Atan2(y, x)

	// Initial calculation for latitude and altitude
	// Assume a flat earth at first, for a more efficient starting point
	p := math.Sqrt(x*x + y*y)
	theta := math.Atan2(z*WGS84_A, p*WGS84_A*(1-WGS84_E*WGS84_E))

	// Compute the latitude and altitude
	latitude = math.Atan2(z+WGS84_E*WGS84_E*WGS84_A*math.Sin(theta)*math.Sin(theta), p)
	altitude = p/math.Cos(latitude) - WGS84_A*math.Sqrt(1-WGS84_E*WGS84_E*math.Sin(latitude)*math.Sin(latitude))

	// Convert latitude and longitude to degrees
	latitude = latitude * 180 / math.Pi
	longitude = longitude * 180 / math.Pi

	return latitude, longitude, altitude
}

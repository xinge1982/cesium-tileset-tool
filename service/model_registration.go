package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type registrationGeoPoint struct {
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	Height    float64 `json:"height"`
}

type registrationPointPair struct {
	ID          string               `json:"id"`
	ModelPoint  registrationGeoPoint `json:"modelPoint"`
	TargetPoint registrationGeoPoint `json:"targetPoint"`
}

type modelRegistrationSolveRequest struct {
	Center registrationGeoPoint       `json:"center"`
	Points []registrationPointPair    `json:"points"`
}

type registrationTranslation struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type registrationQuaternion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
	W float64 `json:"w"`
}

type registrationResidual struct {
	ID       string  `json:"id"`
	ErrorX   float64 `json:"errorX"`
	ErrorY   float64 `json:"errorY"`
	ErrorZ   float64 `json:"errorZ"`
	Distance float64 `json:"distance"`
}

type modelRegistrationResult struct {
	Translation registrationTranslation `json:"translation"`
	Quaternion  registrationQuaternion  `json:"quaternion"`
	RMSError    float64                 `json:"rmsError"`
	MaxError    float64                 `json:"maxError"`
	Residuals   []registrationResidual  `json:"residuals"`
}

type modelRegistrationConfirmRequest struct {
	Key       string             `json:"key"`
	Transform map[string]float64 `json:"transform"`
}

type modelRegistrationController struct {
	searchService *TilesetSourceSearchService
}

func newModelRegistrationController(
	searchService *TilesetSourceSearchService,
) *modelRegistrationController {
	return &modelRegistrationController{searchService: searchService}
}

func (controller *modelRegistrationController) Solve(ctx *gin.Context) {
	if controller.searchService.conn == nil ||
		controller.searchService.conn.DB == nil ||
		!controller.searchService.conn.Ready {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "database not ready"})
		return
	}

	var request modelRegistrationSolveRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	result, err := solveModelRegistration(request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": result})
}

func (controller *modelRegistrationController) Confirm(ctx *gin.Context) {
	if controller.searchService.conn == nil ||
		controller.searchService.conn.DB == nil ||
		!controller.searchService.conn.Ready {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "database not ready"})
		return
	}

	var request modelRegistrationConfirmRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	err := controller.searchService.SaveModelRegistration(
		ctx.Request.Context(),
		ctx.Param("tilesetKey"),
		ctx.Param("sourceId"),
		request,
	)
	if err != nil {
		writeSearchError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{"updated": true},
	})
}

type registrationVector [3]float64

func solveModelRegistration(
	request modelRegistrationSolveRequest,
) (*modelRegistrationResult, error) {
	if len(request.Points) < 3 {
		return nil, errors.New("at least 3 control point pairs are required")
	}
	if err := validateRegistrationGeoPoint(request.Center); err != nil {
		return nil, fmt.Errorf("invalid center: %w", err)
	}

	sources := make([]registrationVector, len(request.Points))
	targets := make([]registrationVector, len(request.Points))
	ids := make(map[string]struct{}, len(request.Points))
	frame := newRegistrationENUFrame(request.Center)
	for index, pair := range request.Points {
		id := strings.TrimSpace(pair.ID)
		if id == "" {
			return nil, fmt.Errorf("control point %d has an empty id", index+1)
		}
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("duplicate control point id %q", id)
		}
		ids[id] = struct{}{}
		if err := validateRegistrationGeoPoint(pair.ModelPoint); err != nil {
			return nil, fmt.Errorf("control point %q model point: %w", id, err)
		}
		if err := validateRegistrationGeoPoint(pair.TargetPoint); err != nil {
			return nil, fmt.Errorf("control point %q target point: %w", id, err)
		}
		sources[index] = frame.offset(pair.ModelPoint)
		targets[index] = frame.offset(pair.TargetPoint)
	}
	if !registrationPointsAreNonCollinear(sources) {
		return nil, errors.New("model control points must contain at least 3 non-collinear points")
	}
	if !registrationPointsAreNonCollinear(targets) {
		return nil, errors.New("target control points must contain at least 3 non-collinear points")
	}

	rotation, quaternion, translation := registrationRigidTransform(sources, targets)
	residuals := make([]registrationResidual, len(request.Points))
	var squaredError, maxError float64
	for index := range sources {
		rotated := registrationMulMatrixVector(rotation, sources[index])
		errorVector := registrationVector{
			rotated[0] + translation[0] - targets[index][0],
			rotated[1] + translation[1] - targets[index][1],
			rotated[2] + translation[2] - targets[index][2],
		}
		distance := registrationNorm(errorVector)
		squaredError += distance * distance
		if distance > maxError {
			maxError = distance
		}
		residuals[index] = registrationResidual{
			ID:       strings.TrimSpace(request.Points[index].ID),
			ErrorX:   errorVector[0],
			ErrorY:   errorVector[1],
			ErrorZ:   errorVector[2],
			Distance: distance,
		}
	}

	return &modelRegistrationResult{
		Translation: registrationTranslation{
			X: translation[0],
			Y: translation[1],
			Z: translation[2],
		},
		Quaternion: registrationQuaternion{
			X: quaternion[0],
			Y: quaternion[1],
			Z: quaternion[2],
			W: quaternion[3],
		},
		RMSError:  math.Sqrt(squaredError / float64(len(sources))),
		MaxError:  maxError,
		Residuals: residuals,
	}, nil
}

func validateRegistrationGeoPoint(point registrationGeoPoint) error {
	if !registrationFinite(point.Longitude) ||
		!registrationFinite(point.Latitude) ||
		!registrationFinite(point.Height) {
		return errors.New("coordinates must be finite")
	}
	if point.Longitude < -180 || point.Longitude > 180 ||
		point.Latitude < -90 || point.Latitude > 90 {
		return errors.New("longitude or latitude is outside WGS84 bounds")
	}
	return nil
}

func registrationFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

type registrationENUFrame struct {
	x, y, z                 float64
	sinLon, cosLon          float64
	sinLatitude, cosLatitude float64
}

func newRegistrationENUFrame(origin registrationGeoPoint) registrationENUFrame {
	lon := origin.Longitude * math.Pi / 180
	lat := origin.Latitude * math.Pi / 180
	x, y, z := registrationGeodeticToECEF(lon, lat, origin.Height)
	return registrationENUFrame{
		x:           x,
		y:           y,
		z:           z,
		sinLon:      math.Sin(lon),
		cosLon:      math.Cos(lon),
		sinLatitude: math.Sin(lat),
		cosLatitude: math.Cos(lat),
	}
}

func (frame registrationENUFrame) offset(point registrationGeoPoint) registrationVector {
	lon := point.Longitude * math.Pi / 180
	lat := point.Latitude * math.Pi / 180
	x, y, z := registrationGeodeticToECEF(lon, lat, point.Height)
	dx, dy, dz := x-frame.x, y-frame.y, z-frame.z
	return registrationVector{
		-frame.sinLon*dx + frame.cosLon*dy,
		-frame.sinLatitude*frame.cosLon*dx - frame.sinLatitude*frame.sinLon*dy + frame.cosLatitude*dz,
		frame.cosLatitude*frame.cosLon*dx + frame.cosLatitude*frame.sinLon*dy + frame.sinLatitude*dz,
	}
}

func registrationGeodeticToECEF(lon, lat, height float64) (float64, float64, float64) {
	const semiMajor = 6378137.0
	const eccentricitySquared = 6.69437999014e-3
	sinLatitude := math.Sin(lat)
	normal := semiMajor / math.Sqrt(1-eccentricitySquared*sinLatitude*sinLatitude)
	return (normal + height) * math.Cos(lat) * math.Cos(lon),
		(normal + height) * math.Cos(lat) * math.Sin(lon),
		(normal*(1-eccentricitySquared) + height) * sinLatitude
}

func registrationPointsAreNonCollinear(points []registrationVector) bool {
	if len(points) < 3 {
		return false
	}
	var longest registrationVector
	var longestSquared float64
	for index := 1; index < len(points); index++ {
		candidate := registrationSubtract(points[index], points[0])
		lengthSquared := registrationDot(candidate, candidate)
		if lengthSquared > longestSquared {
			longest = candidate
			longestSquared = lengthSquared
		}
	}
	if longestSquared < 1e-12 {
		return false
	}
	for index := 1; index < len(points); index++ {
		candidate := registrationSubtract(points[index], points[0])
		cross := registrationCross(longest, candidate)
		if registrationDot(cross, cross) > longestSquared*longestSquared*1e-12 {
			return true
		}
	}
	return false
}

func registrationRigidTransform(
	sources, targets []registrationVector,
) ([3][3]float64, [4]float64, registrationVector) {
	sourceCenter := registrationCentroid(sources)
	targetCenter := registrationCentroid(targets)
	var covariance [3][3]float64
	for index := range sources {
		p := registrationSubtract(sources[index], sourceCenter)
		q := registrationSubtract(targets[index], targetCenter)
		for row := 0; row < 3; row++ {
			for column := 0; column < 3; column++ {
				covariance[row][column] += p[row] * q[column]
			}
		}
	}
	quaternion := registrationQuaternionFromCovariance(covariance)
	rotation := registrationRotationFromQuaternion(quaternion)
	rotatedCenter := registrationMulMatrixVector(rotation, sourceCenter)
	translation := registrationSubtract(targetCenter, rotatedCenter)
	return rotation, quaternion, translation
}

func registrationQuaternionFromCovariance(s [3][3]float64) [4]float64 {
	trace := s[0][0] + s[1][1] + s[2][2]
	matrix := [4][4]float64{
		{trace, s[1][2] - s[2][1], s[2][0] - s[0][2], s[0][1] - s[1][0]},
		{s[1][2] - s[2][1], s[0][0] - s[1][1] - s[2][2], s[0][1] + s[1][0], s[2][0] + s[0][2]},
		{s[2][0] - s[0][2], s[0][1] + s[1][0], -s[0][0] + s[1][1] - s[2][2], s[1][2] + s[2][1]},
		{s[0][1] - s[1][0], s[2][0] + s[0][2], s[1][2] + s[2][1], -s[0][0] - s[1][1] + s[2][2]},
	}
	eigenvalues, eigenvectors := registrationJacobiEigen(matrix)
	maxIndex := 0
	for index := 1; index < 4; index++ {
		if eigenvalues[index] > eigenvalues[maxIndex] {
			maxIndex = index
		}
	}
	quaternion := [4]float64{
		eigenvectors[1][maxIndex],
		eigenvectors[2][maxIndex],
		eigenvectors[3][maxIndex],
		eigenvectors[0][maxIndex],
	}
	norm := math.Sqrt(quaternion[0]*quaternion[0] + quaternion[1]*quaternion[1] + quaternion[2]*quaternion[2] + quaternion[3]*quaternion[3])
	for index := range quaternion {
		quaternion[index] /= norm
	}
	if quaternion[3] < 0 {
		for index := range quaternion {
			quaternion[index] = -quaternion[index]
		}
	}
	return quaternion
}

func registrationJacobiEigen(matrix [4][4]float64) ([4]float64, [4][4]float64) {
	vectors := [4][4]float64{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
	for iteration := 0; iteration < 64; iteration++ {
		p, q := 0, 1
		largest := math.Abs(matrix[p][q])
		for row := 0; row < 4; row++ {
			for column := row + 1; column < 4; column++ {
				if value := math.Abs(matrix[row][column]); value > largest {
					largest, p, q = value, row, column
				}
			}
		}
		if largest < 1e-12 {
			break
		}
		angle := 0.5 * math.Atan2(2*matrix[p][q], matrix[q][q]-matrix[p][p])
		cosine, sine := math.Cos(angle), math.Sin(angle)
		for index := 0; index < 4; index++ {
			if index == p || index == q {
				continue
			}
			left, right := matrix[index][p], matrix[index][q]
			matrix[index][p] = cosine*left - sine*right
			matrix[p][index] = matrix[index][p]
			matrix[index][q] = sine*left + cosine*right
			matrix[q][index] = matrix[index][q]
		}
		pp, qq, pq := matrix[p][p], matrix[q][q], matrix[p][q]
		matrix[p][p] = cosine*cosine*pp - 2*sine*cosine*pq + sine*sine*qq
		matrix[q][q] = sine*sine*pp + 2*sine*cosine*pq + cosine*cosine*qq
		matrix[p][q], matrix[q][p] = 0, 0
		for row := 0; row < 4; row++ {
			left, right := vectors[row][p], vectors[row][q]
			vectors[row][p] = cosine*left - sine*right
			vectors[row][q] = sine*left + cosine*right
		}
	}
	return [4]float64{matrix[0][0], matrix[1][1], matrix[2][2], matrix[3][3]}, vectors
}

func registrationRotationFromQuaternion(q [4]float64) [3][3]float64 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	return [3][3]float64{
		{1 - 2*(y*y+z*z), 2 * (x*y - z*w), 2 * (x*z + y*w)},
		{2 * (x*y + z*w), 1 - 2*(x*x+z*z), 2 * (y*z - x*w)},
		{2 * (x*z - y*w), 2 * (y*z + x*w), 1 - 2*(x*x+y*y)},
	}
}

func registrationCentroid(points []registrationVector) registrationVector {
	var center registrationVector
	for _, point := range points {
		center[0] += point[0]
		center[1] += point[1]
		center[2] += point[2]
	}
	for index := range center {
		center[index] /= float64(len(points))
	}
	return center
}

func registrationMulMatrixVector(matrix [3][3]float64, vector registrationVector) registrationVector {
	return registrationVector{
		matrix[0][0]*vector[0] + matrix[0][1]*vector[1] + matrix[0][2]*vector[2],
		matrix[1][0]*vector[0] + matrix[1][1]*vector[1] + matrix[1][2]*vector[2],
		matrix[2][0]*vector[0] + matrix[2][1]*vector[1] + matrix[2][2]*vector[2],
	}
}

func registrationSubtract(left, right registrationVector) registrationVector {
	return registrationVector{left[0] - right[0], left[1] - right[1], left[2] - right[2]}
}

func registrationDot(left, right registrationVector) float64 {
	return left[0]*right[0] + left[1]*right[1] + left[2]*right[2]
}

func registrationCross(left, right registrationVector) registrationVector {
	return registrationVector{
		left[1]*right[2] - left[2]*right[1],
		left[2]*right[0] - left[0]*right[2],
		left[0]*right[1] - left[1]*right[0],
	}
}

func registrationNorm(vector registrationVector) float64 {
	return math.Sqrt(registrationDot(vector, vector))
}

func (service *TilesetSourceSearchService) SaveModelRegistration(
	ctx context.Context,
	tilesetKey, sourceID string,
	request modelRegistrationConfirmRequest,
) error {
	tileset, exists := service.tilesets[tilesetKey]
	if !exists {
		return fmt.Errorf("tileset %q does not exist", tilesetKey)
	}
	if !tileset.Enabled {
		return fmt.Errorf("tileset %q is disabled", tilesetKey)
	}
	if !tileset.Maintainable {
		return fmt.Errorf("tileset %q is not maintainable", tilesetKey)
	}
	source, err := findSource(tileset.Sources, sourceID)
	if err != nil {
		return err
	}
	if err := validateSourceConfig(source); err != nil {
		return fmt.Errorf("invalid source configuration: %w", err)
	}
	key := strings.TrimSpace(request.Key)
	if key == "" {
		return errors.New("primary key is required")
	}
	if len(request.Transform) != 16 {
		return errors.New("registration transform must contain 16 values")
	}
	for index := 0; index < 16; index++ {
		value, exists := request.Transform[fmt.Sprintf("%d", index)]
		if !exists {
			return fmt.Errorf("registration transform is missing index %d", index)
		}
		if !registrationFinite(value) {
			return errors.New("registration transform contains a non-finite value")
		}
	}
	transformJSON, err := json.Marshal(request.Transform)
	if err != nil {
		return fmt.Errorf("encode registration transform: %w", err)
	}
	tableSQL := qualifiedTableName(source.Table.Schema, source.Table.Name)
	primaryKeySQL := quoteIdentifier(source.Table.PrimaryKey)

	return service.conn.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		alterSQL := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s JSONB",
			tableSQL,
			quoteIdentifier("metadata"),
		)
		if err := tx.Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("auto-create column %q: %w", "metadata", err)
		}

		updateSQL := fmt.Sprintf(
			`UPDATE %s
			    SET %s = jsonb_set(
			        CASE WHEN jsonb_typeof(%s) = 'object' THEN %s ELSE '{}'::jsonb END,
			        '{transform}', ?::jsonb, true
			    )
			  WHERE COALESCE(CAST(%s AS TEXT), '') = ?`,
			tableSQL,
			quoteIdentifier("metadata"),
			quoteIdentifier("metadata"),
			quoteIdentifier("metadata"),
			primaryKeySQL,
		)
		result := tx.Exec(updateSQL, string(transformJSON), key)
		if result.Error != nil {
			return fmt.Errorf("save model registration: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("data not found")
		}
		return nil
	})
}

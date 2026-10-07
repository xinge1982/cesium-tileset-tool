package road

import (
	"cesium-tileset-tool/map3d/road/common"
	barrierrigid "cesium-tileset-tool/map3d/road/plugins/barrier_rigid"
	barrierwave "cesium-tileset-tool/map3d/road/plugins/barrier_wave"
	curbplugin "cesium-tileset-tool/map3d/road/plugins/curb"
	linearinstancedmodel "cesium-tileset-tool/map3d/road/plugins/linear_instanced_model"
	noisewallplugin "cesium-tileset-tool/map3d/road/plugins/noise_wall"
	roadexpansionjoint "cesium-tileset-tool/map3d/road/plugins/road_expansion_joint"
	roadmarksurfaceplugin "cesium-tileset-tool/map3d/road/plugins/road_mark_surface"
	roadmarking "cesium-tileset-tool/map3d/road/plugins/road_marking"
	roadraisedsurface "cesium-tileset-tool/map3d/road/plugins/road_raised_surface"
	roadsubgradeplugin "cesium-tileset-tool/map3d/road/plugins/road_subgrade"
	roadsurfaceplugin "cesium-tileset-tool/map3d/road/plugins/road_surface"
	roadtollisland "cesium-tileset-tool/map3d/road/plugins/road_toll_island"
	"fmt"

	"strconv"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/twpayne/go-geom"
)

type (
	BuildType              = common.BuildType
	MaterialMode           = common.MaterialMode
	UVMapping              = common.UVMapping
	TileContext            = common.TileContext
	BuildOptions           = common.BuildOptions
	MaterialRef            = common.MaterialRef
	MaterialSet            = common.MaterialSet
	MaterialIndices        = common.MaterialIndices
	UVOptions              = common.UVOptions
	FeatureFields          = common.FeatureFields
	FeatureInput           = common.FeatureInput
	CenterlineFeature      = common.CenterlineFeature
	LineFeature            = common.LineFeature
	SurfaceFeature         = common.SurfaceFeature
	Profile2D              = common.Profile2D
	LocalLine              = common.LocalLine
	LocalRings             = common.LocalRings
	BuildRuntime           = common.BuildRuntime
	LineBuilderPlugin      = common.LineBuilderPlugin
	SurfaceBuilderPlugin   = common.SurfaceBuilderPlugin
	BuilderRegistry        = common.BuilderRegistry
	CoordinatePipeline     = common.CoordinatePipeline
	MaterialResolver       = common.MaterialResolver
	FeatureMetadataBuilder = common.FeatureMetadataBuilder
	SurfaceBuilder         = common.SurfaceBuilder
	RoadSurfaceProjector   = common.RoadSurfaceProjector
	ProfileFactory         = common.ProfileFactory
	UVBuilder              = common.UVBuilder
	Triangulator           = common.Triangulator
	MeshPart               = common.MeshPart
)

const (
	BuildTypeRoadMarking               = common.BuildTypeRoadMarking
	BuildTypeRoadMarkingSolid          = common.BuildTypeRoadMarkingSolid
	BuildTypeRoadMarkingDashed         = common.BuildTypeRoadMarkingDashed
	BuildTypeRoadExpansionJoint        = common.BuildTypeRoadExpansionJoint
	BuildTypeRoadCurb                  = common.BuildTypeRoadCurb
	BuildTypeLinearInstancedModel      = common.BuildTypeLinearInstancedModel
	BuildTypeBarrierAntiGlareBoard     = common.BuildTypeBarrierAntiGlareBoard
	BuildTypeBarrierRigid              = common.BuildTypeBarrierRigid
	BuildTypeBarrierNewJersey          = common.BuildTypeBarrierNewJersey
	BuildTypeBarrierConcreteWall       = common.BuildTypeBarrierConcreteWall
	BuildTypeBarrierWave               = common.BuildTypeBarrierWave
	BuildTypeBarrierGuardrailTwoWave   = common.BuildTypeBarrierGuardrailTwoWave
	BuildTypeBarrierGuardrailThreeWave = common.BuildTypeBarrierGuardrailThreeWave
	BuildTypeBarrierGuardrailNoseEnd   = common.BuildTypeBarrierGuardrailNoseEnd
	BuildTypeBarrierGuardrail          = common.BuildTypeBarrierGuardrail
	BuildTypeBarrierNoiseWall          = common.BuildTypeBarrierNoiseWall
	BuildTypeRoadSurface               = common.BuildTypeRoadSurface
	BuildTypeRoadSubgrade              = common.BuildTypeRoadSubgrade
	BuildTypeRoadRaisedSurface         = common.BuildTypeRoadRaisedSurface
	BuildTypeRoadGreenbelt             = common.BuildTypeRoadGreenbelt
	BuildTypeRoadMarkSurface           = common.BuildTypeRoadMarkSurface
	BuildTypeRoadSurfaceExtrude        = common.BuildTypeRoadSurfaceExtrude
	BuildTypeBuildingExtrude           = common.BuildTypeBuildingExtrude
	BuildTypeCanopyColumns             = common.BuildTypeCanopyColumns
	BuildTypeRoadTollIsland            = common.BuildTypeRoadTollIsland

	MaterialModeTexture = common.MaterialModeTexture
	MaterialModeColor   = common.MaterialModeColor

	UVMappingPlanar = common.UVMappingPlanar
	UVMappingStrip  = common.UVMappingStrip
	UVMappingSweep  = common.UVMappingSweep
)

func DefaultBuildOptions() BuildOptions {
	return common.DefaultBuildOptions()
}

func NewBuilderRegistry() *BuilderRegistry {
	return common.NewBuilderRegistry()
}

func DefaultBuilderRegistry() *BuilderRegistry {
	r := common.NewBuilderRegistry()
	r.MustRegisterLine(linearinstancedmodel.NewLinearInstancedModelPlugin())
	r.MustRegisterLine(barrierrigid.NewBarrierRigidPlugin())
	r.MustRegisterLine(barrierwave.NewBarrierWavePlugin())
	r.MustRegisterLine(curbplugin.NewRoadCurbPlugin())
	r.MustRegisterLine(noisewallplugin.NewNoiseWallPlugin())
	r.MustRegisterLine(roadmarking.NewRoadMarkingPlugin())
	r.MustRegisterLine(roadexpansionjoint.NewRoadExpansionJointPlugin())
	r.MustRegisterLine(roadtollisland.NewRoadTollIslandPlugin())
	r.MustRegisterSurface(roadsurfaceplugin.NewRoadSurfaceExtrudePlugin())
	r.MustRegisterSurface(roadsubgradeplugin.NewRoadSubgradePlugin())
	r.MustRegisterSurface(roadraisedsurface.NewRoadRaisedSurfacePlugin())
	r.MustRegisterSurface(roadmarksurfaceplugin.NewRoadMarkSurfacePlugin())
	r.MustRegisterSurface(common.NewPlaceholderSurfacePlugin(common.BuildTypeBuildingExtrude))
	r.MustRegisterSurface(common.NewPlaceholderSurfacePlugin(common.BuildTypeCanopyColumns))
	return r
}

func CalcBasePointAndRegionFromGeometries(geometries ...geom.T) ([]float64, [6]float64, error) {
	return common.CalcBasePointAndRegionFromGeometries(geometries...)
}

func CalcBasePointAndRegionFromFeatures(centerlines []CenterlineFeature, lines []LineFeature, surfaces []SurfaceFeature) ([]float64, [6]float64, error) {
	return common.CalcBasePointAndRegionFromFeatures(centerlines, lines, surfaces)
}

func ResolveTileContextAuto(ctx TileContext, centerlines []CenterlineFeature, lines []LineFeature, surfaces []SurfaceFeature) (TileContext, error) {
	return common.ResolveTileContextAuto(ctx, centerlines, lines, surfaces)
}

func NewCoordinatePipeline(ctx TileContext) (*CoordinatePipeline, error) {
	return common.NewCoordinatePipeline(ctx)
}

func NewMaterialResolver(doc *gltf.Document) *MaterialResolver {
	return common.NewMaterialResolver(doc)
}

func NewFeatureMetadataBuilder(doc *gltf.Document) *FeatureMetadataBuilder {
	return common.NewFeatureMetadataBuilder(doc)
}

func NewSurfaceBuilder(doc *gltf.Document, materials *MaterialResolver, metadata *common.FeatureMetadataBuilder, projector *RoadSurfaceProjector, lineProjectMode string, sampleStep float32) *SurfaceBuilder {
	return common.NewSurfaceBuilder(doc, materials, metadata, projector, lineProjectMode, sampleStep)
}

func NewRoadSurfaceProjector() *RoadSurfaceProjector {
	return common.NewRoadSurfaceProjector()
}

func NewProfileFactory() *ProfileFactory {
	return common.NewProfileFactory()
}

func NewUVBuilder() *UVBuilder {
	return common.NewUVBuilder()
}

func NewTriangulator() *Triangulator {
	return common.NewTriangulator()
}

func EstimateCenterlineFeatureFromSurface(feature SurfaceFeature) (CenterlineFeature, error) {
	return common.EstimateCenterlineFeatureFromSurface(feature)
}

func TriangulateSubgradeExperimentalForTest(rings LocalRings) ([][3]float32, []uint32, bool) {
	return common.TriangulateSubgradeExperimental(rings)
}

func TriangulateSubgradeExperimentalAdaptiveForTest(rings LocalRings, roadID int64) ([][3]float32, []uint32, bool) {
	return common.TriangulateSubgradeExperimentalAdaptive(rings, roadID)
}

func TriangulatePlainSurfaceForTest(rings LocalRings) ([][3]float32, []uint32, error) {
	return common.TriangulatePlainSurfaceForTest(rings)
}

func BuildPlanarSurfacePrimitiveMetersForTest(doc *gltf.Document, pos [][3]float32, indices []uint32, material int, meterX, meterY float32, flipV bool) (*gltf.Primitive, error) {
	return common.BuildPlanarSurfacePrimitiveMetersForTest(doc, pos, indices, material, meterX, meterY, flipV)
}

func BuildTollIslandTopMesh(line LocalLine, width, height float32) (LocalRings, [][3]float32, []uint32, error) {
	return common.BuildTollIslandTopMesh(line, width, height)
}

func BuildTollIslandSideMesh(line LocalLine, width, height float32) ([][3]float32, [][3]float32, [][2]float32, []uint32, error) {
	return common.BuildTollIslandSideMesh(line, width, height)
}

func TriangulateSurfaceByCDTExperimentalForTest(ring LocalRings) ([][3]float32, []uint32, bool, error) {
	return common.TriangulateSurfaceByCDTExperimentalForTest(ring)
}

func parseDashPattern(pattern string) (float64, float64, bool) {
	return common.ParseDashPattern(pattern)
}

func featureFieldString(fields FeatureFields, key string) string {
	if fields == nil {
		return ""
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func featureFieldFloat32(fields FeatureFields, key string) float32 {
	if fields == nil {
		return 0
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float32:
		return x
	case float64:
		return float32(x)
	case int:
		return float32(x)
	case int32:
		return float32(x)
	case int64:
		return float32(x)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 32)
		if err == nil {
			return float32(f)
		}
	}
	return 0
}

func featureFieldInt64(fields FeatureFields, key string) int64 {
	if fields == nil {
		return 0
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err == nil {
			return n
		}
	}
	return 0
}

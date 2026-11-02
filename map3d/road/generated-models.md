# 按数据库分片生成独立道路 tileset

道路沿用原有编译路线：

1. `traffic_feature.InitTileSetTables` 初始化道路分片表。
2. `traffic_feature.RefineUntilStable` 在数据库中统计并细分，确定最终 geohash 叶子层级。
3. `traffic_feature.GenerateAllGeoHashTile` 读取这些叶子，在导出时生成道路面、波形护栏 GLB，复用原有 geohash 层级组织逻辑，输出一个道路 `tileset.json`。

道路源不再跳过数据库分片，不再按每个数据源生成独立子 tileset。道路配置只包含道路相关源；设备、建筑等使用已有模型的源放到其他 tileset 配置，混合配置会报错。

## 分片和建模的职责

`RefineUntilStable` 只负责空间统计和层级，不生成模型。道路面按原始多边形与格子相交来计数；护栏按线路及其横向截面/立柱占用范围统计，避免细格子将实际模型覆盖的邻格误判为空。业务分类过滤在计数和建模中使用相同规则。

道路细分按 `GeoTable.Threshold` 判断，一个格子的源要素数超过阈值才继续细分，最深沿用 `MAX_GEOHASH_LEVEL`（11）。命令行现有默认阈值为 50。这里统计的是源要素数，不是三角形数或 GLB 字节数。未超过阈值的非空子格子保留为叶子；重建时会删除已经不再需要的旧后代分片。

`GenerateRoadSourceModels(ctx, db, table, leaves, outputDirectory, bound)` 接收数据库叶子，返回待加入既有层级的节点和源生成统计；不创建自己的 tileset 层级。它由 `GenerateAllGeoHashTile` 的道路分支调用：

- 道路面从完整 Polygon(Z)/MultiPolygon(Z) 三角化，再将网格裁切到各叶子格子。
- 护栏先按完整 LineString(Z)/MultiLineString(Z) 构建链并确定 UV、采样相位，再按最终叶子裁切波板网格。立柱按采样锚点确定唯一归属，保留 GPU instancing。
- 一个叶子可以有道路面和护栏等多个道路模型内容节点，全部直接引用 GLB。父级包围盒在 ECEF 中合并，各 GLB 的 ENU 变换只应用一次。
- 输出模型位于 `road-models/run-*/source-*/tiles/<geohash>/`，主目录只生成一个道路 `tileset.json`；另外仍有编译/分片报告。道路分支不按 `partition.splitLevel` 拆出其他 tileset JSON。

源几何读取时转换成 EPSG:4326 并保留 Z。护栏输入线 Z 为地面高程。`bound` 限定参与细分的格子及相交源要素；跨越范围的原始要素会保留，模型裁切在叶子边界进行，不在项目范围边界重新裁切。因此边缘格子的模型可能稍超出项目 `bound`。完整护栏链在范围筛选前处理，避免不同项目范围改变链内采样起点。

## 配置示例

在道路 tileset 的 `sources` 中配置以下源，示例表名需替换为实际表名；几何列必须具有正确的 PostGIS SRID。

```yaml
sources:
  - id: road-surface
    modelSource: generated-road
    table:
      schema: public
      name: road_surface_polygons
      primaryKey: gid
      geometryField: geom
    road:
      buildType: road_surface
      # texturePath: /data/textures/road.png
      # centerlineTable:
      #   schema: public
      #   name: road_centerlines
      #   primaryKey: hroad_id
      #   geometryField: geom

  - id: wave-barrier
    modelSource: generated-road
    table:
      schema: public
      name: road_wave_lines
      primaryKey: id
      geometryField: geom
    road:
      buildType: barrier_wave
      waveSubtype: wave_two
      posts:
        diameter: 0.114
        topAboveBoardCenter: 0.15
        spacing: 4
        radialSegments: 12
```

几何类型由 `road.buildType` 明确区分。目前支持 `road_surface`、`barrier_wave`。原有设备源没有 `road`、`modelSource` 时仍按 `existing` 处理；配置 `road` 而省略 `modelSource` 时识别为 `generated-road`。不会依据 `hdpsidewall` 等表名自动改变旧源。

护栏支持 `wave_two`、`wave_three`、`wave_nose_end`。业务表含其他墙体时，用实际分类字段过滤和映射，例如：

```yaml
road:
  buildType: barrier_wave
  waveSubtypeField: rail_kind
  waveSubtypeMapping:
    two: wave_two
    three: wave_three
    nose: wave_nose_end
  filterField: rail_kind
  filterValues: [two, three, nose]
```

这里的字段及编码仅为示例。映射未包含的类别不会参与统计或建模，过滤值通过 SQL 参数绑定。

立柱参数单位为米，`radialSegments` 除外。直径默认/最小 0.114，默认柱顶比波板中心高 0.15；`height` 为显式柱高，优先于自动高度。`lateralOffset` 调整横向位置，零值沿用默认值。未设置纹理时使用颜色材质。道路面的 `hroad_id` 会补充为 `road_id`，用于中心线关联。

旧版 `road.precision` 保留配置兼容，但不再控制主流程分片；实际深度由数据库细分结果决定。当前道路不生成旧设备模型的 LOD0–LOD3。含道路源的增量更新会先重新细分再完整导出，以更新受上游链变化影响的下游模型。

## 直接使用道路包

不经过数据库的调用者可使用统一入口：

```go
result, err := road.BuildRoadModelSlices(ctx, road.RoadModelInput{
    Features: features,
    Centerlines: centerlines, // 道路面可选
}, road.RoadModelOptions{
    BuildType: road.BuildTypeRoadSurface,
    OutputDir: outputDir,
    Geohashes: leafHashes, // 支持 3–11 级混合叶子，不能重叠
    ModelsOnly: true,
})
```

此模式只生成 `result.Tiles` 中的 GLB，每项返回 `Geohash`、`GLBPath`、局部 `Box` 和 ENU `Transform`，调用者负责组织层级。显式空叶子不会回退到固定精度。保留原有独立试验入口：省略 `Geohashes`、`ModelsOnly` 时，按 `Precision`（4–7，默认 6）生成单独的固定精度 tileset。主道路编译不使用该试验路径。

新增道路设施时，在统一入口中增加生成器分派，并在 `traffic_feature.ResolveModelSource` 开放对应类型；根据设施实际占用范围补充分片统计方式。原有设备 GLB 加载流程无需参与。

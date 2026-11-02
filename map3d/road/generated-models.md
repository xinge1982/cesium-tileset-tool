# 在 tileset 导出时生成道路模型

`road.BuildRoadModelSlices` 是供 `traffic_feature` 或其他包调用的统一入口，当前支持 `road_surface` 和 `barrier_wave`。输入为原始 GeoJSON 几何及属性，输出为已切片的 GLB、子 tileset 路径和可挂接到父节点的世界坐标包围盒。新增设施可在该入口增加生成器分派，并在 `traffic_feature.ResolveModelSource` 中开放对应类型。

```go
result, err := road.BuildRoadModelSlices(ctx, road.RoadModelInput{
    Features: features,       // WGS84 Polygon(Z)/MultiPolygon(Z) 道路面
    Centerlines: centerlines, // 可选，用于道路面纵向纹理投影
}, road.RoadModelOptions{
    BuildType: road.BuildTypeRoadSurface,
    OutputDir: outputDir,
    Precision: 6,
})
```

波形护栏改用 `road.BuildTypeBarrierWave`，输入为 LineString(Z)/MultiLineString(Z)，线的 Z 是地面高度。`WaveSubtype` 支持 `wave_two`、`wave_three`、`wave_nose_end`，默认 `wave_two`；也可以通过 `WaveSubtypeField` 和 `WaveSubtypeMapping` 从业务分类字段转换，未映射的类别会跳过。`Posts` 可配置立柱，`Chains` 可调整链预处理。

## 主导出流程配置

在原有 tileset 配置的 `sources` 下增加以下源。示例表名需替换成实际表名；几何列须有正确的 PostGIS SRID，读取时转为 EPSG:4326 并保留 Z。

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
      precision: 6
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
      precision: 6
      waveSubtype: wave_two
      posts:
        diameter: 0.114
        topAboveBoardCenter: 0.15
        spacing: 4
        radialSegments: 12
```

原有点设施/建筑源不必修改：不配置 `road` 和 `modelSource` 时仍按 `existing` 查找本地/MinIO 模型。配置了 `road` 而省略 `modelSource` 时自动识别为 `generated-road`。显式指定 `existing` 同时配置 `road` 会报错，防止错误进入旧模型加载流程。类型识别依据配置，不依据表名推断，因此已有 `hdpsidewall` 源不会被意外改变。

业务表同时包含其他墙体时，可使用 `filterField`、`filterValues` 限定参与生成的记录；值用 SQL 参数绑定。分类字段如果已经保存 `wave_two` 等值，可设置 `waveSubtypeField: feature_type`；其他编码通过映射转换，例如：

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

这里的 `rail_kind` 及编码是配置示例，使用 `hdpsidewall` 时应替换成实际字段和编码。不要把所有墙体统一作为波形护栏。

立柱参数单位为米；直径默认/最小为 0.114，默认柱顶比波板中心高 0.15。`height` 为显式柱高，优先于 `topAboveBoardCenter`；`lateralOffset` 调整横向位置。零值沿用原生成器默认值。未设置纹理时使用颜色材质，不依赖测试目录下的纹理文件。道路面 `hroad_id` 会补充为 `road_id`，以便关联中心线。

## 输出及边界行为

- 每次主 tileset 导出都读取数据库几何、生成模型并切片，输出到 `generated-road/source-*/`，以外部子 tileset 挂到主根节点。生成报告的 `generatedRoads` 记录各源的子 tileset、切片数和实例数。
- 生成道路不进入旧的点模型分区计数、MinIO/本地 GLB 查找和 LOD 合并流程。与原有模型源可以混合使用；只有道路源时无需创建旧的点模型分区表。
- 道路子 tileset 保留自身 ENU 变换；父节点使用 ECEF 包围盒，不再次施加该变换，避免模型偏移或消失。护栏实例与结构化属性由原生成器保留。
- 当前道路按 `precision` 切片（4–7，默认 6），没有另行生成旧点设施的 LOD0–LOD3 模型。
- 为保留链长、起始偏移和立柱采样相位，护栏在范围筛选前读取完整源记录并构造链。分类过滤应保留属于同一护栏链的全部记录。范围 `bound` 当前仅保留完全包含的要素，跨越范围边界的要素被排除，不进行项目边界裁剪；这是现有道路生成器的行为。
- 含道路源的增量更新转为完整 tileset 重建，避免修改链上游后下游采样仍然使用旧模型。原有纯静态模型的更新路径保持原样。
- 所有非几何数据库字段进入源属性；主键额外规范成字符串 `id`，数值解码保留大整数精度。模型生成器会填充其专用的 `type`、`feature_type` 等分类属性。

本次未修改部署中的具体业务表配置；需在对应 tileset 的 `sources` 中启用上述配置后，正常执行原有导出命令。

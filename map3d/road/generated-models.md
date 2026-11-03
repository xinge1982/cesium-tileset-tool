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
lod:
  enabled: true
  lod0:
    geometricError: 120
  lod1:
    geometricError: 35
  lod2:
    geometricError: 7
  lod3:
    geometricError: 0
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

旧版 `road.precision` 保留配置兼容，但不再控制主流程分片；实际深度由数据库细分结果决定。含道路源的增量更新会先重新细分再完整导出，以更新受上游链变化影响的下游模型。

## 按模型部位和子类型匹配贴图

`road.materials` 设置 source 默认材质，`road.materialOverrides` 按已解析的模型子类型覆盖对应部位。每个条目选择 `texturePath` 或 `color`，不能同时配置。空条目继承下一优先级，不修改源属性或原始几何。

| buildType | 可配置部位 | 生成器内部材质槽 |
| --- | --- | --- |
| road_surface | top / side / bottom | Top / Side / Bottom |
| barrier_wave | rail / post | Top / Side |

护栏示例：

```yaml
road:
  buildType: barrier_wave
  waveSubtypeField: feature_type
  materials:
    rail:
      texturePath: networks/texture/road/bxhl_2.png
    post:
      color: "#8D949C"
  materialOverrides:
    wave_three:
      rail:
        texturePath: networks/texture/road/sbb.png
    wave_nose_end:
      rail:
        texturePath: networks/texture/road/bd_01.png
```

子类型使用 `waveSubtype`、`waveSubtypeField`、`waveSubtypeMapping` 解析后的 `wave_two`、`wave_three`、`wave_nose_end`，不使用数据库的原始分类编码。三波仅覆盖波板时，立柱继续继承 source 的 `post` 设置。立柱也可配置 `texturePath`，保留原有实例化网格和 UV。鼻端生成器目前没有立柱几何，`post` 不会为其额外创建立柱。

道路面示例：

```yaml
road:
  buildType: road_surface
  materials:
    top:
      texturePath: networks/texture/road/lm.jpeg
    side:
      color: "#888888"
    bottom:
      color: "#555555"
```

道路面还可使用 `materialOverrides.road_surface` 覆盖各部位。当前道路面切片是零厚度网格，实际使用顶面材质；配置 `side`、`bottom` 本身不会增加侧面或底面几何。

每个部位的选择顺序为：**子类型部位 → source 默认部位 → 旧 `texturePath`（适用部位）→ 内置颜色**。覆盖按整个条目选择，设置颜色会替换下层贴图，而不是把两者混在一起。旧 `texturePath` 对道路面仍作用于 top/side/bottom，对护栏仍只作用于 rail；未配置的立柱保持原来的灰色。未知部位、未知覆盖子类型及同时填写纹理与颜色会明确报错，避免配置拼写错误被忽略。贴图相对路径仍相对于程序运行目录，不是 YAML 所在目录。

实际匹配入口为 `road.ResolveRoadMaterials`，由波形护栏的 GeoJSON 转换及道路面切片生成器调用。默认与覆盖材质通过 `RoadModelOptions.Materials`、`MaterialOverrides` 传入；数据库编译适配器负责将配置映射到这两个参数。原有 `TexturePath` 调用方式保持兼容。

## LOD 与显示距离

`lod.enabled: true` 时，每个生成的道路内容按 LOD0 → LOD1 → LOD2 → LOD3 构造 `REPLACE` 链，节点使用各级配置的 `geometricError`。例如上海配置的 `120 / 35 / 7 / 0` 全部写入最终 JSON。各级误差必须有限、非负、严格递减，LOD3 必须为 0。

当前各级引用同一份现场生成的 GLB，不复制文件、不重复建模；尚未生成不同简化程度的几何。因此这些阈值会控制加载/替换层级，但不会带来网格简化或远景文件体积下降。多个道路源分别构造各自的替换链，保留属性和 GPU instancing。

geohash 空间父节点以第一个正 LOD 误差为基值，并逐级放大，避免停留在空节点而看不到道路。`lod.enabled: false` 时只保留 LOD3 内容，空间误差基值默认 128，内容节点仍为 0。原有设备模型的误差规则保持不变。

`modelFolder`、`localFirst`、`minioFallback` 属于已有设备/建筑模型的获取配置，道路源不读取这些目录或 MinIO 模型。生成报告的 `lod` 记录实际阈值和可用模型组数；共用的 GLB 文件及字节数只在 LOD3 计入一次。

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

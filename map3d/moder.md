# map3d/road 插件生成 GLB 逻辑说明

本文整理 `map-tool\map3d\road` 当前各插件从输入到 GLB 的核心生成链路，重点说明：

- 输入数据类型
- 坐标转换与裁剪
- 三角剖分/扫掠/实例化逻辑
- 贴图与 UV 生成
- 元数据写入
- 关键方法入口

## 1. 总体生成主链路

主入口：
- `map-tool\map3d\road\roadsurface.go`
- `map-tool\map3d\road\roadsurface_api.go`

核心对象：
- `RoadTileBuilder`
- `BuilderRegistry`
- `BuildRuntime`

### 1.1 调用顺序

1. 创建 `RoadTileBuilder`
   - 方法：`NewRoadTileBuilder(...)`
2. 添加输入要素
   - 线：`AddLineFeatures(...)`
   - 面：`AddSurfaceFeatures(...)`
   - 中心线：`AddCenterlineFeatures(...)`
3. 构建运行时依赖
   - 方法：`newRuntime(...)`
4. 预加载中心线到道路投影器
   - 方法：`preloadCenterlines(...)`
5. 先生成所有面类 primitive，再生成线类 primitive
   - 方法：`BuildDocument()`
6. 收尾
   - 元数据：`FeatureMetadataBuilder.Finalize()`
   - 实例节点合并：`mergeInstancedNodesByMeshAndName(...)`
   - primitive 批量合并：`mergePrimitivesByBatch(...)`
7. 写入 GLB
   - 方法：`BuildBinary()`
   - 调用：`appendPrimitivesAsMesh(...)` -> `compactDocument(...)` -> `gltf.NewEncoder(...).Encode(...)`

### 1.2 坐标转换

坐标转换入口：
- `map-tool\map3d\road\common\coords.go`

关键方法：
- `NewCoordinatePipeline(...)`
- `ClipLineFeatureToTile(...)`
- `ClipSurfaceFeatureToTile(...)`
- `ToLocalLine(...)`
- `ToLocalSurface(...)`
- `flatCoordsToLocalPoints(...)`

当前逻辑：
- 输入一般为 `4326` GeoJSON
- 通过 `TileContext.BasePoint` 建立 ENU 局部米制坐标
- `flatCoordsToLocalPoints(...)` 将经纬度转为局部 `[x, y, z]`
  - 注意这里内部用的是：
    - `x = ENU east`
    - `y = ENU up`
    - `z = ENU north`
- 当前 `ClipLineFeatureToTile(...)` / `ClipSurfaceFeatureToTile(...)` 实际上仍是直通返回，没有真正做瓦片裁剪

### 1.3 材质与贴图

材质入口：
- `map-tool\map3d\road\common\material.go`

关键方法：
- `NewMaterialResolver(...)`
- `ResolveMaterialSet(...)`
- `ResolveMaterial(...)`
- `resolveTextureMaterial(...)`

当前逻辑：
- 同一 `MaterialRef` 通过 `materialCacheKey(...)` 做缓存复用
- 纹理模式：`MaterialModeTexture`
- 纯色模式：`MaterialModeColor`
- 会统一设置：
  - `DoubleSided`
  - emissive
  - metallic/roughness
- 部分纹理还会自动尝试挂接 sibling PBR 贴图

### 1.4 元数据

元数据入口：
- `map-tool\map3d\road\common\metadata.go`

关键方法：
- `RegisterFeatureRows(...)`
- `AttachPrimitiveFeatureID(...)`
- `Finalize()`

当前组织：
- 面类 primitive：
  - 顶点属性 `_FEATURE_ID_0`
  - `EXT_mesh_features`
- 实例类节点：
  - `EXT_instance_features`
- 全部共享一张 `EXT_structural_metadata.propertyTables[0]`

### 1.5 线投影到道路面

道路投影器：
- `map-tool\map3d\road\common\project.go`

关键方法：
- `Configure(...)`
- `AddRoadCenterline(...)`
- `AddRoadSurface(...)`
- `ProjectLine(...)`
- `ProjectPoints(...)`

辅助逻辑：
- `densifyLine(...)`
- `refineLineByTriangleBoundaries(...)`
- `smoothProjectedLineY(...)`
- `fillUnmatchedProjectedLineY(...)`

当前投影模式：
- `fast`
  - 只按 `LineSampleStep` 主动插点
- `boundary_split`
  - 先在道路三角边界交点处分段，再按 `LineSampleStep` 插点

默认配置：
- `LineProjectionMode = "fast"`
- `LineSampleStep = 6.0`

### 1.6 三角剖分主策略

主文件：
- `map-tool\map3d\road\common\mesh.go`
- `map-tool\map3d\road\common\cdt_experimental.go`
- `map-tool\map3d\road\common\road_strip.go`

当前通用策略：
- 面类优先 `CDT`
- 再退 `poly2tri`
- 最后退 `Earcut`

关键方法：
- `triangulateSurfaceByCDTExperimental(...)`
- `triangulateRoadSurfaceRing(...)`
- `triangulateSubgradeSurfaceRing(...)`
- `triangulatePlanarPolygonSurfaceRing(...)`
- `triangulateGreenbeltTopRing(...)`
- `triangulateSurfaceByCenterlineSampling(...)`

---

## 2. 插件清单与生成逻辑

当前注册的主插件：

- `road_surface`
- `road_subgrade`
- `road_raised_surface`
- `road_mark_surface`
- `road_marking`
- `road_expansion_joint`
- `road_curb`
- `barrier_rigid`
- `barrier_wave`
- `barrier_noise_wall`
- `linear_instanced_model`
- `road_toll_island`

下面按插件说明。

---

## 3. road_surface（道路面）

插件文件：
- `map-tool\map3d\road\plugins\road_surface\plugin.go`

核心生成方法：
- `BuildSurface(...)`
- `SurfaceBuilder.BuildRoadSurface(...)`

### 3.1 输入
- GeoJSON：`Polygon` / `MultiPolygon`
- `BuildType = road_surface`
- 可选字段：
  - `road_id`
- 材质：`feature.Material.Top`
- UV：`feature.UV`

### 3.2 生成流程
1. `ClipSurfaceFeatureToTile(...)`
2. `ToLocalSurface(...)`
3. `BuildRoadSurface(feature, rings)`
4. 每个 ring 调用：
   - `triangulateRoadSurfaceRing(ring, centerline, hasCenterline)`
5. 三角化策略：
   - `triangulateRoadSurfaceCDTCandidate(...)`
   - 若有输入中心线：`triangulateRoadSurfaceByCenterline(...)`
   - 若无中心线：
     - `smoothRoadSurfaceRingHeights(...)`
     - 再试 `CDT`
   - 再退：`triangulateRoadSurfaceWithEstimatedCenterline(...)`
   - 再退：`triangulateSubgradeByPoly2Tri(...)`
   - 最后：`mgltf.EarcutXZRings(...)`
6. 成功后把道路三角面注册到投影器：
   - `RoadSurfaceProjector.AddRoadSurface(...)`
7. 生成顶面 primitive：
   - `buildPlanarSurfacePrimitiveMeters(...)`
8. 写 metadata：
   - `attachFeatureMetadata(...)`

### 3.3 UV 逻辑
- 顶面 planar meter UV
- 方法：`buildPlanarSurfacePrimitiveMeters(...)`
- `RepeatX/RepeatY` 默认 8m 平铺

### 3.4 输出
- 一个或多个道路顶面 primitive
- 同时进入道路投影器，供标线、伸缩缝等投影使用

---

## 4. road_subgrade（路基面）

插件文件：
- `map-tool\map3d\road\plugins\road_subgrade\plugin.go`

核心生成方法：
- `SurfaceBuilder.BuildRoadSubgrade(...)`

### 4.1 输入
- GeoJSON：`Polygon` / `MultiPolygon`
- `BuildType = road_subgrade`

### 4.2 生成流程
1. `ClipSurfaceFeatureToTile(...)`
2. `ToLocalSurface(...)`
3. `BuildRoadSubgrade(...)`
4. 每个 ring：
   - `triangulatePreferredSubgradeSurfaceRing(...)`
5. 路基 ring 预处理：
   - 原始 ring 无自交则直接试
   - 否则 `cleanSubgradeLocalRings(...)`
6. 正式三角化链：
   - `triangulateSubgradeSurfaceCDTCandidate(...)`
   - `triangulateSubgradeByPoly2Tri(...)`
   - `triangulatePlainSurfaceRing(...)`
7. 生成 primitive：
   - `buildPlanarSurfacePrimitiveMeters(...)`
8. 写 metadata：
   - `attachFeatureMetadata(...)`

### 4.3 关键方法
- `triangulatePreferredSubgradeSurfaceRing(...)`
- `cleanSubgradeLocalRings(...)`
- `sanitizeSubgradeRing(...)`
- `chooseSafeSubgradeSimplification(...)`

### 4.4 UV
- planar meter UV
- 默认平铺尺度 8m

---

## 5. road_raised_surface（绿化带 / 分割岛）

插件文件：
- `map-tool\map3d\road\plugins\road_raised_surface\plugin.go`

核心方法：
- `SurfaceBuilder.BuildRoadGreenbelt(...)`

### 5.1 输入
- GeoJSON：`Polygon` / `MultiPolygon`
- `BuildType = road_raised_surface`
- `feature_type` 常用：
  - `greenbelt`
  - `median_island`
- 字段：
  - `height` / `hight`

### 5.2 生成流程
1. `ClipSurfaceFeatureToTile(...)`
2. `ToLocalSurface(...)`
3. `BuildRoadGreenbelt(...)`
4. 每个 ring 先整体抬高：
   - `raiseLocalRings(...)`
5. 构建顶面 + 侧面：
   - `buildGreenbeltPrimitivesMeters(...)`
6. 顶面三角化链：
   - `triangulateGreenbeltTopRing(...)`
   - `triangulateGreenbeltTopRingCandidates(...)`
7. 候选策略：
   - 若无孔洞，先尝试：`estimateLocalCenterlineFromOuter(...)` + `triangulateRoadSurfaceByCenterline(...)`
   - 否则退：`triangulatePlanarPolygonSurfaceRing(...)`
   - `triangulatePlanarPolygonSurfaceRing(...)` 内部继续：
     - `CDT`
     - `poly2tri`
     - `Earcut`
8. 侧面 UV / 索引：
   - `wallUVsMeters(...)`
   - `wallIndicesOffsetLocal(...)`

### 5.3 输出
- 顶面 primitive
- 侧面 primitive

---

## 6. road_mark_surface（面状道路标识）

插件文件：
- `map-tool\map3d\road\plugins\road_mark_surface\plugin.go`

核心方法：
- `SurfaceBuilder.BuildRoadMarkSurface(...)`

### 6.1 输入
- GeoJSON：`Polygon` / `MultiPolygon`
- `BuildType = road_mark_surface`
- 字段：
  - `road_id`（若要投影到某条道路）

### 6.2 生成流程
1. `ClipSurfaceFeatureToTile(...)`
2. `ToLocalSurface(...)`
3. `BuildRoadMarkSurface(...)`
4. 三角化：
   - `triangulatePlanarPolygonSurfaceRing(...)`
   - 内部：`CDT -> poly2tri -> Earcut`
5. 若存在道路投影器：
   - `ProjectPoints(roadID, pos)`
   - 并整体抬高 `0.05`
6. 构建 primitive：
   - `buildPlanarSurfacePrimitiveMeters(...)`
7. 写 metadata

### 6.3 UV
- planar meter UV
- 默认 1m 平铺

---

## 7. road_marking（标线）

插件文件：
- `map-tool\map3d\road\plugins\road_marking\plugin.go`

核心方法：
- `SurfaceBuilder.BuildRoadMarking(...)`

### 7.1 输入
- GeoJSON：`LineString` / `MultiLineString`
- `BuildType = road_marking`
- 常用字段：
  - `ldid`：关联道路 id
  - `bxkd`：宽度
  - `bxbl`：虚线 pattern
  - `ys`：颜色
  - `startoffset`
  - `projection_mode`
  - `low_cost`

### 7.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. `BuildRoadMarking(...)`
4. 读取标线宽度：
   - 优先 `feature.Width`
   - 再读 `bxkd`
5. 计算投影模式：
   - `resolveLineProjectionMode(...)`
6. 投影到道路：
   - `RoadSurfaceProjector.ProjectLine(roadID, line, sampleStep, projectionMode)`
7. 若 `bxbl` 可解析为虚线：
   - `ParseDashPattern(...)`
   - `splitDashedLines(...)`
8. 对每条有效渲染线：
   - `profiles.ExpandLineToSurface(...)`
   - 必要时 `ProjectPoints(...)`
   - `triangulateExpandedLineSurface(...)`
9. 法线：
   - `computeVertexNormals(...)`
10. UV：
   - `fixedMeterUVs(...)`
11. 汇总为一个 primitive
12. 写 metadata

### 7.3 关键点
- 标线不是 CDT 主场景
- 核心是“投影 + 展宽条带 + 条带三角化”
- `triangulateExpandedLineSurface(...)` 保证展开后的 ring 与中心线段序一致

---

## 8. road_expansion_joint（伸缩缝）

插件文件：
- `map-tool\map3d\road\plugins\road_expansion_joint\plugin.go`

核心方法：
- `SurfaceBuilder.BuildRoadExpansionJoint(...)`

### 8.1 输入
- GeoJSON：线
- `BuildType = road_expansion_joint`
- 默认：
  - `width = 0.5`
  - `height = 0.01`
- 字段：
  - `ldid`
  - `projection_mode`
  - `low_cost`

### 8.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. `BuildRoadExpansionJoint(...)`
4. `ProjectLine(...)`
5. `profiles.ExpandLineToSurface(...)`
6. `ProjectPoints(...)`
7. `triangulateExpandedLineSurface(...)`
8. 法线：`computeVertexNormals(...)`
9. UV：`expandedLineStripUVs(...)`
   - 这里不是普通平面 UV，而是沿线/横向 strip UV
10. 汇总为单个 primitive
11. 写 metadata

---

## 9. road_curb（路缘石）

插件文件：
- `map-tool\map3d\road\plugins\curb\plugin.go`

核心方法：
- `SurfaceBuilder.BuildRoadCurb(...)`

### 9.1 输入
- GeoJSON：线
- `BuildType = road_curb`
- 参数：
  - `width`
  - `height`
  - `direction`

### 9.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. `BuildRoadCurb(...)`
4. 对每条线：
   - `buildRoadCurbMesh(line, width, height, direction)`
5. `buildRoadCurbMesh(...)`：
   - 基于中心线构 3 条共享顶点 strip：
     - 顶面
     - 外侧面
     - 内侧面
   - 端头封盖
6. 组装 primitive
7. 写 metadata

### 9.3 说明
- 当前已优化成共享顶点 strip mesh
- 不再是每段 quad 完全重复顶点

---

## 10. barrier_rigid（新泽西 / 水泥隔离墙）

插件文件：
- `map-tool\map3d\road\plugins\barrier_rigid\plugin.go`

核心方法：
- `SurfaceBuilder.BuildNewJerseyBarrier(...)`

### 10.1 输入
- GeoJSON：线
- `BuildType = barrier_rigid`
- `feature_type`：
  - `new_jersey`
  - `concrete_wall`
- 字段：
  - `width` / `barrier_width`
  - `height` / `hight` / `barrier_height`
  - `direction`

### 10.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. `BuildNewJerseyBarrier(...)`
4. 逐线构 mesh：
   - `buildNewJerseyBarrierMesh(line, width, height, direction)`
5. `buildNewJerseyBarrierMesh(...)` 内部：
   - 构造 profile
   - 沿线 sweep
   - 封端
   - 贴图 UV
6. 汇总成 primitive
7. 写 metadata

### 10.3 复用点
- 水泥隔离墙与新泽西护栏共用同类 profile sweep 逻辑
- 主要差异在材质和输入参数

---

## 11. barrier_wave（二波板 / 三波板 / 鼻端护栏）

插件文件：
- `map-tool\map3d\road\plugins\barrier_wave\plugin.go`

核心方法：
- `BuildGuardrailTwoWave(...)`
- `BuildGuardrailThreeWave(...)`
- `BuildGuardrailNoseEnd(...)`

### 11.1 输入
- GeoJSON：线
- `BuildType = barrier_wave`
- `feature_type`：
  - `wave_two`
  - `wave_three`
  - `wave_nose_end`
- 字段：
  - `rail_height`
  - `rail_height_all`
  - `direction`
  - 鼻端可用贴图角度：`UV.RotateDeg`

### 11.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. 根据 `feature_type` 分流：
   - `BuildGuardrailTwoWave(...)`
   - `BuildGuardrailThreeWave(...)`
   - `BuildGuardrailNoseEnd(...)`

### 11.3 二波/三波主体
- `buildGuardrailTwoWaveMesh(...)`
- `buildGuardrailThreeWaveMesh(...)`

主体逻辑：
- 板体是沿线展开的 strip mesh
- 立柱+连接器先组合为一个 support mesh：
  - `buildGuardrailSupportMesh(...)`
- 再通过实例化节点批量摆放：
  - `guardrailSupportTransforms(...)`
  - `addInstancedPrimitiveNode(...)`

### 11.4 鼻端护栏
- 目前采用单面板 + `DoubleSided`
- UV 可旋转
- 贴图角度通过：
  - `UVOptions.RotateDeg`
- 相关辅助：
  - `rotateUVs(...)`

---

## 12. barrier_noise_wall（声屏障）

插件文件：
- `map-tool\map3d\road\plugins\noise_wall\plugin.go`

核心方法：
- `SurfaceBuilder.BuildNoiseWall(...)`

### 12.1 输入
- GeoJSON：线
- `BuildType = barrier_noise_wall`
- 字段：
  - `height` / `hight`
  - `bend_offset` / `bend` / `bendOffset`
  - `spacing` / `interval`
  - `post_width`
  - `post_depth`
  - `direction`

### 12.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. `BuildNoiseWall(...)`
4. 主板：
   - `buildNoiseWallMesh(line, direction, height, bendOffset, repeatU)`
   - 采用 3 点折线 profile sweep
   - 无厚度薄板
5. 立柱：
   - `noiseWallPostTransforms(...)`
   - `buildNoiseWallBentPostMesh(...)`
   - 再用 `addInstancedPrimitiveNode(...)` 实例化
6. 材质：
   - 主板透明、双面
   - 立柱单独材质
7. 主板 primitive 写 metadata
8. 立柱实例节点写 `EXT_instance_features`

---

## 13. linear_instanced_model（沿线重复小模型）

插件文件：
- `map-tool\map3d\road\plugins\linear_instanced_model\plugin.go`

当前子类型：
- `anti_glare_board`

核心方法：
- `SurfaceBuilder.BuildAntiGlareBoard(...)`

### 13.1 输入
- GeoJSON：线
- `BuildType = linear_instanced_model`
- `feature_type = anti_glare_board`
- 字段：
  - `spacing` / `interval`
  - `startoffset`
  - `model_path`

### 13.2 生成流程
1. `ClipLineFeatureToTile(...)`
2. `ToLocalLine(...)`
3. `BuildAntiGlareBoard(...)`
4. 导入模板模型：
   - `ensureInstancedModelMesh(...)`
   - `importInstancedModelMesh(...)`
5. 沿线采样：
   - `buildAntiGlareBoardTransforms(line, spacing, startOffset)`
6. 实例化写入：
   - `addInstancedMeshNode(...)`
7. metadata：
   - 通过 `EXT_instance_features`

---

## 14. road_toll_island（收费岛）

插件文件：
- `map-tool\map3d\road\plugins\road_toll_island\plugin.go`

核心方法：
- `SurfaceBuilder.BuildRoadTollIsland(...)`

### 14.1 输入
- GeoJSON：线
- 当前假设：收费岛线由 2 个三维点构成
- `BuildType = road_toll_island`
- 字段：
  - `width` / `widths`
  - `height`
  - `head_model_path`
  - `tail_model_path`
  - `head_offset`
  - `tail_offset`
  - `head_yaw_deg`
  - `tail_yaw_deg`

### 14.2 岛身生成流程
1. `ToLocalLine(...)`
2. `BuildRoadTollIsland(...)`
3. 解析尺寸：
   - `resolveTollIslandDimensions(...)`
4. 两点线直接构矩形体：
   - `BuildTollIslandTopMesh(...)`
   - `buildTollIslandRectRings(...)`
5. 顶面：
   - `buildPlanarSurfacePrimitiveMeters(...)`
6. 侧面：
   - `buildTollIslandSideMeshFromRings(...)`
   - 共享顶点 strip
7. 顶面和侧面都写 metadata

### 14.3 岛头 / 岛尾模型流程
1. 解析模型参数：
   - `resolveTollIslandModelOptions(...)`
2. 计算头尾变换：
   - `buildTollIslandEndModelTransforms(...)`
3. 导入模型 mesh：
   - `ensureInstancedModelMesh(...)`
4. 头尾分别实例化为两个节点：
   - `addInstancedMeshNode(...)`

### 14.4 现状说明
- 岛身逻辑已经固定为：
  - 两端点确定长度
  - 宽度决定左右展宽
  - 高度决定抬起
  - 生成长方体岛身
- 岛头 / 岛尾仍在持续校正模型本地方向与比例

---

## 15. 关键公共几何方法一览

### 15.1 线转面 / strip / sweep
- `ProfileFactory.ExpandLineToSurface(...)`
- `ProfileFactory.SweepProfileMesh(...)`
- `triangulateExpandedLineSurface(...)`
- `buildRoadCurbMesh(...)`
- `buildNewJerseyBarrierMesh(...)`
- `buildNoiseWallMesh(...)`
- `buildGuardrailTwoWaveMesh(...)`
- `buildGuardrailThreeWaveMesh(...)`
- `BuildTollIslandTopMesh(...)`
- `BuildTollIslandSideMesh(...)`

### 15.2 面三角剖分
- `triangulateRoadSurfaceRing(...)`
- `triangulateRoadSurfaceCDTCandidate(...)`
- `triangulateSubgradeSurfaceRing(...)`
- `triangulateSubgradeSurfaceCDTCandidate(...)`
- `triangulatePlanarPolygonSurfaceRing(...)`
- `triangulatePlanarPolygonSurfaceCDTCandidate(...)`
- `triangulateGreenbeltTopRing(...)`
- `triangulateGreenbeltTopRingCandidates(...)`
- `triangulateSurfaceByCDTExperimental(...)`
- `triangulateSurfaceByCenterlineSampling(...)`

### 15.3 实例化
- `ensureInstancedModelMesh(...)`
- `importInstancedModelMesh(...)`
- `addInstancedMeshNode(...)`
- `addInstancedPrimitiveNode(...)`
- `buildAntiGlareBoardTransforms(...)`
- `noiseWallPostTransforms(...)`
- `guardrailSupportTransforms(...)`
- `buildTollIslandEndModelTransforms(...)`

### 15.4 UV
- `buildPlanarSurfacePrimitiveMeters(...)`
- `fixedMeterUVs(...)`
- `expandedLineStripUVs(...)`
- `wallUVsMeters(...)`
- `rotateUVs(...)`

---

## 16. 当前项目中各插件最核心的技术路线总结

### 16.1 纯面类（适合 CDT）
- `road_surface`
- `road_subgrade`
- `road_mark_surface`
- `road_raised_surface` 顶面

共同特点：
- GeoJSON 面输入
- 局部米制坐标转换
- `CDT -> poly2tri -> Earcut` 或条带优先后再 `CDT`
- 最终用 `buildPlanarSurfacePrimitiveMeters(...)` 生成 top primitive

### 16.2 线贴合道路类
- `road_marking`
- `road_expansion_joint`

共同特点：
- 线输入
- 通过 `RoadSurfaceProjector` 贴合道路面
- 再展宽为 strip 面
- 不依赖 CDT 作为主逻辑

### 16.3 Sweep / profile 类
- `road_curb`
- `barrier_rigid`
- `barrier_noise_wall`
- `road_toll_island`（岛身是简化矩形，不是通用 sweep）

共同特点：
- 线输入
- 通过 profile 或 strip 生成实体/薄板
- UV 主要沿线展开

### 16.4 板体 + 支撑件实例化类
- `barrier_wave`
- `barrier_noise_wall`
- `linear_instanced_model`

共同特点：
- 主板体作为 strip / sweep mesh
- 支撑件或小模型通过 instancing 摆放

---

## 17. 当前代码里可直接作为排查入口的文件

如果某类模型效果不对，优先看下面这些文件：

- 总入口 / 注册：
  - `map-tool\map3d\road\roadsurface.go`
  - `map-tool\map3d\road\roadsurface_api.go`
- 坐标与投影：
  - `map-tool\map3d\road\common\coords.go`
  - `map-tool\map3d\road\common\project.go`
- 面类三角剖分：
  - `map-tool\map3d\road\common\mesh.go`
  - `map-tool\map3d\road\common\cdt_experimental.go`
  - `map-tool\map3d\road\cdt-go\cdt\binding.go`
- 线 sweep / 条带：
  - `map-tool\map3d\road\common\profile.go`
  - `map-tool\map3d\road\common\road_strip.go`
- 各专项：
  - `map-tool\map3d\road\common\curb.go`
  - `map-tool\map3d\road\common\guardrail.go`
  - `map-tool\map3d\road\common\noise_wall.go`
  - `map-tool\map3d\road\common\anti_glare_board.go`
  - `map-tool\map3d\road\common\toll_island.go`

---

## 18. 一句话总结

当前项目的 GLB 生成分为四条主路线：

1. **面类 -> CDT / poly2tri / Earcut 三角剖分**
2. **线类贴合道路 -> 投影 + 展宽条带**
3. **线类实体构件 -> profile/strip sweep**
4. **重复小构件 -> instancing**

道路面、路基、绿化带顶面现在已经统一到 `CDT` 优先路线；标线、伸缩缝仍以“投影后条带构面”为主；护栏、路缘石、声屏障、收费岛则属于 strip/sweep/实例化类对象。

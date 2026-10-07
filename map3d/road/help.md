# 新插件编写说明

这份文档只说明一件事：在当前 `road` 包结构下，如何新增一个可用插件。

## 当前结构

- 入口：
  - `map-tool\map3d\road\roadsurface.go`
  - `map-tool\map3d\road\roadsurface_api.go`
- 公共能力：
  - `map-tool\map3d\road\common`
- 现有插件：
  - `map-tool\map3d\road\plugins\road_surface`
  - `map-tool\map3d\road\plugins\marking_solid`
  - `map-tool\map3d\road\plugins\marking_dashed`

## 插件类型

- 线插件：实现 `common.LineBuilderPlugin`
- 面插件：实现 `common.SurfaceBuilderPlugin`

判断标准很简单：
- 输入是 `LineString / MultiLineString`，走线插件
- 输入是 `Polygon / MultiPolygon`，走面插件

## 第一步：新增 BuildType

先在 `map-tool\map3d\road\common\types.go` 里增加新的 `BuildType` 常量。

例如新增一个路缘石插件：

```go
const (
    BuildTypeCurb common.BuildType = "curb"
)
```

要求：
- 值必须唯一
- 名字表达“怎么生成”，不要表达业务表名
- 一个 `BuildType` 对应一个插件目录

## 第二步：创建插件目录

在 `map-tool\map3d\road\plugins` 下新建目录。

例如：

```text
map-tool\map3d\road\plugins\curb
```

一般先只放一个 `plugin.go`，复杂后再拆。

## 第三步：实现插件接口

### 线插件模板

```go
package curb

import (
    "fmt"
    "cesium-tileset-tool/map3d/road/common"

    "github.com/qmuntal/gltf"
)

type CurbPlugin struct{}

func NewCurbPlugin() CurbPlugin {
    return CurbPlugin{}
}

func (p CurbPlugin) BuildType() common.BuildType {
    return common.BuildTypeCurb
}

func (p CurbPlugin) BuildLine(runtime common.BuildRuntime, feature common.LineFeature) ([]*gltf.Primitive, error) {
    if feature.BuildType != p.BuildType() {
        return nil, fmt.Errorf("unexpected build type: %s", feature.BuildType)
    }

    clipped, err := runtime.Coordinates.ClipLineFeatureToTile(feature)
    if err != nil {
        return nil, fmt.Errorf("clip line feature: %w", err)
    }

    primitives := make([]*gltf.Primitive, 0)
    for i := range clipped {
        lines, err := runtime.Coordinates.ToLocalLine(clipped[i])
        if err != nil {
            return nil, fmt.Errorf("convert clipped line %d to local: %w", i, err)
        }

        prs, err := runtime.Meshes.BuildLinearBarrier(clipped[i], lines)
        if err != nil {
            return nil, fmt.Errorf("build line feature %d: %w", i, err)
        }
        primitives = append(primitives, prs...)
    }
    return primitives, nil
}
```

### 面插件模板

```go
package building

import (
    "fmt"
    "cesium-tileset-tool/map3d/road/common"

    "github.com/qmuntal/gltf"
)

type BuildingPlugin struct{}

func NewBuildingPlugin() BuildingPlugin {
    return BuildingPlugin{}
}

func (p BuildingPlugin) BuildType() common.BuildType {
    return common.BuildTypeBuildingExtrude
}

func (p BuildingPlugin) BuildSurface(runtime common.BuildRuntime, feature common.SurfaceFeature) ([]*gltf.Primitive, error) {
    if feature.BuildType != p.BuildType() {
        return nil, fmt.Errorf("unexpected build type: %s", feature.BuildType)
    }

    clipped, err := runtime.Coordinates.ClipSurfaceFeatureToTile(feature)
    if err != nil {
        return nil, fmt.Errorf("clip surface feature: %w", err)
    }

    primitives := make([]*gltf.Primitive, 0)
    for i := range clipped {
        rings, err := runtime.Coordinates.ToLocalSurface(clipped[i])
        if err != nil {
            return nil, fmt.Errorf("convert clipped surface %d to local: %w", i, err)
        }

        prs, err := runtime.Meshes.BuildBuilding(clipped[i], rings)
        if err != nil {
            return nil, fmt.Errorf("build surface feature %d: %w", i, err)
        }
        primitives = append(primitives, prs...)
    }
    return primitives, nil
}
```

## 第四步：注册插件

新增插件后，必须在默认注册表里注册，否则入口找不到。

修改文件：
- `map-tool\map3d\road\roadsurface_api.go`

在 `DefaultBuilderRegistry()` 里加入：

```go
r.MustRegisterLine(curb.NewCurbPlugin())
```

或者：

```go
r.MustRegisterSurface(building.NewBuildingPlugin())
```

注意：
- 线插件用 `MustRegisterLine`
- 面插件用 `MustRegisterSurface`

## 第五步：优先复用 common

优先复用 `common`，不要每个插件自己重写一套。

常用能力：
- `runtime.Coordinates`
  - 裁剪
  - 4326 转局部 ENU
- `runtime.Materials`
  - 贴图 / 颜色材质解析
- `runtime.Meshes`
  - 道路面、标线、建筑、顶棚等几何入口
- `runtime.Projector`
  - 用道路三角面给线重匹配高度
  - 道路主中心线缓存
- `runtime.Profiles`
  - 线展宽、横断面
- `runtime.UVs`
  - UV 生成
- `runtime.Triangulator`
  - 三角剖分、挤出

原则：
- 插件负责组织流程
- 公共模块负责几何细节

## 第六步：标准处理流程

### 线插件通常流程

1. 校验 `BuildType`
2. `ClipLineFeatureToTile`
3. `ToLocalLine`
4. 如需要，用 `Projector` 重匹配高度
5. 展宽 / 扫掠 / 切虚线
6. 生成 primitive

### 面插件通常流程

1. 校验 `BuildType`
2. `ClipSurfaceFeatureToTile`
3. `ToLocalSurface`
4. 三角剖分 / 挤出
5. 生成材质和 UV
6. 生成 primitive

## 第七步：输入设计建议

所有插件输入都应基于统一结构：
- `BuildType`
- `Geom`
- `Fields`

线插件常见额外字段：
- `Width`
- `Height`
- `Thickness`
- `Material`
- `UV`

面插件常见额外字段：
- `Height`
- `Thickness`
- `Levels`
- `FloorHeight`
- `Material`
- `UV`

原则：
- 原始业务字段放 `Fields`
- 几何生成必需参数放结构字段
- 不要把所有逻辑都耦合在 `Fields["xxx"]`

## BuildOptions 补充

后续新增插件时，优先复用已有构建参数，不要在插件内部写死：

- `UseInputCenterline`
  - 是否使用外部中心线
- `MarkingSampleStep`
  - 标线投影时的加点步长
- `ProjectSearchDist`
  - 标线投影时道路三角面的搜索距离
- `ProjectSmoothMeter`
  - 标线投影后的高程平滑窗口

如果插件需要新增阈值，优先加到 `BuildOptions`，不要散落在插件里。

## 第八步：道路中心线说明

当前主流程默认不依赖外部道路中心线。

默认行为：
- 先根据道路面点集做 PCA，估算一条参考中心线
- 再使用这条估算中心线做道路条带剖分
- 如果条带剖分失败，会自动回退普通 polygon 三角剖分

只有在特殊场景下，才建议显式使用外部中心线：

```go
opt := DefaultBuildOptions()
opt.UseInputCenterline = true
```

然后再调用：
- `AddCenterlineFeatures(...)`

结论：
- 默认方案：PCA 估算中心线
- 特殊声明后：使用输入中心线覆盖默认方案

## 第九步：标线插件特殊要求

如果插件是“线贴道路面”类型，建议同时输入对应道路面。

原因：
- 标线最终显示高度要和道路三角面一致
- 仅靠原始线 Z 不稳定
- 当前生成顺序是：先生成面，再生成线
- 所以线插件可以依赖 `ldid = road_id` 做高度重匹配

## 第十步：最小测试流程

建议在：
- `map-tool\map3d\road\roadsurface_plugin_test.go`

至少覆盖：
1. 构造 `TileContext`
2. 准备一个 feature
3. `NewRoadTileBuilder(...)`
4. `AddLineFeatures(...)` 或 `AddSurfaceFeatures(...)`
5. `BuildBinary()`
6. 检查是否输出有效 GLB

## 第十一步：提交前检查

至少执行：

```powershell
go test ./map3d/road
```

如果插件改了公共逻辑，再同步检查：
- `map-tool\map3d\road\readme.md`
- `map-tool\map3d\road\help.md`

保证文档和代码一致。

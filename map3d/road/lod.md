# road LOD 设计说明

## 目标

本文档定义当前 `map3d/road` 插件体系的 LOD（Level of Detail）建议规则，目标是：

- 先通过“是否生成 + 采样密度 + 实例密度 + 点简化”控制模型规模
- 暂不优先引入复杂网格后处理简化算法
- 保持道路面、路基等敏感几何的稳定性
- 便于后续直接落到 `BuildOptions.LOD`

---

## LOD 总体定义

### LOD0

远景层。

特点：
- 只保留大体量、强轮廓、强识别对象
- 不保留小型细节和密集重复构件
- 优先保证整体形状和轮廓

### LOD1

中景层。

特点：
- 保留主要道路附属设施
- 对密集对象降采样、降实例密度
- 保留主要视觉特征

### LOD2

近景层。

特点：
- 全量精细表达
- 接近当前完整模型输出

---

## 全局控制参数建议

建议后续在 LOD 体系中统一抽象以下控制项：

- `enabled`：该对象在当前 LOD 是否生成
- `point_simplify_tolerance`：输入边界点简化容差（米）
- `sample_step`：采样步长（米）
- `instance_spacing_scale`：实例间距倍率
- `include_posts`：是否保留立柱
- `include_connectors`：是否保留连接器
- `profile_detail_level`：profile 精度等级
- `min_area`：最小生成面积阈值
- `min_length`：最小生成长度阈值

---

## 插件 LOD 总表

| 插件 | LOD0 | LOD1 | LOD2 |
|---|---|---|---|
| `road_surface` | 保留 | 保留 | 保留 |
| `road_subgrade` | 保留 | 保留 | 保留 |
| `road_raised_surface` | 保留主要大面 | 保留 | 保留 |
| `road_mark_surface` | 不生成 | 只保留大型箭头/文字 | 全量 |
| `road_marking` | 不生成 | 只保留主标线，虚线降密 | 全量 |
| `road_curb` | 不生成 | 保留，降低 profile 精度 | 全量 |
| `barrier_rigid` | 保留 | 保留 | 保留 |
| `barrier_wave` | 不生成或只保留板 | 保留板，立柱/连接器降密 | 全量 |
| `barrier_noise_wall` | 只保留主板 | 主板 + 稀疏立柱 | 全量 |
| `linear_instanced_model` | 不生成 | 稀疏实例 | 全量 |

---

## 各插件详细规则

### 1. `road_surface`

LOD0：
- 保留
- 允许轻度边界降点
- 不改变现有三角剖分主逻辑

LOD1：
- 保留
- 轻度边界降点

LOD2：
- 全量

建议参数：
- `LOD0`: `point_simplify_tolerance = 0.20m`
- `LOD1`: `point_simplify_tolerance = 0.10m`
- `LOD2`: `point_simplify_tolerance = 0`

### 2. `road_subgrade`

LOD0：
- 保留
- 仅做轻度边界降点

LOD1：
- 保留
- 轻度边界降点

LOD2：
- 全量

说明：
- 路基不建议做激进简化
- 路基容易出现花面、弯道拉直、关键点丢失
- 优先控制输入点密度，不做强网格后简化

建议参数：
- `LOD0`: `point_simplify_tolerance = 0.20 ~ 0.30m`
- `LOD1`: `point_simplify_tolerance = 0.10m`
- `LOD2`: `point_simplify_tolerance = 0`

### 3. `road_raised_surface`

包含：
- 绿化带
- 分割岛

LOD0：
- 保留主要大面
- 小面积对象可直接过滤

LOD1：
- 保留
- 顶面/侧面允许轻度降点

LOD2：
- 全量

建议参数：
- `LOD0`: `min_area = 4.0㎡`
- `LOD1`: 正常生成
- `LOD2`: 全量

### 4. `road_mark_surface`

包含：
- 转向箭头
- 地面文字
- 面状道路标识

LOD0：
- 不生成

LOD1：
- 只保留面积较大的 mark

LOD2：
- 全量

建议参数：
- `LOD1`: `min_area = 1.0㎡`
- `LOD2`: 全量

### 5. `road_marking`

包含：
- 实线
- 虚线
- 各类车道标线

LOD0：
- 不生成

LOD1：
- 只保留主要标线
- 虚线降低分段密度
- 可过滤很短的零碎线段

LOD2：
- 全量

建议参数：
- `LOD1`: `min_length = 3.0m`
- `LOD1`: 虚线 `sample_step` 放大
- `LOD2`: 原始规则

### 6. `road_curb`

LOD0：
- 不生成

LOD1：
- 保留
- 降低 profile 精度
- 保留基本纹理效果

LOD2：
- 全量

建议参数：
- `LOD1`: `profile_detail_level = low`
- `LOD2`: `profile_detail_level = full`

### 7. `barrier_rigid`

包含：
- 新泽西护栏
- 水泥隔离墙

LOD0：
- 保留

LOD1：
- 保留

LOD2：
- 保留

说明：
- 刚性护栏几何稳定、轮廓清晰、远景识别度高
- 建议所有 LOD 都保留

建议参数：
- `LOD0`: 允许轻度边界降点
- `LOD1`: 正常
- `LOD2`: 全量

### 8. `barrier_wave`

包含：
- 二波板
- 三波板
- 鼻端护栏

LOD0：
- 建议不生成
- 或只保留波形板，不保留立柱和连接器

LOD1：
- 保留波形板
- 立柱间距扩大
- 连接器可不生成

LOD2：
- 全量

建议参数：
- `LOD0`
  - `include_posts = false`
  - `include_connectors = false`
- `LOD1`
  - `include_posts = true`
  - `post_spacing *= 2`
  - `include_connectors = false`
- `LOD2`
  - 全量

### 9. `barrier_noise_wall`

LOD0：
- 只保留主板
- 不保留立柱

LOD1：
- 主板 + 稀疏立柱

LOD2：
- 全量

建议参数：
- `LOD0`
  - `include_posts = false`
- `LOD1`
  - `include_posts = true`
  - `post_spacing *= 2`
- `LOD2`
  - 原始间距

### 10. `linear_instanced_model`

包含：
- 防眩板
- 后续沿线重复摆放的小型 GLB 模型

LOD0：
- 不生成

LOD1：
- 实例间距扩大 2~4 倍

LOD2：
- 全量

建议参数：
- `LOD1`: `spacing *= 2` 或 `spacing *= 4`
- `LOD2`: 原始间距

---

## 推荐的默认 LOD 组合

### LOD0

保留：
- `road_surface`
- `road_subgrade`
- `road_raised_surface`
- `barrier_rigid`
- `barrier_noise_wall`（仅主板）

过滤：
- `road_mark_surface`
- `road_marking`
- `road_curb`
- `barrier_wave`
- `linear_instanced_model`

### LOD1

保留：
- 除小型高密度实例外的大部分对象

降密：
- `road_marking`
- `barrier_wave`
- `barrier_noise_wall`
- `linear_instanced_model`

### LOD2

- 当前完整效果

---

## 代码层落地建议

建议后续按以下方式接入：

### `BuildOptions`

增加：

```go
type BuildOptions struct {
    ...
    LOD int
}
```

### 插件内部判断

各插件按统一方式处理：

```go
switch runtime.Options.LOD {
case 0:
    // LOD0
case 1:
    // LOD1
case 2:
    // LOD2
default:
    // 默认按 LOD2
}
```

### 公共策略解析

建议增加统一策略解析函数：

```go
func ResolveLODPolicy(buildType BuildType, featureType string, lod int) LODPolicy
```

由该函数统一决定：
- 是否生成
- 采样步长
- 实例间距倍率
- 是否保留立柱
- 是否保留连接器
- 是否简化输入点

---

## 实施优先级建议

建议按以下顺序实现：

### 第一阶段

优先接入：
- `road_marking`
- `linear_instanced_model`
- `barrier_wave`
- `barrier_noise_wall`

原因：
- 对模型体积和渲染压力的影响最明显
- 风险低
- 不容易破坏核心道路几何

### 第二阶段

再接入：
- `road_raised_surface`
- `road_mark_surface`
- `road_curb`

### 第三阶段

最后再谨慎接入：
- `road_surface`
- `road_subgrade`

原因：
- 这两类对象对几何稳定性最敏感
- 需要在不破坏剖分质量的前提下做轻量输入简化

---

## 结论

当前 road 体系的 LOD 设计，推荐先走：

- 要素级过滤
- 输入级简化
- 实例级降密

暂不建议优先走：

- 激进网格后处理简化
- 对道路面/路基做复杂的三角网压缩

对当前工程最现实、最稳的路线是：
- `LOD0 / LOD1 / LOD2` 先通过规则控制生成范围和密度
- 后续再视实际效果决定是否引入更复杂的几何简化链路

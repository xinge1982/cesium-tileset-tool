# road 数据库设计建议

本文档按当前 `map-tool\map3d\road` 已实现插件整理数据库字段建议。

- 相近插件尽量合表
- 优先按“几何类型 + 构件家族”建表

我的建议不是“一插件一张表”，而是下面这种结构：

- 面类：按业务分 4 张左右
- 线类：按构件家族分 4~6 张
- 材质/模型资源：单独一张资源表

这样后续扩展插件时，不需要频繁改库。

---

## 1. 通用字段

建议所有业务表都包含以下公共字段。

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 要素ID | bigint / varchar(64) | 是 | 业务唯一标识 |
| name | 名称 | varchar(200) | 否 | 要素名称 |
| feature_type | 要素类型 | varchar(64) | 是 | 如 `road_surface`、`guardrail_wave` |
| geom | 几何 | geometry(...,4326) | 是 | 统一使用 4326 三维几何 |
| road_id | 关联道路ID | bigint / varchar(64) | 否 | 标线、mark、附属设施常用 |
| ldid | 关联逻辑道路ID | bigint / varchar(64) | 否 | 标线与道路匹配可用 |
| direction | 方向规则 | integer | 否 | 常用约定：`1` 左、`2` 右 |
| height | 高度 | numeric(10,3) | 否 | 通用高度参数，单位米 |
| width | 宽度 | numeric(10,3) | 否 | 通用宽度参数，单位米 |
| thickness | 厚度 | numeric(10,3) | 否 | 通用厚度参数，单位米 |
| color | 颜色 | varchar(32) | 否 | HEX 或颜色代码 |
| texture_code | 贴图编码 | varchar(64) | 否 | 关联资源表更合适 |
| model_code | 模型编码 | varchar(64) | 否 | 关联资源表更合适 |
| startoffset | 起始偏移 | numeric(10,3) | 否 | 虚线、防眩板等沿线布设 |
| ext_props | 扩展属性 | jsonb / text | 否 | 少量不稳定业务字段 |
| created_at | 创建时间 | timestamp | 否 | 审计字段 |
| updated_at | 更新时间 | timestamp | 否 | 审计字段 |

说明：

- 新库不建议再保留 `hight` 这类旧字段
- 所有几何建议统一为 `Z` 几何
- 所有长度类字段统一米制

---

## 2. 面类表设计

---

### 2.1 道路主面表 `road_surface`

用途：

- 道路面

几何类型：

- `PolygonZ`
- `MultiPolygonZ`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 道路面ID | bigint / varchar(64) | 是 | 唯一标识 |
| name | 道路名称 | varchar(200) | 否 | 名称 |
| geom | 道路面几何 | geometry(MultiPolygonZ,4326) | 是 | 道路面 |
| texture_code | 路面贴图编码 | varchar(64) | 否 | 如 `road_lm` |
| uv_repeat_x | 贴图X重复米数 | numeric(10,3) | 否 | 路面平铺参数 |
| uv_repeat_y | 贴图Y重复米数 | numeric(10,3) | 否 | 路面平铺参数 |
| use_centerline | 是否使用参考中心线 | boolean | 否 | 特殊情况下辅助剖分 |

---

### 2.2 路基面表 `road_subgrade`

用途：

- 路基面

几何类型：

- `PolygonZ`
- `MultiPolygonZ`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 路基ID | bigint / varchar(64) | 是 | 唯一标识 |
| name | 路基名称 | varchar(200) | 否 | 名称 |
| geom | 路基几何 | geometry(MultiPolygonZ,4326) | 是 | 路基面 |
| texture_code | 路基贴图编码 | varchar(64) | 否 | 如 `subgrade_lj` |
| uv_repeat_x | 贴图X重复米数 | numeric(10,3) | 否 | 平铺参数 |
| uv_repeat_y | 贴图Y重复米数 | numeric(10,3) | 否 | 平铺参数 |

说明：

- 如果后续路基改为“线 + 宽度”方案，建议另建线性路基表，不建议继续完全依赖面表

---

### 2.3 拉起面通用表 `road_raised_surface`

用途：

- 绿化带
- 分割岛
- 其它需要“面拉起”的对象

几何类型：

- `PolygonZ`
- `MultiPolygonZ`

推荐不要拆成很多表，用一张通用表，通过 `feature_type` 区分：

- `greenbelt`
- `median_island`
- `raised_surface`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 拉起面ID | bigint / varchar(64) | 是 | 唯一标识 |
| feature_type | 拉起面类型 | varchar(64) | 是 | `greenbelt` / `median_island` |
| name | 名称 | varchar(200) | 否 | 名称 |
| geom | 拉起面几何 | geometry(MultiPolygonZ,4326) | 是 | 面几何 |
| height | 拉起高度 | numeric(10,3) | 是 | 默认也建议入库明确化 |
| top_texture_code | 顶面贴图编码 | varchar(64) | 否 | 顶面材质 |
| side_texture_code | 侧面贴图编码 | varchar(64) | 否 | 侧面材质 |
| uv_repeat_x | 顶面X重复米数 | numeric(10,3) | 否 | 平铺参数 |
| uv_repeat_y | 顶面Y重复米数 | numeric(10,3) | 否 | 平铺参数 |

---

### 2.4 路面标志面表 `road_mark_surface`

用途：

- 箭头
- 文字
- 导向符号

几何类型：

- `PolygonZ`
- `MultiPolygonZ`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | Mark ID | bigint / varchar(64) | 是 | 唯一标识 |
| road_id | 关联道路ID | bigint / varchar(64) | 否 | 用于投影道路高程 |
| name | 名称 | varchar(200) | 否 | 名称 |
| geom | Mark几何 | geometry(MultiPolygonZ,4326) | 是 | 面几何 |
| color | 填充颜色 | varchar(32) | 否 | 建议直接存 HEX |
| z_offset | 高程偏移 | numeric(10,3) | 否 | 若有固定抬高可用 |

---

## 3. 线类表设计

---

### 3.1 标线通用表 `road_marking_line`

用途：

- 实线标线
- 虚线标线

几何类型：

- `LineStringZ`
- `MultiLineStringZ`

建议合成一张表，用 `feature_type` 区分：

- `marking_solid`
- `marking_dashed`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 标线ID | bigint / varchar(64) | 是 | 唯一标识 |
| feature_type | 标线类型 | varchar(64) | 是 | `marking_solid` / `marking_dashed` |
| road_id | 关联道路ID | bigint / varchar(64) | 否 | 用于投影高度 |
| geom | 标线几何 | geometry(MultiLineStringZ,4326) | 是 | 线几何 |
| width | 标线宽度 | numeric(10,3) | 是 | 展宽宽度 |
| color | 颜色 | varchar(32) | 否 | 建议存 HEX |
| dash_pattern | 虚线规则 | varchar(64) | 否 | 如 `6,9`、`2,1` |
| startoffset | 起始偏移 | numeric(10,3) | 否 | 虚线起始偏移 |
| texture_code | 贴图编码 | varchar(64) | 否 | 如需贴图可用 |

说明：

- 实线用不到 `dash_pattern`
- 虚线必须有 `dash_pattern`

---

### 3.2 路缘石表 `road_curb`

用途：

- 路缘石

几何类型：

- `LineStringZ`
- `MultiLineStringZ`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 路缘石ID | bigint / varchar(64) | 是 | 唯一标识 |
| geom | 路缘石线 | geometry(MultiLineStringZ,4326) | 是 | 线几何 |
| direction | 展开方向 | integer | 否 | 当前规则：`1` 右，`2` 左 |
| width | 展开宽度 | numeric(10,3) | 是 | 如 `0.13` |
| height | 高度 | numeric(10,3) | 是 | 如 `0.15` |
| texture_code | 贴图编码 | varchar(64) | 否 | 如 `curb_lys` |
| repeat_m | 贴图重复节距 | numeric(10,3) | 否 | 如 `0.5` |

---

### 3.3 通用沿线摆设模型表 `linear_instanced_model`

用途：

- 防眩板
- 沿线重复摆放的小型外部 GLB 模型
- 后续可扩展到轮廓柱、警示桩、小型设备等

几何类型：

- `LineStringZ`
- `MultiLineStringZ`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 摆设模型ID | bigint / varchar(64) | 是 | 唯一标识 |
| feature_type | 模型类型 | varchar(64) | 是 | 如 `anti_glare_board` |
| geom | 布设线 | geometry(MultiLineStringZ,4326) | 是 | 线几何 |
| startoffset | 起始偏移 | numeric(10,3) | 否 | 布设起点偏移 |
| spacing | 间距 | numeric(10,3) | 是 | 默认也建议入库 |
| model_code | 模型编码 | varchar(64) | 是 | 如 `fxb_01` |
| scale_x | X缩放 | numeric(10,3) | 否 | 默认 `1` |
| scale_y | Y缩放 | numeric(10,3) | 否 | 默认 `1` |
| scale_z | Z缩放 | numeric(10,3) | 否 | 默认 `1` |
| offset_x | 横向偏移 | numeric(10,3) | 否 | 相对线方向局部偏移 |
| offset_y | 竖向偏移 | numeric(10,3) | 否 | 相对地面高程偏移 |
| offset_z | 纵向偏移 | numeric(10,3) | 否 | 沿线方向局部偏移 |
| yaw_offset | 角度附加偏移 | numeric(10,3) | 否 | 特殊摆放时可用 |

---

### 3.4 刚性护栏通用表 `barrier_rigid`

用途：

- 新泽西护栏
- 水泥隔离墙

几何类型：

- `LineStringZ`
- `MultiLineStringZ`

建议这两类放一张表，用 `feature_type` 区分：

- `new_jersey`
- `concrete_wall`

原因：

- 都是 profile sweep
- `direction` 规则一致
- 上层参数一致，主要差别只是 profile 类型和贴图

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 刚性护栏ID | bigint / varchar(64) | 是 | 唯一标识 |
| feature_type | 护栏类型 | varchar(64) | 是 | `new_jersey` / `concrete_wall` |
| geom | 护栏线 | geometry(MultiLineStringZ,4326) | 是 | 线几何 |
| direction | 偏移方向 | integer | 否 | 与现有规则一致 |
| width | 底宽 | numeric(10,3) | 是 | 建议明确入库 |
| height | 高度 | numeric(10,3) | 是 | 建议明确入库 |
| profile_code | 截面编码 | varchar(64) | 是 | 如 `new_jersey_std` |
| texture_code | 贴图编码 | varchar(64) | 否 | 如 `xzx`、`snq_01` |
| uv_rotate_deg | 贴图旋转角度 | numeric(10,3) | 否 | 如 `180` |

---

### 3.5 波形护栏通用表 `barrier_wave`

用途：

- 二波护栏
- 三波护栏
- 鼻端护栏

几何类型：

- `LineStringZ`
- `MultiLineStringZ`

建议这三类放一张表，用 `feature_type` 区分：

- `wave_two`
- `wave_three`
- `wave_nose_end`

原因：

- 都属于同一家族
- 参数高度一致
- 只是在板型、是否有立柱、是否有连接器上不同

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 波形护栏ID | bigint / varchar(64) | 是 | 唯一标识 |
| feature_type | 波形护栏类型 | varchar(64) | 是 | `wave_two` / `wave_three` / `wave_nose_end` |
| geom | 护栏线 | geometry(MultiLineStringZ,4326) | 是 | 线几何 |
| direction | 偏移方向 | integer | 否 | 立柱、连接器偏移方向 |
| rail_base_height | 板底标高 | numeric(10,3) | 是 | 建议明确入库 |
| rail_panel_height | 板高 | numeric(10,3) | 是 | 建议明确入库 |
| post_spacing | 立柱间距 | numeric(10,3) | 否 | 鼻端可为空 |
| spacer_depth | 连接器厚度 | numeric(10,3) | 否 | 鼻端可为空 |
| texture_code | 板贴图编码 | varchar(64) | 否 | 如 `bxhl_2`、`sbb`、`bd_01` |
| uv_repeat_m | 贴图重复米数 | numeric(10,3) | 否 | 鼻端当前如 `0.2` |
| has_post | 是否有立柱 | boolean | 是 | 二波、三波 true，鼻端 false |
| has_spacer | 是否有连接器 | boolean | 是 | 二波、三波 true，鼻端 false |

---

### 3.6 声屏障表 `barrier_noise_wall`

用途：

- 顶部向内折弯的声屏障

几何类型：

- `LineStringZ`
- `MultiLineStringZ`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| id | 声屏障ID | bigint / varchar(64) | 是 | 唯一标识 |
| geom | 声屏障线 | geometry(MultiLineStringZ,4326) | 是 | 线几何 |
| direction | 折弯方向 | integer | 否 | 与主板、立柱一致 |
| height | 总高度 | numeric(10,3) | 是 | 建议明确入库 |
| bend_offset | 顶部折弯偏移 | numeric(10,3) | 是 | 建议明确入库 |
| post_spacing | 立柱间距 | numeric(10,3) | 是 | 当前默认 `2.0` |
| post_width | 立柱宽 | numeric(10,3) | 是 | 当前默认 `0.08` |
| post_depth | 立柱厚 | numeric(10,3) | 是 | 当前默认 `0.08` |
| panel_texture_code | 主板贴图编码 | varchar(64) | 否 | 如 `spz_01` |
| post_texture_code | 立柱贴图编码 | varchar(64) | 否 | 如需单独材质可用 |
| is_transparent | 是否透明材质 | boolean | 是 | 当前建议 true |

---

## 4. 推荐资源表

建议单独做一张资源表，避免把贴图路径、模型路径写死在业务表。

表名建议：

- `road_resource`

| English Name | 中文名称 | 字段类型 | 必填 | 说明 |
|---|---|---|---|---|
| resource_code | 资源编码 | varchar(64) | 是 | 唯一编码 |
| resource_name | 资源名称 | varchar(200) | 否 | 名称 |
| resource_type | 资源类型 | varchar(32) | 是 | `texture` / `model` |
| path | 文件路径 | varchar(500) | 是 | 资源路径 |
| remark | 备注 | varchar(500) | 否 | 备注 |

---

## 5. 我对建表方式的建议

你的这个场景，不建议完全按“一个插件一张表”。

更合理的是：

- `road_surface`
- `road_subgrade`
- `road_raised_surface`
- `road_mark_surface`
- `road_marking_line`
- `road_curb`
- `linear_instanced_model`
- `barrier_rigid`
- `barrier_wave`
- `barrier_noise_wall`
- `road_resource`

这里面你刚提到的两个问题，我的建议是：

- 二波板、三波板、鼻端护栏：放一张 `barrier_wave`
- 新泽西护栏、水泥隔离墙：放一张 `barrier_rigid`

这是合理的。

因为从建模逻辑看，它们分别就是两个家族，不需要拆太散。

---

## 6. 后续如果要继续细化

下一步可以继续做两件事之一：

1. 基于这份文档，直接生成 PostgreSQL/PostGIS 建表 SQL
2. 再补一版“字段 -> 程序 BuildType / Material / Profile”的映射表

## PostGIS ?????????????

?? road ??????????**?????**??????????????????? `ST_Intersects(tile_geom, geom)` ???????

### ??

- ??????
- ????????
- ??????????????

### ????

????????????????

- `owner_tile_id` `varchar(64)`???????? ID

??????

- `owner_z` `integer`
- `owner_x` `integer`
- `owner_y` `integer`

??????????

- `owner_tile_id`

?????????

### ?????

#### ???

???????

```sql
ST_LineInterpolatePoint(geom, 0.5)
```

???
- ?????????? 50% ???
- ??????
- ??????????

???????????

#### ???

???????

```sql
ST_PointOnSurface(geom)
```

???
- ???????????
- ??????????

???????????

### ????

1. ??????? `tile_grid`
2. ???????? `owner_tile_id`
3. ?????????? `owner_tile_id = ???`

### SQL ??

#### ???? owner_tile_id

```sql
UPDATE road_marking_line a
SET owner_tile_id = t.tile_id
FROM tile_grid t
WHERE ST_Intersects(t.geom, ST_LineInterpolatePoint(a.geom, 0.5));
```

#### ???? owner_tile_id

```sql
UPDATE road_surface a
SET owner_tile_id = t.tile_id
FROM tile_grid t
WHERE ST_Intersects(t.geom, ST_PointOnSurface(a.geom));
```

#### ????

```sql
SELECT *
FROM road_surface
WHERE owner_tile_id = :tile_id;
```

```sql
SELECT *
FROM road_marking_line
WHERE owner_tile_id = :tile_id;
```

### ?????????????

????

```sql
SELECT *
FROM road_surface
WHERE ST_Intersects(geom, :tile_geom);
```

???
- ???????
- ???????
- ?????????????
- ?????????

### ???????????

?????????
- ???????????????
- ???????????????

??????

- `ST_Intersection(line, tile_geom)`
- `ST_Intersection(polygon, tile_geom)`

?? road ??????????
- ?????
- ?? `owner_tile_id` ??????


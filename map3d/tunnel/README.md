# 隧道面 CDT 生成测试

`TestRoadWallBuildLine` 从已有的三维 Polygon 生成 GLB 和 tileset，墙面、拱顶、平顶、洞口和标牌均使用 CDT；保留输入顶点坐标及绕序，不按 extrude_height 再次拉伸。输入内表面应朝内，洞口保持输入方向。

```powershell
$env:MAP3D_TUNNEL_INPUT = "D:\data\tunnel_walls"
$env:MAP3D_TUNNEL_OUTPUT = "D:\output\tunnel_tileset"
$env:MAP3D_TUNNEL_WALL_TEXTURE = "D:\textures\wall.jpg"
$env:MAP3D_TUNNEL_ROOF_TEXTURE = "D:\textures\roof.jpg"
# 存在 portal_wall 时设置：
$env:MAP3D_TUNNEL_PORTAL_TEXTURE = "D:\textures\portal.png"
go test ./map3d/tunnel -run '^TestRoadWallBuildLine$' -count=1 -v
```

输入可为单个文件或目录，每个文件独立输出。未指定输入时跳过；未指定输出时使用测试临时目录。标牌字体默认使用包目录下的 assets/simkai.ttf，也可用 `MAP3D_TUNNEL_SIGN_FONT` 指定。

CDT 将各个面投影到其面积法线对应的主平面，并将结果索引映射回原始三维顶点，不使用最近点恢复高程。投影自交、退化或重叠点会明确报错，不回退 Earcut。单个面应适合一对一平面投影；空间折叠的面需要先拆分。当前生成入口要求单外环 Polygon，不会静默丢弃孔洞或 MultiPolygon 的其他部分。

左右墙若由对应的上下边界构成，会按相邻断面拆成局部四边形分别执行 CDT，索引映射回原始三维点，避免长墙整面投影引起交叉或跨弯三角形。上下边界要求反向排列、成对点在 ENU 水平距离不超过 2 厘米且高差方向一致；不满足结构时使用通用平面 CDT。曲面墙使用面积加权顶点法线；拱顶各片剖分后保留原有顶点焊接和平滑法线处理。

`MAP3D_TUNNEL_FIXTURES` 指定包含 tunnel_*_walls.geojson 的目录，可运行 `TestTunnelCDTFixtures` 检查实际输入。`map3d/mgltf` 中的确定性单元测试覆盖三个投影方向、反向绕序、凹多边形、闭合环、面积及退化输入。

贴图按面类型分配：`left_wall`、`right_wall` 使用 `MAP3D_TUNNEL_WALL_TEXTURE`；`arched_roof`、`flat_roof` 使用 `MAP3D_TUNNEL_ROOF_TEXTURE`。未配置 roof 时复用 wall 材质，洞口仍使用独立的 portal 贴图。

相邻的完全相同三维点及闭合重复点会在剖分前去除；CDT 接口返回的索引仍指向调用者原始顶点。仅投影重合而三维坐标不同的点仍报错，不会自动合并。生成入口对去重后不足三个顶点的退化面记录日志并跳过。

## 可选的隧道中线

```powershell
$env:MAP3D_TUNNEL_CENTERLINE = "D:\data\tunnel_centerlines.geojson"
$env:MAP3D_TUNNEL_TEXTURE_REPEAT_U = "2"   # 纵向每 2 米重复一次
$env:MAP3D_TUNNEL_TEXTURE_REPEAT_V = "1"   # 横向每 1 米重复一次
```

中线文件可以包含多条隧道，但每个 `group_id` 必须只有一个连续的 `LineString` / `MultiLineString`（支持 XY 或 XYZ），方向与该隧道一致；编号可以是字符串或数字。中线、墙面均为 WGS84 经纬度。中线 Z 可为地面高度；不作为墙底高程。

启用后，按每个面属性中的 `group_id` 精确匹配，缺失编号、重复中线、无匹配中线会报错。只影响左右墙、拱顶和平顶，洞口和标牌保留自己的 UV。没有配置中线时沿用原来的输出方式。

- 纵向 U 使用点在水平中线最近线段上的投影里程，除以纵向重复长度。两侧墙和屋顶使用同一里程参考。
- 墙面 V 使用相对配对墙底的高度；非配对墙面使用该面的最低高度。
- 屋顶 V 使用横断面三维边长的累计距离，包含拱顶端部竖向过渡带；横断面起点取中线有符号横向坐标较小的一端。支持当前生成器输出的四边形屋顶片段；`from_station` / `to_station` 存在时采用当前生成器的顶点顺序（前两点为纵向边），否则按中线里程识别纵横边。普通横断面从左端累计三维弧长；末端极短片段焊接或收缩为三角形时，保留原四边形的断面关联，并从左侧端点计算最短弧长，避免纹理绕回。
- CDT 继续使用已有墙面断面和拱顶分片作为约束，保持三维位置、绕序及法线；不会按照中线重新移动或重采样表面。中线控制 UV 的纵向参考，现有局部分片限制三角形的跨度。

重复长度单位为米，默认均为 1，必须是正有限数。里程使用水平 ENU 距离；急弯或回折处若最近中线线段存在歧义，需要输入与隧道对应的连续单向中线，或进一步按断面里程建立显式关联。

### MultiLineString 输入

同一 Feature 的所有分段共享一个 `group_id`。分段必须按隧道前进方向排列，并保持方向一致；相邻分段首尾的三维距离不得超过 0.02 米。连接点只保留一次，里程和贴图 U 跨段连续累计，不会从零重新开始。加载器不会自动反转或重排分段。

```json
{
  "type": "Feature",
  "properties": { "group_id": "17" },
  "geometry": {
    "type": "MultiLineString",
    "coordinates": [
      [[116.270, 30.845, 400], [116.271, 30.846, 401]],
      [[116.271, 30.846, 401], [116.272, 30.847, 402]]
    ]
  }
}
```

Feature 放在原有 FeatureCollection 中即可。空分段、断开的分段、反向/乱序接缝、分叉端点和闭合多段中线会报错，避免错误连接或里程跳变。

## 两侧平台

上传的平台生成测试已整合为 `TestRoadPlatformBuildLine`：

```powershell
$env:MAP3D_TUNNEL_PLATFORM_INPUT = "D:\data\platform"
$env:MAP3D_TUNNEL_PLATFORM_OUTPUT = "D:\output\platform_tileset"
$env:MAP3D_TUNNEL_CENTERLINE = "D:\data\tunnel_centerlines.geojson"
$env:MAP3D_TUNNEL_PLATFORM_TOP_TEXTURE = "D:\textures\square_tile.jpg"
$env:MAP3D_TUNNEL_PLATFORM_WALL_TEXTURE = "D:\textures\platform_wall.jpg"
$env:MAP3D_TUNNEL_PLATFORM_TILE_SIZE = "0.5"
go test ./map3d/tunnel -run '^TestRoadPlatformBuildLine$' -count=1 -v
```

输入支持单个 GeoJSON 文件或目录；每个文件输出独立 GLB 和 tileset。未指定输入时跳过，未指定输出时使用临时目录。沿用墙面的中线配置和 `group_id` 匹配规则，支持连续有序的 MultiLineString。面按 `group_id`、`side`、`surface_type` 分组，避免不同隧道合并。仅接受单外环 Polygon，孔洞和 MultiPolygon 会明确报错。

所有平台面使用 CDT，保留原始三维坐标与绕序。内外侧壁采用与墙面相同的上下边界局部分片；顶面在能识别中线同里程的两侧边界配对时，也按相邻断面局部分片，其他形状使用通用 CDT。此过程不会按中线移动或重新采样输入模型。

`platform_top` 的 UV 为 `U=中线水平里程/TileSize`、`V=中线有符号横向距离/TileSize`，跨平台片段连续累计，显式启用 REPEAT 采样。默认 TileSize 为 0.5 米，即一张完整贴图对应水平投影中的 0.5×0.5 米，不会将整个长条压缩成一张图片。若图片中包含多块砖，参数表示整张图片覆盖的尺寸，而不是单块砖的尺寸。弯道内外侧长度不同，按中心线坐标铺贴会随弯道产生一定形变。

侧壁 U 沿中线里程、V 相对墙底高度，默认每米重复一次；洞口式端面等非配对立面以面最低高度为 V 原点。未指定中线时，顶面按局部 XZ 米制坐标平铺，侧面保留原有条带投影。平台顶面和侧面贴图独立配置。

回归测试验证方砖纵横比例、片段接缝连续、局部 CDT 索引、独立贴图、REPEAT 采样、MultiLineString 中线生成完整 tileset，以及缺少匹配编号或非法砖尺寸时报错。当前未提供真实平台 GeoJSON，完整导出使用合成平台验证。

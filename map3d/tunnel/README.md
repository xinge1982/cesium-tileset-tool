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

每个墙面独立剖分，曲面墙使用面积加权顶点法线；拱顶各片剖分后保留原有顶点焊接和平滑法线处理。

`MAP3D_TUNNEL_FIXTURES` 指定包含 tunnel_*_walls.geojson 的目录，可运行 `TestTunnelCDTFixtures` 检查实际输入。`map3d/mgltf` 中的确定性单元测试覆盖三个投影方向、反向绕序、凹多边形、闭合环、面积及退化输入。

贴图按面类型分配：`left_wall`、`right_wall` 使用 `MAP3D_TUNNEL_WALL_TEXTURE`；`arched_roof`、`flat_roof` 使用 `MAP3D_TUNNEL_ROOF_TEXTURE`。未配置 roof 时复用 wall 材质，洞口仍使用独立的 portal 贴图。

相邻的完全相同三维点及闭合重复点会在剖分前去除；CDT 接口返回的索引仍指向调用者原始顶点。仅投影重合而三维坐标不同的点仍报错，不会自动合并。生成入口对去重后不足三个顶点的退化面记录日志并跳过。

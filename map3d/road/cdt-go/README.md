# CDT Go Binding（Windows / Linux）

基于 [artem-ogre/CDT](https://github.com/artem-ogre/CDT) 和 [ebitengine/purego](https://github.com/ebitengine/purego) 的 Go 封装，支持在 **Windows** 和 **Linux** 下调用原生动态库完成二维受约束 Delaunay 三角剖分。

本项目适合以下场景：

- 道路面、匝道、桥面等二维参数域三角剖分
- 多边形边界 + 约束边的网格生成
- 带孔洞区域的三角剖分
- 点集凸包范围内的普通三角剖分
- Go 侧做业务逻辑，C++ 侧只负责 triangulation 内核

---

## 1. 功能特性

当前支持：

- `TriangulateInside`  
  对封闭约束边界内部做三角剖分

- `TriangulateWithHoles`  
  对包含孔洞的区域做三角剖分

- `ConvexHullTriangulate`  
  对点集做凸包范围内的三角剖分

- `CleanInput`  
  输入预处理，包括：
  - 去重点
  - 边索引重映射
  - 去除零长度边
  - 去除重复边
  - 检测非法相交边

- `BoundaryEdges`  
  从三角形集合中提取边界边

---

## 2. 项目结构

建议目录结构如下：

```text
project-root/


│  
├─ cdt/
    ─ bin/
│     ├─ windows/
│     │  └─ cdt_bridge.dll
│     └─ linux/
│        └─ cdt_bridge.so
│  ├─ binding.go
│  ├─ binding_windows.go
│  ├─ binding_linux.go
│  └─ binding_test.go
├─ go.mod
├─ main.go
└─ README.md
```

如果你当前项目把动态库放在 `bin/` 目录，也可以，但要和 `libraryPath()` 里的路径保持一致。

---

## 3. 环境要求

### 3.1 Go

推荐：

- Go 1.22 或更高

### 3.2 Windows

推荐环境：

- Windows 10 / 11
- CLion
- CLion 自带的 Bundled MinGW 工具链

### 3.3 Linux

推荐环境：

- Ubuntu / Debian / WSL
- `g++`
- `make` / `cmake`（本项目最小编译不强依赖 cmake）

---

## 4. 依赖说明

### 4.1 C++ 侧

底层 triangulation 使用：

- `artem-ogre/CDT`

### 4.2 Go 侧

动态库调用使用：

- `github.com/ebitengine/purego`
- `golang.org/x/sys/windows`（仅 Windows）

---

## 5. 原生库接口说明

C++ 侧通过 `native/cdt_bridge.h` 暴露 C ABI：

```c
int cdt_triangulate(
    const CDTPoint2* points,
    int point_count,
    const CDTEdge* edges,
    int edge_count,
    int mode,
    CDTResult* out_result
);

void cdt_free_result(CDTResult* result);
```

### 5.1 mode 含义

- `0`：保留约束边界内部区域  
  对应 `eraseOuterTriangles()`

- `1`：保留内部区域，并去除孔洞  
  对应 `eraseOuterTrianglesAndHoles()`

- `2`：保留凸包范围内三角剖分  
  对应 `eraseSuperTriangle()`

---

## 6. Windows 下编译 `cdt_bridge.dll`

### 6.1 CLion 配置

打开 CLion 后确认：

- **文件** → **设置**
- **构建、执行、部署** → **工具链**
- 选择 `Bundled MinGW`

然后确认以下项都是可用的：

- C 编译器
- C++ 编译器
- 调试器
- CMake
- Make

### 6.2 CMake 配置

在工程的 `CMakeLists.txt` 中增加：

```cmake
add_library(cdt_bridge SHARED
    native/cdt_bridge.cpp
)

target_include_directories(cdt_bridge PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/CDT/include
    ${CMAKE_CURRENT_SOURCE_DIR}/native
)

set_target_properties(cdt_bridge PROPERTIES
    OUTPUT_NAME "cdt_bridge"
)

target_compile_features(cdt_bridge PRIVATE cxx_std_17)

if (WIN32)
    target_link_options(cdt_bridge PRIVATE
        -static-libgcc
        -static-libstdc++
    )
endif()
```

### 6.3 构建

在 CLion 中执行：

- **工具** → **CMake** → **重新加载 CMake 项目**
- **构建** → **重新构建 `cdt_bridge`**

成功后通常会生成：

```text
cmake-build-debug/cdt_bridge.dll
```

或：

```text
cmake-build-release/cdt_bridge.dll
```

然后将其复制到：

```text
native/bin/windows/cdt_bridge.dll
```

### 6.4 Windows 运行库问题

如果 `LoadLibrary failed`，通常是因为 DLL 依赖的运行库找不到或版本不匹配。

优先建议：

1. 使用和 CLion 构建时**同一套** MinGW 运行库
2. 尽量使用上面的静态链接选项
3. 必要时将以下文件复制到 DLL 同目录：
   - `libstdc++-6.dll`
   - `libgcc_s_seh-1.dll`
   - `libwinpthread-1.dll`

---

## 7. Linux / WSL 下编译 `libcdt_bridge.so`

进入项目目录，例如：

```bash
cd /mnt/c/MapABC/code/CDT/CDT
```

安装编译器：

```bash
sudo apt update
sudo apt install -y g++
```

创建输出目录：

```bash
mkdir -p native/bin/linux
```

编译：

```bash
g++ -std=c++17 -O2 -fPIC -shared \
  native/cdt_bridge.cpp \
  -I./CDT/include \
  -o native/bin/linux/libcdt_bridge.so
```

检查结果：

```bash
ls native/bin/linux
file native/bin/linux/libcdt_bridge.so
```

如果看到 `ELF 64-bit LSB shared object`，说明构建成功。

---

## 8. Go 模块配置

`go.mod` 示例：

```go
module road-cdt-demo

go 1.22

require (
    github.com/ebitengine/purego v0.10.0
    golang.org/x/sys v0.31.0
)
```

安装依赖：

```bash
go mod tidy
```

---

## 9. Go 封装接口

### 9.1 类型定义

```go
type Point2 struct {
    X float64
    Y float64
}

type Edge struct {
    A int32
    B int32
}

type Triangle struct {
    A int32
    B int32
    C int32
}
```

### 9.2 高层接口

#### `TriangulateInside`

对封闭边界内部进行三角剖分。

```go
pts, tris, err := cdt.TriangulateInside(points, edges)
```

#### `TriangulateWithHoles`

对带孔洞区域进行三角剖分。

```go
pts, tris, err := cdt.TriangulateWithHoles(points, edges)
```

#### `ConvexHullTriangulate`

对点集做凸包范围内的三角剖分，不依赖约束边。

```go
pts, tris, err := cdt.ConvexHullTriangulate(points)
```

#### `CleanInput`

做输入清洗。

```go
clean, err := cdt.CleanInput(points, edges)
if err != nil {
    panic(err)
}

fmt.Println(len(clean.Points), len(clean.Edges))
```

#### `BoundaryEdges`

从三角形提取边界边。

```go
boundary := cdt.BoundaryEdges(tris)
```

---

## 10. 最小使用示例

`main.go` 示例：

```go
package main

import (
    "fmt"
    "log"

    "road-cdt-demo/cdt"
)

func main() {
    defer func() {
        if err := cdt.Close(); err != nil {
            log.Printf("close native library failed: %v", err)
        }
    }()

    points := []cdt.Point2{
        {X: 0, Y: 0},
        {X: 10, Y: 0},
        {X: 10, Y: 10},
        {X: 0, Y: 10},
    }

    edges := []cdt.Edge{
        {A: 0, B: 1},
        {A: 1, B: 2},
        {A: 2, B: 3},
        {A: 3, B: 0},
    }

    outPoints, outTriangles, err := cdt.TriangulateInside(points, edges)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("points:")
    for i, p := range outPoints {
        fmt.Printf("  %d: (%.3f, %.3f)\n", i, p.X, p.Y)
    }

    fmt.Println("triangles:")
    for i, t := range outTriangles {
        fmt.Printf("  %d: [%d %d %d]\n", i, t.A, t.B, t.C)
    }
}
```

运行：

```bash
go run .
```

---

## 11. 带孔洞示例

```go
points := []cdt.Point2{
    {X: 0, Y: 0},
    {X: 10, Y: 0},
    {X: 10, Y: 10},
    {X: 0, Y: 10},
    {X: 3, Y: 3},
    {X: 7, Y: 3},
    {X: 7, Y: 7},
    {X: 3, Y: 7},
}

edges := []cdt.Edge{
    {A: 0, B: 1},
    {A: 1, B: 2},
    {A: 2, B: 3},
    {A: 3, B: 0},
    {A: 4, B: 5},
    {A: 5, B: 6},
    {A: 6, B: 7},
    {A: 7, B: 4},
}

pts, tris, err := cdt.TriangulateWithHoles(points, edges)
```

---

## 12. 凸包三角剖分示例

```go
points := []cdt.Point2{
    {X: 0, Y: 0},
    {X: 10, Y: 0},
    {X: 10, Y: 10},
    {X: 0, Y: 10},
}

pts, tris, err := cdt.ConvexHullTriangulate(points)
```

---

## 13. 输入清洗说明

`CleanInput` 当前会做以下操作：

### 13.1 去重点

使用 `epsilon = 1e-9` 对点做量化判重。

### 13.2 边重映射

如果点去重后索引变化，边会自动重映射。

### 13.3 去除零长度边

如果一条边的两个端点映射到同一个点，则删除。

### 13.4 去除重复边

如果同一条边重复出现，只保留一条。

### 13.5 相交检查

如果两条边在非公共端点处发生相交，会返回错误。

---

## 14. 单元测试

测试文件：

```text
cdt/binding_test.go
```

运行全部测试：

```bash
go test ./cdt -v
```

只运行某个测试：

```bash
go test ./cdt -run TestTriangulateInside_Square -v
```

当前推荐测试覆盖：

- 简单正方形区域
- 带孔洞区域
- 点去重
- 重复边
- 相交边检测
- 凸包三角剖分
- 边界边提取

---

## 15. 常见问题

### 15.1 `LoadLibrary failed: The specified procedure could not be found.`

常见原因：

- DLL 本身依赖的运行库找不到
- 运行库版本和构建工具链不匹配
- 复制了错误版本的 `libstdc++-6.dll`

建议：

- 优先使用 CLion 同一套 MinGW 运行库
- 尽量使用 `-static-libgcc -static-libstdc++`
- 用 `objdump` 或 `Dependencies` 检查依赖

示例：

```bat
"C:\Program Files\JetBrains\CLion 2023.1\bin\mingw\bin\objdump.exe" -p cdt_bridge.dll | findstr "DLL Name"
```

### 15.2 `purego: struct arguments are only supported on darwin and linux`

原因：

- Windows 下 purego 不支持带 struct 返回值的函数 ABI

本项目已通过以下方式规避：

- C 接口返回 `int`
- `CDTResult` 通过输出参数传递

### 15.3 Linux 编译报 `__declspec(dllexport)` 错误

原因：

- `__declspec(dllexport)` 是 Windows 特有语法

解决：

- 使用跨平台导出宏：

```c
#ifdef _WIN32
#define CDT_BRIDGE_EXPORT __declspec(dllexport)
#else
#define CDT_BRIDGE_EXPORT
#endif
```

### 15.4 `CDT::V2d<double>::make` 不存在

原因：

- 你当前使用的 CDT 版本没有这个接口

解决：

直接写：

```cpp
CDT::V2d<double> v;
v.x = x;
v.y = y;
```

---

## 16. 道路面/匝道场景建议

对于三维道路面，推荐流程不是直接对 XYZ 做 3D 剖分，而是：

1. 将道路 patch 参数化到二维域 `(s, t)`
2. 在 `(s, t)` 平面中构建边界与 breakline
3. 用本库做二维受约束三角剖分
4. 将结果顶点再映射回三维道路面

这套方式更适合：

- 匝道
- 桥面
- 带超高/横坡的道路面
- 孔洞、拼接边、分块 patch

---

## 17. 后续可扩展方向

你可以在此基础上继续扩展：

- OBJ / glTF 导出
- 三角形邻接关系计算
- 网格质量检查
- 道路 patch 自动分块
- breakline 自动生成
- 边相交自动拆分
- 更细粒度的尺寸场控制

---

## 18. 许可证与致谢

底层 triangulation 使用：

- `artem-ogre/CDT`

动态加载库使用：

- `ebitengine/purego`

请根据各自上游仓库的许可证要求进行分发和使用。

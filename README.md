# 飞牛 NAS 影音库统计（fnOS fpk 版）

在飞牛 fnOS 上以**应用（fpk）**形式安装运行，由 NAS 本机只读扫描 `/vol*` 下的影视目录，生成一份自包含的影音库统计报表：作品数、容量、分类、卷分布、分辨率/编码/来源构成、季集结构、缺集与重复文件、字幕覆盖等。

界面由应用内置的 Web 页面提供，装好后从 fnOS 桌面图标或 `http://<NAS地址>:8090` 直接打开，不需要在 Windows 上装客户端，也不需要 SSH 远程采集。

> 本仓库有两个长期共存的分支：
>
> | 分支 | 形态 | 适用场景 |
> |---|---|---|
> | `main` | Windows 单文件 GUI 程序（WebView2），通过 SSH **远程**采集 NAS 目录 | 想在 Windows 电脑上看报表、不想在 NAS 上装应用 |
> | `fnos`（本分支） | 飞牛 fnOS 应用包（.fpk），在 NAS **本机**扫描 | 想在 NAS 上装一个应用、用浏览器在任何设备上看报表 |
>
> 两者算法部分（`parse.go` / `analyze.go` / `report.go`）同源，报表口径一致。

![Go](https://img.shields.io/badge/Go-1.21-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/Platform-fnOS%20x86%20%2F%20arm-4E5AE8)
![License](https://img.shields.io/badge/License-MIT-green)

## 特性

- **纯本地只读扫描**：`filepath.WalkDir` 遍历授权目录，仅读取文件名、大小、时间，不写入、不修改媒体库任何文件
- **零外部依赖**：仅使用 Go 标准库，静态单文件二进制，无 CGO、无运行时
- **应用化集成**：符合 fnOS fpk 规范，提供桌面图标、服务端口注册、生命周期脚本（安装/启动/停止/升级/卸载）
- **丰富维度**：分类、媒体库、卷、年份、分辨率、编码、片源、季数分布、字幕覆盖、Top 大文件、最近入库
- **结构体检**：自动识别缺集、重复文件、多版本、命名异常
- **可分发报表**：落盘自包含 HTML（单文件、无外链）与两份 CSV，页面内可直接下载

## 报表内容

| 模块 | 说明 |
|---|---|
| 概览 | 作品数、视频文件数、总容量、平均单片/单集体积、字幕覆盖率 |
| 分类统计 | 电影 / 剧集 / 动漫等分类的作品数与容量 |
| 卷与媒体库分布 | 各存储卷、各媒体库的占用情况 |
| 年份 / 季数分布 | 作品年份趋势、剧集季数结构分布 |
| 规格统计 | 分辨率（4K/1080P/720P）、视频编码、片源（BluRay/WEB-DL/HDTV 等）、音轨、HDR |
| 作品明细 | 逐作品列出季数、集数、容量、所在卷、缺集、重复、结构异常，支持搜索与筛选 |

## 目录结构

```
.
├── main.go                  # HTTP 服务、路由、配置读写（服务端口 8090）
├── scan.go                  # 本地目录遍历（filepath.WalkDir），产出文件清单
├── parse.go                 # 从文件名/路径解析片名、年份、季集、分辨率、编码、片源
├── analyze.go               # 聚合为作品维度，缺集、重复、结构异常、各类分布
├── report.go                # html/template 渲染自包含报表，导出两份 CSV
├── assets/
│   ├── app.html             # 应用界面（编译期 go:embed 内嵌）
│   └── report.html.tmpl     # 报表模板（编译期内嵌）
├── fpk/                     # fnOS 应用包骨架
│   ├── manifest             # 应用元信息（版本、端口、授权路径开关等）
│   ├── app/
│   │   ├── bin/             # 构建产物：linux 静态二进制（不入库）
│   │   └── ui/              # 桌面图标与入口配置
│   ├── cmd/                 # 生命周期脚本：install / start / stop / upgrade / uninstall
│   ├── config/
│   │   ├── privilege        # 运行身份（run-as: root）
│   │   └── resource         # 端口与数据共享目录声明
│   └── ICON.PNG, ICON_256.PNG
├── tools/package.py         # 打包脚本（把 fpk/ 封装成 .fpk，不依赖 fnpack）
├── build.sh                 # 一键构建：交叉编译 + 打包
└── docs/fnos-install.md     # 安装、授权与排障
```

## 构建

环境要求：Go 1.21+、Python 3（仅打包用，无需 fnpack）。可在任意 Linux、WSL 或 NAS 本机上构建。

```bash
./build.sh            # 默认 linux/amd64
./build.sh arm64      # ARM 机型
```

产物：`dist/mediareporter_<版本>_x86.fpk`（arm64 为 `_arm.fpk`）。

也可手动执行等价步骤：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o fpk/app/bin/media-reporter .
python3 tools/package.py amd64
```

`assets/` 下的 HTML 与模板通过 `go:embed` 编译进二进制，打包时无需单独携带。

## 安装

详见 [docs/fnos-install.md](docs/fnos-install.md)，要点：

1. 把 `.fpk` 上传到 NAS；
2. 在 fnOS「应用中心 → 手动安装」选择该文件，或在 SSH 下以应用中心服务账号执行 `appcenter-cli install-fpk <文件>`；
3. 安装后在应用设置里**授权可访问的媒体目录**（`/vol*` 下的共享文件夹）；
4. 打开应用（或访问 `http://<NAS地址>:8090`），选择扫描根目录，点「开始扫描」。

## 使用

1. 打开界面，左侧勾选/填写扫描根目录（默认自动探测 `/volN/1000`）；
2. 点「开始扫描」，日志区实时显示进度；
3. 完成后右侧出报表，并可下载 HTML / CSV 到本地；
4. 报表同时写入应用数据目录（`$TRIM_PKGVAR/data/reports`），可在应用「数据」共享目录中查看。

## 接口

应用在本机 `8090` 端口提供以下接口（供界面调用，也可脚本化使用）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/env` | 卷列表、数据目录、版本、架构 |
| GET/POST | `/api/settings` | 读取/保存扫描根目录 |
| GET | `/api/browse?path=` | 只读列目录（限制在 `/vol*` 之下） |
| POST | `/api/scan` | 启动扫描，body 为 `{"roots":["/vol1/1000"]}` |
| GET | `/api/status` | 扫描进度与日志 |
| GET | `/api/report` | 当前报表 HTML |
| GET | `/api/reports` | 已生成的报表文件列表 |
| GET | `/api/download?f=` | 下载报表文件 |
| GET | `/api/health` | 健康检查 |

## 安全与隐私

- 扫描**只读**：仅调用目录遍历与文件属性读取，不写入、不修改、不移动媒体文件
- 服务仅监听局域网内的应用端口，不对外上报任何数据；报表与配置全部保存在 NAS 本地
- `.gitignore` 已排除二进制、`app.tgz`、`.fpk` 与运行期数据目录，避免把构建产物和真实媒体清单提交到仓库

## License

[MIT](LICENSE)

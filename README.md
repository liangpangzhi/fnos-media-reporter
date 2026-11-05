---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 2f12ef080060517e9dd433d82e19a198_d4bb81cad91f11f19232525400248c00
    ReservedCode1: 4pUnEDuER3IhfNTMcq6MmFTc5JE9dGKHYd2kFvmTNZnAfBzGNvYTsXWwYlKWk45m256yHN8bkK9hVBbp5cy/JiDzjnsiqgO998D9QC2yQNUwzCLOYC8EmsqrogVW5AkSAijmp6/MpIHRg5SZYEuhd5kOuze86TfMRT2MP70+znurkSdz4HSym92Jxns=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 2f12ef080060517e9dd433d82e19a198_d4bb81cad91f11f19232525400248c00
    ReservedCode2: 4pUnEDuER3IhfNTMcq6MmFTc5JE9dGKHYd2kFvmTNZnAfBzGNvYTsXWwYlKWk45m256yHN8bkK9hVBbp5cy/JiDzjnsiqgO998D9QC2yQNUwzCLOYC8EmsqrogVW5AkSAijmp6/MpIHRg5SZYEuhd5kOuze86TfMRT2MP70+znurkSdz4HSym92Jxns=
---

# 飞牛 NAS 影音库统计（FnosMediaReporterGUI）

通过 SSH **只读**扫描飞牛 fnOS（或任意 Linux）NAS 上的影视目录，在 Windows 本地生成一份自包含的影音库统计报表：作品数、容量、分类、卷分布、分辨率/编码/来源构成、季集结构、缺集与重复文件、字幕覆盖等。

单文件 GUI 程序，界面内置，无需浏览器、无需命令行。

![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/Platform-Windows%2010%2F11%20x64-0078D6?logo=windows&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)

## 特性

- **只读采集**：远端仅执行 `find` 列目录与 `stat` 取属性，不写入、不修改 NAS 上任何文件
- **GUI 一体**：内嵌 WebView2 窗口，左侧填连接信息，右侧直接渲染报表
- **丰富维度**：分类、媒体库、卷、年份、分辨率、编码、片源、季数分布、字幕覆盖、Top 大文件、最近入库
- **结构体检**：自动识别缺集、重复文件、多版本、命名异常
- **可分发报表**：一键落盘自包含 HTML（单文件、无外链）与两份 CSV
- **链路自检**：内置 MTU 检测，识别 Tailscale 等虚拟网卡导致“SSH 能连上但传不动”的典型故障，并提供一键修复

## 报表内容

| 模块 | 说明 |
|---|---|
| 概览 | 作品数、视频文件数、总容量、平均单片/单集体积、字幕覆盖率 |
| 分类统计 | 电影 / 剧集 / 动漫等分类的作品数与容量 |
| 卷与媒体库分布 | 各存储卷、各媒体库的占用情况 |
| 年份 / 季数分布 | 作品年份趋势、剧集季数结构分布 |
| 规格统计 | 分辨率（4K/1080P/720P）、视频编码、片源（BluRay/WEB-DL/HDTV 等）、音轨、HDR |
| 作品明细 | 逐作品列出季数、集数、容量、所在卷、缺集、重复、结构异常，支持搜索与筛选 |

## 构建

环境要求：Go 1.21+（本项目使用 1.27.3）、Windows x64。

```bash
go mod tidy
go build -trimpath -ldflags "-H windowsgui -s -w" -o dist/FnosMediaReporterGUI.exe .
```

产物为单个 `FnosMediaReporterGUI.exe`，无需安装。`assets/` 下的 HTML 与模板通过 `go:embed` 编译进可执行文件，不可删除。

## 使用

1. 双击 `FnosMediaReporterGUI.exe`；
2. 填写 NAS 地址、端口（默认 22）、用户名、密码（SSH 免密场景可留空）、扫描根目录（默认 `/vol1/1000` ~ `/vol8/1000`）；
3. 点「测试连接」确认链路可达；
4. 点「开始采集」，日志区实时显示进度，完成后右侧直接出报表；
5. 报表同时写入程序同目录 `reports\`：`影音库统计_<时间>.html`、`_作品清单.csv`、`_文件清单.csv`。

详细步骤与排障见 [docs/usage.md](docs/usage.md)。

## 实现要点

- `main.go`：本地 HTTP 服务（仅监听 `127.0.0.1` 随机端口）+ WebView2 窗口，WebView2 不可用时回退到浏览器 `--app` 模式
- `ssh.go`：基于 `golang.org/x/crypto/ssh` 的连接、批量 `find`/`stat` 采集与吞吐测速；密码为空时走 `none` 认证以适配 Tailscale SSH
- `parse.go`：从文件名/路径解析片名、年份、季集、分辨率、编码、片源、音轨、HDR
- `analyze.go`：聚合为作品维度，计算缺集、重复、结构异常与各类分布
- `report.go`：`html/template` 渲染自包含报表，导出两份 CSV
- `diag.go`：检测 Tailscale/WireGuard 虚拟网卡 MTU，异常时提示并支持一键修复
- `assets/app.html`、`assets/report.html.tmpl`：界面与报表模板，编译期内嵌

## 安全与隐私

- 连接信息保存在程序同目录 `settings.json`；密码默认**不落盘**，需勾选「记住密码」才会保存（文件权限 0600）
- 程序仅使用本机回环端口通信，不对外监听、不上传任何数据
- `.gitignore` 已排除 `settings.json`、`reports/`、`*.exe`，避免把连接信息与真实媒体数据提交到仓库

## 常见问题

| 现象 | 处理 |
|---|---|
| 测试连接超时 | 确认目标在线、SSH 端口开放；Tailscale 场景确认已登录 |
| 能连上但吞吐接近 0 | 典型 MTU 不匹配，点击界面提示条的「一键修复」将虚拟网卡 MTU 调至 1200 |
| 提示认证失败 | 目标是常规 sshd 密码登录，需填写正确密码 |
| 采集结果偏少 | 检查扫描根目录是否漏了某些卷 |

## License

[MIT](LICENSE)
*（内容由AI生成，仅供参考）*

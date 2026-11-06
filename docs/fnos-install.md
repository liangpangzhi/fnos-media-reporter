---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 2f12ef080060517e9dd433d82e19a198_8ab2e66ed9a611f19232525400248c00
    ReservedCode1: eREL7XwwUytyJTbaKYtkb0KW4i3NDuxbFOEeRGla6ySbsoJ8qvHABXguAVtCan0mIlEpsXQsyZWR5Wo//Ad3XBCzorcfmWPojl+XyGYeq3e6q+uGetxni9g+L9h8MWvVy/37oKkESQ5CYlghtWqlT9o6pX92idqb3La/+EFSAEpRVlZO2piUlWuc2ro=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 2f12ef080060517e9dd433d82e19a198_8ab2e66ed9a611f19232525400248c00
    ReservedCode2: eREL7XwwUytyJTbaKYtkb0KW4i3NDuxbFOEeRGla6ySbsoJ8qvHABXguAVtCan0mIlEpsXQsyZWR5Wo//Ad3XBCzorcfmWPojl+XyGYeq3e6q+uGetxni9g+L9h8MWvVy/37oKkESQ5CYlghtWqlT9o6pX92idqb3La/+EFSAEpRVlZO2piUlWuc2ro=
---

# 飞牛 fnOS 安装、授权与排障

本文对应 `fnos` 分支的 fpk 应用（应用名 `mediareporter`，服务端口 `8090`）。

## 1. 安装

### 方式一：应用中心手动安装

1. 将 `dist/mediareporter_<版本>_x86.fpk` 上传到 NAS 任一目录；
2. 打开 fnOS「应用中心」→ 右上角「手动安装」，选择该 `.fpk` 文件，按提示完成。

### 方式二：SSH 命令行安装

fnOS 的应用中心 RPC 服务（`/run/trim_app_cgi/rpcbroker`）对应用中心服务账号开放，**直接以 root 执行会报 ACL 权限错误**，需要切换到 `trim-media` 身份：

```bash
sudo -u trim-media appcenter-cli install-fpk /path/to/mediareporter_1.0.2_x86.fpk
```

升级与卸载同理，使用 `appcenter-cli` 对应子命令，或直接在应用中心操作。

## 2. 授权媒体目录

`fpk/manifest` 中 `disable_authorization_path = false`，表示启用 fnOS 的**授权目录**机制：应用默认只能看到被显式授权的共享文件夹，未授权的路径会表现为「读取失败」或扫描结果为空。

安装后在「应用中心 → 影音库统计 → 设置 / 权限」中勾选需要统计的共享文件夹（通常是 `/vol1/1000` 一类用户主目录下的影视目录）。

如需命令行补授权，使用 fnOS 自带的 `trimacl`（**不要用 setfacl**，普通 ACL 不生效于 Trim-ACL 内核模块）：

```bash
# 用 trimacl 为目标目录补 r-x，并为各级祖先目录补 x（参数以 trimacl --help 为准）
sudo /usr/trim/bin/trimacl --help
```

授权可访问性可用只读接口快速验证：

```bash
curl "http://127.0.0.1:8090/api/browse?path=/vol1/1000"
```

返回 `{"path":...,"items":[...]}` 即表示可读；返回 `读取失败（可能未授权）` 则需要回到上一步补授权。

## 3. 运行身份

`fpk/config/privilege` 设置 `run-as: root`。媒体库目录在 fnOS 上普遍是 `750` 权限、属主为创建该目录的普通用户，普通应用身份无法完整遍历；改为 root 运行是保证扫描不因个别子目录权限中断的最简方案。

应用本身对媒体库**只读**：仅遍历目录、读取文件名/大小/修改时间，不写入、不修改、不移动任何媒体文件。数据与报表统一写入 `$TRIM_PKGVAR/data`。

## 4. 使用

1. 打开应用图标，或浏览器访问 `http://<NAS地址>:8090`；
2. 左侧自动探测 `/volN/1000`，按需增删扫描根目录；
3. 点「开始扫描」，右侧日志实时显示进度；
4. 完成后出报表，可下载 HTML / CSV。

报表落盘位置：`$TRIM_PKGVAR/data/reports/`。

## 5. 排障

| 现象 | 原因与处理 |
|---|---|
| 安装报 `permission denied` / `rpcbroker` 相关 | 用了 root 执行 `appcenter-cli`，改用 `sudo -u trim-media appcenter-cli ...` |
| 扫描结果为 0 或个别目录读不到 | 未授权目录：在应用设置中勾选该共享文件夹，或用 `trimacl` 补 `r-x` 权限 |
| 应用启动失败、无界面 | 查看日志 `$TRIM_PKGVAR/mediareporter.log`，通常是二进制架构与机型不符（x86/arm 混用），用 `bash build.sh arm64` 重新构建 |
| 端口 8090 被占用 | 修改 `fpk/manifest` 的 `service_port` 与 `fpk/app/ui/config`、`fpk/cmd/common` 中的端口后重新打包 |
| 报表缺集判断不准 | 检查作品目录内的文件命名，缺集依赖文件名中的季集标记（如 `S01E05`、`第05集`） |

## 6. 构建环境说明

- 构建机（Linux / WSL / NAS 本机）需 Go 1.21+ 与 Python 3；
- 交叉编译使用 `CGO_ENABLED=0`，产物为静态二进制，可直接在 fnOS 上运行；
- 打包脚本 `tools/package.py` 不依赖 `fnpack`，按 fpk 规范手工组装 `manifest + app.tgz + cmd/ + config/ + 图标`。
*（内容由AI生成，仅供参考）*

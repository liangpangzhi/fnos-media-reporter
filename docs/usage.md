---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 2f12ef080060517e9dd433d82e19a198_d60d0812d91f11f19232525400248c00
    ReservedCode1: 72MshwiUWMWxZepGL3nYw/SBKtSuvR7flfyfVdWNlvR6ME7kDweKIs7gXlYhM3tPe8sD20bkblfp6Y32lT/7pLHd1tuWXpM2gXce6Kb0oQjmQ1eZQZvUPtSk8d46/+6tbzHou3r6bYs3aE0gX9pPH2/nHKlagemcVNW3cM1pPozL9rk5v6EDc4CFrDw=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 2f12ef080060517e9dd433d82e19a198_d60d0812d91f11f19232525400248c00
    ReservedCode2: 72MshwiUWMWxZepGL3nYw/SBKtSuvR7flfyfVdWNlvR6ME7kDweKIs7gXlYhM3tPe8sD20bkblfp6Y32lT/7pLHd1tuWXpM2gXce6Kb0oQjmQ1eZQZvUPtSk8d46/+6tbzHou3r6bYs3aE0gX9pPH2/nHKlagemcVNW3cM1pPozL9rk5v6EDc4CFrDw=
---

# 使用说明

单文件程序 `FnosMediaReporterGUI.exe`，内置界面。SSH 信息在界面里填写，采集结果直接显示在窗口右侧，无需命令行、无需额外浏览器。

## 一、使用步骤

1. 双击 `FnosMediaReporterGUI.exe`，弹出程序窗口。
2. 左侧填写连接信息：
   - 主机地址：NAS 的局域网 IP 或 Tailscale IP（如 `192.168.1.100`、`100.x.x.x`）
   - 端口：默认 `22`
   - 用户名：你的 NAS 账号
   - 密码：SSH 免密场景**可留空**（如 Tailscale SSH 按网络身份认证）；若 NAS 走常规 sshd 密码登录，则必须填写
   - 扫描根目录：每行一个，默认 `/vol1/1000` ~ `/vol8/1000`（如需从 `/vol1/1000` 往上长可自行修改，例如 `/vol1`）
3. 点「测试连接」，确认显示连接成功、目录可达。
4. 点「开始采集」，日志区实时显示进度，完成后右侧直接展示统计报表。
5. 报表同时在 exe 同目录的 `reports\` 下生成 HTML + 两份 CSV，可单独分发。

采集全程只读（`find` 列目录 + `stat` 取属性），不修改 NAS 上任何文件。

## 二、“收不到数据”怎么办

若 SSH 能连上、但数据几乎传不动（吞吐 0 KB/s 或极低），常见原因是虚拟网卡 MTU 与链路不匹配：报文被丢弃，表现为**连接正常、传输停滞**。Tailscale 默认 MTU 1280，在部分链路上实际可通包长更小。

程序启动时会自动检测：

- 检测到异常时，主区顶部出现黄色提示条，其中有「一键修复」按钮；
- 点击后按提示确认，Windows 会弹一次管理员授权（UAC），程序把虚拟网卡 IPv4 MTU 改为 1200 并持久化；
- 修复完成提示条消失，即可正常采集。

手动修复（管理员身份运行 CMD）：

```
netsh interface ipv4 set subinterface "Tailscale" mtu=1200 store=persistent
```

需要还原时把 `1200` 改回 `1280` 再执行一次即可。

未修复时程序会阻止开始采集，避免白等。

## 三、输出物

- `reports\影音库统计_<时间>.html`：完整报表（概览、分类、卷分布、年份、规格、季分布、作品明细等页签，支持搜索与筛选）
- `reports\..._作品清单.csv`：按作品聚合的明细（分类、类型、季数、集数、容量、所在卷、字幕覆盖、缺集、重复、异常）
- `reports\..._文件清单.csv`：逐文件明细（路径、大小、分辨率、编码、来源、音轨、HDR、字幕、修改时间）

## 四、常见问题

| 现象 | 原因与处理 |
|---|---|
| 测试连接失败：连接超时 | 确认目标在线、SSH 端口开放；局域网直连时确认 IP 与防火墙 |
| 测试连接成功但吞吐显示 0 KB/s | 即上文的 MTU 问题，点「一键修复」 |
| 提示认证失败 | 目标走的是常规 sshd 密码登录，需要填写正确密码 |
| 采集很快结束但数字偏少 | 检查扫描根目录是否漏了某些卷 |
| 报表里字幕覆盖为 0 | 该库本身几乎没有外挂字幕文件，非程序问题 |

## 五、兼容性

- Windows 10/11 x64，需系统自带 WebView2 运行时（Win11 默认已装；缺失时会自动回退到 Edge `--app` 窗口）
- NAS 侧需 SSH 可用（Tailscale SSH 或常规 sshd）
- 程序仅用本机回环端口通信，不对外监听
*（内容由AI生成，仅供参考）*

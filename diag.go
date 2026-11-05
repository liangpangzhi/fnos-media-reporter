package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MtuInfo 链路 MTU 诊断结果
type MtuInfo struct {
	Iface   string `json:"iface"`
	Mtu     int    `json:"mtu"`
	Healthy bool   `json:"healthy"`
	Advice  string `json:"advice"`
	NeedFix bool   `json:"needFix"`
	FixHint string `json:"fixHint"`
}

// findVpnIface 找到 Tailscale 虚拟网卡
func findVpnIface() (string, int) {
	ifis, err := net.Interfaces()
	if err != nil {
		return "", 0
	}
	for _, i := range ifis {
		n := strings.ToLower(i.Name)
		if strings.Contains(n, "tailscale") || strings.Contains(n, "wireguard") {
			return i.Name, i.MTU
		}
	}
	return "", 0
}

// ipv4IfaceMtu 通过 netsh 读取该接口 IPv4 层 MTU（只读）
func ipv4IfaceMtu(name string) int {
	out, err := exec.Command("netsh", "interface", "ipv4", "show", "subinterface", name).Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		if f[0] == "MTU" || strings.HasPrefix(f[0], "-") {
			continue
		}
		// 行首为 MTU 数字，且该行最后一列包含接口名
		if v, e := strconv.Atoi(f[0]); e == nil && v > 0 && v < 65536 {
			if strings.EqualFold(f[len(f)-1], name) || len(f) >= 5 {
				return v
			}
		}
	}
	return 0
}

// effectiveMtu 取 IPv4 层实际生效 MTU，取不到时退回链路层
func effectiveMtu() (string, int) {
	name, linkMtu := findVpnIface()
	if name == "" {
		return "", 0
	}
	if v := ipv4IfaceMtu(name); v > 0 {
		return name, v
	}
	return name, linkMtu
}

// DiagnoseMTU 只读诊断
func DiagnoseMTU() MtuInfo {
	name, mtu := effectiveMtu()
	info := MtuInfo{Iface: name, Mtu: mtu}
	if name == "" {
		info.Healthy = true
		info.Advice = "未检测到 Tailscale/WireGuard 虚拟网卡，若走局域网直连可忽略此项。"
		return info
	}
	if mtu > 1200 {
		info.Healthy = false
		info.NeedFix = true
		info.Advice = fmt.Sprintf("检测到 %s 网卡 IPv4 MTU=%d，超过当前链路实际可通行上限（约 1228 字节），大包会被网络设备丢弃，表现为 SSH 能连上但数据几乎传不动、一直\"收不到数据\"。", name, mtu)
		info.FixHint = fmt.Sprintf(`netsh interface ipv4 set subinterface "%s" mtu=1200 store=persistent`, name)
	} else {
		info.Healthy = true
		info.Advice = fmt.Sprintf("%s 网卡 IPv4 MTU=%d，处于安全范围。", name, mtu)
	}
	return info
}

// FixMTU 提权把虚拟网卡 IPv4 MTU 调整为 1200（可逆）
func FixMTU() (string, error) {
	name, _ := findVpnIface()
	if name == "" {
		return "", fmt.Errorf("未找到 Tailscale/WireGuard 网卡")
	}
	psScript := fmt.Sprintf(
		`Start-Process -FilePath netsh -ArgumentList 'interface','ipv4','set','subinterface','"%s"','mtu=1200','store=persistent' -Verb RunAs -Wait`,
		name)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", psScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("提权执行失败（可能取消了 UAC 授权）: %v %s", err, strings.TrimSpace(string(out)))
	}

	for i := 0; i < 12; i++ {
		time.Sleep(500 * time.Millisecond)
		if _, m := effectiveMtu(); m == 1200 {
			return fmt.Sprintf("已将 %s 的 IPv4 MTU 调整为 1200，链路已恢复，可以开始采集。", name), nil
		}
	}
	_, m := effectiveMtu()
	return "", fmt.Errorf("命令已执行，但读取到的 MTU 仍为 %d，请手动检查网卡设置", m)
}

func mtuJSON() string {
	b, _ := json.Marshal(DiagnoseMTU())
	return string(b)
}

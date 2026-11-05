package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// RawFile 远端采集到的单个文件记录
type RawFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod"`
}

// Inventory 全量清单（可缓存）
type Inventory struct {
	Host      string    `json:"host"`
	ScannedAt time.Time `json:"scanned_at"`
	Roots     []string  `json:"roots"`
	Files     []RawFile `json:"files"`
	SubFiles  []RawFile `json:"sub_files"`
}

var videoExts = []string{"mkv", "mp4", "ts", "m2ts", "avi", "rmvb", "wmv", "flv", "mov", "iso", "mpg", "mpeg", "m4v", "webm"}
var sideExts = []string{"srt", "ass", "ssa", "vtt", "sup", "sub", "idx", "nfo"}

// SSHOptions 连接参数（由界面输入）
type SSHOptions struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	KeyFile  string `json:"keyFile"`
}

// ---------- 进度状态 ----------

// ScanStatus 采集状态，前端轮询读取
type ScanStatus struct {
	mu        sync.Mutex
	Phase     string   `json:"phase"`
	Message   string   `json:"message"`
	Files     int      `json:"files"`
	SubFiles  int      `json:"subFiles"`
	BytesRecv int64    `json:"bytesRecv"`
	Logs      []string `json:"logs"`
	Err       string   `json:"error"`
	Report    bool     `json:"reportReady"`
	Elapsed   int      `json:"elapsed"`
	RootIdx   int      `json:"rootIdx"`
	RootTotal int      `json:"rootTotal"`
	started   time.Time
}

func NewScanStatus() *ScanStatus {
	return &ScanStatus{Phase: "idle", Logs: []string{}}
}

func (s *ScanStatus) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Phase = "idle"
	s.Message = ""
	s.Files = 0
	s.SubFiles = 0
	s.BytesRecv = 0
	s.Logs = []string{}
	s.Err = ""
	s.Report = false
	s.Elapsed = 0
	s.RootIdx = 0
	s.RootTotal = 0
}

func (s *ScanStatus) Set(phase, msg string) {
	s.mu.Lock()
	s.Phase = phase
	s.Message = msg
	s.mu.Unlock()
}

func (s *ScanStatus) Log(msg string) {
	s.mu.Lock()
	ts := time.Now().Format("15:04:05")
	s.Logs = append(s.Logs, "["+ts+"] "+msg)
	if len(s.Logs) > 400 {
		s.Logs = s.Logs[len(s.Logs)-400:]
	}
	s.Message = msg
	s.mu.Unlock()
}

func (s *ScanStatus) SetErr(msg string) {
	s.mu.Lock()
	s.Err = msg
	s.Phase = "error"
	s.Message = msg
	s.mu.Unlock()
}

func (s *ScanStatus) AddFiles(n int) {
	s.mu.Lock()
	s.Files += n
	s.mu.Unlock()
}

func (s *ScanStatus) AddSubFiles(n int) {
	s.mu.Lock()
	s.SubFiles += n
	s.mu.Unlock()
}

func (s *ScanStatus) AddBytes(n int64) {
	s.mu.Lock()
	s.BytesRecv += n
	s.mu.Unlock()
}

func (s *ScanStatus) SetRoot(i, total int) {
	s.mu.Lock()
	s.RootIdx = i
	s.RootTotal = total
	s.mu.Unlock()
}

// Snapshot 供前端轮询
func (s *ScanStatus) Snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started.IsZero() && s.Phase != "done" && s.Phase != "error" {
		s.Elapsed = int(time.Since(s.started).Seconds())
	}
	b, _ := json.Marshal(map[string]interface{}{
		"phase":       s.Phase,
		"message":     s.Message,
		"files":       s.Files,
		"subFiles":    s.SubFiles,
		"bytesRecv":   s.BytesRecv,
		"logs":        s.Logs,
		"error":       s.Err,
		"reportReady": s.Report,
		"elapsed":     s.Elapsed,
		"rootIdx":     s.RootIdx,
		"rootTotal":   s.RootTotal,
	})
	return string(b)
}

func (s *ScanStatus) Begin() {
	s.mu.Lock()
	s.started = time.Now()
	s.mu.Unlock()
}

// ---------- SSH ----------

func dialSSH(o SSHOptions) (*ssh.Client, error) {
	var auths []ssh.AuthMethod
	if o.KeyFile != "" {
		b, err := os.ReadFile(o.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("读取私钥失败: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if o.Password != "" {
		auths = append(auths, ssh.Password(o.Password))
		auths = append(auths, ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			ans := make([]string, len(questions))
			for i := range ans {
				ans[i] = o.Password
			}
			return ans, nil
		}))
	}
	// 密码为空时 auths 为空，x/crypto 会自动尝试 none 认证（Tailscale SSH 场景）

	conf := &ssh.ClientConfig{
		User:            o.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", o.Host, o.Port)
	cli, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	return cli, nil
}

// runSimple 执行短命令并取回 stdout 文本
func runSimple(client *ssh.Client, cmd string, timeout time.Duration) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	type res struct {
		out []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b, e := sess.CombinedOutput(cmd)
		ch <- res{b, e}
	}()
	select {
	case r := <-ch:
		return string(r.out), r.err
	case <-time.After(timeout):
		sess.Close()
		return "", fmt.Errorf("命令执行超时")
	}
}

// Precheck 目录与工具探测
type PrecheckResult struct {
	Connected  bool     `json:"connected"`
	ServerInfo string   `json:"serverInfo"`
	HasGzip    bool     `json:"hasGzip"`
	Roots      []string `json:"roots"`
	Missing    []string `json:"missing"`
	Throughput string   `json:"throughput"`
	Warning    string   `json:"warning"`
}

// TestConnection 连接 + 目录探测 + 链路吞吐自检
func TestConnection(o SSHOptions, roots []string) (string, error) {
	start := time.Now()
	cli, err := dialSSH(o)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	res := PrecheckResult{Connected: true}
	res.ServerInfo = fmt.Sprintf("%s@%s:%d", o.User, o.Host, o.Port)

	out, _ := runSimple(cli, "command -v gzip >/dev/null 2>&1 && echo YES || echo NO", 10*time.Second)
	res.HasGzip = strings.Contains(out, "YES")

	var b strings.Builder
	b.WriteString("for r in " + strings.Join(roots, " ") + "; do if [ -d \"$r\" ]; then printf 'OK %s\\n' \"$r\"; else printf 'MISS %s\\n' \"$r\"; fi; done")
	out, _ = runSimple(cli, b.String(), 20*time.Second)
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "OK ") {
			res.Roots = append(res.Roots, strings.TrimPrefix(ln, "OK "))
		} else if strings.HasPrefix(ln, "MISS ") {
			res.Missing = append(res.Missing, strings.TrimPrefix(ln, "MISS "))
		}
	}

	// 下行吞吐自检：base64 200KB，正常链路 <1s
	t0 := time.Now()
	sess, e := cli.NewSession()
	kbs := 0.0
	if e == nil {
		stdout, e2 := sess.StdoutPipe()
		if e2 == nil {
			if e3 := sess.Start("head -c 200000 /dev/zero | base64"); e3 == nil {
				done := make(chan int64, 1)
				go func() {
					n, _ := io.Copy(io.Discard, stdout)
					done <- n
				}()
				select {
				case n := <-done:
					el := time.Since(t0).Seconds()
					if el > 0.01 {
						kbs = float64(n) / 1024 / el
					}
				case <-time.After(12 * time.Second):
					sess.Close()
					kbs = 0
				}
			}
		}
		sess.Close()
	}
	res.Throughput = fmt.Sprintf("%.1f KB/s", kbs)
	if kbs < 30 {
		res.Warning = "下行吞吐异常偏低（正常应达数百 KB/s 以上）。常见原因是 Tailscale/链路 MTU 不匹配：大包被丢弃，SSH 数据流几乎停滞。建议把 Tailscale 网卡 MTU 从 1280 降到 1200 后重试。"
	}
	_ = start

	js, _ := json.Marshal(res)
	return string(js), nil
}

// ---------- 采集 ----------

func buildExtPattern(exts []string) string {
	parts := make([]string, 0, len(exts))
	for _, e := range exts {
		parts = append(parts, "-iname '*."+e+"'")
	}
	return strings.Join(parts, " -o ")
}

// scanRoot 扫描单个根目录，流式解析，返回条数与错误
func scanRoot(client *ssh.Client, root, extPat string, useGzip bool, status *ScanStatus, sink func(RawFile)) (int, error) {
	sess, err := client.NewSession()
	if err != nil {
		return 0, err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return 0, err
	}
	errBuf := &strings.Builder{}
	sess.Stderr = errBuf

	pipe := "cat"
	if useGzip {
		pipe = "gzip -1"
	}
	cmd := fmt.Sprintf(`find %s -xdev -type f \( %s \) -printf '%%p\t%%s\t%%T@\0' 2>/dev/null | %s`, root, extPat, pipe)
	if err := sess.Start(cmd); err != nil {
		return 0, err
	}

	var reader io.Reader = stdout
	var gzr *gzip.Reader
	if useGzip {
		gzr, err = gzip.NewReader(stdout)
		if err != nil {
			sess.Close()
			return 0, fmt.Errorf("gzip 解压失败: %w", err)
		}
		defer gzr.Close()
		reader = gzr
	}

	// 空闲超时监控：45 秒无新数据即中止，避免界面无限等待
	var lastNano int64 = time.Now().UnixNano()
	var stalled int32
	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(3 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				if time.Since(time.Unix(0, atomic.LoadInt64(&lastNano))) > 45*time.Second {
					atomic.StoreInt32(&stalled, 1)
					sess.Close()
					return
				}
			}
		}
	}()
	defer close(stop)

	br := bufio.NewReaderSize(reader, 512*1024)
	count := 0
	buf := make([]byte, 0, 512)

	flush := func(chunk []byte) {
		if len(chunk) == 0 {
			return
		}
		line := strings.TrimSpace(string(chunk))
		if line == "" {
			return
		}
		i := strings.LastIndex(line, "\t")
		if i < 0 {
			return
		}
		i2 := strings.LastIndex(line[:i], "\t")
		if i2 < 0 {
			return
		}
		size, _ := strconv.ParseInt(line[i2+1:i], 10, 64)
		var mt float64
		fmt.Sscanf(line[i+1:], "%f", &mt)
		sink(RawFile{Path: line[:i2], Size: size, ModUnix: int64(mt)})
		count++
	}

	for {
		b, err := br.ReadByte()
		if err != nil {
			if len(buf) > 0 {
				flush(buf)
			}
			if atomic.LoadInt32(&stalled) == 1 {
				return count, fmt.Errorf("链路停滞：45 秒无数据（很可能 MTU 不匹配或网络中断）")
			}
			break
		}
		if b == 0 {
			flush(buf)
			buf = buf[:0]
			atomic.StoreInt64(&lastNano, time.Now().UnixNano())
			continue
		}
		buf = append(buf, b)
		if len(buf) > 64*1024 {
			flush(buf)
			buf = buf[:0]
		}
		if count%500 == 0 {
			atomic.StoreInt64(&lastNano, time.Now().UnixNano())
		}
	}

	err = sess.Wait()
	if err != nil && count == 0 {
		msg := strings.TrimSpace(errBuf.String())
		if msg != "" {
			return 0, fmt.Errorf("%v: %s", err, msg)
		}
		return 0, err
	}
	return count, nil
}

// CollectInventory 完整采集流程
func CollectInventory(o SSHOptions, roots []string, status *ScanStatus) (*Inventory, error) {
	status.Begin()
	status.Set("connecting", "正在建立 SSH 连接 ...")
	status.Log(fmt.Sprintf("连接 %s@%s:%d", o.User, o.Host, o.Port))
	cli, err := dialSSH(o)
	if err != nil {
		status.SetErr(err.Error())
		return nil, err
	}
	defer cli.Close()
	status.Log("SSH 已连接")

	// gzip 探测
	useGzip := true
	if out, _ := runSimple(cli, "command -v gzip >/dev/null 2>&1 && echo YES || echo NO", 8*time.Second); !strings.Contains(out, "YES") {
		useGzip = false
		status.Log("远端无 gzip，改用原始传输（会明显更慢）")
	} else {
		status.Log("远端支持 gzip，启用压缩传输（数据量约减少 80%）")
	}

	inv := &Inventory{Host: o.Host, ScannedAt: time.Now(), Roots: roots}
	vpat := buildExtPattern(videoExts)
	spat := buildExtPattern(sideExts)

	// 逐卷采集，每卷完成即反馈，避免长时间无进展
	status.SetRoot(0, len(roots))
	for i, root := range roots {
		status.SetRoot(i+1, len(roots))
		status.Set("scanning", fmt.Sprintf("正在扫描 %s（第 %d/%d 个卷）", root, i+1, len(roots)))
		n, err := scanRoot(cli, root, vpat, useGzip, status, func(rf RawFile) {
			inv.Files = append(inv.Files, rf)
			status.AddFiles(1)
			status.AddBytes(int64(len(rf.Path)))
		})
		if err != nil {
			status.Log(fmt.Sprintf("%s 扫描异常：%v", root, err))
			if n == 0 && strings.Contains(err.Error(), "链路停滞") {
				status.SetErr("采集链路中断：" + err.Error())
				return nil, err
			}
			continue
		}
		status.Log(fmt.Sprintf("%s 完成，发现 %d 个视频", root, n))
	}

	if len(inv.Files) == 0 {
		msg := "未发现任何视频文件。请检查：1) 扫描根目录是否正确（形如 /vol2/1000/电影1-动漫0-连续剧00）；2) 该账号是否有读取权限；3) 目录下是否确实存在视频文件。"
		status.SetErr(msg)
		return nil, fmt.Errorf("%s", msg)
	}

	status.Set("scanning", "正在采集字幕/NFO 附属文件 ...")
	status.Log("开始采集字幕与 NFO ...")
	for _, root := range roots {
		n, err := scanRoot(cli, root, spat, useGzip, status, func(rf RawFile) {
			inv.SubFiles = append(inv.SubFiles, rf)
			status.AddSubFiles(1)
		})
		if err != nil {
			status.Log(fmt.Sprintf("%s 附属文件扫描跳过：%v", root, err))
			continue
		}
		_ = n
	}
	status.Log(fmt.Sprintf("附属文件 %d 个", len(inv.SubFiles)))
	return inv, nil
}

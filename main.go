package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	webview "github.com/jchv/go-webview2"
)

// App 应用状态
type App struct {
	mu      sync.Mutex
	status  *ScanStatus
	report  string
	summary *Summary
	csv     []string
	busy    bool
}

// exeDirPath 返回可执行文件所在目录，用于存放 settings.json 与 reports 目录。
func exeDirPath() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func outDir() string {
	d := filepath.Join(exeDirPath(), "reports")
	if err := os.MkdirAll(d, 0o755); err != nil {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.TempDir()
		}
		d = filepath.Join(base, "FnosMediaReporter", "reports")
		os.MkdirAll(d, 0o755)
	}
	return d
}

// Settings 界面设置（密码单独可选保存）
type Settings struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Roots   string `json:"roots"`
	SavePID bool   `json:"savePwd"`
	Keyword string `json:"keyword"`
}

func settingsPath() string { return filepath.Join(exeDirPath(), "settings.json") }

func loadSettings() Settings {
	s := Settings{Port: 22, SavePID: false}
	b, err := os.ReadFile(settingsPath())
	if err == nil {
		json.Unmarshal(b, &s)
	}
	if s.Port == 0 {
		s.Port = 22
	}
	return s
}

func saveSettings(s Settings) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(settingsPath(), b, 0o600)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	io.WriteString(w, a.status.Snapshot())
}

type scanReq struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Roots    string `json:"roots"`
	SavePID  bool   `json:"savePwd"`
}

func (a *App) handleTest(w http.ResponseWriter, r *http.Request) {
	var req scanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": "参数解析失败"})
		return
	}
	roots := parseRoots(req.Roots)
	opts := SSHOptions{Host: req.Host, Port: req.Port, User: req.User, Password: req.Password}
	res, err := TestConnection(opts, roots)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	io.WriteString(w, res)
}

func parseRoots(s string) []string {
	raw := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == ' ' })
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (a *App) handleScan(w http.ResponseWriter, r *http.Request) {
	var req scanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": "参数解析失败"})
		return
	}
	roots := parseRoots(req.Roots)
	if len(roots) == 0 {
		writeJSON(w, map[string]string{"error": "请至少填写一个扫描根目录"})
		return
	}
	if req.Host == "" || req.User == "" {
		writeJSON(w, map[string]string{"error": "主机地址和用户名不能为空"})
		return
	}

	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		writeJSON(w, map[string]string{"error": "已有采集任务在运行"})
		return
	}
	a.busy = true
	a.mu.Unlock()

	a.status.reset()
	opts := SSHOptions{Host: req.Host, Port: req.Port, User: req.User, Password: req.Password}

	// 保存设置
	st := Settings{Host: req.Host, Port: req.Port, User: req.User, Roots: req.Roots, SavePID: req.SavePID}
	saveSettings(st)

	go func() {
		defer func() {
			a.mu.Lock()
			a.busy = false
			a.mu.Unlock()
			if p := recover(); p != nil {
				a.status.SetErr("内部异常: " + toStr(p))
			}
		}()

		inv, err := CollectInventory(opts, roots, a.status)
		if err != nil {
			return
		}

		a.status.Set("analyzing", "正在解析与统计 ...")
		a.status.Log("开始解析片名、季集与规格信息")
		sum := Analyze(inv, inv.SubFiles)
		a.status.Log("统计完成，正在生成报表 ...")

		html, err := RenderHTMLString(sum)
		if err != nil {
			a.status.SetErr("报表生成失败: " + err.Error())
			return
		}

		a.mu.Lock()
		a.report = html
		a.summary = sum
		a.mu.Unlock()

		// 同时落盘一份 HTML 与 CSV
		base := "影音库统计_" + time.Now().Format("20060102_150405")
		htmlPath := filepath.Join(outDir(), base+".html")
		os.WriteFile(htmlPath, []byte(html), 0o644)
		csvFiles, _ := ExportCSV(sum, outDir(), base)

		a.mu.Lock()
		a.csv = append([]string{htmlPath}, csvFiles...)
		a.mu.Unlock()

		a.status.Set("done", "采集完成，已生成报表")
		a.status.Log("报表已生成：" + htmlPath)
	}()

	writeJSON(w, map[string]string{"ok": "started"})
}

func (a *App) handleReport(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	h := a.report
	a.mu.Unlock()
	if h == "" {
		http.Error(w, "报表尚未生成", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, h)
}

func (a *App) handleFiles(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	files := append([]string{}, a.csv...)
	a.mu.Unlock()
	writeJSON(w, map[string]interface{}{"files": files, "dir": outDir()})
}

func (a *App) handleOpenDir(w http.ResponseWriter, r *http.Request) {
	exec.Command("explorer.exe", outDir()).Start()
	writeJSON(w, map[string]string{"ok": "opened"})
}

func toStr(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func openBrowser(url string) {
	exec.Command("cmd", "/c", "start", "", url).Start()
}

func main() {
	app := &App{status: NewScanStatus()}

	listenPort := 0
	noWin := false
	for i, a := range os.Args {
		if a == "-port" && i+1 < len(os.Args) {
			listenPort, _ = strconv.Atoi(os.Args[i+1])
		}
		if a == "-nowin" {
			noWin = true
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(listenPort))
	if err != nil {
		os.Exit(1)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", app.handleStatus)
	mux.HandleFunc("/api/test", app.handleTest)
	mux.HandleFunc("/api/scan", app.handleScan)
	mux.HandleFunc("/api/report", app.handleReport)
	mux.HandleFunc("/api/files", app.handleFiles)
	mux.HandleFunc("/api/opendir", app.handleOpenDir)
	mux.HandleFunc("/api/mtu", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		io.WriteString(w, mtuJSON())
	})
	mux.HandleFunc("/api/fixmtu", func(w http.ResponseWriter, r *http.Request) {
		msg, err := FixMTU()
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"ok": msg})
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var s Settings
			json.NewDecoder(r.Body).Decode(&s)
			saveSettings(s)
			writeJSON(w, map[string]string{"ok": "saved"})
			return
		}
		writeJSON(w, loadSettings())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, appHTML)
	})

	go http.Serve(ln, mux)

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/"

	if noWin {
		select {}
	}

	ok := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				ok = false
			}
		}()
		w := webview.New(false)
		if w == nil {
			ok = false
			return
		}
		defer w.Destroy()
		w.SetTitle("飞牛 NAS 影音库统计")
		w.SetSize(1360, 900, webview.HintNone)
		w.Navigate(url)
		w.Run()
	}()

	if !ok {
		// WebView2 不可用时退回系统浏览器（Edge --app 无地址栏窗口）
		edge := `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
		if _, err := os.Stat(edge); err == nil {
			exec.Command(edge, "--app="+url, "--window-size=1360,900").Start()
		} else {
			openBrowser(url)
		}
		select {}
	}
}

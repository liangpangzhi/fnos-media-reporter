package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed assets/app.html
var appHTML string

//go:embed assets/report.html.tmpl
var reportTmplStr string

var (
	dataDir   string
	reportDir string
	cfgPath   string

	repMu      sync.RWMutex
	lastHTML   string
	lastReport string
)

type Settings struct {
	Roots []string `json:"roots"`
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	port := flag.Int("port", 8090, "监听端口")
	data := flag.String("data", "", "数据目录")
	flag.Parse()

	if *data != "" {
		dataDir = *data
	} else if v := os.Getenv("TRIM_PKGVAR"); v != "" {
		dataDir = v
	} else {
		exe, _ := os.Executable()
		dataDir = filepath.Join(filepath.Dir(exe), "data")
	}
	reportDir = filepath.Join(dataDir, "reports")
	cfgPath = filepath.Join(dataDir, "settings.json")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/env", handleEnv)
	mux.HandleFunc("/api/settings", handleSettings)
	mux.HandleFunc("/api/browse", handleBrowse)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/report", handleReport)
	mux.HandleFunc("/api/reports", handleReports)
	mux.HandleFunc("/api/download", handleDownload)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"ok": true, "version": appVersion})
	})

	addr := fmt.Sprintf(":%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("端口 %d 监听失败: %v", *port, err)
	}
	log.Printf("影音库统计启动于 %s，数据目录 %s", addr, dataDir)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 15 * time.Second}
	if err := srv.Serve(ln); err != nil {
		log.Fatal(err)
	}
}

const appVersion = "1.0.2"

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, appHTML)
}

// handleEnv 返回卷列表与数据目录
func handleEnv(w http.ResponseWriter, r *http.Request) {
	type vol struct {
		Path  string `json:"path"`
		Label string `json:"label"`
	}
	var vols []vol
	for i := 1; i <= 32; i++ {
		p := fmt.Sprintf("/vol%d", i)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			vols = append(vols, vol{Path: p, Label: filepath.Base(p)})
		}
	}
	writeJSON(w, map[string]interface{}{
		"volumes":   vols,
		"data_dir":  dataDir,
		"report_dir": reportDir,
		"version":   appVersion,
		"arch":      archName(),
	})
}

func loadSettings() Settings {
	var s Settings
	if b, err := os.ReadFile(cfgPath); err == nil {
		json.Unmarshal(b, &s)
	}
	if len(s.Roots) == 0 {
		s.Roots = detectDefaultRoots()
	}
	return s
}

// detectDefaultRoots 自动探测最常见的媒体目录 /volN/1000
func detectDefaultRoots() []string {
	var out []string
	for i := 1; i <= 32; i++ {
		p := fmt.Sprintf("/vol%d/1000", i)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{"/vol1"}
	}
	return out
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var s Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			writeJSON(w, map[string]string{"error": "参数错误: " + err.Error()})
			return
		}
		var clean []string
		for _, x := range s.Roots {
			x = strings.TrimSpace(x)
			if x != "" {
				clean = append(clean, x)
			}
		}
		s.Roots = clean
		b, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
			writeJSON(w, map[string]string{"error": "保存失败: " + err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "roots": s.Roots})
		return
	}
	writeJSON(w, loadSettings())
}

// handleBrowse 只读列目录，限制在 /vol* 之下
func handleBrowse(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	dir = filepath.Clean(dir)
	if dir != "/" && !strings.HasPrefix(dir, "/vol") {
		writeJSON(w, map[string]string{"error": "仅允许浏览 /vol* 目录"})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, map[string]string{"error": "读取失败（可能未授权）：" + err.Error()})
		return
	}
	type item struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Media bool   `json:"media"`
	}
	var items []item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "@") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		items = append(items, item{Name: e.Name(), Path: p, Media: isMediaDir(p)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, map[string]interface{}{"path": dir, "items": items})
}

// isMediaDir 判断目录内是否直接含影音文件（用于前端高亮）
func isMediaDir(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	names, _ := f.Readdirnames(80)
	for _, n := range names {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(n), "."))
		for _, v := range videoExts {
			if ext == v {
				return true
			}
		}
	}
	return false
}

func handleScan(w http.ResponseWriter, r *http.Request) {
	var req Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": "参数错误: " + err.Error()})
		return
	}
	if len(req.Roots) == 0 {
		req.Roots = loadSettings().Roots
	}
	if !st.tryStart() {
		writeJSON(w, map[string]string{"error": "已有采集任务在运行"})
		return
	}
	go runScan(req.Roots)
	writeJSON(w, map[string]interface{}{"ok": true})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, st.snapshot())
}

func handleReport(w http.ResponseWriter, r *http.Request) {
	repMu.RLock()
	html := lastHTML
	repMu.RUnlock()
	if html == "" {
		html = strings.Replace(reportTmplStr, "{{.Data}}", "{}", 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, html)
}

// handleReports 列出已生成的报表文件
func handleReports(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		writeJSON(w, map[string]interface{}{"files": []interface{}{}})
		return
	}
	type f struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Time string `json:"time"`
		URL  string `json:"url"`
	}
	var out []f
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, f{
			Name: e.Name(),
			Size: info.Size(),
			Time: info.ModTime().Format("2006-01-02 15:04:05"),
			URL:  "/api/download?f=" + e.Name(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	writeJSON(w, map[string]interface{}{"files": out})
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("f"))
	if name == "" || name == "." || strings.Contains(name, "..") {
		http.Error(w, "参数错误", 400)
		return
	}
	p := filepath.Join(reportDir, name)
	if _, err := os.Stat(p); err != nil {
		http.Error(w, "文件不存在", 404)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	http.ServeFile(w, r, p)
}

func archName() string {
	return fmt.Sprintf("%s/%s", osGOOS(), osGOARCH())
}

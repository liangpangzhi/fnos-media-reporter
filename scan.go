package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

func osGOOS() string   { return runtime.GOOS }
func osGOARCH() string { return runtime.GOARCH }

// RawFile 采集到的单个文件记录
type RawFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod"`
}

// Inventory 全量清单
type Inventory struct {
	Host      string    `json:"host"`
	ScannedAt time.Time `json:"scanned_at"`
	Roots     []string  `json:"roots"`
	Files     []RawFile `json:"files"`
	SubFiles  []RawFile `json:"sub_files"`
}

var videoExts = []string{"mkv", "mp4", "ts", "m2ts", "avi", "rmvb", "wmv", "flv", "mov", "iso", "mpg", "mpeg", "m4v", "webm"}
var sideExts = []string{"srt", "ass", "ssa", "vtt", "sup", "sub", "idx", "nfo"}

func extIn(name string, list []string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	for _, v := range list {
		if ext == v {
			return true
		}
	}
	return false
}

// scanStatus 采集状态
type scanStatus struct {
	mu        sync.Mutex
	running   bool
	Phase     string   `json:"phase"`
	Message   string   `json:"message"`
	Error     string   `json:"error"`
	Files     int      `json:"files"`
	SubFiles  int      `json:"sub_files"`
	BytesRecv int64    `json:"bytesRecv"`
	Elapsed   int64    `json:"elapsed"`
	Logs      []string `json:"logs"`
	RootIdx   int      `json:"rootIdx"`
	RootTotal int      `json:"rootTotal"`
	started   time.Time
}

var st = &scanStatus{Phase: "idle", Logs: []string{}}

func (s *scanStatus) tryStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.Phase = "starting"
	s.Message = "准备中"
	s.Error = ""
	s.Files = 0
	s.SubFiles = 0
	s.BytesRecv = 0
	s.Elapsed = 0
	s.RootIdx = 0
	s.RootTotal = 0
	s.Logs = []string{}
	s.started = time.Now()
	return true
}

func (s *scanStatus) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.Elapsed = int64(time.Since(s.started).Seconds())
	}
	return map[string]interface{}{
		"phase":     s.Phase,
		"message":   s.Message,
		"error":     s.Error,
		"files":     s.Files,
		"sub_files": s.SubFiles,
		"bytesRecv": s.BytesRecv,
		"elapsed":   s.Elapsed,
		"logs":      append([]string{}, s.Logs...),
		"rootIdx":   s.RootIdx,
		"rootTotal": s.RootTotal,
	}
}

func (s *scanStatus) log(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Logs = append(s.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
	if len(s.Logs) > 800 {
		s.Logs = s.Logs[len(s.Logs)-800:]
	}
}

func (s *scanStatus) phase(p, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Phase = p
	if msg != "" {
		s.Message = msg
	}
}

func (s *scanStatus) addFile(isSub bool, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if isSub {
		s.SubFiles++
	} else {
		s.Files++
	}
	s.BytesRecv += size
}

func (s *scanStatus) setRootIdx(i, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RootIdx = i
	s.RootTotal = total
}

func (s *scanStatus) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if err != nil {
		s.Phase = "error"
		s.Error = err.Error()
	} else {
		s.Phase = "done"
		s.Message = "已完成"
	}
	s.Elapsed = int64(time.Since(s.started).Seconds())
}

// runScan 本地只读扫描（无需 SSH）
func runScan(roots []string) {
	inv := &Inventory{Host: "localhost", ScannedAt: time.Now(), Roots: roots}
	st.setRootIdx(0, len(roots))
	st.phase("scanning", "正在扫描")

	for i, root := range roots {
		root = filepath.Clean(root)
		if _, err := os.Stat(root); err != nil {
			st.log(fmt.Sprintf("跳过 %s：%v", root, err))
			continue
		}
		st.setRootIdx(i+1, len(roots))
		st.log("扫描目录 " + root)
		walkErr := walkRoot(root, inv)
		if walkErr != nil {
			st.log(fmt.Sprintf("目录 %s 扫描中断：%v", root, walkErr))
		}
	}

	st.log(fmt.Sprintf("扫描结束：视频 %d 个，附属文件 %d 个", len(inv.Files), len(inv.SubFiles)))
	if len(inv.Files) == 0 {
		st.finish(fmt.Errorf("未发现影音文件，请检查扫描目录是否正确，或在应用设置中授权目录后重试"))
		return
	}

	st.phase("analyzing", "正在统计")
	sum := Analyze(inv, inv.SubFiles)

	html, err := RenderHTMLString(sum)
	if err != nil {
		st.finish(fmt.Errorf("报表渲染失败: %v", err))
		return
	}

	base := "影音库统计_" + time.Now().Format("20060102_150405")
	htmlPath := filepath.Join(reportDir, base+".html")
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		st.log("写入报表失败：" + err.Error())
	}
	if files, err := ExportCSV(sum, reportDir, base); err == nil {
		st.log("已导出 CSV：" + strings.Join(baseNames(files), "、"))
	} else {
		st.log("导出 CSV 失败：" + err.Error())
	}

	repMu.Lock()
	lastHTML = html
	lastReport = htmlPath
	repMu.Unlock()

	st.log("报表已生成：" + htmlPath)
	st.finish(nil)
}

func baseNames(ps []string) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, filepath.Base(p))
	}
	return out
}

// walkRoot 递归遍历，遇到无权限目录跳过并记录
func walkRoot(root string, inv *Inventory) error {
	permDenied := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if permDenied < 3 {
				st.log(fmt.Sprintf("无权限或无法访问：%s", p))
			}
			permDenied++
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || name == "#recycle" || name == "lost+found") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if extIn(name, videoExts) {
			info, e := d.Info()
			if e != nil {
				return nil
			}
			inv.Files = append(inv.Files, RawFile{Path: p, Size: info.Size(), ModUnix: info.ModTime().Unix()})
			st.addFile(false, info.Size())
		} else if extIn(name, sideExts) {
			info, e := d.Info()
			if e != nil {
				return nil
			}
			inv.SubFiles = append(inv.SubFiles, RawFile{Path: p, Size: info.Size(), ModUnix: info.ModTime().Unix()})
			st.addFile(true, 0)
		}
		if len(inv.Files)%2000 == 0 && len(inv.Files) > 0 {
			st.phase("scanning", fmt.Sprintf("扫描中（已发现 %d 个视频）", len(inv.Files)))
		}
		return nil
	})
	if permDenied > 0 {
		st.log(fmt.Sprintf("共 %d 个路径因权限被跳过，可在「应用设置 → 授权目录」中放行", permDenied))
	}
	return err
}

package main

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

//go:embed assets/report.html.tmpl
var reportTmplStr string

func nowStr() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

type tmplData struct {
	Data string
}

// RenderHTML 生成自包含 HTML 报表
func RenderHTML(s *Summary, outPath string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	t, err := template.New("report").Parse(reportTmplStr)
	if err != nil {
		return fmt.Errorf("模板解析失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, tmplData{Data: string(b)})
}

// RenderHTMLString 生成自包含 HTML 报表文本（供界面内嵌展示）
func RenderHTMLString(s *Summary) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	t, err := template.New("report").Parse(reportTmplStr)
	if err != nil {
		return "", fmt.Errorf("模板解析失败: %w", err)
	}
	var sb strings.Builder
	if err := t.Execute(&sb, tmplData{Data: string(b)}); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// ExportCSV 导出作品级与文件级两张表
func ExportCSV(s *Summary, dir, base string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	workPath := filepath.Join(dir, base+"_作品清单.csv")
	filePath := filepath.Join(dir, base+"_文件清单.csv")

	wf, err := os.Create(workPath)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(wf)
	w.Write([]string{"分类", "类型", "作品名", "年份", "季数", "集数/文件数", "总容量(GB)",
		"所在卷", "媒体库", "目录数", "字幕覆盖", "缺集", "重复", "结构异常"})
	for _, k := range s.Works {
		w.Write([]string{
			k.Category, k.Kind, k.Title, strconv.Itoa(k.Year), strconv.Itoa(k.SeasonCount),
			strconv.Itoa(k.FileCount), fmt.Sprintf("%.2f", float64(k.TotalSize)/1024/1024/1024),
			strings.Join(k.Volumes, "/"), strings.Join(k.Libraries, "/"),
			strconv.Itoa(len(k.Dirs)),
			fmt.Sprintf("%d/%d", k.SubCount, k.FileCount),
			strings.Join(k.Missing, "; "), strings.Join(k.Duplicates, "; "),
			strings.Join(k.Anomalies, "; "),
		})
	}
	w.Flush()
	wf.Close()

	ff, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}
	w2 := csv.NewWriter(ff)
	w2.Write([]string{"分类", "类型", "作品名", "年份", "季", "集", "单集标题", "大小(GB)",
		"分辨率", "编码", "来源", "音轨", "HDR", "字幕", "所在卷", "媒体库", "完整路径", "修改时间"})
	for _, f := range s.Files {
		w2.Write([]string{
			f.Category, f.Kind, f.Title, strconv.Itoa(f.Year),
			strconv.Itoa(f.Season), strconv.Itoa(f.Episode), f.EpisodeTitle,
			fmt.Sprintf("%.2f", float64(f.Size)/1024/1024/1024),
			f.Resolution, f.Codec, f.Source, f.Audio, f.Dynamic,
			boolStr(f.HasSubtitle), f.Volume, f.Library, f.Path,
			f.ModTime.Format("2006-01-02 15:04"),
		})
	}
	w2.Flush()
	ff.Close()
	return []string{workPath, filePath}, nil
}

func boolStr(b bool) string {
	if b {
		return "有"
	}
	return "无"
}

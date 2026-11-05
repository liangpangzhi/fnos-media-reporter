package main

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Work 一部作品（电影或整部剧）
type Work struct {
	Key         string       `json:"key"`
	Title       string       `json:"title"`
	Year        int          `json:"year"`
	Category    string       `json:"category"`
	Kind        string       `json:"kind"`
	Files       []*MediaFile `json:"-"`
	FileIdx     []int        `json:"file_idx"`
	TotalSize   int64        `json:"total_size"`
	FileCount   int          `json:"file_count"`
	SeasonCount int          `json:"season_count"`
	EpisodeSum  int          `json:"episode_sum"`
	Missing     []string     `json:"missing"`
	Duplicates  []string     `json:"duplicates"`
	Dirs        []string     `json:"dirs"`
	Volumes     []string     `json:"volumes"`
	Libraries   []string     `json:"libraries"`
	MaxSeason   int          `json:"max_season"`
	SubCount    int          `json:"sub_count"`
	Anomalies   []string     `json:"anomalies"`
	Newest      int64        `json:"newest"`
	Oldest      int64        `json:"oldest"`
}

// Summary 报表数据根对象
type Summary struct {
	Host         string       `json:"host"`
	GeneratedAt  string       `json:"generated_at"`
	ScannedAt    string       `json:"scanned_at"`
	Roots        []string     `json:"roots"`
	Works        []*Work      `json:"works"`
	Files        []*MediaFile `json:"files"`
	Stats        Stats        `json:"stats"`
	VolumeStats  []Bucket     `json:"volume_stats"`
	LibStats     []Bucket     `json:"lib_stats"`
	CategoryStat []Bucket     `json:"category_stats"`
	ResStats     []Bucket     `json:"res_stats"`
	CodecStats   []Bucket     `json:"codec_stats"`
	SourceStats  []Bucket     `json:"source_stats"`
	SeasonDist   []Bucket     `json:"season_dist"`
	TopBig       []*Work      `json:"top_big"`
	Recent       []*MediaFile `json:"recent"`
	AnomalyList  []Anomaly    `json:"anomaly_list"`
	GenreStats   []Bucket     `json:"genre_stats"`
}

type Bucket struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Size  int64  `json:"size"`
}

type Anomaly struct {
	Level  string `json:"level"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type Stats struct {
	TotalFiles     int     `json:"total_files"`
	TotalSize      int64   `json:"total_size"`
	TotalWorks     int     `json:"total_works"`
	MovieWorks     int     `json:"movie_works"`
	SeriesWorks    int     `json:"series_works"`
	TotalEpisodes  int     `json:"total_episodes"`
	MissingCount   int     `json:"missing_count"`
	AnomalyCount   int     `json:"anomaly_count"`
	SubCovRate     float64 `json:"sub_cov_rate"`
	Multipart      int     `json:"multipart"`
	AvgMovieSize   int64   `json:"avg_movie_size"`
	AvgEpisodeSize int64   `json:"avg_episode_size"`
}

// Analyze 聚合统计
func Analyze(inv *Inventory, subs []RawFile) *Summary {
	files := make([]*MediaFile, 0, len(inv.Files))
	for _, rf := range inv.Files {
		files = append(files, ParseRawToMedia(rf))
	}
	// 字幕配对
	subIndex := map[string]bool{}
	for _, s := range subs {
		base := strings.ToLower(strings.TrimSuffix(path.Base(s.Path), path.Ext(s.Path)))
		subIndex[path.Dir(s.Path)+"|"+base] = true
	}
	for _, f := range files {
		stem := strings.TrimSuffix(f.FileName, path.Ext(f.FileName))
		if subIndex[path.Dir(f.Path)+"|"+strings.ToLower(stem)] {
			f.HasSubtitle = true
			continue
		}
		// 常见变体：xxx.zh.srt / xxx.chs.ass
		for _, lang := range []string{".zh", ".chs", ".cht", ".chi", ".eng", ".sc", ".tc"} {
			if subIndex[path.Dir(f.Path)+"|"+strings.ToLower(stem+lang)] {
				f.HasSubtitle = true
				break
			}
		}
	}

	// 按作品聚合
	workMap := map[string]*Work{}
	for fi, f := range files {
		key := fmt.Sprintf("%s|%d|%s", f.Category, f.Year, f.Title)
		w, ok := workMap[key]
		if !ok {
			w = &Work{Key: key, Title: f.Title, Year: f.Year, Category: f.Category, Kind: f.Kind}
			workMap[key] = w
		}
		w.Files = append(w.Files, f)
		w.FileIdx = append(w.FileIdx, fi)
		w.TotalSize += f.Size
		w.FileCount++
		if f.HasSubtitle {
			w.SubCount++
		}
		dir := f.Dir
		if !containsStr(w.Dirs, dir) {
			w.Dirs = append(w.Dirs, dir)
		}
		if !containsStr(w.Volumes, f.Volume) {
			w.Volumes = append(w.Volumes, f.Volume)
		}
		if !containsStr(w.Libraries, f.Library) {
			w.Libraries = append(w.Libraries, f.Library)
		}
		if f.Season > w.MaxSeason {
			w.MaxSeason = f.Season
		}
		ts := f.ModTime.Unix()
		if ts > w.Newest || w.Newest == 0 {
			w.Newest = ts
		}
		if ts < w.Oldest || w.Oldest == 0 {
			w.Oldest = ts
		}
	}

	works := make([]*Work, 0, len(workMap))
	for _, w := range workMap {
		finalizeWork(w)
		works = append(works, w)
	}
	sortWorks(works)

	s := &Summary{
		Host:        inv.Host,
		GeneratedAt: nowStr(),
		ScannedAt:   inv.ScannedAt.Format("2006-01-02 15:04:05"),
		Roots:       inv.Roots,
		Works:       works,
		Files:       files,
	}

	// 统计
	s.Stats.TotalFiles = len(files)
	s.Stats.TotalWorks = len(works)
	for _, f := range files {
		s.Stats.TotalSize += f.Size
		if f.Kind == "电影" {
			// count below
		}
	}
	for _, w := range works {
		if w.Kind == "电影" {
			s.Stats.MovieWorks++
			if w.FileCount > 1 {
				s.Stats.Multipart++
			}
		} else {
			s.Stats.SeriesWorks++
			s.Stats.TotalEpisodes += w.FileCount
		}
		s.Stats.MissingCount += len(w.Missing)
	}
	if s.Stats.MovieWorks > 0 {
		var mv int64
		var mc int
		for _, w := range works {
			if w.Kind == "电影" {
				mv += w.TotalSize
				mc += w.FileCount
			}
		}
		if mc > 0 {
			s.Stats.AvgMovieSize = mv / int64(mc)
		}
	}
	var epSize int64
	var epCnt int
	for _, w := range works {
		if w.Kind != "电影" {
			epSize += w.TotalSize
			epCnt += w.FileCount
		}
	}
	if epCnt > 0 {
		s.Stats.AvgEpisodeSize = epSize / int64(epCnt)
	}

	// 字幕覆盖率
	var withSub int
	for _, f := range files {
		if f.HasSubtitle {
			withSub++
		}
	}
	if len(files) > 0 {
		s.Stats.SubCovRate = float64(withSub) / float64(len(files)) * 100
	}

	// 分类 / 卷 / 库 / 规格统计
	s.CategoryStat = bucketBy(files, func(f *MediaFile) string { return f.Category })
	s.VolumeStats = bucketBy(files, func(f *MediaFile) string { return f.Volume })
	s.LibStats = bucketBy(files, func(f *MediaFile) string { return f.Volume + "/" + f.Library })
	s.ResStats = bucketBy(files, func(f *MediaFile) string {
		if f.Resolution == "" {
			return "未标注"
		}
		return f.Resolution
	})
	s.CodecStats = bucketBy(files, func(f *MediaFile) string {
		if f.Codec == "" {
			return "未标注"
		}
		return f.Codec
	})
	s.SourceStats = bucketBy(files, func(f *MediaFile) string {
		if f.Source == "" {
			return "未标注"
		}
		return f.Source
	})
	s.GenreStats = bucketByWorks(works, func(w *Work) string { return w.Category })

	// 季数分布（剧集）：按季数档聚合作品数与容量
	seasonCnt := map[int]int{}
	seasonSize := map[int]int64{}
	for _, w := range works {
		if w.Kind == "电影" {
			continue
		}
		n := w.SeasonCount
		if n <= 0 {
			n = 1
		}
		if n >= 10 {
			n = 10
		}
		seasonCnt[n]++
		seasonSize[n] += w.TotalSize
	}
	for n, c := range seasonCnt {
		name := fmt.Sprintf("%d 季", n)
		if n >= 10 {
			name = "10 季及以上"
		}
		s.SeasonDist = append(s.SeasonDist, Bucket{Name: name, Count: c, Size: seasonSize[n]})
	}
	sort.Slice(s.SeasonDist, func(i, j int) bool {
		ni := strings.TrimSuffix(strings.Split(s.SeasonDist[i].Name, " ")[0], "季")
		nj := strings.TrimSuffix(strings.Split(s.SeasonDist[j].Name, " ")[0], "季")
		a, _ := strconv.Atoi(ni)
		b, _ := strconv.Atoi(nj)
		return a < b
	})

	// Top 大容量作品
	top := make([]*Work, len(works))
	copy(top, works)
	sort.Slice(top, func(i, j int) bool { return top[i].TotalSize > top[j].TotalSize })
	if len(top) > 20 {
		top = top[:20]
	}
	s.TopBig = top

	// 最近入库
	rec := make([]*MediaFile, len(files))
	copy(rec, files)
	sort.Slice(rec, func(i, j int) bool { return rec[i].ModTime.After(rec[j].ModTime) })
	if len(rec) > 30 {
		rec = rec[:30]
	}
	s.Recent = rec

	// 异常清单
	s.AnomalyList = buildAnomalies(works, files)
	s.Stats.AnomalyCount = len(s.AnomalyList)
	return s
}

func finalizeWork(w *Work) {
	seasons := map[int][]int{}
	for _, f := range w.Files {
		if f.Kind == "电影" {
			continue
		}
		seasons[f.Season] = append(seasons[f.Season], f.Episode)
	}
	w.SeasonCount = len(seasons)
	w.EpisodeSum = 0
	var seasonList []int
	for sn := range seasons {
		seasonList = append(seasonList, sn)
	}
	sort.Ints(seasonList)

	// 缺集检测
	for _, sn := range seasonList {
		eps := seasons[sn]
		w.EpisodeSum += len(eps)
		sort.Ints(eps)
		seen := map[int]int{}
		for _, e := range eps {
			seen[e]++
		}
		// 重复
		var dups []int
		for e, c := range seen {
			if c > 1 {
				dups = append(dups, e)
			}
		}
		sort.Ints(dups)
		for _, e := range dups {
			w.Duplicates = append(w.Duplicates, fmt.Sprintf("S%02dE%02d 重复 %d 个文件", sn, e, seen[e]))
		}
		// 缺口：只在集号范围合理时检测（<=1000）；SP（Season 00，总集篇/特别篇）集号天然不连续，跳过
		if len(eps) == 0 || sn == 0 {
			continue
		}
		minE, maxE := eps[0], eps[len(eps)-1]
		if maxE <= 0 || maxE > 1000 {
			continue
		}
		var gaps []int
		for e := minE; e <= maxE; e++ {
			if seen[e] == 0 {
				gaps = append(gaps, e)
			}
		}
		if minE > 1 {
			w.Missing = append(w.Missing, fmt.Sprintf("S%02d 起始于 E%02d（前 %d 集未收录）", sn, minE, minE-1))
		}
		if len(gaps) > 0 && len(gaps) <= 30 {
			var strs []string
			for _, g := range gaps {
				strs = append(strs, fmt.Sprintf("E%02d", g))
			}
			w.Missing = append(w.Missing, fmt.Sprintf("S%02d 缺 %s", sn, strings.Join(strs, "、")))
		} else if len(gaps) > 30 {
			w.Missing = append(w.Missing, fmt.Sprintf("S%02d 缺 %d 集（E%02d-E%02d 区间不连续）", sn, len(gaps), minE, maxE))
		}
	}

	// 作品级异常
	if len(w.Volumes) > 1 {
		w.Anomalies = append(w.Anomalies, "跨卷分散："+strings.Join(w.Volumes, "、"))
	}
	if len(w.Dirs) > 1 {
		w.Anomalies = append(w.Anomalies, fmt.Sprintf("分散在 %d 个目录", len(w.Dirs)))
	}
	if w.Year == 0 {
		w.Anomalies = append(w.Anomalies, "缺年份")
	}
	if w.Kind == "电影" && w.FileCount > 1 {
		w.Anomalies = append(w.Anomalies, fmt.Sprintf("单部电影含 %d 个视频文件（多版本/分段）", w.FileCount))
	}
	if w.Kind == "剧集" && w.SubCount > 0 && w.SubCount < w.FileCount {
		w.Anomalies = append(w.Anomalies, fmt.Sprintf("外挂字幕不全 %d/%d", w.FileCount-w.SubCount, w.FileCount))
	}
}

func buildAnomalies(works []*Work, files []*MediaFile) []Anomaly {
	var out []Anomaly
	for _, w := range works {
		for _, m := range w.Missing {
			out = append(out, Anomaly{Level: "warn", Kind: "缺集", Title: fmt.Sprintf("%s (%d)", w.Title, w.Year), Detail: m})
		}
		for _, d := range w.Duplicates {
			out = append(out, Anomaly{Level: "warn", Kind: "重复", Title: fmt.Sprintf("%s (%d)", w.Title, w.Year), Detail: d})
		}
		for _, a := range w.Anomalies {
			lvl := "info"
			if strings.HasPrefix(a, "跨卷") || strings.HasPrefix(a, "单部电影") {
				lvl = "warn"
			}
			out = append(out, Anomaly{Level: lvl, Kind: "结构", Title: fmt.Sprintf("%s (%d)", w.Title, w.Year), Detail: a})
		}
	}
	for _, f := range files {
		for _, a := range f.Anomalies {
			out = append(out, Anomaly{Level: "info", Kind: "文件", Title: f.Title, Detail: a + "：" + f.Path})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level < out[j].Level
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func bucketBy(files []*MediaFile, keyFn func(*MediaFile) string) []Bucket {
	m := map[string]*Bucket{}
	for _, f := range files {
		k := keyFn(f)
		b, ok := m[k]
		if !ok {
			b = &Bucket{Name: k}
			m[k] = b
		}
		b.Count++
		b.Size += f.Size
	}
	out := make([]Bucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

func bucketByWorks(works []*Work, keyFn func(*Work) string) []Bucket {
	m := map[string]*Bucket{}
	for _, w := range works {
		k := keyFn(w)
		b, ok := m[k]
		if !ok {
			b = &Bucket{Name: k}
			m[k] = b
		}
		b.Count++
		b.Size += w.TotalSize
	}
	out := make([]Bucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

func sortWorks(ws []*Work) {
	sort.Slice(ws, func(i, j int) bool {
		if ws[i].Category != ws[j].Category {
			return ws[i].Category < ws[j].Category
		}
		return ws[i].Title < ws[j].Title
	})
}

func containsStr(arr []string, v string) bool {
	for _, a := range arr {
		if a == v {
			return true
		}
	}
	return false
}

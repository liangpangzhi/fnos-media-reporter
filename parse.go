package main

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MediaFile 单个视频文件的结构化信息
type MediaFile struct {
	Path         string    `json:"path"`
	FileName     string    `json:"-"`
	Ext          string    `json:"ext"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	Volume       string    `json:"volume"`
	Library      string    `json:"library"`
	Category     string    `json:"category"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	Year         int       `json:"year"`
	Season       int       `json:"season"`
	Episode      int       `json:"episode"`
	EpisodeTitle string    `json:"episode_title"`
	Resolution   string    `json:"resolution"`
	Source       string    `json:"source"`
	Codec        string    `json:"codec"`
	Audio        string    `json:"audio"`
	Dynamic      string    `json:"dynamic"`
	HasSubtitle  bool      `json:"has_subtitle"`
	SubtitleNote string    `json:"-"`
	Dir          string    `json:"-"`
	RelDir       string    `json:"-"`
	Anomalies    []string  `json:"anomalies,omitempty"`
}

var (
	reTypeSeg   = regexp.MustCompile(`^(电影|电视剧|连续剧|动漫|动画|番剧|纪录片|记录片|综艺)\s*\d*\s*$`)
	reYear      = regexp.MustCompile(`\((\d{4})\)|\[(\d{4})\]|\.(\d{4})\.|（(\d{4})）`)
	reYearLoose = regexp.MustCompile(`(?:^|[\s\.\[]) ((?:19|20)\d{2})(?:[\s\]]|$)`)

	reSE     = regexp.MustCompile(`(?i)s(\d{1,2})\s*[\.\-_ ]?e(\d{1,4})`)
	reNXE    = regexp.MustCompile(`(?i)(?:^|[^\d])(\d{1,2})x(\d{1,3})(?:[^\d]|$)`)
	reEpCN   = regexp.MustCompile(`第\s*(\d{1,4})\s*[集话期回]`)
	reEpEN   = regexp.MustCompile(`(?i)\bep?\s*(\d{1,4})\b`)
	reEpBr   = regexp.MustCompile(`\[\s*(\d{1,4})\s*\]`)
	reEpTail = regexp.MustCompile(`[-_ ](\d{1,4})(?:\.[a-zA-Z0-9]+)?$`)

	reSeasonDir = regexp.MustCompile(`(?i)season\s*(\d{1,2})|^s(\d{1,2})$|第\s*(\d{1,3})\s*季`)
	reSeasonAny = regexp.MustCompile(`(?i)season\s*(\d{1,2})|第\s*(\d{1,3})\s*季`)

	reRes   = regexp.MustCompile(`(?i)(2160p|1080p|1080i|720p|576p|480p|4k|8k)`)
	reSrc   = regexp.MustCompile(`(?i)(remux|bluray|blu-ray|bdrip|brrip|web-dl|webrip|web|hdtv|hdrip|dvdrip|dvd|hdcam|cam|tvrip)`)
	reCodec = regexp.MustCompile(`(?i)(x265|x264|h\.?265|h\.?264|hevc|avc|av1|mpeg-?2|vc-?1)`)
	reAudio = regexp.MustCompile(`(?i)(truehd|dts-?hd(?: ma)?|dts|ddp|eac3|ac3|aac|flac|atmos|dd)\s*([257]\.1|2\.0)?`)
	reDyn   = regexp.MustCompile(`(?i)(hdr10\+?|hdr|dolby\s*vision|dovi|\bdv\b|10bit|8bit|sdr)`)

	reMultiSpace = regexp.MustCompile(`\s{2,}`)
	reJunkTail   = regexp.MustCompile(`(?i)\s*[-–—]\s*(?:第\s*\d+\s*集\s*[-–—]?\s*)?(?:2160p|1080p|720p|480p).*$`)
)

// ParseRawToMedia 将原始文件记录解析为结构化媒体文件
func ParseRawToMedia(rf RawFile) *MediaFile {
	p := rf.Path
	name := path.Base(p)
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	stem := strings.TrimSuffix(name, path.Ext(name))

	mf := &MediaFile{
		Path:     p,
		FileName: name,
		Ext:      ext,
		Size:     rf.Size,
		ModTime:  time.Unix(rf.ModUnix, 0),
		Dir:      path.Dir(p),
	}

	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) > 0 {
		mf.Volume = segs[0]
	}
	rest := segs
	if len(segs) >= 3 && segs[1] == "1000" {
		mf.Library = segs[2]
		rest = segs[3:]
	} else if len(segs) >= 2 {
		mf.Library = segs[1]
		rest = segs[2:]
	}
	if len(rest) > 0 {
		mf.RelDir = strings.Join(rest, "/")
	}

	// ---- 类型目录识别 ----
	category, kind := "", ""
	typeIdx := -1
	scanned := 0
	for i, s := range rest {
		if i == len(rest)-1 {
			break // 文件名段不参与
		}
		if scanned >= 3 {
			break
		}
		if mm := reTypeSeg.FindStringSubmatch(s); mm != nil {
			t := mm[1]
			if category == "" {
				category = t
				if t == "电视剧" || t == "连续剧" {
					kind = "剧集"
				} else if t == "电影" {
					kind = "电影"
				}
			} else if kind == "" {
				if t == "电影" {
					kind = "电影"
				} else if t == "电视剧" || t == "连续剧" {
					kind = "剧集"
				}
			}
			typeIdx = i
		}
		scanned++
	}
	if category == "" {
		category = "其他"
	}

	// ---- 作品目录与季目录 ----
	body := rest
	if typeIdx >= 0 {
		body = rest[typeIdx+1:]
	}
	var dirSegs []string // 作品目录之后的目录段
	workDir := ""
	if len(body) >= 2 {
		workDir = body[0]
		dirSegs = body[1 : len(body)-1]
	} else if len(body) == 1 {
		// 文件直接位于类型目录下
		workDir = ""
		dirSegs = nil
		mf.Anomalies = append(mf.Anomalies, "放置在类型目录根部，无作品文件夹")
	}

	// 季目录
	season := 0
	seasonFromDir := -1
	for _, d := range dirSegs {
		if mm := reSeasonDir.FindStringSubmatch(d); mm != nil {
			seasonFromDir = firstInt(mm[1], mm[2], mm[3])
			break
		}
	}
	// 季目录中可能含技术信息，例如 "第 10 季 - 1080p WEB-DL H264 DDP 5.1"
	for _, d := range dirSegs {
		if mm := reSeasonAny.FindStringSubmatch(d); mm != nil {
			seasonFromDir = firstInt(mm[1], mm[2], "")
			break
		}
	}
	if seasonFromDir >= 0 {
		season = seasonFromDir
	}

	// ---- 文件名字段解析 ----
	seasonFile, episode, epTitle := parseEpisodeFromName(stem)
	if seasonFromDir < 0 && seasonFile >= 0 {
		season = seasonFile
	}
	if episode == 0 && seasonFromDir >= 0 {
		episode = parseEpisodeLoose(stem)
	}

	// ---- 标题与年份 ----
	title, year := parseTitleFromDir(workDir)
	if title == "" {
		title, year = parseTitleFromFile(stem)
		mf.Anomalies = append(mf.Anomalies, "无作品文件夹，标题取自文件名")
	}
	if workDir != "" && year == 0 {
		if y := parseYearFromFile(stem); y > 0 && strings.Contains(normalizeForMatch(stem), normalizeForMatch(title)) {
			year = y
		}
	}
	if year == 0 {
		mf.Anomalies = append(mf.Anomalies, "作品目录缺少年份")
	}

	mf.Title = title
	mf.Year = int(year)
	mf.Season = season
	mf.Episode = episode
	mf.EpisodeTitle = epTitle
	mf.Category = category

	// 电影判定
	if kind == "" {
		if episode > 0 {
			kind = "剧集"
		} else {
			kind = "电影"
		}
	}
	if kind == "电影" && episode > 0 {
		// 电影目录下带集号，多为上下部
		kind = "电影"
	}
	mf.Kind = kind
	if mf.Kind == "电影" {
		mf.Season = 0
		mf.Episode = 0
		if epTitle != "" {
			mf.EpisodeTitle = epTitle
		}
	} else if season == 0 && seasonFromDir < 0 && seasonFile < 0 {
		// 季信息完全缺失的剧集默认归入第 1 季；显式 S00 视为特别篇，保留
		mf.Season = 1
	}

	// ---- 技术标签 ----
	whole := stem + " " + strings.Join(dirSegs, " ")
	mf.Resolution = strings.ToUpper(firstMatch(reRes, whole))
	mf.Source = canonicalSource(firstMatch(reSrc, whole))
	mf.Codec = canonicalCodec(firstMatch(reCodec, whole))
	if mm := reAudio.FindStringSubmatch(whole); mm != nil {
		a := strings.ToUpper(mm[1])
		if mm[2] != "" {
			a += " " + mm[2]
		}
		mf.Audio = a
	}
	mf.Dynamic = strings.ToUpper(firstMatch(reDyn, whole))

	if mf.Title == "" {
		mf.Title = stem
	}
	return mf
}

func firstInt(vals ...string) int {
	for _, v := range vals {
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return -1
}

func firstMatch(re *regexp.Regexp, s string) string {
	if mm := re.FindStringSubmatch(s); mm != nil {
		return mm[1]
	}
	return ""
}

func canonicalSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "bluray", "blu-ray":
		return "BluRay"
	case "remux":
		return "REMUX"
	case "web-dl":
		return "WEB-DL"
	case "webrip", "web":
		return "WEB"
	case "bdrip", "brrip":
		return "BDRip"
	case "hdtv":
		return "HDTV"
	case "dvdrip", "dvd":
		return "DVD"
	}
	if s == "" {
		return ""
	}
	return strings.ToUpper(s)
}

func canonicalCodec(s string) string {
	t := strings.ToLower(strings.ReplaceAll(s, ".", ""))
	switch t {
	case "x265", "h265", "hevc":
		return "H.265/HEVC"
	case "x264", "h264", "avc":
		return "H.264/AVC"
	case "av1":
		return "AV1"
	}
	if t == "" {
		return ""
	}
	return strings.ToUpper(t)
}

// parseEpisodeFromName 返回 季, 集, 集名；season 为 -1 表示文件名未标注季
func parseEpisodeFromName(stem string) (int, int, string) {
	if mm := reSE.FindStringSubmatchIndex(stem); mm != nil {
		s, _ := strconv.Atoi(stem[mm[2]:mm[3]])
		e, _ := strconv.Atoi(stem[mm[4]:mm[5]])
		tail := stem[mm[1]:]
		return s, e, extractEpisodeTitle(tail)
	}
	if mm := reNXE.FindStringSubmatch(stem); mm != nil {
		s, _ := strconv.Atoi(mm[1])
		e, _ := strconv.Atoi(mm[2])
		return s, e, ""
	}
	return -1, 0, ""
}

// extractEpisodeTitle 从 SxxExx 之后的内容中提取干净的单集标题
func extractEpisodeTitle(tail string) string {
	tail = strings.TrimSpace(tail)
	tail = strings.TrimLeft(tail, "-–—_ ")
	parts := strings.Split(tail, " - ")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if isTechOnly(p) {
			continue
		}
		p = reJunkTail.ReplaceAllString(p, "")
		p = strings.TrimSpace(p)
		// 去掉语言/字幕后缀标记
		for _, s := range []string{".zh", ".chs", ".cht", ".eng", ".CHS", ".ENG"} {
			p = strings.TrimSuffix(p, s)
		}
		if p == "" {
			continue
		}
		return p
	}
	return ""
}

func isTechOnly(s string) bool {
	t := strings.ToLower(s)
	tech := []string{"2160p", "1080p", "1080i", "720p", "480p", "4k", "web-dl", "webrip", "bluray", "blu-ray",
		"remux", "h264", "h265", "x264", "x265", "hevc", "avc", "ddp", "ac3", "aac", "dts", "truehd", "atmos",
		"hdr", "10bit", "8bit", "repack", "proper", "internal", "complete"}
	hits := 0
	words := strings.FieldsFunc(t, func(r rune) bool { return r == ' ' || r == '.' || r == '-' || r == '_' })
	for _, w := range words {
		for _, tt := range tech {
			if w == tt {
				hits++
				break
			}
		}
	}
	return hits > 0 && hits >= len(words)-1
}

func parseEpisodeLoose(stem string) int {
	if mm := reEpCN.FindStringSubmatch(stem); mm != nil {
		n, _ := strconv.Atoi(mm[1])
		return n
	}
	if mm := reEpEN.FindStringSubmatch(stem); mm != nil {
		n, _ := strconv.Atoi(mm[1])
		return n
	}
	if mm := reEpBr.FindStringSubmatch(stem); mm != nil {
		n, _ := strconv.Atoi(mm[1])
		if n > 0 && n < 1000 {
			return n
		}
	}
	if mm := reEpTail.FindStringSubmatch(stem); mm != nil {
		n, _ := strconv.Atoi(mm[1])
		if n > 0 && n < 1000 {
			return n
		}
	}
	return 0
}

// parseTitleFromDir 从作品目录名解析标题与年份
func parseTitleFromDir(dir string) (string, int) {
	if dir == "" {
		return "", 0
	}
	year := 0
	if mm := reYear.FindStringSubmatch(dir); mm != nil {
		for _, g := range mm[1:] {
			if g != "" {
				year, _ = strconv.Atoi(g)
				break
			}
		}
	}
	t := dir
	if year > 0 {
		t = reYear.ReplaceAllString(t, "")
	}
	t = strings.TrimSpace(t)
	t = strings.Trim(t, "[]（）() ")
	t = reMultiSpace.ReplaceAllString(t, " ")
	if t == "" {
		t = dir
	}
	return t, year
}

func parseTitleFromFile(stem string) (string, int) {
	t := stem
	if loc := reSE.FindStringIndex(t); loc != nil {
		t = t[:loc[0]]
	} else if mm := reEpCN.FindStringIndex(t); mm != nil {
		t = t[:mm[0]]
	} else if mm := reEpEN.FindStringIndex(t); mm != nil {
		t = t[:mm[0]]
	}
	t = strings.Trim(t, " -–—_.")
	t = reMultiSpace.ReplaceAllString(t, " ")
	year := parseYearFromFile(stem)
	if year > 0 {
		t2 := reYear.ReplaceAllString(t, "")
		t2 = strings.Trim(t2, " -–—_.")
		if strings.TrimSpace(t2) != "" {
			t = strings.TrimSpace(t2)
		}
	}
	return strings.TrimSpace(t), year
}

func parseYearFromFile(stem string) int {
	if mm := reYear.FindStringSubmatch(stem); mm != nil {
		for _, g := range mm[1:] {
			if g != "" {
				n, _ := strconv.Atoi(g)
				return n
			}
		}
	}
	return 0
}

func normalizeForMatch(s string) string {
	r := strings.NewReplacer(" ", "", "[", "", "]", "", "(", "", ")", "", "（", "", "）", "", "_", "", "-", "")
	return strings.ToLower(r.Replace(s))
}

func (m *MediaFile) Location() string {
	if m.RelDir == "" {
		return "/" + m.Volume
	}
	return fmt.Sprintf("/%s/%s", m.Volume, m.RelDir)
}

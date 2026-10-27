package main

// TMDB 封面集成。
//
// 为什么不用 api.themoviedb.org 作默认：
// 在板子（OrangePi Zero 2W，国内网络）实测，api.themoviedb.org 被解析到
// 199.16.156.75 / face:b00c::（Twitter、Facebook 网段，DNS 污染），连接必超时；
// 而 api.tmdb.org（TMDB 的旧域名，解析到 CloudFront 99.84.152.92）与
// image.tmdb.org 都正常。所以默认走 api.tmdb.org，并保留多域名回退，
// 域名被封或换网络时用户可在设置里改 tmdb_host。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// 默认 API 域名：实测在国内网络可用
	defaultTMDBHost = "https://api.tmdb.org"
	// 备选域名，按顺序回退
	fallbackTMDBHost = "https://api.themoviedb.org"
	// 图片 CDN，实测可用
	tmdbImageBase = "https://image.tmdb.org/t/p"

	// 封面尺寸：w342≈75KB / w500≈143KB / original≈1.1MB
	// 卡片是 2:3 的小图，w342 足够清晰且省流量；详情弹窗也用同一张。
	tmdbPosterSize = "w342"
)

// tmdbClient 走 proxy.go 的共享 Transport：设置里的代理对它即时生效。
var tmdbClient = newHTTPClient(20 * time.Second)

var (
	// 从标题里剥掉季数标记，用于搜索
	reSeasonSuffix = regexp.MustCompile(`(?i)\s*(第\s*[0-9一二三四五六七八九十]+\s*[季期]|season\s*[0-9]+|[0-9]+(st|nd|rd|th)\s*season|part\s*[0-9]+)\s*$`)
	// 标题里的标点，归一化时去掉
	reTitleNoise = regexp.MustCompile(`[\s\-_·:：!！?？,，.。'"“”‘’()（）\[\]【】/\\~～&+]`)
)

// TMDBSeason 是详情接口里 seasons[] 的一项。
// 一季一行，episode_count 才是「这一季有多少集」。
type TMDBSeason struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
}

// TMDBResult 是 TMDB 返回的剧集条目（只取用得到的字段）
type TMDBResult struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`          // 中文名（language=zh-CN 时）
	OriginalName string  `json:"original_name"` // 原名（日文/英文）
	PosterPath   string  `json:"poster_path"`
	FirstAirDate string  `json:"first_air_date"` // 2024-10-04
	Overview     string  `json:"overview"`
	Popularity   float64 `json:"popularity"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	// NumberOfEpisodes 是**整个条目所有季的合计**，只有 /3/tv/{id} 详情接口返回。
	// 多季番不能直接用它当某一季的总集数（幼女战记 2 季合计 24，但每季只有 12），
	// 一季的集数请走 EpisodesOfSeason。
	NumberOfEpisodes int `json:"number_of_episodes"`
	// NumberOfSeasons 与 Seasons 同样只有详情接口返回，用来按季取集数。
	NumberOfSeasons int          `json:"number_of_seasons"`
	Seasons         []TMDBSeason `json:"seasons"`
}

// EpisodesOfSeason 取「某一季」的集数，取不到返回 0（表示未知）。
//
// 优先级：seasons[] 里该季的 episode_count > 单季番的 number_of_episodes。
// season 0 是特别篇，不参与；多季番若 seasons[] 里找不到目标季，
// 宁可返回 0（界面只显示已有集数），也不能拿全季合计去冒充。
func EpisodesOfSeason(r *TMDBResult, season int) int {
	if r == nil {
		return 0
	}
	if season <= 0 {
		season = 1
	}
	for _, s := range r.Seasons {
		if s.SeasonNumber == season {
			return s.EpisodeCount
		}
	}
	// 单季番没必要非得有 seasons[]，此时全季合计就等于这一季
	if r.NumberOfSeasons <= 1 {
		return r.NumberOfEpisodes
	}
	return 0
}

type tmdbSearchResp struct {
	Page    int          `json:"page"`
	Results []TMDBResult `json:"results"`
	Total   int          `json:"total_results"`
}

// tmdbKeyOf 取配置里的 API Key（v3 key）
func tmdbKeyOf(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.TMDBKey)
}

// tmdbHosts 返回候选 API 域名：用户配置的优先，然后默认域名，最后备选
func tmdbHosts(cfg *Config) []string {
	var out []string
	add := func(h string) {
		h = strings.TrimRight(strings.TrimSpace(h), "/")
		if h == "" {
			return
		}
		for _, x := range out {
			if x == h {
				return
			}
		}
		out = append(out, h)
	}
	if cfg != nil {
		add(cfg.TMDBHost)
	}
	add(defaultTMDBHost)
	add(fallbackTMDBHost)
	return out
}

// tmdbGetJSON 依次尝试各域名，第一个成功的返回。
// 返回实际使用的域名，便于日志排查。
func tmdbGetJSON(cfg *Config, path string, q url.Values, out any) (string, error) {
	key := tmdbKeyOf(cfg)
	if key == "" {
		return "", fmt.Errorf("未配置 TMDB API Key")
	}
	var lastErr error
	for _, host := range tmdbHosts(cfg) {
		vals := url.Values{}
		for k, v := range q {
			vals[k] = v
		}
		vals.Set("api_key", key)
		full := host + path + "?" + vals.Encode()

		req, err := http.NewRequest("GET", full, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "anime-go/0.2 (TMDB cover)")

		resp, err := tmdbClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", host, err)
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = fmt.Errorf("%s: %w", host, rerr)
			continue
		}
		if resp.StatusCode != 200 {
			// 401 = key 无效，换域名也没用，直接返回
			if resp.StatusCode == 401 {
				return host, fmt.Errorf("TMDB 拒绝了 API Key（401），请检查 tmdb_key")
			}
			lastErr = fmt.Errorf("%s: HTTP %d %s", host, resp.StatusCode, snippet(body, 120))
			continue
		}
		if err := json.Unmarshal(body, out); err != nil {
			lastErr = fmt.Errorf("%s: 解析 JSON 失败: %w", host, err)
			continue
		}
		return host, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 TMDB 域名")
	}
	return "", lastErr
}

// TMDBSearchTV 按番名搜索剧集（language=zh-CN 会返回中文名）
func TMDBSearchTV(cfg *Config, query string) ([]TMDBResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索词为空")
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("language", "zh-CN")
	q.Set("include_adult", "false")
	var r tmdbSearchResp
	if _, err := tmdbGetJSON(cfg, "/3/search/tv", q, &r); err != nil {
		return nil, err
	}
	return r.Results, nil
}

// TMDBDetail 按 id 取单条详情（用于校正名称/年份）
func TMDBDetail(cfg *Config, id int) (*TMDBResult, error) {
	if id <= 0 {
		return nil, fmt.Errorf("番号无效")
	}
	q := url.Values{}
	q.Set("language", "zh-CN")
	var r TMDBResult
	if _, err := tmdbGetJSON(cfg, "/3/tv/"+strconv.Itoa(id), q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// tmdbPosterURL 拼图片地址
func tmdbPosterURL(posterPath, size string) string {
	p := strings.TrimSpace(posterPath)
	if p == "" {
		return ""
	}
	if size == "" {
		size = tmdbPosterSize
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return tmdbImageBase + "/" + size + p
}

// normTitle 归一化标题，用于比较："胆大党 第二季" → "胆大党第二季" 的去标点小写形式
func normTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reTitleNoise.ReplaceAllString(s, "")
	return s
}

// cleanSearchTitle 去掉季数后缀，作为搜索词
func cleanSearchTitle(title string) string {
	t := strings.TrimSpace(title)
	t = stripYear(t)
	t = reSeasonSuffix.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

// seasonInTitle 尝试从标题里认出季数（第二季/Season 2/2期），认不出返回 0
func seasonInTitle(title string) int {
	t := strings.TrimSpace(title)
	if m := reSeasonSuffix.FindString(t); m != "" {
		digits := regexp.MustCompile(`\d+`).FindString(m)
		if digits != "" {
			if n, err := strconv.Atoi(digits); err == nil {
				return n
			}
		}
		// 中文数字
		for i, cn := range []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"} {
			if strings.Contains(m, cn) {
				return i + 1
			}
		}
		if strings.Contains(m, "十") {
			return 10
		}
	}
	return 0
}

// TMDBPickBest 从候选里挑最匹配的一条。
// 策略：名称归一化完全相同 → 原名相同 → 包含关系 → 回退到热度最高。
// 同时优先有封面的条目。
func TMDBPickBest(results []TMDBResult, title string, wantSeason int) *TMDBResult {
	if len(results) == 0 {
		return nil
	}
	nt := normTitle(cleanSearchTitle(title))
	// 只保留有封面的（没封面拿来也没用），但全都没有时退回全集
	var withPoster []TMDBResult
	for _, r := range results {
		if strings.TrimSpace(r.PosterPath) != "" {
			withPoster = append(withPoster, r)
		}
	}
	pool := withPoster
	if len(pool) == 0 {
		pool = results
	}

	best := -1
	bestScore := -1 << 30
	for i, r := range pool {
		score := 0
		nn := normTitle(r.Name)
		no := normTitle(r.OriginalName)
		switch {
		case nt != "" && nn == nt:
			score += 1000
		case nt != "" && no == nt:
			score += 900
		case nt != "" && nn != "" && (strings.Contains(nn, nt) || strings.Contains(nt, nn)):
			score += 500
		case nt != "" && no != "" && (strings.Contains(no, nt) || strings.Contains(nt, no)):
			score += 450
		}
		// 季数匹配：想要第 2 季而结果名含「第二季/Season 2」→ 加分
		if wantSeason > 1 {
			rs := seasonInTitle(r.Name)
			if rs == 0 {
				rs = seasonInTitle(r.OriginalName)
			}
			if rs == wantSeason {
				score += 300
			}
		}
		// 轻微偏好热门/高分，作为同分时的次序
		score += int(r.VoteAverage * 5)
		if r.VoteCount > 100 {
			score += 20
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	if best < 0 {
		best = 0
	}
	return &pool[best]
}

// ------------------------- 封面缓存 -------------------------

// coverDir 返回封面目录：配置文件同级的 covers/
func (a *App) coverDir() string {
	dir := dirOf(a.cfgPath)
	if dir == "" || dir == "." {
		dir = "."
	}
	return filepath.Join(dir, "covers")
}

// coverPath 某个订阅的封面文件绝对路径
func (a *App) coverPath(subID string) string {
	return filepath.Join(a.coverDir(), safeFileName(subID)+".jpg")
}

// safeFileName 只保留字母数字和 . - _，防止路径穿越
func safeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "cover"
	}
	return out
}

// downloadCover 下载封面到本地，返回写入的字节数
func downloadCover(posterPath, destPath string, size string) (int, error) {
	u := tmdbPosterURL(posterPath, size)
	if u == "" {
		return 0, fmt.Errorf("没有封面地址")
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "anime-go/0.2 (TMDB cover)")
	resp, err := tmdbClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载封面失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("下载封面失败: HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, fmt.Errorf("创建封面目录失败: %w", err)
	}
	// 先写临时文件再 rename，避免半截文件被浏览器缓存
	tmp := destPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, 8<<20))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		if copyErr != nil {
			return 0, copyErr
		}
		return 0, closeErr
	}
	if n < 512 {
		os.Remove(tmp)
		return 0, fmt.Errorf("封面文件过小（%d 字节），可能不是图片", n)
	}
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return int(n), nil
}

// FetchCover 为一条订阅抓取封面：搜 TMDB → 挑最佳 → 下载 → 写回配置。
// 返回给人看的简短说明（成功或原因），error 表示彻底失败。
//
// 默认走 mikan（一季一图），配 cover_source=tmdb 时走下面的 fetchCoverTMDB。
// 无论哪条路成功，都会顺带用 TMDB 把总集数补上（角标「已有/总数」要用），
// 补总集数失败不影响封面本身。
func (a *App) FetchCover(sub *Subscription) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("订阅为空")
	}
	if a.coverSourceOf() == "tmdb" {
		return a.fetchCoverTMDB(sub)
	}
	msg, err := a.fetchCoverMikan(sub)
	if err != nil {
		// mikan 拿不到就退回 TMDB，别让用户面对一张空封面。
		if tmdbKeyOf(a.cfg) != "" {
			if msg2, err2 := a.fetchCoverTMDB(sub); err2 == nil {
				return msg2, nil
			}
		}
		return "", err
	}
	a.fillEpTotalFromTMDB(sub)
	return msg, nil
}

// fetchCoverTMDB 是原来的 TMDB 取海报逻辑。
//
// sub.TMDBID > 0 时直接用这一条，不再搜索——用户手动指定过的条目必须被尊重，
// 否则界面上换了一条，后台又按名字搜回原来那条，等于白改。
func (a *App) fetchCoverTMDB(sub *Subscription) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("订阅为空")
	}
	if tmdbKeyOf(a.cfg) == "" {
		return "", fmt.Errorf("未配置 TMDB API Key，跳过封面抓取")
	}
	var best *TMDBResult
	if sub.TMDBID > 0 {
		d, err := TMDBDetail(a.cfg, sub.TMDBID)
		if err == nil && d != nil {
			best = d
		}
		// 指定的条目取不到（被删/改）就退回按名字搜
	}
	if best == nil {
		query := cleanSearchTitle(sub.Title)
		if query == "" {
			query = strings.TrimSpace(sub.Title)
		}
		results, err := TMDBSearchTV(a.cfg, query)
		if err != nil {
			return "", err
		}
		if len(results) == 0 {
			return "", fmt.Errorf("TMDB 没有搜到「%s」", query)
		}
		best = TMDBPickBest(results, sub.Title, sub.Season)
		if best == nil {
			return "", fmt.Errorf("TMDB 没有搜到可用的「%s」", query)
		}
		// 搜索结果里没有 seasons[]，补一次详情才能按季算集数，
		// 否则多季番会把全季合计（幼女战记 24）当成单季集数。
		if d, derr := TMDBDetail(a.cfg, best.ID); derr == nil && d != nil {
			best = d
		}
	}
	if strings.TrimSpace(best.PosterPath) == "" {
		sub.TMDBID = best.ID
		sub.CoverName = best.Name
		sub.CoverYear = firstAirYear(best.FirstAirDate)
		sub.EpTotal = EpisodesOfSeason(best, sub.Season)
		// 没海报就不动 CoverFrom：封面没换成，来源也不该跟着变。
		return "", fmt.Errorf("「%s」在 TMDB 上没有海报", best.Name)
	}
	path := a.coverPath(sub.ID)
	n, err := downloadCover(best.PosterPath, path, tmdbPosterSize)
	if err != nil {
		return "", err
	}
	sub.TMDBID = best.ID
	sub.Cover = filepath.Base(path)
	sub.CoverName = best.Name
	sub.CoverYear = firstAirYear(best.FirstAirDate)
	sub.EpTotal = EpisodesOfSeason(best, sub.Season)
	sub.CoverFrom = "tmdb"
	return fmt.Sprintf("封面已更新：%s（%s，%d KB）", best.Name, sub.CoverYear, n/1024), nil
}

// ============================================================
//  TMDB 条目匹配（给「新增订阅」界面用）
//
//  为什么要做这件事：封面是从 TMDB 搜出来自动挑的，但挑错时用户既看不见
//  也无从修改——界面里只有一个封面对不对的印象。所以把「匹配到哪一条」
//  显式呈现出来：给出 TMDB 链接，认错时用户能自己换一条，也能决定
//  订阅名用 mikan 的番名还是 TMDB 的中文名。
// ============================================================

// TMDBMatch 是给界面展示的一条 TMDB 匹配结果
type TMDBMatch struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`          // 中文名
	OriginalName string  `json:"original_name"` // 原名
	Year         string  `json:"year"`
	URL          string  `json:"url"`    // TMDB 网页地址
	Poster       string  `json:"poster"` // 可直接用的图片地址
	Overview     string  `json:"overview"`
	Vote         float64 `json:"vote"`
	Query        string  `json:"query"` // 实际用的搜索词，便于排查
	// PosterPath 是 TMDB 原始海报路径，仅供内部下载用，不下发前端
	PosterPath string `json:"-"`
}

// tmdbEntryURL 生成 TMDB 网页地址
func tmdbEntryURL(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("https://www.themoviedb.org/tv/%d", id)
}

// tmdbMatchOf 把搜索结果转成界面用的结构
func tmdbMatchOf(r *TMDBResult, query string) *TMDBMatch {
	if r == nil {
		return nil
	}
	return &TMDBMatch{
		ID:           r.ID,
		Name:         r.Name,
		OriginalName: r.OriginalName,
		Year:         firstAirYear(r.FirstAirDate),
		URL:          tmdbEntryURL(r.ID),
		Poster:       tmdbPosterURL(r.PosterPath, "w185"),
		Overview:     r.Overview,
		Vote:         r.VoteAverage,
		Query:        query,
		PosterPath:   r.PosterPath,
	}
}

// MatchTMDB 按番名自动匹配一条 TMDB 条目。
// 认不出来时返回 nil 与原因，界面显示「未匹配」并允许手动指定。
func (a *App) MatchTMDB(title string, season int) (*TMDBMatch, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("番名为空")
	}
	if tmdbKeyOf(a.cfg) == "" {
		return nil, fmt.Errorf("未配置 TMDB API Key")
	}
	q := cleanSearchTitle(title)
	if q == "" {
		q = title
	}
	results, err := TMDBSearchTV(a.cfg, q)
	if err != nil {
		return nil, err
	}
	// 去掉季数后搜不到，再用原样搜一次（有些番名带季数才有结果）
	if len(results) == 0 && q != title {
		if r2, err2 := TMDBSearchTV(a.cfg, title); err2 == nil && len(r2) > 0 {
			results, q = r2, title
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("TMDB 没有搜到「%s」", q)
	}
	best := TMDBPickBest(results, title, season)
	if best == nil {
		return nil, fmt.Errorf("TMDB 没有可用的「%s」", q)
	}
	return tmdbMatchOf(best, q), nil
}

// MatchTMDBByID 按 TMDB id 取一条（用户手动指定后用）
func (a *App) MatchTMDBByID(id int) (*TMDBMatch, error) {
	if tmdbKeyOf(a.cfg) == "" {
		return nil, fmt.Errorf("未配置 TMDB API Key")
	}
	d, err := TMDBDetail(a.cfg, id)
	if err != nil {
		return nil, err
	}
	return tmdbMatchOf(d, ""), nil
}

// firstAirYear 从 2024-10-04 取 2024
func firstAirYear(d string) string {
	d = strings.TrimSpace(d)
	if len(d) >= 4 && isDigits(d[:4]) {
		return d[:4]
	}
	return ""
}

// FetchCoverAsync 后台抓封面（不阻塞订阅/保存流程），完成后回写配置并记日志。
func (a *App) FetchCoverAsync(subID string) {
	go func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		sub := a.cfg.FindSub(subID)
		if sub == nil {
			return
		}
		msg, err := a.FetchCover(sub)
		if err != nil {
			// 没有海报也算「识别到了条目」：TMDBID / EpTotal 已经写进订阅，
			// 总集数角标依赖它，所以失败分支同样要回写配置。
			if sub.TMDBID > 0 || sub.EpTotal > 0 {
				if serr := SaveConfig(a.cfgPath, a.cfg); serr != nil {
					a.log.Printf("⚠ 条目信息回写失败（%s）：%v", sub.Title, serr)
				}
			}
			a.log.Printf("⚠ 封面抓取失败（%s）：%v", sub.Title, err)
			return
		}
		if err := SaveConfig(a.cfgPath, a.cfg); err != nil {
			a.log.Printf("⚠ 封面已下载但配置回写失败（%s）：%v", sub.Title, err)
			return
		}
		a.log.Printf("✓ %s", msg)
	}()
}

// snippet 截断长文本用于错误信息
func snippet(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// countCovers 统计 covers/ 下已有多少张封面（设置页展示用）
func (a *App) countCovers() int {
	ents, err := os.ReadDir(a.coverDir())
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") {
			n++
		}
	}
	return n
}

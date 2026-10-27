package main

// ============================================================
//  bgm.tv（Bangumi 番组计划）元数据源
//
//  用户 2026-10-27 要求：TMDB 与 bangumi 合并成「源数据」栏目，
//  里面既能配 TMDB 的地址/Key，也能配 bangumi 的 API 地址与 Token，
//  由它们负责抓中文番名、集数、海报。
//
//  为什么值得接（而不是只用 TMDB）：
//    - TMDB 的简体中文条目覆盖率不如 bgm.tv，很多季番只有日文原名或
//      繁体译名，看着和 mikan 上的名字对不上；
//    - bgm.tv 是中文动漫数据库，季番/剧场版都有独立条目与中文名，
//      连 OVA/特别篇都有，正好补上 TMDB 缺的那块；
//    - bgm.tv 的条目自带「总集数」字段（eps），比 TMDB 按季推更直接。
//
//  接口（v0，2026-10 实测）：
//    POST /v0/search/subjects   body 是 JSON：
//         {"keyword":"胆大党","filter":{"type":[2]}}
//         → {"results":N,"data":[{id,name,name_cn,images,eps,...}]}
//    GET  /v0/subjects/{id}
//         → 单条详情（字段同上，另带 summary/date/rating）
//
//  鉴权：官方建议带 Authorization: Bearer <token>（限流更宽松），
//  不带也能查；UA 必须能识别出应用（对方会拦默认 UA）。
//
//  注意：不要把它写成「替代 TMDB」。两者的强项不同——
//  TMDB 的海报分辨率高、剧场版条目全；bgm.tv 的中文名准。
//  所以这里只做「并列的第二个源」，由 covers_ui.go 的分支按用户选择走。
// ============================================================

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// bangumiClient 走 proxy.go 的共享 Transport：设置里的代理对它即时生效。
var bangumiClient = newHTTPClient(20 * time.Second)

// bgm.tv 的条目类型：1=书籍 2=动画 3=音乐 4=游戏 6=三次元
const bangumiTypeAnime = 2

// BangumiImages 是条目的图片集合。bgm.tv 给多个尺寸，取 large 最清楚。
type BangumiImages struct {
	Large  string `json:"large"`
	Common string `json:"common"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
	Grid   string `json:"grid"`
}

// BangumiSubject 是 bgm.tv 的一个条目（只取用得到的字段）。
type BangumiSubject struct {
	ID     int           `json:"id"`
	Name   string        `json:"name"`    // 原名（日文）
	NameCN string        `json:"name_cn"` // 中文名
	Images BangumiImages `json:"images"`
	// Eps 是这个条目的总集数。bgm.tv 对剧场版给 1，对 TV 番给实际集数，
	// 未知时为 0——所以 0 必须当成「不知道」，不能当成「0 集」。
	Eps      int    `json:"eps"`
	Date     string `json:"date"` // 2024-10-04
	Summary  string `json:"summary"`
	Platform string `json:"platform"`
	// Rating 只有详情接口返回
	Rating struct {
		Total int     `json:"total"`
		Score float64 `json:"score"`
	} `json:"rating"`
	// Type 是条目类型；搜索接口不返回它，详情接口返回。
	Type int `json:"type"`
}

// DisplayName 返回「该显示哪个名字」：优先中文名，没有就用原名。
//
// 为什么不在调用处各自判断：中文名的兜底规则要一致，
// 否则会出现「列表显示中文、落盘目录用日文」的错位。
func (b *BangumiSubject) DisplayName() string {
	if b == nil {
		return ""
	}
	if s := strings.TrimSpace(b.NameCN); s != "" {
		return s
	}
	return strings.TrimSpace(b.Name)
}

// PosterURL 返回海报地址（没有就返回空串）。
func (b *BangumiSubject) PosterURL() string {
	if b == nil {
		return ""
	}
	for _, u := range []string{b.Images.Large, b.Images.Common, b.Images.Medium, b.Images.Small} {
		if s := strings.TrimSpace(u); s != "" {
			// bgm.tv 的图片地址有时是 //lain.bgm.tv/... 这种协议相对形式
			if strings.HasPrefix(s, "//") {
				return "https:" + s
			}
			return s
		}
	}
	return ""
}

// bangumiAPIOf 取配置里的 API 根地址（空则用内置默认）。
func bangumiAPIOf(cfg *Config) string {
	if cfg == nil {
		return defaultBangumiAPI
	}
	api := strings.TrimRight(strings.TrimSpace(cfg.BangumiAPI), "/")
	if api == "" {
		return defaultBangumiAPI
	}
	return api
}

// bangumiUsable 判断 bgm.tv 这个源能不能用。
//
// 只要 API 地址有值（默认 https://api.bgm.tv 一定会有）就能用：
// 搜索/详情都是公开接口，不需要 Token。
//
// 早期版本还要求一个 BangumiEnabled 开关，实际用起来是多余的——
// 用户填了 API 就是想用，多一个勾选框只会让人以为「填了却没生效」。
// 现在这个源只在**需要时**才被问到（TMDB 没结果、或封面来源选了 bgm），
// 不用时一次请求都不会发出去。
func bangumiUsable(cfg *Config) bool {
	return cfg != nil && strings.TrimSpace(bangumiAPIOf(cfg)) != ""
}

// bangumiTokenState 说一句「Token 有没有配」（日志用，绝不打印 Token 本身）。
func bangumiTokenState(cfg *Config) string {
	if cfg != nil && strings.TrimSpace(cfg.BangumiToken) != "" {
		return "已配 Token"
	}
	return "未配 Token（公开接口，限流较严）"
}

// bangumiReq 发一个请求到 bgm.tv，统一处理 UA / Token / 超时 / 错误体。
func bangumiReq(cfg *Config, method, path string, body any, out any) error {
	api := bangumiAPIOf(cfg)
	full := api + path

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("组装请求失败: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, full, rdr)
	if err != nil {
		return fmt.Errorf("请求地址无效: %w", err)
	}
	// bgm.tv 会拦掉默认的 Go UA（"Go-http-client"），必须自报身份。
	req.Header.Set("User-Agent", "anime-go/0.3 (https://github.com/j257n5sb4w-pixel/anime-go)")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := strings.TrimSpace(cfg.BangumiToken); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := bangumiClient.Do(req)
	if err != nil {
		return fmt.Errorf("连不上 %s: %w", api, err)
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if rerr != nil {
		return fmt.Errorf("读取响应失败: %w", rerr)
	}
	if resp.StatusCode != 200 {
		switch resp.StatusCode {
		case 400:
			return fmt.Errorf("bgm.tv 认为请求不合法（400）：%s", snippet(raw, 160))
		case 401, 403:
			return fmt.Errorf("bgm.tv 拒绝了 Access Token（%d），请检查 bangumi_token", resp.StatusCode)
		case 404:
			return fmt.Errorf("bgm.tv 没有这个条目（404）")
		case 429:
			return fmt.Errorf("bgm.tv 限流了（429），稍再试或填一个 Token")
		}
		return fmt.Errorf("bgm.tv 返回 HTTP %d：%s", resp.StatusCode, snippet(raw, 160))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析 bgm.tv 响应失败: %w", err)
	}
	return nil
}

// bgmSearchResp 是 /v0/search/subjects 的返回体。
//
// 注意字段名是 `data` 不是 `results`——这是 bgm.tv 和 TMDB 最容易写错的地方。
type bgmSearchResp struct {
	Results int              `json:"results"`
	Data    []BangumiSubject `json:"data"`
}

// BangumiSearch 按关键词搜动画条目。
//
// 只搜 type=2（动画），否则「胆大党」会把漫画/小说条目一起搜出来，
// 那些条目的集数语义完全不同（漫画是卷数），混进来只会误导集数识别。
func BangumiSearch(cfg *Config, query string) ([]BangumiSubject, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索词为空")
	}
	body := map[string]any{
		"keyword": query,
		"filter": map[string]any{
			"type": []int{bangumiTypeAnime},
		},
	}
	var r bgmSearchResp
	if err := bangumiReq(cfg, "POST", "/v0/search/subjects", body, &r); err != nil {
		return nil, err
	}
	return r.Data, nil
}

// BangumiDetail 按 id 取单条详情（拿 eps/中文名/海报/评分）。
func BangumiDetail(cfg *Config, id int) (*BangumiSubject, error) {
	if id <= 0 {
		return nil, fmt.Errorf("条目 id 无效")
	}
	var r BangumiSubject
	if err := bangumiReq(cfg, "GET", "/v0/subjects/"+strconv.Itoa(id), nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// BangumiPickBest 从候选里挑最匹配的一条。
//
// 打分规则和 TMDBPickBest 保持同一套思路（精确 > 原名 > 包含 > 热度），
// 但多一条 bgm.tv 特有的加分：name_cn 与搜索词完全相同时分数最高——
// 因为用户输入的大多是中文名，而 bgm.tv 的中文名是人工录入的，比 TMDB 准。
func BangumiPickBest(results []BangumiSubject, title string, wantSeason int) *BangumiSubject {
	if len(results) == 0 {
		return nil
	}
	nt := normTitle(cleanSearchTitle(title))

	// 优先有海报的（没海报的条目拿来也补不了封面），全都没有时退回全集。
	var withPoster []BangumiSubject
	for _, r := range results {
		if r.PosterURL() != "" {
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
		ncn := normTitle(r.NameCN)
		nname := normTitle(r.Name)
		switch {
		case nt != "" && ncn == nt:
			score += 1200 // 中文名完全一致：最强的信号
		case nt != "" && nname == nt:
			score += 1000
		case nt != "" && ncn != "" && (strings.Contains(ncn, nt) || strings.Contains(nt, ncn)):
			score += 600
		case nt != "" && nname != "" && (strings.Contains(nname, nt) || strings.Contains(nt, nname)):
			score += 500
		}
		// 季数匹配：想要第 2 季而条目名含「第二季/Season 2」→ 加分
		if wantSeason > 1 {
			rs := seasonInTitle(r.NameCN)
			if rs == 0 {
				rs = seasonInTitle(r.Name)
			}
			if rs == wantSeason {
				score += 300
			}
		}
		// 同分时的次序：评分人数多、评分高的更可能是本体条目
		score += int(r.Rating.Score * 5)
		if r.Rating.Total > 100 {
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

// BangumiMatch 搜一次并挑最佳条目（给「按标题找元数据」的调用方用）。
func BangumiMatch(cfg *Config, title string, season int) (*BangumiSubject, error) {
	if !bangumiUsable(cfg) {
		return nil, fmt.Errorf("bgm.tv 数据源未启用")
	}
	query := cleanSearchTitle(title)
	if query == "" {
		query = strings.TrimSpace(title)
	}
	results, err := BangumiSearch(cfg, query)
	if err != nil {
		return nil, err
	}
	best := BangumiPickBest(results, title, season)
	if best == nil {
		return nil, fmt.Errorf("bgm.tv 上没搜到「%s」", query)
	}
	return best, nil
}

// BangumiMatchByID 按 id 取条目（用户在 UI 里点了具体某条时用）。
func BangumiMatchByID(cfg *Config, id int) (*BangumiSubject, error) {
	if !bangumiUsable(cfg) {
		return nil, fmt.Errorf("bgm.tv 数据源未启用")
	}
	return BangumiDetail(cfg, id)
}

// bangumiEntryURL 拼条目页地址（日志/提示里给人点）。
func bangumiEntryURL(id int) string {
	if id <= 0 {
		return ""
	}
	return "https://bgm.tv/subject/" + strconv.Itoa(id)
}

// TestBangumi 验证「API 地址 + Token」能不能用，并回报一次真实搜索的结果。
//
// 与 testTMDB 同一形态：设置页允许「先测再存」，所以这里只用传入的参数，
// 不读也不写全局配置。
func TestBangumi(cfg *Config, keyword string) (string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		keyword = "孤独摇滚"
	}
	start := time.Now()
	results, err := BangumiSearch(cfg, keyword)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return fmt.Sprintf("连上了（%dms），但「%s」没有结果——可能 API 正常、只是没这个词", ms, keyword), nil
	}
	top := results[0]
	name := top.DisplayName()
	if name == "" {
		name = "（无标题）"
	}
	return fmt.Sprintf("连上了（%dms）：搜「%s」得 %d 条，第一条「%s」（id=%d，eps=%d）",
		ms, keyword, len(results), name, top.ID, top.Eps), nil
}

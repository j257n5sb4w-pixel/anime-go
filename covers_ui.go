package main

// 封面相关的 HTTP 接口：
//   GET  /api/cover?id=<订阅id>        —— 返回本地缓存的封面图（无则 404）
//   GET  /api/tmdb/search?q=<关键词>   —— 搜 TMDB 候选，供手动指定
//   POST /api/sub/cover                —— 为一条订阅抓/换封面（可指定 tmdb_id）
//   POST /api/covers/refresh           —— 给所有缺封面的订阅补抓（后台跑）

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// coverVerOf 返回封面版本号，供前端做 cache-busting。
//
// 用文件 mtime（秒）：换海报时 downloadCover 会重写文件，mtime 随之变化。
// 早期版本前端拿 tmdb_id 当版本号，但同一部番各季共用同一个 tmdb_id，
// 第 2 季换了海报 URL 却不变，浏览器在 max-age（1 天）内一直用旧图，
// 表现为「列表里还是第 1 季海报、点进编辑才是第 2 季」。
func coverVerOf(a *App, sub *Subscription) int64 {
	if a == nil || sub == nil || strings.TrimSpace(sub.Cover) == "" {
		return 0
	}
	p := filepath.Join(a.coverDir(), filepath.Base(sub.Cover))
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return 0
	}
	return st.ModTime().Unix()
}

// handleCover 直接把 covers/ 下的 jpg 吐给浏览器。
// 用 ETag + Cache-Control，浏览器二次访问走 304，省流量。
func (s *server) handleCover(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "缺少 id", 400)
		return
	}
	sub := s.app.cfg.FindSub(id)
	if sub == nil || strings.TrimSpace(sub.Cover) == "" {
		http.Error(w, "没有封面", 404)
		return
	}
	p := filepath.Join(s.app.coverDir(), filepath.Base(sub.Cover))
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		http.Error(w, "封面文件不存在", 404)
		return
	}
	etag := fmt.Sprintf(`"%s-%d"`, filepath.Base(sub.Cover), st.ModTime().Unix())
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// 前端已经用 cover_v（文件 mtime）做参数区分，这里可以放宽缓存；
	// 但为了「换了海报能立刻看到」，还是收短到 1 小时——
	// 光靠 ETag 不够，因为浏览器在 max-age 内根本不发请求。
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", etag)
	http.ServeFile(w, r, p)
}

// handleTMDBSearch 搜 TMDB，返回候选列表（前端让用户挑一个换封面）
func (s *server) handleTMDBSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请填搜索词"})
		return
	}
	if tmdbKeyOf(s.app.cfg) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "未配置 TMDB API Key，请先到「设置」里填写"})
		return
	}
	results, err := TMDBSearchTV(s.app.cfg, q)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	type item struct {
		ID       int     `json:"id"`
		Name     string  `json:"name"`
		Original string  `json:"original_name"`
		Year     string  `json:"year"`
		Poster   string  `json:"poster"` // 可直接用的图片地址（TMDB CDN）
		HasCover bool    `json:"has_cover"`
		Vote     float64 `json:"vote"`
	}
	out := make([]item, 0, len(results))
	for _, x := range results {
		if strings.TrimSpace(x.PosterPath) == "" {
			continue // 没海报的条目对选封面没意义
		}
		out = append(out, item{
			ID:       x.ID,
			Name:     x.Name,
			Original: x.OriginalName,
			Year:     firstAirYear(x.FirstAirDate),
			Poster:   tmdbPosterURL(x.PosterPath, "w185"),
			HasCover: true,
			Vote:     x.VoteAverage,
		})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "count": len(out), "items": out})
}

// handleTMDBMatch 按番名匹配 TMDB 条目，返回最佳匹配 + 候选列表。
//
// 参数：
//
//	title   番名（mikan 侧的名称）
//	season  季数，用于在候选里偏向同名第 N 季
//	id      指定 TMDB id 时直接取这一条（用户手动改过之后）
//
// 界面用它显示「TMDB 识别结果」和链接，认错时用户从候选里换一条。
func (s *server) handleTMDBMatch(w http.ResponseWriter, r *http.Request) {
	if tmdbKeyOf(s.app.cfg) == "" {
		writeJSON(w, 200, map[string]any{
			"ok": false, "error": "未配置 TMDB API Key，无法识别",
			"need_key": true,
		})
		return
	}
	q := r.URL.Query()
	season := 0
	if v := strings.TrimSpace(q.Get("season")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			season = n
		}
	}

	// 指定 id：直接取这一条，候选列表仍按 title 搜（方便继续切换）
	if idStr := strings.TrimSpace(q.Get("id")); idStr != "" {
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "TMDB id 不合法"})
			return
		}
		m, err := s.app.MatchTMDBByID(id)
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "match": m, "candidates": s.tmdbCandidates(q.Get("title"), season)})
		return
	}

	title := strings.TrimSpace(q.Get("title"))
	if title == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少 title"})
		return
	}
	m, err := s.app.MatchTMDB(title, season)
	cands := s.tmdbCandidates(title, season)
	if err != nil {
		// 认不出来不是错误：界面显示「未匹配」，让用户手动搜
		writeJSON(w, 200, map[string]any{
			"ok": true, "match": nil, "error": err.Error(), "candidates": cands,
		})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "match": m, "candidates": cands})
}

// tmdbCandidates 取前几条候选，供用户手动切换（失败时返回空，不影响主流程）
func (s *server) tmdbCandidates(title string, season int) []TMDBMatch {
	title = strings.TrimSpace(title)
	if title == "" || tmdbKeyOf(s.app.cfg) == "" {
		return []TMDBMatch{}
	}
	q := cleanSearchTitle(title)
	if q == "" {
		q = title
	}
	results, err := TMDBSearchTV(s.app.cfg, q)
	if err != nil || len(results) == 0 {
		return []TMDBMatch{}
	}
	// 把最佳匹配排到最前面，其余按 TMDB 原顺序
	best := TMDBPickBest(results, title, season)
	out := make([]TMDBMatch, 0, len(results))
	if best != nil {
		if m := tmdbMatchOf(best, q); m != nil {
			out = append(out, *m)
		}
	}
	for i := range results {
		if best != nil && results[i].ID == best.ID {
			continue
		}
		if m := tmdbMatchOf(&results[i], q); m != nil {
			out = append(out, *m)
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// coverReq 是 /api/sub/cover 的请求体
type coverReq struct {
	ID     string `json:"id"`
	TMDBID int    `json:"tmdb_id"` // >0 时用指定条目，否则自动搜索
	Query  string `json:"query"`   // 自动搜索时可用别的关键词
	// Source 指定封面来源：mikan（默认，一季一图）/ tmdb。
	// 前端「重新抓取」按钮传 mikan，避免老订阅一直卡在 TMDB 那张总海报上。
	Source string `json:"source"`
}

// handleSubCoverSet 为一条订阅抓取或更换封面。
// 同步执行（要下载图片），前端按钮转圈提示。
func (s *server) handleSubCoverSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req coverReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败: " + err.Error()})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少订阅 id"})
		return
	}

	s.app.mu.Lock()
	defer s.app.mu.Unlock()

	sub := s.app.cfg.FindSub(req.ID)
	if sub == nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在: " + req.ID})
		return
	}

	// 指定了 TMDB 条目，或显式要求走 TMDB，才需要 API Key。
	// 走 mikan 不需要任何 key——这是把封面默认切到 mikan 的直接好处。
	src := strings.ToLower(strings.TrimSpace(req.Source))
	useTMDB := req.TMDBID > 0 || src == "tmdb"
	if useTMDB && req.Query == "" && tmdbKeyOf(s.app.cfg) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "未配置 TMDB API Key，请先到「设置」里填写"})
		return
	}

	var err error
	var msg string
	if req.TMDBID > 0 {
		// 指定条目：直接取详情后下载
		detail, derr := TMDBDetail(s.app.cfg, req.TMDBID)
		if derr != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": derr.Error()})
			return
		}
		if strings.TrimSpace(detail.PosterPath) == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "这个条目在 TMDB 上没有海报"})
			return
		}
		p := s.app.coverPath(sub.ID)
		n, derr2 := downloadCover(detail.PosterPath, p, tmdbPosterSize)
		if derr2 != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": derr2.Error()})
			return
		}
		sub.TMDBID = detail.ID
		sub.Cover = filepath.Base(p)
		sub.CoverName = detail.Name
		sub.CoverYear = firstAirYear(detail.FirstAirDate)
		// detail 来自 TMDBDetail，带 seasons[]，可以按季取集数
		sub.EpTotal = EpisodesOfSeason(detail, sub.Season)
		sub.CoverFrom = "tmdb"
		msg = fmt.Sprintf("封面已更换：%s（%s，%d KB）", detail.Name, sub.CoverYear, n/1024)
	} else if src == "mikan" {
		// 明确要求走 mikan：抓这一季自己的海报
		msg, err = s.app.fetchCoverMikan(sub)
		if err == nil {
			s.app.fillEpTotalFromTMDB(sub)
		}
	} else if q := strings.TrimSpace(req.Query); q != "" {
		results, serr := TMDBSearchTV(s.app.cfg, q)
		if serr != nil {
			err = serr
		} else if len(results) == 0 {
			err = fmt.Errorf("TMDB 没有搜到「%s」", q)
		} else {
			best := TMDBPickBest(results, sub.Title, sub.Season)
			if best == nil || strings.TrimSpace(best.PosterPath) == "" {
				err = fmt.Errorf("「%s」的搜索结果里没有可用海报", q)
			} else {
				p := s.app.coverPath(sub.ID)
				n, derr := downloadCover(best.PosterPath, p, tmdbPosterSize)
				if derr != nil {
					err = derr
				} else {
					sub.TMDBID = best.ID
					sub.Cover = filepath.Base(p)
					sub.CoverName = best.Name
					sub.CoverYear = firstAirYear(best.FirstAirDate)
					// 这里 best 来自搜索结果，没有 seasons[]；
					// 重新取一次详情，否则只能拿到全季合计。
					if d, derr := TMDBDetail(s.app.cfg, best.ID); derr == nil && d != nil {
						sub.EpTotal = EpisodesOfSeason(d, sub.Season)
					} else {
						sub.EpTotal = EpisodesOfSeason(best, sub.Season)
					}
					sub.CoverFrom = "tmdb"
					msg = fmt.Sprintf("封面已更新：%s（%s，%d KB）", best.Name, sub.CoverYear, n/1024)
				}
			}
		}
	} else if src == "tmdb" {
		// 明确要求走 TMDB 时不能落到 FetchCover——
		// 那个函数会按全局 cover_source 走，全局是 mikan 时这里就白传了。
		msg, err = s.app.fetchCoverTMDB(sub)
	} else {
		msg, err = s.app.FetchCover(sub)
	}

	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if serr := SaveConfig(s.app.cfgPath, s.app.cfg); serr != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "封面已下载但配置回写失败: " + serr.Error()})
		return
	}
	s.app.log.Printf("✓ %s：%s", sub.Title, msg)
	writeJSON(w, 200, map[string]any{
		"ok": true, "message": msg,
		"cover": sub.Cover, "cover_name": sub.CoverName,
		"cover_year": sub.CoverYear, "tmdb_id": sub.TMDBID,
		"cover_from": sub.CoverFrom, "ep_total": sub.EpTotal,
	})
}

// handleCoversRefresh 给缺封面或缺总集数的订阅补抓（后台异步，立即返回数量）
func (s *server) handleCoversRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	// 封面走 mikan 时不需要 TMDB key；只有显式配了 cover_source=tmdb 才拦。
	if s.app.coverSourceOf() == "tmdb" && tmdbKeyOf(s.app.cfg) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "未配置 TMDB API Key，请先到「设置」里填写"})
		return
	}
	s.app.mu.Lock()
	var ids []string
	useMikan := s.app.coverSourceOf() == "mikan"
	for i := range s.app.cfg.Subscriptions {
		sub := &s.app.cfg.Subscriptions[i]
		// 顺带把「总集数」补上：老订阅有封面但 ep_total 还是 0，
		// 不重抓就永远显示不出 n / 总数。
		missing := strings.TrimSpace(sub.Cover) == "" || sub.EpTotal <= 0
		// 走 mikan 时，从 TMDB 拿来的老封面也算「要补」——
		// 否则老订阅永远停在 TMDB 那张多季共用海报上，换源等于没生效。
		oldSource := useMikan && sub.CoverFrom != "mikan" && !strings.HasPrefix(strings.TrimSpace(sub.Cover), "http")
		if missing || oldSource {
			ids = append(ids, sub.ID)
		}
	}
	total := len(s.app.cfg.Subscriptions)
	s.app.mu.Unlock()

	if len(ids) == 0 {
		writeJSON(w, 200, map[string]any{"ok": true, "queued": 0, "total": total,
			"message": fmt.Sprintf("全部 %d 个订阅都已有封面和总集数", total)})
		return
	}
	for _, id := range ids {
		s.app.FetchCoverAsync(id)
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "queued": len(ids), "total": total,
		"message": fmt.Sprintf("已排队补抓 %d 个订阅的封面/总集数（共 %d 个），稍等几秒刷新即可", len(ids), total),
	})
}

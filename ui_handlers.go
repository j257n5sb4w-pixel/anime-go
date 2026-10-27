package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// SaveConfig 把当前配置写回磁盘（供 Web UI 持久化用）
func SaveConfig(path string, cfg *Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

var reBangumi = regexp.MustCompile(`bangumiId=(\d+)`)

// reGroupTag 提取标题开头的 [字幕组] 标签
var reGroupTag = regexp.MustCompile(`^\s*\[([^\]]+)\]`)

// handleParse 抓 RSS 并返回频道的标题、条目、集数、命中判断
func (s *server) handleParse(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少 url"})
		return
	}
	body, err := fetchFeed(raw, s.app.cfg.UA)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	feedTitle, items, err := ParseFeed(body)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	// 建议番名：从频道标题取，去掉尾部说明
	suggest := strings.TrimSpace(feedTitle)
	if i := strings.Index(suggest, "/"); i > 0 {
		suggest = strings.TrimSpace(suggest[:i])
	}

	// 字幕组：从第一条的 [XXX] 里取
	subgroup := ""
	if len(items) > 0 {
		if m := reGroupTag.FindStringSubmatch(items[0].Title); len(m) > 1 {
			subgroup = strings.TrimSpace(m[1])
		}
	}

	// 集数识别
	tmp := &Subscription{EpPatterns: defaultEpPatterns}
	type row struct {
		Title   string `json:"title"`
		Episode int    `json:"episode"`
		Matched bool   `json:"matched"`
		HasLink bool   `json:"has_link"`
		PubDate string `json:"pub_date"`
	}
	var rows []row
	for _, it := range items {
		ep := ExtractEpisode(it.Title, tmp.EpPatterns)
		hasLink := it.Torrent != "" || it.Magnet != ""
		rows = append(rows, row{
			Title: it.Title, Episode: ep,
			Matched: hasLink, HasLink: hasLink,
			PubDate: it.PubTime.Format("01-02 15:04"),
		})
	}

	// TMDB 匹配：把「识别成了哪一条」直接返回给界面，
	// 认错时用户能看到链接并改掉（以前只有封面，看不出来也没法改）。
	var tmdbErr string
	var tmdb *TMDBMatch
	if tmdbKeyOf(s.app.cfg) != "" {
		if m, err := s.app.MatchTMDB(suggest, 0); err != nil {
			tmdbErr = err.Error()
		} else {
			tmdb = m
		}
	} else {
		tmdbErr = "未配置 TMDB API Key"
	}

	writeJSON(w, 200, map[string]any{
		"ok":            true,
		"feed_title":    feedTitle,
		"suggest_title": suggest,
		"subgroup":      subgroup,
		// 从番名猜季度（「第二季」→2），认不出返回 0，前端按 1 处理。
		// 只是给表单一个默认值，用户随时可以改（「第贝塔季」这种猜不出来）。
		"guess_season": GuessSeasonFromTitle(suggest),
		"count":        len(rows),
		"items":        rows,
		"tmdb":         tmdb,
		"tmdb_error":   tmdbErr,
	})
}

// handleSubscribe 新增订阅。默认回填历史，skip_backfill=true 时只建基线。
func (s *server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req struct {
		URL          string   `json:"url"`
		Title        string   `json:"title"`
		FileTitle    string   `json:"file_title"`
		Subgroup     string   `json:"subgroup"`
		Season       int      `json:"season"`
		WeekLabel    string   `json:"week_label"`
		Match        []string `json:"match"`
		Exclude      []string `json:"exclude"`
		SkipBackfill bool     `json:"skip_backfill"`
		// SavePath / RenameTemplate：留空则用全局默认。
		//
		// 这两个字段原先只在 /api/sub/update 里支持，/api/subscribe 完全忽略它们，
		// 导致「新建时指定的路径被静默丢弃、只认 Settings 里的默认值」。
		// 白名单写窄了（如只允许 /downloads/anime-go/e2e/）时，
		// 新建订阅会莫名其妙报「路径被安全策略拒绝」，而用户明明传了合法 save_path。
		SavePath       string `json:"save_path"`
		RenameTemplate string `json:"rename_template"`
		TotalEpisodes  int    `json:"total_episodes"`
		Enabled        *bool  `json:"enabled"`
		// TMDBID：界面里确认过的 TMDB 条目（0 = 让程序自己搜）。
		// 用户手动改过之后带上来，就按这一条抓封面、也用它当 TMDB 名。
		TMDBID int `json:"tmdb_id"`
		// NameSource：订阅名用哪一侧，"mikan"（默认）或 "tmdb"。
		NameSource string `json:"name_source"`
		// MikanName：mikan 侧的原始番名（选了 TMDB 名时必须给，
		// 否则「切回 mikan 名」就无从下手了）。
		MikanName string `json:"mikan_name"`
		// EpOffset：剧集号偏移（-12 之类），把字幕组连续编号纠正成本季集号。
		EpOffset int `json:"ep_offset"`
		// DirName：番名目录名。留空则由程序扫磁盘解析（复用同名目录）。
		// 允许显式指定，是为了处理「手动建的目录名跟番名对不上」这种边角情况。
		DirName string `json:"dir_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败: " + err.Error()})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.Title = strings.TrimSpace(req.Title)
	if req.URL == "" || req.Title == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "url 和 title 必填"})
		return
	}
	if req.Season <= 0 {
		req.Season = 1
	}

	id := "sub-" + sha1Hex(req.URL)[:8]
	if m := reBangumi.FindStringSubmatch(req.URL); len(m) > 1 {
		id = "ani-" + m[1]
	}

	// 查重：同一个 RSS **且同一个季度**才算重复。
	//
	// 为什么放开「同 RSS 不同季」：mikan 上一个条目偶尔会跨季更新
	// （同一 bangumiId 里既有第 1 季又有第 2 季的资源），
	// 用户明确要求「先订第 2 季、后订第 1 季」也要能用。
	// 只按 URL 判重会把他挡在门外（实测踩到过）。
	s.app.mu.Lock()
	idTaken := false
	for _, x := range s.app.cfg.Subscriptions {
		if x.ID == id {
			idTaken = true
		}
		if x.URL == req.URL && x.Season == req.Season {
			s.app.mu.Unlock()
			writeJSON(w, 409, map[string]any{"ok": false,
				"error": fmt.Sprintf("该 RSS 的第 %d 季已在订阅列表中", req.Season)})
			return
		}
	}
	// ID 撞了（同条目不同季）：给 ID 补上季度后缀，保证两条订阅各自独立。
	// 老订阅的 ID 不受影响（它们本来就不撞）。
	if idTaken {
		id = fmt.Sprintf("%s-s%d", id, req.Season)
	}
	s.app.mu.Unlock()

	sub := Subscription{
		ID: id, Title: req.Title, FileTitle: req.FileTitle,
		URL: req.URL, Season: req.Season, Subgroup: req.Subgroup,
		WeekLabel: req.WeekLabel,
		Match:     req.Match, Exclude: req.Exclude, Enabled: true,
		SkipBackfill:   req.SkipBackfill,
		SavePath:       strings.TrimSpace(req.SavePath),
		RenameTemplate: strings.TrimSpace(req.RenameTemplate),
		TotalEpisodes:  req.TotalEpisodes,
		TMDBID:         req.TMDBID,
		MikanName:      strings.TrimSpace(req.MikanName),
		EpOffset:       req.EpOffset,
		DirName:        strings.TrimSpace(req.DirName),
	}
	if req.Enabled != nil {
		sub.Enabled = *req.Enabled
	}

	// 订阅名取自 mikan 还是 TMDB。
	//
	// 默认 mikan（用户要求）：mikan 的番名就是他的订阅习惯，
	// TMDB 中文名有时会把「第二季」并进主标题，反而更乱。
	src := strings.ToLower(strings.TrimSpace(req.NameSource))
	if src != "tmdb" {
		src = "mikan"
	}
	if src == "tmdb" && sub.TMDBID > 0 {
		// 名字要以 TMDB 为准，但 TMDBID 是前端传的：
		// 仍然回查一次，避免前端传了个和名字对不上的 id。
		if m, err := s.app.MatchTMDBByID(sub.TMDBID); err == nil && strings.TrimSpace(m.Name) != "" {
			if sub.MikanName == "" {
				sub.MikanName = sub.Title // 记住 mikan 原名，方便切回去
			}
			sub.Title = strings.TrimSpace(m.Name)
			sub.FileTitle = "" // 清掉可能过期的缓存，交给 applyDefaults 重算
			if sub.CoverYear == "" {
				sub.CoverYear = m.Year
			}
		} else {
			src = "mikan" // 回查失败就退回 mikan 名，不能把订阅搞成空名字
		}
	}
	sub.NameSource = src
	if sub.MikanName == "" {
		sub.MikanName = sub.Title
	}
	// 没传字幕组时从 RSS 条目推断
	if sub.Subgroup == "" {
		if body, err := fetchFeed(sub.URL, s.app.cfg.UA); err == nil {
			if _, items, err2 := ParseFeed(body); err2 == nil && len(items) > 0 {
				if m := reGroupTag.FindStringSubmatch(items[0].Title); len(m) > 1 {
					sub.Subgroup = strings.TrimSpace(m[1])
				}
			}
		}
	}
	sub.applyDefaults()

	// 解析「同一个番名目录」：扫追更库根目录，找到同一部番的已有目录就复用它
	// （哪怕是手动建的、带年份的、写法不一样的），这样第 1/2/3 季才会落进
	// 同一个目录、各自建 Season N 子目录。
	//
	// ⚠ 必须在 applyDefaults 之后：模板里 {{dirname}} 要渲染出东西。
	// 这里有磁盘 IO（列一级子目录），量级是几百个条目，不做递归。
	s.app.mu.Lock()
	if strings.TrimSpace(sub.DirName) == "" {
		sub.DirName = s.app.ResolveDirName(&sub)
	}
	s.app.mu.Unlock()

	testPath := RenderSavePath(&sub, 1)
	if err := ValidateSavePath(testPath, s.app.cfg.AllowWritePrefixes); err != nil {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "路径被安全策略拒绝: " + err.Error()})
		return
	}

	// 先把 <番名>/Season N 建出来（本机挂载点侧）。
	//
	// 为什么主动建：qB 收到 savepath 会自己建，但它建完到我们在 cifs 挂载点
	// 看见之间有延迟，紧接着的 renameFile 可能打到还不存在的目录上。
	// 另一个更实际的作用：先订第 2 季、后订第 1 季时，第 1 季落地时目录已存在，
	// 不会出现「同名目录被当成新建」的歧义。
	if err := s.app.EnsureSeasonDirs(testPath); err != nil {
		s.app.log.Printf("[%s] 建目录失败（交给 qBittorrent 自建）: %v", sub.Title, err)
	}

	s.app.mu.Lock()
	s.app.cfg.Subscriptions = append(s.app.cfg.Subscriptions, sub)
	s.app.mu.Unlock()
	if err := SaveConfig(s.app.cfgPath, s.app.cfg); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "配置保存失败: " + err.Error()})
		return
	}

	msg := "已添加订阅「" + sub.Title + "」"

	// 订阅成功即后台抓封面（不阻塞这次响应；抓不到也不影响订阅本身）
	//
	// 不能再用 tmdbKeyOf 把关：封面默认走 mikan，抓 mikan 封面不需要 TMDB key。
	// 否则没配 key 的用户订阅后永远没有封面（只有手动点「重新抓取」才有）。
	if coverFetchable(s.app) {
		s.app.FetchCoverAsync(sub.ID)
	}

	// 首次订阅：立即抓一次 RSS
	if req.SkipBackfill {
		msg += "，已建基线（不下载历史，等新集）"
		if body, err := fetchFeed(sub.URL, s.app.cfg.UA); err == nil {
			if _, items, err2 := ParseFeed(body); err2 == nil && len(items) > 0 {
				keys := make([]string, 0, len(items))
				maxEp := 0
				for _, it := range items {
					keys = append(keys, it.Key())
					if ep := ExtractEpisode(it.Title, sub.EpPatterns); ep > maxEp {
						maxEp = ep
					}
				}
				s.app.state.MarkSeen(sub.ID, keys)
				s.app.state.Update(sub.ID, func(x *SubState) {
					x.Initialized = true
					x.MaxEpisode = maxEp
				})
				_ = s.app.state.Save()
			}
		}
	} else {
		// 首次订阅：先只同步推第 1 集，确认成功就返回；
		// 剩下的集数交给后台按 E02、E03… 逐条补推。
		//
		// 为什么不再一次性推完：RSS 里常有十几集，逐条往 qB 写种子会让
		// 这个 HTTP 请求卡很久，界面上表现为「点添加后一直转圈」。
		//
		// ⚠ 内部已持 a.mu（推单条时才锁），这里不要再包一层，
		// 否则 a.mu 是非可重入的 sync.Mutex，会自己把自己锁死。
		first, pending, perr := s.app.initialPushAndQueue(&sub)
		switch {
		case perr != nil:
			// 订阅本身已经保存成功了，首推失败不能回滚订阅；
			// 告诉用户原因即可，剩下的集数后台还会继续试。
			msg += "，但第 1 集推送失败（" + perr.Error() + "），后台会继续重试"
			if pending > 0 {
				msg += fmt.Sprintf("，剩余 %d 集已排队", pending)
			}
		case first.OK:
			msg += "，第 1 集已推送"
			if pending > 0 {
				msg += fmt.Sprintf("，剩余 %d 集后台按顺序推送中", pending)
			} else {
				msg += "，RSS 里没有更多待推剧集"
			}
		case first.Skipped:
			msg += "，第 1 集跳过（" + first.Reason + "）"
			if pending > 0 {
				msg += fmt.Sprintf("，剩余 %d 集后台按顺序推送中", pending)
			}
		default:
			msg += "，第 1 集未推送（" + first.Reason + "）"
		}
	}

	writeJSON(w, 200, map[string]any{"ok": true, "message": msg, "id": id})
}

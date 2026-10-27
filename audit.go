package main

// 订阅体检（GET /api/subs/audit）
//
// 为什么要有这个：订阅页只显示「最高 E4 · 累计 4」这类程序自己的记账，
// 而记账和现实可以完全脱节——最典型的一种是 match 规则把条目静默过滤掉，
// 却照样写进 seen：界面说「回填了 4 条」，qBittorrent 里一个任务都没有，
// 而且因为 seen 已经锁死，之后每小时检查都不会再试一次。用户从界面上
// 一点都看不出来这是死局，只看到「怎么不下载」。
//
// 判定原则：以「qBittorrent 里到底有没有任务」为事实基准，反过来推每条
// 订阅卡在哪一环：抓取 / 规则过滤 / 集数识别 / 路径安全阀 / seen 锁死。
// 只读，不改任何状态；要修由前端按钮触发 /api/sub/reset。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AuditIssue struct {
	Code     string `json:"code"`
	Level    string `json:"level"` // error | warn | info
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Sample   string `json:"sample,omitempty"`
	CanReset bool   `json:"can_reset"` // 可由「重置状态并重推」修复
}

type AuditSub struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Cover    string       `json:"cover"`
	TMDBID   int          `json:"tmdb_id"`
	Enabled  bool         `json:"enabled"`
	Level    string       `json:"level"` // error | warn | ok
	Issues   []AuditIssue `json:"issues"`
	RSS      int          `json:"rss"`      // RSS 里的条目数
	Matched  int          `json:"matched"`  // 命中 match 规则的条目数
	Filtered int          `json:"filtered"` // 被规则挡掉的条目数
	Pending  int          `json:"pending"`  // 现在就该推、但还没推的条目数
	Seen     int          `json:"seen"`     // 已记录为「见过」的条目数
	Pushed   int          `json:"pushed"`   // 程序记账里推送过的集数
	QBCount  int          `json:"qb_count"` // qBittorrent 里实际匹配到的任务数
	Owned    int          `json:"owned"`    // 已有集数（qB 任务数，失败时退回记账）
	TotalEp  int          `json:"total_ep"` // 总集数（0=未知）
	LastErr  string       `json:"last_error"`
	LastRun  time.Time    `json:"last_run"`
}

func auditLevelRank(l string) int {
	switch l {
	case "error":
		return 3
	case "warn":
		return 2
	case "info":
		return 1
	}
	return 0
}

func auditMaxLevel(cur, add string) string {
	if auditLevelRank(add) > auditLevelRank(cur) {
		return add
	}
	return cur
}

// handleSubsAudit 体检全部订阅。
//
// 每条订阅都会真的抓一次 RSS（这是唯一能看见「现在 RSS 里有什么」的办法），
// 因此耗时随订阅数增长；用 4 并发 + 单条 20s 上限兜住。
func (s *server) handleSubsAudit(w http.ResponseWriter, r *http.Request) {
	s.app.mu.Lock()
	subs := make([]Subscription, len(s.app.cfg.Subscriptions))
	copy(subs, s.app.cfg.Subscriptions)
	s.app.mu.Unlock()

	// qBittorrent 任务列表取一次，所有人共用（取不到就退化成「答不上来」）
	var tasks []Torrent
	qbOK := true
	if s.app.qbit != nil {
		list, err := s.app.qbit.List()
		if err != nil {
			qbOK = false
		} else {
			tasks = list
		}
	} else {
		qbOK = false
	}

	out := make([]AuditSub, len(subs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range subs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = s.auditOne(&subs[i], tasks, qbOK)
		}(i)
	}
	wg.Wait()

	problems := 0
	for i := range out {
		if out[i].Level == "error" || out[i].Level == "warn" {
			problems++
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok":            true,
		"checked_at":    time.Now(),
		"total":         len(out),
		"problem_count": problems,
		"qbit_ok":       qbOK,
		"items":         out,
	})
}

func (s *server) auditOne(sub *Subscription, tasks []Torrent, qbOK bool) AuditSub {
	res := AuditSub{
		ID:      sub.ID,
		Title:   sub.Title,
		Cover:   sub.Cover,
		TMDBID:  sub.TMDBID,
		Enabled: sub.Enabled,
		Level:   "ok",
		Issues:  []AuditIssue{},
		TotalEp: episodeTotalOf(sub),
	}
	st := s.app.state.Get(sub.ID)
	res.LastErr = st.LastError
	res.LastRun = st.LastRun
	res.Pushed = st.Pushed
	res.Seen = len(st.Seen)

	qbCount := 0
	if qbOK {
		qbCount = len(MatchTasks(sub, tasks))
	}
	res.QBCount = qbCount
	res.Owned = qbCount
	if !qbOK {
		// 连不上 qB 时至少给个记账口径，不让角标空着
		res.Owned = len(st.PushedEps)
	}

	add := func(is AuditIssue) {
		res.Issues = append(res.Issues, is)
		res.Level = auditMaxLevel(res.Level, is.Level)
	}

	if !sub.Enabled {
		add(AuditIssue{Code: "disabled", Level: "info", Title: "订阅已停用",
			Detail: "这条订阅被手动停用了，程序不会去抓它。需要恢复就编辑订阅勾上「启用」。"})
	}
	if !qbOK {
		add(AuditIssue{Code: "qb_unreachable", Level: "warn", Title: "连不上 qBittorrent",
			Detail: "无法读取 qBittorrent 任务列表，下面所有「已有集数」都是程序记账值，可能不准。请检查 qBittorrent 是否在跑、地址与密钥是否对。"})
	}

	body, err := fetchFeed(sub.URL, s.app.cfg.UA)
	if err != nil {
		add(AuditIssue{Code: "fetch_failed", Level: "error", Title: "RSS 抓不下来",
			Detail: "抓取失败：" + err.Error() + "。RSS 地址失效或站点暂时不可达，这条订阅现在是死的。"})
		return res
	}
	feedTitle, items, err := ParseFeed(body)
	if err != nil {
		add(AuditIssue{Code: "parse_failed", Level: "error", Title: "RSS 解析失败",
			Detail: "解析失败：" + err.Error()})
		return res
	}
	_ = feedTitle
	res.RSS = len(items)

	if len(items) == 0 {
		add(AuditIssue{Code: "rss_empty", Level: "warn", Title: "RSS 里一条资源都没有",
			Detail: "抓到的订阅源是空的。可能是这个字幕组还没发布，也可能 RSS 地址指向了错误的番剧。"})
		return res
	}

	var (
		matched, filtered, pending int
		noLink, epUnknown          int
		firstFilteredSample        string
		firstFilteredReason        string
		firstNoLinkSample          string
		firstEpUnknownSample       string
	)

	for _, it := range items {
		ok, reason := titleMatches(s.app.cfg, sub, it.Title)
		if !ok {
			filtered++
			if firstFilteredSample == "" {
				firstFilteredSample = it.Title
				firstFilteredReason = reason
			}
			continue
		}
		matched++
		if it.Torrent == "" && it.Magnet == "" {
			noLink++
			if firstNoLinkSample == "" {
				firstNoLinkSample = it.Title
			}
			continue
		}
		// 剧场版不做集数识别：ep 恒为 0，不算「集数识别失败」
		ep := 0
		if !sub.Movie {
			ep = ExtractEpisode(it.Title, sub.EpPatterns)
			if ep == 0 && !IsMovie(it.Title) {
				epUnknown++
				if firstEpUnknownSample == "" {
					firstEpUnknownSample = it.Title
				}
				continue
			}
		}
		if !sub.Movie && sub.TotalEpisodes > 0 && ep > sub.TotalEpisodes {
			continue
		}
		if s.app.state.HasSeen(sub.ID, it.Key()) || s.app.state.HasPushedEp(sub.ID, ep) {
			continue
		}
		pending++
	}
	res.Matched = matched
	res.Filtered = filtered
	res.Pending = pending

	// —— 判据零：剧场版订阅 ——
	//
	// 剧场版是「一次性任务」，用追更那几条判据（「一集都没下成」等）
	// 会给出误导性的结论，所以单独判。
	if sub.Movie {
		switch {
		case st.MovieDone:
			add(AuditIssue{Code: "movie_done", Level: "info", Title: "剧场版已下载完成",
				Detail: "这是一次性订阅：已推送成功，之后每小时检查都不会再抓取。" +
					"想换版本（比如从 1080p 换成 4K）请勾「重置状态并重推」，它会清掉完成标记重新下一次。"})
		case pending > 0:
			add(AuditIssue{Code: "movie_pending", Level: "info", Title: "剧场版等待推送",
				Detail: fmt.Sprintf("RSS 里有 %d 条可推送的资源（已命中规则），下一轮检查会推送其中一条，推成功即完成。", pending)})
		case filtered > 0 && matched == 0:
			add(AuditIssue{Code: "movie_all_filtered", Level: "warn", Title: "剧场版资源都被过滤规则挡掉了",
				Detail: fmt.Sprintf("RSS 里 %d 条资源全部未命中 match 规则（或命中 exclude）。当前 match=%s，exclude=%s。%s",
					filtered, quoteRuleList(s.app.cfg.EffectiveMatch(sub)), quoteRuleList(s.app.cfg.EffectiveExclude(sub)), firstFilteredReason),
				Sample: firstFilteredSample})
		case noLink > 0:
			add(AuditIssue{Code: "movie_no_link", Level: "warn", Title: "剧场版条目没有下载地址",
				Detail: fmt.Sprintf("%d 条命中了规则但没有种子/磁力，只能等下一个版本。", noLink),
				Sample: firstNoLinkSample})
		default:
			add(AuditIssue{Code: "movie_empty", Level: "warn", Title: "RSS 里暂时没有可下载的剧场版资源",
				Detail: "这条订阅还是空的：等 mikan 出源即可，下一轮检查会自动推。想立刻试可以点「立即检查」。"})
		}
		return res
	}

	// —— 判据一：一集都没下成，但 seen 已经把该推的条目锁死了 ——
	// 这是最隐蔽的一种，也是用户实际遇到的那种。
	if qbOK && qbCount == 0 && st.Pushed == 0 {
		switch {
		case pending > 0:
			add(AuditIssue{Code: "never_pushed_pending", Level: "error", Title: "有可下载的条目，却一集都没推出去",
				Detail:   fmt.Sprintf("RSS 里有 %d 条现在就能推的条目（已命中规则、未记录过）。下次检查本该推它们；如果一直不动，通常是配置的保存路径被安全策略拒绝了，或者 qBittorrent 加种失败。", pending),
				CanReset: true})
		case matched > 0 && res.Seen > 0:
			add(AuditIssue{Code: "seen_locked", Level: "error", Title: "条目全被「已见过」锁死，一集没下",
				Detail:   fmt.Sprintf("命中规则的 %d 条历史资源都已被记录为「见过」，但 qBittorrent 里没有任何对应任务。常见原因：当初的过滤规则把条目挡掉了（被挡的条目也会记进 seen，且不写日志），之后即使改了规则也不会重试。勾「重置状态并重推」即可清掉记录、重新推一遍。", matched),
				CanReset: true})
		case matched == 0 && filtered > 0:
			add(AuditIssue{Code: "all_filtered", Level: "error", Title: "所有条目都被过滤规则挡掉了",
				Detail: fmt.Sprintf("RSS 里 %d 条资源全部未命中 match 规则（或命中 exclude）。当前 match=%s，exclude=%s。%s", filtered,
					quoteRuleList(s.app.cfg.EffectiveMatch(sub)), quoteRuleList(s.app.cfg.EffectiveExclude(sub)), firstFilteredReason),
				Sample:   firstFilteredSample,
				CanReset: true})
		default:
			add(AuditIssue{Code: "never_pushed", Level: "error", Title: "从未下载过任何一集",
				Detail:   "qBittorrent 里没有这条订阅的任务，程序记账也显示推送过 0 集。",
				CanReset: true})
		}
	}

	// —— 判据二：记账说推过，qB 里却没有 ——
	if qbOK && st.Pushed > 0 && qbCount == 0 {
		add(AuditIssue{Code: "pushed_but_gone", Level: "warn", Title: "记账推送过，qBittorrent 里找不到",
			Detail: fmt.Sprintf("程序记录推送过 %d 集，但 qBittorrent 里没有匹配的任务。可能是任务被删了、文件被清了，或者下载路径与订阅的「保存路径」对不上（任务被认领失败）。", st.Pushed)})
	}

	// 「已有集数」比记账少：qB 里任务数少于程序认为推过的集数
	if qbOK && qbCount > 0 && st.Pushed > qbCount {
		add(AuditIssue{Code: "partial_missing", Level: "warn", Title: "有部分任务在 qBittorrent 里找不到了",
			Detail: fmt.Sprintf("程序记录推送过 %d 集，qBittorrent 里只匹配到 %d 个任务。少掉的那部分可能是被手动删了，或者保存路径被改过。", st.Pushed, qbCount)})
	}

	// —— 判据三：有条目被规则挡掉（已有下载的不算错，只做提示） ——
	// 「一集都没下成且全被挡」已在判据一里报过 error，这里不重复报。
	blanketFailed := qbOK && qbCount == 0 && st.Pushed == 0
	if filtered > 0 && matched == 0 && len(items) > 0 && !blanketFailed {
		add(AuditIssue{Code: "partial_filtered", Level: "warn", Title: "所有条目都未命中规则",
			Detail: fmt.Sprintf("%d 条资源全部未命中 match 规则（或命中 exclude），但历史上下载是成功的，所以只提示。当前 match=%s，exclude=%s。%s", filtered,
				quoteRuleList(s.app.cfg.EffectiveMatch(sub)), quoteRuleList(s.app.cfg.EffectiveExclude(sub)), firstFilteredReason),
			Sample: firstFilteredSample})
	} else if filtered > 0 && matched > 0 {
		lv := "info"
		if qbCount == 0 {
			lv = "warn"
		}
		add(AuditIssue{Code: "partial_filtered", Level: lv, Title: "有条目被规则挡掉",
			Detail: fmt.Sprintf("%d 条中有 %d 条未命中 match/exclude 规则。当前 match=%s，exclude=%s。%s", len(items), filtered,
				quoteRuleList(s.app.cfg.EffectiveMatch(sub)), quoteRuleList(s.app.cfg.EffectiveExclude(sub)), firstFilteredReason),
			Sample: firstFilteredSample})
	}

	if noLink > 0 {
		add(AuditIssue{Code: "no_link", Level: "warn", Title: "有条目既没有种子也没有磁力",
			Detail: fmt.Sprintf("%d 条命中了规则但没有下载地址，只能跳过。", noLink),
			Sample: firstNoLinkSample})
	}
	if epUnknown > 0 {
		add(AuditIssue{Code: "ep_unknown", Level: "warn", Title: "有标题认不出集数",
			Detail: fmt.Sprintf("%d 条认不出集数，会被跳过。可在订阅里填「自定义集数正则」修正。", epUnknown),
			Sample: firstEpUnknownSample})
	}
	if st.LastError != "" {
		add(AuditIssue{Code: "last_error", Level: "warn", Title: "上次检查报过错",
			Detail: "最近一次错误：" + st.LastError})
	}
	if pending > 0 && qbCount > 0 {
		add(AuditIssue{Code: "pending", Level: "info", Title: "有条目等着下一轮推送",
			Detail: fmt.Sprintf("有 %d 条当前可推送还没推，每小时检查会自动处理。", pending)})
	}
	if res.TotalEp > 0 && res.Owned >= res.TotalEp && qbCount > 0 {
		add(AuditIssue{Code: "complete", Level: "info", Title: "看起来已经齐了",
			Detail: fmt.Sprintf("已有 %d 集 / 共 %d 集。", res.Owned, res.TotalEp)})
	}

	return res
}

// episodeTotalOf 取「总集数」：订阅里手填的优先，其次 TMDB 缓存的，都没有就是 0（未知）。
func episodeTotalOf(sub *Subscription) int {
	if sub.TotalEpisodes > 0 {
		return sub.TotalEpisodes
	}
	if sub.EpTotal > 0 {
		return sub.EpTotal
	}
	return 0
}

// recentEpisodeOf 取记账里最高的集数，作为「已有集数」的兜底。
// 只在 qB 连不上时才会被用到。
func recentEpisodeOf(st *SubState) int {
	if st == nil {
		return 0
	}
	ep := st.MaxEpisode
	for e, ok := range st.PushedEps {
		if ok && e > ep {
			ep = e
		}
	}
	return ep
}

func quoteRuleList(list []string) string {
	if len(list) == 0 {
		return "（空）"
	}
	parts := make([]string, 0, len(list))
	for _, x := range list {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		parts = append(parts, x)
	}
	if len(parts) == 0 {
		return "（空）"
	}
	return strings.Join(parts, "、")
}

// handleSubReset 清掉一条订阅的抓取记录（seen / pushed_eps），让它从头再推一遍。
//
// 只清状态，不动订阅配置，也不立刻抓取——前端随后调 /api/check?force=1，
// 那样只会重推这条（其它订阅的 seen 完好，不受影响）。
func (s *server) handleSubReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
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
	var cp Subscription
	if cur := s.app.cfg.FindSub(req.ID); cur != nil {
		cp = *cur
	} else {
		s.app.mu.Unlock()
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在: " + req.ID})
		return
	}
	s.app.mu.Unlock()
	sub := &cp
	// 先登记 inflight 再清状态：清完状态到 initialPushAndQueue 真正占位之间
	// 有个窗口，定时检查（或手动「立即检查」）正好插进来就会走 checkOne
	// 把整条订阅一口气推完——真机上撞到过一次。
	// 这个标记最终由 initialPushAndQueue 或 worker 释放。
	s.app.markInflight(req.ID)
	s.app.state.ResetSub(req.ID)
	if err := s.app.state.Save(); err != nil {
		s.app.clearInflight(req.ID)
		writeJSON(w, 500, map[string]any{"ok": false, "error": "状态回写失败: " + err.Error()})
		return
	}
	s.app.log.Printf("[%s] 已重置抓取记录（重新从第 1 集开始补推）", sub.Title)

	// 重置后同样走「先推第 1 集、其余排队」的路径：
	// 一次全推十几集会同时往 qB 写十几条种子，正是要避免的卡顿。
	first, pending, perr := s.app.initialPushAndQueue(sub)
	msg := "已重置「" + sub.Title + "」的抓取记录"
	switch {
	case perr != nil:
		msg += "，但第 1 集推送失败（" + perr.Error() + "）"
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
	default:
		msg += "，第 1 集未推送（" + first.Reason + "）"
	}
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"id":      req.ID,
		"pending": pending,
		"message": msg,
	})
}

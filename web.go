package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// GET /           简易状态页（纯文本，不是 ani-rss 那种 Vue UI）
// GET /api/status 状态 JSON
// GET /api/subs   订阅列表
// GET /api/logs   最近日志
// GET /api/preview?id=<订阅ID>  预览该订阅当前命中的条目（不推送）
// POST /api/check 立即检查一次（?force=1 忽略首次基线）
type server struct {
	app     *App
	startAt time.Time
	running bool
	runMu   sync.Mutex
}

func NewServer(a *App) *server {
	return &server{app: a, startAt: time.Now()}
}

func (s *server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 根路径给 Web UI，/status 给纯文本状态页（保留）
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			if !s.authed(r) {
				if !s.app.auth.HasAccount() {
					http.Redirect(w, r, "/setup", http.StatusFound)
				} else {
					http.Redirect(w, r, "/login", http.StatusFound)
				}
				return
			}
			s.handleUI(w, r)
			return
		}
		if r.URL.Path == "/status" {
			s.handleIndex(w, r)
			return
		}
		http.NotFound(w, r)
	})
	// 登录相关：必须在闸门之外，否则无法完成首次注册/登录
	mux.HandleFunc("/setup", s.handleSetupPage)
	mux.HandleFunc("/login", s.handleLoginPage)
	mux.HandleFunc("/api/setup", s.handleSetupSubmit)
	mux.HandleFunc("/api/login", s.handleLoginSubmit)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/info", s.requireAuth(s.handleAuthInfo))
	mux.HandleFunc("/api/auth/apikey", s.requireAuth(s.handleRotateAPIKey))
	mux.HandleFunc("/api/auth/password", s.requireAuth(s.handleChangePassword))

	// 业务接口：统一走 requireAuth（回环免登录，远程要会话或 API Key）
	mux.HandleFunc("/api/parse", s.requireAuth(s.handleParse))
	mux.HandleFunc("/api/subscribe", s.requireAuth(s.handleSubscribe))
	mux.HandleFunc("/api/sub/get", s.requireAuth(s.handleSubGet))
	mux.HandleFunc("/api/sub/update", s.requireAuth(s.handleSubUpdate))
	mux.HandleFunc("/api/sub/delete", s.requireAuth(s.handleSubDelete))
	mux.HandleFunc("/api/sub/tasks", s.requireAuth(s.handleSubTasks))
	mux.HandleFunc("/api/home", s.requireAuth(s.handleHome))
	mux.HandleFunc("/api/downloads", s.requireAuth(s.handleDownloads))
	mux.HandleFunc("/api/settings", s.requireAuth(s.handleSettingsGet))
	mux.HandleFunc("/api/settings/save", s.requireAuth(s.handleSettingsSave))
	mux.HandleFunc("/api/settings/test", s.requireAuth(s.handleSettingsTest))
	mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
	mux.HandleFunc("/api/subs", s.requireAuth(s.handleSubs))
	mux.HandleFunc("/api/logs", s.requireAuth(s.handleLogs))
	mux.HandleFunc("/api/preview", s.requireAuth(s.handlePreview))
	mux.HandleFunc("/api/check", s.requireAuth(s.handleCheck))
	mux.HandleFunc("/api/mikan/search", s.requireAuth(s.handleMikanSearch))
	mux.HandleFunc("/api/mikan/bangumi", s.requireAuth(s.handleMikanBangumi))
	mux.HandleFunc("/api/mikan/season", s.requireAuth(s.handleMikanSeason))

	// TMDB 封面
	mux.HandleFunc("/api/cover", s.requireAuth(s.handleCover))
	mux.HandleFunc("/api/tmdb/search", s.requireAuth(s.handleTMDBSearch))
	mux.HandleFunc("/api/tmdb/match", s.requireAuth(s.handleTMDBMatch))
	mux.HandleFunc("/api/sub/cover", s.requireAuth(s.handleSubCoverSet))
	mux.HandleFunc("/api/covers/refresh", s.requireAuth(s.handleCoversRefresh))

	// 订阅本地文件列表（只读）
	mux.HandleFunc("/api/sub/files", s.requireAuth(s.handleSubFiles))

	// 订阅体检：找出「因为规则/状态锁死等原因一集都没下成」的订阅
	mux.HandleFunc("/api/subs/audit", s.requireAuth(s.handleSubsAudit))
	mux.HandleFunc("/api/sub/reset", s.requireAuth(s.handleSubReset))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.app.log.Printf("HTTP 服务启动于 %s", addr)
	return srv.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// isLoopback 判断请求是否来自本机。
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// ---- 认证：统一登录 + API Key ----
//
// 背景（2026-10-26 重做）：早先给破坏性接口配过 admin_token，但那个方案有死锁——
// 想设令牌必须先带着令牌调用 /api/settings/save，局域网浏览器永远进不来。
// 现在改为：
//   1. 首访没账号 → /setup 注册（一次性，建完即返回 410）
//   2. 之后 /login 登录，换 HttpOnly 签名 Cookie，登录即拥有全部权限
//   3. 另有 API Key（Authorization: Bearer 或 X-API-Key），供 AI/脚本免登录调用
//
// 本机回环免登录：能在机器上执行命令的人本来就拥有全部权限，
// 放行只是为了不把运维脚本和自检也一起锁死。

// publicPath 判断该路径是否无需登录即可访问。
func publicPath(p string) bool {
	switch p {
	case "/login", "/setup", "/favicon.ico":
		return true
	}
	return false
}

// authed 判断请求是否已通过认证（回环 / 会话 Cookie / API Key 任一）。
//
// ⚠ 三条路径的过期策略是**故意不同**的：
//   - 回环：不过期（能登上机器的人本就拥有全部权限）
//   - 会话 Cookie：按「设置 → 登录」里的有效期（用户 2026-10-26 定过：默认 2 小时、
//     0 单次有效、-1 永久）
//   - API Key：**永不过期**（用户 2026-10-26 明确要求「api 的登录不设置时间限制」）。
//     它只会因为主动「重置 API Key」或删除 auth.json 而失效，改有效期不碰它
//     —— SetSessionTTLMinutes 只轮换 session_secret，不碰 api_key。
func (s *server) authed(r *http.Request) bool {
	if isLoopback(r) {
		return true
	}
	if c, err := r.Cookie(authCookie); err == nil && s.app.auth.CheckSession(c.Value) {
		return true
	}
	if k := apiKeyFromRequest(r); k != "" && s.app.auth.VerifyAPIKey(k) {
		return true
	}
	return false
}

func apiKeyFromRequest(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get(apiKeyHeader)); k != "" {
		return k
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if v, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// requireAuth 是全局闸门：未登录的 API 请求返回 401，页面请求跳转到 /login 或 /setup。
func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.URL.Path) || s.authed(r) {
			next(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, 401, map[string]any{"ok": false, "error": "未登录", "need_login": true})
			return
		}
		target := "/login"
		if !s.app.auth.HasAccount() {
			target = "/setup"
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

// handleSetupPage 渲染首次注册页。
func (s *server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if s.app.auth.HasAccount() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(setupHTML))
}

// handleLoginPage 渲染登录页。
func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if !s.app.auth.HasAccount() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	if s.authed(r) && !isLoopback(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	html := strings.ReplaceAll(loginHTML, "__TTL_TEXT__", ttlText(s.app.auth.SessionTTLMinutes()))
	_, _ = w.Write([]byte(html))
}

// handleSetupSubmit 创建首个账号。
//
// 只允许在还没有账号时调用；成功后即可用同一组凭据登录。
// 这里必须允许远程调用，否则又回到"必须先登录才能设置登录"的死锁。
func (s *server) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	if s.app.auth.HasAccount() {
		writeJSON(w, 410, map[string]any{"ok": false, "error": "账号已存在，请直接登录"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败"})
		return
	}
	if err := s.app.auth.Register(req.Username, req.Password); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.app.log.Printf("✓ 已创建账号 %q（来自 %s）", strings.TrimSpace(req.Username), r.RemoteAddr)
	s.setSessionCookie(w, r)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "账号已创建", "username": s.app.auth.Username()})
}

// handleLoginSubmit 校验账号密码并下发会话。
func (s *server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败"})
		return
	}
	if !s.app.auth.Verify(strings.TrimSpace(req.Username), req.Password) {
		s.app.log.Printf("⚠ 登录失败：账号 %q 密码错误（来自 %s）", req.Username, r.RemoteAddr)
		// 统一措辞，不透露账号是否存在
		writeJSON(w, 401, map[string]any{"ok": false, "error": "账号或密码不正确"})
		return
	}
	s.setSessionCookie(w, r)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "登录成功"})
}

// handleLogout 清除会话。
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: authCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已退出"})
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	ck := &http.Cookie{
		Name:     authCookie,
		Value:    s.app.auth.NewSession(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	switch ttl := s.app.auth.SessionTTLMinutes(); {
	case ttl < 0:
		// 永久：给足 Cookie 寿命，浏览器会一直留着
		ck.MaxAge = permanentCookieMaxAge
	case ttl == 0:
		// 单次有效：MaxAge 留 0 → 不写 Max-Age/Expires，就是会话 Cookie，
		// 浏览器关闭即丢弃。注意 MaxAge=0 的语义是"不指定"，删除要用 MaxAge=-1。
	default:
		ck.MaxAge = ttl * 60
	}
	http.SetCookie(w, ck)
}

// handleAuthInfo 返回当前登录态与 API Key（供设置页展示）。
func (s *server) handleAuthInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok":                  true,
		"has_account":         s.app.auth.HasAccount(),
		"username":            s.app.auth.Username(),
		"api_key":             s.app.auth.APIKey(),
		"loopback":            isLoopback(r),
		"session_ttl_minutes": s.app.auth.SessionTTLMinutes(),
	})
}

// handleRotateAPIKey 重置 API Key。
func (s *server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	k, err := s.app.auth.RotateAPIKey()
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "写入失败: " + err.Error()})
		return
	}
	s.app.log.Printf("API Key 已重置（来自 %s）", r.RemoteAddr)
	writeJSON(w, 200, map[string]any{"ok": true, "api_key": k, "message": "API Key 已重置，旧 Key 立即失效"})
}

// handleChangePassword 修改登录密码（需要旧密码）。
func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "新密码不能为空"})
		return
	}
	if err := s.app.auth.ChangePassword(req.OldPassword, req.NewPassword); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.app.log.Printf("登录密码已修改（来自 %s）", r.RemoteAddr)
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"message": "密码已修改，其它设备上的登录已失效，请重新登录",
	})
}

// handleUI 输出主页面（左侧栏五视图）
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(webUI))
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	st := s.app.state.Snapshot()
	fmt.Fprintf(w, "anime-go  运行中\n\n")
	fmt.Fprintf(w, "启动时间 : %s\n", s.startAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "检查间隔 : %d 分钟\n", s.app.cfg.IntervalMinutes)
	fmt.Fprintf(w, "试运行   : %v\n", s.app.cfg.DryRun)
	fmt.Fprintf(w, "qBittorrent: %s (分类 %s)\n\n", s.app.cfg.Qbit.URL, s.app.cfg.Qbit.Category)

	fmt.Fprintf(w, "订阅（%d 条启用）\n", s.app.enabledCount())
	for i := range s.app.cfg.Subscriptions {
		sub := &s.app.cfg.Subscriptions[i]
		mark := " "
		if sub.Enabled {
			mark = "✓"
		}
		stt := st[sub.ID]
		fmt.Fprintf(w, "  %s %-38s  最高E%-3d 累计%d", mark, truncate(sub.Title, 38), stt.MaxEpisode, stt.Pushed)
		if stt.LastError != "" {
			fmt.Fprintf(w, "  ⚠ %s", truncate(stt.LastError, 50))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\n接口: /api/status /api/subs /api/logs /api/preview?id=XX /api/check\n")
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// 最近一次检查的时间/结果由 App 记录：定时循环与手动「立即检查」都会刷新，
	// 不再只有走 /api/check 时才更新（否则 last_check 恒为零值）。
	lr, last := s.app.runStat()

	// 已有集数以 qBittorrent 里的实际任务为准（界面角标「已有/总数」用）。
	// 取不到就退回程序记账值，不能让整个状态接口因为 qB 抖动而失败。
	tasks, qbErr := s.app.qbit.List()
	qbOK := qbErr == nil

	subs := make([]map[string]any, 0, len(s.app.cfg.Subscriptions))
	for i := range s.app.cfg.Subscriptions {
		sub := &s.app.cfg.Subscriptions[i]
		st := s.app.state.Get(sub.ID)
		owned := len(st.PushedEps)
		if qbOK {
			owned = len(MatchTasks(sub, tasks))
		}
		subs = append(subs, map[string]any{
			"id":          sub.ID,
			"title":       sub.Title,
			"enabled":     sub.Enabled,
			"feed_title":  st.FeedTitle,
			"max_episode": st.MaxEpisode,
			"pushed":      st.Pushed,
			"initialized": st.Initialized,
			"last_error":  st.LastError,
			"last_title":  st.LastTitle,
			"subgroup":    sub.Subgroup,
			"season":      sub.Season,
			"last_run":    st.LastRun,
			// 封面（订阅页封面墙用）
			"cover":      sub.Cover,
			"tmdb_id":    sub.TMDBID,
			"cover_from": sub.CoverFrom,
			"cover_name": sub.CoverName,
			"cover_year": sub.CoverYear,
			// cover_v 是封面版本号：列表里的 <img> 用它做 cache-busting。
			// 不能再用 tmdb_id——同一部番各季共用同一个 tmdb_id，
			// 换了海报 URL 却不变，浏览器会一直拿 max-age 内的旧图。
			"cover_v": coverVerOf(s.app, sub),
			// 集数进度：角标显示「已有/总数」，总数未知时前端只显示已有
			"owned":     owned,
			"total_ep":  episodeTotalOf(sub),
			"seen":      len(st.Seen),
			"recent_ep": recentEpisodeOf(st),
		})
	}
	writeJSON(w, 200, map[string]any{
		"ok":               true,
		"uptime_seconds":   int(time.Since(s.startAt).Seconds()),
		"interval_minutes": s.app.cfg.IntervalMinutes,
		"dry_run":          s.app.cfg.DryRun,
		"qbit":             s.app.cfg.Qbit.URL,
		"category":         s.app.cfg.Qbit.Category,
		"qbit_ok":          qbOK,
		"last_check":       lr,
		"last_results":     last,
		"subscriptions":    subs,
	})
}

func (s *server) handleSubs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.app.cfg.Subscriptions)
}

// handlePreview 抓一次 RSS，列出当前会命中的条目（不推送、不改状态）
func (s *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	sub := s.app.cfg.FindSub(id)
	if sub == nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在: " + id})
		return
	}
	body, err := fetchFeed(sub.URL, s.app.cfg.UA)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	feedTitle, items, err := ParseFeed(body)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	type row struct {
		Title   string `json:"title"`
		Episode int    `json:"episode"`
		// EpOut 是套用剧集偏移后真正写进文件名的集号。
		// 没配偏移时与 Episode 相同；配了 -12 之后这条是 1 而不是 13。
		EpOut    int    `json:"ep_out"`
		Movie    bool   `json:"movie"`
		Matched  bool   `json:"matched"`
		Reason   string `json:"reason"`
		Rename   string `json:"rename,omitempty"`
		SavePath string `json:"save_path,omitempty"`
		Seen     bool   `json:"seen"`
		HasLink  bool   `json:"has_link"`
	}
	rows := make([]row, 0, len(items))
	for _, it := range items {
		ok, reason := titleMatches(s.app.cfg, sub, it.Title)
		ep := ExtractEpisode(it.Title, sub.EpPatterns)
		rw := row{
			Title:   it.Title,
			Episode: ep,
			EpOut:   DisplayEpisode(sub, ep),
			Movie:   IsMovie(it.Title),
			Matched: ok,
			Reason:  reason,
			Seen:    s.app.state.HasSeen(sub.ID, it.Key()),
			HasLink: it.Torrent != "" || it.Magnet != "",
		}
		if ok {
			rw.Rename = RenderRename(sub, ep)
			rw.SavePath = RenderSavePath(sub, ep)
		}
		rows = append(rows, rw)
	}
	// 按集数倒序，方便看最新
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Episode > rows[j].Episode })
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"feed_title": feedTitle,
		"count":      len(rows),
		// 把目录名一并返回：用户能看到这部番会落进哪个目录，
		// 进而发现「第 2 季没进第 1 季的目录」这类问题。
		"dir_name": DirNameOf(sub),
		"items":    rows,
	})
}

func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "lines": s.app.log.Tail(150)})
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "GET" {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	s.runMu.Lock()
	if s.running {
		s.runMu.Unlock()
		writeJSON(w, 429, map[string]any{"ok": false, "error": "已有检查在进行中"})
		return
	}
	s.running = true
	s.runMu.Unlock()

	go func() {
		defer func() {
			s.runMu.Lock()
			s.running = false
			s.runMu.Unlock()
		}()
		force := r.URL.Query().Get("force") == "1"
		if _, err := s.app.CheckAll(force); err != nil {
			s.app.log.Printf("检查出错: %v", err)
		}
	}()

	writeJSON(w, 202, map[string]any{"ok": true, "message": "检查已开始，结果见 /api/status 与 /api/logs"})
}

func mustAbs(p string) string {
	if p == "" || p[0] == '/' {
		return p
	}
	wd, _ := os.Getwd()
	return wd + "/" + p
}

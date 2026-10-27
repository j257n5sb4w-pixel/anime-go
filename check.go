package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 日志：写文件 + 同时保留内存尾部给 /api/logs 用
type Logger struct {
	mu    sync.Mutex
	file  *os.File
	lines []string
	max   int
}

func NewLogger(path string) (*Logger, error) {
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f, max: 300}, nil
}

func (l *Logger) Printf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := time.Now().Format("01-02 15:04:05") + " " + msg
	log.Println(msg)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		fmt.Fprintln(l.file, line)
	}
	l.lines = append(l.lines, line)
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
}

func (l *Logger) Tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.lines) {
		n = len(l.lines)
	}
	out := make([]string, n)
	copy(out, l.lines[len(l.lines)-n:])
	return out
}

// Pending 是一条待推送的条目
type Pending struct {
	SubID     string
	SubTitle  string
	Episode   int
	FileTitle string
	RawTitle  string
	SavePath  string
	Rename    string
	Item      FeedItem
}

// Result 是单条推送的结果
type Result struct {
	SubID    string
	SubTitle string
	Episode  int
	RawTitle string
	SavePath string
	OK       bool
	Skipped  bool
	Reason   string
}

type App struct {
	cfg     *Config
	state   *State
	log     *Logger
	qbit    *Qbit
	auth    *AuthStore
	mu      sync.Mutex // 防止两次检查并发
	cfgPath string     // 配置文件路径，Web UI 改订阅后回写用

	// 后台补推队列。
	//
	// 队列本身只传 subID，取订阅副本、抓 RSS、逐集推送都在 worker 里做；
	// 每次真正推送时才短暂持 a.mu，避免把 Web UI 的其他接口堵住
	// （a.mu 在 CheckAll 里是全程持有的，含网络请求）。
	queueOnce sync.Once
	queueCh   chan string

	// inflight 记录「正在后台逐条补推」的订阅。
	//
	// ⚠ 存在的理由：补推一条订阅要几十秒（每集间隔 1.2s）。若这期间
	// 正好撞上每小时定时检查，CheckAll→checkOne 会看到剩下那些集
	// 「还没 seen」，于是一口气全推——正好又把队列想避免的卡顿做了出来。
	// 所以 CheckAll 必须跳过正在补推的订阅。
	inflightMu sync.Mutex
	inflight   map[string]bool

	// 最近一次检查的完成时间与结果。
	//
	// 修复（2026-10-26）：原来这份数据挂在 server 上，只有走 GET /api/check
	// 的那条路径会写；常驻定时循环直接调 a.CheckAll()，压根碰不到 server，
	// 于是 /api/status 的 last_check 永远返回 time.Time 零值
	// （"0001-01-01T00:00:00Z"），界面上「上次检查」永远空白。
	// 现在挂在 App 上，由 CheckAll 自己记，两条路径都能刷新。
	runStatMu   sync.Mutex
	lastRunAt   time.Time
	lastResults []Result
}

// backfillPushDelay 是后台补推时两集之间的间隔。
//
// 需求来源：一次性把十几集种子同时写进 qBittorrent 会明显卡顿，
// 所以改成一条一条来；1.2s 足够让 qB 处理完上一条的 add + rename。
const backfillPushDelay = 1200 * time.Millisecond

func NewApp(cfg *Config, cfgPath string) *App {
	lg, _ := NewLogger(cfg.LogFile)
	if lg == nil {
		lg = &Logger{}
	}
	// 账号信息与配置分开放，避免改设置时把密码哈希一起回写覆盖。
	authPath := "auth.json"
	if dir := dirOf(cfgPath); dir != "" && dir != "." {
		authPath = filepath.Join(dir, "auth.json")
	}
	auth := NewAuthStore(authPath)
	// 登录有效期来自配置（默认 120 分钟；0 = 单次有效；-1 = 永久），启动时套用一次；
	// 之后在设置页改会直接调 SetSessionTTLMinutes 热生效。
	// 这里用 InitSessionTTL 而非 SetSessionTTLMinutes：重启不该轮换密钥，
	// 否则每次重启都把已登录的浏览器踢下线。
	auth.InitSessionTTL(cfg.SessionTTLMinutesValue())
	return &App{
		cfg:     cfg,
		cfgPath: cfgPath,
		state:   LoadState(cfg.StateFile),
		log:     lg,
		qbit:    NewQbit(cfg.Qbit, cfg.UA),
		auth:    auth,
	}
}

// pushCfg 是后台补推需要的一份配置快照。
//
// 为什么要快照：a.cfg 会被 /api/settings/save 在 a.mu 内改写
// （qbit、UA、全局过滤规则、白名单都可能变）。后台 worker 若直接读
// a.cfg，就是无锁读共享结构。这里在锁内拷出需要的几项，之后网络
// 请求和排期都用这份拷贝，锁自然就不必长时间持有。
type pushCfg struct {
	ua      string
	match   []string
	exclude []string
}

// pushCfgLocked 读取全局默认规则快照。⚠ 调用方必须已持有 a.mu。
func (a *App) pushCfgLocked() pushCfg {
	return pushCfg{
		ua:      a.cfg.UA,
		match:   append([]string(nil), a.cfg.DefaultMatch...),
		exclude: append([]string(nil), a.cfg.DefaultExclude...),
	}
}

// pushCfgForLocked 按具体订阅取规则（订阅里有私有规则就用订阅的）。
// ⚠ 调用方必须已持有 a.mu。
//
// ⚠ 必须 append 拷贝：EffectiveMatch 返回的是 sub.Match 本身（同一底层数组），
// 而 sub 指向 cfg.Subscriptions 里的元素，后续改写会让快照跟着变。
func (a *App) pushCfgForLocked(sub *Subscription) pushCfg {
	return pushCfg{
		ua:      a.cfg.UA,
		match:   append([]string(nil), a.cfg.EffectiveMatch(sub)...),
		exclude: append([]string(nil), a.cfg.EffectiveExclude(sub)...),
	}
}

// initialPushAndQueue 是「新建订阅」的推送入口。
//
// 需求（用户原话）：新订阅时分开来，如果第一集推送成功就先确认订阅成功，
// 然后再按 2、3…… 的顺序一条条推送。
//
// 所以这里：
//  1. 只同步推第 1 集（RSS 里集号最小的那条），拿到结果就返回给 HTTP；
//  2. 剩下的排进后台队列，由 worker 按集号升序一条条补推。
//
// ⚠ 必须先把 Initialized 置为 true：否则下一次定时检查会认为这是
// 「首次订阅」，又走 checkOne 的回填分支，绕过队列把历史集一次推完。
func (a *App) initialPushAndQueue(sub *Subscription) (first Result, pending int, err error) {
	a.state.Update(sub.ID, func(x *SubState) { x.Initialized = true })

	// 先占位再干活：worker 真正被调度起来有时间差，而定时检查可能正好在
	// 这一刻启动（真机上就撞到过）。不先登记 inflight 的话，CheckAll 会认为
	// 没人补推，直接走 checkOne 把这整条订阅一口气推完——正是要避免的卡顿。
	// pending==0 说明没有后台任务接管，这里自己释放；否则交给 backfillRemaining。
	a.markInflight(sub.ID)
	defer func() {
		if pending == 0 {
			a.clearInflight(sub.ID)
		}
	}()

	// 抓 RSS 放在锁外：一次 fetch 最长 25s，锁住的话 Web UI 整个卡住。
	a.mu.Lock()
	pc := a.pushCfgForLocked(sub)
	a.mu.Unlock()

	plan, perr := a.backfillPlan(sub, pc)
	if perr != nil {
		return Result{SubID: sub.ID, SubTitle: sub.Title, Reason: perr.Error()}, 0, perr
	}
	if len(plan) == 0 {
		return Result{SubID: sub.ID, SubTitle: sub.Title, Reason: "RSS 里没有需要推送的条目"}, 0, nil
	}

	first = Result{SubID: sub.ID, SubTitle: sub.Title, Episode: plan[0].ep, RawTitle: plan[0].it.Title}
	a.mu.Lock()
	if a.cfg.DryRun {
		first.Skipped = true
		first.Reason = "dry_run"
		a.state.MarkPushedEp(sub.ID, plan[0].ep)
		a.state.MarkSeen(sub.ID, []string{plan[0].key})
		a.log.Printf("[%s] [DRY] E%02d（订阅后首推，试运行不实际推送）", sub.Title, plan[0].ep)
	} else {
		first, err = a.pushOne(sub, plan[0].it, plan[0].ep)
	}
	a.mu.Unlock()
	_ = a.state.Save()

	// 无论首推成败都排队：失败的那条会在后台重新排期时再次被选中，
	// 而成功的已经 MarkSeen，不会再被推第二遍。
	if len(plan) > 1 {
		pending = len(plan) - 1
		if !a.enqueueBackfill(sub.ID) {
			// 没排进队列就没有 worker 接管 inflight，得自己放掉，
			// 否则这条订阅会被永久当成「补推中」，定时检查再也不碰它。
			pending = 0
		}
	}
	return first, pending, err
}
func (a *App) pushOne(sub *Subscription, it FeedItem, ep int) (Result, error) {
	res := Result{SubID: sub.ID, SubTitle: sub.Title, Episode: ep, RawTitle: it.Title}

	key := it.Key()
	// 剧场版 ep 恒为 0，HasPushedEp 对 ep<=0 恒返回 false，
	// 重复判断交给 HasSeen(key)（唯一标识）。
	if a.state.HasSeen(sub.ID, key) || a.state.HasPushedEp(sub.ID, ep) {
		res.Skipped = true
		res.Reason = "已推送过"
		return res, nil
	}

	fileTitle := sub.FileTitle
	if fileTitle == "" {
		fileTitle = CleanFileTitle(it.Title, sub.Subgroup, stripYear(sub.Title))
	}
	// ⚠ 渲染前必须把推导出的 fileTitle 写进副本：
	// RenderRename/RenderSavePath 只认 sub.FileTitle，直接传 sub 会让
	// 上面算出的 fileTitle 白算（默认回落到不含字幕组清洗的 stripYear(Title)）。
	renderSub := *sub
	renderSub.FileTitle = fileTitle
	// 用全局统一渲染（render.go），禁止手拼变量
	rename := RenderRename(&renderSub, ep)
	savePath := RenderSavePath(&renderSub, ep)
	res.SavePath = savePath

	if err := ValidateSavePath(savePath, a.cfg.AllowWritePrefixes); err != nil {
		res.Reason = err.Error()
		return res, err
	}
	if err := a.push(sub, it, savePath, rename); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			// qB 里已经有这个种子：算成功，但记账必须和正常推送一致，
			// 否则「后台补推完成：成功 3 集」却 pushed=0 / max_episode=0，
			// 界面上的角标和累计数全是错的。
			a.state.MarkPushedEp(sub.ID, ep)
			a.state.MarkSeen(sub.ID, []string{key})
			a.state.Update(sub.ID, func(x *SubState) {
				x.Pushed++
				x.LastTitle = rename
				if ep > x.MaxEpisode {
					x.MaxEpisode = ep
				}
			})
			_ = a.state.Save()
			res.OK = true
			res.Reason = "已存在于 qBittorrent"
			a.log.Printf("[%s] E%02d 已存在于 qBittorrent(订阅后补推): %s", sub.Title, ep, rename)
			return res, nil
		}
		res.Reason = err.Error()
		return res, err
	}
	a.state.MarkPushedEp(sub.ID, ep)
	a.state.MarkSeen(sub.ID, []string{key})
	a.state.Update(sub.ID, func(x *SubState) {
		x.Pushed++
		x.LastTitle = rename
		if ep > x.MaxEpisode {
			x.MaxEpisode = ep
		}
	})
	_ = a.state.Save()
	res.OK = true
	a.log.Printf("[%s] E%02d 已推送(订阅后补推): %s -> %s", sub.Title, ep, rename, savePath)
	return res, nil
}

// startBackfillWorker 惰性启动后台补推 worker（进程内只有一个）。
func (a *App) startBackfillWorker() {
	a.queueOnce.Do(func() {
		a.queueCh = make(chan string, 64)
		go a.backfillWorker()
	})
}

// enqueueBackfill 把「某订阅剩下的集数」排进后台队列。
//
// 队列只传 subID：worker 会重新抓一次 RSS 重新排期，
// 这样推送失败的集不用额外记录，重新排期时自然还会被选中。
//
// 返回 false 表示没排进去（队列满），调用方要负责释放 inflight。
func (a *App) enqueueBackfill(subID string) bool {
	a.startBackfillWorker()
	select {
	case a.queueCh <- subID:
		return true
	default:
		// 队列满（正常不会发生）：交给下一次定时检查兜底
		a.log.Printf("[%s] 后台补推队列已满，等下次定时检查补推", subID)
		return false
	}
}

func (a *App) backfillWorker() {
	for subID := range a.queueCh {
		a.backfillRemaining(subID)
	}
}

// markInflight / clearInflight 维护「正在后台补推」的订阅集合。
func (a *App) markInflight(subID string) {
	a.inflightMu.Lock()
	if a.inflight == nil {
		a.inflight = make(map[string]bool, 4)
	}
	a.inflight[subID] = true
	a.inflightMu.Unlock()
}

func (a *App) clearInflight(subID string) {
	a.inflightMu.Lock()
	delete(a.inflight, subID)
	a.inflightMu.Unlock()
}

// isInflight 判断某订阅是否正被后台补推。
func (a *App) isInflight(subID string) bool {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	return a.inflight[subID]
}

// backfillItem 是排好期的一条待推条目。
type backfillItem struct {
	it  FeedItem
	ep  int
	key string
}

// backfillPlan 抓一次 RSS，排出「还需要推送」的条目，按集号升序。
//
// 过滤逻辑与 checkOne 主循环保持一致（match/exclude/无链接/认不出集数/
// 集数上限/同集去重），被过滤的条目在这里就记 seen——否则它们每次排期
// 都会被重新解析一遍。
//
// 与 checkOne 的唯一差别是排序：checkOne 按 RSS 原始顺序（通常新集在前），
// 这里必须按集号从小到大，才能满足「先 1 后 2、3…」的推送要求。
//
// ⚠ pc 必须是锁内拷出的快照，函数内不要再读 a.cfg。
func (a *App) backfillPlan(sub *Subscription, pc pushCfg) ([]backfillItem, error) {
	body, err := fetchFeed(sub.URL, pc.ua)
	if err != nil {
		a.state.Update(sub.ID, func(x *SubState) { x.LastError = err.Error() })
		return nil, err
	}
	feedTitle, items, err := ParseFeed(body)
	if err != nil {
		a.state.Update(sub.ID, func(x *SubState) { x.LastError = err.Error() })
		return nil, err
	}
	a.state.Update(sub.ID, func(x *SubState) {
		x.LastError = ""
		x.FeedTitle = feedTitle
	})

	matchRules := compileRules(pc.match)
	excludeRules := compileRules(pc.exclude)

	plan := make([]backfillItem, 0, len(items))
	drop := []string{}

	for _, it := range items {
		key := it.Key()
		if a.state.HasSeen(sub.ID, key) {
			continue
		}
		if len(matchRules) > 0 && !matchesAll(matchRules, it.Title) {
			drop = append(drop, key)
			continue
		}
		if matchesAny(excludeRules, it.Title) {
			drop = append(drop, key)
			continue
		}
		if it.Torrent == "" && it.Magnet == "" {
			drop = append(drop, key)
			continue
		}
		ep := ExtractEpisode(it.Title, sub.EpPatterns)
		if ep == 0 && !IsMovie(it.Title) {
			drop = append(drop, key)
			continue
		}
		if sub.TotalEpisodes > 0 && DisplayEpisode(sub, ep) > sub.TotalEpisodes {
			drop = append(drop, key)
			continue
		}
		if a.state.HasPushedEp(sub.ID, ep) {
			drop = append(drop, key)
			continue
		}
		plan = append(plan, backfillItem{it: it, ep: ep, key: key})
	}
	if len(drop) > 0 {
		a.state.MarkSeen(sub.ID, drop)
	}
	// 集号升序；剧场版 ep=0 统一排到最后，避免插在正片前面
	sort.SliceStable(plan, func(i, j int) bool {
		ei, ej := plan[i].ep, plan[j].ep
		if ei == 0 {
			return false
		}
		if ej == 0 {
			return true
		}
		return ei < ej
	})
	return plan, nil
}

// backfillRemaining 后台逐条推完某订阅剩下的集。
//
// 每次推送前才短暂持 a.mu，推完立刻释放：CheckAll 是全程持锁的，
// 后台补齐若也长持锁，整个 Web UI 都会卡住。
func (a *App) backfillRemaining(subID string) {
	a.markInflight(subID)
	defer a.clearInflight(subID)

	a.mu.Lock()
	cur := a.cfg.FindSub(subID)
	var cp Subscription
	var pc pushCfg
	if cur != nil {
		cp = *cur
		pc = a.pushCfgForLocked(cur)
	}
	a.mu.Unlock()
	if cur == nil {
		a.log.Printf("[%s] 后台补推：订阅已不存在，跳过", subID)
		return
	}

	plan, err := a.backfillPlan(&cp, pc)
	if err != nil {
		a.log.Printf("[%s] 后台补推：抓取失败 %v", cp.Title, err)
		return
	}
	if len(plan) == 0 {
		a.log.Printf("[%s] 后台补推：没有待推集数", cp.Title)
		return
	}
	a.log.Printf("[%s] 后台补推开始：共 %d 集，从 E%02d 起逐条推送", cp.Title, len(plan), plan[0].ep)

	okCnt, failCnt, dryCnt := 0, 0, 0
	for i, item := range plan {
		if i > 0 {
			time.Sleep(backfillPushDelay)
		}
		a.mu.Lock()
		cur = a.cfg.FindSub(subID)
		if cur == nil || !cur.Enabled {
			a.mu.Unlock()
			a.log.Printf("[%s] 后台补推中断：订阅已被删除或停用（已推 %d 集）", cp.Title, okCnt)
			return
		}
		cp = *cur
		// 试运行：不真推，但要按 checkOne 的语义记账（标已推集数 + seen），
		// 否则 dry_run 下每轮都会把同一批条目重新算进来。
		if a.cfg.DryRun {
			a.state.MarkPushedEp(subID, item.ep)
			a.state.MarkSeen(subID, []string{item.key})
			a.log.Printf("[%s] [DRY] E%02d（后台补推，试运行不实际推送）", cp.Title, item.ep)
			a.mu.Unlock()
			dryCnt++
			continue
		}
		res, perr := a.pushOne(&cp, item.it, item.ep)
		a.mu.Unlock()

		if perr != nil {
			failCnt++
			a.log.Printf("[%s] E%02d 后台补推失败（下轮重试）: %v", cp.Title, item.ep, perr)
			continue
		}
		if res.OK {
			okCnt++
		}
	}
	_ = a.state.Save()
	if dryCnt > 0 {
		a.log.Printf("[%s] 后台补推完成：试运行 %d 集（未实际推送），失败 %d 集", cp.Title, dryCnt, failCnt)
		return
	}
	a.log.Printf("[%s] 后台补推完成：成功 %d 集，失败 %d 集", cp.Title, okCnt, failCnt)
}

// CheckAll 遍历所有启用的订阅，返回本轮结果
func (a *App) CheckAll(force bool) ([]Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var results []Result
	a.log.Printf("开始检查 %d 条订阅（force=%v, dry_run=%v）", a.enabledCount(), force, a.cfg.DryRun)

	for i := range a.cfg.Subscriptions {
		sub := &a.cfg.Subscriptions[i]
		if !sub.Enabled {
			continue
		}
		// 正在后台逐条补推的订阅跳过：它剩下的集数由队列负责，
		// 这里再推一遍就等于把「一次性全推」的卡顿又做回来了。
		// 先算：isInflight 自己带锁，别在持有别的锁时调。
		if a.isInflight(sub.ID) {
			a.log.Printf("[%s] 后台补推进行中，本轮定时检查跳过", sub.Title)
			continue
		}
		rs := a.checkOne(sub, force)
		results = append(results, rs...)
	}

	if err := a.state.Save(); err != nil {
		a.log.Printf("保存状态失败: %v", err)
	}
	ok, skip := 0, 0
	for _, r := range results {
		if r.OK {
			ok++
		} else if r.Skipped {
			skip++
		}
	}
	a.log.Printf("检查完成：推送 %d 条，跳过 %d 条", ok, skip)

	// 记在 App 上而不是 server 上：定时循环与手动「立即检查」共用这一份。
	a.runStatMu.Lock()
	a.lastRunAt = time.Now()
	a.lastResults = results
	a.runStatMu.Unlock()

	return results, nil
}

// runStat 取最近一次检查的时间与结果（供 /api/status 用）。
//
// 返回副本，调用方拿着它在锁外序列化不会和下一轮 CheckAll 竞态。
func (a *App) runStat() (time.Time, []Result) {
	a.runStatMu.Lock()
	defer a.runStatMu.Unlock()
	at := a.lastRunAt
	var rs []Result
	if len(a.lastResults) > 0 {
		rs = append([]Result(nil), a.lastResults...)
	}
	return at, rs
}

func (a *App) enabledCount() int {
	n := 0
	for _, s := range a.cfg.Subscriptions {
		if s.Enabled {
			n++
		}
	}
	return n
}

func (a *App) checkOne(sub *Subscription, force bool) []Result {
	// 只取快照用于判断初始状态；所有写入都通过 Update() 在锁内完成。
	st := a.state.Get(sub.ID)
	wasInitialized := st.Initialized
	a.state.Update(sub.ID, func(x *SubState) { x.LastRun = time.Now() })

	body, err := fetchFeed(sub.URL, a.cfg.UA)
	if err != nil {
		a.state.Update(sub.ID, func(x *SubState) { x.LastError = err.Error() })
		a.log.Printf("[%s] 抓取失败: %v", sub.Title, err)
		return []Result{{SubID: sub.ID, SubTitle: sub.Title, Reason: err.Error()}}
	}
	a.state.Update(sub.ID, func(x *SubState) { x.LastError = "" })

	feedTitle, items, err := ParseFeed(body)
	if err != nil {
		a.state.Update(sub.ID, func(x *SubState) { x.LastError = err.Error() })
		a.log.Printf("[%s] 解析失败: %v", sub.Title, err)
		return []Result{{SubID: sub.ID, SubTitle: sub.Title, Reason: err.Error()}}
	}
	a.state.Update(sub.ID, func(x *SubState) { x.FeedTitle = feedTitle })

	// 首次运行：默认把 RSS 里现存的全部剧集都下载一遍，
	// 然后进入增量模式（每小时只抓新增集）。
	// 例外：SkipBackfill=true 时退回"只建基线不推"的旧行为。
	if !wasInitialized && !force {
		if sub.SkipBackfill {
			keys := make([]string, 0, len(items))
			maxEp := 0
			for _, it := range items {
				keys = append(keys, it.Key())
				if ep := ExtractEpisode(it.Title, sub.EpPatterns); ep > maxEp {
					maxEp = ep
				}
			}
			a.state.Update(sub.ID, func(x *SubState) {
				x.Initialized = true
				x.MaxEpisode = maxEp
			})
			a.state.MarkSeen(sub.ID, keys)
			a.log.Printf("[%s] 首次基线：记录 %d 条已有资源，最高集数 %d，本轮不推送（skip_backfill=true）", sub.Title, len(items), maxEp)
			return nil
		}
		// 回填模式：不标 seen，直接落到下面的推送循环，把历史全下一遍
		a.state.Update(sub.ID, func(x *SubState) { x.Initialized = true })
		a.log.Printf("[%s] 首次订阅：回填 RSS 中全部 %d 条历史资源", sub.Title, len(items))
	}

	// 订阅里留空即继承「设置 → 下载」里的全局默认规则
	matchRules := compileRules(a.cfg.EffectiveMatch(sub))
	excludeRules := compileRules(a.cfg.EffectiveExclude(sub))

	var results []Result
	newSeen := []string{}

	for _, it := range items {
		key := it.Key()

		// 已处理过
		if a.state.HasSeen(sub.ID, key) {
			continue
		}

		// 过滤规则
		if len(matchRules) > 0 && !matchesAll(matchRules, it.Title) {
			newSeen = append(newSeen, key)
			continue
		}
		if matchesAny(excludeRules, it.Title) {
			newSeen = append(newSeen, key)
			continue
		}
		if it.Torrent == "" && it.Magnet == "" {
			newSeen = append(newSeen, key)
			continue
		}

		ep := ExtractEpisode(it.Title, sub.EpPatterns)
		if ep == 0 && !IsMovie(it.Title) {
			newSeen = append(newSeen, key)
			a.log.Printf("[%s] 无法识别集数，跳过: %s", sub.Title, truncate(it.Title, 70))
			continue
		}

		// 集数上限
		//
		// ⚠ 用偏移后的集号比较：第 2 季从第 13 集开始编号、EpOffset=-12 时，
		// 原始 13 换算后是本季第 1 集。若拿原始 13 去和「本季 12 集」的上限比，
		// 整季会被判定超限而全部丢弃。
		if sub.TotalEpisodes > 0 && DisplayEpisode(sub, ep) > sub.TotalEpisodes {
			newSeen = append(newSeen, key)
			continue
		}

		// 同集多版本去重：一集只推一个版本（优先命中 match 顺序靠前的）
		//
		// 用 HasPushedEp 而不是直接查 map：ep<=0（剧场版）时它恒返回 false，
		// 避免一条订阅下的所有剧场版共用一个 ep=0 槽位而互相顶掉。
		// 剧场版之间的重复由 HasSeen(key) 兜底。
		if a.state.HasPushedEp(sub.ID, ep) {
			newSeen = append(newSeen, key)
			continue
		}

		fileTitle := sub.FileTitle
		if fileTitle == "" {
			fileTitle = CleanFileTitle(it.Title, sub.Subgroup, stripYear(sub.Title))
		}

		// ⚠ 必须走 render.go 的唯一出口。
		// 历史上这里手写了 vars + render()，绕过了 SubVars/RenderSavePath：
		//   - 少了 normPath，尾斜杠/双斜杠会让推送路径与 MatchTasks 算出的路径不一致；
		//   - 变量集合容易与 SubVars 漂移（缺 ep_n 等）。
		// FileTitle 是临时推导出来的，先落到副本上再渲染，避免污染原订阅。
		renderSub := *sub
		renderSub.FileTitle = fileTitle
		rename := RenderRename(&renderSub, ep)
		savePath := RenderSavePath(&renderSub, ep)
		// 安全阀：路径不在白名单内时拒绝推送，绝不误写正式库
		if err := ValidateSavePath(savePath, a.cfg.AllowWritePrefixes); err != nil {
			newSeen = append(newSeen, key)
			a.log.Printf("[%s] E%02d 路径被安全策略拒绝: %v", sub.Title, ep, err)
			results = append(results, Result{
				SubID: sub.ID, SubTitle: sub.Title, Episode: ep,
				RawTitle: it.Title, SavePath: savePath, Reason: err.Error(),
			})
			continue
		}

		res := Result{
			SubID:    sub.ID,
			SubTitle: sub.Title,
			Episode:  ep,
			RawTitle: it.Title,
			SavePath: savePath,
		}

		if a.cfg.DryRun {
			res.Skipped = true
			res.Reason = "dry_run"
			newSeen = append(newSeen, key)
			// 试运行也记录集数，让输出与真实推送时的去重行为一致
			a.state.MarkPushedEp(sub.ID, ep)
			a.log.Printf("[%s] [DRY] E%02d -> %s | %s", sub.Title, ep, savePath, rename)
			results = append(results, res)
			continue
		}

		// 推送
		if err := a.push(sub, it, savePath, rename); err != nil {
			if errors.Is(err, ErrAlreadyExists) {
				// 已在 qBittorrent 里（通常是 ani-rss 先下载过），算成功不重试。
				// 记账要和正常推送一致，否则累计数与最高集数会漏这一条。
				res.OK = true
				res.Reason = "已存在于 qBittorrent"
				newSeen = append(newSeen, key)
				a.state.MarkPushedEp(sub.ID, ep)
				a.state.Update(sub.ID, func(x *SubState) {
					x.Pushed++
					x.LastTitle = rename
					if ep > x.MaxEpisode {
						x.MaxEpisode = ep
					}
				})
				a.log.Printf("[%s] E%02d 已存在于 qBittorrent，跳过: %s", sub.Title, ep, rename)
				results = append(results, res)
				continue
			}
			res.Reason = err.Error()
			a.log.Printf("[%s] E%02d 推送失败: %v", sub.Title, ep, err)
			results = append(results, res)
			continue // 不标记 seen，下轮重试
		}

		res.OK = true
		newSeen = append(newSeen, key)
		a.state.MarkPushedEp(sub.ID, ep)
		a.state.Update(sub.ID, func(x *SubState) {
			x.Pushed++
			x.LastTitle = rename
			if ep > x.MaxEpisode {
				x.MaxEpisode = ep
			}
		})
		a.log.Printf("[%s] E%02d 已推送: %s -> %s", sub.Title, ep, rename, savePath)
		results = append(results, res)
	}

	if len(newSeen) > 0 {
		a.state.MarkSeen(sub.ID, newSeen)
	}
	return results
}

func (a *App) push(sub *Subscription, it FeedItem, savePath, rename string) error {
	category := a.cfg.Qbit.Category
	var (
		hash string
		err  error
	)
	if it.Torrent != "" {
		data, derr := fetchBytes(it.Torrent, a.cfg.UA)
		if derr != nil {
			// 种子下不动时退回磁力
			if it.Magnet != "" {
				hash, err = a.qbit.AddMagnet(it.Magnet, savePath, category, false)
				if err != nil {
					return err
				}
				return a.applyRename(hash, rename)
			}
			return derr
		}
		name := sha1Hex(rename) + ".torrent"
		hash, err = a.qbit.AddTorrent(name, data, savePath, category, false)
		if err != nil {
			return err
		}
		return a.applyRename(hash, rename)
	}
	hash, err = a.qbit.AddMagnet(it.Magnet, savePath, category, false)
	if err != nil {
		return err
	}
	return a.applyRename(hash, rename)
}

// applyRename 在任务加入后调用 renameFile。
// qBittorrent 5.x 的 add 接口不接受 rename 字段，必须分开做；
// 拿不到 hash 或重命名失败都不影响下载本身，只记日志。
func (a *App) applyRename(hash, rename string) error {
	if hash == "" || rename == "" {
		return nil
	}
	// 字幕组为空时模板会渲染出 "[] 番名 S01E01"，去掉空括号
	rename = cleanRename(rename)
	// 任务刚加入时 qBittorrent 尚未生成文件列表，/torrents/files 会 404。
	// 轮询等它出现（最多约 12 秒），再改名。
	var lastErr error
	for i := 0; i < 8; i++ {
		err := a.qbit.RenameFiles(hash, rename)
		if err == nil {
			return nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "404") {
			break // 非"文件列表未就绪"的错误，不必重试
		}
		time.Sleep(1500 * time.Millisecond)
	}
	a.log.Printf("重命名未生效（不影响下载）: %v", lastErr)
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// titleMatches 用于 /api/preview 里显示匹配判断
func titleMatches(cfg *Config, sub *Subscription, title string) (bool, string) {
	if rl := compileRules(cfg.EffectiveMatch(sub)); len(rl) > 0 && !matchesAll(rl, title) {
		return false, "match 规则未全部命中"
	}
	if matchesAny(compileRules(cfg.EffectiveExclude(sub)), title) {
		return false, "命中 exclude 规则"
	}
	return true, "ok"
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

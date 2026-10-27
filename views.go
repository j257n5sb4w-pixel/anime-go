package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

// ============================================================
//  下载视图：只展示「属于 anime-go 订阅」的 qBittorrent 任务
//
//  过滤依据（宁可少显示，绝不混入无关任务）：
//   1. 分类必须等于配置里的 category（默认 anime-go）
//   2. 并且 save_path 落在某个订阅的保存路径下，或任务名含某订阅番名/字幕组
//
//  这样即使别人在同一个分类里塞了任务，也不会显示出来。
// ============================================================

// DlTask 是下载视图用的任务条目
type DlTask struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	State      string  `json:"state"`
	StateText  string  `json:"state_text"`
	Progress   float64 `json:"progress"`
	Size       int64   `json:"size"`
	Downloaded int64   `json:"downloaded"`
	DLSSpeed   int64   `json:"dlspeed"`
	UPSpeed    int64   `json:"upspeed"`
	ETA        int64   `json:"eta"`
	Ratio      float64 `json:"ratio"`
	SavePath   string  `json:"save_path"`
	SubTitle   string  `json:"sub_title"` // 归属哪个订阅
	AddedOn    int64   `json:"added_on"`
	Seeds      int     `json:"seeds"`
}

// handleDownloads 返回属于本程序订阅的任务
func (s *server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.app.qbit.List()
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": "连接 qBittorrent 失败: " + err.Error()})
		return
	}

	// 预先给每个订阅算出它的保存路径前缀和名称关键词
	type subKey struct {
		title    string
		pathPre  string
		fileTit  string
		subgroup string
	}
	var keys []subKey
	for i := range s.app.cfg.Subscriptions {
		sub := &s.app.cfg.Subscriptions[i]
		keys = append(keys, subKey{
			title:    sub.Title,
			pathPre:  RenderSavePath(sub, 0),
			fileTit:  SubVars(sub, 0)["filetitle"],
			subgroup: strings.TrimSpace(sub.Subgroup),
		})
	}

	cat := s.app.cfg.Qbit.Category
	out := []DlTask{}
	for _, t := range tasks {
		// 条件1：分类必须匹配（留空则不限）
		if cat != "" && t.Category != cat {
			continue
		}

		// 条件2：必须能归属到某个订阅
		owner := ""
		tp := normPath(t.SavePath)
		for _, k := range keys {
			if k.pathPre != "" && (tp == k.pathPre || strings.HasPrefix(tp+"/", k.pathPre+"/")) {
				owner = k.title
				break
			}
			// 路径对不上时，用番名/字幕组兜底（用户可能改过目录）
			if k.fileTit != "" && containsFold(t.Name, k.fileTit) {
				owner = k.title
				break
			}
			if k.subgroup != "" && containsFold(t.Name, k.subgroup) &&
				k.fileTit != "" && containsFold(t.Name, k.fileTit) {
				owner = k.title
				break
			}
		}
		if owner == "" {
			continue // 不属于任何订阅，不显示
		}

		out = append(out, DlTask{
			Hash: t.Hash, Name: t.Name, State: t.State,
			StateText:  stateText(t.State),
			Progress:   t.Progress,
			Size:       t.Size,
			Downloaded: t.Downloaded,
			DLSSpeed:   t.DLSSpeed,
			UPSpeed:    t.UPSpeed,
			ETA:        sampleETA(t.ETA),
			Ratio:      t.Ratio,
			SavePath:   t.SavePath,
			SubTitle:   owner,
			AddedOn:    t.AddedOn,
			Seeds:      t.NumSeeds,
		})
	}

	// 按添加时间倒序
	sort.SliceStable(out, func(i, j int) bool { return out[i].AddedOn > out[j].AddedOn })

	// 汇总
	var totalSize, totalDone, dlSpeed int64
	downloading, completed, paused := 0, 0, 0
	for _, t := range out {
		totalSize += t.Size
		totalDone += int64(float64(t.Size) * t.Progress)
		dlSpeed += t.DLSSpeed
		switch {
		case t.Progress >= 0.999:
			completed++
		case isPausedState(t.State):
			paused++
		default:
			downloading++
		}
	}

	writeJSON(w, 200, map[string]any{
		"ok":    true,
		"tasks": out,
		"count": len(out),
		"summary": map[string]any{
			"total_size":  totalSize,
			"total_done":  totalDone,
			"dl_speed":    dlSpeed,
			"downloading": downloading,
			"completed":   completed,
			"paused":      paused,
		},
		"category":  cat,
		"sub_count": len(s.app.cfg.Subscriptions),
	})
}

// stateText 把 qBittorrent 的状态码翻成中文
func stateText(s string) string {
	switch s {
	case "downloading", "forcedDL", "metaDL", "forcedMetaDL":
		return "下载中"
	case "uploading", "forcedUP":
		return "做种中"
	case "stalledDL":
		return "等待中"
	case "stalledUP":
		return "做种空闲"
	case "queuedDL", "queuedUP":
		return "排队中"
	case "pausedDL", "pausedUP", "stoppedDL", "stoppedUP":
		return "已暂停"
	case "checkingDL", "checkingUP", "checkingResumeData":
		return "校验中"
	case "error", "missingFiles":
		return "出错"
	case "allocating":
		return "分配空间"
	case "moving":
		return "移动文件"
	default:
		return s
	}
}

func isPausedState(s string) bool {
	return strings.Contains(s, "aused") || strings.Contains(s, "topped")
}

// sampleETA 把 qBittorrent 的 ETA 秒数处理成合理值（8640000 = 未知）
func sampleETA(v int64) int64 {
	if v >= 8640000 {
		return -1
	}
	return v
}

// handleHome 首页：按星期几返回今天的订阅
func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	weekday := int(now.Weekday()) // 0=周日
	names := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

	type HomeSub struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Subgroup   string `json:"subgroup"`
		MaxEpisode int    `json:"max_episode"`
		Pushed     int    `json:"pushed"`
		Enabled    bool   `json:"enabled"`
		WeekLabel  string `json:"week_label"` // 自动推断或手动设置
		IsToday    bool   `json:"is_today"`
		LastRun    string `json:"last_run"`
		LastError  string `json:"last_error"`
		TodayNew   int    `json:"today_new"` // 今天新增集数（本轮推送数）
	}

	all := []HomeSub{}
	today := []HomeSub{}
	for i := range s.app.cfg.Subscriptions {
		sub := &s.app.cfg.Subscriptions[i]
		st := s.app.state.Get(sub.ID)

		// 星期标签：优先用手动配置，否则按「上次有新集的时间」推断
		label := strings.TrimSpace(sub.WeekLabel)
		if label == "" && !st.LastRun.IsZero() {
			label = names[int(st.LastRun.Weekday())]
		}
		if label == "" {
			label = "未定"
		}

		item := HomeSub{
			ID: sub.ID, Title: sub.Title, Subgroup: sub.Subgroup,
			MaxEpisode: st.MaxEpisode, Pushed: st.Pushed,
			Enabled:   sub.Enabled,
			WeekLabel: label,
			IsToday:   label == names[weekday],
			LastError: st.LastError,
		}
		if !st.LastRun.IsZero() {
			// 给前端 RFC3339，让它按浏览器本地时区显示成「刚刚 / 3 分钟前」。
			// 原来固定成 "01-02 15:04" 是板子本地时区，跨时区看会错位。
			item.LastRun = st.LastRun.Format(time.RFC3339)
		}
		all = append(all, item)
		if item.IsToday && sub.Enabled {
			today = append(today, item)
		}
	}

	// 本周每天各有多少订阅，供侧栏小日历用
	weekCount := map[string]int{}
	for _, it := range all {
		if it.WeekLabel != "未定" {
			weekCount[it.WeekLabel]++
		}
	}

	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"today":      names[weekday],
		"date":       now.Format("2006-01-02"),
		"today_subs": today,
		"all_subs":   all,
		"week_count": weekCount,
		"total":      len(all),
	})
}

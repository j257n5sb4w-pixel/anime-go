package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ============================================================
//  订阅的编辑 / 删除 / 启停
//
//  ⚠️ 删除订阅的三级行为（用户 2026-10-26 要求）：
//   级别1: 只删订阅配置        （默认，最安全）
//   级别2: + 删 qBit 任务记录   （deleteFiles=false，文件保留）
//   级别3: + 删本地文件         （deleteFiles=true，**不可恢复**）
//
//  "删本地文件"必须：
//   - 用户在前端显式勾选复选框
//   - 前端二次输入确认（输入番名）
//   - 后端用 MatchTasks 精确匹配该订阅自己的任务（路径+番名+字幕组三重校验）
//   - 绝不使用 "all" 或通配，只删匹配出来的具体 hash
// ============================================================

// SubEditRequest 是编辑订阅的请求体（其余字段保持原值）
type SubEditRequest struct {
	ID         string    `json:"id"`
	Title      *string   `json:"title"`
	FileTitle  *string   `json:"file_title"`
	URL        *string   `json:"url"`
	Season     *int      `json:"season"`
	Subgroup   *string   `json:"subgroup"`
	Match      *[]string `json:"match"`
	Exclude    *[]string `json:"exclude"`
	TotalEp    *int      `json:"total_episodes"`
	SavePath   *string   `json:"save_path"`
	RenameTpl  *string   `json:"rename_template"`
	Enabled    *bool     `json:"enabled"`
	SkipBkfill *bool     `json:"skip_backfill"`
	// EpOffset 剧集号偏移（-12 之类）。用指针：0 是合法值（不偏移），
	// 不能把「没传」和「传了 0」混为一谈。
	EpOffset *int `json:"ep_offset"`
	// DirName 番名目录名。传空串 = 让程序重新扫磁盘解析。
	DirName *string `json:"dir_name"`
	// Movie 是否剧场版订阅。用指针：false 也要能传（把剧场版改回追更）。
	Movie *bool `json:"movie"`
	// NoUpdate 推成功一条就不再更新（普通番也能勾；剧场版恒为 true）。
	NoUpdate   *bool `json:"no_update"`
	ResetState bool  `json:"reset_state"`
}

func (s *server) handleSubUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req SubEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少 id"})
		return
	}

	s.app.mu.Lock()
	defer s.app.mu.Unlock()

	idx := -1
	for i := range s.app.cfg.Subscriptions {
		if s.app.cfg.Subscriptions[i].ID == req.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在: " + req.ID})
		return
	}
	sub := &s.app.cfg.Subscriptions[idx]

	// 先整份备份，任何校验失败都能完整回滚
	backup := *sub

	if req.Title != nil && strings.TrimSpace(*req.Title) != "" {
		sub.Title = strings.TrimSpace(*req.Title)
	}
	if req.FileTitle != nil {
		sub.FileTitle = strings.TrimSpace(*req.FileTitle)
	}
	if req.URL != nil && strings.TrimSpace(*req.URL) != "" {
		sub.URL = strings.TrimSpace(*req.URL)
	}
	if req.Season != nil && *req.Season > 0 {
		sub.Season = *req.Season
	}
	if req.Subgroup != nil {
		sub.Subgroup = strings.TrimSpace(*req.Subgroup)
	}
	if req.Match != nil {
		sub.Match = *req.Match
	}
	if req.Exclude != nil {
		sub.Exclude = *req.Exclude
	}
	if req.TotalEp != nil {
		sub.TotalEpisodes = *req.TotalEp
	}
	if req.SavePath != nil {
		sub.SavePath = strings.TrimSpace(*req.SavePath)
	}
	if req.RenameTpl != nil {
		sub.RenameTemplate = strings.TrimSpace(*req.RenameTpl)
	}
	if req.Enabled != nil {
		sub.Enabled = *req.Enabled
	}
	if req.SkipBkfill != nil {
		sub.SkipBackfill = *req.SkipBkfill
	}
	if req.EpOffset != nil {
		sub.EpOffset = *req.EpOffset
	}
	// 剧场版开关。改它等于换一整套落盘模板（applyDefaults 里按 Movie 展开），
	// 所以顺手清掉 DirName 让下面重新解析。
	if req.Movie != nil && *req.Movie != sub.Movie {
		sub.Movie = *req.Movie
		sub.DirName = ""
		// 剧场版/追更互换后，旧的 save_path、重命名模板几乎一定不再适用，
		// 清成空让 applyDefaults 用新一套默认值（用户想要自定义可以改完再填）。
		sub.SavePath = ""
		sub.RenameTemplate = ""
		if sub.Movie {
			sub.Season = 1
			sub.TotalEpisodes = 0
			sub.EpOffset = 0
			sub.SkipBackfill = false
		}
	}
	// 「只下一次」开关（剧场版天然包含它，普通番可以单独勾）。
	// 勾上之后再推成功一条就会停更，见 checkOne 里的 Completed 判断。
	if req.NoUpdate != nil {
		sub.NoUpdate = *req.NoUpdate
	}
	// 剧场版没有季度/集数概念，改动了也一律归零，避免界面出现「第 2 季」。
	if sub.Movie {
		sub.Season = 1
		sub.TotalEpisodes = 0
		sub.EpOffset = 0
	}
	// 目录名：显式传了就照办（空串 = 重新解析）；
	// 没传但标题改了 → 跟着重算，否则改完番名还在往老目录里写。
	titleChanged := req.Title != nil && strings.TrimSpace(*req.Title) != "" && backup.Title != sub.Title
	if req.DirName != nil {
		d := strings.TrimSpace(*req.DirName)
		if d == "" {
			sub.DirName = ""
		} else {
			sub.DirName = d
		}
	} else if titleChanged {
		sub.DirName = ""
	}

	sub.applyDefaults()
	sub.applyMovieDefaults(s.app.cfg)
	if strings.TrimSpace(sub.DirName) == "" && !sub.Movie {
		sub.DirName = s.app.ResolveDirName(sub)
	}
	testPath := RenderSavePath(sub, 1)
	if err := ValidateSavePath(testPath, s.app.cfg.AllowWritePrefixes); err != nil {
		*sub = backup
		writeJSON(w, 403, map[string]any{"ok": false, "error": "路径被安全策略拒绝: " + err.Error()})
		return
	}

	if err := SaveConfig(s.app.cfgPath, s.app.cfg); err != nil {
		*sub = backup
		writeJSON(w, 500, map[string]any{"ok": false, "error": "配置保存失败: " + err.Error()})
		return
	}

	if req.ResetState {
		s.app.state.ResetSub(sub.ID)
		_ = s.app.state.Save()
	}

	s.app.log.Printf("订阅已更新: %s (%s)", sub.Title, sub.ID)

	// 还没封面、或标题改了导致封面可能不对 → 后台补抓一张（不阻塞响应）
	//
	// 注意：这里不能再用 tmdbKeyOf 做前置条件。封面默认来源是 mikan，
	// 抓 mikan 封面根本不需要 TMDB key；只有全局设成 tmdb 封面源时才真需要 key。
	if coverFetchable(s.app) && (strings.TrimSpace(sub.Cover) == "" || backup.Title != sub.Title) {
		s.app.FetchCoverAsync(sub.ID)
	}

	writeJSON(w, 200, map[string]any{"ok": true, "message": "已保存「" + sub.Title + "」"})
}

// handleSubTasks 列出该订阅在 qBittorrent 里对应的任务（删除前预览用，只读）
func (s *server) handleSubTasks(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	sub := s.app.cfg.FindSub(id)
	if sub == nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在"})
		return
	}
	tasks, err := s.app.qbit.List()
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": "连接 qBittorrent 失败: " + err.Error()})
		return
	}
	matched := MatchTasks(sub, tasks)
	if matched == nil {
		matched = []MatchedTask{} // 保证 JSON 是 [] 不是 null
	}
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"count":      len(matched),
		"tasks":      matched,
		"summary":    DescribeTasks(matched),
		"want_path":  RenderSavePath(sub, 1),
		"file_title": sub.FileTitle,
		"subgroup":   sub.Subgroup,
	})
}

// handleSubDelete 删除订阅。
//
// delete_torrents  : 是否同时删除 qBittorrent 里的任务记录
// delete_files     : 是否同时删除本地文件（**不可恢复**，必须 delete_torrents=true）
func (s *server) handleSubDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req struct {
		ID             string `json:"id"`
		DeleteTorrents bool   `json:"delete_torrents"`
		DeleteFiles    bool   `json:"delete_files"`
		// Confirm 前端勾选「我已确认删除」后回传 true，删除本地文件时必填。
		// 只靠前端挡不住直接 POST /api/sub/delete 的脚本，所以后端也校验这一位。
		Confirm bool `json:"confirm"`
		// ConfirmTitle 是旧版"手输番名"的确认方式，保留兼容；
		// 现在要么 Confirm=true，要么番名完全一致，二者满足其一即可。
		ConfirmTitle string `json:"confirm_title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少 id"})
		return
	}

	sub := s.app.cfg.FindSub(req.ID)
	if sub == nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在: " + req.ID})
		return
	}
	title := sub.Title

	// 删文件必须删任务，且必须确认
	if req.DeleteFiles {
		if !req.DeleteTorrents {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "删本地文件必须同时删除任务记录"})
			return
		}
		// 二次确认：勾选框（confirm=true）或旧版手输番名，二者满足其一。
		// 不可恢复的操作不接受"什么都没有"的授权。
		if !req.Confirm && strings.TrimSpace(req.ConfirmTitle) != title {
			writeJSON(w, 400, map[string]any{
				"ok":    false,
				"error": "删除本地文件需要先勾选确认",
			})
			return
		}
	}

	// 处理 qBittorrent 任务（必须在删除订阅配置之前做，
	// 因为匹配要用订阅的 save_path/filetitle/subgroup）
	var matched []MatchedTask
	if req.DeleteTorrents {
		tasks, err := s.app.qbit.List()
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": "连接 qBittorrent 失败，" + err.Error() + "（订阅未删除）"})
			return
		}
		matched = MatchTasks(sub, tasks)
		wantPath := RenderSavePath(sub, 1)
		s.app.log.Printf("[删除] 订阅=%s 渲染路径=%s qBit总任务=%d 匹配=%d",
			sub.Title, wantPath, len(tasks), len(matched))
		// 匹配为 0 时把最接近的任务打出来，便于排查
		if len(matched) == 0 && len(tasks) > 0 {
			for _, t := range tasks {
				if strings.Contains(t.Name, sub.Subgroup) || strings.Contains(t.SavePath, stripYear(sub.Title)) {
					s.app.log.Printf("[删除] 未匹配但相关: name=%s save_path=%s（与渲染路径不同）",
						t.Name, t.SavePath)
				}
			}
		}
		if len(matched) > 0 {
			hashes := make([]string, 0, len(matched))
			for _, t := range matched {
				hashes = append(hashes, t.Hash)
			}
			if err := s.app.qbit.DeleteTorrents(hashes, req.DeleteFiles); err != nil {
				writeJSON(w, 502, map[string]any{"ok": false, "error": "删除任务失败: " + err.Error() + "（订阅未删除）"})
				return
			}
		}
	}

	// 文件夹一并删除：qB 只删文件内容，Season 目录会留下。
	// 配了 downloads_mount 才会真的动手，否则这里什么都不做。
	var removedDirs []string
	var dirErrs []string
	if req.DeleteFiles {
		removedDirs, dirErrs = s.app.removeSubDirs(sub, matched)
		for _, e := range dirErrs {
			s.app.log.Printf("[删除] 目录清理: %s", e)
		}
		if len(removedDirs) > 0 {
			s.app.log.Printf("[删除] 已清理 %d 个目录: %s", len(removedDirs), strings.Join(removedDirs, ", "))
		}
	}

	// 移除订阅配置
	s.app.mu.Lock()
	idx := -1
	for i := range s.app.cfg.Subscriptions {
		if s.app.cfg.Subscriptions[i].ID == req.ID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		s.app.cfg.Subscriptions = append(
			s.app.cfg.Subscriptions[:idx:idx],
			s.app.cfg.Subscriptions[idx+1:]...,
		)
	}
	if err := SaveConfig(s.app.cfgPath, s.app.cfg); err != nil {
		s.app.mu.Unlock()
		writeJSON(w, 500, map[string]any{"ok": false, "error": "配置保存失败: " + err.Error()})
		return
	}
	s.app.mu.Unlock()

	s.app.state.ResetSub(req.ID)
	_ = s.app.state.Save()

	// 组织回执
	msg := "已删除订阅「" + title + "」"
	if req.DeleteTorrents {
		if len(matched) > 0 {
			msg += fmt.Sprintf("\n• 删除了 %d 个 qBittorrent 任务", len(matched))
		} else {
			msg += "\n• 没有匹配到 qBittorrent 任务"
		}
	}
	if req.DeleteFiles && len(matched) > 0 {
		msg += "\n• 同时删除了这些任务的本地文件"
	}
	if req.DeleteFiles {
		switch {
		case len(removedDirs) > 0:
			msg += fmt.Sprintf("\n• 已删除 %d 个文件夹", len(removedDirs))
		case len(s.app.cfg.EffectiveMounts()) == 0:
			msg += "\n• 未配置下载目录挂载映射（mounts），文件夹保留"
		default:
			msg += "\n• 没有需要清理的文件夹"
		}
	}
	if !req.DeleteTorrents {
		msg += "\n• qBittorrent 任务和已下载文件都保留着"
	}

	s.app.log.Printf("订阅已删除: %s (%s) 删任务=%v 删文件=%v 匹配%d个",
		title, req.ID, req.DeleteTorrents, req.DeleteFiles, len(matched))

	writeJSON(w, 200, map[string]any{
		"ok":            true,
		"message":       msg,
		"deleted_tasks": len(matched),
		"deleted_files": req.DeleteFiles,
		"removed_dirs":  removedDirs,
		"dir_errors":    dirErrs,
	})
}

// handleSubGet 取单条订阅完整信息（供编辑弹窗回填）
func (s *server) handleSubGet(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	sub := s.app.cfg.FindSub(id)
	if sub == nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在"})
		return
	}
	st := s.app.state.Get(id)
	writeJSON(w, 200, map[string]any{
		"ok":           true,
		"subscription": sub,
		"max_episode":  st.MaxEpisode,
		"pushed":       st.Pushed,
		"initialized":  st.Initialized,
		"feed_title":   st.FeedTitle,
		"last_error":   st.LastError,
	})
}

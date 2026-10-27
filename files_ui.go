package main

import (
	"net/http"
	"sort"
	"strings"
)

// ============================================================
//  订阅「本地文件」列表
//
//  ⚠ 架构事实（务必先读，别再用本地文件系统扫了）：
//  本程序跑在 OrangePi 上，但**文件不在本机**——qBittorrent 跑在
//  192.168.50.17，save_path 形如 /downloads/anime-go/... 是**那台机器**的
//  路径。板子上 /downloads/anime-go 只是个空目录占位，直接扫本地文件系统
//  永远只能得到空列表（2026-10-26 实测确认：/downloads/anime-go 为空，
//  全盘 find 找不到「至高之力」「久留美」）。
//  删除订阅时的 delete_files 也是让 qBittorrent 自己删的
//  （qbit.DeleteTorrents(hashes, deleteFiles)），本程序从不碰这些文件。
//
//  所以「查看本地文件」必须问 qBittorrent：
//    List() → MatchTasks() → Files(hash)
//  这样拿到的才是真实落盘的文件与各自的下载进度。
//
//  安全：全程只读，不删除、不改名、不移动；用 MatchTasks 的严格匹配，
//  不会把别人的任务列进来。
// ============================================================

// 单次最多列多少个文件，防止整季合集把页面撑爆
const localFilesMaxEntries = 400

// LocalFile 是列表里的一行（一个种子内的一个文件）。
type LocalFile struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Torrent  string  `json:"torrent"` // 所属种子名，UI 用它分组/判断集数
	Hash     string  `json:"hash"`
}

// LocalTorrent 是该订阅在 qBittorrent 里的一个任务。
type LocalTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	SavePath string  `json:"save_path"`
	State    string  `json:"state"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

// handleSubFiles 列出某个订阅在 qBittorrent 里的文件。
//
//	GET /api/sub/files?id=ani-4071
func (s *server) handleSubFiles(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少 id"})
		return
	}
	sub := s.app.cfg.FindSub(id)
	if sub == nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "订阅不存在"})
		return
	}

	resp := map[string]any{
		"ok":       true,
		"id":       sub.ID,
		"title":    sub.Title,
		"source":   "qbit",
		"files":    []LocalFile{},
		"torrents": []LocalTorrent{},
	}

	tasks, err := s.app.qbit.List()
	if err != nil {
		writeJSON(w, 502, map[string]any{
			"ok":    false,
			"error": "连接 qBittorrent 失败：" + err.Error(),
		})
		return
	}

	matched := MatchTasks(sub, tasks)
	if len(matched) == 0 {
		resp["exists"] = false
		resp["error"] = "qBittorrent 里还没有这一部的任务（可能还没推送过，或已被清理）"
		writeJSON(w, 200, resp)
		return
	}
	resp["exists"] = true

	var (
		files     []LocalFile
		torrents  []LocalTorrent
		total     int64
		truncated bool
		paths     []string
		seenPath  = map[string]bool{}
	)

	for _, m := range matched {
		torrents = append(torrents, LocalTorrent{
			Hash: m.Hash, Name: m.Name, SavePath: m.SavePath,
			State: m.State, Size: m.Size, Progress: m.Progress,
		})
		if m.SavePath != "" && !seenPath[m.SavePath] {
			seenPath[m.SavePath] = true
			paths = append(paths, m.SavePath)
		}

		fs, err := s.app.qbit.Files(m.Hash)
		if err != nil {
			// 单个种子查不到文件不影响其它种子
			s.app.log.Printf("[文件列表] hash=%s 查询失败: %v", m.Hash, err)
			continue
		}
		for _, f := range fs {
			if len(files) >= localFilesMaxEntries {
				truncated = true
				break
			}
			files = append(files, LocalFile{
				Name:     f.Name,
				Size:     f.Size,
				Progress: f.Progress,
				Torrent:  m.Name,
				Hash:     m.Hash,
			})
			total += f.Size
		}
	}

	// 按种子名 + 文件名排序，让同一集的多个文件挨在一起
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Torrent != files[j].Torrent {
			return files[i].Torrent < files[j].Torrent
		}
		return files[i].Name < files[j].Name
	})

	resp["files"] = files
	resp["torrents"] = torrents
	resp["save_paths"] = paths
	resp["count"] = len(files)
	resp["total_size"] = total
	resp["truncated"] = truncated
	resp["torrent_count"] = len(torrents)
	writeJSON(w, 200, resp)
}

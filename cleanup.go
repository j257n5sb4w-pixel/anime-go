package main

// cleanup.go —— 删除订阅时把 qBittorrent 那边的文件夹也一起删掉。
//
// 背景：本程序跑在板子上，番剧文件在另一台机器的 qBittorrent 里
// （qB 的 /downloads/anime-go/...）。qB 的「删除种子 + 文件」只删文件内容，
// Season 目录之类的空文件夹会留在原地。所以这里先按配置把 qB 路径
// 映射到本机的 SMB 挂载点，再删目录。
//
// 安全边界（都很重要）：
//  1. 只有配了 downloads_mount 才动手，否则完全跳过；
//  2. 只删 allow_write_prefixes 之下、且比前缀根更深一层的路径，
//     前缀根自己（/downloads/anime-go）永远不会被删；
//  3. 删完自己这一层后，只会向上删「已经空了」的父目录，
//     遇到非空目录立刻停下（os.Remove 对非空目录本来就返回错误）。

import (
	"os"
	"path/filepath"
	"strings"
)

// qbPathToLocal 把 qBittorrent 侧的路径映射到本机挂载点下的路径。
//
// 支持多挂载点（Config.Mounts）：按 qB 侧根目录做**最长前缀匹配**，
// 这样 /media/新番追更/... 与历史遗留的 /downloads/anime-go/... 能同时映射。
// 老配置只有 downloads_mount/downloads_root 时，EffectiveMounts() 会把它
// 折成一条，行为与改造前一致。
func (a *App) qbPathToLocal(qbPath string) (string, bool) {
	p := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(qbPath)), "/")
	if p == "" {
		return "", false
	}
	var (
		bestQbit  string
		bestLocal string
	)
	for _, m := range a.cfg.EffectiveMounts() {
		root := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(m.Qbit)), "/")
		local := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(m.Local)), "/")
		if root == "" || local == "" {
			continue
		}
		if p != root && !strings.HasPrefix(p, root+"/") {
			continue
		}
		// 最长前缀优先：/media/新番追更 胜过 /media
		if len(root) > len(bestQbit) {
			bestQbit = root
			bestLocal = local
		}
	}
	if bestQbit == "" {
		return "", false
	}
	if p == bestQbit {
		return bestLocal, true
	}
	rel := strings.TrimPrefix(p, bestQbit+"/")
	if rel == "" {
		return bestLocal, true
	}
	return filepath.Join(bestLocal, filepath.FromSlash(rel)), true
}

// allowedLocalRoot 找出这条 qB 路径落在哪个 allow_write_prefixes 之下，
// 并换算成对应的本机根目录。找不到就返回 false（调用方应当不动手）。
//
// ⚠ 前缀自己是 qB 侧路径，映射到本机时要用同一条 Mounts 规则，
// 否则 /media/新番追更/ 会被当成 /mnt/media 的子路径却算错根。
func (a *App) allowedLocalRoot(qbPath string) (string, bool) {
	p := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(qbPath)), "/")
	best := ""
	for _, pre := range a.cfg.AllowWritePrefixes {
		pre = strings.TrimRight(filepath.ToSlash(strings.TrimSpace(pre)), "/")
		if pre == "" {
			continue
		}
		// 必须真的在前缀之内（相等或前缀 + "/" 开头），避免 /downloads/a 命中 /downloads/abc
		if p != pre && !strings.HasPrefix(p, pre+"/") {
			continue
		}
		if len(pre) > len(best) {
			best = pre
		}
	}
	if best == "" {
		return "", false
	}
	return a.qbPathToLocal(best)
}

// removeSubDirs 在 qB 删完任务之后，把这条订阅自己的目录删掉，
// 并向上清理变空的父目录。返回删掉的本地路径列表和失败原因。
func (a *App) removeSubDirs(sub *Subscription, matched []MatchedTask) (removed []string, errs []string) {
	if len(a.cfg.EffectiveMounts()) == 0 {
		return nil, nil // 没配挂载点：本机看不见文件，静默跳过
	}

	// 候选目录：优先用 qB 实际报出来的 save_path（最准），
	// 没匹配到任务时退回按订阅配置渲染出来的路径。
	seen := map[string]bool{}
	var cands []string
	for _, t := range matched {
		p := strings.TrimRight(strings.TrimSpace(t.SavePath), "/")
		if p != "" && !seen[p] {
			seen[p] = true
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		season := sub.Season
		if season <= 0 {
			season = 1
		}
		for _, s := range []int{season, 1} {
			p := strings.TrimRight(RenderSavePath(sub, s), "/")
			if p != "" && !seen[p] {
				seen[p] = true
				cands = append(cands, p)
			}
		}
	}

	for _, qbDir := range cands {
		local, ok := a.qbPathToLocal(qbDir)
		if !ok {
			continue
		}
		rootLocal, ok := a.allowedLocalRoot(qbDir)
		if !ok {
			errs = append(errs, "跳过（不在允许写入的前缀里）: "+qbDir)
			continue
		}
		local = filepath.Clean(local)
		rootLocal = filepath.Clean(rootLocal)
		// 前缀根自己以及比它更浅的路径，一律不删
		if local == rootLocal || !strings.HasPrefix(local, rootLocal+string(filepath.Separator)) {
			errs = append(errs, "跳过（不允许删前缀根）: "+qbDir)
			continue
		}
		if _, err := os.Stat(local); err != nil {
			if os.IsNotExist(err) {
				continue // 本来就没有，不算错
			}
			errs = append(errs, err.Error())
			continue
		}

		// ⚠ 多季共用一个番名目录时，绝不能把整个番名目录 RemoveAll 掉。
		//
		// 场景：某番/Season 1 与 某番/Season 2 同属一个番名目录，
		// 用户只删第 2 季。如果 qB 报的 SavePath 恰好是番名目录本身
		// （模板里没有 Season 这一级、或历史任务就是这么存的），
		// 直接 RemoveAll 会把第 1 季的文件一起干掉。
		//
		// 判据：候选目录的末级名字不是本订阅该有的 Season 目录，
		// 就说明它比目标浅一层 —— 这时只删本订阅对应的 Season 子目录。
		wantBase := filepath.Base(filepath.Clean(RenderSavePath(sub, seasonOf(sub))))
		targets := []string{local}
		if filepath.Base(local) != wantBase {
			sub2 := local
			if wantBase != "" && wantBase != "." && wantBase != "/" {
				sub2 = filepath.Join(local, wantBase)
			}
			if sub2 != local {
				targets = nil
				if _, err := os.Stat(sub2); err == nil {
					targets = append(targets, sub2)
				}
				// 目标 Season 子目录不存在：什么都不删，避免误伤
				if len(targets) == 0 {
					errs = append(errs, "跳过（只找到番名目录，未找到 "+wantBase+"）: "+qbDir)
					continue
				}
			}
		}

		for _, dir := range targets {
			if err := os.RemoveAll(dir); err != nil {
				errs = append(errs, "删除目录失败 "+dir+": "+err.Error())
				continue
			}
			removed = append(removed, dir)

			// 向上清理：只删已经空了的父目录，碰到非空就停。
			// 这一步让「某番/Season 2 删掉后，某番 若也空了」也能一并收走；
			// 兄弟季还在的话 os.Remove 会失败，正好停下。
			parent := filepath.Dir(dir)
			for parent != rootLocal && strings.HasPrefix(parent, rootLocal+string(filepath.Separator)) {
				if err := os.Remove(parent); err != nil {
					break // 非空（或没权限），到此为止
				}
				removed = append(removed, parent)
				parent = filepath.Dir(parent)
			}
		}
	}
	return removed, errs
}

// seasonOf 取订阅的季度号，缺失按 1。
func seasonOf(sub *Subscription) int {
	if sub == nil || sub.Season <= 0 {
		return 1
	}
	return sub.Season
}

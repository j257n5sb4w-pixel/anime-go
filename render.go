package main

import (
	"fmt"
	"strings"
)

// ============================================================
//  订阅路径/重命名渲染的**唯一出口**
//
//  历史教训（2026-10-26 反复踩坑）：
//  同一个 save_path 曾在 4 个地方各自渲染，传的变量各不相同
//  （web.go 的预览只传了部分变量，缺 season_n / ep_n），
//  导致"推送时写到 A 路径，删除时算出 B 路径，永远匹配不上"。
//
//  ✅ 今后所有需要渲染 save_path / rename 的地方，
//     必须调用 SubVars() + RenderSavePath() / RenderRename()，
//     绝不允许再手写 map[string]string。
// ============================================================

// SubVars 生成渲染模板所需的**完整**变量集合。
// 任何新增模板变量都必须加到这里，保证全程序一致。
//
// ep 传入的是**字幕组原始集号**；函数内部按 sub.EpOffset 换算成
// 「落盘/重命名用的集号」再填 {{ep}}。
//
// 为什么不干脆让调用方先算好再传：
// 因为调用方遍布 check.go / web.go / cleanup.go / match_tasks.go，
// 只要有一处忘了减偏移，就会出现「预览显示 S02E01、实际写进去是 S02E13」。
// 把偏移收口在唯一出口，就没有第二处可以出错。
func SubVars(sub *Subscription, ep int) map[string]string {
	fileTitle := strings.TrimSpace(sub.FileTitle)
	if fileTitle == "" {
		fileTitle = stripYear(sub.Title)
	}
	outEp := DisplayEpisode(sub, ep)
	epStr := "00"
	if outEp > 0 {
		epStr = fmt.Sprintf("%02d", outEp)
	}
	return map[string]string{
		"title":     sub.Title,
		"dirname":   DirNameOf(sub),
		"filetitle": fileTitle,
		"season":    fmt.Sprintf("%02d", sub.Season),
		"season_n":  fmt.Sprintf("%d", sub.Season),
		"ep":        epStr,
		"ep_n":      fmt.Sprintf("%d", outEp),
		"subgroup":  sub.Subgroup,
	}
}

// RenderSavePath 渲染订阅的保存路径。全程序唯一入口。
func RenderSavePath(sub *Subscription, ep int) string {
	return normPath(render(sub.SavePath, SubVars(sub, ep)))
}

// RenderRename 渲染订阅的重命名模板。全程序唯一入口。
func RenderRename(sub *Subscription, ep int) string {
	return cleanRename(render(sub.RenameTemplate, SubVars(sub, ep)))
}

// DisplayEpisode 把字幕组的原始集号换算成「写进文件名/用于上限判断」的集号。
//
// 场景：日漫第二季常接着第一季连续编号，第 13 集其实是本季第 1 集。
// 填 EpOffset = -12 就把它纠正成 S02E01，与 TMDB 的刮削数据对齐。
//
// 边界处理：
//   - 剧场版（原来 ep == 0）保持 0，绝不因为偏移变成一个诡异的正数；
//   - 偏移后 <= 0 时退回原始集号——宁可文件名不对，也不能出现 S02E00/E-3
//     这种谁也认不出的名字（用户真要那么干，可以在 EpPatterns 里自己处理）。
func DisplayEpisode(sub *Subscription, ep int) int {
	if sub == nil || ep <= 0 || sub.EpOffset == 0 {
		return ep
	}
	out := ep + sub.EpOffset
	if out <= 0 {
		return ep
	}
	return out
}

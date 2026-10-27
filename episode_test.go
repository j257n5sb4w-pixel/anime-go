package main

import "testing"

// TestExtractEpisodeRealWorld 锁定真实 mikan 源里出现过的命名风格。
//
// 背景：2026-10-26 用 mikanime.tv 真实抓取时发现，
// "[黒ネズミたち] ... Kurumi-chan - 03 (ABEMA 1920x1080 ...)" 被判定
// "无法识别集数"而永久跳过；"- 09v2"、"EP10" 同样漏识别。
// 这些资源一旦漏识别就再也不会被推送，属于静默丢集，因此固化成本测试。
func TestExtractEpisodeRealWorld(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		// 方括号集数
		{"[绿茶字幕组] FX战士久留美 / FX Senshi Kurumi-chan [03][WebRip][1080p][简繁日内封]", 3},
		// " - N [" 形式
		{"[樱桃花字幕组]FX战士久留美 FX Senshi Kurumi-chan - 4 [1080p][简日内嵌]", 4},
		{"[LoliHouse] FX Senshi Kurumi-chan - 05 [WebRip 1080p HEVC-10bit AAC]", 5},
		// " - N (" 形式：曾经漏识别
		{"[黒ネズミたち] FX 战士久留美 / FX Senshi Kurumi-chan - 03 (ABEMA 1920x1080 AVC AAC)", 3},
		// " - NvX" 版本号形式：曾经漏识别
		{"[Group] Anime Name - 09v2 [1080p]", 9},
		{"[Group] Anime Name - 12v3 [1080p]", 12},
		// "EP N" 形式：曾经漏识别
		{"[Group] Anime Name EP10 [1080p]", 10},
		{"[Group] Anime Name Ep.07 [1080p]", 7},
		// 中文集数
		{"FX战士久留美 第07话 [1080p]", 7},
		{"[Group] 动画名 第11集 [1080p]", 11},
		// SxxExx
		{"[Sub] Some Anime S02E08 [1080p]", 8},
		// 剧场版/无集数：必须仍然返回 0，不能强行编个集数出来
		{"[LinRip] 超时空辉夜姬！／Chou Kaguya-hime [BDRemux 1080p AVC FLAC]", 0},
		{"[Group] Anime Movie [BDRemux 1080p]", 0},
	}

	for _, c := range cases {
		if got := ExtractEpisode(c.title, nil); got != c.want {
			t.Errorf("ExtractEpisode(%q) = %d, want %d", c.title, got, c.want)
		}
	}
}

// TestExtractEpisodeNoResolutionTrap 确保分辨率数字不会被当成集数。
func TestExtractEpisodeNoResolutionTrap(t *testing.T) {
	for _, s := range []string{
		"[Group] Anime Name [1920x1080][AVC]",
		"[Group] Anime Name - 1080p [HEVC]",
	} {
		if got := ExtractEpisode(s, nil); got == 1080 || got == 1920 {
			t.Errorf("ExtractEpisode(%q) 把分辨率当成了集数: %d", s, got)
		}
	}
}

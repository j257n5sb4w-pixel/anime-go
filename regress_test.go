package main

// 既有行为的回归测试：确认这次修复没有破坏原来的正确逻辑。
// 数据取自 2026-10-26 对真实 mikan RSS 与真实 qBittorrent 的实测样本。

import (
	"testing"
)

// ---------- 匹配：真实番名 ----------

func TestMatchRealFansubs(t *testing.T) {
	cases := []struct {
		sub   *Subscription
		tasks []Torrent
		want  int
	}{
		{
			// 繁简差异：订阅简体，任务繁体
			sub: &Subscription{
				Title: "FX战士久留美 (2026)", Season: 1, Subgroup: "ANi",
				SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
			},
			tasks: []Torrent{
				{Hash: "h1", Name: "[ANi] FX 戰士久留美 - 03 [1080P]",
					SavePath: "/downloads/anime-go/FX战士久留美 (2026)/Season 1"},
				{Hash: "h2", Name: "[Lilith-Raws] 别的番 - 01 [1080P]",
					SavePath: "/downloads/anime-go/别的番 (2026)/Season 1"},
			},
			want: 1,
		},
		{
			// 中译名完全不同，靠目录名（A1 路径相等）认领
			sub: &Subscription{
				Title: "某暗部的少女共栖 (2026)", Season: 1, Subgroup: "ANi",
				SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
			},
			tasks: []Torrent{
				{Hash: "h1", Name: "[ANi] 暗部共生少女 - 01 [1080P]",
					SavePath: "/downloads/anime-go/某暗部的少女共栖 (2026)/Season 1"},
			},
			want: 1,
		},
		{
			// 12 部真实番交叉：互不认领
			sub: &Subscription{
				Title: "天才治疗师 (2025)", Season: 1, Subgroup: "ANi",
				SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
			},
			tasks: []Torrent{
				{Hash: "x1", Name: "[ANi] 乱世千金倪亚 - 01", SavePath: "/downloads/anime-go/乱世千金倪亚 (2026)/Season 1"},
				{Hash: "x2", Name: "[ANi] 学生会也有洞 - 01", SavePath: "/downloads/anime-go/学生会也有洞 (2026)/Season 1"},
			},
			want: 0,
		},
		{
			// 仓库根路径任务不被任何订阅认领
			sub: &Subscription{
				Title: "某暗部的少女共栖 (2026)", Season: 1, Subgroup: "ANi",
				SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
			},
			tasks: []Torrent{
				{Hash: "r1", Name: "[ANi] 某暗部的少女共栖 - 01", SavePath: "/downloads/anime-go"},
			},
			want: 0,
		},
		{
			// 路径归一化：尾斜杠/双斜杠/前后空格
			sub: &Subscription{
				Title: "某暗部的少女共栖 (2026)", Season: 1, Subgroup: "ANi",
				SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
			},
			tasks: []Torrent{
				{Hash: "n1", Name: "[ANi] 某暗部的少女共栖 - 01",
					SavePath: "/downloads/anime-go/某暗部的少女共栖 (2026)/Season 1/"},
				{Hash: "n2", Name: "[ANi] 某暗部的少女共栖 - 02",
					SavePath: "  /downloads/anime-go//某暗部的少女共栖 (2026)//Season 1  "},
			},
			want: 2,
		},
	}

	for i, c := range cases {
		got := MatchTasks(c.sub, c.tasks)
		if len(got) != c.want {
			t.Errorf("case %d (%s): 匹配 %d, want %d", i, c.sub.Title, len(got), c.want)
			for _, m := range got {
				t.Logf("   matched hash=%s path=%s", m.Hash, m.SavePath)
			}
		}
	}
}

// ---------- 集数解析 ----------

func TestExtractEpisodeReal(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		{"[ANi] 某暗部的少女共栖 - 03 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]", 3},
		{"[Lilith-Raws] 某番 - 12 [Baha][WEB-DL][1080p][AVC AAC][CHT][MP4]", 12},
		{"[ANi] 某番 - 第05话 [1080P]", 5},
		{"[VCB-Studio] Some Anime [01][BDRip 1080p]", 1},
		{"[ANi] 某番 [1080P]", 0},
	}
	for _, c := range cases {
		if got := ExtractEpisode(c.title, nil); got != c.want {
			t.Errorf("ExtractEpisode(%.50q) = %d, want %d", c.title, got, c.want)
		}
	}
}

// ---------- 文件名清洗 ----------

func TestCleanFileTitle(t *testing.T) {
	got := CleanFileTitle("[ANi] 某暗部的少女共栖 - 03 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]",
		"ANi", "某暗部的少女共栖")
	if got == "" {
		t.Fatal("CleanFileTitle 返回空")
	}
	t.Logf("CleanFileTitle = %q", got)
}

// ---------- 状态机 ----------

func TestStateSeenAndPushed(t *testing.T) {
	st := LoadState("/tmp/agbuild/_test_state.json")
	st.MarkSeen("s1", []string{"k1", "k2"})
	if !st.HasSeen("s1", "k1") || !st.HasSeen("s1", "k2") {
		t.Error("MarkSeen 后 HasSeen 应为 true")
	}
	if st.HasSeen("s1", "k3") {
		t.Error("未记录的 key 不应 HasSeen")
	}
	st.MarkPushedEp("s1", 3)
	if !st.HasPushedEp("s1", 3) {
		t.Error("MarkPushedEp 后 HasPushedEp 应为 true")
	}
	if st.HasPushedEp("s1", 4) {
		t.Error("未推送的集数不应 HasPushedEp")
	}
}

// ---------- 渲染变量 ----------

func TestSubVarsAndRender(t *testing.T) {
	sub := &Subscription{
		Title: "某暗部的少女共栖", Season: 2, Subgroup: "ANi", FileTitle: "暗部共生少女",
	}
	v := SubVars(sub, 5)
	if v["title"] != "某暗部的少女共栖" {
		t.Errorf("title = %q", v["title"])
	}
	if v["season"] != "02" || v["season_n"] != "2" {
		t.Errorf("season = %q, season_n = %q", v["season"], v["season_n"])
	}
	if v["ep"] != "05" || v["ep_n"] != "5" {
		t.Errorf("ep = %q, ep_n = %q", v["ep"], v["ep_n"])
	}
	if v["subgroup"] != "ANi" {
		t.Errorf("subgroup = %q", v["subgroup"])
	}

	v0 := SubVars(sub, 0)
	if v0["ep"] != "00" {
		t.Errorf("剧场版 ep = %q, want 00", v0["ep"])
	}

	bad := &Subscription{Title: "a/b", Season: 1, RenameTemplate: "{{title}}/{{filetitle}}"}
	got := RenderRename(bad, 1)
	if got == "" {
		t.Error("RenderRename 返回空")
	}
	if got[0] == '/' {
		t.Errorf("RenderRename 以斜杠开头: %q", got)
	}
}

// ---------- RSS 解析 ----------

func TestParseFeedRealShape(t *testing.T) {
	body := `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:torrent="https://mikanani.me/0.1/">
<channel>
<title>Mikan Project - 某暗部的少女共栖</title>
<item>
  <title>[ANi] 某暗部的少女共栖 - 01 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]</title>
  <enclosure type="application/x-bittorrent" url="https://mikanani.me/Download/20261001/abc123.torrent"/>
</item>
<item>
  <title>[ANi] 某暗部的少女共栖 - 02 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]</title>
  <enclosure type="application/x-bittorrent" url="https://mikanani.me/Download/20261008/def456.torrent"/>
</item>
</channel>
</rss>`
	chTitle, items, err := ParseFeed(body)
	if err != nil {
		t.Fatalf("ParseFeed 出错: %v", err)
	}
	if chTitle == "" {
		t.Error("频道标题为空")
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, it := range items {
		if it.Torrent == "" {
			t.Errorf("条目缺少 torrent 链接: %q", it.Title)
		}
		if it.Key() == "" {
			t.Errorf("Key 为空: %q", it.Title)
		}
	}
	if e := ExtractEpisode(items[0].Title, nil); e != 1 {
		t.Errorf("首条集数 = %d, want 1", e)
	}
}

// ---------- 规则编译 ----------

func TestCompileRules(t *testing.T) {
	rules := compileRules([]string{
		`{{ANi}}:1080`,
		`{{!Lilith-Raws}}:.*`,
		`4K`,
	})
	if len(rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(rules))
	}
	if matchesAll(rules[:1], "[Lilith-Raws] 某番 - 01 1080p") {
		t.Error("ANi 前缀规则不应命中 Lilith-Raws 的资源")
	}
	if !matchesAll(rules[:1], "[ANi] 某番 - 01 1080p") {
		t.Error("ANi 前缀规则应命中 ANi 的资源")
	}
	if matchesAny(rules[1:2], "[Lilith-Raws] 某番 - 01") {
		t.Error("否定规则被当成命中")
	}
}

// ---------- 路径规范化 ----------

func TestNormPath(t *testing.T) {
	cases := [][2]string{
		{"/a/b/", "/a/b"},
		{"/a//b", "/a/b"},
		{"  /a/b  ", "/a/b"},
		{"/a/./b", "/a/b"},
		{"/a/b", "/a/b"},
	}
	for _, c := range cases {
		if got := normPath(c[0]); got != c[1] {
			t.Errorf("normPath(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

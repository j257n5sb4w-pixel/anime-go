package main

// 缺陷复现/修复验证测试。每个用例都对应 2026-10-26 实测复现过的真实缺陷；
// 用修复前的代码跑会失败，用修复后的代码跑应当全部通过。

import (
	"strings"
	"testing"
)

// ============ 缺陷 1/2/3：路径穿越与前缀绕过 ============

func TestValidateSavePathTraversal(t *testing.T) {
	allow := []string{"/downloads/anime-go/"}

	// 全部是修复前被放行的危险路径，必须一律拒绝
	bad := []string{
		"/downloads/anime-go/../../../../etc (2026)/Season 1",
		"/downloads/anime-go/../../media/新番追更/我的番 (2026)/Season 1",
		`/downloads/anime-go/..\..\media\新番追更/x`,
		"/downloads/anime-go/../../etc",
		"/downloads/anime-go/%2e%2e/%2e%2e/etc",
		"/downloads/anime-go/../anime-go-evil/x",
		"/etc/passwd",
		"relative/path",
		"/downloads/anime-go/\x00evil",
	}
	for _, p := range bad {
		if err := ValidateSavePath(p, allow); err == nil {
			t.Errorf("危险路径被放行: %q", p)
		}
	}

	// 合法路径必须放行
	good := []string{
		"/downloads/anime-go/某番 (2026)/Season 1",
		"/downloads/anime-go/a/b/c",
		"/downloads/anime-go/x/../y", // 清理后仍在允许前缀内
	}
	for _, p := range good {
		if err := ValidateSavePath(p, allow); err != nil {
			t.Errorf("合法路径被拒绝: %q → %v", p, err)
		}
	}
}

func TestValidateSavePathPrefixWithoutTrailingSlash(t *testing.T) {
	// 用户手写配置时经常漏掉尾斜杠
	allow := []string{"/downloads/anime-go"}

	if err := ValidateSavePath("/downloads/anime-go-secrets/x", allow); err == nil {
		t.Error("兄弟目录 /downloads/anime-go-secrets 不应被 /downloads/anime-go 前缀放行")
	}
	if err := ValidateSavePath("/downloads/anime-go/正常/x", allow); err != nil {
		t.Errorf("正常子目录被误拒: %v", err)
	}
	if err := ValidateSavePath("/downloads/anime-go", allow); err != nil {
		t.Errorf("前缀本身应被放行: %v", err)
	}
}

func TestValidateSavePathDenyListBypass(t *testing.T) {
	// 2026-10-27 起 /media/剧场版 是剧场版的正式落盘目录，不再是 deny 名单：
	// 这条用例改成验证「deny 名单本身还在生效」——用一个仍在名单里的前缀。
	allow := []string{"/media/"}

	bad := []string{
		"/media/.anime-go-deny/x",
		"/media/../media/.anime-go-deny/x", // 穿越绕过 deny
		"/downloads/../media/.anime-go-deny/x",
	}
	for _, p := range bad {
		if err := ValidateSavePath(p, allow); err == nil {
			t.Errorf("deny 前缀未拦住: %q", p)
		}
	}

	// /media 下其他目录仍可写
	if err := ValidateSavePath("/media/其他/某番", allow); err != nil {
		t.Errorf("非 deny 前缀被误拒: %v", err)
	}
	// 2026-10-26 起「新番追更」已解禁，写进 allow 后应当放行
	if err := ValidateSavePath("/media/新番追更/某番/Season 1", allow); err != nil {
		t.Errorf("新番追更已解禁，不应被拒: %v", err)
	}
	// 剧场版目录同样已解禁
	if err := ValidateSavePath("/media/剧场版/某剧场版", allow); err != nil {
		t.Errorf("剧场版目录已解禁，不应被拒: %v", err)
	}
}

func TestCleanSavePath(t *testing.T) {
	cases := [][2]string{
		{"/a/b/", "/a/b"},
		{"/a//b", "/a/b"},
		{"/a/./b", "/a/b"},
		{"/a/x/../b", "/a/b"},
		{`/a\b`, "/a/b"},
		{"  /a/b  ", "/a/b"},
		{"/../../etc", "/etc"},
	}
	for _, c := range cases {
		if got := cleanSavePath(c[0]); got != c[1] {
			t.Errorf("cleanSavePath(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// ============ 缺陷 4：季号子串误匹配（误删风险） ============

func TestMatchTasksSeasonSiblings(t *testing.T) {
	sub := &Subscription{
		Title:    "魔法★探险家 (2026)",
		Season:   1,
		Subgroup: "ANi",
		SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
	}

	tasks := []Torrent{
		// 第一季：应当被认领
		{Hash: "s1", Name: "[ANi] 魔法★探险家 - 01 [1080P]",
			SavePath: "/downloads/anime-go/魔法★探险家 (2026)/Season 1"},
		// 第二季：**不应**被第一季订阅认领
		{Hash: "s2", Name: "[ANi] 魔法★探险家 第二季 - 01 [1080P]",
			SavePath: "/downloads/anime-go/魔法★探险家 第二季 (2026)/Season 1"},
	}

	got := MatchTasks(sub, tasks)
	if len(got) != 1 {
		t.Fatalf("匹配 %d 个任务, want 1（只能认领第一季）", len(got))
	}
	if got[0].Hash != "s1" {
		t.Errorf("认领了错误的任务: %s（应只认领第一季 s1）", got[0].Hash)
	}
}

func TestMatchTasksSeasonVariants(t *testing.T) {
	// 各种季后缀写法都要能识别为"兄弟季"，不能互相认领
	pairs := [][2]string{
		{"某番 (2026)", "某番 第二季 (2026)"},
		{"某番 (2026)", "某番 第2期 (2026)"},
		{"某番 (2026)", "某番 Season 2 (2026)"},
		{"某番 (2026)", "某番 S2 (2026)"},
		{"某番", "某番 第三季"},
	}
	for _, p := range pairs {
		if !isSeasonSibling(p[0], p[1]) {
			t.Errorf("isSeasonSibling(%q, %q) = false, want true", p[0], p[1])
		}
	}

	// 不同番剧不应判为兄弟季
	if isSeasonSibling("魔法★探险家 (2026)", "天才治疗师 (2025)") {
		t.Error("不同番剧被误判为兄弟季")
	}
	// 同一部番自身不是"兄弟季"（否则会把自己排除掉）
	if isSeasonSibling("某番 (2026)", "某番 (2026)") {
		t.Error("同一番名被判为兄弟季")
	}
}

func TestStripSeasonSuffix(t *testing.T) {
	cases := [][2]string{
		{"某番第二季", "某番"},
		{"某番 第二季", "某番"},
		{"某番season2", "某番"},
		{"某番", "某番"},
	}
	for _, c := range cases {
		if got := stripSeasonSuffix(c[0]); got != c[1] {
			t.Errorf("stripSeasonSuffix(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// 回归：收紧 A2 之后，原来靠模糊匹配能认领的场景必须仍然可用
func TestMatchTasksUserRenamedFolder(t *testing.T) {
	sub := &Subscription{
		Title: "某暗部的少女共栖 (2026)", Season: 1, Subgroup: "ANi",
		SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
	}
	// 用户手动改了文件夹名（加了后缀批注），路径不再等于模板渲染值
	tasks := []Torrent{
		{Hash: "u1", Name: "[ANi] 某暗部的少女共栖 - 01 [1080P]",
			SavePath: "/downloads/anime-go/某暗部的少女共栖 (2026) 追更中/Season 1"},
	}
	got := MatchTasks(sub, tasks)
	if len(got) != 1 {
		t.Fatalf("用户改名的目录未被认领, got %d", len(got))
	}
}

func TestMatchTasksNoCrossAnime(t *testing.T) {
	// 前缀相近的不同番剧不能互相认领
	sub := &Subscription{
		Title: "魔法 (2026)", Season: 1,
		SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
	}
	tasks := []Torrent{
		{Hash: "z1", Name: "[ANi] 魔法少女小圆 - 01",
			SavePath: "/downloads/anime-go/魔法少女小圆 (2011)/Season 1"},
	}
	if got := MatchTasks(sub, tasks); len(got) != 0 {
		t.Errorf("前缀相同的不同番被误认领: %d 个", len(got))
	}
}

// ============ 剧场版识别 ============

func TestIsMovieReal(t *testing.T) {
	cases := []struct {
		title string
		want  bool
	}{
		{"[LinRip] 超时空辉夜姬！／Chou Kaguya-hime [BDRemux 1080p AVC FLAC]", true},
		{"[ANi] 某番 剧场版 [1080P]", true},
		{"[ANi] 某番 [MOVIE][1080P]", true},
		{"[ANi] 某番 - 03 [1080P]", false},
		{"[ANi] 某番 S02E05 [1080P]", false},
	}
	for _, c := range cases {
		if got := IsMovie(c.title); got != c.want {
			t.Errorf("IsMovie(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

// ============ 渲染一致性：checkOne 与 MatchTasks 必须算出同一个路径 ============

func TestRenderConsistency(t *testing.T) {
	sub := &Subscription{
		Title:     "某番 (2026)",
		Season:    2,
		Subgroup:  "ANi",
		FileTitle: "",
		SavePath:  "/downloads/anime-go/{{title}}/Season {{season_n}}/",
	}
	// 不同集数渲染出的 save 路径必须一致（模板里不含 ep）
	p0 := RenderSavePath(sub, 0)
	p1 := RenderSavePath(sub, 1)
	p13 := RenderSavePath(sub, 13)
	if p0 != p1 || p1 != p13 {
		t.Errorf("save_path 随集数变化: %q / %q / %q", p0, p1, p13)
	}
	// 必须已归一化：无重复斜杠、无尾斜杠
	if strings.Contains(p0, "//") || strings.HasSuffix(p0, "/") {
		t.Errorf("RenderSavePath 未归一化: %q", p0)
	}

	// 含 ep 的模板则应当随集数变化
	sub2 := *sub
	sub2.SavePath = "/downloads/anime-go/{{title}}/E{{ep}}"
	if RenderSavePath(&sub2, 1) == RenderSavePath(&sub2, 13) {
		t.Error("含 {{ep}} 的模板渲染出相同路径")
	}
}

// ============ 模板安全：渲染结果不能逃出模板目录 ============

func TestRenderPathWithDotDotTitle(t *testing.T) {
	// 番名里塞 .. 也不能让渲染结果逃出白名单
	sub := &Subscription{
		Title:    "../../etc",
		Season:   1,
		SavePath: "/downloads/anime-go/{{title}}/Season {{season_n}}",
	}
	p := RenderSavePath(sub, 1)
	if err := ValidateSavePath(p, []string{"/downloads/anime-go/"}); err == nil {
		t.Errorf("含 .. 的番名渲染后逃出白名单却被放行: %q", p)
	}
}

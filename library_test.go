package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ============ 目录名推导 ============

func TestBaseAnimeDirName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"魔法★探险家 第二季 (2026)", "魔法★探险家"},
		{"胆大党 (2026)", "胆大党"},
		{"转生就是剑 第二季", "转生就是剑"},
		{"某番 Season 3", "某番"},
		{"某番 2期", "某番"},
		{"某番 S2", "某番"},
		{"第贝塔季", "第贝塔季"}, // 猜不出来就原样保留，绝不乱剥
		{"学生会也有洞！", "学生会也有洞！"},
	}
	for _, c := range cases {
		if got := baseAnimeDirName(c.in); got != c.want {
			t.Errorf("baseAnimeDirName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDirNameOfPrefersFixed(t *testing.T) {
	// 固化过的 DirName 必须优先：即使用户后来把标题改成别的，
	// 已下好的文件也不能换目录，否则删除订阅时找不到文件夹。
	sub := &Subscription{Title: "某番 第二季", DirName: "我自己起的名字"}
	if got := DirNameOf(sub); got != "我自己起的名字" {
		t.Errorf("DirNameOf = %q", got)
	}
	// 没固化过就现算
	sub2 := &Subscription{Title: "某番 第二季"}
	if got := DirNameOf(sub2); got != "某番" {
		t.Errorf("DirNameOf = %q", got)
	}
}

// ============ 同名目录挑选 ============

func TestPickSameAnimeDir(t *testing.T) {
	existing := []string{"魔法★探险家 (2026)", "胆大党", "某番 第二季", "某暗部的少女共栖 (2026)"}

	// 完全同名
	if got := pickSameAnimeDir("胆大党", existing); got != "胆大党" {
		t.Errorf("完全同名应命中，got %q", got)
	}
	// base 不带季、磁盘上带季 → 认领带季那个（说明第 2 季先订过）
	if got := pickSameAnimeDir("某番", existing); got != "某番 第二季" {
		t.Errorf("应复用带季目录，got %q", got)
	}
	// base 带季、磁盘上不带季 → 认领不带季那个（说明第 1 季先订过）
	if got := pickSameAnimeDir("胆大党 第二季", existing); got != "胆大党" {
		t.Errorf("应复用不带季目录，got %q", got)
	}
	// 带年份与不带年份视为同一部
	if got := pickSameAnimeDir("魔法★探险家", existing); got != "魔法★探险家 (2026)" {
		t.Errorf("应忽略年份差异，got %q", got)
	}
	// 绝不做子串匹配
	if got := pickSameAnimeDir("魔法", existing); got != "" {
		t.Errorf("子串不应命中，got %q", got)
	}
	if got := pickSameAnimeDir("完全不存在的番", existing); got != "" {
		t.Errorf("不该命中，got %q", got)
	}
}

func TestListSubdirsSkipsNoise(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"某番", ".fg.tmp", "#recycle"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := listSubdirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "某番" {
		t.Errorf("应只返回正常子目录，got %v", got)
	}
	// 目录不存在时返回 (nil, nil)，不报错
	if got, err := listSubdirs(filepath.Join(root, "nope")); err != nil || got != nil {
		t.Errorf("不存在应返回 nil,nil，got %v,%v", got, err)
	}
}

// ResolveDirName 必须真的看磁盘（走挂载映射后的本机路径），
// 这正是「顺序无关」需求的核心。
func TestResolveDirNameUsesDisk(t *testing.T) {
	local := t.TempDir()
	// 挂载映射把 qB 的 /media 映射到 local，所以库根 /media/新番追更
	// 在本机是 local/新番追更。
	root := filepath.Join(local, "新番追更")
	if err := os.MkdirAll(filepath.Join(root, "某番 第二季"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: &Config{
		LibraryRoot: "/media/新番追更",
		Mounts:      []PathMount{{Qbit: "/media", Local: local}},
	}}
	// 第 1 季后订：base 是「某番」，磁盘上已有「某番 第二季」→ 必须复用它
	if got := a.ResolveDirName(&Subscription{Title: "某番"}); got != "某番 第二季" {
		t.Errorf("应复用磁盘上的同名目录，got %q", got)
	}
	// 全新的番：没有同名目录 → 用剥过季后缀的名字
	if got := a.ResolveDirName(&Subscription{Title: "全新番 第二季"}); got != "全新番" {
		t.Errorf("新番应返回剥季后缀的名字，got %q", got)
	}
}

// 没配挂载映射时不能崩，退回原始路径逻辑。
func TestResolveDirNameNoMount(t *testing.T) {
	a := &App{cfg: &Config{LibraryRoot: "/media/新番追更"}}
	if got := a.ResolveDirName(&Subscription{Title: "某番 第二季"}); got != "某番" {
		t.Errorf("got %q", got)
	}
}

// ============ 剧集偏移 ============

func TestDisplayEpisode(t *testing.T) {
	cases := []struct {
		ep     int
		offset int
		want   int
	}{
		{13, -12, 1},  // 第 2 季从 13 开始 → 本季第 1 集
		{24, -12, 12}, // 第 2 季最后一集
		{5, 0, 5},     // 不偏移
		{5, 3, 8},     // 正偏移
		{1, -5, 1},    // 偏移后 <=0 时退回原值，不能出现 S02E00
		{0, -12, 0},   // 剧场版保持 0
	}
	for _, c := range cases {
		sub := &Subscription{EpOffset: c.offset}
		if got := DisplayEpisode(sub, c.ep); got != c.want {
			t.Errorf("DisplayEpisode(ep=%d, off=%d) = %d, want %d", c.ep, c.offset, got, c.want)
		}
	}
}

// 偏移只作用于落盘/展示的集号，去重仍用原始集号——
// 否则改了偏移会把已经推过的集当成新集重推一遍。
func TestEpOffsetDoesNotAffectSeen(t *testing.T) {
	sub := &Subscription{ID: "x", Title: "某番 第二季", FileTitle: "某番", Season: 2, EpOffset: -12,
		RenameTemplate: "[{{subgroup}}] {{filetitle}} S{{season}}E{{ep}}",
		SavePath:       "/media/新番追更/{{dirname}}/Season {{season_n}}"}
	sub.applyDefaults()
	if got := RenderRename(sub, 13); got != "某番 S02E01" {
		t.Errorf("Rename = %q", got)
	}
	if got := RenderSavePath(sub, 13); got != "/media/新番追更/某番/Season 2" {
		t.Errorf("SavePath = %q", got)
	}
	// 记账用的是字幕组原始集号 13，不是偏移后的 1
	st := LoadState(filepath.Join(t.TempDir(), "s.json"))
	st.MarkPushedEp(sub.ID, 13)
	if !st.HasPushedEp(sub.ID, 13) {
		t.Error("应按原始集号 13 记账")
	}
	if st.HasPushedEp(sub.ID, 1) {
		t.Error("不应把偏移后的 1 当成已推过")
	}
}

// ============ 配置迁移 ============

func TestDirNameMigrationKeepsExistingPaths(t *testing.T) {
	c := &Config{
		AllowWritePrefixes: []string{"/media/新番追更/"},
		Subscriptions: []Subscription{
			// 老默认模板（{{title}}）→ 应换成新默认，且 DirName 固化成原标题，
			// 保证渲染结果一个字符都不变。
			{ID: "a", Title: "某番 第二季", Season: 2, SavePath: legacyTitleSavePath},
			// 用户自定义模板 → 一个字都不能动
			{ID: "b", Title: "手改番", SavePath: "/media/新番追更/{{title}}/S{{season_n}}"},
		},
	}
	before := RenderSavePath(&Subscription{Title: "某番 第二季", SavePath: legacyTitleSavePath, Season: 2}, 1)
	c.applyDefaults()
	after := RenderSavePath(&c.Subscriptions[0], 1)
	if before != after {
		t.Errorf("迁移改变了落盘路径: %q -> %q", before, after)
	}
	if c.Subscriptions[0].DirName != "某番 第二季" {
		t.Errorf("DirName 应固化成原标题，got %q", c.Subscriptions[0].DirName)
	}
	if c.Subscriptions[1].SavePath != "/media/新番追更/{{title}}/S{{season_n}}" {
		t.Errorf("自定义模板被误改: %q", c.Subscriptions[1].SavePath)
	}
	if !c.DirNameMigrated {
		t.Error("应打上迁移标记")
	}
	// 幂等
	c.applyDefaults()
	if c.Subscriptions[1].SavePath != "/media/新番追更/{{title}}/S{{season_n}}" {
		t.Errorf("二次 applyDefaults 改动了自定义模板: %q", c.Subscriptions[1].SavePath)
	}
}

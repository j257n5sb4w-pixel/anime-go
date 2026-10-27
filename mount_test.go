package main

import (
	"testing"
)

// ============ 多挂载点：qB 路径 → 本机路径 ============

func TestEffectiveMountsLegacyCompat(t *testing.T) {
	// 老配置只有 downloads_mount/downloads_root，应当折成一条映射
	c := &Config{DownloadsMount: "/mnt/dl", DownloadsRoot: "/downloads"}
	c.normalizeMounts()
	ms := c.EffectiveMounts()
	if len(ms) != 1 {
		t.Fatalf("老配置应折成 1 条映射，实际 %d 条: %+v", len(ms), ms)
	}
	if ms[0].Qbit != "/downloads" || ms[0].Local != "/mnt/dl" {
		t.Errorf("映射内容不对: %+v", ms[0])
	}
}

func TestEffectiveMountsBothPresent(t *testing.T) {
	// 同时配了 Mounts 与老字段时，以 Mounts 为准
	c := &Config{
		DownloadsMount: "/mnt/dl",
		DownloadsRoot:  "/downloads",
		Mounts: []PathMount{
			{Qbit: "/media/", Local: "/mnt/media/"}, // 带尾斜杠，应被清掉
			{Qbit: "/downloads", Local: "/mnt/dl"},
		},
	}
	c.normalizeMounts()
	ms := c.EffectiveMounts()
	if len(ms) != 2 {
		t.Fatalf("应保留 2 条映射，实际 %d: %+v", len(ms), ms)
	}
	if ms[0].Qbit != "/media" || ms[0].Local != "/mnt/media" {
		t.Errorf("尾斜杠未清理: %+v", ms[0])
	}
}

func TestNormalizeMountsDropsInvalid(t *testing.T) {
	c := &Config{Mounts: []PathMount{
		{Qbit: "", Local: "/mnt/x"},       // 缺 qB 侧
		{Qbit: "/media", Local: ""},       // 缺本机侧
		{Qbit: "media", Local: "/mnt/m"},  // 非绝对路径
		{Qbit: "/media", Local: "/mnt/m"}, // 唯一有效
		{Qbit: "/media", Local: "/mnt/dup"},
	}}
	c.normalizeMounts()
	ms := c.EffectiveMounts()
	if len(ms) != 1 {
		t.Fatalf("应只剩 1 条有效映射，实际 %d: %+v", len(ms), ms)
	}
	if ms[0].Local != "/mnt/m" {
		t.Errorf("去重应保留第一条，得到 %+v", ms[0])
	}
}

func TestQbPathToLocalMultiMount(t *testing.T) {
	a := &App{cfg: &Config{Mounts: []PathMount{
		{Qbit: "/media", Local: "/mnt/media"},
		{Qbit: "/downloads", Local: "/mnt/dl"},
	}}}

	cases := []struct{ qb, want string }{
		{"/media/新番追更/某番/Season 1", "/mnt/media/新番追更/某番/Season 1"},
		{"/downloads/anime-go/老番/Season 1", "/mnt/dl/anime-go/老番/Season 1"},
		{"/media", "/mnt/media"},
		{"/downloads/", "/mnt/dl"},
	}
	for _, csel := range cases {
		got, ok := a.qbPathToLocal(csel.qb)
		if !ok {
			t.Errorf("映射失败: %q", csel.qb)
			continue
		}
		if got != csel.want {
			t.Errorf("qbPathToLocal(%q) = %q, want %q", csel.qb, got, csel.want)
		}
	}

	// 不在任何映射前缀下的路径必须返回 false
	if _, ok := a.qbPathToLocal("/etc/passwd"); ok {
		t.Error("映射表外的路径不应被映射")
	}
	// 兄弟目录不能被前缀误命中
	if _, ok := a.qbPathToLocal("/mediaa/x"); ok {
		t.Error("/mediaa 不应被 /media 前缀命中")
	}
}

func TestQbPathToLocalLongestPrefixWins(t *testing.T) {
	// 有嵌套映射时，更长的前缀要胜出
	a := &App{cfg: &Config{Mounts: []PathMount{
		{Qbit: "/media", Local: "/mnt/media"},
		{Qbit: "/media/新番追更", Local: "/mnt/xin"},
	}}}
	got, ok := a.qbPathToLocal("/media/新番追更/某番/Season 1")
	if !ok {
		t.Fatal("映射失败")
	}
	if got != "/mnt/xin/某番/Season 1" {
		t.Errorf("最长前缀未胜出: %q", got)
	}
}

// ============ 落盘位置一次性迁移 ============

func TestSavePathMigration(t *testing.T) {
	c := &Config{
		DownloadsMount:     "/mnt/media",
		AllowWritePrefixes: []string{"/downloads/anime-go/"},
		Subscriptions: []Subscription{
			{ID: "a", Title: "老番", SavePath: legacyDefaultSavePath},
			{ID: "b", Title: "手改番", SavePath: "/downloads/anime-go-special/{{title}}"},
			{ID: "c", Title: "空路径"},
		},
	}
	c.applyDefaults()

	for _, sub := range c.Subscriptions {
		switch sub.ID {
		case "a":
			if sub.SavePath != defaultSavePath {
				t.Errorf("沿用老默认的订阅未被迁移: %q", sub.SavePath)
			}
		case "b":
			if sub.SavePath != "/downloads/anime-go-special/{{title}}" {
				t.Errorf("用户自定义路径被误改: %q", sub.SavePath)
			}
		case "c":
			if sub.SavePath != defaultSavePath {
				t.Errorf("空路径应回落到新默认: %q", sub.SavePath)
			}
		}
	}
	if !c.SavePathMigrated {
		t.Error("迁移后应打上标记")
	}
	// 新目录必须进白名单，否则默认路径会被自己的安全阀拦下
	if !hasPrefixItem(c.AllowWritePrefixes, defaultAllowPrefix) {
		t.Errorf("白名单缺少新目录: %v", c.AllowWritePrefixes)
	}

	// 再跑一次不能有副作用（幂等），且用户手改过的老路径不该被二次迁移
	c.Subscriptions[1].SavePath = legacyDefaultSavePath
	c.applyDefaults()
	if c.Subscriptions[1].SavePath != legacyDefaultSavePath {
		t.Errorf("打过标记后不应再迁移用户选择的老路径: %q", c.Subscriptions[1].SavePath)
	}
}

func TestDefaultSavePathIsAllowed(t *testing.T) {
	// 默认路径必须能被默认白名单放行，否则新订阅一律推送失败
	sub := &Subscription{Title: "某番 (2026)", Season: 1, SavePath: defaultSavePath}
	sub.applyDefaults()
	p := RenderSavePath(sub, 1)
	if err := ValidateSavePath(p, DefaultAllowPrefixes()); err != nil {
		t.Fatalf("默认路径被安全阀拒绝: %s → %v", p, err)
	}
	// 但正式库里不归追更管的目录仍然要拒
	if err := ValidateSavePath("/media/剧场版/某剧场版/Season 1", DefaultAllowPrefixes()); err == nil {
		t.Error("剧场版目录不应被默认白名单放行")
	}
}

func TestDefaultAllowPrefixMatchesOnlyOwnDir(t *testing.T) {
	// 前缀带尾斜杠，兄弟目录不能被放行
	if err := ValidateSavePath("/media/新番追更备份/x", DefaultAllowPrefixes()); err == nil {
		t.Error("兄弟目录不应被 /media/新番追更/ 前缀命中")
	}
	if err := ValidateSavePath("/media/新番追更"+"x/某番", DefaultAllowPrefixes()); err == nil {
		t.Error("紧邻名字不应被命中")
	}
}

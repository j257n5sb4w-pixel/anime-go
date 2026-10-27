package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 多季共用一个番名目录时，删除某一季只能删掉它自己的 Season 子目录。
//
// 这是真实风险：qB 报的 SavePath 有可能就是番名目录本身（模板里没写 Season
// 那一级、或者历史任务就是这么存的），此时若直接 RemoveAll 会把兄弟季
// 已经下好的文件一起干掉。
func TestRemoveSubDirsKeepsSiblingSeason(t *testing.T) {
	local := t.TempDir()
	root := filepath.Join(local, "新番追更")
	shared := filepath.Join(root, "某番")
	s1 := filepath.Join(shared, "Season 1")
	s2 := filepath.Join(shared, "Season 2")
	for _, d := range []string{s1, s2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "x.mp4"), []byte("v"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := &App{cfg: &Config{
		AllowWritePrefixes: []string{"/media/新番追更/"},
		Mounts:             []PathMount{{Qbit: "/media", Local: local}},
	}}
	sub := &Subscription{
		// 第 2 季那条订阅
		Title: "某番 第二季", Season: 2,
		SavePath: "/media/新番追更/{{dirname}}/Season {{season_n}}",
	}
	sub.applyDefaults()

	// qB 报出来的路径是**番名目录本身**（比目标浅一层）
	matched := []MatchedTask{{Hash: "h", SavePath: "/media/新番追更/某番"}}
	removed, errs := a.removeSubDirs(sub, matched)
	t.Logf("removed=%v errs=%v", removed, errs)

	if _, err := os.Stat(s1); err != nil {
		t.Errorf("兄弟季 Season 1 被误删了: %v", err)
	}
	if _, err := os.Stat(s2); err == nil {
		t.Error("本季 Season 2 应当被删掉")
	}
	// 番名目录因为还装着 Season 1，必须留着
	if _, err := os.Stat(shared); err != nil {
		t.Errorf("还有兄弟季时番名目录不该被删: %v", err)
	}
}

// 反过来：删掉最后一季时，空掉的番名目录要一并收走。
func TestRemoveSubDirsCleansEmptyAnimeDir(t *testing.T) {
	local := t.TempDir()
	root := filepath.Join(local, "新番追更")
	s1 := filepath.Join(root, "某番", "Season 1")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s1, "x.mp4"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &App{cfg: &Config{
		AllowWritePrefixes: []string{"/media/新番追更/"},
		Mounts:             []PathMount{{Qbit: "/media", Local: local}},
	}}
	sub := &Subscription{Title: "某番", Season: 1,
		SavePath: "/media/新番追更/{{dirname}}/Season {{season_n}}"}
	sub.applyDefaults()

	removed, _ := a.removeSubDirs(sub, []MatchedTask{{Hash: "h", SavePath: "/media/新番追更/某番/Season 1"}})
	t.Logf("removed=%v", removed)

	if _, err := os.Stat(filepath.Join(root, "某番")); err == nil {
		t.Error("空掉的番名目录应当被一并删除")
	}
	// 但根目录绝不能动
	if _, err := os.Stat(root); err != nil {
		t.Errorf("追更库根目录不该被删: %v", err)
	}
}

// 找不到目标 Season 子目录时，宁可什么都不删，也不能去删番名目录。
func TestRemoveSubDirsRefusesWhenSeasonMissing(t *testing.T) {
	local := t.TempDir()
	root := filepath.Join(local, "新番追更")
	// 番名目录存在，但里面没有 Season 3
	if err := os.MkdirAll(filepath.Join(root, "某番", "Season 1"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := &App{cfg: &Config{
		AllowWritePrefixes: []string{"/media/新番追更/"},
		Mounts:             []PathMount{{Qbit: "/media", Local: local}},
	}}
	sub := &Subscription{Title: "某番", Season: 3,
		SavePath: "/media/新番追更/{{dirname}}/Season {{season_n}}"}
	sub.applyDefaults()

	removed, errs := a.removeSubDirs(sub, []MatchedTask{{Hash: "h", SavePath: "/media/新番追更/某番"}})
	t.Logf("removed=%v errs=%v", removed, errs)

	if len(removed) != 0 {
		t.Errorf("不该删任何东西，got %v", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "某番", "Season 1")); err != nil {
		t.Errorf("兄弟季被误删: %v", err)
	}
	if len(errs) == 0 {
		t.Error("应当留下一条「跳过」说明，否则用户以为删成功了")
	}
}

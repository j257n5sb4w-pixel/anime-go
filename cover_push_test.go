package main

// 回归测试：没有封面绝不影响下载（2026-10-27 用户报的 bug）。
//
// 现场：新人订阅一部还没封面的番，封面抓取失败（mikan 的 IPv6 连接被 reset），
// 紧接着首推也没成，界面上就是「没封面 → 也没下载」。真正的根因是封面抓取
// 曾经整段持着 a.mu 做网络请求（最长 25s），把首推/检查全堵在锁上。
//
// 这里锁两件事：
//  1. 订阅没有封面时，pushOne 照常把种子交给 qBittorrent；
//  2. FetchCoverAsync 立刻返回，抓取失败不改订阅、不阻塞调用方。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeQbit 起一个只认「加种子成功」的假 qBittorrent（不碰网络、不需要真 qB）。
func fakeQbit(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/app/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("v5.0.0"))
	})
	mux.HandleFunc("/api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success_count": 1, "failure_count": 0, "pending_count": 0,
			"added_torrent_ids": []string{"deadbeef"},
		})
	})
	mux.HandleFunc("/api/v2/torrents/renameFile", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Ok."))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeTorrent 提供一个假的 .torrent 直链（内容不重要，够非空即可）。
func fakeTorrent(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write([]byte("d8:announce16:http://tracker/4:infod6:lengthi1e4:name4:testee"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestApp(t *testing.T, qbitURL string) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{
		UA:                 "anime-go-test/0.2",
		Qbit:               QbitConfig{URL: qbitURL, Category: "anime-go"},
		AllowWritePrefixes: []string{"/media/新番追更/", "/media/剧场版/"},
	}
	cfg.applyDefaults()
	lg, err := NewLogger(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("创建日志失败: %v", err)
	}
	app := &App{
		cfg:      cfg,
		cfgPath:  filepath.Join(dir, "config.json"),
		state:    LoadState(filepath.Join(dir, "state.json")),
		log:      lg,
		qbit:     NewQbit(cfg.Qbit, cfg.UA),
		queueCh:  make(chan string, 8),
		inflight: map[string]bool{},
	}
	if err := SaveConfig(app.cfgPath, cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	return app
}

// TestPushOneWithoutCover 没有封面 → 推送照旧成功，且剧场版落到剧场版目录。
func TestPushOneWithoutCover(t *testing.T) {
	qb := fakeQbit(t)
	tor := fakeTorrent(t)
	app := newTestApp(t, qb.URL)

	sub := &Subscription{
		ID:       "t-movie",
		Title:    "测试剧场版",
		URL:      "https://mikanime.tv/RSS/Bangumi?bangumiId=1&subgroupid=2",
		Subgroup: "LoliHouse",
		Movie:    true,
		Cover:    "", // 关键：没有封面
	}
	sub.applyDefaults()
	sub.applyMovieDefaults(app.cfg)
	app.cfg.Subscriptions = []Subscription{*sub}

	it := FeedItem{Title: "[LoliHouse] 测试剧场版 [BDRip 1080p]", Torrent: tor.URL}
	res, err := app.pushOne(sub, it, 0)
	if err != nil {
		t.Fatalf("没有封面时推送不该失败: %v", err)
	}
	if !res.OK {
		t.Fatalf("推送未成功: ok=%v reason=%q", res.OK, res.Reason)
	}
	if !strings.HasPrefix(res.SavePath, "/media/剧场版/") {
		t.Fatalf("剧场版应落到 /media/剧场版/ 下，实际 %q", res.SavePath)
	}
	st := app.state.Get(sub.ID)
	if st == nil || st.Pushed != 1 {
		t.Fatalf("推送计数没记上: %+v", st)
	}
	if !st.MovieDone {
		t.Fatal("剧场版推送成功后应标记 movie_done（之后不再更新）")
	}
}

// TestFetchCoverAsyncNeverBlocks 抓不到封面时：调用立刻返回、订阅不丢、推送路径照常。
func TestFetchCoverAsyncNeverBlocks(t *testing.T) {
	qb := fakeQbit(t)
	app := newTestApp(t, qb.URL)
	// 指向一个必然连不上的站点：连接被拒，速度很快，不依赖外网。
	app.cfg.MikanHost = "http://127.0.0.1:1"

	sub := &Subscription{
		ID:    "t-nocover",
		Title: "没有封面的番",
		URL:   "https://mikanime.tv/RSS/Bangumi?bangumiId=1234&subgroupid=5",
		Cover: "",
	}
	sub.applyDefaults()
	app.cfg.Subscriptions = []Subscription{*sub}

	start := time.Now()
	app.FetchCoverAsync(sub.ID)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("封面抓取不该阻塞调用方，实际耗时 %v", d)
	}

	// 等后台那一轮失败落地（连接被拒是毫秒级；给足余量但不依赖外网）。
	time.Sleep(700 * time.Millisecond)

	got := app.cfg.FindSub(sub.ID)
	if got == nil {
		t.Fatal("封面抓取失败把订阅弄丢了")
	}
	if strings.TrimSpace(got.Cover) != "" {
		t.Fatalf("抓不到封面时 Cover 应保持为空，实际 %q", got.Cover)
	}
	if p := RenderSavePath(got, 1); strings.TrimSpace(p) == "" {
		t.Fatal("封面失败后落盘路径不该变空")
	}
	// 再推一次依然正常（封面失败没有污染任何状态）。
	it := FeedItem{Title: "[LoliHouse] 没有封面的番 - 01 [1080p]", Torrent: fakeTorrent(t).URL}
	if res, err := app.pushOne(got, it, 1); err != nil || !res.OK {
		t.Fatalf("封面失败后推送不应受影响: ok=%v err=%v", res.OK, err)
	}
}

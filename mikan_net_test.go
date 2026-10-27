//go:build nettest

// 联网测试：只在 go test -tags nettest 时运行，避免默认 go test 打外网。

package main

import (
	"testing"
)

func liveCfg() *Config {
	return &Config{MikanHost: "https://mikanime.tv", UA: "Mozilla/5.0 (X11; Linux aarch64) anime-go/0.1"}
}

func TestMikanLiveSearch(t *testing.T) {
	items, err := MikanSearch(liveCfg(), "胆大党")
	if err != nil {
		t.Fatalf("search err: %v", err)
	}
	t.Logf("results=%d", len(items))
	for i, it := range items {
		if i >= 5 {
			break
		}
		t.Logf("  [%s] %s | cover=%s | %s", it.ID, it.Title, it.Cover, it.URL)
	}
	if len(items) == 0 {
		t.Fatal("no items")
	}
	if items[0].ID == "" || items[0].Title == "" {
		t.Fatalf("bad first item: %+v", items[0])
	}
}

func TestMikanLiveSearchMulti(t *testing.T) {
	items, err := MikanSearch(liveCfg(), "魔法")
	if err != nil {
		t.Fatalf("search err: %v", err)
	}
	t.Logf("results=%d", len(items))
	for i, it := range items {
		if i >= 6 {
			break
		}
		t.Logf("  [%s] %s", it.ID, it.Title)
	}
}

func TestMikanLiveBangumi(t *testing.T) {
	b, err := MikanBangumiByID(liveCfg(), "3981")
	if err != nil {
		t.Fatalf("bangumi err: %v", err)
	}
	t.Logf("id=%s title=%q week=%q cover=%s", b.ID, b.Title, b.Week, b.Cover)
	t.Logf("rss_all=%s", b.RSSAll)
	t.Logf("groups=%d", len(b.Groups))
	for i, g := range b.Groups {
		if i >= 4 {
			break
		}
		t.Logf("  %-20s sg=%-6s day=%-10s rss=%s", g.Label, g.SubgroupID, g.UpdateDay, g.RSS)
	}
	if len(b.Groups) == 0 {
		t.Fatal("no groups")
	}
	if b.Title == "" {
		t.Fatal("empty title")
	}
}

func TestMikanLiveIDText(t *testing.T) {
	items, err := MikanSearch(liveCfg(), "id: 3981")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	t.Logf("id=%s title=%s", items[0].ID, items[0].Title)
}

// 解析器本身的自检：不联网
func TestHTMLParserBasics(t *testing.T) {
	doc := parseHTML(`<div class="a b"><li><a href="/x/12">t</a></li></div>`)
	if n := queryFirst(doc, "div.a"); n == nil || !n.hasClass("b") {
		t.Fatal("class match failed")
	}
	lis := queryAll(doc, "div > li")
	if len(lis) != 1 {
		t.Fatalf("child selector: got %d", len(lis))
	}
	if got := textOf(queryFirst(doc, "li a")); got != "t" {
		t.Fatalf("textOf=%q", got)
	}
	if got := queryFirst(doc, `a`).attr("href"); got != "/x/12" {
		t.Fatalf("attr=%q", got)
	}
}

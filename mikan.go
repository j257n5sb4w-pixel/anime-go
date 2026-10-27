package main

// ============================================================
//  mikan 番剧搜索（对照 ani-rss 的实现）
//
//  ani-rss 的「搜索」不是 JSON API，而是服务端抓 mikan 网页再解析 HTML：
//    MikanService.search()  →  /Home/Search?searchstr=xxx
//    MikanService.getGroups()→  /Home/Bangumi/{id}
//  这里做同样的事，只是换成 Go 标准库解析。
//
//  mikanime.tv 的真实结构（2026-10 实测，和 mikanani.me 有差异）：
//
//   搜索页：
//     <div class="... an-ul">
//       <li>
//         <a href="/Home/Bangumi/3981">
//           <span data-src="/images/Bangumi/....jpg" class="b-lazy"></span>
//           <div class="an-info"><div class="an-info-group">
//             <div class="an-text" title="魔法少女奈叶">魔法少女奈叶</div>
//           </div></div>
//         </a>
//       </li>
//
//   番剧页：
//     <p class="bangumi-title">标题 <a class="mikan-rss" href="/RSS/Bangumi?bangumiId=3981">
//     <p class="bangumi-info">放送日期：星期六</p>
//     <li class="leftbar-item">
//       <span><a class="subgroup-name" data-anchor="#45">爱恋字幕社</a></span>
//       <span class="date">10/5/2026</span>
//     </li>
//     <div class="subgroup-text" id="45"> ... <a class="mikan-rss" href="...&subgroupid=45">
//
//  注意：mikan 把中日文都写成 &#x9B54; 这样的数字实体，
//  所有取出来的字符串都必须过一遍 html.UnescapeString。
// ============================================================

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultMikanHost = "https://mikanime.tv"

// mikan 页面很大（搜索页 4MB+），25s 的共享 client 容易超时，单独给一个。
var mikanClient = &http.Client{Timeout: 45 * time.Second}

var (
	// ani-rss 的特例语法：直接搜 "id: 3981"
	reMikanIDText = regexp.MustCompile(`^id:\s*(\d+)$`)
	// 从 /Home/Bangumi/3981 取番号
	reTrailingNum = regexp.MustCompile(`(\d+)/?$`)
	// 从 style="background: url('/images/...')" 里取封面
	reCSSURL = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)
	// 季度选择器里的年份
	reFourDigits = regexp.MustCompile(`(1[89]\d{2}|20\d{2}|2[1-9]\d{2})`)
)

// ============================================================
//  极简 HTML 解析器
//
//  只用标准库，够解析 mikan 的页面就行：
//  建一棵节点树 + 一个支持 "tag"、".class"、"#id"、"a b"（后代）、"a > b"（子）的选择器。
//  容忍未闭合标签（li/p/td 遇到同名开标签自动闭合）、注释、doctype、script/style 原文。
// ============================================================

type hnode struct {
	tag      string
	attrs    map[string]string
	children []*hnode
	data     string // 文本节点内容（tag 为空）
}

func (n *hnode) attr(k string) string {
	if n == nil || n.attrs == nil {
		return ""
	}
	return n.attrs[k]
}

func (n *hnode) hasClass(c string) bool {
	if n == nil {
		return false
	}
	for _, x := range strings.Fields(n.attr("class")) {
		if x == c {
			return true
		}
	}
	return false
}

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// 这些标签允许省略闭合，遇到同名开标签就先关掉上一个
var autoCloseTags = map[string]bool{
	"li": true, "p": true, "tr": true, "td": true, "th": true,
	"option": true, "dt": true, "dd": true,
}

const (
	maxHTMLNodes = 500000 // 防御：4MB 页面也不至于把 1GB 的板子吃光
	maxHTMLDepth = 512
)

func parseHTML(src string) *hnode {
	root := &hnode{tag: "#doc"}
	stack := []*hnode{root}
	nodes := 0
	i := 0
	for i < len(src) {
		if src[i] != '<' {
			j := strings.IndexByte(src[i:], '<')
			if j < 0 {
				j = len(src) - i
			}
			if txt := src[i : i+j]; strings.TrimSpace(txt) != "" {
				cur := stack[len(stack)-1]
				cur.children = append(cur.children, &hnode{data: txt})
				nodes++
			}
			i += j
			if nodes > maxHTMLNodes {
				break
			}
			continue
		}
		// 注释
		if strings.HasPrefix(src[i:], "<!--") {
			if k := strings.Index(src[i:], "-->"); k >= 0 {
				i += k + 3
			} else {
				break
			}
			continue
		}
		// doctype / 处理指令
		if strings.HasPrefix(src[i:], "<!") || strings.HasPrefix(src[i:], "<?") {
			if k := strings.IndexByte(src[i:], '>'); k >= 0 {
				i += k + 1
			} else {
				break
			}
			continue
		}
		// 闭标签
		if strings.HasPrefix(src[i:], "</") {
			k := strings.IndexByte(src[i:], '>')
			if k < 0 {
				break
			}
			name := strings.ToLower(strings.TrimSpace(src[i+2 : i+k]))
			i += k + 1
			for d := len(stack) - 1; d > 0; d-- {
				if stack[d].tag == name {
					stack = stack[:d]
					break
				}
			}
			continue
		}
		// 开标签：先找 '>'（跳过引号里的）
		end := -1
		var quote byte
		for j := i + 1; j < len(src); j++ {
			c := src[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '"' || c == '\'' {
				quote = c
				continue
			}
			if c == '>' {
				end = j
				break
			}
		}
		if end < 0 {
			break
		}
		raw := src[i+1 : end]
		i = end + 1

		selfClose := false
		trimmed := strings.TrimRight(raw, " \t\r\n")
		if strings.HasSuffix(trimmed, "/") {
			selfClose = true
			raw = strings.TrimSuffix(trimmed, "/")
		}
		name, attrs := splitTag(raw)
		if name == "" {
			continue
		}

		// script/style 内容不做解析，整段吃掉
		if name == "script" || name == "style" {
			closeTag := "</" + name
			low := strings.ToLower(src[i:])
			if k := strings.Index(low, closeTag); k >= 0 {
				seg := src[i+k:]
				if e := strings.IndexByte(seg, '>'); e >= 0 {
					i += k + e + 1
				} else {
					i = len(src)
				}
			} else {
				i = len(src)
			}
			continue
		}

		// 自动闭合
		if autoCloseTags[name] && len(stack) > 1 && stack[len(stack)-1].tag == name {
			stack = stack[:len(stack)-1]
		}

		n := &hnode{tag: name, attrs: attrs}
		cur := stack[len(stack)-1]
		cur.children = append(cur.children, n)
		nodes++
		if nodes > maxHTMLNodes {
			break
		}
		if !selfClose && !voidTags[name] && len(stack) < maxHTMLDepth {
			stack = append(stack, n)
		}
	}
	return root
}

// splitTag 把 `div class="a" id=b` 拆成标签名和属性表
func splitTag(raw string) (string, map[string]string) {
	name := raw
	rest := ""
	if k := strings.IndexAny(raw, " \t\r\n"); k >= 0 {
		name = raw[:k]
		rest = raw[k+1:]
	}
	attrs := map[string]string{}
	for len(rest) > 0 {
		rest = strings.TrimLeft(rest, " \t\r\n")
		if rest == "" {
			break
		}
		var key string
		if k := strings.IndexAny(rest, "= \t\r\n"); k < 0 {
			key, rest = rest, ""
		} else {
			key, rest = rest[:k], rest[k:]
		}
		rest = strings.TrimLeft(rest, " \t\r\n")
		val := ""
		if strings.HasPrefix(rest, "=") {
			rest = strings.TrimLeft(rest[1:], " \t\r\n")
			if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
				q := rest[0]
				if e := strings.IndexByte(rest[1:], q); e >= 0 {
					val = rest[1 : 1+e]
					rest = rest[1+e+1:]
				} else {
					val = rest[1:]
					rest = ""
				}
			} else if e := strings.IndexAny(rest, " \t\r\n"); e < 0 {
				val, rest = rest, ""
			} else {
				val, rest = rest[:e], rest[e:]
			}
		}
		key = strings.ToLower(key)
		if key != "" {
			if _, dup := attrs[key]; !dup {
				attrs[key] = val
			}
		}
	}
	return strings.ToLower(strings.TrimSpace(name)), attrs
}

// textOf 取节点下的纯文本（跳过 script/style），压掉多余空白并还原 HTML 实体
func textOf(n *hnode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*hnode)
	walk = func(x *hnode) {
		if x.tag == "script" || x.tag == "style" {
			return
		}
		if x.tag == "" {
			b.WriteString(x.data)
			return
		}
		for _, c := range x.children {
			walk(c)
		}
	}
	walk(n)
	return html.UnescapeString(strings.Join(strings.Fields(b.String()), " "))
}

// ============================================================
//  选择器
// ============================================================

type selStep struct {
	comb byte // ' ' 后代 / '>' 直接子节点
	tag  string
	id   string
	cls  []string
}

func parseSel(sel string) []selStep {
	var steps []selStep
	comb := byte(' ')
	i := 0
	for i < len(sel) {
		c := sel[i]
		if c == ' ' || c == '>' {
			if c == '>' {
				comb = '>'
			}
			i++
			continue
		}
		j := i
		for j < len(sel) && sel[j] != ' ' && sel[j] != '>' {
			j++
		}
		tok := sel[i:j]
		st := selStep{comb: comb}
		k := 0
		if k < len(tok) && tok[k] != '.' && tok[k] != '#' {
			e := k
			for e < len(tok) && tok[e] != '.' && tok[e] != '#' {
				e++
			}
			st.tag = strings.ToLower(tok[k:e])
			k = e
		}
		for k < len(tok) {
			mark := tok[k]
			k++
			e := k
			for e < len(tok) && tok[e] != '.' && tok[e] != '#' {
				e++
			}
			v := tok[k:e]
			if mark == '.' {
				st.cls = append(st.cls, v)
			} else {
				st.id = v
			}
			k = e
		}
		steps = append(steps, st)
		comb = ' '
		i = j
	}
	return steps
}

func (st selStep) match(n *hnode) bool {
	if n == nil || n.tag == "" {
		return false
	}
	if st.tag != "" && n.tag != st.tag {
		return false
	}
	if st.id != "" && n.attr("id") != st.id {
		return false
	}
	for _, c := range st.cls {
		if !n.hasClass(c) {
			return false
		}
	}
	return true
}

// queryAll 返回按文档顺序排列的全部匹配节点
func queryAll(root *hnode, sel string) []*hnode {
	steps := parseSel(sel)
	if len(steps) == 0 || root == nil {
		return nil
	}
	cur := []*hnode{root}
	for _, st := range steps {
		var next []*hnode
		seen := map[*hnode]bool{}
		add := func(n *hnode) {
			if n == nil || seen[n] || !st.match(n) {
				return
			}
			seen[n] = true
			next = append(next, n)
		}
		for _, base := range cur {
			if st.comb == '>' {
				for _, c := range base.children {
					add(c)
				}
				continue
			}
			var walk func(*hnode)
			walk = func(x *hnode) {
				for _, c := range x.children {
					add(c)
					walk(c)
				}
			}
			walk(base)
		}
		if len(next) == 0 {
			return nil
		}
		cur = next
	}
	return cur
}

func queryFirst(root *hnode, sel string) *hnode {
	if ns := queryAll(root, sel); len(ns) > 0 {
		return ns[0]
	}
	return nil
}

// ============================================================
//  mikan 搜索
// ============================================================

// MikanSearchItem 是搜索结果的单条番剧
type MikanSearchItem struct {
	ID    string `json:"id"`    // mikan 番号（bangumiId）
	Title string `json:"title"` // 番名
	URL   string `json:"url"`   // 番剧页
	Cover string `json:"cover"` // 封面（可能为空）
}

// MikanGroup 是一个字幕组
type MikanGroup struct {
	Label      string `json:"label"`       // 字幕组名
	SubgroupID string `json:"subgroup_id"` // subgroupid
	RSS        string `json:"rss"`         // 该字幕组的 RSS 地址
	UpdateDay  string `json:"update_day"`  // 最后更新日期（原文）
}

// MikanBangumi 是番剧详情
type MikanBangumi struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Cover  string       `json:"cover"`
	Week   string       `json:"week"`    // "星期六"
	RSSAll string       `json:"rss_all"` // 整部番（不分子幕组）的 RSS
	Groups []MikanGroup `json:"groups"`
	URL    string       `json:"url"`
}

func mikanHostOf(cfg *Config) string {
	h := ""
	if cfg != nil {
		h = strings.TrimSpace(cfg.MikanHost)
	}
	if h == "" {
		h = defaultMikanHost
	}
	return strings.TrimRight(h, "/")
}

func mikanUA(cfg *Config) string {
	if cfg != nil && strings.TrimSpace(cfg.UA) != "" {
		return cfg.UA
	}
	return "Mozilla/5.0 (X11; Linux aarch64) anime-go/0.1"
}

// absURL 把 mikan 页面里的相对地址补成绝对地址
func absURL(host, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if strings.HasPrefix(p, "//") {
		return "https:" + p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return host + p
}

func fetchMikan(host, path, ua string) (string, error) {
	u := path
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = host + path
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	resp, err := mikanClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("连接 mikan 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return "", fmt.Errorf("读取 mikan 页面失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("mikan 返回 %d", resp.StatusCode)
	}
	return string(body), nil
}

// MikanSearch 搜索番剧。text 支持 ani-rss 的 "id: 3981" 语法。
func MikanSearch(cfg *Config, text string) ([]MikanSearchItem, error) {
	host := mikanHostOf(cfg)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("请输入要搜索的番名")
	}
	// ani-rss：^id: (\d+)$ 直接按番号取
	if m := reMikanIDText.FindStringSubmatch(text); m != nil {
		b, err := MikanBangumiByID(cfg, m[1])
		if err != nil {
			return nil, err
		}
		return []MikanSearchItem{{ID: b.ID, Title: b.Title, URL: b.URL, Cover: b.Cover}}, nil
	}

	body, err := fetchMikan(host, "/Home/Search?searchstr="+url.QueryEscape(text), mikanUA(cfg))
	if err != nil {
		return nil, err
	}
	doc := parseHTML(body)

	// 结果卡片容器：mikanime.tv 是 div.an-ul，mikanani.me 是 .sk-bangumi
	var containers []*hnode
	containers = append(containers, queryAll(doc, "div.an-ul")...)
	if len(containers) == 0 {
		containers = append(containers, queryAll(doc, ".an-ul")...)
	}
	if len(containers) == 0 {
		containers = append(containers, queryAll(doc, ".sk-bangumi")...)
	}

	var items []MikanSearchItem
	seen := map[string]bool{}
	for _, cont := range containers {
		for _, li := range queryAll(cont, "li") {
			it, ok := parseMikanCard(li, host)
			if !ok || seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			items = append(items, it)
			if len(items) >= 80 { // 结果太多没意义，也省板子内存
				return items, nil
			}
		}
	}
	return items, nil
}

// parseMikanCard 从 <li> 里取一条番剧卡片
func parseMikanCard(li *hnode, host string) (MikanSearchItem, bool) {
	var it MikanSearchItem
	a := queryFirst(li, "a")
	if a == nil {
		return it, false
	}
	href := a.attr("href")
	if m := reTrailingNum.FindStringSubmatch(href); m != nil {
		it.ID = m[1]
	}
	if it.ID == "" {
		return it, false
	}
	it.URL = absURL(host, href)

	if sp := queryFirst(li, "span"); sp != nil {
		it.Cover = absURL(host, sp.attr("data-src"))
	}
	if t := queryFirst(li, ".an-text"); t != nil {
		it.Title = strings.TrimSpace(t.attr("title"))
		if it.Title == "" {
			it.Title = textOf(t)
		}
	}
	if it.Title == "" {
		it.Title = textOf(a)
	}
	it.Title = html.UnescapeString(strings.TrimSpace(it.Title))
	if it.Title == "" {
		return it, false
	}
	return it, true
}

// MikanBangumiByID 抓番剧页，解析标题/封面/放送星期/字幕组 RSS
func MikanBangumiByID(cfg *Config, id string) (*MikanBangumi, error) {
	host := mikanHostOf(cfg)
	id = strings.TrimSpace(id)
	if id == "" || !isDigits(id) {
		return nil, fmt.Errorf("番号不合法: %s", id)
	}
	body, err := fetchMikan(host, "/Home/Bangumi/"+id, mikanUA(cfg))
	if err != nil {
		return nil, err
	}
	return parseMikanBangumi(body, host, id), nil
}

func parseMikanBangumi(body, host, id string) *MikanBangumi {
	doc := parseHTML(body)
	b := &MikanBangumi{
		ID:  id,
		URL: host + "/Home/Bangumi/" + id,
	}

	if p := queryFirst(doc, "p.bangumi-title"); p != nil {
		b.Title = textOf(p)
	}
	if b.Title == "" {
		if h := queryFirst(doc, "h1"); h != nil {
			b.Title = textOf(h)
		}
	}

	// 放送日期 → 星期几（首页「今日订阅」直接用得上）
	for _, p := range queryAll(doc, "p.bangumi-info") {
		t := textOf(p)
		if strings.HasPrefix(t, "放送日期") {
			if i := strings.Index(t, "："); i >= 0 {
				b.Week = strings.TrimSpace(t[i+len("："):])
			} else if i := strings.Index(t, ":"); i >= 0 {
				b.Week = strings.TrimSpace(t[i+1:])
			}
			break
		}
	}

	// 封面：mikanime.tv 放在 div.bangumi-poster 的 background-image 里
	for _, sel := range []string{"div.bangumi-poster", "div.m-detail-blur-bg", "div.content"} {
		if c := queryFirst(doc, sel); c != nil {
			if m := reCSSURL.FindStringSubmatch(c.attr("style")); m != nil {
				b.Cover = absURL(host, m[1])
				break
			}
		}
	}
	if b.Cover == "" {
		if sp := queryFirst(doc, "span.b-lazy"); sp != nil {
			b.Cover = absURL(host, sp.attr("data-src"))
		}
	}

	// 整部番的 RSS
	if a := queryFirst(doc, "a.mikan-rss"); a != nil {
		b.RSSAll = absURL(host, a.attr("href"))
	}
	if b.RSSAll == "" {
		b.RSSAll = host + "/RSS/Bangumi?bangumiId=" + id
	}

	// 字幕组列表
	for _, li := range queryAll(doc, "li.leftbar-item") {
		a := queryFirst(li, "a.subgroup-name")
		if a == nil {
			continue
		}
		g := MikanGroup{
			Label:      textOf(a),
			SubgroupID: strings.TrimPrefix(strings.TrimSpace(a.attr("data-anchor")), "#"),
		}
		if d := queryFirst(li, "span.date"); d != nil {
			g.UpdateDay = textOf(d)
		}
		if g.SubgroupID != "" {
			// 优先用页面上真实的 RSS 链接，找不到再按格式拼
			if sec := queryFirst(doc, "#"+g.SubgroupID); sec != nil {
				if r := queryFirst(sec, "a.mikan-rss"); r != nil {
					g.RSS = absURL(host, r.attr("href"))
				}
			}
			if g.RSS == "" {
				g.RSS = host + "/RSS/Bangumi?bangumiId=" + id + "&subgroupid=" + g.SubgroupID
			}
		}
		if g.Label == "" && g.SubgroupID == "" {
			continue
		}
		b.Groups = append(b.Groups, g)
	}
	return b
}

// ============================================================
//  当季新番（按星期分组）
//
//  ani-rss 用的是 /Home/BangumiCoverFlowByDayOfWeek?year=&seasonStr=，
//  mikanime.tv 上的真实结构（2026-10 实测）：
//
//    <div class="sk-bangumi" data-dayofweek="0">
//      <div id="data-row-0" class="row">星期日</div>
//      <div class="an-box"><ul class="list-inline an-ul">
//        <li><a href="/Home/Bangumi/3981">
//              <span data-src="/images/Bangumi/....jpg?..." class="b-lazy"></span>
//              <div class="an-info"><div class="an-info-group">
//                <div class="an-text" title="标题">标题</div>
//    ...
//
//  data-dayofweek: 0=星期日 … 6=星期六，7=剧场版，8=OVA
//  注意：不带 year/seasonStr 时该地址返回 500，必须两个参数都带上。
// ============================================================

// MikanSeasonItem 是当季列表里的一条番剧
type MikanSeasonItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Cover string `json:"cover"`
}

// MikanDay 是「某一档」的番剧集合（通常是星期几）
type MikanDay struct {
	DOW   int               `json:"dow"`   // 0=星期日 … 6=星期六，7=剧场版，8=OVA
	Label string            `json:"label"` // 星期日 / 剧场版 / OVA
	Items []MikanSeasonItem `json:"items"`
}

// MikanSeason 是一个季度的当季新番
type MikanSeason struct {
	Year   int        `json:"year"`
	Season string     `json:"season"` // 冬 春 夏 秋
	Label  string     `json:"label"`  // 2026 秋
	Total  int        `json:"total"`
	Days   []MikanDay `json:"days"`
}

// mikanSeasonNames 按季度序号排（1-3 冬、4-6 春、7-9 夏、10-12 秋）
var mikanSeasonNames = []string{"冬", "春", "夏", "秋"}

// mikanDayLabels 是 data-dayofweek 的兜底名称（页面里取不到时用）
var mikanDayLabels = map[int]string{
	0: "星期日", 1: "星期一", 2: "星期二", 3: "星期三",
	4: "星期四", 5: "星期五", 6: "星期六", 7: "剧场版", 8: "OVA",
}

// MikanCurrentSeason 按时间算出当前季度
func MikanCurrentSeason(t time.Time) (int, string) {
	idx := (int(t.Month()) - 1) / 3
	if idx < 0 || idx >= len(mikanSeasonNames) {
		idx = 0
	}
	return t.Year(), mikanSeasonNames[idx]
}

// MikanSeasonOptions 从当前季度往回退 n 个，返回给前端做下拉框
func MikanSeasonOptions(now time.Time, n int) []map[string]any {
	if n <= 0 {
		n = 8
	}
	year, season := MikanCurrentSeason(now)
	idx := 0
	for i, s := range mikanSeasonNames {
		if s == season {
			idx = i
			break
		}
	}
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{
			"year":   year,
			"season": mikanSeasonNames[idx],
			"label":  fmt.Sprintf("%d %s", year, mikanSeasonNames[idx]),
			"value":  fmt.Sprintf("%d-%s", year, mikanSeasonNames[idx]),
		})
		idx--
		if idx < 0 {
			idx = len(mikanSeasonNames) - 1
			year--
		}
	}
	return out
}

// mikanSeasonValid 校验季度名
func mikanSeasonValid(s string) bool {
	for _, v := range mikanSeasonNames {
		if v == s {
			return true
		}
	}
	return false
}

// ============================================================
//  季度清单：直接从 mikan 首页的 .date-select 解析
//
//  以前 MikanSeasonOptions 用本地时钟往回推算 9 个季度，问题是：
//    · 12 月底本地还算「2026 秋」，而站点已经上了「2027 冬」的番组；
//    · 只能往前 9 个季度，想看两年前的冷番就没有。
//  现在改为抓首页（https://mikanime.tv/）里的季度选择器，站点上有什么就能选什么，
//  同时把「站点认为的当前季度」也一并取回来，做到和站点同步更新。
//
//  首页结构（已实测）：
//    <ul class="navbar-nav date-select">
//      <div class="date-text"> 2026 秋季番组 </div>
//      <li class="dropdown-submenu"><a class="default-cursor">2026</a>
//        <ul class="dropdown-menu">
//          <li><a data-year="2026" data-season="&#x79CB;">秋季番组</a></li>  ← 秋 夏 春 冬
//          同形 3 条
//
//  注意：年份内顺序是 秋、夏、春、冬，不能依赖顺序，按 data-season 的值判断。
// ============================================================

// MikanSeasonOption 是季度下拉框的一项
type MikanSeasonOption struct {
	Year   int    `json:"year"`
	Season string `json:"season"`
	Label  string `json:"label"` // 2026 秋
	Value  string `json:"value"` // 2026-秋
}

var (
	mikanSeasonCatMu   sync.Mutex
	mikanSeasonCatAt   time.Time
	mikanSeasonCatHost string
	mikanSeasonCatOpts []MikanSeasonOption
	mikanSeasonCatCur  *MikanSeasonOption
)

// 首页有 180KB+，别每次都抓；季度清单一天变不了几次
const mikanSeasonCatTTL = 30 * time.Minute

// mikanSeasonOpt 由 (year, season) 造一项
func mikanSeasonOpt(year int, season string) MikanSeasonOption {
	return MikanSeasonOption{
		Year:   year,
		Season: season,
		Label:  fmt.Sprintf("%d %s", year, season),
		Value:  fmt.Sprintf("%d-%s", year, season),
	}
}

// seasonRank 返回季度在一年内的次序（冬=0 春=1 夏=2 秋=3），认不出返回 -1
func seasonRank(s string) int {
	for i, v := range mikanSeasonNames {
		if v == s {
			return i
		}
	}
	return -1
}

// cleanSeasonAttr 把 data-season 的值收拾成 冬/春/夏/秋
//
// 页面上是数字实体（&#x79CB;），调用方已 html.UnescapeString，
// 但个别老页面会多带「季番组」后缀，这里一并去掉。
func cleanSeasonAttr(v string) string {
	v = strings.TrimSpace(html.UnescapeString(v))
	for _, suf := range []string{"季番组", "番组", "季度", "季"} {
		v = strings.TrimSuffix(v, suf)
	}
	v = strings.TrimSpace(v)
	if seasonRank(v) >= 0 {
		return v
	}
	return ""
}

// parseMikanSeasonNav 解析首页季度选择器
// 返回按时间倒序排好的清单，以及站点标注的「当前季度」（取不到时为 nil）。
func parseMikanSeasonNav(body string) ([]MikanSeasonOption, *MikanSeasonOption) {
	doc := parseHTML(body)

	var cur *MikanSeasonOption
	if n := queryFirst(doc, "ul.date-select .date-text"); n != nil {
		txt := strings.TrimSpace(html.UnescapeString(textOf(n)))
		var year int
		var season string
		if m := reFourDigits.FindString(txt); m != "" {
			if v, err := strconv.Atoi(m); err == nil {
				year = v
			}
		}
		for _, s := range mikanSeasonNames {
			if strings.Contains(txt, s) {
				season = s
				break
			}
		}
		if year > 0 && season != "" {
			o := mikanSeasonOpt(year, season)
			cur = &o
		}
	}

	seen := map[int]map[string]bool{}
	var opts []MikanSeasonOption
	for _, a := range queryAll(doc, "ul.date-select a") {
		ys := strings.TrimSpace(a.attr("data-year"))
		ss := cleanSeasonAttr(a.attr("data-season"))
		if ys == "" || ss == "" {
			continue
		}
		year, err := strconv.Atoi(ys)
		if err != nil || year < 1950 || year > 2100 {
			continue
		}
		if seen[year] == nil {
			seen[year] = map[string]bool{}
		}
		if seen[year][ss] {
			continue
		}
		seen[year][ss] = true
		opts = append(opts, mikanSeasonOpt(year, ss))
	}
	if len(opts) == 0 {
		return nil, cur
	}

	// 倒序：新年份在前；同年内 秋→夏→春→冬
	sort.SliceStable(opts, func(i, j int) bool {
		if opts[i].Year != opts[j].Year {
			return opts[i].Year > opts[j].Year
		}
		return seasonRank(opts[i].Season) > seasonRank(opts[j].Season)
	})
	return opts, cur
}

// MikanSeasonCatalog 取季度清单（带 30 分钟缓存）。
// 抓首页失败时退回本地推算，保证下拉框永远有东西可选。
func MikanSeasonCatalog(cfg *Config) ([]MikanSeasonOption, *MikanSeasonOption) {
	host := mikanHostOf(cfg)
	now := time.Now()

	mikanSeasonCatMu.Lock()
	if mikanSeasonCatHost == host && len(mikanSeasonCatOpts) > 0 && now.Sub(mikanSeasonCatAt) < mikanSeasonCatTTL {
		opts, cur := mikanSeasonCatOpts, mikanSeasonCatCur
		mikanSeasonCatMu.Unlock()
		return opts, cur
	}
	mikanSeasonCatMu.Unlock()

	body, err := fetchMikan(host, "/", mikanUA(cfg))
	if err != nil {
		// 抓不到就用旧缓存；再没有就本地推算
		mikanSeasonCatMu.Lock()
		defer mikanSeasonCatMu.Unlock()
		if len(mikanSeasonCatOpts) > 0 {
			return mikanSeasonCatOpts, mikanSeasonCatCur
		}
		return mikanLocalSeasonOptions(now, 12), nil
	}
	opts, cur := parseMikanSeasonNav(body)
	if len(opts) == 0 {
		mikanSeasonCatMu.Lock()
		defer mikanSeasonCatMu.Unlock()
		if len(mikanSeasonCatOpts) > 0 {
			return mikanSeasonCatOpts, mikanSeasonCatCur
		}
		return mikanLocalSeasonOptions(now, 12), nil
	}

	mikanSeasonCatMu.Lock()
	mikanSeasonCatHost = host
	mikanSeasonCatAt = now
	mikanSeasonCatOpts = opts
	mikanSeasonCatCur = cur
	mikanSeasonCatMu.Unlock()
	return opts, cur
}

// mikanLocalSeasonOptions 是站点抓不到时用的兜底（本地推算）
func mikanLocalSeasonOptions(now time.Time, n int) []MikanSeasonOption {
	year, season := MikanCurrentSeason(now)
	idx := seasonRank(season)
	if idx < 0 {
		idx = 0
	}
	out := make([]MikanSeasonOption, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, mikanSeasonOpt(year, mikanSeasonNames[idx]))
		idx--
		if idx < 0 {
			idx = len(mikanSeasonNames) - 1
			year--
		}
	}
	return out
}

// MikanSeasonByDate 抓某个季度的当季新番，按 data-dayofweek 分组
func MikanSeasonByDate(cfg *Config, year int, season string) (*MikanSeason, error) {
	host := mikanHostOf(cfg)
	season = strings.TrimSpace(season)
	if !mikanSeasonValid(season) {
		return nil, fmt.Errorf("季度只能是 冬/春/夏/秋")
	}
	if year < 1990 || year > 2100 {
		return nil, fmt.Errorf("年份不合法: %d", year)
	}

	path := fmt.Sprintf("/Home/BangumiCoverFlowByDayOfWeek?year=%d&seasonStr=%s",
		year, url.QueryEscape(season))
	body, err := fetchMikan(host, path, mikanUA(cfg))
	if err != nil {
		return nil, err
	}
	return parseMikanSeason(body, host, year, season), nil
}

func parseMikanSeason(body, host string, year int, season string) *MikanSeason {
	se := &MikanSeason{
		Year:   year,
		Season: season,
		Label:  fmt.Sprintf("%d %s", year, season),
	}
	doc := parseHTML(body)

	for _, blk := range queryAll(doc, "div.sk-bangumi") {
		dow := -1
		if v := strings.TrimSpace(blk.attr("data-dayofweek")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				dow = n
			}
		}
		if dow < 0 {
			continue
		}

		day := MikanDay{DOW: dow, Label: mikanDayLabels[dow]}
		// 页面上有现成的分组标题（#data-row-N），优先用它
		if h := queryFirst(blk, fmt.Sprintf("#data-row-%d", dow)); h != nil {
			if t := strings.TrimSpace(html.UnescapeString(textOf(h))); t != "" {
				day.Label = t
			}
		}
		if day.Label == "" {
			day.Label = fmt.Sprintf("第 %d 档", dow)
		}

		seen := map[string]bool{}
		for _, li := range queryAll(blk, "li") {
			it, ok := parseMikanCard(li, host)
			if !ok || seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			day.Items = append(day.Items, MikanSeasonItem{
				ID: it.ID, Title: it.Title, URL: it.URL, Cover: it.Cover,
			})
		}
		if len(day.Items) == 0 {
			continue
		}
		se.Total += len(day.Items)
		se.Days = append(se.Days, day)
	}
	return se
}

// ============================================================
//  HTTP 接口
// ============================================================

// handleMikanSearch  GET /api/mikan/search?text=xxx
func (s *server) handleMikanSearch(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("text"))
	if text == "" {
		text = strings.TrimSpace(r.URL.Query().Get("q"))
	}
	if text == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少搜索关键字"})
		return
	}
	items, err := MikanSearch(s.app.cfg, text)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":    true,
		"total": len(items),
		"items": items,
		"host":  mikanHostOf(s.app.cfg),
	})
}

// handleMikanBangumi  GET /api/mikan/bangumi?id=3981（也接受 url=）
func (s *server) handleMikanBangumi(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	raw := strings.TrimSpace(r.URL.Query().Get("url"))

	if id == "" && raw != "" {
		// 只允许抓 mikan 自己的域名：避免这个接口变成任意 URL 代理
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "url 不合法"})
			return
		}
		want, err := url.Parse(mikanHostOf(s.app.cfg))
		if err != nil || !strings.EqualFold(u.Hostname(), want.Hostname()) {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "只支持 mikan 站点地址"})
			return
		}
		if m := reTrailingNum.FindStringSubmatch(u.Path); m != nil {
			id = m[1]
		}
	}
	if id == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少番号 id"})
		return
	}
	if _, err := strconv.Atoi(id); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "番号必须是数字"})
		return
	}

	b, err := MikanBangumiByID(s.app.cfg, id)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "bangumi": b})
}

// handleMikanSeason  GET /api/mikan/season?year=2026&season=秋
// 不带参数时返回「站点当前季度」；下拉选项也来自站点首页，随站点自动更新。
func (s *server) handleMikanSeason(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// 清单与当前季度都问站点；抓不到才退回本地时钟
	options, siteCur := MikanSeasonCatalog(s.app.cfg)
	curYear, curSeason := MikanCurrentSeason(time.Now())
	if siteCur != nil {
		curYear, curSeason = siteCur.Year, siteCur.Season
	}
	year := curYear
	season := curSeason

	if v := strings.TrimSpace(q.Get("year")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1990 || n > 2100 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "年份不合法"})
			return
		}
		year = n
	}
	if v := strings.TrimSpace(q.Get("season")); v != "" {
		if !mikanSeasonValid(v) {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "季度只能是 冬/春/夏/秋"})
			return
		}
		season = v
	}

	se, err := MikanSeasonByDate(s.app.cfg, year, season)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if se.Days == nil {
		se.Days = []MikanDay{}
	}
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"season": se,
		"current": map[string]any{
			"year":   curYear,
			"season": curSeason,
			"label":  fmt.Sprintf("%d %s", curYear, curSeason),
		},
		"options": options,
		"host":    mikanHostOf(s.app.cfg),
	})
}

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// 默认集数规则，按优先级尝试；分组 2（或第一个非空捕获）作为集数
var defaultEpPatterns = []string{
	// 北宇治: [北宇治字幕组] 学生会也有洞！ / Seitokai [03][无修版]...
	`\]\s*\[(\d{1,4})(?:\.5)?\]`,
	// ANi / LoliHouse: ... - 24 [1080P]
	//
	// 2026-10-26 实测补强：原先只认数字后紧跟 "[" / END / 行尾，
	// 于是这两种真实存在的命名被判"无法识别集数"而永久跳过：
	//   [黒ネズミたち] FX 战士久留美 / FX Senshi Kurumi-chan - 03 (ABEMA 1920x1080 ...)
	//   [Group] Anime Name - 09v2 [1080p]
	// 现在允许数字后跟版本号 v2/v3，以及以 "(" 开头的画质说明。
	`\s-\s(\d{1,4})(?:\.5)?(?:[vV]\d+)?\s*(?:\[|\(|$|\s*END)`,
	// EP10 / Ep.10 / EP. 10（要求带 P，避免把单独的 E 误当集数标记）
	`(?i)\bEP\.?\s*(\d{1,4})(?:\.5)?\b`,
	// 通用 SxxExx
	`[Ss]\d{1,2}[Ee](\d{1,4})`,
	// 第12话 / 第12集 / 第12話
	`第\s*(\d{1,4})\s*[话話集]`,
	// 【12】 或 【12.5】
	`【\s*(\d{1,4})(?:\.5)?\s*】`,
	// 兜底：标题尾部独立数字 [12]
	`\[(\d{1,4})(?:v\d)?\]`,
}

var (
	reMagnet   = regexp.MustCompile(`magnet:\?xt=urn:btih:([A-Za-z0-9]+)[^\s"'<>]*`)
	reInfoHash = regexp.MustCompile(`(?i)urn:btih:([a-z0-9]{32,40})`)
	reTagSpan  = regexp.MustCompile(`\[[^\]]*\]`)
	reYear     = regexp.MustCompile(`\((\d{4})\)`)
)

func extractMagnet(s string) string {
	if s == "" {
		return ""
	}
	m := reMagnet.FindString(s)
	if m == "" {
		return ""
	}
	return strings.TrimRight(m, `"'<>`)
}

func extractInfoHash(magnet string) string {
	m := reInfoHash.FindStringSubmatch(magnet)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// ExtractEpisode 从标题里抽集数。返回 0 表示抽不到（剧场版等）
func ExtractEpisode(title string, patterns []string) int {
	if len(patterns) == 0 {
		patterns = defaultEpPatterns
	}
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		if m := re.FindStringSubmatch(title); len(m) > 1 {
			// 取第一个非空的捕获组
			for _, g := range m[1:] {
				if g == "" {
					continue
				}
				if n, err := strconv.Atoi(g); err == nil && n > 0 && n < 3000 {
					return n
				}
				break
			}
		}
	}
	return 0
}

// IsMovie 判断是否剧场版（无集数概念的条目）。
//
// 只靠 "[MOVIE]"/"[剧场版]" 这两个标记会漏掉绝大多数真实剧场版：
//   - 中文常用「剧场版」「电影版」「劇場版」前缀，或直接什么都不写；
//   - 日文原盘常用「劇場版」「映画」；
//   - 压制组常用「BDRemux」「BDMV」「[Movie]」括注。
//
// 2026-10-26 实测：「[LinRip] 超时空辉夜姬！／Chou Kaguya-hime [BDRemux 1080p AVC FLAC]」
// 被判定为非剧场版，从而以"无法识别集数"永久丢弃——正确的剧场版订阅永远下不到。
func IsMovie(title string) bool {
	up := strings.ToUpper(title)

	// 显式标记，优先级最高
	for _, m := range []string{"[MOVIE]", "[剧场版]", "[劇場版]", "剧场版", "劇場版", "映画"} {
		if strings.Contains(up, strings.ToUpper(m)) {
			return true
		}
	}

	// 压制组惯用的剧场版特征：整部作品打包，通常是单文件。
	//
	// ⚠ 必须排除 TV 录制/合集干扰：
	//   "[BDRemux]" 出现即视为剧场版（TV 版罕有整季 BDRemux 单种）；
	//   "BDMV"/"BDBOX"/"COMPLETE" 同理，但 "COMPLETE" 也可能表示整季合集，
	//   所以只在同时没有集数区间写法（如 "01-12"）时才认。
	hasPack := false
	for _, m := range []string{"BDREMUX", "BD-BOX", "BDBOX", "BDMV", "MOVIE", "FILM"} {
		if strings.Contains(up, m) {
			hasPack = true
			break
		}
	}
	if !hasPack {
		return false
	}
	// 带 "01-12"/"1-12" 这种集数区间的，是整季合集而非剧场版
	if epRangeRe.MatchString(title) {
		return false
	}
	if strings.Contains(up, "COMPLETE") && !strings.Contains(up, "BDREMUX") {
		return false
	}
	return true
}

// epRangeRe 匹配 "01-12"、"1-12" 这类集数区间（整季合集的标志）。
var epRangeRe = regexp.MustCompile(`\b0?\d{1,3}\s*[-~]\s*0?\d{1,3}\b`)

// compileRules 编译 match/exclude 规则。
// 支持 ani-rss 的 "{{字幕组}}:regex" 语法：前缀不匹配时整条规则视为不命中。
type rule struct {
	prefix  string // 已小写，空表示无前缀限制
	negate  bool   // prefix 前加 ! 表示"不含该字幕组"
	pattern *regexp.Regexp
	raw     string
}

// cleanRuleList 清洗并校验一组规则（用于设置页保存全局默认规则）。
// 返回去掉空行后的列表，以及无法编译的正则（形如 "规则 -> 错误原因"）。
// compileRules 遇到坏正则会静默退回字面量匹配，设置页要提前把这个问题摆到台面上。
func cleanRuleList(list []string) ([]string, []string) {
	out := make([]string, 0, len(list))
	var bad []string
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		body := s
		if strings.HasPrefix(body, "{{") {
			if end := strings.Index(body, "}}"); end > 0 {
				body = strings.TrimPrefix(body[end+2:], ":")
			}
		}
		if _, err := regexp.Compile(body); err != nil {
			bad = append(bad, s+" -> "+err.Error())
			continue
		}
		out = append(out, s)
	}
	return out, bad
}

func compileRules(list []string) []rule {
	out := make([]rule, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		var r rule
		r.raw = s
		body := s
		if strings.HasPrefix(body, "{{") {
			if end := strings.Index(body, "}}"); end > 0 {
				pfx := body[2:end]
				body = strings.TrimPrefix(body[end+2:], ":")
				if strings.HasPrefix(pfx, "!") {
					r.negate = true
					pfx = pfx[1:]
				}
				r.prefix = strings.ToLower(strings.TrimSpace(pfx))
			}
		}
		re, err := regexp.Compile(body)
		if err != nil {
			// 非法正则退回字面量匹配
			re = regexp.MustCompile(regexp.QuoteMeta(body))
		}
		r.pattern = re
		out = append(out, r)
	}
	return out
}

func (r rule) match(title string) bool {
	if r.prefix != "" {
		has := strings.Contains(strings.ToLower(title), r.prefix)
		if r.negate == has { // 要求不含却含了 / 要求含却没有
			return false
		}
	}
	return r.pattern.MatchString(title)
}

func matchesAll(rules []rule, title string) bool {
	for _, r := range rules {
		if !r.match(title) {
			return false
		}
	}
	return true
}

func matchesAny(rules []rule, title string) bool {
	for _, r := range rules {
		if r.match(title) {
			return true
		}
	}
	return false
}

// CleanFileTitle 从 RSS 条目标题里提取"番名"，用于重命名。
// 例："[ANi] Dan Da Dan S02 /  胆大党 第二季 - 24 [1080P][Baha]..." -> "胆大党 第二季"
func CleanFileTitle(title, subgroup, fallback string) string {
	t := strings.TrimSpace(title)
	// 去掉所有 [xxx] 标签
	t = reTagSpan.ReplaceAllString(t, " ")
	// 去掉集数尾巴 " - 24"
	if i := strings.LastIndex(t, " - "); i > 0 {
		tail := strings.TrimSpace(t[i+3:])
		if isDigits(strings.Fields(tail + " ")[0]) || tail == "" {
			t = t[:i]
		}
	}
	t = strings.TrimSpace(t)
	// 多语言标题用 / 分隔，优先取含中文的一段
	if parts := strings.Split(t, "/"); len(parts) > 1 {
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if containsHan(p) && !isMostlyLatin(p) {
				t = p
				break
			}
		}
		if t == strings.TrimSpace(title) {
			t = strings.TrimSpace(parts[0])
		}
	}
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		return fallback
	}
	return t
}

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func isMostlyLatin(s string) bool {
	han, latin := 0, 0
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			han++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			latin++
		}
	}
	return latin > han
}

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// cleanRename 清掉渲染结果里的空占位残留。
// 例："[] 番名 S01E01" → "番名 S01E01"
//
//	"[] 番名 S01E01"（中文括号同理）
func cleanRename(s string) string {
	s = strings.TrimSpace(s)
	// 开头的空方括号/圆括号/全角括号
	for _, pair := range [][2]string{{"[]", ""}, {"()", ""}, {"【】", ""}, {"［］", ""}} {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	// 替换后可能出现连续空格，规范化
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

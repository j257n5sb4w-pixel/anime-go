package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ============================================================
//  订阅 ↔ qBittorrent 任务的精确匹配
//
//  这是整个程序最危险的一段逻辑：匹配结果会用于删除。
//  因此匹配条件必须**极其保守**——宁可漏删，绝不多删。
//
//  ⚠️⚠️ 最重要的一条：必须"路径相等"或"路径指向本番目录"，
//  绝不能只用「仓库根目录 + 字幕组」——那样所有 ANi 的番会互相命中！
//  （2026-10-26 实测踩过这个坑：10 条订阅全都匹配到同一条任务）
//
//  匹配规则（A 且 B）：
//   A. 路径属于本订阅：
//        A1. save_path 归一化后与订阅渲染路径完全相等，或
//        A2. save_path 的最后两级目录名包含订阅番名（用户改过目录名的情况）
//   B. 名称归属本订阅（二选一）：
//        B1. 配了 subgroup → 任务名含该字幕组
//        B2. 没配 subgroup → 任务名含番名
//  另外，B1 失败但任务名含番名时也放行（字幕组名与标题不一致的兜底）。
//
//  为什么字幕组优先：同一番的中译名与 mikan 正式名常不一致
//  （订阅叫「某暗部的少女共栖」，资源写「暗部共生少女」），
//  纯按番名会漏。但字幕组必须配合路径校验，否则会跨番误杀。
// ============================================================

// MatchedTask 是一对确定的"该订阅的任务"
type MatchedTask struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	SavePath string  `json:"save_path"`
	Size     int64   `json:"size"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
}

// normPath 归一化路径：去掉末尾斜杠、合并重复斜杠、消解 "." 与 ".."。
//
// 必须做完整 Clean，不能只合并 "//"：
// qBittorrent 自己会对 savepath 做 filepath.Clean，所以它返回的 save_path
// 已是清理后的形式；而本程序从模板渲染出来的路径可能含 "../" 或 "./"。
// 两边不对齐，MatchTasks 就会拿 "带 ../ 的模板结果" 去比 "清理后的真实路径"，
// 永远匹配不上——删除订阅时文件残留就是这类问题的典型表现。
//
// 实现直接复用 security 侧的 cleanSavePath，保证"校验用的路径"与
// "匹配用的路径"是同一套规范化规则，不再各自实现一份。
func normPath(p string) string {
	return cleanSavePath(p)
}

// MatchTasks 找出属于该订阅的 qBittorrent 任务。
// 匹配条件苛刻，用于删除前的安全筛选。
func MatchTasks(sub *Subscription, tasks []Torrent) []MatchedTask {
	if sub == nil {
		return nil
	}

	// ⚠️ 必须用全局统一渲染函数，绝不能自己拼变量。
	// 历史上这里手写的变量缺 filetitle/ep，导致匹配路径与实际写入路径不一致。
	// ep 传 0：save_path 模板里几乎不用 ep，用 0 渲染不影响目录。
	wantPath := RenderSavePath(sub, 0)
	if wantPath == "" {
		return nil // 路径算不出来就一个都不匹配
	}

	fileTitle := strings.TrimSpace(SubVars(sub, 0)["filetitle"])

	subgroup := strings.TrimSpace(sub.Subgroup)
	titleStem := stripYear(sub.Title) // 去掉 (2026) 的番名，用于宽松比对目录名

	// 目录名里出现本番番名的位置，必须"到词尾为止"。
	// 反例（2026-10-26 实测）：订阅「魔法★探险家 (2026)」会认领
	// 「魔法★探险家 第二季 (2026)/Season 1」里的任务——因为
	// strings.Contains 命中了更长的季名，删第一季会连第二季一起删。
	wantSeg := lastTitleSegment(wantPath)

	var out []MatchedTask
	for _, t := range tasks {
		tp := normPath(t.SavePath)

		// 条件 A1：保存路径与本订阅渲染路径完全相等
		// → 这是最强信号：目录就是本程序按模板建出来的，直接认领，不再看任务名。
		//   （任务名是 RSS 原始标题，常与番名繁简不同、带空格，
		//     如订阅「FX战士久留美」vs 资源「[ANi] FX 戰士久留美」，
		//     用名称匹配必然失败，2026-10-26 踩坑）
		if tp == wantPath {
			out = append(out, MatchedTask{
				Hash: t.Hash, Name: t.Name, SavePath: t.SavePath,
				Size: t.Size, State: t.State, Progress: t.Progress,
			})
			continue
		}

		// 条件 A2：路径不在预期位置，但目录名就是本番的目录
		// → 用户可能手动改过文件夹名（改了父目录、挪了位置）。
		//
		// 这个分支直接决定"删订阅时会不会连带删掉别的番"，所以判据从严：
		// 只认"同名目录"，不认"名字里包含"。
		// 反例（修复前实测）：订阅「魔法 (2026)」会把
		// 「魔法少女小圆 (2011)/Season 1」认领走——裸子串匹配的必然结果。
		seg := lastTitleSegment(tp)

		// 同名不同季的拦截必须在"同名判定"之前：
		// 「某番」与「某番 第二季」去季后缀后相同，若不先拦，
		// 删第一季会连第二季一起删。
		if seg == "" || wantSeg == "" || (seg != wantSeg && isSeasonSibling(seg, wantSeg)) {
			continue
		}

		if !sameAnimeSegment(seg, titleStem) {
			continue
		}

		// 条件 B：任务名也要能归属本订阅（双保险）
		nameOK := false
		if subgroup != "" && containsFold(t.Name, subgroup) {
			nameOK = true
		} else if fileTitle != "" && containsFold(t.Name, fileTitle) {
			nameOK = true
		} else if containsFold(t.Name, titleStem) {
			nameOK = true
		}
		if !nameOK {
			continue
		}

		out = append(out, MatchedTask{
			Hash:     t.Hash,
			Name:     t.Name,
			SavePath: t.SavePath,
			Size:     t.Size,
			State:    t.State,
			Progress: t.Progress,
		})
	}
	return out
}

// lastTitleSegment 取保存路径里"番名那一级"的目录名。
// /downloads/anime-go/某番 (2026)/Season 1 → "某番 (2026)"
func lastTitleSegment(p string) string {
	p = normPath(p)
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	// 倒数第二级一般是番名目录，倒数第一级是 Season N
	return parts[len(parts)-2]
}

// containsSegment 判断 haystack 的某一级目录名是否"以 needle 开头或结尾"，
// 而不是任意位置的子串。
//
// 注意：这是较宽松的辅助函数，仅供展示/提示类逻辑使用。
// 「删订阅时该认领哪些任务」请用 sameAnimeSegment（严格同名判定），
// 否则「魔法」会吃掉「魔法少女小圆」。
func containsSegment(haystack, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	nh := normalizeName(haystack)
	nn := normalizeName(needle)
	if nn == "" {
		return false
	}
	for _, seg := range strings.Split(nh, "/") {
		if seg == "" {
			continue
		}
		if seg == nn || strings.HasPrefix(seg, nn) || strings.HasSuffix(seg, nn) {
			return true
		}
	}
	return false
}

// sameAnimeSegment 判断目录名 seg 是否就是番名 stem 对应的那个目录。
//
// 判定：两边各取"核心标题"后要求**完全相等**。核心标题 =
//
//	去掉末尾 (YYYY) 之后的一切 → 繁转简 → 去标点空格 → 去季后缀。
//
// 为什么不能只用 strings.Contains（修复前就是这么写的）：
//
//	「魔法」是「魔法少女小圆」的前缀，子串判定会把两部完全不同的番关联起来，
//	删一个订阅就连带删掉别人的文件——这是最严重的一类事故（2026-10-26 实测可复现）。
//
// 为什么允许 (YYYY) 之后还有内容：
//
//	用户手动改目录时常在后面加批注，如「某暗部的少女共栖 (2026) 追更中」。
//	年份之后的部分不属于番名，截断即可；年份之前的部分仍要求严格相等，
//	所以「魔法少女小圆 (2011)」不会被「魔法 (2026)」认领。
func sameAnimeSegment(seg, stem string) bool {
	a := coreTitle(seg)
	b := coreTitle(stem)
	if a == "" || b == "" {
		return false
	}
	return a == b
}

// coreTitle 截到第一个年份标记为止，再归一化成可比较的核心标题。
//
//	"某暗部的少女共栖 (2026) 追更中" → "某暗部的少女共栖"
//	"某番 第二季 (2026)"            → "某番"
//	"魔法少女小圆 (2011)"           → "魔法少女小圆"
func coreTitle(s string) string {
	if cut, ok := cutAtYear(s); ok {
		s = cut
	}
	n := normalizeName(s)
	n = stripArabicYearSuffix(n) // 兜底：年份没带括号时也要剥掉
	n = stripSeasonSuffix(n)
	return n
}

// cutAtYear 找到 "(YYYY)" 或 "（YYYY）" 的结尾位置，返回其之前的部分。
// 同时兼容裸年份 "某番 2026"。
func cutAtYear(s string) (string, bool) {
	runes := []rune(s)
	for i := 0; i+4 < len(runes); i++ {
		// 检查 runes[i:i+4] 是否 4 位数字且首位非 0
		if runes[i] < '1' || runes[i] > '9' {
			continue
		}
		allDigit := true
		for k := 0; k < 4; k++ {
			if runes[i+k] < '0' || runes[i+k] > '9' {
				allDigit = false
				break
			}
		}
		if !allDigit {
			continue
		}
		// 年份后面必须不是数字（避免把 20261 之类的长数字切开）
		if i+4 < len(runes) && runes[i+4] >= '0' && runes[i+4] <= '9' {
			continue
		}
		return strings.TrimSpace(strings.TrimRight(string(runes[:i]), " (（[【")), true
	}
	return s, false
}

// normalizeTitleForMatch 已由 coreTitle 取代（coreTitle 会先按年份截断，
// 比"剥末尾年份"更准确：「某番 2026 追更中」这种也能处理）。
// 保留函数名是为了兼容可能的既有调用点，内部直接转调 coreTitle。
func normalizeTitleForMatch(s string) string {
	return coreTitle(s)
}

// seasonSMatch 匹配"第 N 季"的短写法 s2/s3/…/s12（归一化后已无空格）。
var seasonSMatch = regexp.MustCompile(`(?i)s\d{1,2}$`)

// stripArabicYearSuffix 去掉结尾的 4 位年份（归一化后括号已没了）："某番2026" → "某番"
func stripArabicYearSuffix(s string) string {
	for len(s) > 4 {
		tail := s[len(s)-4:]
		if tail[0] >= '1' && tail[0] <= '9' &&
			tail[1] >= '0' && tail[1] <= '9' &&
			tail[2] >= '0' && tail[2] <= '9' &&
			tail[3] >= '0' && tail[3] <= '9' {
			s = s[:len(s)-4]
			continue
		}
		break
	}
	return strings.TrimSpace(s)
}

// seasonSuffixes 番剧名里表示"第 N 季"的常见写法。
// 用于识别「X」与「X 第二季」这种只差季后缀的兄弟关系。
//
// ⚠ 注意 normalizeName 会先把空格、括号、标点全部去掉，
// 所以这里的条目必须写成**归一化之后**的形式（"season 2" → "season2"）。
// 中文数字与阿拉伯数字两套写法都要有：字幕组两种都用。
var seasonSuffixes = []string{
	"第一季", "第二季", "第三季", "第四季", "第五季", "第六季", "第七季", "第八季",
	"第一期", "第二期", "第三期", "第四期", "第五期", "第六期", "第七期", "第八期",
	"第1季", "第2季", "第3季", "第4季", "第5季", "第6季", "第7季", "第8季",
	"第1期", "第2期", "第3期", "第4期", "第5期", "第6期", "第7期", "第8期",
	"season1", "season2", "season3", "season4", "season5", "season6",
	"2ndseason", "3rdseason",
	"ii", "iii", "iv", "v",
}

// normSeasonKey 把番名归一化成"用于判断是否同一部番不同季"的键。
//
// 关键点：必须先剥年份、再剥季后缀。
// 反例：「魔法★探险家 第二季 (2026)」若先 normalizeName 会得到
// "魔法探险家第二季2026"——年份跟在最后，"第二季"不再是后缀，
// stripSeasonSuffix 自然剥不掉，于是 isSeasonSibling 判定失败（2026-10-26 实测）。
func normSeasonKey(s string) string {
	s = stripYear(s)             // 去掉 " (2026)"
	s = normalizeName(s)         // 繁转简 + 去标点空格 + 小写
	s = stripArabicYearSuffix(s) // 去掉归一化后残留的 "2026"
	return stripSeasonSuffix(s)
}

// stripSeasonSuffix 去掉名字末尾的季后缀，返回"基础番名"。
// 入参必须是已 normalizeName 过的串。
func stripSeasonSuffix(s string) string {
	n := normalizeName(s)
	if n == "" {
		return ""
	}
	changed := true
	for changed {
		changed = false
		// 长的后缀优先匹配，避免 "第二季" 被短后缀截断
		for _, suf := range seasonSuffixes {
			ns := normalizeName(suf)
			if ns == "" || len(n) <= len(ns) {
				continue
			}
			if strings.HasSuffix(n, ns) {
				n = strings.TrimSuffix(n, ns)
				changed = true
				break
			}
		}
		if changed {
			continue
		}
		// "s2"/"s12" 这类写法长度不固定，用正则统一处理：
		// 归一化后 "某番 S2" → "某番s2"，列表匹配不到，必须走这里。
		if m := seasonSMatch.FindStringIndex(n); m != nil && m[1] == len(n) && m[0] > 0 {
			n = n[:m[0]]
			changed = true
		}
	}
	return strings.TrimSpace(n)
}

// isSeasonSibling 判断两个目录名是否"同一部番的不同季"。
// 例：「魔法★探险家 (2026)」vs「魔法★探险家 第二季 (2026)」→ true
//
//	「某暗部的少女共栖 (2026)」vs「某暗部的少女共栖 (2026)」→ false
func isSeasonSibling(a, b string) bool {
	na, nb := normalizeName(stripYear(a)), normalizeName(stripYear(b))
	if na == nb {
		return false
	}
	ba, bb := normSeasonKey(a), normSeasonKey(b)
	// 任一剥完后为空说明整个名字就是季后缀，无法判断，保守返回 false
	if ba == "" || bb == "" {
		return false
	}
	// 去季后缀后相同 → 是同一部番的不同季，不是本订阅的任务
	return ba == bb
}

// DescribeTasks 生成人类可读的任务清单（删除前展示给用户确认）
func DescribeTasks(ts []MatchedTask) string {
	if len(ts) == 0 {
		return "（没有匹配到任何 qBittorrent 任务）"
	}
	var b strings.Builder
	var total int64
	for i, t := range ts {
		if i >= 20 {
			fmt.Fprintf(&b, "... 还有 %d 个\n", len(ts)-20)
			break
		}
		fmt.Fprintf(&b, "• %s\n  %.0f%% · %s · %s\n",
			t.Name, t.Progress*100, humanSize(t.Size), t.State)
		total += t.Size
	}
	fmt.Fprintf(&b, "\n共 %d 个任务，合计 %s", len(ts), humanSize(total))
	return b.String()
}

func humanSize(n int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/GB)
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/MB)
	case n >= KB:
		return fmt.Sprintf("%.0f KB", float64(n)/KB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// containsFold 判断 s 是否包含 sub，忽略：
//   - 繁体/简体差异（戰/战、術/术 等常用异体）
//   - 空格与常见分隔符
//
// 用于任务名与番名的宽松比对（任务名是 RSS 原始标题，繁简常不一致）。
func containsFold(s, sub string) bool {
	if sub == "" {
		return false
	}
	ns, nsub := normalizeName(s), normalizeName(sub)
	return strings.Contains(ns, nsub)
}

// normalizeName 归一化名称：繁转简 + 去空格/标点 + 小写
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		// 繁体常用字 → 简体（只覆盖番剧名里高频的那批）
		if v, ok := t2sMap[r]; ok {
			r = v
		}
		// 丢弃空格、常见标点、括号
		switch r {
		case ' ', '\t', '　', '-', '_', '・', '·', '。', '，', ',', '.', '!', '！', '?', '？',
			'（', '）', '(', ')', '[', ']', '【', '】', '★', '☆':
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// t2sMap 繁体→简体高频字表（番剧名里常见的那些）。
// 注意：Go 的 map 字面量不允许重复 key，加字前先确认没写过。
var t2sMap = map[rune]rune{
	'戰': '战', '術': '术', '學': '学', '園': '园', '國': '国', '語': '语', '車': '车',
	'東': '东', '馬': '马', '鳥': '鸟', '魚': '鱼', '龍': '龙', '鳳': '凤', '雲': '云',
	'電': '电', '話': '话', '開': '开', '關': '关', '門': '门', '間': '间', '實': '实',
	'驗': '验', '體': '体', '愛': '爱', '戀': '恋', '們': '们', '個': '个', '這': '这',
	'會': '会', '來': '来', '時': '时', '對': '对', '為': '为', '無': '无', '異': '异',
	'轉': '转', '髮': '发', '裡': '里', '麼': '么', '樣': '样', '總': '总', '統': '统',
	'隊': '队', '級': '级', '給': '给', '與': '与', '從': '从', '沒': '没', '嗎': '吗',
	'後': '后', '種': '种', '現': '现', '發': '发', '動': '动', '機': '机', '鬥': '斗',
	'華': '华', '麗': '丽', '雙': '双', '劍': '剑', '聖': '圣', '騎': '骑', '絕': '绝',
	'樂': '乐', '聲': '声', '優': '优', '劇': '剧', '場': '场', '館': '馆', '書': '书',
	'畫': '画', '筆': '笔', '記': '记', '錄': '录', '號': '号', '數': '数', '據': '据',
	'網': '网', '線': '线', '連': '连', '續': '续', '結': '结', '終': '终', '於': '于',
	'還': '还', '過': '过', '進': '进', '運': '运', '達': '达', '遠': '远', '邊': '边',
	'點': '点', '熱': '热', '燈': '灯', '營': '营', '業': '业', '員': '员', '單': '单',
	'價': '价', '錢': '钱', '買': '买', '賣': '卖', '銀': '银', '鐵': '铁', '鋼': '钢',
	'練': '练', '習': '习', '題': '题', '問': '问', '認': '认', '識': '识', '覺': '觉',
	'經': '经', '歷': '历', '險': '险', '難': '难', '頭': '头', '臉': '脸', '腳': '脚',
	'願': '愿', '夢': '梦', '憶': '忆', '獵': '猎', '獸': '兽', '惡': '恶', '靈': '灵',
	'詛': '诅', '驅': '驱', '師': '师', '職': '职', '團': '团', '夥': '伙', '侶': '侣',
	'妳': '你', '祂': '他', '牠': '它', '誰': '谁', '歲': '岁', '萬': '万', '億': '亿',
	'讓': '让', '張': '张', '將': '将', '軍': '军', '紅': '红', '綠': '绿', '藍': '蓝',
	'黃': '黄', '驚': '惊', '嚇': '吓', '屍': '尸', '處': '处', '獨': '独',
}

// repoRootOf 从渲染出的保存路径里推出"仓库根目录"。
// 例：/downloads/anime-go/某番 (2026)/Season 1 → /downloads/anime-go
// 这样即使用户手动把番剧文件夹改了名，也能在本仓库范围内兜底匹配。
func repoRootOf(savePath string) string {
	p := normPath(savePath)
	parts := strings.Split(p, "/")
	// 至少要有 /a/b/c 三段才有意义
	if len(parts) < 4 {
		return ""
	}
	// 去掉最后两段（番名文件夹 + Season N）
	root := strings.Join(parts[:len(parts)-2], "/")
	if root == "" || root == "/" {
		return ""
	}
	return root
}

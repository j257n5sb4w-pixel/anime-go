package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// reSeasonShortSuffix 匹配番名末尾的短季写法：「某番 S2」「某番 2期」。
//
// 与 tmdb.go 的 reSeasonSuffix 互补：那个管「第二季 / Season 2 / 2期」里
// **带「第」** 的写法，实际片名里「某番 2期」「某番 S2」也很常见，
// 这里补齐，避免多季落进两个平行目录。
// 必须到串尾，避免把「S2」当普通词误剥。
var reSeasonShortSuffix = regexp.MustCompile(`(?i)\s*(?:(?:s|season)\s*\d{1,2}|\d{1,2}\s*期)\s*$`)

// ============================================================
//  多季番剧的「同一个番名目录」解析
//
//  需求（用户 2026-10-26/27）：
//    同一部番的第 1 季、第 2 季要落在**同一个番名目录**下，
//    各自建 Season 1 / Season 2 子目录，而不是
//    「某番」+「某番 第二季」两个平行目录。
//
//    /media/新番追更/某番/Season 1
//    /media/新番追更/某番/Season 2      ← 第 2 季也进「某番」
//
//  顺序必须无关：先订第 2 季、后订第 1 季，两个 Season 目录都要正确落位。
//  目录还可能**不是本程序建的**（用户手动放的），所以必须真的去看磁盘。
// ============================================================

// DirNameOf 返回该订阅应当落盘的番名目录名（**纯函数，不碰磁盘**）。
//
// 优先级：
//  1. sub.DirName —— 订阅创建/编辑时已经解析好并固化的结果；
//  2. 否则从 Title 剥掉季后缀现算一个。
//
// ⚠ 必须是纯函数：SubVars 会在 check.go / cleanup.go / match_tasks.go
// 等处的锁内被调用，里面做磁盘 IO 会把整个程序拖住。
// 想拿「磁盘上真实的同名目录名」请用 ResolveDirName。
func DirNameOf(sub *Subscription) string {
	if sub == nil {
		return ""
	}
	if d := strings.TrimSpace(sub.DirName); d != "" {
		return d
	}
	return baseAnimeDirName(sub.Title)
}

// baseAnimeDirName 从订阅名里剥掉「第二季 / Season 2 / 2期 / S2」这类后缀，
// 以及尾部年份，得到目录名。
//
// 例：「魔法★探险家 第二季 (2026)」→「魔法★探险家」
//
//	「胆大党 (2026)」          →「胆大党」
//	「转生就是剑 第二季」       →「转生就是剑」
func baseAnimeDirName(title string) string {
	t := strings.TrimSpace(stripYear(title))
	if t == "" {
		return strings.TrimSpace(title)
	}
	if base := stripSeasonSuffixLoose(t); base != "" {
		return base
	}
	return t
}

// stripSeasonSuffixLoose 比 match_tasks.go 的 stripSeasonSuffix 宽松一档：
// 它接受**未**经 normalizeName 的原始字符串，返回时保留原始大小写与空格。
//
// 为什么不能直接复用那个：那个函数注释里明确写了「入参必须是已 normalizeName
// 过的串」，传原始串进去会因为去不掉空格而匹配失败（「第二季」带前导空格）。
func stripSeasonSuffixLoose(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 反复剥，处理「某番 第二季 第2期」这种叠加写法
	for i := 0; i < 4; i++ {
		prev := s
		s = strings.TrimSpace(reSeasonSuffix.ReplaceAllString(s, ""))
		s = strings.TrimSpace(reSeasonShortSuffix.ReplaceAllString(s, ""))
		if s == prev || s == "" {
			break
		}
	}
	return strings.TrimSpace(s)
}

// ResolveDirName 解析该订阅实际要用的番名目录名。
//
// 逻辑（用户需求核心）：
//  1. 去追更库根目录下列出已有子目录；
//  2. 若有目录与「按番名剥季后缀所得的名字」是同一部番 → **复用那个目录名**
//     （哪怕是用户手动建的、带年份的、带自己习惯写法的）；
//  3. 没有 → 用剥过季后缀的番名当新目录名。
//
// 这样「先订第 2 季、后订第 1 季」也能收敛到同一个目录：
// 第 2 季先订时没有同名目录 → 建「某番」；第 1 季后订时扫到「某番」
// 与本季剥后同名 → 复用「某番」，两个 Season 目录自然并列。
//
// ⚠ 调用方必须持有 a.mu（会读 a.cfg），且注意有磁盘 IO（读根目录，
//
//	最多几百个条目，不做递归）。
func (a *App) ResolveDirName(sub *Subscription) string {
	base := baseAnimeDirName(sub.Title)
	if base == "" {
		return strings.TrimSpace(sub.Title)
	}
	// ⚠ LibraryRootPath 返回的是 **qBittorrent 侧** 路径（/media/新番追更），
	// 不是本机挂载点（/mnt/media/新番追更）。直接 os.ReadDir 会永远 ENOENT，
	// 于是「复用同名目录」静默失效、每次都新建一个 —— 正是这个需求要解决的问题。
	existing, err := listSubdirs(a.libraryRootLocal())
	if err != nil || len(existing) == 0 {
		return base // 目录还没建（首次订阅）或不可读：直接用算出来的名字
	}
	if hit := pickSameAnimeDir(base, existing); hit != "" {
		return hit
	}
	return base
}

// libraryRootLocal 把追更库根目录映射到本机挂载点。
//
// 映射不到时退回原始路径（用户可能把 LibraryRoot 直接填成本机路径，
// 例如在板子上本地跑、没有 cifs 挂载的场景）。
func (a *App) libraryRootLocal() string {
	root := a.cfg.LibraryRootPath()
	if local, ok := a.qbPathToLocal(root); ok {
		return local
	}
	return root
}

// LibraryRootPath 返回追更库根目录（qBittorrent 侧路径）。
func (c *Config) LibraryRootPath() string {
	if c != nil {
		if s := strings.TrimSpace(c.LibraryRoot); s != "" {
			return strings.TrimRight(s, "/")
		}
	}
	return defaultLibraryRoot
}

// listSubdirs 列出 dir 下的一级子目录名。目录不存在时返回 (nil, nil)。
func listSubdirs(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "#") {
			continue // 跳过 .fg.* 临时目录与 #recycle 之类
		}
		out = append(out, n)
	}
	return out, nil
}

// pickSameAnimeDir 在已有目录里挑出「与 base 是同一部番」的那个。
//
// 判据（从严到宽，命中即返回）：
//  1. 完全相同；
//  2. 双向 isSeasonSibling —— 覆盖「已有目录带季名、base 不带」
//     与「base 带季名、已有目录不带」两种方向；
//  3. 去季后缀 + 去年份后完全相同。
//
// 不做子串匹配：「魔法」不能认领「魔法少女小圆」（历史上踩过的坑）。
func pickSameAnimeDir(base string, existing []string) string {
	nb := normSeasonKey(base)
	for _, d := range existing {
		if d == base {
			return d
		}
		if isSeasonSibling(d, base) {
			return d
		}
		if nb != "" && normSeasonKey(d) == nb {
			return d
		}
	}
	return ""
}

// EnsureSeasonDirs 确保 <root>/<dirName>/Season N 这一整套目录存在，
// 并返回 qBittorrent 侧的最终目录路径。
//
// 为什么要主动建目录而不是交给 qB：
// qB 收到 add 带 savepath 时会自己建，但它建完我们在本机（cifs 挂载点）
// 也能看到——这中间有延迟，紧接着的 renameFile 可能打到还没出现的目录上。
// 先在本机建好，删除订阅时的 removeSubDirs 也才有东西可删。
//
// localRoot 是根目录在本机的挂载路径；拿不到映射时直接跳过（返回 qbitDir）。
func (a *App) EnsureSeasonDirs(qbitDir string) error {
	local, ok := a.qbPathToLocal(qbitDir)
	if !ok {
		return nil // 没有挂载映射：本机看不见，交给 qB 自己建
	}
	return os.MkdirAll(local, 0o755)
}

// seasonDirOf 从 save_path 模板里剥出「Season N」这一级的名字，
// 用于「同番多季」时给用户提示。拿不到就返回空。
func seasonDirOf(p string) string {
	p = normPath(p)
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if strings.HasPrefix(strings.ToLower(last), "season") {
		return last
	}
	return ""
}

// GuessSeasonFromTitle 从番名里猜季度数字：认出「第二季/Season 2/2期」返回 2，
// 认不出返回 0（调用方决定默认值）。
//
// 复用 tmdb.go 的 seasonInTitle：它对「第贝塔季」这种奇葩命名也只会返回 0，
// 所以界面上必须允许手动改（用户明确要求）。
func GuessSeasonFromTitle(title string) int {
	return seasonInTitle(title)
}

// joinQbitPath 拼接 qBittorrent 侧路径，统一用 / 且去掉重复斜杠。
func joinQbitPath(parts ...string) string {
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), "/")
		if p != "" {
			segs = append(segs, p)
		}
	}
	return "/" + strings.Join(segs, "/")
}

// filepathAbs 是 filepath.Abs 的薄封装，只为让调用点读起来短一点。
func filepathAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

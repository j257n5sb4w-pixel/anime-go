package main

import (
	"fmt"
	"path"
	"strings"
)

// ============================================================
//  安全约束（务必修读，任何改动不得绕过）
//
//  1) 【绝不 deleteFiles=true】
//     /torrents/delete 带 deleteFiles=true 会立即 unlink 源文件，
//     不经 UGOS 回收站，ext4 下无法恢复。本程序**不提供**删除任务的
//     任何功能，更没有这个调用。清理任务请人工在 WebUI 操作。
//
//  2) 【写入路径白名单】
//     默认只允许写到 AllowPrefixes 里列出的前缀之下。
//     生产库（如 /media/新番追更）必须由用户在 config.json 的
//     allow_write_prefixes 里显式声明，否则该条订阅会被拒绝推送。
//     这样即使配置写错，也不会误写到正式库。
//
//  3) 【首次运行只建基线】
//     新订阅第一次抓取只记录已有条目、不推送，避免一上线就下整季历史。
//     ⚠ 删 state.json 重建基线前必须先 systemctl stop，否则退出时会
//       把内存里的旧状态回写，导致基线失效、历史被全量重推。
//
//  4) 【同集去重】
//     同一集只推一个版本（简中内嵌优先），避免重复下载。
// ============================================================

// cleanSavePath 把用户输入的路径归一化成"真正会落盘的位置"，用于安全校验。
//
// 为什么要先 Clean 再判前缀：
//
//	qBittorrent 收到 savepath 后会自己 filepath.Clean（折叠 ".."、"//"、"."），
//	所以校验时必须按**清理后**的路径来判断，否则
//	"/downloads/anime-go/../../media/新番追更/x" 会骗过前缀检查，
//	而 qBittorrent 实际写到 /media/新番追更/x（2026-10-26 实测可复现）。
//
// 另外把反斜杠统一成 "/"：Windows 风格分隔符在 qB 侧同样会被当作路径分隔符。
func cleanSavePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	// path.Clean 会消解 ".."、"//"、"." 并去掉尾斜杠；
	// 结果若逃出根（如 "/../etc" → "/etc"），Clean 会把它夹回根，符合预期。
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	return p
}

// hasEncodedSeparator 判断路径里是否含百分号编码的路径分隔符/点点。
//
// path.Clean 不懂百分号编码："%2e%2e" 在它眼里只是一个普通目录名，
// 但下游（qBittorrent 的 URL 表单、反向代理、某些文件系统 API）可能先解码再落盘，
// 于是 "%2e%2e/%2e%2e/media" 又能绕出去。这类路径一律拒绝，不做"猜下游会不会解码"的赌博。
func hasEncodedSeparator(p string) bool {
	low := strings.ToLower(p)
	for _, enc := range []string{"%2e", "%2f", "%5c", "%00"} {
		if strings.Contains(low, enc) {
			return true
		}
	}
	return false
}

// isUnderPrefix 判断目录 p 是否位于前缀 prefix 之下（含相等）。
//
// 用"整段比较"而不是裸 HasPrefix：否则前缀 "/downloads/anime-go"（用户手写时
// 常漏掉尾斜杠）会误放行兄弟目录 "/downloads/anime-go-secrets"。
func isUnderPrefix(p, prefix string) bool {
	prefix = cleanSavePath(prefix)
	if prefix == "" {
		return false
	}
	if p == prefix {
		return true
	}
	if prefix == "/" {
		return strings.HasPrefix(p, "/")
	}
	return strings.HasPrefix(p, prefix+"/")
}

// ValidateSavePath 检查目标路径是否在允许的前缀列表内。
// prefixes 为空时使用内置默认（只允许测试隔离目录）。
//
// ⚠ 先把路径 Clean 再判断：调用方传进来的可能是含 ".." 的模板渲染结果。
func ValidateSavePath(p string, prefixes []string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("save_path 为空")
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("save_path 必须是绝对路径: %s", p)
	}
	// 含 NUL 的路径一定有问题（Go 字符串可以带 NUL，
	// 但传给 qBittorrent 的 form 字段里出现 NUL 属于注入面）
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("save_path 含非法字符 NUL: %q", p)
	}
	// 百分号编码的 "../"、"/"、"\"、"NUL"：path.Clean 无法消解，
	// 下游一旦解码就能绕出白名单，直接拒绝。
	if hasEncodedSeparator(p) {
		return fmt.Errorf("save_path 含非法百分号编码序列: %q", p)
	}

	clean := cleanSavePath(p)

	// 明确禁止的正式库前缀（即使有人写进 allow 列表也拦下，
	// 必须人工改这里的白名单才放行）
	//
	// 2026-10-26 变更：用户要求把「追更」的落盘位置正式迁到
	// /media/新番追更（qB 容器同时挂了 /volume1/dl→/downloads 与
	// /volume1/media→/media），因此这里**解除**了对该目录的全局禁令——
	// 它的保护改由 allow 前缀（必须显式写成 /media/新番追更/）承担。
	// 「剧场版」是不归追更管的正式库，仍然一律拒绝。
	denyPrefixes := []string{
		"/media/剧场版",
	}

	allowed := prefixes
	if len(allowed) == 0 {
		allowed = DefaultAllowPrefixes()
	}
	for _, a := range allowed {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !isUnderPrefix(clean, a) {
			continue
		}
		// 命中允许前缀后，再检查是否落在 deny 列表里
		for _, d := range denyPrefixes {
			if isUnderPrefix(clean, d) {
				return fmt.Errorf("拒绝写入正式库路径 %s（需在代码白名单里显式解除）", clean)
			}
		}
		return nil
	}
	return fmt.Errorf("save_path %s 不在允许的前缀 %v 内，已拒绝推送", clean, allowed)
}

// DefaultAllowPrefixes 返回内置默认允许前缀。
//
// 只放「追更」目录：2026-10-26 起追更直接落到正式库
// /media/新番追更/，老的 /downloads/anime-go/ 不再默认放行
// （用户要保留老位置时，自己在设置的 allow 前缀里写出来）。
func DefaultAllowPrefixes() []string {
	return []string{defaultAllowPrefix}
}

package main

// 自定义 hosts（域名 → IP）支持。
//
// 背景：在板子（OrangePi Zero 2W，国内网络）实测 api.themoviedb.org 被解析到
// 199.16.156.75 / face:b00c::（Twitter / Facebook 网段，典型 DNS 污染），
// 连接必然超时。tmdb.go 里用「换域名 + 多域名回退」绕过了一次，
// 但污染名单是会变的（今天污染 A，明天可能换成 image.tmdb.org），
// 每次都改代码发新版显然不现实。
//
// 所以这里给一个「程序内的 hosts」：在设置 → 源数据里填「域名 = IP」，
// 拨号时对这些域名直接连指定 IP，绕开系统 DNS。
//
// 为什么不做成改 /etc/hosts：
//   - 要 root、要重载、要跟着系统升级走；
//   - 板子上跑 systemd、NAS 上跑容器，两个环境的 hosts 位置还不一样；
//   - 用户改完想立刻验证，改文件做不到「下一个请求就生效」。
//
// TLS 不受影响：我们只替换**拨号目标**，URL 里的 Host 不变，
// http.Transport 仍然按 URL 里的域名做 SNI 与证书校验。
// 换句话说，把 api.themoviedb.org 指到任意 IP，证书链照样是对得上的。

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// currentHostOverrides 是当前生效的覆盖表；nil / 空 = 全部走系统 DNS。
//
// 用 atomic.Pointer[map] 而不是 sync.RWMutex：拨号是每个连接都做的事，
// 读多写极少，原子读比加锁便宜；保存设置时整个 map 替换，不原地改。
var currentHostOverrides atomic.Pointer[map[string]string]

// normalizeHostOverrides 清理覆盖表：去空白、域名小写、校验 IP、
// 丢掉重复（同一个域名以第一条为准）。
//
// 为什么允许 IPv6：部分污染是「A 记录正常、AAAA 被投毒」，
// 把域名指到一个可用的 IPv6 地址反而更快。
func normalizeHostOverrides(in []HostOverride) []HostOverride {
	out := make([]HostOverride, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, h := range in {
		host := strings.ToLower(strings.Trim(strings.TrimSpace(h.Host), "."))
		ip := strings.Trim(strings.TrimSpace(h.IP), "[]")
		if host == "" || ip == "" {
			continue
		}
		// 域名里不允许出现空格与斜杠——填错时静静忽略比拨号时才炸好，
		// 但也不能悄悄吞掉：handleSettingsSave 会对非法项单独报错。
		if strings.ContainsAny(host, " /\\") {
			continue
		}
		if net.ParseIP(ip) == nil {
			continue
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, HostOverride{Host: host, IP: ip})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// validateHostOverrides 在保存设置时给出「哪一条写错了」的明确报错。
//
// normalizeHostOverrides 是静默丢弃（防配置炸掉），这里是保存前的严格校验
// ——两者都要有：前者保证坏配置不会让程序起不来，后者保证用户知道写错了。
func validateHostOverrides(in []HostOverride) error {
	var bad []string
	for _, h := range in {
		host := strings.TrimSpace(h.Host)
		ip := strings.Trim(strings.TrimSpace(h.IP), "[]")
		if host == "" && ip == "" {
			continue // 空行直接忽略
		}
		if host == "" {
			bad = append(bad, fmt.Sprintf("「%s」缺域名", ip))
			continue
		}
		if ip == "" {
			bad = append(bad, fmt.Sprintf("「%s」缺 IP", host))
			continue
		}
		if strings.ContainsAny(host, " /\\") {
			bad = append(bad, fmt.Sprintf("「%s」不是合法域名", host))
			continue
		}
		if net.ParseIP(ip) == nil {
			bad = append(bad, fmt.Sprintf("「%s → %s」的 IP 格式不对", host, ip))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("hosts 覆盖有误：%s", strings.Join(bad, "；"))
	}
	return nil
}

// ApplyHostOverrides 把覆盖表设为当前生效值（启动时、保存设置后各调一次）。
//
// 换表后主动断开空闲连接：旧连接还连着旧 IP，不主动断的话
// 用户改完 hosts 会「看起来没生效」，直到连接自己超时。
func ApplyHostOverrides(list []HostOverride) {
	norm := normalizeHostOverrides(list)
	if len(norm) == 0 {
		currentHostOverrides.Store(nil)
		sharedTransport.CloseIdleConnections()
		return
	}
	m := make(map[string]string, len(norm))
	for _, h := range norm {
		m[h.Host] = h.IP
	}
	currentHostOverrides.Store(&m)
	sharedTransport.CloseIdleConnections()
}

// overrideFor 查一个域名是否有覆盖（含「www.」之类的子域不强匹配：
// 只做精确匹配，避免一条 api.example.com → 1.2.3.4 影响别的子域）。
func overrideFor(host string) (string, bool) {
	m := currentHostOverrides.Load()
	if m == nil || len(*m) == 0 {
		return "", false
	}
	ip, ok := (*m)[strings.ToLower(strings.Trim(host, "[]"))]
	return ip, ok
}

// HostOverridesApplied 返回当前生效的覆盖条数（设置页展示用）。
func HostOverridesApplied() int {
	m := currentHostOverrides.Load()
	if m == nil {
		return 0
	}
	return len(*m)
}

// dialWithOverrides 是 sharedTransport 的 DialContext。
//
// 只有「有覆盖」的域名才走自定义解析；其余一律交给 net.Dialer
// （默认走系统 DNS + Happy Eyeballs），保持与标准库一致的行为。
func dialWithOverrides(d *net.Dialer, ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return d.DialContext(ctx, network, addr)
	}
	if ip, ok := overrideFor(host); ok {
		// 覆盖后只连这一个 IP：不走 Happy Eyeballs、不做回退。
		// 理由：用户填这条就是「我确定这个域名该连这里」，
		// 悄悄回退到被污染的 DNS 结果只会让人以为覆盖没生效。
		if strings.Contains(ip, ":") {
			addr = net.JoinHostPort("["+ip+"]", port)
		} else {
			addr = net.JoinHostPort(ip, port)
		}
	}
	return d.DialContext(ctx, network, addr)
}

// HostTestResult 是「测试 hosts 覆盖」的结果。
//
// 同时给出系统 DNS 的答案与覆盖后的实际连接结果：
// 用户因此能一眼看出「DNS 被污染成哪个 IP」以及「覆盖有没有救回来」。
type HostTestResult struct {
	OK        bool     `json:"ok"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	DNS       []string `json:"dns"`      // 系统 DNS 给出的地址
	Override  string   `json:"override"` // 当前生效的覆盖（空 = 没配）
	Connected string   `json:"connected"`
	LatencyMS int64    `json:"latency_ms"`
	Error     string   `json:"error"`
}

// TestHost 验证「这个域名能不能连通」，并报告覆盖前后的差异。
//
// 不保存任何东西，也不碰全局 Transport：设置页允许「先测再存」。
func TestHost(host string, port int, scheme string) HostTestResult {
	res := HostTestResult{Host: host, Port: port}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		res.Error = "请先填写域名"
		return res
	}
	if port <= 0 {
		port = 443
		if scheme == "http" {
			port = 80
		}
	}
	res.Port = port

	// 1) 系统 DNS 答案（这条最有诊断价值：被污染时这里就是坏 IP）
	if ips, err := net.LookupHost(host); err == nil {
		res.DNS = ips
	}

	// 2) 覆盖后的目标
	target := host
	if ip, ok := overrideFor(host); ok {
		res.Override = ip
		target = ip
	}
	addr := net.JoinHostPort(strings.Trim(target, "[]"), fmt.Sprint(port))

	start := time.Now()
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).Dial("tcp", addr)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	conn.Close()
	res.OK = true
	res.Connected = addr
	return res
}

// describeHostOverrides 把覆盖表说成一行话（日志用）。
func describeHostOverrides(list []HostOverride) string {
	norm := normalizeHostOverrides(list)
	if len(norm) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(norm))
	for _, h := range norm {
		parts = append(parts, h.Host+"="+h.IP)
	}
	return strings.Join(parts, "，")
}

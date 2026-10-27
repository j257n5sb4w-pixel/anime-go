package main

// 出站代理（设置 → 代理）
//
// 需求（用户 2026-10-27）：设置里能配代理的「协议、端口、账号密码」。
//
// 为什么做成「一个全局可切换的 Transport」，而不是每个 client 各建一个：
//   - 全程序有 5 个 http.Client（RSS、mikan 页面、mikan 封面、TMDB、qBittorrent），
//     请求分散在十几个函数里，其中很多函数（fetchFeed / fetchMikan / downloadCover）
//     根本拿不到 *Config；
//   - http.Transport.Proxy 是**每个请求**都会调用的回调，所以只要把「当前生效的
//     代理」放进原子变量，保存设置后立刻生效，不需要重启进程，也不需要把 cfg
//     沿着调用链一路传下去。
//
// 注意 Socks5：net/http 的 Transport 原生认 "socks5://" 的 Proxy URL（Go 1.9+），
// 所以 http / https / socks5 三种协议不需要各写一套拨号逻辑。
//
// ⚠ 已建立的 keep-alive 连接不会被换代理（Proxy 回调只在新建连接时用），
// 这是可接受的：改代理后最多等一个 IdleConnTimeout。
// 保存设置时还会主动 CloseIdleConnections，让切换立即生效。

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ProxySettings 是一份出站代理配置。
type ProxySettings struct {
	Enabled bool `json:"enabled"`
	// Protocol 只支持 http / https / socks5，空值按 http 处理。
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	// Username / Password 可选：http 代理走 Proxy-Authorization，
	// socks5 走用户名/密码认证（RFC 1929）。
	Username string `json:"username"`
	Password string `json:"password"`
}

// proxyProtocols 是协议下拉框的可选值（设置页也用这份）。
var proxyProtocols = []string{"http", "https", "socks5"}

// normalize 归一化：协议小写并落进白名单、主机去掉协议前缀与空白、端口夹进合法范围。
func (p *ProxySettings) normalize() {
	p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
	if p.Protocol == "" {
		p.Protocol = "http"
	}
	ok := false
	for _, x := range proxyProtocols {
		if p.Protocol == x {
			ok = true
			break
		}
	}
	if !ok {
		p.Protocol = "http"
	}
	h := strings.TrimSpace(p.Host)
	// 用户很可能直接把浏览器地址栏里的 "http://192.168.50.17" 粘进来，
	// 这里顺手剥掉协议与路径，只留主机（IPv6 的方括号保留）。
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	p.Host = strings.TrimSpace(h)
	if p.Port < 0 {
		p.Port = 0
	}
	if p.Port > 65535 {
		p.Port = 65535
	}
	p.Username = strings.TrimSpace(p.Username)
}

// URL 把设置拼成 http.Transport 认识的代理地址。
// Enabled=false 时返回 (nil, nil) —— 表示直连。
func (p ProxySettings) URL() (*url.URL, error) {
	if !p.Enabled {
		return nil, nil
	}
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return nil, fmt.Errorf("代理已启用，但代理主机为空")
	}
	if p.Port <= 0 || p.Port > 65535 {
		return nil, fmt.Errorf("代理端口不合法: %d", p.Port)
	}
	scheme := p.Protocol
	if scheme == "" {
		scheme = "http"
	}
	u := &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(p.Port)),
	}
	if p.Username != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u, nil
}

// Describe 返回「协议://主机:端口」这种可读描述，用于日志（不含密码）。
func (p ProxySettings) Describe() string {
	if !p.Enabled {
		return "直连"
	}
	return fmt.Sprintf("%s://%s:%d", p.Protocol, p.Host, p.Port)
}

// Validate 检查启用状态下的必填项。
//
// 关闭时永远通过：用户可能只是想临时停用代理，留着旧的地址端口
// 下次再开，没必要逼他清空。
func (p ProxySettings) Validate() error {
	if !p.Enabled {
		return nil
	}
	_, err := p.URL()
	return err
}

// currentProxy 保存当前生效的代理；nil = 直连。
var currentProxy atomic.Pointer[url.URL]

// proxyForRequest 是 Transport.Proxy 的实现：每个请求都会调用一次，
// 因此保存设置后新连接立刻走新代理。
//
// 内网 / 回环地址一律直连：qBittorrent 通常在局域网里（比如 192.168.50.17:8888），
// 把 LAN 请求丢给代理会多绕一跳，代理在远端时更是直接连不上。
// 代理配置的用途是访问 mikan / TMDB 这些公网站点，不是给局域网用的。
func proxyForRequest(req *http.Request) (*url.URL, error) {
	u := currentProxy.Load()
	if u == nil {
		return nil, nil
	}
	if isLocalHost(req.URL.Hostname()) {
		return nil, nil
	}
	return u, nil
}

// isLocalHost 判断主机名是否是「本机 / 局域网」地址。
//
// 只看字面量：域名不做 DNS 解析（解析会给每个请求加一次查询，
// 而且 *.local 之类的名字解析出来也未必是内网）。
func isLocalHost(host string) bool {
	if host == "" {
		return false
	}
	h := strings.Trim(strings.ToLower(host), "[]")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// sharedTransport 是所有出站请求共用的 Transport。
//
// 与默认 Transport 的区别只在于 Proxy 是动态的：其余参数照抄
// http.DefaultTransport 的量级，避免「换了 Transport 反而变慢/不复用连接」。
var sharedTransport = &http.Transport{
	Proxy: proxyForRequest,
	DialContext: (&net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          32,
	MaxIdleConnsPerHost:   4,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	// 部分代理（尤其自建 squid/nginx）对 TLS 的 SNI/ALPN 处理不标准，
	// 这里不强制 h2 之外的特殊配置，保持默认即可。
	TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
}

// newHTTPClient 建一个走共享 Transport 的 client。
// 所有出站请求都必须经过它，否则设置里的代理对该请求无效。
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: sharedTransport}
}

// ApplyProxy 把配置里的代理设为当前生效值（启动时、保存设置后各调一次）。
// 传 nil 或 Enabled=false 都是「直连」。
func ApplyProxy(p ProxySettings) error {
	u, err := p.URL()
	if err != nil {
		return err
	}
	currentProxy.Store(u)
	// 换代理后旧连接还挂在旧代理上，主动断开让下一个请求重新建连。
	sharedTransport.CloseIdleConnections()
	return nil
}

// CurrentProxy 返回当前生效的代理（nil = 直连），供 /api/status 展示。
func CurrentProxy() *url.URL { return currentProxy.Load() }

// ProxyTestResult 是一次「测试代理」的结果。
type ProxyTestResult struct {
	OK        bool   `json:"ok"`
	Proxy     string `json:"proxy"`
	Target    string `json:"target"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error"`
}

// TestProxy 用给定设置真实发一次请求，验证代理通不通。
//
// 特意不碰全局 Transport：设置页允许「先测再存」，
// 测试失败不能把正在跑的抓取任务带沟里。
// target 为空时测 mikan 站点（这是本程序最依赖的外部站）。
func TestProxy(p ProxySettings, target, ua string) ProxyTestResult {
	res := ProxyTestResult{Proxy: p.Describe()}
	p.normalize()
	u, err := p.URL()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if strings.TrimSpace(target) == "" {
		target = defaultMikanHost + "/"
	}
	res.Target = target
	if ua == "" {
		ua = "Mozilla/5.0 (X11; Linux aarch64) anime-go/0.1"
	}

	tr := &http.Transport{
		Proxy:                 http.ProxyURL(u),
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	defer tr.CloseIdleConnections()
	cli := &http.Client{Timeout: 20 * time.Second, Transport: tr}

	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		res.Error = "测试地址不合法: " + err.Error()
		return res
	}
	req.Header.Set("User-Agent", ua)
	start := time.Now()
	resp, err := cli.Do(req)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	res.OK = resp.StatusCode > 0 && resp.StatusCode < 500
	return res
}

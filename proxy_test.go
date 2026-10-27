package main

// 代理配置的单元测试（2026-10-27 新增「设置 → 代理」时一并加）。
//
// 这里不碰真实网络：只验证拼 URL、归一化、校验、以及「内网请求不走代理」
// 这条最容易写错、写错了又最难发现的规则。

import (
	"net/http"
	"net/url"
	"testing"
)

func TestProxyURL(t *testing.T) {
	cases := []struct {
		name string
		p    ProxySettings
		want string
		err  bool
	}{
		{
			name: "未启用 = 直连",
			p:    ProxySettings{Enabled: false, Host: "1.2.3.4", Port: 8080},
			want: "",
		},
		{
			name: "http 无认证",
			p:    ProxySettings{Enabled: true, Protocol: "http", Host: "192.168.50.17", Port: 20170},
			want: "http://192.168.50.17:20170",
		},
		{
			name: "socks5 带认证",
			p:    ProxySettings{Enabled: true, Protocol: "socks5", Host: "10.0.0.2", Port: 1080, Username: "u", Password: "p"},
			want: "socks5://u:p@10.0.0.2:1080",
		},
		{
			name: "协议大小写与主机前缀都要归一化",
			p:    ProxySettings{Enabled: true, Protocol: "HTTP", Host: "http://proxy.example.com/", Port: 3128},
			want: "http://proxy.example.com:3128",
		},
		{name: "启用但没主机 = 报错", p: ProxySettings{Enabled: true, Port: 8080}, err: true},
		{name: "启用但端口为 0 = 报错", p: ProxySettings{Enabled: true, Host: "1.2.3.4"}, err: true},
	}

	for _, c := range cases {
		p := c.p
		p.normalize()
		u, err := p.URL()
		if c.err {
			if err == nil {
				t.Errorf("%s：应当报错却成功了（%v）", c.name, u)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s：意外报错 %v", c.name, err)
			continue
		}
		got := ""
		if u != nil {
			got = u.String()
		}
		if got != c.want {
			t.Errorf("%s：URL = %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestProxyNormalizeDefaults(t *testing.T) {
	// 协议留空按 http；端口越界要夹回来
	p := ProxySettings{Enabled: true, Host: " 1.2.3.4 ", Port: 70000}
	p.normalize()
	if p.Protocol != "http" {
		t.Errorf("协议留空应回落 http，得到 %q", p.Protocol)
	}
	if p.Port != 65535 {
		t.Errorf("端口应夹到 65535，得到 %d", p.Port)
	}
	if p.Host != "1.2.3.4" {
		t.Errorf("主机应去掉空白，得到 %q", p.Host)
	}
	// 关闭状态下的 Validate 永远通过（允许先关掉、留着旧地址）
	if err := (ProxySettings{Enabled: false, Host: "", Port: 0}).Validate(); err != nil {
		t.Errorf("未启用时 Validate 不应报错: %v", err)
	}
	if err := (ProxySettings{Enabled: true, Host: "1.2.3.4", Port: 3128}).Validate(); err != nil {
		t.Errorf("合法配置不应报错: %v", err)
	}
}

func TestProxyBypassesLAN(t *testing.T) {
	// 装上代理，然后逐个请求看 Proxy 回调给不给代理地址
	old := currentProxy.Load()
	defer currentProxy.Store(old)

	pu, _ := url.Parse("http://192.168.50.17:20170")
	currentProxy.Store(pu)

	lan := []string{
		"http://192.168.50.17:8888/api/v2/app/version",
		"http://10.0.0.5/x",
		"http://172.16.3.4/x",
		"http://127.0.0.1:7788/",
		"http://localhost:8080/",
		"http://[::1]:80/",
	}
	for _, raw := range lan {
		req, err := http.NewRequest("GET", raw, nil)
		if err != nil {
			t.Fatalf("构造请求失败 %s: %v", raw, err)
		}
		got, err := proxyForRequest(req)
		if err != nil {
			t.Errorf("%s：Proxy 回调报错 %v", raw, err)
		}
		if got != nil {
			t.Errorf("%s：内网请求不应走代理，却得到 %v", raw, got)
		}
	}

	// 公网请求必须走代理，否则「开了代理却没生效」是最难查的故障
	for _, raw := range []string{"https://mikanime.tv/", "https://api.tmdb.org/3/search/tv"} {
		req, err := http.NewRequest("GET", raw, nil)
		if err != nil {
			t.Fatalf("构造请求失败 %s: %v", raw, err)
		}
		got, err := proxyForRequest(req)
		if err != nil {
			t.Errorf("%s：Proxy 回调报错 %v", raw, err)
		}
		if got == nil {
			t.Errorf("%s：公网请求应当走代理", raw)
		}
	}
}

func TestProxyDisabledMeansDirect(t *testing.T) {
	old := currentProxy.Load()
	defer currentProxy.Store(old)

	currentProxy.Store(nil)
	req, _ := http.NewRequest("GET", "https://mikanime.tv/", nil)
	if got, err := proxyForRequest(req); err != nil || got != nil {
		t.Errorf("未启用代理时应直连，得到 %v / %v", got, err)
	}
}

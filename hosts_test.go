package main

// hosts 覆盖（域名 → IP）的单元测试（2026-10-28 补）。
//
// 分两类：
//   - 离线用例（默认跑）：归一化、校验、查表、结论措辞；
//   - 联网用例：只在 -tags nettest 时跑，需要真实出网，不放进默认集。
//
// 为什么要有 hosts 这个功能，以及「DNS 污染 vs SNI 拦截」的区别，
// 见 hosts.go 顶部注释与 README 的「源数据」一节。

import (
	"strings"
	"testing"
)

func TestNormalizeHostOverrides(t *testing.T) {
	cases := []struct {
		name string
		in   []HostOverride
		want []HostOverride
	}{
		{
			name: "去空白、域名小写、去尾点",
			in:   []HostOverride{{Host: "  API.TMDB.org. ", IP: " 1.2.3.4 "}},
			want: []HostOverride{{Host: "api.tmdb.org", IP: "1.2.3.4"}},
		},
		{
			name: "空项一律丢掉",
			in:   []HostOverride{{Host: "", IP: "1.2.3.4"}, {Host: "a.com", IP: ""}},
			want: []HostOverride{},
		},
		{
			name: "IP 不合法就丢掉（不报错，报错交给 validate）",
			in:   []HostOverride{{Host: "a.com", IP: "not-an-ip"}},
			want: []HostOverride{},
		},
		{
			name: "同一个域名只留第一条",
			in:   []HostOverride{{Host: "a.com", IP: "1.1.1.1"}, {Host: "a.com", IP: "2.2.2.2"}},
			want: []HostOverride{{Host: "a.com", IP: "1.1.1.1"}},
		},
		{
			name: "IPv6 允许，方括号会被剥掉",
			in:   []HostOverride{{Host: "a.com", IP: "[2606:4700::1]"}},
			want: []HostOverride{{Host: "a.com", IP: "2606:4700::1"}},
		},
		{
			name: "按域名排序，保证渲染稳定",
			in:   []HostOverride{{Host: "z.com", IP: "1.1.1.1"}, {Host: "a.com", IP: "2.2.2.2"}},
			want: []HostOverride{{Host: "a.com", IP: "2.2.2.2"}, {Host: "z.com", IP: "1.1.1.1"}},
		},
		{
			name: "域名里带空格/斜杠的丢掉",
			in:   []HostOverride{{Host: "a b.com", IP: "1.1.1.1"}, {Host: "a/b.com", IP: "1.1.1.1"}},
			want: []HostOverride{},
		},
	}
	for _, c := range cases {
		got := normalizeHostOverrides(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: 条数 %d，期望 %d（%+v）", c.name, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: 第 %d 项 = %+v，期望 %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
}

func TestValidateHostOverrides(t *testing.T) {
	// 合法：不该报错
	for _, in := range [][]HostOverride{
		nil,
		{},
		{{Host: "api.themoviedb.org", IP: "108.138.246.6"}},
		{{Host: "a.com", IP: "2606:4700::1"}},
	} {
		if err := validateHostOverrides(in); err != nil {
			t.Errorf("合法输入被判错：%+v → %v", in, err)
		}
	}

	// 非法：必须报错，并且错误信息要能指出是哪一条
	bad := []HostOverride{{Host: "ok.com", IP: "1.1.1.1"}, {Host: "bad.com", IP: "999.1.1.1"}}
	err := validateHostOverrides(bad)
	if err == nil {
		t.Fatal("非法 IP 没被拦下")
	}
	if !strings.Contains(err.Error(), "bad.com") {
		t.Errorf("错误信息没指出问题条目：%v", err)
	}
	if err := validateHostOverrides([]HostOverride{{Host: "", IP: "1.1.1.1"}}); err == nil {
		t.Error("空域名没被拦下")
	}
}

func TestOverrideFor(t *testing.T) {
	defer ApplyHostOverrides(nil)

	ApplyHostOverrides([]HostOverride{
		{Host: "api.themoviedb.org", IP: "108.138.246.6"},
	})
	if ip, ok := overrideFor("api.themoviedb.org"); !ok || ip != "108.138.246.6" {
		t.Errorf("精确命中失败：%q %v", ip, ok)
	}
	// 只做精确匹配：子域不继承，避免一条覆盖牵连到别的域名
	if _, ok := overrideFor("image.tmdb.org"); ok {
		t.Error("无关域名不应命中")
	}
	// 大小写不敏感（拨号时传进来的 host 不保证已小写）
	if _, ok := overrideFor("API.THEMOVIEDB.ORG"); !ok {
		t.Error("大小写不应影响命中")
	}

	ApplyHostOverrides(nil)
	if HostOverridesApplied() != 0 {
		t.Errorf("清空后仍报 %d 条", HostOverridesApplied())
	}
	if _, ok := overrideFor("api.themoviedb.org"); ok {
		t.Error("清空后仍然命中")
	}
}

// 结论码 → 人话：三种失败必须给出不同的、能指导下一步的措辞。
// 这一段是这次误判的根源，所以专门锁住。
func TestHostTestTextVerdicts(t *testing.T) {
	cases := []struct {
		name string
		res  HostTestResult
		want []string // 必须包含的关键词
		deny []string // 不允许出现的词
	}{
		{
			name: "成功且有 TLS 信息",
			res:  HostTestResult{OK: true, Verdict: verdictOK, Connected: "1.2.3.4:443", LatencyMS: 300, TLSInfo: "TLS 1.3 · *.tmdb.org"},
			want: []string{"连通", "TLS 1.3", "300 ms"},
		},
		{
			name: "TCP 通但 TLS 被中断 → 必须说清改 IP 没用、要用代理",
			res:  HostTestResult{Verdict: verdictTLSFailed, Connected: "172.67.73.67:443", LatencyMS: 256, Error: "read: connection reset by peer"},
			want: []string{"TLS", "代理", "没用"},
			deny: []string{"连通"}, // 绝不能报成「连通」
		},
		{
			name: "覆盖的 IP 连不上 → 要提示这条是 hosts 指的",
			res:  HostTestResult{Verdict: verdictTCPFailOvride, Override: "47.88.58.234", Host: "api.themoviedb.org", Port: 443, Error: "connection refused"},
			want: []string{"47.88.58.234", "hosts"},
		},
		{
			name: "普通连不上",
			res:  HostTestResult{Verdict: verdictTCPFailed, Host: "a.com", Port: 443, LatencyMS: 8000, Error: "i/o timeout"},
			want: []string{"连不上", "a.com", "443"},
		},
	}
	for _, c := range cases {
		got := hostTestText(c.res)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: 缺少 %q → %s", c.name, w, got)
			}
		}
		for _, d := range c.deny {
			if strings.Contains(got, d) {
				t.Errorf("%s: 不该出现 %q → %s", c.name, d, got)
			}
		}
	}
}

// describeHostOverrides 用于日志与设置页提示，空表要说「无」而不是空串，
// 否则界面会显示成一块空白，看起来像没生效。
func TestDescribeHostOverrides(t *testing.T) {
	if got := describeHostOverrides(nil); got != "无" {
		t.Errorf("空表描述 = %q，期望「无」", got)
	}
	got := describeHostOverrides([]HostOverride{
		{Host: "b.com", IP: "2.2.2.2"},
		{Host: "a.com", IP: "1.1.1.1"},
	})
	if !strings.Contains(got, "a.com=1.1.1.1") || !strings.Contains(got, "b.com=2.2.2.2") {
		t.Errorf("描述内容不对：%s", got)
	}
}

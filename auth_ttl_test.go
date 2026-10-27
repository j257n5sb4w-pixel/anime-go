package main

// 登录有效期（session_ttl_minutes）的行为测试。
//
// 三种取值语义：
//   -1 永久 / 0 单次有效（关浏览器即失效） / >0 这么多分钟后失效
//
// 覆盖点：
//   1. 默认 120 分钟：会话有效，exp 落在 ~120 分钟后
//   2. -1：exp == 0（服务端不设过期）
//   3. 0：exp == 0，但 Cookie 不带 Max-Age（会话 Cookie）
//   4. 过期会话被拒
//   5. 收紧轮换密钥、放宽不轮换（含 0 与 -1 之间的严格程度排序）
//   6. 配置层归一：nil → 120；负数 → -1；0 原样保留；超上限截断

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestAuth(t *testing.T, ttl int) *AuthStore {
	t.Helper()
	dir := t.TempDir()
	a := NewAuthStore(filepath.Join(dir, "auth.json"))
	if err := a.Register("admin", "test123456"); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	a.InitSessionTTL(ttl)
	return a
}

// expOf 从会话值里解出 exp 段。
func expOf(t *testing.T, sid string) int64 {
	t.Helper()
	i := strings.LastIndex(sid, ".")
	if i <= 0 {
		t.Fatalf("会话值格式不对: %q", sid)
	}
	payload := sid[:i]
	j := strings.LastIndex(payload, "|")
	if j <= 0 {
		t.Fatalf("payload 格式不对: %q", payload)
	}
	var exp int64
	if _, err := fmt.Sscanf(payload[j+1:], "%d", &exp); err != nil {
		t.Fatalf("解析 exp 失败: %v", err)
	}
	return exp
}

func TestSessionTTLDefault120(t *testing.T) {
	a := newTestAuth(t, defaultSessionTTLMinutes)
	sid := a.NewSession()
	if !a.CheckSession(sid) {
		t.Fatal("默认 120 分钟的会话应当有效")
	}
	want := time.Now().Add(120 * time.Minute).Unix()
	if got := expOf(t, sid); got < want-60 || got > want+60 {
		t.Fatalf("exp 应约为 120 分钟后: got %d want ~%d", got, want)
	}
}

func TestSessionTTLForever(t *testing.T) {
	a := newTestAuth(t, permanentSessionTTL)
	sid := a.NewSession()
	if !a.CheckSession(sid) {
		t.Fatal("永久会话应当有效")
	}
	if got := expOf(t, sid); got != 0 {
		t.Fatalf("永久会话的 exp 应为 0，实际 %d", got)
	}
}

func TestSessionTTLSingleUse(t *testing.T) {
	a := newTestAuth(t, singleUseSessionTTL)
	sid := a.NewSession()
	if !a.CheckSession(sid) {
		t.Fatal("单次有效的会话在服务端应当有效（靠浏览器丢 Cookie 失效）")
	}
	if got := expOf(t, sid); got != 0 {
		t.Fatalf("单次有效的 exp 应为 0（服务端不设过期），实际 %d", got)
	}
}

func TestSessionExpiredRejected(t *testing.T) {
	a := newTestAuth(t, 120)
	a.mu.RLock()
	secret := a.data.Secret
	a.mu.RUnlock()
	payload := fmt.Sprintf("admin|%d", time.Now().Add(-time.Minute).Unix())
	if a.CheckSession(payload + "." + sign(secret, payload)) {
		t.Fatal("已过期的会话必须被拒绝")
	}
}

func TestTTLTightened(t *testing.T) {
	cases := []struct {
		old, nw int
		want    bool
	}{
		{120, 30, true},   // 变短 → 收紧
		{30, 120, false},  // 变长 → 放宽
		{-1, 120, true},   // 永久 → 有限 → 收紧
		{120, -1, false},  // 有限 → 永久 → 放宽
		{-1, 0, true},     // 永久 → 单次 → 收紧
		{0, -1, false},    // 单次 → 永久 → 放宽
		{0, 120, false},   // 单次 → 有限 → 放宽（服务端仍不设过期）
		{120, 0, true},    // 有限 → 单次 → 收紧
		{0, 0, false},     // 没变
		{120, 120, false}, // 没变
		{-1, -1, false},   // 没变
	}
	for _, c := range cases {
		if got := ttlTightened(c.old, c.nw); got != c.want {
			t.Errorf("ttlTightened(%d, %d) = %v, want %v", c.old, c.nw, got, c.want)
		}
	}
}

func TestChangingTTLRotatesSessions(t *testing.T) {
	a := newTestAuth(t, 120)
	old := a.NewSession()
	if !a.CheckSession(old) {
		t.Fatal("改之前会话应有效")
	}

	// 收紧（120 → 30）：轮换密钥，旧会话立即失效
	rotated, err := a.SetSessionTTLMinutes(30)
	if err != nil {
		t.Fatalf("收紧失败: %v", err)
	}
	if !rotated {
		t.Fatal("收紧有效期应当轮换会话密钥")
	}
	if a.CheckSession(old) {
		t.Fatal("收紧后旧会话必须失效（密钥已轮换）")
	}
	if a.SessionTTLMinutes() != 30 {
		t.Fatalf("有效期应为 30，实际 %d", a.SessionTTLMinutes())
	}
	fresh := a.NewSession()
	if !a.CheckSession(fresh) {
		t.Fatal("收紧后新签发的会话应有效")
	}

	// 改成单次有效（30 → 0）：也算收紧
	rotated, err = a.SetSessionTTLMinutes(singleUseSessionTTL)
	if err != nil {
		t.Fatalf("改为单次有效失败: %v", err)
	}
	if !rotated {
		t.Fatal("改为单次有效应当轮换会话密钥")
	}
	if a.CheckSession(fresh) {
		t.Fatal("改为单次有效后旧会话必须失效")
	}
	single := a.NewSession()
	if !a.CheckSession(single) {
		t.Fatal("单次有效的会话应有效")
	}

	// 放宽（0 → 永久）：不轮换
	rotated, err = a.SetSessionTTLMinutes(permanentSessionTTL)
	if err != nil {
		t.Fatalf("放宽失败: %v", err)
	}
	if rotated {
		t.Fatal("放宽有效期不应轮换会话密钥")
	}
	if !a.CheckSession(single) {
		t.Fatal("放宽后已有会话应继续有效")
	}
	if a.SessionTTLMinutes() != permanentSessionTTL {
		t.Fatalf("有效期应为 -1，实际 %d", a.SessionTTLMinutes())
	}
}

func TestConfigNormalizesTTL(t *testing.T) {
	i := func(v int) *int { return &v }
	cases := []struct {
		name string
		in   *int
		want int
	}{
		{"缺字段按默认", nil, defaultSessionTTLMinutes},
		{"显式 0 = 单次有效", i(0), singleUseSessionTTL},
		{"-1 永久", i(-1), permanentSessionTTL},
		{"-99 归一为 -1", i(-99), permanentSessionTTL},
		{"正常值", i(30), 30},
		{"7 天", i(10080), 10080},
		{"超上限截断", i(maxSessionTTLMinutes + 1), maxSessionTTLMinutes},
	}
	for _, c := range cases {
		cfg := &Config{SessionTTLMinutes: c.in}
		cfg.applyDefaults()
		if got := cfg.SessionTTLMinutesValue(); got != c.want {
			t.Errorf("%s: 输入 %v 应归一为 %d，实际 %d", c.name, fmtPtr(c.in), c.want, got)
		}
		if cfg.SessionTTLMinutes == nil {
			t.Errorf("%s: applyDefaults 后指针不应为 nil", c.name)
		}
	}
}

func fmtPtr(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

func TestTTLText(t *testing.T) {
	cases := map[int]string{
		-1:    "永久（永不过期）",
		0:     "单次有效（关闭浏览器即失效）",
		30:    "30 分钟",
		120:   "2 小时",
		1440:  "1 天",
		10080: "7 天",
		90:    "90 分钟（约 1.5 小时）",
	}
	for in, want := range cases {
		if got := ttlText(in); got != want {
			t.Errorf("ttlText(%d) = %q, want %q", in, got, want)
		}
	}
}

// ---- Cookie 下发：单次有效必须不带 Max-Age ----

func newCookieServer(t *testing.T, ttl int) *server {
	t.Helper()
	a := newTestAuth(t, ttl)
	return &server{app: &App{auth: a, log: &Logger{}}, startAt: time.Now()}
}

func cookieFor(t *testing.T, ttl int) *http.Cookie {
	t.Helper()
	s := newCookieServer(t, ttl)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	s.setSessionCookie(w, r)
	ck := w.Result().Cookies()
	if len(ck) == 0 {
		t.Fatal("没有下发 Cookie")
	}
	return ck[0]
}

func TestCookieMaxAgeByTTL(t *testing.T) {
	// 单次有效：MaxAge 必须为 0（不写 Max-Age/Expires）
	if ck := cookieFor(t, singleUseSessionTTL); ck.MaxAge != 0 {
		t.Errorf("单次有效的 Cookie MaxAge 应为 0（会话 Cookie），实际 %d", ck.MaxAge)
	} else if strings.Contains(cookieFor(t, singleUseSessionTTL).String(), "Max-Age") {
		t.Error("单次有效的 Set-Cookie 不应包含 Max-Age")
	}

	// 永久：10 年
	if ck := cookieFor(t, permanentSessionTTL); ck.MaxAge != permanentCookieMaxAge {
		t.Errorf("永久 Cookie MaxAge 应为 %d，实际 %d", permanentCookieMaxAge, ck.MaxAge)
	}

	// 有限：分钟 × 60
	if ck := cookieFor(t, 120); ck.MaxAge != 7200 {
		t.Errorf("120 分钟的 Cookie MaxAge 应为 7200，实际 %d", ck.MaxAge)
	}
	if ck := cookieFor(t, 45); ck.MaxAge != 2700 {
		t.Errorf("45 分钟的 Cookie MaxAge 应为 2700，实际 %d", ck.MaxAge)
	}
}

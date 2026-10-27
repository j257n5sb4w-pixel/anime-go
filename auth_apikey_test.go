package main

// API Key 与「登录有效期」无关的回归测试。
//
// 用户要求（2026-10-26）：「api 的登录不设置时间限制」。
// API Key 是脚本/AI 用的长期凭据，只有主动重置（RotateAPIKey）或删除 auth.json
// 才失效；改登录有效期（含改成 0 单次有效）绝不能影响它。
//
// ⚠ 这里也守住一个易错点：SetSessionTTLMinutes 只轮换 session_secret，
// 如果哪天有人顺手把 api_key 也轮换了，这里的测试会立刻炸。

import (
	"path/filepath"
	"testing"
)

// newAuthWithKey 建一个已注册的 AuthStore，返回它当时的 API Key。
func newAuthWithKey(t *testing.T) (*AuthStore, string) {
	t.Helper()
	dir := t.TempDir()
	a := NewAuthStore(filepath.Join(dir, "auth.json"))
	if err := a.Register("admin", "test123456"); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	key := a.APIKey()
	if key == "" {
		t.Fatal("注册后应自动生成 API Key")
	}
	return a, key
}

func TestAPIKeySurvivesTTLChanges(t *testing.T) {
	a, key := newAuthWithKey(t)

	// 依次改成各种有效期，API Key 必须始终有效
	for _, ttl := range []int{30, singleUseSessionTTL, permanentSessionTTL, 5, defaultSessionTTLMinutes} {
		if _, err := a.SetSessionTTLMinutes(ttl); err != nil {
			t.Fatalf("设置有效期 %d 失败: %v", ttl, err)
		}
		if !a.VerifyAPIKey(key) {
			t.Fatalf("改成有效期 %d 后 API Key 失效了——有效期不该影响 API Key", ttl)
		}
		if got := a.APIKey(); got != key {
			t.Fatalf("改有效期后 API Key 变了：%q → %q", key, got)
		}
	}
}

func TestAPIKeyHasNoExpiry(t *testing.T) {
	a, key := newAuthWithKey(t)

	// 会话密钥轮换（收紧有效期）会踢掉所有浏览器会话，但 API Key 不受影响
	if _, err := a.SetSessionTTLMinutes(1); err != nil {
		t.Fatalf("收紧失败: %v", err)
	}
	sid := a.NewSession()
	if !a.CheckSession(sid) {
		t.Fatal("新会话应有效")
	}
	if !a.VerifyAPIKey(key) {
		t.Fatal("会话轮换后 API Key 仍应有效")
	}

	// 改密码会轮换会话密钥并踢掉所有设备，但 API Key 保持不变
	if err := a.ChangePassword("test123456", "newpass123"); err != nil {
		t.Fatalf("改密码失败: %v", err)
	}
	if a.CheckSession(sid) {
		t.Fatal("改密码后旧会话应失效")
	}
	if !a.VerifyAPIKey(key) {
		t.Fatal("改密码后 API Key 仍应有效（这是脚本/AI 的长期凭据）")
	}
}

func TestAPIKeyOnlyRotatedExplicitly(t *testing.T) {
	a, key := newAuthWithKey(t)

	newKey, err := a.RotateAPIKey()
	if err != nil {
		t.Fatalf("重置 API Key 失败: %v", err)
	}
	if newKey == key {
		t.Fatal("重置后应当是新的 Key")
	}
	if a.VerifyAPIKey(key) {
		t.Fatal("重置后旧 Key 必须失效")
	}
	if !a.VerifyAPIKey(newKey) {
		t.Fatal("重置后新 Key 应有效")
	}
}

func TestAPIKeyRejectsWrongValue(t *testing.T) {
	a, _ := newAuthWithKey(t)
	for _, bad := range []string{"", "wrong", "deadbeef"} {
		if a.VerifyAPIKey(bad) {
			t.Fatalf("错误 Key %q 不应通过校验", bad)
		}
	}
}

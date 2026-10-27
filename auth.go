package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 认证与授权
//
// 设计取舍（2026-10-26 重做）：
//   旧方案是"给破坏性接口配 admin_token"，但它有死锁——想设令牌必须先有令牌，
//   局域网浏览器永远进不来。现在改为统一登录：
//     · 首次访问 /setup 注册账号密码（一次性，建完即失效）
//     · 之后 /login 登录，拿到 HttpOnly 会话 Cookie，登录即拥有全部权限
//     · 另外生成一枚 API Key，供 AI / 脚本免登录调用（Authorization: Bearer 或 X-API-Key）
//   密码用 PBKDF2-HMAC-SHA256 加随机盐存储，绝不落明文；API Key 单独存储、可随时重置。
//
// 本机回环（127.0.0.1 / ::1）默认免登录，便于在机器上直接 curl 和排障；
// 这不是安全边界（能登机器的人本就拥有全部权限），只是为了不把运维工具也锁死。

const (
	pbkdf2Iter    = 120000
	authCookie    = "anime_go_sid"
	apiKeyHeader  = "X-API-Key"
	authFileModes = 0o600

	// defaultSessionTTLMinutes 是登录有效期默认值：2 小时（用户 2026-10-26 要求）。
	defaultSessionTTLMinutes = 120
	// permanentSessionTTL 是「永不过期」的取值，设置里填 -1 即可。
	permanentSessionTTL = -1
	// singleUseSessionTTL 是「单次有效」的取值：只发会话 Cookie（不带 Max-Age），
	// 浏览器一关就丢，下次打开必须重新登录。
	singleUseSessionTTL = 0
	// maxSessionTTLMinutes 是自定义有效期的上限（约一年）。
	maxSessionTTLMinutes = 525600
	// permanentCookieMaxAge 是永久会话的 Cookie 寿命（浏览器侧），10 年足够当「永久」。
	permanentCookieMaxAge = 10 * 365 * 24 * 3600
)

// AuthData 是落盘的账号信息。密码只存哈希，API Key 明文（需要展示给用户）。
type AuthData struct {
	Username  string    `json:"username"`
	Salt      string    `json:"salt"`
	Hash      string    `json:"hash"`
	Iter      int       `json:"iter"`
	APIKey    string    `json:"api_key"`
	Secret    string    `json:"session_secret"`
	CreatedAt time.Time `json:"created_at"`
}

// AuthStore 管理账号与登录状态。
type AuthStore struct {
	mu   sync.RWMutex
	path string
	data AuthData
	// ttlMinutes 是登录有效期（分钟）。-1 = 永不过期。
	// 放在 AuthStore 里而不是每次读 Config，是为了 /api/settings/save
	// 改完立刻生效，且读写都有锁保护。
	ttlMinutes int
}

func NewAuthStore(path string) *AuthStore {
	a := &AuthStore{path: path, ttlMinutes: defaultSessionTTLMinutes}
	a.load()
	return a
}

func (a *AuthStore) load() {
	b, err := os.ReadFile(a.path)
	if err != nil {
		return
	}
	var d AuthData
	if err := json.Unmarshal(b, &d); err != nil {
		return
	}
	a.data = d
}

func (a *AuthStore) save() error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, b, authFileModes); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// HasAccount 是否已经注册过账号（决定首访去 /setup 还是 /login）。
func (a *AuthStore) HasAccount() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.data.Username != ""
}

// Username 返回当前账号名。
func (a *AuthStore) Username() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.data.Username
}

// Register 首次注册：写入账号密码并生成 API Key 与会话密钥。
// 已存在账号时返回错误，调用方据此拒绝二次注册。
func (a *AuthStore) Register(user, pass string) error {
	user = strings.TrimSpace(user)
	if user == "" {
		return errors.New("账号不能为空")
	}
	if len(pass) < 6 {
		return errors.New("密码至少 6 位")
	}
	salt := randomHex(16)
	hash := pbkdf2Hex(pass, salt, pbkdf2Iter)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.data.Username != "" {
		return errors.New("账号已存在")
	}
	a.data = AuthData{
		Username:  user,
		Salt:      salt,
		Hash:      hash,
		Iter:      pbkdf2Iter,
		APIKey:    randomHex(24),
		Secret:    randomHex(32),
		CreatedAt: time.Now(),
	}
	return a.save()
}

// Verify 校验账号密码。
func (a *AuthStore) Verify(user, pass string) bool {
	a.mu.RLock()
	d := a.data
	a.mu.RUnlock()
	if d.Username == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(d.Username)) != 1 {
		return false
	}
	iter := d.Iter
	if iter <= 0 {
		iter = pbkdf2Iter
	}
	got := pbkdf2Hex(pass, d.Salt, iter)
	return subtle.ConstantTimeCompare([]byte(got), []byte(d.Hash)) == 1
}

// APIKey 返回当前 API Key（用于展示给用户）。
func (a *AuthStore) APIKey() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.data.APIKey
}

// RotateAPIKey 重新生成 API Key，旧 Key 立即失效。
func (a *AuthStore) RotateAPIKey() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data.APIKey = randomHex(24)
	if err := a.save(); err != nil {
		return "", err
	}
	return a.data.APIKey, nil
}

// SetSessionTTLMinutes 设置登录有效期（分钟）。
//
//	-1（或其它负数）= 永久
//	 0            = 单次有效（浏览器关闭即失效）
//	>0            = 这么多分钟后失效
//
// 返回值 rotated 表示是否轮换了会话密钥。
// 只在「收紧」有效期时才轮换：旧会话的到期时间已经写死在各自的 Cookie / 会话值里，
// 不轮换的话它们会继续按旧的宽松期限有效，改短就等于没改。
// 反过来放宽（改长、改永久）时不动密钥——放宽策略没有安全理由踢人下线。
func (a *AuthStore) SetSessionTTLMinutes(m int) (rotated bool, err error) {
	if m < 0 {
		m = permanentSessionTTL
	} else if m > maxSessionTTLMinutes {
		m = maxSessionTTLMinutes
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.ttlMinutes
	a.ttlMinutes = m

	if !ttlTightened(old, m) {
		return false, nil
	}
	a.data.Secret = randomHex(32)
	return true, a.save()
}

// ttlTightened 判断从 old 改成 nw 是否算「收紧」。
//
// 严格程度排序：单次有效(0) > 有限时长(正数，越小越严) > 永久(-1)。
func ttlTightened(old, nw int) bool {
	switch {
	case nw < 0: // 改成永久：永远不算收紧
		return false
	case nw == 0: // 改成单次有效：除非本来就是单次，否则都算收紧
		return old != 0
	default: // 改成有限时长：原本是永久，或新值比旧值小
		return old < 0 || nw < old
	}
}

// InitSessionTTL 启动时套用配置里的有效期，不落盘、不轮换密钥，
// 因此重启后已登录的浏览器不会掉线。
func (a *AuthStore) InitSessionTTL(m int) {
	if m < 0 {
		m = permanentSessionTTL
	}
	a.mu.Lock()
	a.ttlMinutes = m
	a.mu.Unlock()
}

// SessionTTLMinutes 返回当前登录有效期（分钟）。
// -1 = 永久，0 = 单次有效。
func (a *AuthStore) SessionTTLMinutes() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ttlMinutes
}

// VerifyAPIKey 校验 API Key（常数时间比较）。
func (a *AuthStore) VerifyAPIKey(k string) bool {
	a.mu.RLock()
	want := a.data.APIKey
	a.mu.RUnlock()
	if want == "" || k == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(k), []byte(want)) == 1
}

// ChangePassword 修改密码：先校验旧密码，再用新盐重算哈希。
// 会话密钥一并轮换，因此改密后其它设备上的旧会话立即失效（当前设备会重新登录）。
func (a *AuthStore) ChangePassword(oldPass, newPass string) error {
	newPass = strings.TrimSpace(newPass)
	if len(newPass) < 6 {
		return errors.New("新密码至少 6 位")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.data.Username == "" {
		return errors.New("尚未注册账号")
	}
	iter := a.data.Iter
	if iter <= 0 {
		iter = pbkdf2Iter
	}
	got := pbkdf2Hex(oldPass, a.data.Salt, iter)
	if subtle.ConstantTimeCompare([]byte(got), []byte(a.data.Hash)) != 1 {
		return errors.New("当前密码不正确")
	}

	salt := randomHex(16)
	a.data.Salt = salt
	a.data.Hash = pbkdf2Hex(newPass, salt, pbkdf2Iter)
	a.data.Iter = pbkdf2Iter
	a.data.Secret = randomHex(32) // 轮换会话密钥：踢掉其它设备
	return a.save()
}

// ---- 会话：无状态签名 Cookie，重启不掉线 ----

// NewSession 生成 <user|exp>.<hmac> 形式的会话值。
//
// exp = 0 表示「服务端不设过期」，具体有两种语义，区别只在 Cookie 怎么下发：
//   - 有效期 -1（永久）：Cookie 带 10 年 Max-Age
//   - 有效期  0（单次）：Cookie 不带 Max-Age，是会话 Cookie，关浏览器即丢失
func (a *AuthStore) NewSession() string {
	a.mu.RLock()
	user, secret, ttl := a.data.Username, a.data.Secret, a.ttlMinutes
	a.mu.RUnlock()
	exp := int64(0)
	if ttl > 0 {
		exp = time.Now().Add(time.Duration(ttl) * time.Minute).Unix()
	}
	payload := fmt.Sprintf("%s|%d", user, exp)
	return payload + "." + sign(secret, payload)
}

// CheckSession 校验签名与有效期。
func (a *AuthStore) CheckSession(v string) bool {
	a.mu.RLock()
	user, secret := a.data.Username, a.data.Secret
	a.mu.RUnlock()
	if user == "" || v == "" {
		return false
	}
	i := strings.LastIndex(v, ".")
	if i <= 0 {
		return false
	}
	payload, sig := v[:i], v[i+1:]
	if subtle.ConstantTimeCompare([]byte(sig), []byte(sign(secret, payload))) != 1 {
		return false
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 2 || parts[0] != user {
		return false
	}
	var exp int64
	if _, err := fmt.Sscanf(parts[1], "%d", &exp); err != nil {
		return false
	}
	if exp == 0 {
		return true // 永久会话（有效期设为 -1）
	}
	return time.Now().Unix() < exp
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ---- 底层工具 ----

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 熵源不可用时宁可失败也不生成可预测密钥
		panic("crypto/rand 不可用: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// pbkdf2Hex 实现 PBKDF2-HMAC-SHA256（RFC 2898），避免为 hash 引入外部依赖。
func pbkdf2Hex(pass, salt string, iter int) string {
	if iter <= 0 {
		iter = pbkdf2Iter
	}
	key := pbkdf2SHA256([]byte(pass), []byte(salt), iter, 32)
	return hex.EncodeToString(key)
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)

	buf := make([]byte, 4)
	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf)

		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

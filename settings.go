package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ============================================================
//  设置（Settings）框架
//
//  设计目标：以后所有"全局可调项"都往这里加，不用再动其他文件。
//
//  当前已接入：
//    - qBittorrent WebUI 主机 / 端口 / API Key / 分类
//    - 检查间隔（分钟）
//    - 下载目录白名单
//
//  未来可加（框架已留位置）：
//    - 通知推送（QQ / Bark / Telegram）
//    - TMDB 刮削开关与 API Key
//    - 重命名模板默认值、字幕组偏好
//    - 日志级别、保留天数
//
//  ⚠️ 敏感字段（API Key）返回时打码，但保存时允许传空 = 不修改。
// ============================================================

// SettingsPayload 是前端提交的设置表单
type SettingsPayload struct {
	// qBittorrent
	QbitHost     *string `json:"qbit_host"`
	QbitPort     *int    `json:"qbit_port"`
	QbitAPIKey   *string `json:"qbit_apikey"`
	QbitCategory *string `json:"qbit_category"`

	// 抓取
	IntervalMinutes *int    `json:"interval_minutes"`
	UA              *string `json:"ua"`

	// TMDB 封面
	TMDBKey  *string `json:"tmdb_key"`
	TMDBHost *string `json:"tmdb_host"`

	// 安全
	AllowWritePrefixes *[]string `json:"allow_write_prefixes"`

	// 下载目录挂载（删除订阅时连文件夹一起删）
	DownloadsMount *string `json:"downloads_mount"`
	DownloadsRoot  *string `json:"downloads_root"`
	// Mounts 是多条 qB→本机 映射（新配置；老的两个字段仍兼容）
	Mounts *[]PathMount `json:"mounts"`

	// 全局默认过滤规则（订阅里留空即继承这里的值）
	DefaultMatch   *[]string `json:"default_match"`
	DefaultExclude *[]string `json:"default_exclude"`

	// LibraryRoot 追更库根目录（qBittorrent 侧路径）。
	// 解析「同一个番名目录」时在这一级下列子目录。
	LibraryRoot *string `json:"library_root"`

	// CoverSource 封面来源：mikan（默认，一季一图）或 tmdb。
	CoverSource *string `json:"cover_source"`

	// 登录有效期（分钟）。-1 = 永不过期。
	SessionTTLMinutes *int `json:"session_ttl_minutes"`
}

// handleSettingsGet 返回当前设置（API Key 打码）
func (s *server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.cfg
	host, port := splitQbitURL(cfg.Qbit.URL)

	writeJSON(w, 200, map[string]any{
		"ok": true,
		"settings": map[string]any{
			"qbit_host":       host,
			"qbit_port":       port,
			"qbit_apikey":     maskKey(cfg.Qbit.APIKey),
			"qbit_apikey_set": cfg.Qbit.APIKey != "",
			"qbit_category":   cfg.Qbit.Category,
			"qbit_url":        cfg.Qbit.URL,

			"interval_minutes": cfg.IntervalMinutes,
			"ua":               cfg.UA,

			"tmdb_key":     maskKey(cfg.TMDBKey),
			"tmdb_key_set": cfg.TMDBKey != "",
			"tmdb_host":    cfg.TMDBHost,
			"covers_count": s.app.countCovers(),

			"allow_write_prefixes": cfg.AllowWritePrefixes,
			"downloads_mount":      cfg.DownloadsMount,
			"downloads_root":       cfg.DownloadsRoot,
			"mounts":               cfg.EffectiveMounts(),
			"default_save_path":    defaultSavePath,
			"default_rename":       defaultRename,
			"library_root":         cfg.LibraryRootPath(),
			"cover_source":         cfg.CoverSource,
			"default_match":        cfg.DefaultMatch,
			"default_exclude":      cfg.DefaultExclude,
			"session_ttl_minutes":  s.app.auth.SessionTTLMinutes(),
			"listen":               cfg.Listen,
			"state_file":           cfg.StateFile,
			"log_file":             cfg.LogFile,
			"dry_run":              cfg.DryRun,
		},
		// 供前端渲染"未来功能"占位区
		"planned": []map[string]string{
			{"name": "通知推送", "desc": "QQ / Bark / Telegram 推送到手机"},
			{"name": "TMDB 刮削", "desc": "自动生成 nfo 与封面"},
			{"name": "默认模板", "desc": "统一设置重命名与保存路径模板"},
			{"name": "日志保留", "desc": "按天清理历史日志"},
		},
	})
}

// handleSettingsSave 保存设置。只改传了的字段，其余保持原值。
func (s *server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "只接受 POST"})
		return
	}
	var req SettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败: " + err.Error()})
		return
	}

	s.app.mu.Lock()
	cfg := s.app.cfg
	backup := *cfg // 整份备份，失败可回滚
	backup.Qbit = cfg.Qbit

	var changed []string

	// ---- qBittorrent 连接参数 ----
	if req.QbitHost != nil || req.QbitPort != nil {
		host, port := splitQbitURL(cfg.Qbit.URL)
		if req.QbitHost != nil {
			host = strings.TrimSpace(*req.QbitHost)
		}
		if req.QbitPort != nil {
			port = *req.QbitPort
		}
		if host == "" {
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{"ok": false, "error": "qBittorrent 主机不能为空"})
			return
		}
		if port <= 0 || port > 65535 {
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{"ok": false, "error": "端口必须在 1-65535 之间"})
			return
		}
		cfg.Qbit.URL = buildQbitURL(host, port)
		changed = append(changed, "qBittorrent 地址")
	}
	// API Key 传空字符串表示"不修改"，只有明确传新值才覆盖
	if req.QbitAPIKey != nil && strings.TrimSpace(*req.QbitAPIKey) != "" {
		cfg.Qbit.APIKey = strings.TrimSpace(*req.QbitAPIKey)
		changed = append(changed, "API Key")
	}
	if req.QbitCategory != nil && strings.TrimSpace(*req.QbitCategory) != "" {
		cfg.Qbit.Category = strings.TrimSpace(*req.QbitCategory)
		changed = append(changed, "分类")
	}

	// ---- 抓取参数 ----
	if req.IntervalMinutes != nil {
		if *req.IntervalMinutes < 1 || *req.IntervalMinutes > 1440 {
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{"ok": false, "error": "检查间隔必须在 1-1440 分钟之间"})
			return
		}
		cfg.IntervalMinutes = *req.IntervalMinutes
		changed = append(changed, "检查间隔")
	}
	if req.UA != nil && strings.TrimSpace(*req.UA) != "" {
		cfg.UA = strings.TrimSpace(*req.UA)
	}

	// ---- TMDB 封面 ----
	// Key 传空 = 不修改；传特殊值 "-" = 清空（停用封面功能）
	if req.TMDBKey != nil {
		v := strings.TrimSpace(*req.TMDBKey)
		switch {
		case v == "-":
			cfg.TMDBKey = ""
			changed = append(changed, "TMDB Key（已清空）")
		case v != "":
			cfg.TMDBKey = v
			changed = append(changed, "TMDB Key")
		}
	}
	if req.TMDBHost != nil && strings.TrimSpace(*req.TMDBHost) != "" {
		cfg.TMDBHost = strings.TrimSpace(*req.TMDBHost)
		changed = append(changed, "TMDB 域名")
	}

	// ---- 下载目录白名单 ----
	if req.AllowWritePrefixes != nil {
		cfg.AllowWritePrefixes = *req.AllowWritePrefixes
		changed = append(changed, "写入白名单")
	}

	// ---- 下载目录挂载点（删除订阅时连文件夹一起删） ----
	if req.Mounts != nil {
		cfg.Mounts = *req.Mounts
		// 保持老字段与 Mounts[0] 一致，避免两处配置打架
		if len(cfg.Mounts) > 0 {
			cfg.DownloadsRoot = cfg.Mounts[0].Qbit
			cfg.DownloadsMount = cfg.Mounts[0].Local
		}
		changed = append(changed, "挂载映射")
	}
	if req.DownloadsMount != nil {
		cfg.DownloadsMount = strings.TrimSpace(*req.DownloadsMount)
		changed = append(changed, "下载目录挂载点")
	}
	if req.DownloadsRoot != nil && strings.TrimSpace(*req.DownloadsRoot) != "" {
		cfg.DownloadsRoot = strings.TrimSpace(*req.DownloadsRoot)
		changed = append(changed, "qB 下载根目录")
	}
	// 老表单只改了两个旧字段时，把 Mounts 重建成那一条，
	// 否则 normalizeMounts 会继续沿用旧 Mounts，改了等于没改。
	if req.Mounts == nil && (req.DownloadsMount != nil || req.DownloadsRoot != nil) {
		if strings.TrimSpace(cfg.DownloadsMount) == "" {
			cfg.Mounts = nil
		} else {
			cfg.Mounts = []PathMount{{Qbit: cfg.DownloadsRoot, Local: cfg.DownloadsMount}}
		}
	}

	// ---- 全局默认过滤规则（订阅留空即继承） ----
	if req.DefaultMatch != nil {
		rules, bad := cleanRuleList(*req.DefaultMatch)
		if len(bad) > 0 {
			*cfg = backup
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{
				"ok":    false,
				"error": "过滤规则正则有误：" + strings.Join(bad, "；"),
			})
			return
		}
		cfg.DefaultMatch = rules
		changed = append(changed, "默认识别规则")
	}
	if req.DefaultExclude != nil {
		rules, bad := cleanRuleList(*req.DefaultExclude)
		if len(bad) > 0 {
			*cfg = backup
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{
				"ok":    false,
				"error": "排除规则正则有误：" + strings.Join(bad, "；"),
			})
			return
		}
		cfg.DefaultExclude = rules
		changed = append(changed, "默认排除规则")
	}

	// ---- 追更库根目录（解析同名番名目录用） ----
	if req.LibraryRoot != nil {
		v := strings.TrimRight(strings.TrimSpace(*req.LibraryRoot), "/")
		if v != "" && !strings.HasPrefix(v, "/") {
			*cfg = backup
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{
				"ok":    false,
				"error": "追更库根目录必须是以 / 开头的 qBittorrent 侧绝对路径",
			})
			return
		}
		cfg.LibraryRoot = v
		changed = append(changed, "追更库根目录")
	}

	// ---- 封面来源 ----
	// mikan 逐季建条目、一季一张海报；TMDB 多季合一条目只有一张总海报。
	if req.CoverSource != nil {
		v := strings.ToLower(strings.TrimSpace(*req.CoverSource))
		if v != "mikan" && v != "tmdb" {
			*cfg = backup
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{
				"ok":    false,
				"error": "封面来源只能是 mikan 或 tmdb",
			})
			return
		}
		cfg.CoverSource = v
		changed = append(changed, "封面来源")
	}

	// ---- 登录有效期 ----
	// -1 = 永久；0 = 单次有效（关浏览器失效）；>0 = 分钟数（最长一年）。
	if req.SessionTTLMinutes != nil {
		v := *req.SessionTTLMinutes
		if v < permanentSessionTTL || v > maxSessionTTLMinutes {
			*cfg = backup
			s.app.mu.Unlock()
			writeJSON(w, 400, map[string]any{
				"ok": false,
				"error": fmt.Sprintf(
					"登录有效期只能填 0（单次有效）、-1（永久）或 1-%d 分钟（约一年）",
					maxSessionTTLMinutes),
			})
			return
		}
		vv := v
		cfg.SessionTTLMinutes = &vv
		// 提示语由下面的 ttlNote 统一给出，不再往 changed 里塞一条重复的
	}

	cfg.applyDefaults()

	// 连接测试：改了 qBit 参数就先试连，失败则整份回滚
	qbitChanged := req.QbitHost != nil || req.QbitPort != nil ||
		(req.QbitAPIKey != nil && strings.TrimSpace(*req.QbitAPIKey) != "") ||
		(req.QbitCategory != nil && strings.TrimSpace(*req.QbitCategory) != "")
	var testErr error
	if qbitChanged {
		testQ := NewQbit(cfg.Qbit, cfg.UA)
		if _, err := testQ.Version(); err != nil {
			testErr = err
		}
	}
	if testErr != nil {
		*cfg = backup
		s.app.mu.Unlock()
		writeJSON(w, 400, map[string]any{
			"ok":    false,
			"error": "连接 qBittorrent 失败，设置未保存：" + testErr.Error(),
		})
		return
	}

	if err := SaveConfig(s.app.cfgPath, cfg); err != nil {
		*cfg = backup
		s.app.mu.Unlock()
		writeJSON(w, 500, map[string]any{"ok": false, "error": "保存配置失败: " + err.Error()})
		return
	}

	// 重建 qbit 客户端：它内部持有配置副本，不重建则新设置不生效
	s.app.qbit = NewQbit(cfg.Qbit, cfg.UA)
	s.app.mu.Unlock()

	// 登录有效期热生效。
	// 收紧期限时会轮换会话密钥（旧会话按老期限活着，不轮换等于没改短），
	// 因此紧接着给当前这台设备补发一枚新 Cookie，避免保存完把自己踢下线；
	// 放宽或改永久时不轮换密钥，其它设备继续正常在线。
	ttlNote := ""
	ttlVal := 0
	if req.SessionTTLMinutes != nil {
		ttlVal = *cfg.SessionTTLMinutes
		rotated, err := s.app.auth.SetSessionTTLMinutes(ttlVal)
		if err != nil {
			s.app.log.Printf("⚠ 登录有效期写入失败: %v", err)
			ttlNote = fmt.Sprintf("，但登录有效期写盘失败：%v（重启后可能回退）", err)
		} else {
			s.setSessionCookie(w, r)
			ttlNote = fmt.Sprintf("，登录有效期 %s", ttlText(ttlVal))
			if rotated {
				ttlNote += "，本机已续期；其它设备需重新登录"
			}
		}
	}

	if len(changed) == 0 && ttlNote == "" {
		s.app.log.Printf("设置已保存（无变更）")
	} else {
		parts := changed
		if ttlNote != "" {
			parts = append(parts, fmt.Sprintf("登录有效期 %s", ttlText(ttlVal)))
		}
		s.app.log.Printf("设置已更新: %s", strings.Join(parts, "、"))
	}

	msg := "设置已保存"
	if len(changed) > 0 {
		msg += "：" + strings.Join(changed, "、")
	}
	if req.IntervalMinutes != nil {
		msg += fmt.Sprintf("（检查间隔 %d 分钟，重启服务后完全生效）", cfg.IntervalMinutes)
	}
	msg += ttlNote
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

// ttlText 把分钟数说成人话，用于界面与日志提示。
// 三种特殊值：-1 永久、0 单次有效。
func ttlText(minutes int) string {
	switch {
	case minutes == permanentSessionTTL:
		return "永久（永不过期）"
	case minutes == singleUseSessionTTL:
		return "单次有效（关闭浏览器即失效）"
	case minutes < 60:
		return fmt.Sprintf("%d 分钟", minutes)
	case minutes%1440 == 0:
		return fmt.Sprintf("%d 天", minutes/1440)
	case minutes%60 == 0:
		return fmt.Sprintf("%d 小时", minutes/60)
	default:
		return fmt.Sprintf("%d 分钟（约 %.1f 小时）", minutes, float64(minutes)/60)
	}
}

// handleSettingsTest 测试 qBittorrent 连接（可用临时参数，不保存）
func (s *server) handleSettingsTest(w http.ResponseWriter, r *http.Request) {
	var req SettingsPayload
	if r.Method == "POST" {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	cfg := s.app.cfg.Qbit
	if req.QbitHost != nil || req.QbitPort != nil {
		host, port := splitQbitURL(cfg.URL)
		if req.QbitHost != nil && strings.TrimSpace(*req.QbitHost) != "" {
			host = strings.TrimSpace(*req.QbitHost)
		}
		if req.QbitPort != nil && *req.QbitPort > 0 {
			port = *req.QbitPort
		}
		cfg.URL = buildQbitURL(host, port)
	}
	if req.QbitAPIKey != nil && strings.TrimSpace(*req.QbitAPIKey) != "" {
		cfg.APIKey = strings.TrimSpace(*req.QbitAPIKey)
	}

	start := time.Now()
	q := NewQbit(cfg, s.app.cfg.UA)
	ver, err := q.Version()
	if err != nil {
		writeJSON(w, 200, map[string]any{
			"ok":    false,
			"error": err.Error(),
			"url":   cfg.URL,
		})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"version": ver,
		"url":     cfg.URL,
		"ms":      time.Since(start).Milliseconds(),
	})
}

// ---- 辅助函数 ----

// splitQbitURL 从 "http://192.168.50.17:8888" 拆出 host 和 port
func splitQbitURL(u string) (string, int) {
	u = strings.TrimSpace(strings.TrimRight(u, "/"))
	host := u
	port := 8080
	// 去掉协议头
	if i := strings.Index(u, "://"); i >= 0 {
		host = u[i+3:]
	}
	// 拆端口
	if i := strings.LastIndex(host, ":"); i >= 0 {
		if p, err := strconv.Atoi(host[i+1:]); err == nil {
			port = p
			host = host[:i]
		}
	}
	return host, port
}

// buildQbitURL 拼回 URL。默认用 http 协议（局域网内足够）。
func buildQbitURL(host string, port int) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimRight(host, "/")
	return fmt.Sprintf("http://%s:%d", host, port)
}

// maskKey 给 API Key 打码，只留头尾
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 10 {
		return strings.Repeat("*", len(k))
	}
	return k[:6] + strings.Repeat("*", 8) + k[len(k)-4:]
}

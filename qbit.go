package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Qbit 是 qBittorrent WebAPI 客户端（兼容 5.x API Key 与 4.x 用户名密码）
type Qbit struct {
	cfg    QbitConfig
	client *http.Client
	ua     string
	cookie string // 4.x 登录后的 SID
}

func NewQbit(cfg QbitConfig, ua string) *Qbit {
	return &Qbit{
		cfg:    cfg,
		ua:     ua,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (q *Qbit) do(method, path string, body io.Reader, ctype string) (*http.Response, error) {
	req, err := http.NewRequest(method, q.cfg.URL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", q.ua)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if q.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+q.cfg.APIKey)
	} else if q.cookie != "" {
		req.Header.Set("Cookie", "SID="+q.cookie)
	}
	return q.client.Do(req)
}

// Login 4.x 兼容路径：用户名密码换 SID。有 APIKey 时不需要。
func (q *Qbit) Login() error {
	if q.cfg.APIKey != "" || q.cfg.Username == "" {
		return nil
	}
	form := url.Values{}
	form.Set("username", q.cfg.Username)
	form.Set("password", q.cfg.Password)
	resp, err := q.do("POST", "/api/v2/auth/login", strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("qBittorrent 登录失败: HTTP %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "SID" {
			q.cookie = c.Value
			return nil
		}
	}
	return fmt.Errorf("qBittorrent 登录未返回 SID")
}

func (q *Qbit) Version() (string, error) {
	resp, err := q.do("GET", "/api/v2/app/version", nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("查询版本失败: HTTP %d（检查 API Key 是否正确）", resp.StatusCode)
	}
	return strings.TrimSpace(string(b)), nil
}

// Torrent 是 /torrents/info 里我们关心的字段
type Torrent struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	SavePath   string  `json:"save_path"`
	Category   string  `json:"category"`
	State      string  `json:"state"`
	Progress   float64 `json:"progress"`
	Size       int64   `json:"size"`
	Downloaded int64   `json:"downloaded"`
	DLSSpeed   int64   `json:"dlspeed"`
	UPSpeed    int64   `json:"upspeed"`
	ETA        int64   `json:"eta"`
	Ratio      float64 `json:"ratio"`
	AddedOn    int64   `json:"added_on"`
	NumSeeds   int     `json:"num_seeds"`
	NumLeechs  int     `json:"num_leechs"`
}

func (q *Qbit) List() ([]Torrent, error) {
	resp, err := q.do("GET", "/api/v2/torrents/info", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("拉取任务列表失败: HTTP %d", resp.StatusCode)
	}
	var out []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析任务列表失败: %w", err)
	}
	return out, nil
}

// Hashes 返回现有任务的 hash 集合（小写）
func (q *Qbit) Hashes() (map[string]Torrent, error) {
	list, err := q.List()
	if err != nil {
		return nil, err
	}
	m := make(map[string]Torrent, len(list))
	for _, t := range list {
		m[strings.ToLower(t.Hash)] = t
	}
	return m, nil
}

// AddTorrent 上传 .torrent 文件内容。
// 返回新任务的 info hash（拿不到时返回空串）。
// 注意：qBittorrent 5.x 的 add 接口会忽略 rename 字段，重命名需调用 RenameFiles。
func (q *Qbit) AddTorrent(fileName string, data []byte, savePath, category string, paused bool) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fw, err := mw.CreateFormFile("torrents", fileName)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	_ = mw.WriteField("savepath", savePath)
	if category != "" {
		_ = mw.WriteField("category", category)
	}
	if paused {
		_ = mw.WriteField("paused", "true")
		_ = mw.WriteField("stopped", "true")
	}
	_ = mw.WriteField("autoTMM", "false")
	_ = mw.WriteField("contentLayout", "Original")
	mw.Close()

	return q.add("torrents/add", &buf, mw.FormDataContentType())
}

// AddMagnet 用磁力添加
func (q *Qbit) AddMagnet(magnet, savePath, category string, paused bool) (string, error) {
	form := url.Values{}
	form.Set("urls", magnet)
	form.Set("savepath", savePath)
	if category != "" {
		form.Set("category", category)
	}
	if paused {
		form.Set("paused", "true")
		form.Set("stopped", "true")
	}
	form.Set("autoTMM", "false")
	return q.add("torrents/add", strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded")
}

func (q *Qbit) add(path string, body io.Reader, ctype string) (string, error) {
	resp, err := q.do("POST", "/api/v2/"+strings.TrimPrefix(path, "/"), body, ctype)
	if err != nil {
		return "", fmt.Errorf("提交任务失败: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	// 409 = 该种子已存在于 qBittorrent（ani-rss 或本程序之前加过），
	// 这不是失败，当作"已在库中"处理，避免下轮无限重试。
	if resp.StatusCode == 409 {
		return "", ErrAlreadyExists
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("提交任务失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	// qBittorrent 5.x 返回 JSON；4.x 返回纯文本 "Ok." / "Fails."
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, "{") {
		var r struct {
			SuccessCount int      `json:"success_count"`
			FailureCount int      `json:"failure_count"`
			PendingCount int      `json:"pending_count"`
			AddedIDs     []string `json:"added_torrent_ids"`
		}
		if json.Unmarshal(b, &r) == nil {
			if r.SuccessCount == 0 && r.FailureCount > 0 {
				return "", ErrAlreadyExists
			}
			if len(r.AddedIDs) > 0 {
				return strings.ToLower(r.AddedIDs[0]), nil
			}
		}
		return "", nil
	}
	if s := strings.TrimSpace(string(b)); s == "Fails." {
		return "", ErrAlreadyExists
	}
	return "", nil
}

// ErrAlreadyExists 表示该种子已在 qBittorrent 中
var ErrAlreadyExists = errors.New("种子已存在于 qBittorrent")

// DeleteTorrents 删除任务。
//
// ⚠️⚠️ deleteFiles=true 会**立即 unlink 磁盘上的源文件**，不经过 UGOS 回收站，
// ext4 下无法恢复。这是整个程序里唯一会删除文件的地方。
// 只在用户于 Web UI 显式勾选"同时删除本地文件"时才允许调用，
// 且调用方必须先用 MatchTasks 精确匹配该订阅自己的任务，绝不能全库乱删。
func (q *Qbit) DeleteTorrents(hashes []string, deleteFiles bool) error {
	if len(hashes) == 0 {
		return nil
	}
	form := url.Values{}
	form.Set("hashes", strings.Join(hashes, "|"))
	if deleteFiles {
		form.Set("deleteFiles", "true")
	} else {
		form.Set("deleteFiles", "false")
	}
	resp, err := q.do("POST", "/api/v2/torrents/delete",
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return fmt.Errorf("删除任务失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != 200 {
		return fmt.Errorf("删除任务失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// FileEntry 是 /torrents/files 里的一项
type FileEntry struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

func (q *Qbit) Files(hash string) ([]FileEntry, error) {
	resp, err := q.do("GET", "/api/v2/torrents/files?hash="+url.QueryEscape(hash), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("查询种子文件列表失败: HTTP %d", resp.StatusCode)
	}
	var out []FileEntry
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析文件列表失败: %w", err)
	}
	return out, nil
}

// RenameFiles 把种子内的主视频文件重命名成 baseName（自动保留原扩展名）。
// 只重命名最大的那个视频文件；字幕/样片等附属文件保持原样。
func (q *Qbit) RenameFiles(hash, baseName string) error {
	files, err := q.Files(hash)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	// 选体积最大的文件当正片
	main := files[0]
	for _, f := range files {
		if f.Size > main.Size {
			main = f
		}
	}
	ext := ""
	if i := strings.LastIndex(main.Name, "."); i > 0 {
		ext = main.Name[i:]
	}
	newName := baseName + ext
	if newName == main.Name {
		return nil
	}

	form := url.Values{}
	form.Set("hash", hash)
	form.Set("oldPath", main.Name)
	form.Set("newPath", newName)
	resp, err := q.do("POST", "/api/v2/torrents/renameFile",
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return fmt.Errorf("重命名失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != 200 {
		return fmt.Errorf("重命名失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if strings.Contains(string(body), "Fails") {
		return fmt.Errorf("qBittorrent 拒绝重命名: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func fetchBytes(rawURL, ua string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载种子失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("下载种子失败 HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

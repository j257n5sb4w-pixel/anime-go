package main

// mikan 封面抓取。
//
// 为什么不用 TMDB 当封面源：
//   TMDB 把同一部番的多季合成一个剧集条目，只有一张总海报，于是
//   「某番」和「某番 第二季」在订阅页上长得一模一样，分不清谁是谁。
//   mikan 是逐季建条目的（每个 bangumiId 各一张海报），天然一季一图。
//
// 拿图方式：番剧详情页的 div.bangumi-poster 里是 background-image: url(...)，
// 形如 /images/Bangumi/202610/fd74ed32.jpg。原图是 jpg，但**带上
// ?format=webp 会返回 image/webp**，所以这里只加宽高、不加 format，
// 保证拿到的还是 image/jpeg（实测 magic 是 ff d8 ff）。

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mikanCoverSize 是向 mikan 请求封面时带的宽高。
// 400x560 约 54 KB，和原来 TMDB w500 的体积、比例都接近。
const mikanCoverSize = "?width=400&height=560"

var mikanCoverClient = &http.Client{Timeout: 25 * time.Second}

// coverSourceOf 取封面的主来源，默认 mikan。
func (a *App) coverSourceOf() string {
	src := ""
	if a != nil && a.cfg != nil {
		src = strings.ToLower(strings.TrimSpace(a.cfg.CoverSource))
	}
	if src == "tmdb" {
		return "tmdb"
	}
	return "mikan"
}

// coverFetchable 判断当前配置下是否值得尝试抓封面。
//
// 默认来源是 mikan，从 mikan 取海报既不需要 API Key 也不需要额外配置，
// 所以绝大多数情况下都是 true。只有把封面来源显式切成 tmdb 而又没填
// TMDB Key 时才没意义（那种配置下任何请求都会返回「未配置 TMDB API Key」）。
//
// 之前新建/编辑订阅处直接拿 tmdbKeyOf(cfg) != "" 做前置条件，导致
// 没配 TMDB Key 的用户订阅后永远拿不到封面（封面功能已改为 mikan 后
// 这个条件就过期了）。
func coverFetchable(a *App) bool {
	if a == nil || a.cfg == nil {
		return false
	}
	if a.coverSourceOf() == "tmdb" && tmdbKeyOf(a.cfg) == "" {
		return false
	}
	return true
}

// bangumiIDOf 从订阅的 RSS 地址里取出 mikan 番号。
// RSS 形如 https://mikanime.tv/RSS/Bangumi?bangumiId=4114&subgroupid=202
func bangumiIDOf(sub *Subscription) string {
	if sub == nil {
		return ""
	}
	if m := reBangumi.FindStringSubmatch(sub.URL); m != nil {
		return m[1]
	}
	return ""
}

// downloadImageURL 把一个完整 URL 的图片存到本地，返回字节数。
// 与 downloadCover 的区别：那个只认 TMDB 的 poster_path，这个认任意 URL。
func downloadImageURL(u, destPath string, ua string) (int, error) {
	if strings.TrimSpace(u) == "" {
		return 0, fmt.Errorf("没有封面地址")
	}
	if ua == "" {
		ua = "anime-go/0.2 (cover)"
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", "https://mikanime.tv/")
	resp, err := mikanCoverClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载封面失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("下载封面失败: HTTP %d", resp.StatusCode)
	}
	// 只接受位图，避免把 HTML 错误页当封面存下来
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "image/") {
		return 0, fmt.Errorf("下载封面失败: 返回的不是图片（%s）", ct)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, fmt.Errorf("创建封面目录失败: %w", err)
	}
	tmp := destPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, 8<<20))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		if copyErr != nil {
			return 0, copyErr
		}
		return 0, closeErr
	}
	if n < 512 {
		os.Remove(tmp)
		return 0, fmt.Errorf("封面文件过小（%d 字节），可能不是图片", n)
	}
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return int(n), nil
}

// fetchCoverMikan 从 mikan 番剧页抓这一季自己的海报。
//
// 只改 Cover / CoverName / CoverFrom，**不动 TMDBID**：
// 「在 TMDB 打开」那个跳转链接仍然要能点，它和封面来源是两件事。
func (a *App) fetchCoverMikan(sub *Subscription) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("订阅为空")
	}
	id := bangumiIDOf(sub)
	if id == "" {
		return "", fmt.Errorf("订阅地址里没有 bangumiId，无法从 mikan 取封面")
	}
	b, err := MikanBangumiByID(a.cfg, id)
	if err != nil {
		return "", err
	}
	if b == nil || strings.TrimSpace(b.Cover) == "" {
		return "", fmt.Errorf("mikan 番剧页上没有找到封面（%s）", id)
	}
	u := b.Cover
	// 去掉已有的 query，再统一加上只含宽高的参数。
	// 带 format=webp 会拿回 webp，存成 .jpg 就名不副实了。
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	u += mikanCoverSize

	path := a.coverPath(sub.ID)
	n, err := downloadImageURL(u, path, mikanUA(a.cfg))
	if err != nil {
		return "", err
	}
	sub.Cover = filepath.Base(path)
	sub.CoverName = strings.TrimSpace(b.Title)
	sub.CoverFrom = "mikan"
	return fmt.Sprintf("封面已更新：%s（mikan，%d KB）", sub.CoverName, n/1024), nil
}

// fillEpTotalFromTMDB 顺带把总集数补上。
//
// 封面上显示的是「已有/总数」，总数只有 TMDB 给得准（mikan 页面上没有
// 明确的总集数）。这一步失败**不影响封面**，所以只返回是否成功。
//
// 每次都重算而不是「已有值就跳过」：早期版本误把整条目全季合计
// （幼女战记 24）写进了单季的 EpTotal，必须能自愈回 12。
// 只有拿到有效值（>0）才覆盖，拿不到就保留原值，避免把手填的上限擦掉。
func (a *App) fillEpTotalFromTMDB(sub *Subscription) bool {
	if sub == nil || tmdbKeyOf(a.cfg) == "" {
		return sub != nil && sub.EpTotal > 0
	}
	var best *TMDBResult
	if sub.TMDBID > 0 {
		if d, err := TMDBDetail(a.cfg, sub.TMDBID); err == nil && d != nil {
			best = d
		}
	}
	if best == nil {
		query := cleanSearchTitle(sub.Title)
		if query == "" {
			query = strings.TrimSpace(sub.Title)
		}
		results, err := TMDBSearchTV(a.cfg, query)
		if err != nil || len(results) == 0 {
			return sub.EpTotal > 0
		}
		best = TMDBPickBest(results, sub.Title, sub.Season)
	}
	if best == nil {
		return sub.EpTotal > 0
	}
	// 搜索结果里没有 seasons[]，补一次详情才能按季取集数；
	// 否则多季番只能退回全季合计，又会把 12 写成 24。
	if len(best.Seasons) == 0 {
		if d, derr := TMDBDetail(a.cfg, best.ID); derr == nil && d != nil {
			best = d
		}
	}
	if sub.TMDBID <= 0 {
		sub.TMDBID = best.ID
	}
	if n := EpisodesOfSeason(best, sub.Season); n > 0 {
		sub.EpTotal = n
	}
	if sub.CoverYear == "" {
		sub.CoverYear = firstAirYear(best.FirstAirDate)
	}
	return sub.EpTotal > 0
}

// fillMissingEpTotals 在启动后补一轮「每季总集数」。
//
// 只处理 EpTotal 为空的订阅（applyDefaults 的 EpTotalMigrated 会把历史错误值清掉），
// 拿到有效值就回填。封面不动 —— 换图是用户显式点「补抓」的语义。
//
// 无论是否补到，最后都会 SaveConfig 一次：EpTotalMigrated 这类一次性迁移
// 只在内存里生效过，不落盘的话下次启动又会把已经纠正好的值当成「待迁移」。
func (a *App) fillMissingEpTotals() {
	a.mu.Lock()
	targets := make([]Subscription, 0, len(a.cfg.Subscriptions))
	if tmdbKeyOf(a.cfg) != "" {
		for i := range a.cfg.Subscriptions {
			s := a.cfg.Subscriptions[i]
			// 用户手填了上限就尊重它，不再覆盖
			if s.TotalEpisodes > 0 || s.EpTotal > 0 {
				continue
			}
			targets = append(targets, s)
		}
	}
	a.mu.Unlock()

	filled := 0
	for i := range targets {
		sub := targets[i]
		a.mu.Lock()
		ok := a.fillEpTotalFromTMDB(&sub)
		if ok {
			if cur := a.cfg.FindSub(sub.ID); cur != nil {
				cur.EpTotal = sub.EpTotal
				cur.TMDBID = sub.TMDBID
				if cur.CoverYear == "" {
					cur.CoverYear = sub.CoverYear
				}
			}
		}
		a.mu.Unlock()
		if ok {
			filled++
			a.log.Printf("[%s] 已补齐每季总集数：%d", sub.Title, sub.EpTotal)
		}
	}

	a.mu.Lock()
	if err := SaveConfig(a.cfgPath, a.cfg); err != nil {
		a.log.Printf("回写配置失败: %v", err)
	}
	a.mu.Unlock()
	if filled > 0 {
		a.log.Printf("每季总集数补齐完成：%d 条", filled)
	}
}

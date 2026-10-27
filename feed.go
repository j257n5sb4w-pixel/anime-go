package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RSS 2.0 结构（mikan / anibt 都在这个形状内）
type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title     string     `xml:"title"`
	Link      string     `xml:"link"`
	Desc      string     `xml:"description"`
	GUID      string     `xml:"guid"`
	PubDate   string     `xml:"pubDate"`
	Enclosure *enclosure `xml:"enclosure"`
	// mikan 扩展节点，用于拿到真实发布时间
	Torrent struct {
		PubDate string `xml:"pubDate"`
	} `xml:"torrent"`
}

type enclosure struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Length string `xml:"length,attr"`
}

// FeedItem 是解析后的条目
type FeedItem struct {
	Title     string
	Torrent   string // .torrent 直链
	Magnet    string // 部分源（anibt）只给磁力
	InfoHash  string
	Link      string
	PubTime   time.Time
	SizeBytes int64
}

func (f FeedItem) Key() string {
	if f.InfoHash != "" {
		return "hash:" + strings.ToLower(f.InfoHash)
	}
	if f.Torrent != "" {
		return "url:" + f.Torrent
	}
	return "title:" + f.Title
}

// httpClient 走 proxy.go 的共享 Transport：设置里的代理对它即时生效。
var httpClient = newHTTPClient(25 * time.Second)

func fetchFeed(url, ua string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 RSS 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("读取 RSS 失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("RSS 返回 %d: %s", resp.StatusCode, url)
	}
	return string(body), nil
}

// ParseFeed 解析 RSS 文本
func ParseFeed(body string) (channelTitle string, items []FeedItem, err error) {
	var doc rssDoc
	// mikan 的 XML 里含裸 & 符号时会解析失败，做一次宽松替换
	safe := sanitizeXML(body)
	if e := xml.Unmarshal([]byte(safe), &doc); e != nil {
		return "", nil, fmt.Errorf("解析 RSS XML 失败: %w", e)
	}
	channelTitle = strings.TrimSpace(strings.TrimPrefix(doc.Channel.Title, "Mikan Project - "))

	for _, it := range doc.Channel.Items {
		fi := FeedItem{
			Title: strings.TrimSpace(it.Title),
			Link:  strings.TrimSpace(it.Link),
		}
		if it.Enclosure != nil && it.Enclosure.URL != "" {
			fi.Torrent = it.Enclosure.URL
			fi.SizeBytes = parseInt64(it.Enclosure.Length)
		}
		// 磁力可能在 description / link 里
		for _, cand := range []string{it.Link, it.Desc, it.GUID, it.Title} {
			if m := extractMagnet(cand); m != "" {
				fi.Magnet = m
				fi.InfoHash = extractInfoHash(m)
				break
			}
		}
		fi.PubTime = parsePubTime(it.Torrent.PubDate, it.PubDate)
		items = append(items, fi)
	}
	return channelTitle, items, nil
}

func parsePubTime(candidates ...string) time.Time {
	layouts := []string{
		time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
		"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05",
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		for _, l := range layouts {
			if t, err := time.Parse(l, c); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// sanitizeXML 把非法裸 & 换成 &amp;（已经是实体的不动）
func sanitizeXML(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 64)
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			// 检查是否已是合法实体
			rest := s[i:]
			legal := false
			for _, ent := range []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;", "&#"} {
				if strings.HasPrefix(rest, ent) {
					legal = true
					break
				}
			}
			if !legal {
				b.WriteString("&amp;")
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func parseMagnetIn(s string) string { return extractMagnet(s) }

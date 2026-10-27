package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// QbitConfig 是 NAS 上 qBittorrent 的连接参数
type QbitConfig struct {
	URL      string `json:"url"`      // 例：http://192.168.50.17:8888
	APIKey   string `json:"apikey"`   // qBittorrent 5.x 的 API Key（Bearer 鉴权）
	Username string `json:"username"` // 老版本走 cookie 登录时用
	Password string `json:"password"`
	Category string `json:"category"` // 任务分类
}

// Subscription 是一条番剧订阅
type Subscription struct {
	ID        string `json:"id"`
	Title     string `json:"title"`      // 目录名，如 "胆大党 (2026)"
	FileTitle string `json:"file_title"` // 文件名里的番名，留空则自动去掉年份
	URL       string `json:"url"`        // mikan RSS 地址
	Season    int    `json:"season"`

	Subgroup string `json:"subgroup"` // 用于 {{subgroup}} 替换

	// Match 全部命中才算；支持 ani-rss 的 "{{字幕组}}:regex" 写法
	Match []string `json:"match"`
	// Exclude 任意命中即跳过
	Exclude []string `json:"exclude"`
	// EpPatterns 自定义集数正则（留空用内置规则），第一个分组为集数
	EpPatterns []string `json:"ep_patterns"`

	TotalEpisodes  int    `json:"total_episodes"`  // 0 = 不限
	RenameTemplate string `json:"rename_template"` // 留空用 "[{{subgroup}}] {{filetitle}} S{{season}}E{{ep}}"
	SavePath       string `json:"save_path"`       // 留空用默认模板
	Enabled        bool   `json:"enabled"`

	// WeekLabel 该番的更新星期（"星期一"…"星期日"），留空则按上次更新时间推断。
	// 用于首页「今日订阅」展示。
	WeekLabel string `json:"week_label"`

	// SkipBackfill 首次订阅时是否跳过历史回填。
	// 默认 false = 把 RSS 里现存的剧集全部下载（用户 2026-10-26 要求）。
	// 设为 true 则只记录基线不下载，等后续新集。
	// 用"跳过"而非"回填"做字段名，是为了让 bool 零值等于默认行为。
	SkipBackfill bool `json:"skip_backfill"`

	// ---- TMDB 封面 ----
	// Cover 本地封面文件名（在 covers/ 目录下），空表示还没抓到。
	Cover string `json:"cover"`
	// TMDBID 匹配到的 TMDB 剧集 id，0 表示未匹配。
	TMDBID int `json:"tmdb_id"`
	// CoverName / CoverYear 是封面对应的中文名与首播年，仅用于界面展示。
	CoverName string `json:"cover_name"`
	CoverYear string `json:"cover_year"`
	// CoverFrom 记封面是从哪来的：mikan / tmdb / bgm / url（手动贴的）/ 空（老数据）。
	// 老订阅没有这个字段，界面上按「未知来源」处理，不影响展示。
	CoverFrom string `json:"cover_from"`

	// BangumiID 是匹配到的 bgm.tv 条目 id（0 = 未匹配）。
	//
	// 与 TMDBID 并存而不是二选一：两个源各有所长（TMDB 海报好、bgm.tv
	// 中文名准），用户可能在 UI 里分别指定。指定过的条目会被尊重——
	// 下次抓取直接用它，不再按名字重搜（否则改一条、后台又搜回原来那条）。
	BangumiID int `json:"bangumi_id,omitempty"`

	// NameSource 记录订阅名取自哪一侧："" 或 "mikan"（默认，用 mikan 的番名）
	// 或 "tmdb"（用 TMDB 的中文名）。
	//
	// 为什么要存下来：mikan 的番名常常带「第二季」「/ 续篇」这类站点自己的写法，
	// 而 TMDB 的中文名更干净；但也有人就认 mikan 那一套。这只影响 Title，
	// 老配置没有这个字段时按 mikan 处理（零值即默认）。
	NameSource string `json:"name_source"`
	// MikanName 记录当时的 mikan 番名，切到 TMDB 名之后还能切回来，
	// 也让「这个订阅原来叫什么」有据可查。
	MikanName string `json:"mikan_name"`

	// EpTotal 是 TMDB 报告的这部番的总集数（封面抓取时顺带记下）。
	// 与 TotalEpisodes 的区别：TotalEpisodes 是用户手填的上限（0=不限），
	// EpTotal 是「这部番一共多少集」这个客观事实，只用于界面显示进度。
	EpTotal int `json:"ep_total"`

	// EpOffset 是剧集号偏移，作用在**写进文件名/用于上限判断**的集号上。
	//
	// 为什么需要：日漫第二季常接着第一季连续编号（第 13 集其实是本季第 1 集），
	// 直接重命名成 S02E13 会和 TMDB 的刮削数据对不上（TMDB 里第 2 季是 E01…）。
	// 填 -12 就把它纠正成 S02E01。
	//
	// ⚠ 去重与进度记录仍用字幕组的原始集号，只有展示/落盘用偏移后的值，
	// 这样改偏移不会让已经推过的集被当成新集重推一遍。
	EpOffset int `json:"ep_offset"`

	// DirName 是「实际落盘用的番名目录名」。
	//
	// 为什么单独存一个字段：同一部番的第 1/2/3 季要落在同一个番名目录下
	// （用户 2026-10-26 要求），但目录名只能有一个——先订阅的那季决定用哪个名字，
	// 后订阅的季复用同一个目录。这个字段就是把解析结果**固化**下来，
	// 保证推送写进去的路径、删除时算出来的路径、文件列表查的路径三者永远一致
	// （render.go 顶部那段「路径必须单一出口」的教训）。
	//
	// 为空 = 还没解析过，用 Title 当目录名（老行为）。
	DirName string `json:"dir_name"`

	// Movie 标记这是一条「剧场版」订阅（用户 2026-10-27 要求）。
	//
	// 剧场版与 TV 番的三点根本区别，全部由这个标记驱动：
	//  1. 不做集数识别：标题里没有「- 03」这类集号，硬抽会把
	//     「[BDRemux 1080p]」里的数字当成集数；
	//  2. 只推一次：推成功一条就收工，不再按小时轮询更新
	//     （state.SubState.MovieDone 记账）；
	//  3. 落盘位置单独一套：默认 /media/剧场版/<番名>，与追更库分开，
	//     对应模板见 Config.MovieSavePath / Config.MovieRename。
	Movie bool `json:"movie,omitempty"`

	// NoUpdate 表示这条订阅下过一次就不再更新（用户 2026-10-27 要求）。
	//
	// 与 Movie 的关系：Movie 蕴含 NoUpdate，反之不然。
	//  - 剧场版：Movie=true，落盘去剧场版目录、不识别集数；
	//  - 连载中的番想「下完这集就停」：NoUpdate=true，目录和集数识别照旧。
	// 行为判断统一用 NoUpdateNow()，不要直接读 Movie（否则 NoUpdate 会失效）。
	NoUpdate bool `json:"no_update,omitempty"`
}

// NoUpdateNow 报告这条订阅是否「一次性」：推成功一条就不再检查更新。
//
// 剧场版天然是一次性的；普通番也可以手动勾成一次性。
// 所有「要不要停更 / 要不要跳过基线 / 要不要补推历史」的判断都应走它。
func (s *Subscription) NoUpdateNow() bool {
	if s == nil {
		return false
	}
	return s.Movie || s.NoUpdate
}

type Config struct {
	Listen          string `json:"listen"`
	IntervalMinutes int    `json:"interval_minutes"`
	DryRun          bool   `json:"dry_run"` // true 只记录不推送
	StateFile       string `json:"state_file"`
	LogFile         string `json:"log_file"`
	UA              string `json:"ua"`
	// MikanHost 是搜索/番剧页用的站点地址，留空用 https://mikanime.tv
	MikanHost string `json:"mikan_host"`
	// TMDBKey 是 TMDB v3 API Key，用于抓封面；留空则不抓封面。
	TMDBKey string `json:"tmdb_key"`
	// TMDBHost 是 TMDB API 域名，留空用 https://api.tmdb.org
	// （themoviedb.org 在部分网络下被污染，实测 api.tmdb.org 可用）
	TMDBHost      string         `json:"tmdb_host"`
	Qbit          QbitConfig     `json:"qbit"`
	Subscriptions []Subscription `json:"subscriptions"`
	// AllowWritePrefixes 显式声明允许写入的路径前缀。
	// 留空 = 用内置默认（只放行 /media/新番追更/ 追更目录）。
	// 要写别的位置必须在配置文件里写出来（这是有意的摩擦）。
	AllowWritePrefixes []string `json:"allow_write_prefixes"`
	// DownloadsMount 是把 qBittorrent 那边的下载根目录挂到本机的位置。
	// 例如 qB 的 /downloads 通过 SMB 挂到本机 /mnt/dl，这里就写 /mnt/dl。
	// 留空 = 本机看不到下载文件，删除订阅时不会去删文件夹。
	//
	// ⚠ 这两个字段现在只是 Mounts[0] 的 legacy 镜像（老配置兼容用）。
	// 新配置请用 Mounts，它支持多个 qB 侧根目录。
	DownloadsMount string `json:"downloads_mount"`
	// DownloadsRoot 是 qBittorrent 侧的下载根目录前缀，默认 /downloads。
	// 映射规则：qB 的 /downloads/a/b -> 本机的 <DownloadsMount>/a/b
	DownloadsRoot string `json:"downloads_root"`

	// Mounts 是 qB 路径到本机挂载点的映射表，支持多条。
	//
	// 为什么需要多条：追更文件现在落在 qB 的 /media/新番追更，而历史遗留的
	// 老订阅可能还在 /downloads/anime-go。qB 容器同时挂了
	// /volume1/dl→/downloads 与 /volume1/media→/media，本机就要分别挂
	// /mnt/dl 与 /mnt/media 才能都看见。删除订阅时按最长前缀匹配。
	Mounts []PathMount `json:"mounts"`

	// SessionTTLMinutes 是登录有效期（分钟）。
	//
	// 用指针是为了区分「没配过」和「显式填 0」——两者在 int 里都是 0，
	// 但含义完全不同：
	//   nil          = 老配置没这个字段 → 按默认 120 分钟
	//   -1           = 永久，登录一次一直有效
	//   0            = 单次有效（会话 Cookie，关浏览器即失效）
	//   >0           = 这么多分钟后失效
	SessionTTLMinutes *int `json:"session_ttl_minutes"`

	// DefaultMatch / DefaultExclude 是全局默认的下载过滤正则。
	// 订阅里留空时就用这里的值；订阅里自己写了则以订阅为准。
	// 这样换季、换压制规格时只需要在设置里改一次，不用每部番逐个改。
	DefaultMatch   []string `json:"default_match"`
	DefaultExclude []string `json:"default_exclude"`

	// 老版本把默认过滤规则硬编码进了每条订阅，这里记录是否已做过一次性迁移。
	RulesMigrated bool `json:"rules_migrated"`

	// 老版本的落盘位置是 /downloads/anime-go/...（下载盘的临时区）。
	// 2026-10-26 起用户要求追更直接落到正式库 /media/新番追更/...，
	// 这里记录是否已把「沿用老默认路径」的订阅改写到新位置。
	// 打标记而不是每次都比对：用户之后手动改回老路径是有意为之，不该被再改走。
	SavePathMigrated bool `json:"save_path_migrated"`

	// LibraryRoot 是「追更库」的根目录（qBittorrent 侧的路径）。
	//
	// 用途：解析「同名番名目录」时要知道去哪个目录下列子目录。
	// 之所以独立于 save_path 模板：模板是每条订阅自己的字符串，
	// 从模板里反推根目录既不可靠也没必要——根目录全库只有一个。
	LibraryRoot string `json:"library_root"`

	// 老版本的默认 save_path 用 {{title}}（订阅名）当目录名，
	// 导致同一部番的第 1、2 季各自建目录（「某番」与「某番 第二季」）。
	// 2026-10-26 起改为 {{dirname}}（去季后缀的番名），这里记录是否已迁移。
	DirNameMigrated bool `json:"dir_name_migrated"`

	// CoverSource 是封面主来源：mikan（默认）或 tmdb。
	//
	// 之所以默认 mikan：TMDB 把多季合成一个条目、只有一张总海报，
	// 订阅页上第 1 季和第 2 季的封会长得一模一样；mikan 是逐季建条目的，
	// 每个 bangumiId 各有一张，天然一季一图。
	CoverSource string `json:"cover_source"`

	// EpTotalMigrated 记录是否已清掉历史错误的「全季合计集数」。
	//
	// 早期版本把 TMDB number_of_episodes（整个条目所有季合计）当成某一季的
	// 总集数写入 Subscription.EpTotal，多季番因此显示错（幼女战记 2 季各 12 集，
	// 第 2 季却显示 24）。改为按季取集数后，历史值无法推算，只能清空重算。
	EpTotalMigrated bool `json:"ep_total_migrated"`

	// MovieSavePath 是**剧场版订阅**的默认保存路径模板（设置 → 下载）。
	//
	// 与追更库分开是用户明确要求：剧场版是整部电影一个文件，
	// 混在 /media/新番追更/<番名>/Season N 里既不对也应被刮削器当成剧集。
	// 留空 = 用内置默认 /media/剧场版/{{dirname}}。
	MovieSavePath string `json:"movie_save_path"`

	// MovieRename 是剧场版订阅的默认重命名模板（同样在设置 → 下载里改）。
	//
	// 不能沿用 TV 的 "[{{subgroup}}] {{filetitle}} S{{season}}E{{ep}}"：
	// 剧场版 ep 恒为 0，渲染出来是 "S01E00"，所有电影都变成"第0集"。
	// 留空 = 用内置默认 "[{{subgroup}}] {{filetitle}}"。
	MovieRename string `json:"movie_rename"`

	// Proxy 是出站代理（设置 → 代理）。
	// 影响 RSS、mikan 页面、封面、TMDB、qBittorrent 全部出站请求，
	// 见 proxy.go 顶部的说明（改完即时生效，不需要重启）。
	Proxy ProxySettings `json:"proxy"`

	// HostOverrides 是「域名 → IP」的自定义解析表（设置 → 源数据 → 手动 hosts）。
	//
	// 为什么不直接改系统 hosts：本程序在板子上由 systemd 托管、在 NAS 上跑容器，
	// 改 /etc/hosts 要 root、要重启、还要跟着备份走；而 DNS 污染是会变的目标
	// （今天污染 api.themoviedb.org，明天可能换成 image.tmdb.org），
	// 能在设置页当场改、改完下一个请求就生效，才是实际用得上的形态。
	//
	// 只在程序内部生效（拨号时替换 IP），系统里其它程序不受影响。
	HostOverrides []HostOverride `json:"host_overrides"`

	// ---- bgm.tv（Bangumi 番组计划）元数据源 ----
	//
	// 用户 2026-10-27 要求：TMDB 与 bangumi 合并成「源数据」栏目，
	// 里面能配 TMDB 的地址/Key，也能配 bangumi 的 API 地址与 Token，
	// 由它们负责抓中文番名、集数、海报。
	//
	// 为什么值得接：TMDB 的简体中文条目覆盖率不如 bgm.tv，
	// 而 bgm.tv 是中文动漫数据库，季番/剧场版都有独立条目与中文名。
	BangumiAPI string `json:"bangumi_api"`
	// BangumiToken 是 bgm.tv 的 Access Token（个人设置 → 开放接口 里申请）。
	// 搜索与条目查询不强制要 Token，但带上后限流更宽松；留空也能用。
	BangumiToken string `json:"bangumi_token"`
	// BangumiEnabled 已废弃，仅为兼容老配置保留：早期要求勾选开关才用 bgm.tv，
	// 现在只要 BangumiAPI 有值就可用（默认一定有），勾选框已从界面移除。
	// 字段留着是为了不把用户配置里的旧值打丢（未知字段会被拒绝写回）。
	BangumiEnabled bool `json:"bangumi_enabled,omitempty"`
}

// HostOverride 是一条「域名 → IP」的强制解析。
//
// IP 可以是 IPv4 也可以是 IPv6（IPv6 不用加方括号，拨号时按需拼）。
type HostOverride struct {
	Host string `json:"host"`
	IP   string `json:"ip"`
}

// PathMount 描述「qBittorrent 侧的根目录 → 本机挂载点」的一条映射。
// 例如 {Qbit: "/media", Local: "/mnt/media"}。
type PathMount struct {
	// Qbit 是 qBittorrent 容器内看到的路径前缀，必须是不带尾斜杠的绝对路径。
	Qbit string `json:"qbit"`
	// Local 是本机上对应的挂载点，留空表示这条映射无效。
	Local string `json:"local"`
}

const (
	defaultListen   = ":7788"
	defaultInterval = 60 // 每小时检查一次 RSS 有无新集（用户 2026-10-26 要求）
	defaultCategory = "anime-go"
	// 默认落盘位置：直接写进正式库的「新番追更」。
	// qBittorrent 容器同时挂了 /volume1/dl→/downloads 与 /volume1/media→/media，
	// 所以 qB 侧写 /media/新番追更/... 就是 NAS 上的 /volume1/media/新番追更/...。
	// 注意：这个目录**不在** safe 默认白名单里，必须由 allow_write_prefixes 显式放开。
	//
	// 命名按用户 2026-10-26 的要求用「番名/Season N」，不带年份。
	// ⚠ 2026-10-27 起把 {{title}} 换成 {{dirname}}：{{title}} 用的是订阅名，
	// 而同一部番的不同季订阅名不同（「某番」「某番 第二季」），会各自建一个目录；
	// {{dirname}} 是去掉季后缀的番名，多季才会落进同一个目录、各自建 Season 子目录。
	defaultSavePath = "/media/新番追更/{{dirname}}/Season {{season_n}}"
	defaultRename   = "[{{subgroup}}] {{filetitle}} S{{season}}E{{ep}}"
	// 旧版默认路径（用 {{title}}），仅用于一次性迁移识别。
	legacyTitleSavePath = "/media/新番追更/{{title}}/Season {{season_n}}"
	// qBittorrent 侧的下载根目录，用于把 qB 路径映射到本机挂载点。
	// 追更落到 /media 之后，qB 的「下载根」实际上就是 /（/media 与 /downloads 并列），
	// 但这里仍保留 /downloads 作为旧路径的映射根，见 qbPathToLocal 的多挂载点支持。
	defaultDownloadsRoot = "/downloads"
	// 旧版本的默认落盘位置，仅用于一次性迁移识别。
	legacyDefaultSavePath = "/downloads/anime-go/{{title}}/Season {{season_n}}"
	// 默认的「追更库根目录」（qBittorrent 侧路径）。
	// 解析同名番名目录时在这一级下列子目录。
	defaultLibraryRoot = "/media/新番追更"
	// 默认允许写入的前缀：追更正式库。
	// 注意必须带尾斜杠——isUnderPrefix 是整段比较，不带斜杠会把
	// 「新番追更备份」这类兄弟目录一起放行。
	defaultAllowPrefix = "/media/新番追更/"

	// 剧场版正式库：与追更库并列，剧场版订阅默认落在这里（用户 2026-10-27 指定）。
	// {{dirname}} 与追更库同义（去季后缀的番名），一部电影一个目录。
	defaultMovieSavePath = "/media/剧场版/{{dirname}}"
	// 剧场版文件名不带集号：ep 恒为 0，套 TV 模板会得到 "S01E00"。
	defaultMovieRename = "[{{subgroup}}] {{filetitle}}"
	// 剧场版目录的 allow 前缀。默认白名单里包含它（见 DefaultAllowPrefixes）。
	defaultMovieAllowPrefix = "/media/剧场版/"

	// bgm.tv 的 API 根地址（设置 → 源数据 → bangumi）。
	// 走 v0 接口：/v0/search/subjects 搜条目，/v0/subjects/{id} 取详情。
	defaultBangumiAPI = "https://api.bgm.tv"
)

// mikanMirrors 是设置页「bangumi 连接」下拉里自带的几个站点。
//
// 只列真实存在、且页面结构相同（/Home/Bangumi/...、/RSS/Bangumi?...）的域名：
// mikan 换过一次主域名（mikanani.me → mikanime.tv），旧域名长期并行可用，
// 国内网络下两者的可达性还不一样，所以值得让用户自己选。
// 第四个选项是「自定义」，见 ui.go 里的 s_mikanhost_other。
var mikanMirrors = []struct {
	Host string
	Note string
}{
	{"https://mikanime.tv", "官方主站"},
	{"https://mikanani.me", "旧域名（常作为备用）"},
}

// MovieSavePathValue 返回生效的剧场版保存路径模板（已去尾斜杠）。
func (c *Config) MovieSavePathValue() string {
	if c != nil {
		if s := strings.TrimRight(strings.TrimSpace(c.MovieSavePath), "/"); s != "" {
			return s
		}
	}
	return defaultMovieSavePath
}

// MovieRenameValue 返回生效的剧场版重命名模板。
func (c *Config) MovieRenameValue() string {
	if c != nil {
		if s := strings.TrimSpace(c.MovieRename); s != "" {
			return s
		}
	}
	return defaultMovieRename
}

// applyMovieDefaults 把「剧场版」专属的默认模板套到一条订阅上。
//
// 为什么必须单独抽出来：Config.applyDefaults 会在加载配置时做一次，
// 但新增/编辑订阅时只调了 sub.applyDefaults()（它不知道 Config），
// 于是新建的剧场版订阅会带着 TV 默认值（/media/新番追更/.../Season N、
// 带 S{{season}}E{{ep}} 的文件名）落库 —— 2026-10-27 冒烟测试实测踩到。
//
// 只改「仍是默认值」的字段：用户手写过的模板一律不动。
func (s *Subscription) applyMovieDefaults(c *Config) {
	if s == nil || !s.Movie {
		return
	}
	if sameSavePath(s.SavePath, defaultSavePath) {
		s.SavePath = c.MovieSavePathValue()
	}
	if strings.TrimSpace(s.RenameTemplate) == defaultRename {
		s.RenameTemplate = c.MovieRenameValue()
	}
}

// MovieAllowPrefix 返回剧场版落盘目录对应的 allow 前缀。
//
// 从模板里取「第一个占位符之前」的那段静态目录：
// "/media/剧场版/{{dirname}}" → "/media/剧场版/"。
// 用户在设置里把剧场版目录改到别处时，白名单也要跟着变，
// 否则默认路径会被自己的安全阀（ValidateSavePath）拦下来。
func (c *Config) MovieAllowPrefix() string {
	return templateStaticPrefix(c.MovieSavePathValue())
}

// templateStaticPrefix 取路径模板里不含占位符的最长目录前缀（带尾斜杠）。
// 例："/media/剧场版/{{dirname}}" → "/media/剧场版/"；"/media/x" → "/media/x/"。
func templateStaticPrefix(tpl string) string {
	t := strings.TrimSpace(tpl)
	if i := strings.Index(t, "{{"); i >= 0 {
		if j := strings.LastIndex(t[:i], "/"); j >= 0 {
			t = t[:j+1]
		} else {
			return ""
		}
	}
	t = strings.TrimRight(t, "/")
	if t == "" || !strings.HasPrefix(t, "/") {
		return ""
	}
	return t + "/"
}

// 默认过滤规则：只留 1080p 及以上的正片，排除 720p 与合集。
// 特意写得宽一些——早期版本默认带 `mp4|MP4`，遇到 HEVC/MKV 源会把整部番
// 静默挡掉（ani-4114 就是这么一集没下成的），所以现在默认不再限制封装格式。
var (
	defaultMatchRules   = []string{`(1080|2160)[Pp]?`}
	defaultExcludeRules = []string{`720[Pp]`, `合集`}
)

// 早期版本把默认规则直接写进了每条订阅（新增订阅表单里硬编码）。
// 于是「改全局默认」对老订阅毫无作用，而且那句 mp4|MP4 还会把 HEVC 源全部挡掉。
// 这里做一次性识别：规则与老默认完全一致的订阅，就当作「没配过」，改为继承全局默认。
var (
	legacyDefaultMatch   = []string{"1080[Pp]", "mp4|MP4"}
	legacyDefaultExclude = []string{"720[Pp]", "合集"}
)

func sameRuleList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != b[i] {
			return false
		}
	}
	return true
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = defaultListen
	}
	if c.IntervalMinutes <= 0 {
		c.IntervalMinutes = defaultInterval
	}
	if c.StateFile == "" {
		c.StateFile = "state.json"
	}
	if c.LogFile == "" {
		c.LogFile = "anime-go.log"
	}
	if c.UA == "" {
		c.UA = "Mozilla/5.0 (X11; Linux aarch64) anime-go/0.1"
	}
	if c.MikanHost == "" {
		c.MikanHost = defaultMikanHost
	}
	c.MikanHost = strings.TrimRight(c.MikanHost, "/")
	if c.TMDBHost == "" {
		c.TMDBHost = defaultTMDBHost
	}
	c.TMDBHost = strings.TrimRight(strings.TrimSpace(c.TMDBHost), "/")
	c.TMDBKey = strings.TrimSpace(c.TMDBKey)
	if c.Qbit.Category == "" {
		c.Qbit.Category = defaultCategory
	}
	if c.Qbit.URL != "" {
		c.Qbit.URL = strings.TrimRight(c.Qbit.URL, "/")
	}
	c.DownloadsMount = strings.TrimRight(strings.TrimSpace(c.DownloadsMount), "/")
	if c.DownloadsRoot == "" {
		c.DownloadsRoot = defaultDownloadsRoot
	}
	c.DownloadsRoot = strings.TrimRight(strings.TrimSpace(c.DownloadsRoot), "/")
	c.normalizeMounts()
	// 登录有效期归一：
	//   nil      → 默认 120 分钟（老配置没这个字段）
	//   < 0      → -1（永久）
	//   0        → 0（单次有效，原样保留，不能当"没配过"）
	//   > 0      → 原值
	if c.SessionTTLMinutes == nil {
		v := defaultSessionTTLMinutes
		c.SessionTTLMinutes = &v
	} else if *c.SessionTTLMinutes < 0 {
		v := permanentSessionTTL
		c.SessionTTLMinutes = &v
	}
	// 上限兜底：超过一年按一年算，避免 JSON 里塞进离谱数字
	if v := *c.SessionTTLMinutes; v > maxSessionTTLMinutes {
		nv := maxSessionTTLMinutes
		c.SessionTTLMinutes = &nv
	}
	// 全局默认过滤规则：没配过就给一套宽一点的默认值
	if c.DefaultMatch == nil {
		c.DefaultMatch = append([]string{}, defaultMatchRules...)
	}
	if c.DefaultExclude == nil {
		c.DefaultExclude = append([]string{}, defaultExcludeRules...)
	}
	// 只迁移一次：迁移完打上标记，之后用户在订阅里手写同样两条规则不会被再次清掉。
	if !c.RulesMigrated {
		for i := range c.Subscriptions {
			// 与老版本硬编码默认值一致的订阅，视为「沿用了全局默认」，
			// 清空后就会跟随设置里改过的全局规则。
			if sameRuleList(c.Subscriptions[i].Match, legacyDefaultMatch) {
				c.Subscriptions[i].Match = nil
			}
			if sameRuleList(c.Subscriptions[i].Exclude, legacyDefaultExclude) {
				c.Subscriptions[i].Exclude = nil
			}
		}
		c.RulesMigrated = true
	}
	// 落盘位置一次性迁移：把「沿用老默认路径」的订阅改到 /media/新番追更。
	// 同时确保 allow 前缀里有新目录，否则默认路径会被自己的安全阀拦下来。
	if !c.SavePathMigrated {
		for i := range c.Subscriptions {
			if sameSavePath(c.Subscriptions[i].SavePath, legacyDefaultSavePath) {
				c.Subscriptions[i].SavePath = defaultSavePath
			}
		}
		if !hasPrefixItem(c.AllowWritePrefixes, defaultAllowPrefix) {
			c.AllowWritePrefixes = append(c.AllowWritePrefixes, defaultAllowPrefix)
		}
		c.SavePathMigrated = true
	}

	// 目录名改为「去季后缀的番名」后的一次性迁移。
	//
	// 关键：老订阅的 DirName 直接固化成它**当前**的 Title，让渲染结果一位都不变。
	// 不这么做的话，「某番 第二季」这种老订阅会突然从
	// /media/新番追更/某番 第二季/Season 2 变成 /media/新番追更/某番/Season 2，
	// 已经下好的种子会被判定成「路径不匹配」，删除订阅时也找不到目录。
	//
	// 新订阅（DirName 为空）才走「扫磁盘找同名目录」的解析逻辑。
	if !c.DirNameMigrated {
		for i := range c.Subscriptions {
			s := &c.Subscriptions[i]
			if strings.TrimSpace(s.DirName) == "" {
				s.DirName = strings.TrimSpace(s.Title)
			}
			// 只改「仍是旧默认模板」的那一批，用户自己写的自定义模板一律不碰。
			//
			// 为什么不能无条件把 {{title}} 都换成 {{dirname}}：用户可能故意用
			// {{title}} 让每季分目录（例如「某番 第二季/Season 2」），
			// 替换掉会改变他已有的落盘结构。默认模板换掉是因为它本来就该合并。
			if sameSavePath(s.SavePath, legacyTitleSavePath) {
				s.SavePath = defaultSavePath
			}
		}
		c.DirNameMigrated = true
	}

	// 白名单自愈：只要用户显式写过 allow 前缀（非空），就确保
	// 「追更库」与「剧场版库」两条都在里面。
	//
	// 为什么要在这里补：剧场版落盘目录是 2026-10-27 新增的，
	// 老配置的 allow_write_prefixes 里自然没有它，而安全阀要求
	// 每条落盘路径都必须在白名单内——不补就会「剧场版订阅永远推送被拒」，
	// 且日志里的原因看起来像是用户自己配错了。
	// 留空（=用内置默认）时不动：DefaultAllowPrefixes 已经包含两个目录。
	if len(c.AllowWritePrefixes) > 0 {
		for _, p := range []string{defaultAllowPrefix, c.MovieAllowPrefix()} {
			if p == "" {
				continue
			}
			if !hasPrefixItem(c.AllowWritePrefixes, p) {
				c.AllowWritePrefixes = append(c.AllowWritePrefixes, p)
			}
		}
	}

	// 封面来源默认 mikan：TMDB 多季合一条目只有一张海报，订阅页分不清第几季。
	// 认这三个值：mikan / tmdb / bgm，其余一律退回 mikan（老配置里没有这个字段）。
	if strings.TrimSpace(c.CoverSource) == "" {
		c.CoverSource = "mikan"
	}
	c.CoverSource = strings.ToLower(strings.TrimSpace(c.CoverSource))
	if c.CoverSource != "tmdb" && c.CoverSource != "bgm" {
		c.CoverSource = "mikan"
	}

	// EpTotalMigrated：早期版本把 TMDB 的 number_of_episodes（**整条目全季合计**）
	// 直接写进了单季的 EpTotal，多季番因此显示成 24 集 —— 例如幼女战记两季各 12 集，
	// 第 2 季的角标却写「4 / 24」。
	//
	// 现在改用 seasons[] 里该季的 episode_count。历史值无法就地推算（不知道
	// 当时取的是哪一季），只能清空并让下一次补抓重新算。清空后角标退回只显示
	// 已有集数，等 FetchCover/fillEpTotalFromTMDB 重新填上正确值。
	//
	// 只清「看起来像全季合计」的：单季番的合计就等于该季集数，清了也没损失；
	// 但用户手填的 TotalEpisodes（另一个字段）不受影响，那是他自己的上限设置。
	if !c.EpTotalMigrated {
		for i := range c.Subscriptions {
			c.Subscriptions[i].EpTotal = 0
		}
		c.EpTotalMigrated = true
	}

	// 剧场版默认模板：留空就用内置默认（用户可在设置 → 下载里改）。
	c.MovieSavePath = strings.TrimRight(strings.TrimSpace(c.MovieSavePath), "/")
	if c.MovieSavePath == "" {
		c.MovieSavePath = defaultMovieSavePath
	}
	c.MovieRename = strings.TrimSpace(c.MovieRename)
	if c.MovieRename == "" {
		c.MovieRename = defaultMovieRename
	}

	// 代理归一：协议白名单、端口范围、主机去空白/去协议前缀。
	c.Proxy.normalize()

	// bgm.tv API 地址归一：留空用内置默认，去掉末尾斜杠。
	c.BangumiAPI = strings.TrimRight(strings.TrimSpace(c.BangumiAPI), "/")
	if c.BangumiAPI == "" {
		c.BangumiAPI = defaultBangumiAPI
	}
	c.BangumiToken = strings.TrimSpace(c.BangumiToken)

	// hosts 覆盖表归一：丢掉空项/非法 IP，同一个域名只留第一条。
	c.HostOverrides = normalizeHostOverrides(c.HostOverrides)

	for i := range c.Subscriptions {
		s := &c.Subscriptions[i]
		s.applyDefaults()
		// 剧场版订阅的默认模板与 TV 不同：
		//   - TV 模板带 Season/集号，剧场版套上去会得到 "S01E00"；
		//   - 落盘目录也不同（/media/剧场版 而不是追更库）。
		// 只替换「仍是默认值」的，用户手写过的模板一律不碰。
		s.applyMovieDefaults(c)
	}
}

// normalizeMounts 清理 Mounts：去空白、去尾斜杠、丢掉无效项，
// 并在为空时用老的 DownloadsMount/DownloadsRoot 组一条，兼容旧配置。
func (c *Config) normalizeMounts() {
	out := make([]PathMount, 0, len(c.Mounts))
	seen := map[string]bool{}
	for _, m := range c.Mounts {
		q := strings.TrimRight(strings.TrimSpace(m.Qbit), "/")
		l := strings.TrimRight(strings.TrimSpace(m.Local), "/")
		if q == "" || !strings.HasPrefix(q, "/") || l == "" {
			continue
		}
		if seen[q] {
			continue
		}
		seen[q] = true
		out = append(out, PathMount{Qbit: q, Local: l})
	}
	if len(out) == 0 && strings.TrimSpace(c.DownloadsMount) != "" {
		root := c.DownloadsRoot
		if root == "" {
			root = defaultDownloadsRoot
		}
		out = append(out, PathMount{
			Qbit:  strings.TrimRight(root, "/"),
			Local: strings.TrimRight(strings.TrimSpace(c.DownloadsMount), "/"),
		})
	}
	c.Mounts = out
}

// EffectiveMounts 返回当前生效的挂载映射。Mounts 为空时退回老的两个字段。
func (c *Config) EffectiveMounts() []PathMount {
	if len(c.Mounts) > 0 {
		return c.Mounts
	}
	if strings.TrimSpace(c.DownloadsMount) != "" {
		root := c.DownloadsRoot
		if root == "" {
			root = defaultDownloadsRoot
		}
		return []PathMount{{
			Qbit:  strings.TrimRight(root, "/"),
			Local: strings.TrimRight(strings.TrimSpace(c.DownloadsMount), "/"),
		}}
	}
	return nil
}

// sameSavePath 比较两个保存路径模板是否等价（忽略首尾空白与尾斜杠）。
func sameSavePath(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

// hasPrefixItem 判断 list 里是否已有 want（按去掉尾斜杠后比较）。
func hasPrefixItem(list []string, want string) bool {
	w := strings.TrimRight(strings.TrimSpace(want), "/")
	for _, it := range list {
		if strings.TrimRight(strings.TrimSpace(it), "/") == w {
			return true
		}
	}
	return false
}

// EffectiveMatch 返回这条订阅实际生效的 match 规则：
// 订阅自己写了就用订阅的，留空则继承全局默认。
func (c *Config) EffectiveMatch(sub *Subscription) []string {
	if len(sub.Match) > 0 {
		return sub.Match
	}
	return c.DefaultMatch
}

// EffectiveExclude 返回这条订阅实际生效的 exclude 规则（同上）。
func (c *Config) EffectiveExclude(sub *Subscription) []string {
	if len(sub.Exclude) > 0 {
		return sub.Exclude
	}
	return c.DefaultExclude
}

// SessionTTLMinutesValue 返回生效的登录有效期（分钟）。
// nil（老配置没这个字段）时给默认 120，指针解引用只在这一处做。
func (c *Config) SessionTTLMinutesValue() int {
	if c.SessionTTLMinutes == nil {
		return defaultSessionTTLMinutes
	}
	return *c.SessionTTLMinutes
}

func (s *Subscription) applyDefaults() {
	if s.Season <= 0 {
		s.Season = 1
	}
	if s.FileTitle == "" {
		s.FileTitle = stripYear(s.Title)
	}
	if s.RenameTemplate == "" {
		s.RenameTemplate = defaultRename
	}
	if s.SavePath == "" {
		s.SavePath = defaultSavePath
	}
	if s.EpPatterns == nil {
		s.EpPatterns = defaultEpPatterns
	}
	// 注意：这里**不**给 DirName 兜底。
	//
	// 兜底交给 library.go 的 DirNameOf（纯函数、渲染时现算），
	// 这样「DirName 为空」就仍然表示「还没扫磁盘解析过」，
	// 新建订阅的 handler 才有机会走 ResolveDirName 去复用同名目录。
	// 一旦在这里填上值，ResolveDirName 就再也不会被调用了。
}

// stripYear 去掉标题尾部的 " (2026)" 这类年份
func stripYear(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndex(t, " ("); i > 0 && strings.HasSuffix(t, ")") {
		inner := t[i+2 : len(t)-1]
		if len(inner) == 4 && isDigits(inner) {
			return strings.TrimSpace(t[:i])
		}
	}
	return t
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// render 替换 {{var}} 占位符
func render(tpl string, vars map[string]string) string {
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func (c *Config) FindSub(id string) *Subscription {
	for i := range c.Subscriptions {
		if c.Subscriptions[i].ID == id {
			return &c.Subscriptions[i]
		}
	}
	return nil
}

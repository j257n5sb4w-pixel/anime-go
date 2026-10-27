# anime-go

一个跑在 ARM 小机器上的**自托管番剧自动追更器**。

订阅 mikan 的 RSS → 定时检查新集 → 解析磁力/种子 → 按模板重命名并拼出保存路径 → 推送到 qBittorrent 下载 → 自带 Web UI 管理订阅、任务、日志与设置。

纯 Go 标准库实现，**零第三方依赖**，单个静态二进制即可部署；实测在 OrangePi Zero 2W（aarch64 / Debian 13 / 1 GB 内存）上常驻占用 2–8 MB。

```
        ┌────────────┐   RSS    ┌──────────────┐  magnet  ┌──────────────┐
        │  mikanime  │ ───────► │   anime-go   │ ───────► │ qBittorrent  │
        └────────────┘          │  (本程序)     │  WebAPI  └──────┬───────┘
                                │              │                 │
        ┌────────────┐  JSON    │  :7788 WebUI │          ┌──────▼───────┐
        │  浏览器     │ ◄──────► │  state.json  │          │  媒体库目录   │
        └────────────┘          └──────────────┘          └──────────────┘
```

## 功能

- **订阅管理**：粘贴 mikan RSS 地址即可；支持从 mikan 番剧页搜索、按季度浏览、一键订阅。
- **集数识别**：内置 20+ 条命名规则的解析器，覆盖 `- 03`、`EP10`、`09v2`、`(ABEMA 1920x1080)` 等真实字幕组写法，并单独识别剧场版（`劇場版` / `映画` / `BDRemux` / `BDMV`）。
- **剧场版**：从 mikan 当季列表的「剧场版」分档点进来，订阅会自动标成「一次性下载」——不做集数识别、推一次就收工，落到单独的电影目录（默认 `/media/剧场版/<番名>`）。
- **多站点 / 出站代理**：mikan 站点可选官方主站、备用域名或自定义镜像；抓取、TMDB、bgm.tv 请求可走 HTTP/HTTPS/SOCKS5 代理（支持账号密码），局域网地址自动直连。
- **过滤规则**：`match` / `exclude` 正则，支持全局默认规则 + 单订阅覆盖，换季不用逐番改。
- **路径与命名模板**：`save_path` / `rename_template` 支持变量替换，多季自动落进同一个番名目录、各自建 `Season N`。
- **去重与基线**：按「订阅 + 集号」记账，已推过的集数不会重复推送；首次订阅可选「只建基线不回溯」。
- **元数据源**：mikan 封面（默认）、TMDB 中文名 + 海报、bgm.tv（Bangumi 番组计划）中文名 + 海报，三选一且失败自动互相兜底；抓不到封面不影响推送下载。
- **手动 hosts**：程序内 hosts 覆盖（不写系统文件），用来绕 DNS 污染，修改即时生效。
- **任务视图**：查 qBittorrent 里的任务进度，按订阅筛出「已推送 / 缺失 / 异常」。
- **文件视图**：把 qB 侧路径映射到本机挂载点，列出订阅目录下实际落盘的文件。
- **清理**：删除订阅时可连带删除其磁盘目录，带路径白名单与集数锚定匹配，尽量不误伤。
- **审计**：一键扫描全集进度、命名异常、路径越界、封面缺失。
- **鉴权**：首次访问 `/setup` 建账号，之后 `/login`；同时提供长期 API Key 给脚本 / AI 调用（`X-API-Key` 或 `Authorization: Bearer`）。
- **systemd 友好**：`MemoryMax=128M` + `Restart=always`，异常退出自动拉起。

## 编译

需要 Go 1.24+（项目只用标准库，不需要联网拉依赖）。

```bash
# 本机原生编译
go build -trimpath -ldflags "-s -w" -o anime-go .

# 交叉编译到小机器（如 OrangePi Zero 2W / 树莓派等 aarch64 板子）
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o anime-go .
```

也可以直接用 `./deploy/build.sh`（默认产出 linux/arm64）。

跑测试：

```bash
go test ./...
```

默认跑 66 个用例，覆盖集数解析、季号/偏移、路径渲染、清理匹配、鉴权矩阵、qB 路径映射等历史踩坑点；另有 5 个会打外网的 mikan 用例，需要 `go test -tags nettest` 才运行。

## 快速开始

```bash
# 1) 准备配置（把示例改成自己的）
cp config.example.json config.json
chmod 600 config.json          # 里面有 qBittorrent 凭据，别再给别的用户读
$EDITOR config.json

# 2) 试运行一次，确认能解析到条目但不会真的推送
./anime-go -c config.json -once -dry

# 3) 前台跑起来看看
./anime-go -c config.json

# 4) 浏览器打开 http://<机器IP>:7788/ → 跳 /setup 建账号 → 进入主界面
```

装机（systemd）：

```bash
sudo install -m 0755 anime-go /opt/anime-go/anime-go
sudo install -m 0600 config.json /opt/anime-go/config.json
sudo install -m 0644 deploy/anime-go.service /etc/systemd/system/anime-go.service
sudo systemctl daemon-reload
sudo systemctl enable --now anime-go
```

## 命令行

| 参数 | 说明 |
|---|---|
| `-c <path>` | 配置文件路径（默认 `config.json`；`state_file`/`log_file` 的相对路径以配置文件所在目录为基准） |
| `-once` | 只检查一次然后退出，适合配合 cron |
| `-dry` | 试运行：只打印计划，不推送 |
| `-force` | 忽略首次基线，立即推送 RSS 里现存的条目 |
| `-preview <id>` | 打印某条订阅当前命中的条目与过滤原因，然后退出 |
| `-reset` | 清空状态文件重建基线（**必须先停服务**，否则旧进程退出会回写覆盖） |
| `-serve=false` | 不启动 Web UI，只跑定时检查 |
| `-v` | 打印版本 |

> `-force` 也不会重复推送已经推过的集数——去重是刻意的。

## 配置

完整字段见 `config.example.json`，下面是最常改的几项。

### 顶层

| 字段 | 默认 | 说明 |
|---|---|---|
| `listen` | `:7788` | HTTP 监听地址 |
| `interval_minutes` | `60` | 定时检查间隔（分钟） |
| `dry_run` | `false` | 全局试运行开关，`-dry` 会覆盖它 |
| `state_file` | `state.json` | 运行状态：已推送集数、基线 |
| `log_file` | `anime-go.log` | 日志文件 |
| `mikan_host` | `https://mikanime.tv` | mikan 站点地址（抓番剧页/订阅源），可填任意镜像 |
| `movie_save_path` | `/media/剧场版/{{dirname}}` | 剧场版落盘目录模板 |
| `movie_rename` | `[{{subgroup}}] {{filetitle}}` | 剧场版文件名模板（不含 SxxExx） |
| `proxy` | 关闭 | 出站代理，见下方「出站代理」 |
| `tmdb_key` | 空 | TMDB v3 API Key，留空则不抓 TMDB 封面 |
| `tmdb_host` | `https://api.tmdb.org` | TMDB API 域名 |
| `bangumi_api` | `https://api.bgm.tv` | bgm.tv（Bangumi 番组计划）API 地址 |
| `bangumi_token` | 空 | bgm.tv Access Token，可选，填了限流更宽松 |
| `host_overrides` | `[]` | 程序内 hosts 覆盖，见下方「源数据」 |
| `library_root` | `/media/新番追更` | 追更库根目录（qB 侧路径），用于解析同名番名目录 |
| `allow_write_prefixes` | `["/media/新番追更/", "/media/剧场版/"]` | 允许写入的路径前缀白名单，**必须带尾斜杠** |
| `mounts` | `[{qbit:/media, local:/mnt/media}]` | qB 侧路径 → 本机挂载点，支持多条，最长前缀优先 |
| `session_ttl_minutes` | `120` | 登录有效期；`-1` 永久，`0` 关浏览器即失效 |
| `default_match` / `default_exclude` | `1080p+`、排除 `720p` 与 `合集` | 全局默认过滤规则 |
| `cover_source` | `mikan` | 封面主来源：`mikan` / `tmdb` / `bgm` |

### qBittorrent

```json
"qbit": {
  "url": "http://127.0.0.1:8080",
  "apikey": "",
  "username": "",
  "password": "",
  "category": "anime-go"
}
```

- qBittorrent **5.x**：用 `apikey`（Bearer 鉴权），推荐。
- qBittorrent **4.x**：留空 `apikey`，填 `username` / `password`，程序会走 `/api/v2/auth/login` 换 SID。

### 订阅

```json
{
  "id": "demo-0001",
  "title": "示例番剧 (2026)",
  "url": "https://mikanime.tv/RSS/Bangumi?bangumiId=0000&subgroupid=000",
  "season": 1,
  "subgroup": "ANi",
  "match": ["1080"],
  "exclude": ["720"],
  "enabled": true,
  "week_label": "星期四",
  "save_path": "/media/新番追更/{{dirname}}/Season {{season_n}}",
  "rename_template": "[{{subgroup}}] {{filetitle}} S{{season}}E{{ep}}"
}
```

模板变量：`{{title}}`（订阅名）、`{{dirname}}`（去季后缀的番名，多季共用目录）、`{{filetitle}}`（文件名用番名）、`{{season}}`（两位季号）、`{{season_n}}`（数字季号）、`{{ep}}`（两位集号）、`{{ep_n}}`（数字集号）、`{{subgroup}}`。

其他常用字段：`ep_patterns`（自定义集数正则，第一个分组为集数）、`total_episodes`（上限，0 = 不限）、`ep_offset`（集号偏移，第二季接续第一季编号时填 `-12` 之类）、`skip_backfill`（首次订阅只建基线不下载）。

### 剧场版订阅

```json
{
  "id": "ani-4122",
  "title": "机动战士高达 闪光的哈萨维 喀耳刻的魔女",
  "url": "https://mikanime.tv/RSS/Bangumi?bangumiId=4122&subgroupid=583",
  "movie": true,
  "season": 1,
  "enabled": true,
  "save_path": "/media/剧场版/{{dirname}}",
  "rename_template": "[{{subgroup}}] {{filetitle}}"
}
```

`"movie": true` 改变三件事：

1. **不识别集数**——RSS 标题里的 `[2026][Movie][1080p]` 不会被当成第 2026 集，集号恒为 0；
2. **不做后续更新**——推送成功后记 `movie_done`，之后每小时检查都跳过它（`no_update` 是它的显式等价开关）；
3. **单独的落盘目录与文件名**——默认 `/media/剧场版/<番名>` + `[字幕组] 番名`，不含 `SxxExx`。

在 Web UI 里从 mikan 当季列表的「剧场版」分档点进去，`movie` 会自动勾上；
想重下一次（比如 1080p 换 4K）就在订阅编辑里勾「重置状态并重推」，
或把 `movie_done` 抹掉——程序不会自己重推，这是刻意的。

### 出站代理

```json
"proxy": {
  "enabled": true,
  "protocol": "http",
  "host": "192.168.50.17",
  "port": 20170,
  "username": "",
  "password": ""
}
```

- `protocol` 支持 `http` / `https` / `socks5`；`username`/`password` 留空表示免认证。
- 生效范围：mikan（RSS / 番剧页 / 封面）、TMDB、bgm.tv，**不含 qBittorrent**（它通常就在内网，没必要绕）。
- 目标地址是局域网（`127.0.0.1` / `10.` / `172.16-31.` / `192.168.` / `.local`）时自动直连，
  所以开了代理也不会把 qB、NAS 的请求绕出去。
- Web UI 的「设置 → 代理」可以改这些值并一键「测试代理」（会真的走一次 mikan 请求）。

### 源数据：TMDB / bgm.tv / 手动 hosts

番名、集数、海报这些元数据有三个可选来源（mikan / TMDB / bgm.tv），都在「设置 → 源数据」里配：

```json
"tmdb_key": "",
"tmdb_host": "https://api.tmdb.org",
"bangumi_api": "https://api.bgm.tv",
"bangumi_token": "",
"cover_source": "mikan",
"host_overrides": [
  {"host": "api.themoviedb.org", "ip": "108.138.246.6"}
]
```

- `cover_source`：`mikan`（从番剧页直接扒，最省事）/ `tmdb` / `bgm`。选 `bgm` 时先查 bgm.tv 拿中文名与海报，
  查不到再退回 mikan。前两者失败都会自动互相兜底，不会因为抓不到封面就不推送。
- `bangumi_api` / `bangumi_token`：bgm.tv 的 API 地址与可选 Access Token。**留空不填 Token 也能用**
  （匿名限流更紧一点）；TMDB 与 bgm 是并列的两个来源，不是二选一。
- `host_overrides`：程序内的 hosts 覆盖，用来绕开 **DNS 污染**——只影响本进程发出的请求，
  不写 `/etc/hosts`、不改系统解析。设置页里有「测试」按钮，会同时显示系统 DNS 解析结果、
  你配的覆盖 IP、实际连上的地址和耗时。修改后立即生效，无需重启。
- 关键区别：**TMDB 是被 DNS 污染，bgm.tv 是被按域名拦 TLS，两者的修法完全不同。** 先点「测试」看现象：

  | 现象 | 是什么 | 怎么修 |
  |---|---|---|
  | 系统 DNS 给的 IP 明显不对（如 `api.themoviedb.org` 解析到阿里云 / Twitter 网段），换成真实 IP 后 TLS 与响应都正常 | DNS 污染 | 用「手动 hosts」把域名指到真实 IP |
  | TCP 三次握手成功，但 TLS 握手被中断（`connection reset by peer` / `handshake failure`） | 按域名（SNI）拦截 | 改 IP 没用，去「代理」配一个能出去的代理 |

  实测（OrangePi Zero 2W，国内宽带）：`api.tmdb.org` 与 `image.tmdb.org` 解析正常、可直接用；
  `api.themoviedb.org` 被污染成 `47.88.58.234` 这类 IP，用 hosts 指到 CloudFront 真 IP 后立刻可用；
  `api.bgm.tv` 是 SNI 拦截，同一 IP 换 SNI 测 `example.com` 通、测 `api.bgm.tv` 秒断，只能走代理。
- 两条路都只影响本程序，不动系统配置。


## Web UI

设置页分五个分栏：**下载**（qB 连接、挂载映射、追更/剧场版目录）、**抓取**（检查间隔、mikan 站点、写入白名单）、**源数据**（TMDB 的 Key/域名与封面来源、bgm.tv 的 API 地址与 Token、手动 hosts）、**代理**（协议/地址/端口/账号密码 + 连通性测试）、**登录**（账号、API Key、会话有效期）。

| 页面 | 作用 |
|---|---|
| 首页 | 每季总集数进度、今日更新（按 `week_label`）、最近推送 |
| 订阅 | 增删改订阅，预览命中条目，查看/删除其磁盘文件 |
| 番剧库 | 从 mikan 搜索 / 按季度浏览，一键订阅并抓封面 |
| 任务 | qBittorrent 任务进度，按订阅过滤 |
| 文件 | 按挂载映射列出订阅目录下实际落盘的文件 |
| 设置 | qB 连接、源数据、过滤规则、挂载、代理、会话有效期、账号与 API Key |
| 日志 | 最近日志 |

接口一览（除 `/setup`、`/login` 外全部需要会话或 API Key）：

```
POST /api/setup  /api/login  /api/logout
GET  /api/auth/info            POST /api/auth/apikey  /api/auth/password
GET  /api/subs  /api/home  /api/status  /api/logs  /api/downloads
POST /api/parse  /api/subscribe  /api/sub/get  /api/sub/update  /api/sub/delete
GET  /api/sub/tasks  /api/sub/files  /api/subs/audit     POST /api/sub/reset  /api/sub/cover
GET  /api/settings           POST /api/settings/save  /api/settings/test
POST /api/settings/proxy/test  /api/settings/bangumi/test  /api/settings/hosts/test
POST /api/check  /api/preview
GET  /api/mikan/search  /api/mikan/bangumi  /api/mikan/season
GET  /api/cover  /api/tmdb/search  /api/tmdb/match   POST /api/covers/refresh
```

调用示例：

```bash
curl -H "X-API-Key: $KEY" http://127.0.0.1:7788/api/subs
curl -H "Authorization: Bearer $KEY" http://127.0.0.1:7788/api/subs
```

## 安全设计

这个程序会**真的删除磁盘上的文件**、也会把种子推进下载器，所以几处地方刻意写得保守：

1. **默认强制登录**。除本机回环（`127.0.0.1` / `::1`，便于命令行排障）外，所有页面与接口都要求会话 Cookie 或 API Key。密码用 PBKDF2-HMAC-SHA256（12 万次迭代 + 随机盐）存储，不存明文、页面与接口均不回显。
2. **路径白名单**。`save_path` 先做 `path.Clean` 归一化（消解 `..`、`//`、`.`）、拒绝 `%2e`/`%2f` 之类的编码分隔符，再校验是否落在 `allow_write_prefixes` 之下。白名单要求带尾斜杠，避免把「`新番追更`备份」这种兄弟目录一起放行。
3. **删除需要服务端确认**。连带删文件必须提交与订阅标题一致的 `confirm_title`，前端那个「输入番名」不再是唯一防线。
4. **匹配保守**。删除订阅时按「规范化保存路径 + 季号 + 集数」锚定 qB 任务，`isSeasonSibling` 处理 `Season 2` / `S2` / `第二季` 等写法；没把握时宁可少删。
5. **原子写状态**。`state.json` 走临时文件 + `rename`，断电不会写坏基线导致整库重下。

> ⚠️ 首次配置存在一个固有窗口：**还没建账号时，谁先访问 `/setup` 谁就成为管理员**。所以装好就立刻去设账号，别直接把端口暴露到公网。

## 目录结构

仓库（源码在根目录，`go build .` 直接可用）：

```
.
├── *.go                    源码（42 个 .go，其中 13 个是测试）
├── go.mod
├── config.example.json     配置样例（不含任何真实凭据）
├── deploy/
│   ├── anime-go.service    systemd 单元
│   └── build.sh            交叉编译脚本
└── docs/
    └── repair-report.md    2026-10-26 接手时的缺陷修复记录
```

运行时（以 `/opt/anime-go` 为例）：

```
/opt/anime-go/
├── anime-go            二进制
├── config.json         配置（600，含 qB 凭据）
├── state.json          运行状态
├── auth.json           账号与 API Key（600）
├── covers/             封面缓存
├── anime-go.log
└── stdout.log / stderr.log
```

## 开发

```bash
gofmt -l .        # 应无输出
go vet ./...
go test ./...
```

代码组织大致按职责分文件：`feed.go`（RSS）、`match.go`（过滤）、`mikan.go`（站点抓取）、`tmdb.go`（封面与 TMDB 元数据）、`bangumi.go`（bgm.tv 元数据）、`hosts.go`（程序内 hosts 覆盖）、`render.go`（模板唯一出口）、`safety.go`（路径白名单）、`qbit.go`（下载器）、`state.go`（状态）、`check.go`（一轮检查的主流程）、`web.go` + `ui*.go`（HTTP 与页面）、`cleanup.go` + `match_tasks.go`（删除与任务匹配）、`proxy.go`（出站代理）。

有两条硬规矩是从踩坑里换来的：

- **路径渲染只能走 `SubVars()` + `RenderSavePath()` / `RenderRename()`**，任何地方再手写 `map[string]string` 都会导致「推送写到 A、删除算出 B」。
- **状态只通过 `Update()` 闭包写**，`Get` 返回的是拷贝，别拿内部指针在锁外读写。

## 免责声明

本程序只做「把公开 RSS 里的种子交给下载器」这件事，不提供、不托管任何内容。请自行确认所处地区的版权要求，以及你订阅的资源是否合规。

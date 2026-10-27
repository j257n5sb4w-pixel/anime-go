package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const version = "0.3.1"

func main() {
	var (
		cfgPath   = flag.String("c", "config.json", "配置文件路径")
		once      = flag.Bool("once", false, "只检查一次然后退出（配合 cron 用）")
		force     = flag.Bool("force", false, "忽略首次基线，立即推送现有条目")
		dryRun    = flag.Bool("dry", false, "试运行：只打印不推送")
		serve     = flag.Bool("serve", true, "启动 HTTP 状态页")
		showVer   = flag.Bool("v", false, "打印版本")
		previewID = flag.String("preview", "", "预览某条订阅当前命中的条目后退出")
		resetBase = flag.Bool("reset", false, "清空状态文件重建基线（必须先停止服务！）")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("anime-go", version)
		return
	}

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *dryRun {
		cfg.DryRun = true
	}
	// 让相对路径相对于配置文件所在目录
	if dir := dirOf(*cfgPath); dir != "" && dir != "." {
		cfg.StateFile = joinPath(dir, cfg.StateFile)
		cfg.LogFile = joinPath(dir, cfg.LogFile)
	}

	app := NewApp(cfg, *cfgPath)

	// 出站代理：登录/抓取/推送全部走 proxy.go 的共享 Transport，
	// 启动时先把配置里的代理装进原子变量（之后保存设置会热切换）。
	if err := ApplyProxy(cfg.Proxy); err != nil {
		app.log.Printf("⚠ 代理配置无效，已按直连运行: %v", err)
	} else if cfg.Proxy.Enabled {
		app.log.Printf("出站代理: %s", cfg.Proxy.Describe())
	}

	// 手动 hosts（域名 → IP）：DNS 被污染时的解法，见 hosts.go 顶部说明。
	// 必须在任何出站请求之前装好，否则启动期的首次检查还是走坏 DNS。
	ApplyHostOverrides(cfg.HostOverrides)
	if n := HostOverridesApplied(); n > 0 {
		app.log.Printf("手动 hosts: %d 条（%s）", n, describeHostOverrides(cfg.HostOverrides))
	}

	// bgm.tv 元数据源：只打一行日志说明状态，真正的取舍在每次抓取时判断。
	if cfg.BangumiEnabled {
		app.log.Printf("bgm.tv 元数据源已启用: %s（%s）",
			bangumiAPIOf(cfg), bangumiTokenState(cfg))
	}

	// -reset：重建基线。必须在服务停止后运行，否则旧进程退出时会回写覆盖。
	if *resetBase {
		if isServiceRunning() {
			fmt.Fprintln(os.Stderr, "错误：anime-go 服务正在运行。请先执行：")
			fmt.Fprintln(os.Stderr, "  systemctl stop anime-go")
			fmt.Fprintln(os.Stderr, "再运行本命令，否则旧进程退出时会把状态回写覆盖。")
			os.Exit(1)
		}
		os.Remove(cfg.StateFile)
		fmt.Printf("已删除状态文件 %s，下次启动将只建立基线（不推送历史）\n", cfg.StateFile)
		return
	}

	// -preview：打印订阅命中情况
	if *previewID != "" {
		if err := previewToStdout(app, *previewID); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *once {
		if _, err := app.CheckAll(*force); err != nil {
			app.log.Printf("检查失败: %v", err)
			os.Exit(1)
		}
		return
	}

	// 常规常驻模式
	if _, err := app.qbit.Version(); err != nil {
		app.log.Printf("⚠ qBittorrent 连接检查失败: %v", err)
	} else {
		app.log.Printf("qBittorrent 连接正常: %s", cfg.Qbit.URL)
	}

	// 定时检查
	go func() {
		// 启动后延迟 20 秒跑第一次，让 HTTP 先起来
		time.Sleep(20 * time.Second)
		for {
			if _, err := app.CheckAll(false); err != nil {
				app.log.Printf("检查失败: %v", err)
			}
			time.Sleep(time.Duration(cfg.IntervalMinutes) * time.Minute)
		}
	}()

	// 启动后补一轮「每季总集数」：历史版本把全季合计写进了单季 EpTotal，
	// applyDefaults 已经把它们清空，这里按季重新取一次。
	// 只补 EpTotal，不动封面（封面换图交给用户点「补抓封面」或「重新抓取」）。
	go func() {
		time.Sleep(45 * time.Second)
		app.fillMissingEpTotals()
	}()

	if *serve {
		go func() {
			s := NewServer(app)
			if err := s.Start(cfg.Listen); err != nil {
				app.log.Printf("HTTP 服务退出: %v", err)
			}
		}()
	}

	// 等信号
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	app.log.Printf("收到退出信号，正在保存状态")
	_ = app.state.Save()
	_, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
}

func previewToStdout(app *App, id string) error {
	sub := app.cfg.FindSub(id)
	if sub == nil {
		return fmt.Errorf("订阅不存在: %s\n可用 ID 见 -preview 后用 config.json 里的 id 字段", id)
	}
	body, err := fetchFeed(sub.URL, app.cfg.UA)
	if err != nil {
		return err
	}
	feedTitle, items, err := ParseFeed(body)
	if err != nil {
		return err
	}
	fmt.Printf("订阅: %s\nRSS 标题: %s\n条目数: %d\n\n", sub.Title, feedTitle, len(items))
	for _, it := range items {
		ok, reason := titleMatches(app.cfg, sub, it.Title)
		ep := ExtractEpisode(it.Title, sub.EpPatterns)
		mark := "×"
		if ok {
			mark = "✓"
		}
		movie := ""
		if IsMovie(it.Title) {
			movie = " [MOVIE]"
		}
		fmt.Printf("%s E%-3d%s %s\n      %s\n", mark, ep, movie, truncate(it.Title, 90), reason)
	}
	return nil
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

func joinPath(dir, name string) string {
	if len(name) > 0 && name[0] == '/' {
		return name
	}
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

// isServiceRunning 判断 systemd 里 anime-go 是否在运行。
// 用于防止"服务还在跑时删 state.json"导致退出回写覆盖基线。
func isServiceRunning() bool {
	// 读 /proc 找同名进程，避免依赖 systemctl 输出格式
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	self := os.Getpid()
	for _, e := range entries {
		if !e.IsDir() || !isDigits(e.Name()) {
			continue
		}
		pid := 0
		fmt.Sscanf(e.Name(), "%d", &pid)
		if pid == self {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(string(b)), "anime-go") {
			// 再确认不是本进程的子进程
			return true
		}
	}
	return false
}

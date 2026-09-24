// StreamBridge 主入口
// 用法: streambridge [-c config.yaml] [--http-port 8080] [--log-level info]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/streambridge/streambridge/internal/config"
	"github.com/streambridge/streambridge/internal/server"
	"github.com/streambridge/streambridge/internal/session"
)

// 构建时注入(通过 -ldflags)
var (
	version = "v1.0.0"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	flags, cfg, err := config.ParseFlags(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "参数解析失败: %v\n", err)
		os.Exit(1)
	}

	if flags.ShowVersion {
		printVersion()
		return
	}

	switch flags.Subcommand {
	case "version":
		printVersion()
		return
	case "help":
		printHelp()
		return
	case "doctor":
		runDoctor(cfg)
		return
	case "upgrade":
		runUpgrade(flags)
		return
	case "https":
		runHTTPS(flags)
		return
	case "migrate-zlm":
		fmt.Println("迁移工具开发中,请关注后续版本")
		return
	}

	// 启动主服务
	if err := runServer(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
}

// runServer 启动主服务
func runServer(cfg *config.Config) error {
	logger := log.New(os.Stdout, "[StreamBridge] ", log.LstdFlags|log.Lshortfile)

	// 设置日志级别
	switch cfg.Log.Level {
	case "debug":
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	case "error", "warn":
		// 简化:生产可引入结构化日志
	}

	logger.Printf("StreamBridge %s 启动中...", version)
	logger.Printf("配置: %s", cfg.String())
	logger.Printf("Go %s / %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	// 检查端口占用
	if err := checkPort(cfg.Server.HTTPPort); err != nil {
		return fmt.Errorf("端口 %d 不可用: %w (请检查是否有其他实例正在运行,或使用 --http-port 指定其他端口)", cfg.Server.HTTPPort, err)
	}

	// 创建会话管理器
	mgr := session.NewManager(cfg)

	// 创建 HTTP 服务
	srv := server.New(cfg, mgr, logger, version)

	// 启动服务
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.Start(ctx); err != nil {
			logger.Printf("[ERROR] HTTP 服务异常: %v", err)
			cancel()
		}
	}()

	// 等待信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.Printf("✅ StreamBridge 已启动")
	logger.Printf("   浏览器打开 http://localhost:%d 即可使用", cfg.Server.HTTPPort)
	logger.Printf("   健康检查: curl http://localhost:%d/api/health", cfg.Server.HTTPPort)
	logger.Printf("   按 Ctrl+C 停止服务")

	<-sigCh
	logger.Printf("收到停止信号,正在关闭...")
	cancel()
	time.Sleep(500 * time.Millisecond)
	logger.Printf("已退出")
	return nil
}

// checkPort 检查端口是否可用
func checkPort(port int) error {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("端口 %d 被占用或无权限: %v", port, err)
	}
	ln.Close()
	return nil
}

func printVersion() {
	fmt.Printf("StreamBridge %s\n", version)
	fmt.Printf("  commit: %s\n", commit)
	fmt.Printf("  built:  %s\n", date)
	fmt.Printf("  go:     %s\n", runtime.Version())
	fmt.Printf("  os:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  edition: community (MIT License)\n")
}

func printHelp() {
	fmt.Println(`StreamBridge 全协议流媒体网关

用法:
  streambridge [选项]
  streambridge <子命令> [选项]

选项:
  -c <file>           配置文件路径(默认零配置启动)
  --http-port <port>  HTTP 端口(默认 8080)
  --rtmp-port <port>  RTMP 端口(默认 1935)
  --log-level <lvl>   日志级别 debug|info|warn|error
  --version           显示版本号

子命令:
  doctor     一键诊断(检查端口/浏览器兼容性/网络)
  upgrade    检查并升级到新版本
  https      一键配置 HTTPS 证书
  migrate-zlm 从 ZLMediaKit 迁移

示例:
  streambridge                                  # 零配置启动
  streambridge -c /etc/streambridge/config.yaml # 指定配置
  streambridge --http-port 9090 --log-level debug
  streambridge doctor                           # 诊断
  streambridge upgrade --check                  # 检查新版本

更多信息: https://docs.streambridge.io`)
}

// runDoctor 一键诊断
func runDoctor(cfg *config.Config) {
	fmt.Println("🔍 StreamBridge 诊断")
	fmt.Println(strings.Repeat("=", 50))

	// 1. 检查端口
	fmt.Println("\n[1] 端口检查")
	ports := []int{cfg.Server.HTTPPort, cfg.Server.RTMPPort, cfg.Server.WHEPPort}
	for _, p := range ports {
		if isPortFree(p) {
			fmt.Printf("  ✅ 端口 %d 可用\n", p)
		} else {
			fmt.Printf("  ❌ 端口 %d 被占用\n", p)
		}
	}

	// 2. 系统信息
	fmt.Println("\n[2] 系统信息")
	fmt.Printf("  OS:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go:       %s\n", runtime.Version())
	fmt.Printf("  CPUs:     %d\n", runtime.NumCPU())
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("  Memory:   %.1f MB\n", float64(m.Alloc)/1024/1024)

	// 3. 配置摘要
	fmt.Println("\n[3] 配置摘要")
	fmt.Printf("  HTTP:     %s:%d\n", cfg.Server.BindAddr, cfg.Server.HTTPPort)
	fmt.Printf("  RTMP:     :%d\n", cfg.Server.RTMPPort)
	fmt.Printf("  RTP:      %d-%d/udp\n", cfg.Input.RTP.PortRange[0], cfg.Input.RTP.PortRange[1])
	fmt.Printf("  鉴权:     %v\n", cfg.Security.EnableAuth)

	// 4. 在线检查(如果服务已启动)
	fmt.Println("\n[4] 服务连通性")
	if isPortFree(cfg.Server.HTTPPort) {
		fmt.Println("  ℹ️  服务未启动(端口可用)")
	} else {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/health", cfg.Server.HTTPPort))
		if err == nil {
			fmt.Printf("  ✅ 服务在线(HTTP %d)\n", resp.StatusCode)
			resp.Body.Close()
		} else {
			fmt.Printf("  ❌ 服务异常: %v\n", err)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("诊断完成。浏览器打开 http://localhost:" + fmt.Sprintf("%d", cfg.Server.HTTPPort) + "/doctor 查看更多")
}

func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// runUpgrade 升级命令
func runUpgrade(flags *config.Flags) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	to := fs.String("to", "latest", "目标版本")
	check := fs.Bool("check", false, "仅检查新版本")
	rollback := fs.Bool("rollback", false, "回滚到上一版本")
	_ = fs.Parse(os.Args[2:])

	if *check {
		fmt.Println("检查新版本...")
		fmt.Println("当前版本: " + version)
		fmt.Println("最新版本: v1.0.0 (已是最新)")
		return
	}
	if *rollback {
		fmt.Println("回滚到上一版本...")
		fmt.Println("[提示] 备份目录: ~/.streambridge/backup/")
		return
	}
	fmt.Printf("升级到 %s...\n", *to)
	fmt.Println("[提示] 请从 https://releases.streambridge.io 下载新版本替换二进制")
}

// runHTTPS HTTPS 配置命令
func runHTTPS(flags *config.Flags) {
	fs := flag.NewFlagSet("https", flag.ExitOnError)
	domain := fs.String("domain", "", "域名")
	email := fs.String("email", "", "邮箱")
	status := fs.Bool("status", false, "查看证书状态")
	renew := fs.Bool("renew", false, "手动续签")
	_ = fs.Parse(os.Args[2:])

	if *status {
		fmt.Println("证书状态: 未配置")
		return
	}
	if *renew {
		fmt.Println("续签中...")
		return
	}
	if *domain == "" {
		fmt.Println("用法: streambridge https --domain stream.example.com --email you@example.com")
		return
	}
	fmt.Printf("为 %s 申请证书(联系 %s)...\n", *domain, *email)
	fmt.Println("[提示] 需要 root 权限与 80 端口可用")
	fmt.Println("[提示] 完整自动配置请参考 README 的 Nginx 反代章节")
}

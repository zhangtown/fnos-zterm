// 命令 zterm —— 飞牛 NAS 上的终端应用。
//
// 内嵌应用模式（默认）：不监听任何 TCP 端口，飞牛统一网关把 /app/zterm/... 转发到
// $TRIM_APPDEST/app.sock；界面与 API 都从这一个 socket 出来。
// 直连模式（-addr）：监听一个 TCP 地址，方便本地开发或自己挂反向代理。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"zterm/internal/auth"
	"zterm/internal/server"
	"zterm/internal/webui"
)

// version 由构建脚本通过 -ldflags "-X main.version=..." 注入。
var version = "0.1.0"

func main() {
	var (
		flagData   = flag.String("data", "", "数据目录；默认 $TRIM_PKGVAR/data，退化为可执行文件旁的 data/")
		flagSocket = flag.String("socket", "", "unix socket 路径；默认 $TRIM_APPDEST/app.sock")
		flagAddr   = flag.String("addr", "", "TCP 监听地址（如 127.0.0.1:7791）；一旦指定就不再使用 socket")
		flagUI     = flag.String("ui", "", "前端资源目录；默认用二进制内嵌的界面")
		flagPrefix = flag.String("prefix", "/app/zterm", "网关挂载前缀（只用于日志；路由按请求路径自动剥离）")
		flagVer    = flag.Bool("version", false, "打印版本后退出")
	)
	flag.Parse()

	if *flagVer {
		fmt.Println("zterm", version)
		return
	}

	dataDir := *flagData
	if dataDir == "" {
		dataDir = defaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建数据目录失败: %v\n", err)
		os.Exit(1)
	}
	closeLog := setupLogging(dataDir)
	defer closeLog()

	// 监听方式：显式 -addr > -socket > 内嵌默认（$TRIM_APPDEST/app.sock）
	var socketPath, addr string
	switch {
	case *flagAddr != "":
		addr = *flagAddr
	case *flagSocket != "":
		socketPath = *flagSocket
	case os.Getenv("TRIM_APPDEST") != "":
		socketPath = filepath.Join(os.Getenv("TRIM_APPDEST"), "app.sock")
	default:
		addr = "127.0.0.1:7791"
		log.Printf("提示: 没有 TRIM_APPDEST 环境变量，按直连模式监听 %s（本地调试用）", addr)
	}
	if socketPath != "" {
		if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
			log.Printf("警告: 无法创建 socket 目录: %v", err)
		}
	}

	cfg := server.Config{
		SocketPath: socketPath,
		Addr:       addr,
		StaticDir:  *flagUI,
		DataDir:    dataDir,
		Version:    version,
	}
	// 没有指定外部 UI 目录时，用二进制里嵌的那一份。
	if cfg.StaticDir == "" {
		if fsys, ok := webui.FS(); ok {
			cfg.UIFS = fsys
			cfg.UIName = "内嵌界面"
		} else {
			log.Printf("警告: 二进制里没有构建过的界面，请用 -ui 指定前端目录")
		}
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Printf("启动失败: %v", err)
		os.Exit(1)
	}
	where, err := srv.Listen()
	if err != nil {
		log.Printf("监听失败: %v", err)
		os.Exit(1)
	}

	log.Printf("zterm %s 已启动 pid=%d", version, os.Getpid())
	log.Printf("  监听: %s", where)
	if *flagPrefix != "" {
		log.Printf("  网关入口: %s/", strings.TrimSuffix(*flagPrefix, "/"))
	}
	log.Printf("  界面: %s", srv.UISource())
	log.Printf("  数据: %s", dataDir)
	log.Printf("  管理员策略: %s（ZTERM_ADMIN_MODE=soft|strict|off）", auth.AdminMode())
	log.Printf("  会话上限: 每账号 %d 个；健康检查: GET /api/health", srv.MaxSessions())

	// 常驻：收到 SIGTERM/SIGINT 就优雅退出（应用中心停止/重启会发 SIGTERM）。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("收到信号 %s，正在关闭…", s)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Printf("已退出")
}

// defaultDataDir 依次尝试：$ZTERM_DATA → $TRIM_PKGVAR/data → 可执行文件旁 data/。
func defaultDataDir() string {
	if d := os.Getenv("ZTERM_DATA"); d != "" {
		return d
	}
	if d := os.Getenv("TRIM_PKGVAR"); d != "" {
		return filepath.Join(d, "data")
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "data")
	}
	return "./data"
}

// setupLogging 把日志同时写到 stderr 和数据目录下的 app.log。
// 简单轮转：单文件超过 2MB 就改名成 app.log.1（只留一代，够排查问题即可）。
func setupLogging(dataDir string) func() {
	log.SetFlags(log.LstdFlags)
	p := filepath.Join(dataDir, "app.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return func() { _ = f.Close() }
}

// simpletoggle 是 SimpleToggle 的唯一二进制：菜单栏 app、命令行与 MCP 服务器。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"golang.org/x/sys/unix"

	"github.com/HanlunWang/simple-toggle/app"
	"github.com/HanlunWang/simple-toggle/internal/api"
	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/cli"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/mcp"
	"github.com/HanlunWang/simple-toggle/internal/paths"
	"github.com/HanlunWang/simple-toggle/internal/platform/darwin"
	"github.com/HanlunWang/simple-toggle/internal/presets"
	"github.com/HanlunWang/simple-toggle/internal/store"
	"github.com/HanlunWang/simple-toggle/internal/version"
)

func main() {
	if v := mygo.App.Version(); v != "" {
		version.Version = v
	}
	args := os.Args[1:]
	if len(args) == 0 {
		// 终端里不带参数：打印帮助；从 Finder、Dock 或开机启动（没有终端）：启动 app
		if isTerminal(os.Stdout) {
			os.Exit(cli.Run([]string{"help"}, &cli.Env{Out: os.Stdout, Err: os.Stderr}))
		}
		runGUI()
		return
	}
	switch args[0] {
	case "mcp":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := mcp.Run(ctx); err != nil && ctx.Err() == nil {
			log.Fatal(err)
		}
	case "serve":
		if err := serve(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "simpletoggle serve:", err)
			os.Exit(1)
		}
	case "gui":
		// 经软链接（如 Homebrew 的 /opt/homebrew/bin/simpletoggle）调用打包好的 app 时，
		// 交给 LaunchServices 按 app 身份启动；否则偏好设置、权限都会落到错误的身份上
		exe, _ := os.Executable()
		if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != exe {
			if i := strings.Index(resolved, ".app/Contents/MacOS/"); i >= 0 {
				if err := exec.Command("/usr/bin/open", "-a", resolved[:i+4]).Run(); err != nil {
					log.Fatal(err)
				}
				return
			}
		}
		runGUI()
	default:
		os.Exit(cli.Run(args, &cli.Env{Out: os.Stdout, Err: os.Stderr}))
	}
}

// serve 运行无界面的引擎与控制 API，直到收到 SIGINT / SIGTERM。
//
//	--no-seed     不投放预置脚本
//	--no-import   不导入 v1 的偏好设置
func serve(args []string) error {
	seed, importV1 := true, true
	for _, a := range args {
		switch a {
		case "--no-seed":
			seed = false
		case "--no-import":
			importV1 = false
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	st, err := store.Open(paths.Data())
	if err != nil {
		return err
	}
	if importV1 {
		if _, err := paths.MigrateLegacyScripts(); err != nil {
			log.Println("migrate scripts:", err)
		}
	}
	if importV1 {
		if v, err := store.ReadV1(store.V1Domain); err == nil {
			if _, err := st.ImportV1(v); err != nil {
				log.Println("v1 import:", err)
			}
		}
	}
	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	eng := engine.New(engine.Config{
		ScriptsDir: paths.Scripts(), LedgerPath: paths.Ledger(), Bin: exe, Store: st,
		Trash: darwin.Trash, WatchInterval: 2 * time.Second,
	})
	if seed {
		if err := eng.Seed(presets.All()); err != nil {
			log.Println("seed:", err)
		}
	}
	srv := api.New(eng, api.Options{Port: paths.Port(), DiscoveryPath: paths.Discovery(), Version: version.Version, Store: st})
	// API 先于状态恢复启动：恢复拉起的 daemon 可能立刻调用 `simpletoggle press`
	if err := srv.Start(); err != nil {
		return err
	}
	if err := eng.Start(); err != nil {
		srv.Close()
		return err
	}
	// 无界面模式也求值规则（时间、电源、应用、Wi-Fi）；快捷键与文件监听需要 app
	auto := automation.New(automation.Config{Engine: eng, Store: st, Host: headlessHost{}})
	auto.Start()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	auto.Stop()
	eng.Shutdown()
	srv.Close()
	return nil
}

func runGUI() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// isTerminal 报告 f 是否连着终端。只看「字符设备」不够：从 Finder、Dock 或开机启动时
// 标准输出是 /dev/null，它也是字符设备，会被误认成终端而打印帮助后退出。
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TIOCGETA)
	return err == nil
}

// headlessHost 是无界面模式的 automation.Host：没有全局快捷键与文件监听。
type headlessHost struct{}

var errNeedsApp = errors.New("needs the SimpleToggle app")

func (headlessHost) RegisterShortcut(string, func()) error { return errNeedsApp }
func (headlessHost) UnregisterShortcut(string)             {}
func (headlessHost) Watch(string, func()) (func(), error)  { return nil, errNeedsApp }
func (headlessHost) Notify(title, body string)             { log.Println(title+":", body) }

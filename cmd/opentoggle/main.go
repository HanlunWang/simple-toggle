// opentoggle 是 OpenToggle 的唯一二进制：菜单栏 app（M2 起）、命令行与 MCP 服务器。
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/HanlunWang/open-toggle/internal/api"
	"github.com/HanlunWang/open-toggle/internal/cli"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/mcp"
	"github.com/HanlunWang/open-toggle/internal/paths"
	"github.com/HanlunWang/open-toggle/internal/platform/darwin"
	"github.com/HanlunWang/open-toggle/internal/presets"
	"github.com/HanlunWang/open-toggle/internal/store"
	"github.com/HanlunWang/open-toggle/internal/version"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(cli.Run([]string{"help"}, &cli.Env{Out: os.Stdout, Err: os.Stderr}))
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
			fmt.Fprintln(os.Stderr, "opentoggle serve:", err)
			os.Exit(1)
		}
	case "gui":
		fmt.Fprintln(os.Stderr, "the OpenToggle v2 menu bar app arrives in milestone M2; use `opentoggle serve` for a headless engine")
		os.Exit(1)
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
	// API 先于状态恢复启动：恢复拉起的 daemon 可能立刻调用 `opentoggle press`
	if err := srv.Start(); err != nil {
		return err
	}
	if err := eng.Start(); err != nil {
		srv.Close()
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	eng.Shutdown()
	srv.Close()
	return nil
}

// Package paths 集中管理 SimpleToggle 的文件位置。
//
//	SIMPLETOGGLE_HOME  配置目录，默认 ~/.config/simple-toggle（脚本、api.json、daemons.json；
//	                   更名前是 ~/.config/open-toggle，第一次启动时复制旧脚本）
//	SIMPLETOGGLE_DATA  数据目录，默认 ~/Library/Application Support/SimpleToggle（state.json、runs.jsonl）
//	SIMPLETOGGLE_PORT  控制 API 端口，默认 43737
//
// 环境变量主要用于测试与验收：让一个完全隔离的实例与正在使用的 app 并存。
package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// Config 是配置目录。
func Config() string {
	if v := os.Getenv("SIMPLETOGGLE_HOME"); v != "" {
		return v
	}
	return filepath.Join(home(), ".config", "simple-toggle")
}

// Scripts 是默认的脚本目录。
func Scripts() string { return filepath.Join(Config(), "switches") }

// Discovery 是控制 API 的发现文件。
func Discovery() string { return filepath.Join(Config(), "api.json") }

// Ledger 是常驻进程的 PID 账本。
func Ledger() string { return filepath.Join(Config(), "daemons.json") }

// Data 是数据目录。
func Data() string {
	if v := os.Getenv("SIMPLETOGGLE_DATA"); v != "" {
		return v
	}
	return filepath.Join(home(), "Library", "Application Support", "SimpleToggle")
}

// LegacyConfig 是更名前（OpenToggle）的配置目录。
func LegacyConfig() string { return filepath.Join(home(), ".config", "open-toggle") }

// MigrateLegacyScripts 在第一次启动时把旧目录（~/.config/open-toggle/switches）里的脚本
// 复制到新的脚本目录：只在新的脚本目录还不存在、且没有用 SIMPLETOGGLE_HOME 指定目录时进行。
// 目录一旦建好就不再迁移：用户删光脚本后，旧脚本不会复活。
// 旧目录原样保留（可以回退到旧版本）。返回复制的文件数。
func MigrateLegacyScripts() (int, error) {
	if os.Getenv("SIMPLETOGGLE_HOME") != "" {
		return 0, nil
	}
	return copyScripts(filepath.Join(LegacyConfig(), "switches"), Scripts())
}

func copyScripts(from, to string) (int, error) {
	if _, err := os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		return 0, nil // 已经启动过（或无法判断）：不迁移
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return 0, nil // 没有旧目录：不需要迁移
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return n, err
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), data, info.Mode().Perm()); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Port 是控制 API 端口。
func Port() int {
	if n, err := strconv.Atoi(os.Getenv("SIMPLETOGGLE_PORT")); err == nil && n > 0 && n < 65536 {
		return n
	}
	return 43737
}

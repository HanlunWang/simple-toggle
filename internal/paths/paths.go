// Package paths 集中管理 OpenToggle 的文件位置。
//
//	OPENTOGGLE_HOME  配置目录，默认 ~/.config/open-toggle（脚本、api.json、daemons.json，与 v1 相同）
//	OPENTOGGLE_DATA  数据目录，默认 ~/Library/Application Support/OpenToggle（state.json、runs.jsonl）
//	OPENTOGGLE_PORT  控制 API 端口，默认 43737
//
// 环境变量主要用于测试与验收：让一个完全隔离的实例与正在使用的 app 并存。
package paths

import (
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
	if v := os.Getenv("OPENTOGGLE_HOME"); v != "" {
		return v
	}
	return filepath.Join(home(), ".config", "open-toggle")
}

// Scripts 是默认的脚本目录。
func Scripts() string { return filepath.Join(Config(), "switches") }

// Discovery 是控制 API 的发现文件。
func Discovery() string { return filepath.Join(Config(), "api.json") }

// Ledger 是常驻进程的 PID 账本。
func Ledger() string { return filepath.Join(Config(), "daemons.json") }

// Data 是数据目录。
func Data() string {
	if v := os.Getenv("OPENTOGGLE_DATA"); v != "" {
		return v
	}
	return filepath.Join(home(), "Library", "Application Support", "OpenToggle")
}

// Port 是控制 API 端口。
func Port() int {
	if n, err := strconv.Atoi(os.Getenv("OPENTOGGLE_PORT")); err == nil && n > 0 && n < 65536 {
		return n
	}
	return 43737
}

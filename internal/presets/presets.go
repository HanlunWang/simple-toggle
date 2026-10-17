// Package presets 是随 app 发布的脚本：一部分首次启动时投放到脚本目录（预置），
// 全部都列在工作台的脚本库里，用户可以随时再添加。
package presets

import (
	"embed"

	"github.com/HanlunWang/open-toggle/internal/engine"
)

//go:embed switches/*.sh library/*.sh
var files embed.FS

// Item 是脚本库里的一项。
type Item struct {
	File     string
	Content  string
	Category string // system | network | focus | dev | media
	Desc     string // 简体中文描述
	DescEn   string
	Seeded   bool // 首次启动时自动投放
	Shown    bool // 投放后默认在面板中显示
}

// catalog 决定投放顺序、默认可见性（与 v1 一致）与脚本库里的分类。
var catalog = []Item{
	{File: "keep-awake.sh", Category: "system", Seeded: true, Shown: true, Desc: "阻止休眠，可设时长与模式，菜单栏显示倒计时。", DescEn: "Prevent sleep for a while, with a countdown in the menu bar."},
	{File: "hidden-files.sh", Category: "system", Seeded: true, Shown: true, Desc: "在 Finder 中显示隐藏文件。", DescEn: "Show hidden files in Finder."},
	{File: "dark-mode.sh", Category: "system", Seeded: true, Shown: true, Desc: "切换系统深色外观。", DescEn: "Switch the system appearance to dark."},
	{File: "mute-audio.sh", Category: "media", Seeded: true, Shown: true, Desc: "静音系统输出。", DescEn: "Mute the system output."},
	{File: "hide-desktop.sh", Category: "system", Seeded: true, Shown: true, Desc: "隐藏桌面图标，适合录屏与演示。", DescEn: "Hide desktop icons for recordings and demos."},
	{File: "dock-autohide.sh", Category: "system", Seeded: true, Shown: true, Desc: "自动隐藏程序坞。", DescEn: "Hide the Dock automatically."},
	{File: "wifi.sh", Category: "network", Seeded: true, Desc: "打开或关闭 Wi-Fi。", DescEn: "Turn Wi-Fi on or off."},
	{File: "http-server.sh", Category: "dev", Seeded: true, Desc: "在指定目录启动静态文件服务器。", DescEn: "Serve a folder over HTTP."},
	{File: "web-proxy.sh", Category: "network", Seeded: true, Desc: "系统 HTTP / HTTPS 代理，地址可配置。", DescEn: "System HTTP and HTTPS proxy with a configurable address."},
	{File: "flush-dns.sh", Category: "network", Desc: "清空 DNS 缓存（需要管理员密码）。", DescEn: "Clear the DNS cache (asks for an administrator password)."},
	{File: "public-ip.sh", Category: "network", Desc: "查询公网 IP 并复制到剪贴板。", DescEn: "Look up your public IP and copy it."},
	{File: "pomodoro.sh", Category: "focus", Desc: "专注计时，结束时通知，菜单栏显示倒计时。", DescEn: "A focus timer that notifies you when the time is up."},
	{File: "mic-mute.sh", Category: "media", Desc: "把输入音量设为 0，关闭时恢复原来的音量。", DescEn: "Set the input volume to zero and restore it afterwards."},
	{File: "derived-data.sh", Category: "dev", Desc: "删除 Xcode 构建缓存，释放磁盘空间。", DescEn: "Delete Xcode's build cache to free disk space."},
	{File: "orbstack.sh", Category: "dev", Desc: "启动或停止 OrbStack 容器引擎。", DescEn: "Start or stop the OrbStack container engine."},
}

func read(file string) (string, bool) {
	for _, dir := range []string{"switches/", "library/"} {
		if data, err := files.ReadFile(dir + file); err == nil {
			return string(data), true
		}
	}
	return "", false
}

// All 返回首次启动时投放的预置脚本。
func All() []engine.Example {
	var out []engine.Example
	for _, it := range catalog {
		if !it.Seeded {
			continue
		}
		if content, ok := read(it.File); ok {
			out = append(out, engine.Example{File: it.File, Content: content, ShownByDefault: it.Shown})
		}
	}
	return out
}

// Library 返回脚本库的全部条目（含内容）。
func Library() []Item {
	out := make([]Item, 0, len(catalog))
	for _, it := range catalog {
		if content, ok := read(it.File); ok {
			it.Content = content
			out = append(out, it)
		}
	}
	return out
}

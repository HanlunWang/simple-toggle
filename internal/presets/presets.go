// Package presets 是随 app 发布的预置开关脚本。
package presets

import (
	"embed"

	"github.com/HanlunWang/open-toggle/internal/engine"
)

//go:embed switches/*.sh
var files embed.FS

// manifest 决定投放顺序与默认可见性（与 v1 一致）：shown=false 的安装后先隐藏。
var manifest = []struct {
	file  string
	shown bool
}{
	{"keep-awake.sh", true},
	{"hidden-files.sh", true},
	{"dark-mode.sh", true},
	{"mute-audio.sh", true},
	{"hide-desktop.sh", true},
	{"dock-autohide.sh", true},
	{"wifi.sh", false},
	{"http-server.sh", false},
	{"web-proxy.sh", false},
}

// All 返回全部预置脚本。
func All() []engine.Example {
	out := make([]engine.Example, 0, len(manifest))
	for _, m := range manifest {
		data, err := files.ReadFile("switches/" + m.file)
		if err != nil {
			continue
		}
		out = append(out, engine.Example{File: m.file, Content: string(data), ShownByDefault: m.shown})
	}
	return out
}

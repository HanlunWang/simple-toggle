// Package app 是 SimpleToggle 的界面层（mygo 原生 UI）：菜单栏图标、弹出面板与各个窗口。
// 这是全仓库唯一 import mygo 的地方；引擎与 API 在 internal/ 下独立运行。
package app

import (
	"github.com/egoist/mygo/ui"
)

// 设计令牌（designs/simpletoggle-v2）：暖中性黑白，层级只靠前景色的透明度区分，
// 一个强调色只用在开启态、运行中与焦点上。
type palette struct {
	Dark               bool
	BG, FG, Card, Elev ui.Color
	L1, L2, L3, L4     ui.Color // 前景色 4% / 8% / 14% / 26%
	T1, T2, T3         ui.Color // 前景色 92% / 62% / 40%
	Accent, OnAccent   ui.Color
	Green, Red, Amber  ui.Color
	Ink                bool // 强调色为「墨」（纯黑白）
}

// Accent 是可选的强调色。
type Accent string

const (
	AccentInk    Accent = "ink"
	AccentOrange Accent = "orange"
	AccentBlue   Accent = "blue"
	AccentGreen  Accent = "green"
	AccentViolet Accent = "violet"
)

var accentLight = map[Accent]string{AccentOrange: "#f54e00", AccentBlue: "#2f6fed", AccentGreen: "#1f8a65", AccentViolet: "#7c5cff"}
var accentDark = map[Accent]string{AccentOrange: "#ff6a2b", AccentBlue: "#5b8cff", AccentGreen: "#4cb38a", AccentViolet: "#9b85ff"}

func alpha(c ui.Color, a float32) ui.Color { return ui.RGBA(c.R, c.G, c.B, a) }

func newPalette(dark bool, accent Accent) palette {
	p := palette{Dark: dark}
	if dark {
		p.BG, p.FG, p.Card, p.Elev = ui.Hex("#14120b"), ui.Hex("#edecec"), ui.Hex("#1b1913"), ui.Hex("#1d1b15")
		p.Green, p.Red, p.Amber = ui.Hex("#4cb38a"), ui.Hex("#e0607e"), ui.Hex("#d9a441")
	} else {
		p.BG, p.FG, p.Card, p.Elev = ui.Hex("#f7f7f4"), ui.Hex("#26251e"), ui.Hex("#f2f1ed"), ui.Hex("#fbfbf9")
		p.Green, p.Red, p.Amber = ui.Hex("#1f8a65"), ui.Hex("#cf2d56"), ui.Hex("#b7791f")
	}
	p.L1, p.L2, p.L3, p.L4 = alpha(p.FG, 0.04), alpha(p.FG, 0.08), alpha(p.FG, 0.14), alpha(p.FG, 0.26)
	p.T1, p.T2, p.T3 = alpha(p.FG, 0.92), alpha(p.FG, 0.62), alpha(p.FG, 0.40)
	table := accentLight
	if dark {
		table = accentDark
	}
	if hex, ok := table[accent]; ok {
		p.Accent, p.OnAccent = ui.Hex(hex), ui.Hex("#ffffff")
	} else {
		p.Accent, p.OnAccent, p.Ink = p.FG, p.BG, true
	}
	return p
}

// theme 把调色板映射为控件用的 ui.Theme。
func (p palette) theme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Dark = p.Dark
	t.Background, t.Surface = p.BG, p.Card
	t.SurfaceHover, t.SurfacePressed = alpha(p.FG, 0.06), alpha(p.FG, 0.10)
	t.Border = p.L3
	t.Text, t.TextMuted = p.T1, p.T2
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = p.Accent, alpha(p.Accent, 0.88), alpha(p.Accent, 0.78), p.OnAccent
	t.Danger, t.Warning, t.Success = p.Red, p.Amber, p.Green
	t.Selection, t.Focus = alpha(p.Accent, 0.22), alpha(p.Accent, 0.45)
	t.Inverse, t.InverseText = p.FG, p.BG
	t.Radius = 7
	return &t
}

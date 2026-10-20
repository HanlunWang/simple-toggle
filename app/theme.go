// Package app 是 SimpleToggle 的界面层（mygo 原生 UI）：菜单栏图标、弹出面板与各个窗口。
// 这是全仓库唯一 import mygo 的地方；引擎与 API 在 internal/ 下独立运行。
package app

import (
	"github.com/egoist/mygo/ui"
)

// 设计语言 v2（designs/simpletoggle-style-lab）：「安静的界面，会发光的状态」。
// 黑白为底，层级只靠前景色的透明度；一个信号色只用在开启、运行中与焦点上。
// 菜单栏面板是 B（Console）风格，工作台与其他窗口是柔化的 C（Signal）风格，开关统一用 A 的圆形发光拨杆。
type palette struct {
	Dark                    bool
	BG, BG2, FG, Card, Elev ui.Color // 底 · 侧栏 · 墨色 · 卡片 · 浮层
	L1, L2, L3, L4          ui.Color // 前景色 4% / 8% / 13% / 22%：发丝线与填充
	T1, T2, T3, T4          ui.Color // 前景色 94% / 64% / 42% / 26%：文字层级
	Accent, OnAccent        ui.Color // 信号色与其上的文字
	Green, Red, Amber       ui.Color
	Ink                     bool   // 信号色就是墨色（石墨）
	Surface                 string // surfacePanel（B）或 surfaceWork（C）
}

const (
	surfacePanel = "panel"
	surfaceWork  = "work"
)

// Accent 是配色方案（设置里的「配色」）；名字沿用旧的强调色设置。
type Accent string

const (
	PalGraphite Accent = "graphite" // Vercel：纯中性灰，信号色就是墨色
	PalSignal   Accent = "signal"   // Nothing：纯黑白 + 唯一的红
	PalPaper    Accent = "paper"    // Cursor：暖中性 + 橙
	PalPeach    Accent = "peach"    // opencode：桃色
	PalPhosphor Accent = "phosphor" // 磷光绿
	PalIce      Accent = "ice"      // 冰蓝
)

var palettes = []Accent{PalGraphite, PalSignal, PalPaper, PalPeach, PalPhosphor, PalIce}

// 每套配色 7 个基色：bg、bg2、card、elev、fg、signal、onSignal（深色 / 浅色）
var palDark = map[Accent][7]string{
	PalGraphite: {"#0a0a0a", "#111111", "#161616", "#1c1c1c", "#ededed", "#f2f2f2", "#0a0a0a"},
	PalSignal:   {"#000000", "#0b0b0b", "#121212", "#191919", "#f5f5f5", "#ff2a1f", "#ffffff"},
	PalPaper:    {"#14120b", "#1b1913", "#1f1d16", "#25231b", "#edecec", "#ff6a2b", "#ffffff"},
	PalPeach:    {"#0a0a0a", "#141414", "#1a1a1a", "#1e1e1e", "#eeeeee", "#fab283", "#1a1009"},
	PalPhosphor: {"#060907", "#0b100c", "#101711", "#141d16", "#d9e8dc", "#7fd88f", "#06120a"},
	PalIce:      {"#0a0a0a", "#111111", "#161616", "#1c1c1c", "#ededed", "#52a8ff", "#00142b"},
}
var palLight = map[Accent][7]string{
	PalGraphite: {"#fafafa", "#f2f2f2", "#ffffff", "#ffffff", "#171717", "#171717", "#ffffff"},
	PalSignal:   {"#ffffff", "#f3f3f3", "#ffffff", "#ffffff", "#000000", "#d71921", "#ffffff"},
	PalPaper:    {"#f7f7f4", "#f0efea", "#fbfbf9", "#ffffff", "#26251e", "#f54e00", "#ffffff"},
	PalPeach:    {"#ffffff", "#fafafa", "#ffffff", "#ffffff", "#1a1a1a", "#d07a2d", "#ffffff"},
	PalPhosphor: {"#f4f7f3", "#ebf0ea", "#fbfdfb", "#ffffff", "#13201a", "#1f8a4c", "#ffffff"},
	PalIce:      {"#ffffff", "#f7f7f7", "#ffffff", "#ffffff", "#171717", "#006bff", "#ffffff"},
}

// normalizePalette 把未知值与 2.0 的强调色名换成配色方案。
func normalizePalette(a Accent) Accent {
	switch a {
	case "ink", "violet", "":
		return PalGraphite
	case "orange":
		return PalPaper
	case "blue":
		return PalIce
	case "green":
		return PalPhosphor
	}
	if _, ok := palDark[a]; ok {
		return a
	}
	return PalGraphite
}

func alpha(c ui.Color, a float32) ui.Color { return ui.RGBA(c.R, c.G, c.B, a) }

func newPalette(dark bool, accent Accent) palette {
	accent = normalizePalette(accent)
	v := palLight[accent]
	if dark {
		v = palDark[accent]
	}
	p := palette{Dark: dark, Surface: surfaceWork}
	p.BG, p.BG2, p.Card, p.Elev, p.FG = ui.Hex(v[0]), ui.Hex(v[1]), ui.Hex(v[2]), ui.Hex(v[3]), ui.Hex(v[4])
	p.Accent, p.OnAccent = ui.Hex(v[5]), ui.Hex(v[6])
	p.Ink = accent == PalGraphite
	if dark {
		p.Green, p.Red, p.Amber = ui.Hex("#46c08a"), ui.Hex("#ff5f57"), ui.Hex("#e8a73c")
	} else {
		p.Green, p.Red, p.Amber = ui.Hex("#1f8a65"), ui.Hex("#d1383d"), ui.Hex("#b7791f")
	}
	if accent == PalSignal {
		p.Red = p.Accent // Nothing：红色是唯一的信号色，错误靠 ✕ 记号区分
	}
	p.L1, p.L2, p.L3, p.L4 = alpha(p.FG, 0.04), alpha(p.FG, 0.08), alpha(p.FG, 0.13), alpha(p.FG, 0.22)
	p.T1, p.T2, p.T3, p.T4 = alpha(p.FG, 0.94), alpha(p.FG, 0.64), alpha(p.FG, 0.42), alpha(p.FG, 0.26)
	return p
}

// panelStyle 返回菜单栏面板用的 B 风格调色板。
func (p palette) panelStyle() palette { p.Surface = surfacePanel; return p }

// 信号色的几档透明度
func (p palette) sigSoft() ui.Color { return alpha(p.Accent, 0.14) }
func (p palette) sigLine() ui.Color { return alpha(p.Accent, 0.55) }
func (p palette) glow() ui.Color    { return alpha(p.Accent, 0.55) }
func (p palette) glowSoft() ui.Color {
	return alpha(p.Accent, 0.24)
}

// 圆角：面板（B）5 / 8 / 12，工作台（柔化的 C）4 / 6 / 10
func (p palette) rCtl() float32 {
	if p.Surface == surfacePanel {
		return 5
	}
	return 4
}
func (p palette) rCard() float32 {
	if p.Surface == surfacePanel {
		return 8
	}
	return 6
}

// theme 把调色板映射为控件用的 ui.Theme。
func (p palette) theme(base *ui.Theme) *ui.Theme {
	registerFonts()
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
	t.Radius = p.rCtl()
	t.Font = uiFont
	return &t
}

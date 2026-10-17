package app

import (
	"github.com/egoist/mygo/ui"
)

// 工作台与设置共用的小控件：按设计稿的尺寸与配色画，不依赖 mygo 默认外观。

type btnKind int

const (
	btnPlain   btnKind = iota // 描边
	btnPrimary                // 实心（强调色或墨色）
	btnGhost                  // 无边框
	btnDanger                 // 描边，红字
)

// button 是一个小号按钮：可选图标、文字、快捷键提示。disabled 时不响应点击。
func (p palette) button(c *ui.Context, key, label, iconName string, kind btnKind, disabled bool) ui.Element {
	b := ui.Row(c.Key(key)).Height(26).Padding(0, 10).Radius(7).Gap(6).AlignItems(ui.Center).Label(label)
	fg := p.T1
	switch kind {
	case btnPrimary:
		bg := p.Accent
		if p.Ink {
			bg = p.FG
		}
		if b.Hovered() && !disabled {
			bg = alpha(bg, 0.88)
		}
		b.Background(bg)
		fg = p.OnAccent
	case btnGhost:
		if b.Hovered() && !disabled {
			b.Background(p.L2)
		}
		fg = p.T2
		if b.Hovered() {
			fg = p.T1
		}
	default:
		b.Border(1, p.L3)
		if b.Hovered() && !disabled {
			b.Background(p.L1)
		}
		if kind == btnDanger {
			fg = p.Red
		}
	}
	if disabled {
		b.Opacity(0.45)
	}
	b.Children(func() {
		if iconName != "" {
			icon(c, iconName, 13).TextColor(fg)
		}
		if label != "" {
			ui.Text(c, label).FontSize(12.5).TextColor(fg).SingleLine()
		}
	})
	return b
}

// clicked 返回按钮是否被点击（禁用时恒为 false）。
func clicked(b ui.Element, disabled bool) bool { return !disabled && b.Clicked() }

// iconBtn 是 26×26 的图标按钮；active 时有底色。
func (p palette) iconBtn(c *ui.Context, key, name, tip string, active bool) ui.Element {
	b := ui.Row(c.Key(key)).Size(26, 26).Radius(7).AlignItems(ui.Center).Justify(ui.Center).Tooltip(tip).Label(tip)
	if active || b.Hovered() {
		b.Background(p.L2)
	}
	col := p.T2
	if active || b.Hovered() {
		col = p.T1
	}
	b.Children(func() { icon(c, name, 14).TextColor(col) })
	return b
}

// badge 是小号标签。tone：""（中性）、"green"、"red"、"amber"、"accent"。
func (p palette) badge(c *ui.Context, text, tone string) ui.Element {
	bg, fg := p.L2, p.T2
	switch tone {
	case "green":
		bg, fg = alpha(p.Green, 0.14), p.Green
	case "red":
		bg, fg = alpha(p.Red, 0.14), p.Red
	case "amber":
		bg, fg = alpha(p.Amber, 0.16), p.Amber
	case "accent":
		if !p.Ink {
			bg, fg = alpha(p.Accent, 0.14), p.Accent
		}
	}
	b := ui.Row(c).Height(18).Padding(0, 6).Radius(5).AlignItems(ui.Center).Background(bg)
	b.Children(func() { ui.Text(c, text).FontSize(11).FontWeight(500).TextColor(fg).SingleLine() })
	return b
}

// hair 是 1px 分隔线。
func (p palette) hair(c *ui.Context) ui.Element {
	return ui.Box(c).FillWidth().Height(1).Shrink(0).Background(p.L2)
}

// vhair 是竖向 1px 分隔线。
func (p palette) vhair(c *ui.Context) ui.Element {
	return ui.Box(c).Width(1).FillHeight().Shrink(0).Background(p.L2)
}

// caption 是区块小标题（11px，次级颜色）。
func (p palette) caption(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(p.T3).SingleLine()
}

// hint 是说明文字。
func (p palette) hint(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).TextColor(p.T3).Wrap()
}

// segmented 是等宽分段控件；返回新选中的序号（未变化时与 selected 相同）。
func (p palette) segmented(c *ui.Context, key string, selected int, labels ...string) int {
	out := selected
	ui.Row(c.Key(key)).FillWidth().Height(26).Padding(2).Radius(7).Background(p.L2).Gap(2).Children(func() {
		for i, l := range labels {
			s := ui.Row(c.Key(i)).Grow(1).Basis(0).FillHeight().Radius(5).AlignItems(ui.Center).Justify(ui.Center).Label(l)
			col := p.T2
			if i == selected {
				s.Background(p.BG).Shadow(0, 1, 2, 0, alpha(p.FG, 0.12))
				col = p.T1
			} else if s.Hovered() {
				col = p.T1
			}
			s.Children(func() { ui.Text(c, l).FontSize(12).TextColor(col).SingleLine() })
			if s.Clicked() {
				out = i
			}
		}
	})
	return out
}

// navItem 是侧栏的一行：图标、文字、右侧计数或附件。
func (p palette) navItem(c *ui.Context, key, iconName, label string, selected, dim bool, trailing func()) ui.Element {
	r := ui.Row(c.Key(key)).FillWidth().Height(28).Padding(0, 8).Radius(7).Gap(9).AlignItems(ui.Center).Label(label)
	if selected {
		r.Background(p.L2)
	} else if r.Hovered() {
		r.Background(p.L1)
	}
	col := p.T2
	if selected {
		col = p.T1
	}
	if dim {
		col = p.T3
	}
	r.Children(func() {
		if iconName != "" {
			icon(c, iconName, 15).TextColor(col)
		}
		t := ui.Text(c, label).FontSize(13).TextColor(col).SingleLine().Grow(1).MinWidth(0)
		if selected {
			t.FontWeight(500)
		}
		if trailing != nil {
			trailing()
		}
	})
	return r
}

// mono 是等宽小字。
func (p palette) mono(c *ui.Context, text string, size float32, col ui.Color) ui.Element {
	return ui.Text(c, text).Font("monospace").FontSize(size).TextColor(col).SingleLine()
}

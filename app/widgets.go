package app

import (
	"strings"

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
	} else {
		pressPose(b, 0.96)
	}
	b.Transition(motionColors)
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
	on := active || b.Hovered()
	b.Background(b.AnimateColor("bg", pick(on, p.L2, ui.Transparent), animHover))
	col := b.AnimateColor("fg", pick(on, p.T1, p.T2), animHover)
	pressPose(b, 0.92)
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
	// 终端风格的小标题：等宽、大写、加字距（中文不受大写影响）
	return ui.Text(c, strings.ToUpper(text)).Font(monoFont).FontSize(10.5).FontWeight(500).LetterSpacing(0.7).
		TextColor(p.T3).SingleLine()
}

// count 是侧栏、标题旁的计数：等宽，数字变化时滚动。
func (p palette) count(c *ui.Context, key string, n int) ui.Element {
	return p.countText(c, key, itoa(n))
}

func (p palette) countText(c *ui.Context, key, text string) ui.Element {
	return ui.Text(c.Key(key), text).Font(monoFont).FontSize(11).TextColor(p.T3).SingleLine().
		ContentTransition(ui.ContentTransition{Numeric: true})
}

// hint 是说明文字。
func (p palette) hint(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).TextColor(p.T3).Wrap()
}

// segmented 是等宽分段控件；返回新选中的序号（未变化时与 selected 相同）。
// 选中块由轨道自己画，在分段之间用弹簧滑动。
func (p palette) segmented(c *ui.Context, key string, selected int, labels ...string) int {
	out := selected
	n := len(labels)
	track := ui.Row(c.Key(key)).FillWidth().Height(26).Padding(2).Radius(7).Background(p.L2).Gap(2)
	pos := track.AnimateTo("thumb", float32(selected), animSlide)
	track.Draw(func(pt *ui.Painter, r ui.Rect) {
		if n == 0 || selected < 0 {
			return
		}
		const pad, gap = 2, 2
		w := (r.W - 2*pad - float32(n-1)*gap) / float32(n)
		x := r.X + pad + pos*(w+gap)
		thumb := ui.Rect{X: x, Y: r.Y + pad, W: w, H: r.H - 2*pad}
		pt.Shadow(thumb, 5, 0, 1, 2, 0, alpha(p.FG, 0.12))
		pt.Fill(thumb, p.BG, 5)
	})
	track.Children(func() {
		for i, l := range labels {
			s := ui.Row(c.Key(i)).Grow(1).Basis(0).FillHeight().Radius(5).AlignItems(ui.Center).Justify(ui.Center).Label(l)
			col := p.T2
			if i == selected || s.Hovered() {
				col = p.T1
			}
			s.Children(func() { ui.Text(c, l).FontSize(12).TextColor(s.AnimateColor("fg", col, animHover)).SingleLine() })
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
	r.Background(r.AnimateColor("hover", pick(r.Hovered() && !selected, p.L1, ui.Transparent), animHover))
	col := p.T2
	if selected {
		col = p.T1
	}
	if dim {
		col = p.T3
	}
	col = r.AnimateColor("fg", col, animHover)
	r.Children(func() {
		if selected { // 选中底色在同一组的条目之间滑动（组 = key 的前缀）
			group := key
			if i := strings.IndexByte(key, '-'); i > 0 {
				group = key[:i]
			}
			ui.Box(c.Key("sel")).Absolute().Top(0).Left(0).Fill().PassThrough().Radius(7).Background(p.L2).
				Transition(ui.ElementTransition{Animation: &animSlide, Match: slideMatch{group}})
		}
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

// monoFont 是界面里的等宽字体（系统的 SF Mono）。
const monoFont = "monospace"

// mono 是等宽小字。
func (p palette) mono(c *ui.Context, text string, size float32, col ui.Color) ui.Element {
	return ui.Text(c, text).Font(monoFont).FontSize(size).TextColor(col).SingleLine()
}

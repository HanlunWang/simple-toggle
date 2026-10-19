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

// button 是一个小号按钮：可选图标、文字。disabled 时不响应点击。
// 工作台（C）里按钮文字是等宽字；圆角随表面（4 / 5）。
func (p palette) button(c *ui.Context, key, label, iconName string, kind btnKind, disabled bool) ui.Element {
	b := ui.Row(c.Key(key)).Height(26).Padding(0, 10).Radius(p.rCtl()).Gap(6).AlignItems(ui.Center).Label(label)
	hover := b.Hovered() && !disabled
	fg, bg, bd := p.T1, ui.Transparent, p.L3
	switch kind {
	case btnPrimary:
		bg, bd, fg = p.FG, p.FG, p.BG
		if hover {
			bg = alpha(p.FG, 0.88)
		}
	case btnGhost:
		bd, fg = ui.Transparent, p.T2
		if hover {
			bg, fg = p.L2, p.T1
		}
	default:
		if hover {
			bg, bd = p.L1, p.L4
		}
		if kind == btnDanger {
			fg = p.Red
		}
	}
	b.Background(b.AnimateColor("bg", bg, animHover)).Border(1, b.AnimateColor("bd", bd, animHover))
	if disabled {
		b.Opacity(0.4)
	} else {
		pressPose(b, 0.96)
	}
	col := b.AnimateColor("fg", fg, animHover)
	b.Children(func() {
		if iconName != "" {
			icon(c, iconName, 13).TextColor(col)
		}
		if label != "" {
			t := ui.Text(c, label).FontSize(12.5).TextColor(col).SingleLine()
			if p.Surface == surfaceWork {
				t.Font(monoFont).FontSize(11.5)
			}
		}
	})
	return b
}

// sigButton 是信号色的主操作（激活场景等），带一点光。
func (p palette) sigButton(c *ui.Context, key, label, iconName string) ui.Element {
	b := ui.Row(c.Key(key)).Height(26).Padding(0, 11).Radius(p.rCtl()).Gap(6).AlignItems(ui.Center).Label(label).
		Background(p.Accent).Shadow(0, 0, 12, 0, p.glowSoft())
	if b.Hovered() {
		b.Background(alpha(p.Accent, 0.9))
	}
	pressPose(b, 0.96)
	b.Children(func() {
		if iconName != "" {
			icon(c, iconName, 13).TextColor(p.OnAccent)
		}
		ui.Text(c, label).Font(monoFont).FontSize(11.5).TextColor(p.OnAccent).SingleLine()
	})
	return b
}

// clicked 返回按钮是否被点击（禁用时恒为 false）。
func clicked(b ui.Element, disabled bool) bool { return !disabled && b.Clicked() }

// iconBtn 是 26×26 的图标按钮；active 时有底色。
func (p palette) iconBtn(c *ui.Context, key, name, tip string, active bool) ui.Element {
	b := ui.Row(c.Key(key)).Size(26, 26).Radius(p.rCtl() + 1).AlignItems(ui.Center).Justify(ui.Center).Tooltip(tip).Label(tip)
	on := active || b.Hovered()
	b.Background(b.AnimateColor("bg", pick(on, p.L2, ui.Transparent), animHover))
	col := b.AnimateColor("fg", pick(on, p.T1, p.T2), animHover)
	pressPose(b, 0.92)
	b.Children(func() { icon(c, name, 14).TextColor(col) })
	return b
}

// badge 是小号标签。tone：""（中性）、"green"、"red"、"amber"、"accent"。
// 工作台（C）：等宽字、透明底、1px 同色描边；面板（B）：等宽字、淡色底。
func (p palette) badge(c *ui.Context, text, tone string) ui.Element {
	fg := p.T2
	switch tone {
	case "green":
		fg = p.Green
	case "red":
		fg = p.Red
	case "amber":
		fg = p.Amber
	case "accent":
		fg = p.Accent
	}
	b := ui.Row(c).Height(18).Padding(0, 6).AlignItems(ui.Center)
	if p.Surface == surfaceWork {
		b.Radius(2).Border(1, pick(tone == "", p.L4, alpha(fg, 0.7)))
	} else {
		b.Radius(4).Background(pick(tone == "", p.L2, alpha(fg, 0.15)))
	}
	b.Children(func() { ui.Text(c, text).Font(monoFont).FontSize(10.5).TextColor(fg).SingleLine() })
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

// caption 是区块小标题：等宽、大写、加字距（中文不受大写影响）。
// 面板（B）前面是「// 」，工作台（C）前面是一个信号色的小方点。
func (p palette) caption(c *ui.Context, text string) ui.Element {
	return ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
		if p.Surface == surfacePanel {
			ui.Text(c, "//").Font(monoFont).FontSize(10.5).TextColor(p.T4)
		} else {
			ui.Box(c).Size(4, 4).Radius(0.5).Background(p.Accent)
		}
		ui.Text(c, strings.ToUpper(text)).Font(monoFont).FontSize(10.5).FontWeight(500).LetterSpacing(0.9).
			TextColor(p.T3).SingleLine()
	})
}

// frame 是框线区块（TUI 的 ┌─ 标题 ─┐）：1px 发丝线框，小标题压在上边框上，右侧可放计数等。
func (p palette) frame(c *ui.Context, key, title string, trailing, body func()) ui.Element {
	return ui.Column(c.Key(key)).FillWidth().Margin(8, 0, 0).Padding(16, 12, 12).Radius(p.rCard()).Border(1, p.L2).Gap(10).Children(func() {
		ui.Row(c.Key("t")).Absolute().Top(-8).Left(10).Padding(0, 6).Background(p.BG).Children(func() { p.caption(c, title) })
		if trailing != nil {
			ui.Row(c.Key("r")).Absolute().Top(-9).Right(10).Padding(0, 6).Background(p.BG).AlignItems(ui.Center).Children(trailing)
		}
		body()
	})
}

// count 是侧栏、标题旁的计数：等宽，数字变化时滚动。
func (p palette) count(c *ui.Context, key string, n int) ui.Element {
	return p.countText(c, key, itoa(n))
}

func (p palette) countText(c *ui.Context, key, text string) ui.Element {
	return ui.Text(c.Key(key), text).Font(monoFont).FontSize(11).TextColor(p.T4).SingleLine().
		ContentTransition(ui.ContentTransition{Numeric: true})
}

// hint 是说明文字。
func (p palette) hint(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).TextColor(p.T3).Wrap()
}

// segmented 是等宽分段控件；返回新选中的序号（未变化时与 selected 相同）。
// 工作台（C）：透明底 + 1px 框，选中块是浅底 + 底部 2px 信号色，在分段之间弹簧滑动。
func (p palette) segmented(c *ui.Context, key string, selected int, labels ...string) int {
	out := selected
	n := len(labels)
	r := p.rCtl()
	track := ui.Row(c.Key(key)).FillWidth().Height(26).Padding(2).Radius(r).Border(1, p.L3).Gap(2)
	pos := track.AnimateTo("thumb", float32(selected), animSlide)
	track.Draw(func(pt *ui.Painter, rr ui.Rect) {
		if n == 0 || selected < 0 {
			return
		}
		const pad, gap = 3, 2 // 框线 1 + 内边距 2
		w := (rr.W - 2*pad - float32(n-1)*gap) / float32(n)
		x := rr.X + pad + pos*(w+gap)
		thumb := ui.Rect{X: x, Y: rr.Y + pad, W: w, H: rr.H - 2*pad}
		pt.Fill(thumb, p.L2, r-2)
		pt.Fill(ui.Rect{X: thumb.X + 2, Y: thumb.Y + thumb.H - 2, W: thumb.W - 4, H: 2}, p.Accent, 1)
	})
	track.Children(func() {
		for i, l := range labels {
			s := ui.Row(c.Key(i)).Grow(1).Basis(0).FillHeight().Radius(r - 2).AlignItems(ui.Center).Justify(ui.Center).Label(l)
			col := p.T2
			if i == selected || s.Hovered() {
				col = p.T1
			}
			s.Children(func() {
				ui.Text(c, l).Font(monoFont).FontSize(11).TextColor(s.AnimateColor("fg", col, animHover)).SingleLine()
			})
			if s.Clicked() {
				out = i
			}
		}
	})
	return out
}

// navItem 是侧栏的一行：图标、文字、右侧计数或附件。
// 工作台（C）：等宽字；选中时文字变成信号色，左边一个 ›，没有底色块。
func (p palette) navItem(c *ui.Context, key, iconName, label string, selected, dim bool, trailing func()) ui.Element {
	r := ui.Row(c.Key(key)).FillWidth().Height(28).Padding(0, 8, 0, 14).Radius(p.rCtl()).Gap(9).AlignItems(ui.Center).Label(label)
	r.Background(r.AnimateColor("hover", pick(r.Hovered() && !selected, p.L1, ui.Transparent), animHover))
	col := p.T2
	switch {
	case selected:
		col = p.Accent
	case dim:
		col = p.T3
	case r.Hovered():
		col = p.T1
	}
	col = r.AnimateColor("fg", col, animColor)
	r.Children(func() {
		if selected { // 选中的 › 在同一组的条目之间滑动（组 = key 的前缀）
			group := key
			if i := strings.IndexByte(key, '-'); i > 0 {
				group = key[:i]
			}
			ui.Text(c.Key("sel"), "›").Font(monoFont).FontSize(13).FontWeight(600).TextColor(p.Accent).
				Absolute().Left(2).Top(5).PassThrough().
				Transition(ui.ElementTransition{Animation: &animSlide, Match: slideMatch{group}})
		}
		if iconName != "" {
			icon(c, iconName, 15).TextColor(col)
		}
		t := ui.Text(c, label).Font(monoFont).FontSize(12.5).TextColor(col).SingleLine().Grow(1).MinWidth(0)
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
	return ui.Text(c, text).Font(monoFont).FontSize(size).TextColor(col).SingleLine()
}

// paletteGrid 是配色方案的选择：每张卡片左半深色、右半浅色，各画一个开着的发光开关。
func (p palette) paletteGrid(c *ui.Context, cur Accent, set func(Accent)) {
	ui.Grid(c.Key("palettes")).FillWidth().Columns(3).Gap(8).Children(func() {
		for _, id := range palettes {
			id := id
			sel := cur == id
			card := ui.Column(c.Key("pal-" + string(id))).Padding(9).Gap(7).Radius(p.rCard()).Label(tr("pal_" + string(id)))
			card.Border(1, card.AnimateColor("bd", pick(sel, p.sigLine(), pick(card.Hovered(), p.L3, p.L2)), animColor)).
				Shadow(0, 0, 14, 0, card.AnimateColor("glow", pick(sel, p.glowSoft(), alpha(p.Accent, 0)), animColor))
			pressPose(card, 0.98)
			card.Children(func() {
				ui.Row(c).FillWidth().Height(30).Radius(4).Clip().Children(func() {
					for _, dark := range []bool{true, false} {
						sw := newPalette(dark, id)
						ui.Box(c).Grow(1).Basis(0).FillHeight().Background(sw.BG).Draw(func(pt *ui.Painter, r ui.Rect) {
							tr := ui.Rect{X: r.X + 10, Y: r.Y + (r.H-14)/2, W: 26, H: 14}
							pt.Shadow(tr, 7, 0, 0, 6, 0, sw.glowSoft())
							pt.Fill(tr, sw.sigSoft(), 7)
							pt.Stroke(tr, sw.sigLine(), 7, 1)
							k := ui.Rect{X: tr.X + tr.W - 11, Y: tr.Y + 3, W: 8, H: 8}
							pt.Shadow(k, 4, 0, 0, 5, 0, sw.glow())
							pt.Fill(k, sw.Accent, 4)
							pt.Fill(ui.Rect{X: tr.X + tr.W + 7, Y: r.Y + r.H/2 - 2, W: 22, H: 4}, alpha(sw.FG, 0.7), 2)
						})
					}
				})
				ui.Text(c, tr("pal_"+string(id))).FontSize(12.5).FontWeight(500).TextColor(p.T1).SingleLine()
				ui.Text(c, tr("pal_"+string(id)+"_h")).Font(monoFont).FontSize(10.5).TextColor(p.T3).SingleLine().Ellipsis("…")
			})
			if card.Clicked() && !sel {
				set(id)
			}
		}
	})
}

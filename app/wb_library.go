package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/presets"
)

// 脚本库：随 app 发布的脚本，按分类浏览、预览源码、一键添加到脚本目录。

var libCats = []string{"all", "system", "network", "focus", "dev", "media"}

type libEntry struct {
	presets.Item
	header contract.Header
}

var libCache []libEntry

func library() []libEntry {
	if libCache == nil {
		for _, it := range presets.Library() {
			h, _ := contract.Parse(it.Content)
			libCache = append(libCache, libEntry{it, h})
		}
	}
	return libCache
}

func (e libEntry) desc() string {
	if strs["quit"] == "Quit" {
		return e.DescEn
	}
	return e.Desc
}

func (w *workbench) installed(file string) bool { return w.view(file) != nil }

func (w *workbench) libItems() []libEntry {
	q := strings.ToLower(strings.TrimSpace(w.libQuery))
	var out []libEntry
	for _, e := range library() {
		if w.libCat != "all" && e.Category != w.libCat {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.header.Name), q) && !strings.Contains(strings.ToLower(e.desc()), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (w *workbench) libraryCats(c *ui.Context) {
	pal := w.pal
	counts := map[string]int{}
	for _, e := range library() {
		counts["all"]++
		counts[e.Category]++
	}
	ui.Column(c).FillWidth().Padding(6, 0, 0).Gap(1).Children(func() {
		for _, cat := range libCats {
			cat := cat
			n := counts[cat]
			if n == 0 {
				continue
			}
			item := pal.navItem(c, "lc-"+cat, "", tr("cat_"+cat), w.libCat == cat, false, func() {
				pal.count(c, "n", n)
			})
			if item.Clicked() {
				w.libCat = cat
			}
		}
	})
}

func (w *workbench) install(e libEntry) {
	err := w.b.InstallScript(e.File, e.Content)
	switch {
	case errors.Is(err, engine.ErrExists):
		w.setNotice(tr("lib_exists"), true)
	case err != nil:
		w.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
	default:
		w.dirty = true
	}
}

func (w *workbench) library(c *ui.Context) {
	pal := w.pal
	items := w.libItems()
	w.topBar(c, func() {
		ui.Row(c).Grow(1).Gap(7).AlignItems(ui.Center).Children(func() {
			icon(c, "book-open", 15).TextColor(pal.T1)
			ui.Text(c, tr("lib_title")).FontSize(13).FontWeight(500).TextColor(pal.T1)
			pal.mono(c, fmt.Sprintf(tr("lib_n"), len(items)), 12, pal.T3)
			if w.notice != "" {
				col := pal.T2
				if w.noticeErr {
					col = pal.Red
				}
				w.noticeLabel(c, col)
			}
		})
		ui.SearchField(c.Key("lib-q"), &w.libQuery).Width(220).Placeholder(tr("lib_search"))
	})
	var cur *libEntry
	for i := range items {
		if items[i].File == w.libSel {
			cur = &items[i]
		}
	}
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		sc := ui.Scroll(c.Key("lib-scroll")).Grow(1).MinWidth(0).FillHeight().Padding(20)
		// 列数按这一帧的窗口宽度算（侧栏 236 + 分隔线、抽屉 380 + 分隔线、左右留白 40），
		// 不读上一帧的尺寸：否则切到脚本库的第一帧会按默认宽度画，再突然缩小
		winW, _ := c.Size()
		width := winW - 237 - 40
		if cur != nil {
			width -= 381
		}
		sc.Children(func() {
			ui.Grid(c.Key("lib-grid")).FillWidth().Columns(max(1, int((width+10)/220))).Gap(10).Children(func() {
				for i, e := range items {
					w.libCard(c, e, i)
				}
			})
		})
		if cur != nil {
			pal.vhair(c)
			w.libDrawer(c, *cur)
		}
	})
}

func (w *workbench) libCard(c *ui.Context, e libEntry, i int) {
	pal := w.pal
	sel := w.libSel == e.File
	card := ui.Column(c.Key("lib-" + e.File)).MinHeight(128).Padding(14).Radius(6).Gap(8).Label(e.header.Name).
		Transition(ui.ElementTransition{Duration: 220 * time.Millisecond, Ease: ui.EaseInOut, Position: true, Size: true,
			Enter: &ui.Motion{Scale: 0.96}, Exit: &ui.Motion{Scale: 0.96}})
	bg, border := ui.Transparent, pal.L2
	switch {
	case sel:
		bg, border = pal.L1, pal.L4
	case card.Hovered():
		bg, border = pal.L1, pal.L3
	}
	card.Background(card.AnimateColor("bg", bg, animHover)).Border(pick(sel, float32(1.5), 1), card.AnimateColor("bd", border, animHover))
	pressPose(card, 0.985)
	riseIn(card, time.Duration(min(i, 12))*25*time.Millisecond) // 第一次出现时逐张浮现
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			switchIcon(c, e.header.Icon, 17).TextColor(pal.T1)
			ui.Spacer(c)
			pal.badge(c, tr("type_"+string(e.header.Type)), "")
		})
		ui.Text(c, e.header.Name).FontSize(13).FontWeight(500).TextColor(pal.T1)
		ui.Text(c, e.desc()).FontSize(11.5).TextColor(pal.T3).Wrap().Grow(1)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, tr("cat_"+e.Category)).FontSize(11).TextColor(pal.T3)
			ui.Spacer(c)
			if w.installed(e.File) {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					icon(c, "check", 12).TextColor(pal.T3)
					ui.Text(c, tr("lib_added")).FontSize(11.5).TextColor(pal.T3)
				})
			} else if pal.button(c, "add", tr("lib_add"), "plus", btnPlain, false).Clicked() {
				w.install(e)
				return
			}
		})
	})
	if card.Clicked() {
		if sel {
			w.libSel = ""
		} else {
			w.libSel = e.File
		}
	}
}

var libPreview = map[string]*editor.State{}

func (w *workbench) libDrawer(c *ui.Context, e libEntry) {
	pal := w.pal
	ui.Column(c.Key("drawer")).Width(380).Shrink(0).FillHeight().Background(pal.BG).
		Transition(ui.ElementTransition{Duration: 200 * time.Millisecond, Enter: &ui.Motion{X: 24}}).Children(func() {
		ui.Row(c).FillWidth().Height(46).Shrink(0).Padding(0, 12, 0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			switchIcon(c, e.header.Icon, 15).TextColor(pal.T1)
			ui.Text(c, e.header.Name).FontSize(13).FontWeight(500).TextColor(pal.T1).Grow(1).SingleLine()
			if pal.iconBtn(c, "drawer-close", "x", tr("collapse"), false).Clicked() {
				w.libSel = ""
			}
		})
		pal.hair(c)
		ui.Column(c).FillWidth().Padding(14, 16).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				pal.badge(c, tr("type_"+string(e.header.Type)), "")
				pal.hint(c, tr("hint_"+string(e.header.Type)))
			})
			ui.Text(c, e.desc()).FontSize(12.5).TextColor(pal.T2).Wrap()
		})
		pal.hair(c)
		st := libPreview[e.File]
		if st == nil {
			buf := editor.NewBuffer(e.Content)
			buf.SetLanguage(editor.LanguageFor(e.File))
			st = &editor.State{Buffer: buf, ReadOnly: true, SoftWrap: true, Label: e.File}
			libPreview[e.File] = st
		}
		th := editor.DefaultTheme(c.Theme())
		th.Background = pal.Card
		st.Theme = th
		editor.View(c.Key("lib-code-"+e.File), st).Grow(1).FillWidth().MinHeight(0)
		pal.hair(c)
		ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center).Children(func() {
			pal.hint(c, tr("lib_after")).Grow(1)
			if w.installed(e.File) {
				pal.button(c, "lib-added", tr("lib_added"), "", btnPlain, true)
			} else if pal.button(c, "lib-add", tr("lib_add_long"), "", btnPrimary, false).Clicked() {
				w.install(e)
			}
		})
	})
}

package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 待确认的修改：AI 提交的脚本先停在这里，用户看过差异后确认写入或拒绝。

const secChanges = "changes"

type changeView struct {
	id   string
	diff *editor.DiffState
	base string // 比较用的旧文本（提交时的版本或当前文件）
}

func (w *workbench) pendingCount() int {
	n := 0
	for _, c := range w.changes {
		if c.Status == "pending" {
			n++
		}
	}
	return n
}

func (w *workbench) changeByID(id string) (store.Change, bool) {
	for _, c := range w.changes {
		if c.ID == id {
			return c, true
		}
	}
	return store.Change{}, false
}

// clientLabel 把 detail（mcp/claude-code、cli/agent:claude-code）写成给人看的客户端名。
func clientLabel(c store.Change) string {
	d := c.Detail
	for _, p := range []string{"mcp/", "cli/agent:", "cli/"} {
		d = strings.TrimPrefix(d, p)
	}
	if d == "" {
		d = c.Source
	}
	return d
}

func (w *workbench) changeList(c *ui.Context) {
	pal := w.pal
	var pending, recent []store.Change
	for _, ch := range w.changes {
		if ch.Status == "pending" {
			pending = append(pending, ch)
		} else if len(recent) < 10 {
			recent = append(recent, ch)
		}
	}
	item := func(ch store.Change) {
		sel := w.selChange == ch.ID
		r := ui.Row(c.Key("chg-"+ch.ID)).FillWidth().Padding(7, 8).Radius(4).Gap(8).AlignItems(ui.Start).Label(ch.Name)
		if sel {
			r.Background(pal.L2)
		} else if r.Hovered() {
			r.Background(pal.L1)
		}
		r.Children(func() {
			name := "file-diff"
			if ch.Kind == "create" {
				name = "file-plus"
			}
			ui.Row(c).Width(16).Margin(2, 0, 0, 0).Justify(ui.Center).Children(func() { icon(c, name, 14).TextColor(pal.T2) })
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, ch.Name).FontSize(12.5).TextColor(pal.T1).SingleLine()
				ui.Text(c, fmt.Sprintf(tr("chg_from"), clientLabel(ch))).FontSize(11.5).TextColor(pal.T3).SingleLine()
			})
			switch ch.Status {
			case "pending":
				ui.Box(c).Size(6, 6).Radius(3).Margin(6, 0, 0, 0).Background(pal.Amber)
			case "approved":
				icon(c, "check", 12).TextColor(pal.Green)
			default:
				icon(c, "x", 12).TextColor(pal.T3)
			}
		})
		if r.Clicked() {
			w.selChange = ch.ID
		}
	}
	ui.Column(c).FillWidth().Padding(6, 0, 0).Gap(1).Children(func() {
		for _, ch := range pending {
			item(ch)
		}
		if len(recent) > 0 {
			ui.Row(c).FillWidth().Padding(12, 8, 4).Children(func() { pal.caption(c, tr("chg_recent")) })
			for _, ch := range recent {
				item(ch)
			}
		}
	})
}

func (w *workbench) changeEditor(c *ui.Context) {
	pal := w.pal
	ch, ok := w.changeByID(w.selChange)
	if !ok {
		for _, x := range w.changes {
			if x.Status == "pending" {
				ch, ok = x, true
				w.selChange = x.ID
				break
			}
		}
	}
	if !ok {
		w.topBar(c, func() {})
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			icon(c, "shield-check", 28).TextColor(pal.T3)
			ui.Text(c, tr("chg_none")).FontSize(14).FontWeight(500).TextColor(pal.T1)
			ui.Text(c, tr("chg_none_h")).FontSize(12.5).TextColor(pal.T3)
		})
		return
	}
	// 比较的旧文本：修改用提交时的版本；磁盘上又变了就用当前文件
	base, moved := ch.Previous, false
	if ch.Kind == "update" && ch.Status == "pending" {
		if cur, err := w.b.Script(ch.Switch); err == nil && cur != ch.Previous {
			base, moved = cur, true
		}
	}
	cv := w.changeViews[ch.ID]
	if cv == nil || cv.base != base {
		old := editor.NewBuffer(base)
		nw := editor.NewBuffer(ch.Content)
		lang := editor.LanguageFor("script.sh")
		old.SetLanguage(lang)
		nw.SetLanguage(lang)
		cv = &changeView{id: ch.ID, base: base, diff: &editor.DiffState{Old: old, New: nw}}
		w.changeViews[ch.ID] = cv
	}
	w.topBar(c, func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
			name := "file-diff"
			kind := tr("chg_update")
			if ch.Kind == "create" {
				name, kind = "file-plus", tr("chg_create")
			}
			icon(c, name, 15).TextColor(pal.T1)
			ui.Text(c, ch.Name).FontSize(13).FontWeight(500).TextColor(pal.T1).SingleLine().MinWidth(0)
			pal.badge(c, kind, "")
			pal.badge(c, fmt.Sprintf(tr("chg_from"), clientLabel(ch)), "accent")
			pal.mono(c, ch.Created.Local().Format("01-02 15:04"), 11.5, pal.T3)
			w.noticeText(c)
		})
		switch ch.Status {
		case "pending":
			if pal.button(c, "chg-reject", tr("chg_reject"), "x", btnPlain, false).Clicked() {
				if _, err := w.b.RejectChange(ch.ID); err != nil {
					w.setNotice(err.Error(), true)
				}
				w.dirty = true
			}
			if pal.button(c, "chg-approve", tr("chg_approve"), "check", btnPrimary, false).Clicked() {
				if got, err := w.b.ApproveChange(ch.ID); err != nil {
					w.setNotice(fmt.Sprintf(tr("chg_failed"), err), true)
				} else if got.Switch != "" {
					delete(w.docs, got.Switch) // 重新从磁盘载入
				}
				w.dirty = true
			}
		case "approved":
			pal.badge(c, tr("chg_approved"), "green")
		case "failed":
			pal.badge(c, fmt.Sprintf(tr("chg_failed"), ch.Error), "red")
		default:
			pal.badge(c, tr("chg_rejected"), "")
		}
	})
	if moved {
		ui.Row(c).FillWidth().Padding(8, 16).Gap(8).AlignItems(ui.Center).Background(alpha(pal.Amber, 0.12)).Children(func() {
			icon(c, "triangle-alert", 14).TextColor(pal.Amber)
			ui.Text(c, tr("chg_disk_moved")).FontSize(12).TextColor(pal.T1).Wrap().Grow(1)
		})
	}
	editor.DiffView(c.Key("diff-"+ch.ID), cv.diff).Grow(1).FillWidth().MinHeight(0)
}

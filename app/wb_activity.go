package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/store"
)

// 活动：全部运行记录（来源、耗时、退出码、输出），按结果和来源筛选，可以导出和清空。

var sourceOrder = []string{"manual", "shortcut", "scene", "automation", "ai", "cli", "api", "restore", "system"}

func (w *workbench) loadRuns() {
	if !w.runsDirty {
		return
	}
	w.runsDirty = false
	w.runs = w.b.Runs(store.Filter{Limit: 2000})
}

func (w *workbench) filteredRuns() []store.Run {
	w.loadRuns()
	if w.actFilter == "all" {
		return w.runs
	}
	var out []store.Run
	for _, r := range w.runs {
		if (w.actFilter == "failed" && !r.OK) || r.Source == w.actFilter {
			out = append(out, r)
		}
	}
	return out
}

func (w *workbench) activityFilters(c *ui.Context) {
	pal := w.pal
	w.loadRuns()
	counts := map[string]int{"all": len(w.runs)}
	for _, r := range w.runs {
		counts[r.Source]++
		if !r.OK {
			counts["failed"]++
		}
	}
	filters := []string{"all", "failed"}
	for _, s := range sourceOrder {
		if counts[s] > 0 {
			filters = append(filters, s)
		}
	}
	ui.Column(c).FillWidth().Padding(6, 0, 0).Gap(1).Children(func() {
		for _, f := range filters {
			f := f
			label := tr("f_" + f)
			if f != "all" && f != "failed" {
				label = sourceLabel(f)
			}
			n := counts[f]
			item := pal.navItem(c, "af-"+f, "", label, w.actFilter == f, false, func() {
				pal.mono(c, itoa(n), 11, pal.T3)
			})
			if item.Clicked() {
				w.actFilter, w.runSel = f, 0
			}
		}
	})
}

func (w *workbench) activity(c *ui.Context) {
	pal := w.pal
	runs := w.filteredRuns()
	if w.runSel >= len(runs) {
		w.runSel = max(0, len(runs)-1)
	}
	w.topBar(c, func() {
		ui.Row(c).Grow(1).Gap(7).AlignItems(ui.Center).Children(func() {
			icon(c, "activity", 15).TextColor(pal.T1)
			ui.Text(c, tr("act_title")).FontSize(13).FontWeight(500).TextColor(pal.T1)
			pal.mono(c, fmt.Sprintf(tr("act_n"), len(runs)), 12, pal.T3)
			if w.notice != "" {
				col := pal.T2
				if w.noticeErr {
					col = pal.Red
				}
				ui.Text(c, w.notice).FontSize(11.5).TextColor(col).SingleLine().MinWidth(0)
			}
		})
		if pal.button(c, "export", tr("act_export"), "download", btnGhost, len(runs) == 0).Clicked() && len(runs) > 0 && w.exportRuns != nil {
			w.exportRuns(append([]store.Run(nil), runs...))
		}
		if pal.button(c, "clear", tr("act_clear"), "", btnGhost, len(w.runs) == 0).Clicked() && len(w.runs) > 0 {
			w.clearOpen = true
		}
	})
	if w.clearOpen {
		if ui.AlertDialog(c.Key("clear-dialog"), &w.clearOpen, tr("act_clear_title"), tr("act_clear_msg"), tr("cancel"), tr("act_clear")) == 1 {
			_ = w.b.ClearRuns()
			w.runsDirty = true
		}
	}
	if len(runs) == 0 {
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Text(c, tr("act_empty")).FontSize(13).TextColor(pal.T3)
		})
		return
	}
	names := map[string]string{}
	icons := map[string]string{}
	for _, v := range w.views {
		names[v.ID], icons[v.ID] = v.Name, v.Icon
	}
	cols := []ui.TableColumn{
		{Title: tr("col_time"), Width: 128},
		{Title: tr("col_switch")},
		{Title: tr("col_action"), Width: 64},
		{Title: tr("col_source"), Width: 190},
		{Title: tr("col_duration"), Width: 80, Align: ui.End},
		{Title: tr("col_result"), Width: 56, Align: ui.Center},
	}
	// 选中行用浅灰底（设计稿），而不是强调色：只对表格换一套主题
	normal := c.Theme()
	tableTheme := *normal
	tableTheme.Accent, tableTheme.AccentText = pal.L3, pal.T1
	c.SetTheme(&tableTheme)
	w.runList.Selected = &w.runSel
	w.runList.Key = func(row int) any { return runs[row].ID }
	ui.Table(c.Key("runs"), &w.runList, cols, len(runs), func(row, col int) {
		r := runs[row]
		switch col {
		case 0:
			pal.mono(c, r.Time.Local().Format("01-02 15:04:05"), 11.5, pal.T3)
		case 1:
			ui.Row(c).Gap(7).AlignItems(ui.Center).MinWidth(0).Children(func() {
				name := r.Name
				if n, ok := names[r.Switch]; ok {
					name = n
				}
				if ic, ok := icons[r.Switch]; ok {
					switchIcon(c, ic, 13).TextColor(pal.T2)
				}
				ui.Text(c, name).FontSize(12.5).TextColor(pal.T1).SingleLine().MinWidth(0)
			})
		case 2:
			ui.Text(c, actionLabel(r.Action)).FontSize(12.5).TextColor(pal.T1)
		case 3:
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				pal.badge(c, sourceLabel(r.Source), sourceTone(r.Source))
				ui.Text(c, r.Detail).FontSize(11.5).TextColor(pal.T3).SingleLine().MinWidth(0)
			})
		case 4:
			pal.mono(c, durText(time.Duration(r.Duration)*time.Millisecond), 11.5, pal.T3)
		case 5:
			if r.OK {
				icon(c, "check", 14).TextColor(pal.Green)
			} else {
				icon(c, "x", 14).TextColor(pal.Red)
			}
		}
	}).Grow(1).FillWidth().MinHeight(0)
	c.SetTheme(normal)

	cur := runs[w.runSel]
	pal.hair(c)
	ui.Row(c).FillWidth().Height(34).Shrink(0).Padding(0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
		icon(c, "terminal", 12).TextColor(pal.T1)
		ui.Text(c, tr("tab_output")).FontSize(12).TextColor(pal.T1)
		code := "exit " + itoa(cur.ExitCode)
		pal.mono(c, cur.Time.Local().Format("15:04:05")+" · "+durText(time.Duration(cur.Duration)*time.Millisecond)+" · "+code, 11, pal.T3)
	})
	ui.Scroll(c.Key("run-out")).FillWidth().Height(170).Shrink(0).Padding(4, 16, 12).Children(func() {
		lines := outputLines(cur.Output)
		if len(lines) == 0 {
			ui.Text(c, tr("no_output")).FontSize(12).TextColor(pal.T3)
		}
		for i, l := range lines {
			w.outRow(c, "ro-"+itoa(i), l)
		}
	})
}

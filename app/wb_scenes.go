package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 场景：侧栏列表与编辑页。修改立即保存（名称为空时暂不保存）。

func (w *workbench) sceneByID(id string) *engine.SceneView {
	for i := range w.scenes {
		if w.scenes[i].ID == id {
			return &w.scenes[i]
		}
	}
	return nil
}

// ownerName 把 "switch:<id>" / "scene:<id>" / "rule:<id>" 写成给人看的名称。
func (w *workbench) ownerName(owner string) string {
	kind, id, _ := strings.Cut(owner, ":")
	switch kind {
	case "switch":
		if v := w.view(id); v != nil {
			return v.Name
		}
	case "scene":
		if sv := w.sceneByID(id); sv != nil {
			return sv.Name
		}
	case "rule":
		for _, r := range w.rules {
			if r.ID == id {
				return triggerText(r.Trigger)
			}
		}
	}
	return id
}

func (w *workbench) sceneList(c *ui.Context) {
	pal := w.pal
	if len(w.scenes) == 0 {
		ui.Text(c, tr("scenes_empty")).FontSize(12).TextColor(pal.T3).Margin(12, 8)
		return
	}
	ui.Column(c).FillWidth().Padding(6, 0, 0).Gap(1).Children(func() {
		for _, sv := range w.scenes {
			sv := sv
			sel := w.selScene == sv.ID
			r := ui.Row(c.Key("sc-"+sv.ID)).FillWidth().Height(30).Padding(0, 8).Radius(7).Gap(8).AlignItems(ui.Center).Label(sv.Name)
			if sel {
				r.Background(pal.L2)
			} else if r.Hovered() {
				r.Background(pal.L1)
			}
			r.Children(func() {
				ui.Row(c).Width(16).Justify(ui.Center).Children(func() { switchIcon(c, sceneIcon(sv.Scene), 14).TextColor(pal.T2) })
				ui.Text(c, sv.Name).FontSize(12.5).TextColor(pal.T1).SingleLine().Grow(1).MinWidth(0)
				if sv.Active {
					col := pal.Accent
					if pal.Ink {
						col = pal.FG
					}
					ui.Box(c).Size(6, 6).Radius(3).Background(col)
				}
				if sv.Shortcut != "" {
					pal.mono(c, shortcutLabel(sv.Shortcut), 10.5, pal.T3)
				}
			})
			if r.Clicked() {
				w.selScene = sv.ID
			}
		}
	})
}

func sceneIcon(sc store.Scene) string {
	if sc.Icon != "" {
		return sc.Icon
	}
	return "lucide:layers"
}

// newScene 新建一个空场景并打开它。
func (w *workbench) newScene() {
	sc, err := w.b.SaveScene(store.Scene{Name: tr("scene_new_name"), Icon: "lucide:layers", Restore: true})
	if err != nil {
		w.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
		return
	}
	w.section, w.selScene = secScenes, sc.ID
	w.dirty = true
}

// saveScene 保存场景；名称为空时只保留草稿。
func (w *workbench) saveScene(sc store.Scene) {
	w.sceneDraft[sc.ID] = sc
	if strings.TrimSpace(sc.Name) == "" {
		return
	}
	if _, err := w.b.SaveScene(sc); err != nil {
		w.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
		return
	}
	delete(w.sceneDraft, sc.ID)
	w.dirty = true
	if w.auto != nil {
		w.auto.Reload()
	}
}

func (w *workbench) sceneEditor(c *ui.Context) {
	pal := w.pal
	sv := w.sceneByID(w.selScene)
	if sv == nil && len(w.scenes) > 0 {
		w.selScene = w.scenes[0].ID
		sv = &w.scenes[0]
	}
	if sv == nil {
		w.topBar(c, func() {})
		w.emptyState(c, "layers", tr("scenes_empty"), tr("scenes_empty_h"), tr("new_scene_bt"), w.newScene)
		return
	}
	sc := sv.Scene
	if d, ok := w.sceneDraft[sc.ID]; ok {
		sc = d
	}
	w.topBar(c, func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
			switchIcon(c, sceneIcon(sc), 15).TextColor(pal.T1)
			ui.Text(c, sc.Name).FontSize(13).FontWeight(500).TextColor(pal.T1).SingleLine().MinWidth(0)
			pal.mono(c, fmt.Sprintf(tr("scene_steps"), len(sc.Steps)), 12, pal.T3)
			if sv.Active {
				pal.badge(c, tr("scene_active"), "accent")
			}
			w.noticeText(c)
		})
		label, iconName := tr("scene_activate"), "play"
		if sv.Active && sc.Restore {
			label, iconName = tr("scene_restore_bt"), "undo-2"
		}
		if pal.button(c, "sc-run", label, iconName, btnPrimary, len(sc.Steps) == 0).Clicked() && len(sc.Steps) > 0 {
			if _, err := w.b.ToggleScene(sc.ID, engine.Manual, ""); err != nil {
				w.setNotice(err.Error(), true)
			}
		}
		more := pal.iconBtn(c, "sc-more", "ellipsis", tr("more"), w.moreOpen)
		if more.Clicked() {
			w.moreOpen = !w.moreOpen
		}
		ui.PopoverBase(c, more, &w.moreOpen, func(panel ui.Element) {
			w.menuPanel(panel)
			panel.Children(func() {
				if w.menuItem(c, "sc-del", "trash-2", tr("scene_delete")) {
					w.moreOpen, w.deleteOpen = false, true
				}
			})
		})
	})
	if w.deleteOpen {
		if ui.AlertDialog(c.Key("sc-del-dialog"), &w.deleteOpen, fmt.Sprintf(tr("scene_del_title"), sc.Name), tr("scene_del_msg"), tr("cancel"), tr("delete")) == 1 {
			_ = w.b.DeleteScene(sc.ID)
			w.selScene = ""
			w.dirty = true
			if w.auto != nil {
				w.auto.Reload()
			}
		}
	}
	w.page(c, "scene-"+sc.ID, func() {
		// 名称与图标
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			w.iconButton(c, "sc-icon", sceneIcon(sc), &w.sceneIcon, func(spec string) {
				sc.Icon = spec
				w.saveScene(sc)
			})
			w.field(c, sc.ID+"/name", sc.Name, tr("scene_name_ph"), false, func(s string) {
				sc.Name = s
				w.saveScene(sc)
			}).FontSize(18).FontWeight(600)
		})
		// 步骤
		w.card(c, "sc-steps", func() {
			ui.Row(c).FillWidth().Padding(12, 14).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("scene_on")).FontSize(12.5).FontWeight(600).TextColor(pal.T1).Grow(1)
				ui.Text(c, tr("scene_in_order")).FontSize(12).TextColor(pal.T3)
			})
			if len(sc.Steps) == 0 {
				pal.hair(c)
				ui.Text(c, tr("scene_no_steps")).FontSize(12.5).TextColor(pal.T3).Margin(10, 14)
			}
			for i, st := range sc.Steps {
				i, st := i, st
				pal.hair(c)
				w.stepRow(c, &sc, i, st)
			}
			pal.hair(c)
			ui.Row(c).FillWidth().Padding(8, 14).Gap(8).AlignItems(ui.Center).Children(func() {
				options, ids := []string{tr("pick_switch")}, []string{""}
				for _, v := range w.views {
					options = append(options, v.Name)
					ids = append(ids, v.ID)
				}
				sel := tr("pick_switch")
				ui.Select(c.Key("add-step"), &sel, options).Width(220).Label(tr("add_step"))
				if sel != tr("pick_switch") {
					for i, o := range options {
						if o == sel && ids[i] != "" {
							action := "on"
							if v := w.view(ids[i]); v != nil && v.Type == contract.Action {
								action = "run"
							}
							sc.Steps = append(sc.Steps, store.SceneStep{Switch: ids[i], Action: action})
							w.saveScene(sc)
							break
						}
					}
				}
				pal.hint(c, tr("add_step"))
			})
		})
		// 快捷键与选项
		w.card(c, "sc-opts", func() {
			w.cardRow(c, tr("shortcut"), tr("sc_hint_scene"), func() {
				ui.Column(c).Width(260).Shrink(0).Children(func() {
					if v, changed := w.shortcutField(c, "sc-shortcut-"+sc.ID, sc.Shortcut, w.issueFor("scene:"+sc.ID)); changed {
						sc.Shortcut = v
						w.saveScene(sc)
					}
				})
			})
			pal.hair(c)
			w.cardRow(c, tr("scene_restore"), tr("scene_restore_h"), func() {
				on := sc.Restore
				w.pal.toggle(c, "sc-restore", &on, false, false).Label(tr("scene_restore"))
				if on != sc.Restore {
					sc.Restore = on
					w.saveScene(sc)
				}
			})
			pal.hair(c)
			w.cardRow(c, tr("scene_show"), fmt.Sprintf(tr("scene_show_h"), sc.Name), func() {
				on := sc.ShowName
				w.pal.toggle(c, "sc-show", &on, false, false).Label(tr("scene_show"))
				if on != sc.ShowName {
					sc.ShowName = on
					w.saveScene(sc)
				}
			})
		})
	})
}

func (w *workbench) stepRow(c *ui.Context, sc *store.Scene, i int, st store.SceneStep) {
	pal := w.pal
	v := w.view(st.Switch)
	ui.Row(c.Key("step-"+itoa(i))).FillWidth().Padding(8, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		pal.mono(c, itoa(i+1), 11, pal.T3).Width(14)
		name, ic := st.Switch+tr("rule_missing"), "circle-help"
		if v != nil {
			name, ic = v.Name, v.Icon
		}
		switchIcon(c, ic, 15).TextColor(pal.T1)
		ui.Text(c, name).FontSize(13).TextColor(pal.T1).SingleLine().Grow(1).MinWidth(0)
		if v != nil && v.Type == contract.Action || st.Action == "run" {
			pal.badge(c, tr("step_run"), "")
		} else {
			cur := 0
			if st.Action == "off" {
				cur = 1
			}
			ui.Row(c).Width(130).Children(func() {
				if n := pal.segmented(c, "seg", cur, tr("step_on"), tr("step_off")); n != cur {
					sc.Steps[i].Action = []string{"on", "off"}[n]
					w.saveScene(*sc)
				}
			})
		}
		if i > 0 && pal.iconBtn(c, "up", "arrow-up", tr("move_up"), false).Clicked() {
			sc.Steps[i-1], sc.Steps[i] = sc.Steps[i], sc.Steps[i-1]
			w.saveScene(*sc)
		}
		if i < len(sc.Steps)-1 && pal.iconBtn(c, "down", "arrow-down", tr("move_down"), false).Clicked() {
			sc.Steps[i+1], sc.Steps[i] = sc.Steps[i], sc.Steps[i+1]
			w.saveScene(*sc)
		}
		if pal.iconBtn(c, "rm", "x", tr("remove"), false).Clicked() {
			sc.Steps = append(sc.Steps[:i:i], sc.Steps[i+1:]...)
			w.saveScene(*sc)
		}
	})
}

// ───────── 页面通用件 ─────────

// page 是居中、最宽 760 的滚动页面。
func (w *workbench) page(c *ui.Context, key string, fn func()) {
	ui.Scroll(c.Key(key)).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(28, 32, 60).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(760).Gap(22).Children(fn)
		})
	})
}

// card 是带细边框的卡片。
func (w *workbench) card(c *ui.Context, key string, fn func()) {
	pal := w.pal
	bg := pal.Elev
	if pal.Dark {
		bg = pal.Card
	}
	ui.Column(c.Key(key)).FillWidth().Radius(12).Border(1, pal.L2).Background(bg).Children(fn)
}

// cardRow 是卡片里的一行：左边标题与说明，右边控件。
func (w *workbench) cardRow(c *ui.Context, title, hint string, ctl func()) {
	pal := w.pal
	ui.Row(c).FillWidth().Padding(10, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).TextColor(pal.T1)
			if hint != "" {
				ui.Text(c, hint).FontSize(11).TextColor(pal.T3).Wrap()
			}
		})
		ctl()
	})
}

// emptyState 是空列表的提示与新建按钮。
func (w *workbench) emptyState(c *ui.Context, iconName, title, hint, action string, do func()) {
	pal := w.pal
	ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
		icon(c, iconName, 28).TextColor(pal.T3)
		ui.Text(c, title).FontSize(14).FontWeight(500).TextColor(pal.T1)
		ui.Text(c, hint).FontSize(12.5).TextColor(pal.T3)
		if pal.button(c, "empty-new", action, "plus", btnPlain, false).Clicked() {
			do()
		}
	})
}

func (w *workbench) noticeText(c *ui.Context) {
	if w.notice == "" {
		return
	}
	col := w.pal.T2
	if w.noticeErr {
		col = w.pal.Red
	}
	ui.Text(c, w.notice).FontSize(11.5).TextColor(col).SingleLine().MinWidth(0)
}

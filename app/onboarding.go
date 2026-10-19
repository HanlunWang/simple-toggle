package app

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/png"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/integrations"
)

// 首次启动引导：五步，每一步都可以跳过（designs/simpletoggle-v2/onboarding.jsx）。

//go:embed icons/appicon.png
var appIconPNG []byte

var appIconBitmap *ui.Bitmap

func appIcon() *ui.Bitmap {
	if appIconBitmap == nil {
		if img, err := png.Decode(bytes.NewReader(appIconPNG)); err == nil {
			appIconBitmap = ui.NewBitmap(img)
		}
	}
	return appIconBitmap
}

type onboardingHost struct {
	installed     func(file string) bool
	install       func(file, content string) error
	prefs         func() Prefs
	setPref       func(name, value string)
	clients       func() []integrations.Status
	plan          func(client string) (integrations.Plan, error)
	apply         func(p integrations.Plan) error
	panelShortcut func() string
	finish        func(openPanel bool) // 写下「已引导」并关闭窗口
}

const onboardingSteps = 5

type onboarding struct {
	h      *onboardingHost
	pal    palette
	step   int
	picked map[string]bool
	cat    string
	note   string

	demoOn    bool
	demoRunAt time.Time

	plan     *integrations.Plan
	planOpen bool
}

func newOnboarding(h *onboardingHost) *onboarding {
	return &onboarding{h: h, picked: map[string]bool{}, cat: "all", demoOn: true}
}

func (o *onboarding) render(c *ui.Context) {
	prefs := o.h.prefs()
	base := c.Theme()
	o.pal = newPalette(base.Dark, prefs.Accent)
	c.SetTheme(o.pal.theme(base))
	pal := o.pal
	c.Root().Background(pal.BG)
	ui.Column(c).Fill().Children(func() {
		bar := c.TitleBar()
		ui.Row(c).FillWidth().Height(max(bar.Height, 46)).Shrink(0).Padding(0, 14).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Spacer(c)
			if o.step < onboardingSteps-1 && pal.button(c, "ob-skip", tr("ob_skip"), "", btnGhost, false).Clicked() {
				o.h.finish(false)
			}
		})
		ui.Scroll(c.Key("ob-body-" + itoa(o.step))).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(8, 40, 24).Children(func() {
				switch o.step {
				case 0:
					o.intro(c)
				case 1:
					o.picks(c, prefs)
				case 2:
					o.looks(c, prefs)
				case 3:
					o.ai(c)
				default:
					o.done(c)
				}
			})
		})
		pal.hair(c)
		o.footer(c)
	})
	if o.planOpen && o.plan != nil {
		planDialog(c, pal, &o.planOpen, *o.plan, func(p integrations.Plan) {
			if err := o.h.apply(p); err != nil {
				o.note = err.Error()
			}
		})
	}
	if !o.demoRunAt.IsZero() && time.Since(o.demoRunAt) < 3*time.Second {
		c.After(100 * time.Millisecond)
	}
}

func (o *onboarding) h1(c *ui.Context, text, aside string) {
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		ui.Text(c, text).FontSize(22).FontWeight(600).TextColor(o.pal.T1)
		if aside != "" {
			ui.Text(c, aside).FontSize(15).TextColor(o.pal.T3)
		}
	})
}

func (o *onboarding) p(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(13.5).TextColor(o.pal.T2).Wrap().MaxWidth(540).Margin(6, 0, 0)
}

// ───────── 第 1 步：三种类型 ─────────

func (o *onboarding) intro(c *ui.Context) {
	pal := o.pal
	if b := appIcon(); b != nil {
		pal.brand(c, 20).Margin(0, 0, 18) // Logo：点阵 › + simpletoggle
		_ = b
	}
	o.h1(c, tr("ob_h1"), "")
	o.p(c, tr("ob_p1"))
	ui.Row(c).FillWidth().Gap(12).Margin(26, 0, 0).Children(func() {
		card := func(key, nameKey, descKey string, demo func()) {
			ui.Column(c.Key(key)).Grow(1).Basis(0).MinHeight(160).Padding(16).Radius(6).Border(1, pal.L2).Gap(12).Children(func() {
				ui.Row(c).FillWidth().Height(52).Radius(5).Background(pal.L1).Padding(0, 12).Gap(10).AlignItems(ui.Center).Children(demo)
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, tr(nameKey)).FontSize(13.5).FontWeight(600).TextColor(pal.T1)
					ui.Text(c, tr(descKey)).FontSize(12).TextColor(pal.T3).Wrap()
				})
			})
		}
		card("t-toggle", "type_toggle", "ob_t_toggle", func() {
			icon(c, "moon", 16).TextColor(pal.T1)
			ui.Text(c, "Dark Mode").FontSize(13).TextColor(pal.T1).SingleLine().Grow(1).MinWidth(0)
			pal.toggle(c, "demo-sw", &o.demoOn, false, false).Label("Dark Mode")
		})
		card("t-daemon", "type_daemon", "ob_t_daemon", func() {
			col := pal.Accent
			if pal.Ink {
				col = pal.FG
			}
			ui.Box(c).Size(6, 6).Radius(3).Background(col)
			icon(c, "coffee", 16).TextColor(pal.T1)
			ui.Text(c, "Keep Awake").FontSize(13).TextColor(pal.T1).SingleLine().Grow(1).MinWidth(0)
			pal.mono(c, "1:42:10", 11, pal.T3)
		})
		card("t-action", "type_action", "ob_t_action", func() {
			icon(c, "rotate-cw", 16).TextColor(pal.T1)
			ui.Text(c, "Flush DNS").FontSize(13).TextColor(pal.T1).SingleLine().Grow(1).MinWidth(0)
			el := time.Since(o.demoRunAt)
			btn := ui.Row(c.Key("demo-run")).Height(24).Padding(0, 10).Radius(999).Gap(6).AlignItems(ui.Center).Border(1, pal.L3).Label(tr("run"))
			btn.Children(func() {
				switch {
				case !o.demoRunAt.IsZero() && el < 1300*time.Millisecond:
					ui.Spinner(c).Size(12, 12)
					ui.Textf(c, "%.1fs", el.Seconds()).Font("monospace").FontSize(11.5).TextColor(pal.T1)
				case !o.demoRunAt.IsZero() && el < 2800*time.Millisecond:
					icon(c, "check", 13).TextColor(pal.Green)
					ui.Text(c, tr("done")).FontSize(12).TextColor(pal.Green)
				default:
					icon(c, "play", 11).TextColor(pal.T1)
					ui.Text(c, tr("run")).FontSize(12).TextColor(pal.T1)
				}
			})
			if btn.Clicked() {
				o.demoRunAt = time.Now()
			}
		})
	})
}

// ───────── 第 2 步：挑选脚本 ─────────

func (o *onboarding) newPicks() []libEntry {
	var out []libEntry
	for _, e := range library() {
		if o.picked[e.File] && !o.h.installed(e.File) {
			out = append(out, e)
		}
	}
	return out
}

func (o *onboarding) picks(c *ui.Context, prefs Prefs) {
	pal := o.pal
	o.h1(c, tr("ob_h2"), "")
	o.p(c, tr("ob_p2"))
	ui.Row(c).FillWidth().Gap(6).Margin(16, 0, 0).AlignItems(ui.Center).Children(func() {
		for _, cat := range libCats {
			cat := cat
			ch := ui.Row(c.Key("cat-"+cat)).Height(24).Padding(0, 10).Radius(999).AlignItems(ui.Center).Label(tr("cat_" + cat))
			col := pal.T2
			if o.cat == cat {
				ch.Background(pal.L3)
				col = pal.T1
			} else {
				ch.Border(1, pal.L3)
			}
			ch.Children(func() { ui.Text(c, tr("cat_"+cat)).FontSize(12).TextColor(col) })
			if ch.Clicked() {
				o.cat = cat
			}
		}
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf(tr("ob_picked"), len(o.newPicks()))).FontSize(12).TextColor(pal.T3)
	})
	ui.Grid(c.Key("picks")).FillWidth().Columns(3).Gap(8).Margin(14, 0, 0).Children(func() {
		for _, e := range library() {
			e := e
			if o.cat != "all" && e.Category != o.cat {
				continue
			}
			have := o.h.installed(e.File)
			sel := have || o.picked[e.File]
			cell := ui.Row(c.Key("pk-"+e.File)).Padding(10, 12).Radius(6).Gap(10).AlignItems(ui.Center).Label(e.header.Name)
			switch {
			case sel && pal.Ink:
				cell.Border(1.5, pal.FG).Background(pal.L1)
			case sel:
				cell.Border(1.5, pal.Accent).Background(pal.L1)
			default:
				cell.Border(1, pal.L2)
				if cell.Hovered() {
					cell.Background(pal.L1)
				}
			}
			if have {
				cell.Opacity(0.6)
			}
			cell.Children(func() {
				switchIcon(c, e.header.Icon, 16).TextColor(pal.T1)
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, e.header.Name).FontSize(12.5).TextColor(pal.T1).SingleLine()
					ui.Text(c, tr("type_"+string(e.header.Type))+" · "+e.desc()).FontSize(11).TextColor(pal.T3).SingleLine()
				})
				if sel {
					icon(c, "check", 14).TextColor(pal.T1)
				}
			})
			if cell.Clicked() && !have {
				o.picked[e.File] = !o.picked[e.File]
			}
		}
	})
	_ = prefs
}

// ───────── 第 3 步：外观 ─────────

func (o *onboarding) looks(c *ui.Context, prefs Prefs) {
	pal := o.pal
	o.h1(c, tr("ob_h3"), "")
	o.p(c, tr("ob_p3"))
	pal.caption(c, tr("g_theme")).Margin(22, 0, 8)
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		for _, mode := range []string{"light", "dark", "system"} {
			mode := mode
			card := ui.Column(c.Key("ob-theme-" + mode)).Grow(1).Basis(0).Gap(7).AlignItems(ui.Center).Label(tr("theme_" + mode))
			card.Children(func() {
				prev := ui.Row(c).FillWidth().Height(96).Radius(5).Clip()
				if prefs.Theme == mode {
					if pal.Ink {
						prev.Border(2, pal.FG)
					} else {
						prev.Border(2, pal.Accent)
					}
				} else {
					prev.Border(1, pal.L3)
				}
				prev.Children(func() { themePreview(c, mode) })
				ui.Text(c, tr("theme_"+mode)).FontSize(12).TextColor(pal.T1)
			})
			if card.Clicked() {
				o.h.setPref("theme", mode)
			}
		}
	})
	ui.Column(c).FillWidth().Gap(8).Margin(18, 0, 0).Children(func() {
		pal.caption(c, tr("accent"))
		pal.paletteGrid(c, prefs.Accent, func(a Accent) { o.h.setPref("palette", string(a)) })
	})
	ui.Row(c).FillWidth().Gap(28).Margin(18, 0, 0).Children(func() {
		ui.Column(c).Gap(8).Children(func() {
			pal.caption(c, tr("layout"))
			cur := slices.Index([]string{"list", "grid"}, prefs.Layout)
			ui.Row(c).Width(170).Children(func() {
				if n := pal.segmented(c, "ob-layout", max(cur, 0), tr("layout_list"), tr("layout_grid")); n != cur {
					o.h.setPref("layout", []string{"list", "grid"}[n])
				}
			})
		})
	})
}

// ───────── 第 4 步：AI ─────────

func (o *onboarding) ai(c *ui.Context) {
	pal := o.pal
	o.h1(c, tr("ob_h4"), tr("ob_optional"))
	o.p(c, tr("ob_p4"))
	ui.Column(c.Key("ob-clients")).FillWidth().Margin(22, 0, 0).Radius(6).Border(1, pal.L2).Children(func() {
		for i, st := range o.h.clients() {
			st := st
			if i > 0 {
				pal.hair(c)
			}
			ui.Row(c.Key("obc-"+st.Client)).FillWidth().Padding(12, 14).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Size(28, 28).Radius(4).Background(pal.L2).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					icon(c, "bot", 15).TextColor(pal.T1)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, tr("cl_"+st.Client)).FontSize(13).TextColor(pal.T1)
					hint := map[string]string{integrations.ClaudeCode: "cl_claude_h", integrations.Cursor: "cl_cursor_h", integrations.Codex: "cl_codex_h"}[st.Client]
					ui.Text(c, tr(hint)).FontSize(11.5).TextColor(pal.T3)
				})
				switch {
				case st.Current:
					pal.badge(c, tr("cl_connected"), "green")
				case !st.Present && !st.Connected:
					pal.badge(c, tr("cl_missing"), "")
				default:
					label := tr("cl_add")
					if st.Connected {
						label = tr("cl_update")
					}
					if pal.button(c, "obc-add-"+st.Client, label, "", btnPlain, false).Clicked() {
						p, err := o.h.plan(st.Client)
						if err != nil {
							o.note = tr("cl_no_cli")
							if err != integrations.ErrNoCLI {
								o.note = err.Error()
							}
							return
						}
						o.plan, o.planOpen = &p, true
					}
				}
			})
		}
	})
	if o.note != "" {
		ui.Text(c, o.note).FontSize(12).TextColor(pal.Amber).Margin(10, 0, 0).Wrap()
	}
	ui.Row(c).Gap(6).Margin(12, 0, 0).AlignItems(ui.Center).Children(func() {
		icon(c, "terminal", 13).TextColor(pal.T3)
		ui.Text(c, tr("ob_cli_hint")).FontSize(12).TextColor(pal.T3)
	})
}

// ───────── 第 5 步：完成 ─────────

func (o *onboarding) done(c *ui.Context) {
	pal := o.pal
	ui.Column(c).FillWidth().MinHeight(300).AlignItems(ui.Center).Justify(ui.Center).Gap(14).Children(func() {
		ui.Row(c).Height(30).Padding(0, 14).Radius(5).Gap(16).AlignItems(ui.Center).Background(ui.Hex("#2a2a2a")).Children(func() {
			ui.Text(c, "Wi-Fi").FontSize(12.5).TextColor(ui.Hex("#ffffff"))
			if b := trayPreview(o.h.prefs().MenubarStyle); b != nil {
				bw, bh := b.Size()
				ui.Row(c).Padding(2, 6).Radius(6).Background(alpha(ui.Hex("#ffffff"), 0.25)).Children(func() {
					ui.Image(c, b).Size(float32(bw)/2, float32(bh)/2)
				})
			}
			ui.Text(c, time.Now().Format("15:04")).FontSize(12.5).TextColor(ui.Hex("#ffffff"))
		})
		o.h1(c, tr("ob_h5"), "")
		text := tr("ob_p5")
		if sc := o.h.panelShortcut(); sc != "" {
			text = fmt.Sprintf(tr("ob_p5_sc"), automation.ShortcutSymbols(sc))
		}
		ui.Text(c, text).FontSize(13.5).TextColor(pal.T2)
	})
}

// ───────── 底栏 ─────────

func (o *onboarding) footer(c *ui.Context) {
	pal := o.pal
	ui.Row(c.Key("ob-foot")).FillWidth().Padding(14, 20).Gap(10).AlignItems(ui.Center).Children(func() {
		// 步骤：等宽的 01 02 03…，当前一步是信号色并带 ▸
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			for i := 0; i < onboardingSteps; i++ {
				col := pal.T4
				switch {
				case i == o.step:
					col = pal.Accent
				case i < o.step:
					col = pal.T2
				}
				label := pick(i == o.step, "▸", "") + fmt.Sprintf("%02d", i+1)
				ui.Text(c.Key("dot-"+itoa(i)), label).Font(monoFont).FontSize(11).TextColor(col).SingleLine()
			}
		})
		ui.Spacer(c)
		if o.step > 0 && pal.button(c, "ob-back", tr("ob_back"), "", btnPlain, false).Clicked() {
			o.step--
		}
		if o.step == 3 && pal.button(c, "ob-later", tr("ob_later"), "", btnGhost, false).Clicked() {
			o.step++
		}
		label := tr("ob_continue")
		switch o.step {
		case 0:
			label = tr("ob_start")
		case 1:
			if n := len(o.newPicks()); n > 0 {
				label = fmt.Sprintf(tr("ob_add_n"), n)
			}
		case onboardingSteps - 1:
			label = tr("ob_open_panel")
		}
		if pal.button(c, "ob-next", label, "", btnPrimary, false).Clicked() {
			switch o.step {
			case 1:
				for _, e := range o.newPicks() {
					_ = o.h.install(e.File, e.Content)
				}
				o.picked = map[string]bool{}
			case onboardingSteps - 1:
				o.h.finish(true)
				return
			}
			o.step++
		}
	})
}

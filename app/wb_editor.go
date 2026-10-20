package app

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// ───────── 文档 ─────────

// doc 是一个打开的脚本：编辑器状态、磁盘上的版本、解析出的契约头与检查结果。
type doc struct {
	id       string
	st       *editor.State
	disk     string // 最后一次从磁盘读入或保存的内容
	ver      uint64 // 已解析的缓冲区版本
	header   contract.Header
	issues   []contract.Issue
	conflict bool // 有未保存的修改时，磁盘上的文件又被改了
	band     [2]int
	hasBand  bool
}

func newDoc(id, text string) *doc {
	buf := editor.NewBuffer(text)
	buf.SetLanguage(editor.LanguageFor("script.sh"))
	d := &doc{id: id, disk: text, ver: math.MaxUint64}
	d.st = &editor.State{Buffer: buf, SoftWrap: true, Label: id}
	return d
}

func (d *doc) text() string    { return d.st.Buffer.String() }
func (d *doc) modified() bool  { return d.text() != d.disk }
func (d *doc) hasErrors() bool { return contract.HasErrors(d.issues) }

// sync 在缓冲区变化后重新解析契约头、检查脚本，并更新诊断与契约头的底色。
func (d *doc) sync(pal palette) {
	_ = pal
	buf := d.st.Buffer
	v := buf.Version()
	if v == d.ver {
		return
	}
	d.ver = v
	text := buf.String()
	d.header, _ = contract.Parse(text)
	d.issues = contract.Lint(text)
	var diags []editor.Diagnostic
	for _, is := range d.issues {
		if is.Line <= 0 || is.Line > buf.LineCount() {
			continue
		}
		start := buf.Offset(editor.Pos{Line: is.Line - 1})
		end := start + len(buf.Line(is.Line-1))
		sev := editor.SeverityWarning
		if is.Severity == "error" {
			sev = editor.SeverityError
		}
		diags = append(diags, editor.Diagnostic{Start: start, End: end, Severity: sev, Message: issueText(is), Source: "SimpleToggle"})
	}
	d.st.SetDiagnostics(v, diags)
	d.band[0], d.band[1], d.hasBand = 0, 0, false
	if first, last, ok := contract.HeaderRange(text); ok {
		d.band[0], d.band[1], d.hasBand = first, last, true
	}
}

// paintBand 给契约头铺底；每帧按当前主题取色（切换深浅色后立即跟上）。
func (d *doc) paintBand(pal palette) {
	d.st.LineBackgrounds = nil
	if !d.hasBand {
		return
	}
	// 契约头：一层很淡的信号色
	d.st.LineBackgrounds = []editor.LineBackground{{First: d.band[0], Last: d.band[1], Color: alpha(pal.Accent, 0.07)}}
}

// apply 把检查器的修改写回缓冲区：只替换变化的那一段，光标与撤销历史都保留。
func (d *doc) apply(newText string) {
	old := d.text()
	if old == newText {
		return
	}
	p := 0
	for p < len(old) && p < len(newText) && old[p] == newText[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(newText)-p && old[len(old)-1-s] == newText[len(newText)-1-s] {
		s++
	}
	d.st.Buffer.Edit(editor.Edit{Start: p, End: len(old) - s, Text: newText[p : len(newText)-s]})
}

// issueText 返回检查结果的界面文案（中文按 code 翻译，其余用英文原文）。
func issueText(is contract.Issue) string {
	if f, ok := strs["lint."+is.Code]; ok {
		return fmt.Sprintf(f, is.Args...)
	}
	return is.Message
}

func (w *workbench) currentDoc() *doc {
	if w.selected == "" || w.view(w.selected) == nil {
		return nil
	}
	return w.docFor(w.selected)
}

func (w *workbench) docFor(id string) *doc {
	if d, ok := w.docs[id]; ok {
		return d
	}
	text, err := w.b.Script(id)
	if err != nil {
		return nil
	}
	d := newDoc(id, text)
	w.docs[id] = d
	return d
}

// checkDisk 在脚本目录变化后比对已打开的文档：没有改动的直接载入新版本，
// 有改动的标记冲突，由用户选择。
func (w *workbench) checkDisk() {
	for id, d := range w.docs {
		text, err := w.b.Script(id)
		if err != nil || text == d.disk {
			continue
		}
		if !d.modified() {
			d.apply(text)
			d.disk = text
			continue
		}
		if text != d.text() {
			d.conflict = true
		} else {
			d.disk = text
		}
	}
}

// save 保存当前文档；契约有错误时拒绝并打开「问题」页。
func (w *workbench) save(d *doc) bool {
	text := d.text()
	if _, err := w.b.UpdateScript(d.id, text, engine.Manual); err != nil {
		var le *engine.LintError
		if errors.As(err, &le) {
			w.bottomOpen, w.bottomTab = true, 1
			w.setNotice(tr("save_blocked"), true)
		} else {
			w.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
		}
		return false
	}
	d.disk, d.conflict = text, false
	w.savedAt[d.id] = time.Now()
	w.dirty = true
	return true
}

// ───────── 新建与删除 ─────────

func template(t contract.Type, name string) string {
	head := "#!/bin/bash\n# <switch.name> " + name + "\n"
	switch t {
	case contract.Action:
		return head + "# <switch.icon> lucide:play\n# <switch.type> action\n#\n" +
			"# Runs once per press, without arguments. Exit 0 means success;\n" +
			"# the last line of output is shown in the panel.\necho \"done\"\n"
	case contract.Daemon:
		return head + "# <switch.icon> lucide:repeat\n# <switch.type> daemon\n#\n" +
			"# SimpleToggle runs `<script> run` and keeps the process; turning the\n" +
			"# switch off sends SIGTERM. `exec` lets the signal reach your command.\n" +
			"exec caffeinate -d\n"
	default:
		return head + "# <switch.icon> lucide:toggle-right\n# <switch.type> toggle\n#\n" +
			"# on / off run once each; status prints \"on\" or \"off\" (polled every 5 s).\n" +
			"case \"$1\" in\n  on)\n    ;;\n  off)\n    ;;\n  status)\n    echo off\n    ;;\nesac\n"
	}
}

func (w *workbench) create(t contract.Type) {
	name := tr("new_name")
	id, _, err := w.b.CreateScript(template(t, name))
	if err != nil {
		w.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
		return
	}
	w.dirty = true
	w.open(secSwitches, id)
}

func (w *workbench) remove(id string) {
	if err := w.b.DeleteScript(id); err != nil {
		w.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
		return
	}
	delete(w.docs, id)
	w.selected = ""
	w.dirty = true
}

// ───────── 测试 ─────────

func (w *workbench) appendOut(id string, lines ...outLine) {
	out := append(w.outputs[id], lines...)
	if len(out) > 400 {
		out = out[len(out)-400:]
	}
	w.outputs[id] = out
	w.outScroll.Y = math.MaxFloat32
}

func outputLines(text string) []outLine {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	var out []outLine
	for _, l := range strings.Split(text, "\n") {
		out = append(out, outLine{outText, l})
	}
	return out
}

func durText(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func resultLines(code int, out string, dur time.Duration, timedOut bool) []outLine {
	lines := outputLines(out)
	switch {
	case timedOut:
		lines = append(lines, outLine{outErr, fmt.Sprintf(tr("timed_out"), durText(dur))})
	case code == 0:
		lines = append(lines, outLine{outOK, fmt.Sprintf(tr("exit_ok"), durText(dur))})
	default:
		lines = append(lines, outLine{outErr, fmt.Sprintf(tr("exit_fail"), code, durText(dur))})
	}
	return lines
}

// test 运行当前脚本的一个分支。有未保存的修改时先保存（保存失败就不运行）。
func (w *workbench) test(d *doc, arg string) {
	if d.modified() && !w.save(d) {
		return
	}
	id := d.id
	w.bottomOpen, w.bottomTab = true, 0
	label := filepath.Base(id)
	if arg != "" {
		label += " " + arg
	}
	w.appendOut(id, outLine{outCmd, "$ " + label})
	w.busy[id] = true
	done := func(lines ...outLine) {
		w.onMain(func() {
			delete(w.busy, id)
			w.appendOut(id, lines...)
			w.dirty, w.runsDirty = true, true
		})
	}
	fail := func(err error) {
		delete(w.busy, id)
		w.appendOut(id, outLine{outErr, err.Error()})
	}
	switch arg {
	case "": // action
		ch, err := w.b.Run(id, engine.Manual, wbDetail)
		if err != nil {
			fail(err)
			return
		}
		go func() {
			r, ok := <-ch
			if !ok {
				done()
				return
			}
			done(resultLines(r.ExitCode, r.Output, time.Duration(r.Duration)*time.Millisecond, r.ExitCode == -1)...)
		}()
	case "status":
		go func() {
			res, err := w.b.Status(id)
			if err != nil {
				done(outLine{outErr, err.Error()})
				return
			}
			done(resultLines(res.ExitCode, res.Output, res.Duration, res.TimedOut)...)
		}()
	default: // on / off / start / stop
		on := arg == "on" || arg == "start"
		ch, err := w.b.SetSwitch(id, on, engine.Manual, wbDetail)
		if err != nil {
			fail(err)
			return
		}
		go func() {
			<-ch
			if arg == "start" {
				if _, running := w.b.DaemonOutput(id); running {
					done(outLine{outInfo, tr("daemon_started")})
				} else {
					done(w.lastRunLines(id)...)
				}
				return
			}
			done(w.lastRunLines(id)...)
		}()
	}
}

// lastRunLines 取这个开关最近一次由工作台触发的运行记录。
func (w *workbench) lastRunLines(id string) []outLine {
	runs := w.b.Runs(store.Filter{Switch: id, Limit: 5})
	for _, r := range runs {
		if r.Detail == wbDetail {
			if r.Action == "stop" {
				return append(outputLines(r.Output), outLine{outInfo, tr("daemon_stopped")})
			}
			return resultLines(r.ExitCode, r.Output, time.Duration(r.Duration)*time.Millisecond, r.ExitCode == -1)
		}
	}
	return nil
}

// ───────── 视图 ─────────

func (w *workbench) switchEditor(c *ui.Context) {
	pal := w.pal
	d := w.currentDoc()
	if d == nil {
		w.topBar(c, func() {})
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			icon(c, "square-terminal", 28).TextColor(pal.T3)
			ui.Text(c, tr("no_switch")).FontSize(13).TextColor(pal.T3)
		})
		return
	}
	d.sync(pal)
	d.paintBand(pal)
	v := w.view(d.id)
	w.editorBar(c, d, v)
	if d.conflict {
		w.conflictBanner(c, d)
	}
	// 每个开关一个编辑区：在侧栏换开关时新内容淡入
	ui.Row(c.Key("ed-grid-" + d.id)).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch).
		Transition(ui.ElementTransition{Duration: 160 * time.Millisecond, Enter: &ui.Motion{}}).Children(func() {
		ui.Column(c.Key("ed-col")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			ui.Row(c.Key("band")).FillWidth().Padding(6, 16).Gap(8).AlignItems(ui.Center).Background(pal.BG).Children(func() {
				icon(c, "link-2", 12).TextColor(pal.T3)
				ui.Text(c, tr("band_note")).FontSize(11).TextColor(pal.T3).SingleLine()
			})
			th := editor.DefaultTheme(c.Theme())
			th.Background = pal.BG
			d.st.Theme = th
			d.st.Font = ui.Font{Size: w.prefs.EditorFontSize}
			d.st.LineNumbers = editor.LineNumbersOn
			if !w.prefs.LineNumbers {
				d.st.LineNumbers = editor.LineNumbersOff
			}
			editor.View(c.Key("editor-"+d.id), d.st).Grow(1).FillWidth().MinHeight(0)
			w.bottomPanel(c, d)
		})
		if w.inspector {
			pal.vhair(c)
			ui.Scroll(c.Key("insp")).Width(296).Shrink(0).FillHeight().Children(func() { w.inspectorView(c, d, v) })
		}
	})
	if v != nil && v.Type == contract.Daemon && v.State == engine.On && w.bottomOpen && w.bottomTab == 0 {
		c.After(time.Second) // 常驻进程的输出持续刷新
	}
	if t, ok := w.savedAt[d.id]; ok {
		if r := time.Until(t.Add(1600 * time.Millisecond)); r > 0 {
			c.After(r)
		} else {
			delete(w.savedAt, d.id)
		}
	}
}

func (w *workbench) editorBar(c *ui.Context, d *doc, v *engine.View) {
	pal := w.pal
	h := d.header
	w.topBar(c, func() {
		ui.Row(c.Key("crumb")).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
			switchIcon(c, h.Icon, 15).TextColor(pal.T1)
			name := h.Name
			if name == "" {
				name = tr("untitled")
			}
			ui.Text(c, name).FontSize(13).FontWeight(500).TextColor(pal.T1).SingleLine().MinWidth(0)
			pal.mono(c, d.id, 12, pal.T3)
			if n := len(d.issues); n == 0 {
				pal.badge(c, tr("contract_ok"), "green")
			} else {
				tone := "amber"
				if d.hasErrors() {
					tone = "red"
				}
				b := ui.Row(c.Key("problems-badge")).Children(func() { pal.badge(c, fmt.Sprintf(trn("n_problems", n), n), tone) })
				if b.Clicked() {
					w.bottomOpen, w.bottomTab = true, 1
				}
			}
			// 常驻开关运行中：信号色的小条，九点点阵 + running（有倒计时就显示剩余时间）
			if v != nil && v.Type == contract.Daemon && v.State == engine.On {
				ui.Row(c.Key("running")).Height(22).Padding(0, 8).Radius(4).Gap(7).AlignItems(ui.Center).Background(pal.sigSoft()).
					Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Enter: &ui.Motion{Scale: 0.9}, Exit: &ui.Motion{}}).Children(func() {
					pal.dotRun(c, 10)
					text := tr("w_running")
					if v.Remaining != nil {
						text += " · " + countdown(*v.Remaining)
					}
					ui.Text(c.Key("rt"), text).Font(monoFont).FontSize(11).TextColor(pal.Accent).SingleLine().
						ContentTransition(ui.ContentTransition{Numeric: true, Down: true})
				})
			}
			switch {
			case w.notice != "":
				col := pal.T2
				if w.noticeErr {
					col = pal.Red
				}
				w.noticeLabel(c, col)
			case d.modified():
				ui.Text(c, tr("unsaved")).Font(monoFont).FontSize(11).TextColor(pal.T3).SingleLine()
			case !w.savedAt[d.id].IsZero():
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					icon(c, "check", 12).TextColor(pal.Green)
					ui.Text(c, tr("saved")).FontSize(11.5).TextColor(pal.Green)
				})
			}
		})
		busy := w.busy[d.id] || (v != nil && (v.State == engine.Pending || v.State == engine.Running))
		switch h.Type {
		case contract.Action:
			if b := pal.button(c, "t-run", tr("run"), "play", btnPlain, busy); clicked(b, busy) {
				w.test(d, "")
			}
		case contract.Daemon:
			if b := pal.button(c, "t-start", tr("start"), "play", btnPlain, busy); clicked(b, busy) {
				w.test(d, "start")
			}
			if b := pal.button(c, "t-stop", tr("stop"), "square", btnPlain, busy); clicked(b, busy) {
				w.test(d, "stop")
			}
		default:
			for _, a := range []string{"on", "off", "status"} {
				if b := pal.button(c, "t-"+a, a, "", btnPlain, busy); clicked(b, busy) {
					w.test(d, a)
				}
			}
		}
		more := pal.iconBtn(c, "more", "ellipsis", tr("more"), w.moreOpen)
		if more.Clicked() {
			w.moreOpen = !w.moreOpen
		}
		ui.PopoverBase(c, more, &w.moreOpen, func(panel ui.Element) {
			w.menuPanel(panel)
			panel.Children(func() {
				path := filepath.Join(w.b.ScriptsDir(), d.id)
				if w.menuItem(c, "m-open", "square-arrow-out-up-right", tr("open_external")) {
					w.moreOpen = false
					w.openPath(path)
				}
				if w.menuItem(c, "m-reveal", "folder-search", tr("reveal")) {
					w.moreOpen = false
					w.reveal(path)
				}
				ui.Box(c).FillWidth().Height(1).Margin(4, 6).Background(pal.L2)
				if w.menuItem(c, "m-delete", "trash-2", tr("delete_switch")) {
					w.moreOpen, w.deleteOpen = false, true
				}
			})
		})
		if pal.iconBtn(c, "toggle-insp", "panel-right", tr("inspector"), w.inspector).Clicked() {
			w.inspector = !w.inspector
		}
		modified := d.modified()
		if clicked(pal.button(c, "save", tr("save")+"  ⌘S", "", btnPrimary, !modified), !modified) {
			w.save(d)
		}
	})
	if w.deleteOpen {
		name := d.header.Name
		switch ui.AlertDialog(c.Key("delete-dialog"), &w.deleteOpen, fmt.Sprintf(tr("delete_title"), name), tr("delete_msg"), tr("cancel"), tr("delete")) {
		case 1:
			w.remove(d.id)
		}
	}
}

func (w *workbench) conflictBanner(c *ui.Context, d *doc) {
	pal := w.pal
	ui.Row(c.Key("conflict")).FillWidth().Padding(8, 16).Gap(10).AlignItems(ui.Center).Background(alpha(pal.Amber, 0.12)).Children(func() {
		icon(c, "triangle-alert", 14).TextColor(pal.Amber)
		ui.Text(c, tr("disk_changed")).FontSize(12).TextColor(pal.T1).Grow(1)
		if pal.button(c, "reload-disk", tr("reload_disk"), "", btnPlain, false).Clicked() {
			if text, err := w.b.Script(d.id); err == nil {
				d.apply(text)
				d.disk = text
			}
			d.conflict = false
		}
		if pal.button(c, "keep-mine", tr("keep_mine"), "", btnGhost, false).Clicked() {
			if text, err := w.b.Script(d.id); err == nil {
				d.disk = text
			}
			d.conflict = false
		}
	})
}

// ───────── 底部面板 ─────────

func (w *workbench) bottomPanel(c *ui.Context, d *doc) {
	pal := w.pal
	pal.hair(c)
	history := w.b.Runs(store.Filter{Switch: d.id, Limit: 50})
	ui.Row(c.Key("btabs")).FillWidth().Height(34).Shrink(0).Padding(0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		tabs := []struct {
			iconName, label string
			count           int
		}{
			{"terminal", tr("tab_output"), -1},
			{"", tr("tab_problems"), len(d.issues)},
			{"", tr("tab_history"), len(history)},
		}
		for i, t := range tabs {
			sel := w.bottomOpen && w.bottomTab == i
			// C：选中的标签下面一条 2px 信号色
			tb := ui.Row(c.Key("tab-"+itoa(i))).Height(26).Padding(0, 9).Radius(4).Gap(6).AlignItems(ui.Center).Label(t.label)
			under := tb.AnimateTo("u", b2f(sel), animColor)
			tb.Draw(func(pt *ui.Painter, r ui.Rect) {
				if under > 0.01 {
					pt.Fill(ui.Rect{X: r.X + 8, Y: r.Y + r.H - 2, W: (r.W - 16) * under, H: 2}, pal.Accent, 1)
				}
			})
			col := pal.T3
			if sel || tb.Hovered() {
				col = pal.T1
			}
			col = tb.AnimateColor("fg", col, animHover)
			tb.Children(func() {
				if t.iconName != "" {
					icon(c, t.iconName, 12).TextColor(col)
				}
				ui.Text(c, t.label).Font(monoFont).FontSize(11.5).TextColor(col).SingleLine()
				if t.count >= 0 {
					pal.countText(c, "n", itoa(t.count)).TextColor(col)
				}
			})
			if tb.Clicked() {
				w.bottomOpen, w.bottomTab = true, i
			}
		}
		ui.Spacer(c)
		name, tip := "chevron-down", tr("collapse")
		if !w.bottomOpen {
			name, tip = "chevron-up", tr("expand")
		}
		if pal.iconBtn(c, "bottom-toggle", name, tip, false).Clicked() {
			w.bottomOpen = !w.bottomOpen
		}
	})
	if !w.bottomOpen {
		return
	}
	body := ui.Scroll(c.Key("bbody-"+itoa(w.bottomTab))).FillWidth().Height(170).Shrink(0).Padding(4, 16, 12)
	switch w.bottomTab {
	case 0:
		body.TrackScroll(&w.outScroll)
		body.Children(func() { w.outputView(c, d) })
	case 1:
		body.Children(func() { w.problemsView(c, d) })
	default:
		body.Children(func() { w.historyView(c, history) })
	}
}

func (w *workbench) outputView(c *ui.Context, d *doc) {
	pal := w.pal
	lines := w.outputs[d.id]
	if len(lines) == 0 {
		ui.Text(c, tr("output_ready")).FontSize(12).TextColor(pal.T3).Margin(4, 0)
	}
	// 只显示最后 maxOutRows 行：常驻进程的输出（最多 64KB）每秒刷新，不必每次都排几千行
	lines = tailLines(c, pal, lines, "o")
	for i, l := range lines {
		w.outRow(c, "o-"+itoa(i), l)
	}
	if out, running := w.b.DaemonOutput(d.id); running {
		for i, l := range tailLines(c, pal, outputLines(out), "live") {
			w.outRow(c, "live-"+itoa(i), l)
		}
		ui.Row(c.Key("live-run")).Gap(8).AlignItems(ui.Center).Padding(3, 0).Children(func() {
			pal.dotRun(c, 10)
			ui.Text(c, tr("w_running")).Font(monoFont).FontSize(12).TextColor(pal.Accent)
		})
	}
}

const maxOutRows = 200

// tailLines 返回最后 maxOutRows 行；截掉了的话先显示一行说明。
func tailLines(c *ui.Context, pal palette, lines []outLine, key string) []outLine {
	if len(lines) <= maxOutRows {
		return lines
	}
	ui.Text(c.Key(key+"-more"), fmt.Sprintf(trn("out_earlier", len(lines)-maxOutRows), len(lines)-maxOutRows)).FontSize(11).TextColor(pal.T3).Margin(2, 0)
	return lines[len(lines)-maxOutRows:]
}

func (w *workbench) outRow(c *ui.Context, key string, l outLine) {
	pal := w.pal
	col := pal.T1
	switch l.kind {
	case outCmd:
		col = pal.T2
	case outOK:
		col = pal.Green
	case outErr:
		col = pal.Red
	case outInfo:
		col = pal.T3
	}
	text := l.text
	if text == "" {
		text = " "
	}
	ui.Text(c.Key(key), text).Font(monoFont).FontSize(12).TextColor(col).Wrap().Selectable()
}

func (w *workbench) problemsView(c *ui.Context, d *doc) {
	pal := w.pal
	if len(d.issues) == 0 {
		ui.Text(c, tr("no_problems")).FontSize(12).TextColor(pal.T3).Margin(4, 0)
		return
	}
	for i, is := range d.issues {
		is := is
		r := ui.Row(c.Key("prob-"+itoa(i))).FillWidth().Padding(5, 4).Radius(6).Gap(8).AlignItems(ui.Start)
		if r.Hovered() && is.Line > 0 {
			r.Background(pal.L1)
		}
		r.Children(func() {
			name, col := "triangle-alert", pal.Amber
			if is.Severity == "error" {
				name, col = "circle-x", pal.Red
			}
			icon(c, name, 14).TextColor(col).Margin(1, 0, 0, 0)
			ui.Text(c, issueText(is)).FontSize(12).TextColor(pal.T1).Wrap().Grow(1).MinWidth(0)
			where := tr("whole_file")
			if is.Line > 0 {
				where = fmt.Sprintf(tr("line_n"), is.Line)
			}
			pal.mono(c, where, 11, pal.T3)
		})
		if r.Clicked() && is.Line > 0 {
			d.st.GoTo(editor.Pos{Line: is.Line - 1})
			d.st.ScrollTo(is.Line - 1)
		}
	}
}

func (w *workbench) historyView(c *ui.Context, runs []store.Run) {
	pal := w.pal
	if len(runs) == 0 {
		ui.Text(c, tr("no_history")).FontSize(12).TextColor(pal.T3).Margin(4, 0)
		return
	}
	for i, r := range runs {
		r := r
		ui.Row(c.Key("h-"+itoa(i))).FillWidth().Padding(4, 0).Gap(10).AlignItems(ui.Center).Children(func() {
			col := pal.Green
			if !r.OK {
				col = pal.Red
			}
			ui.Box(c).Size(6, 6).Radius(3).Background(col)
			pal.mono(c, r.Time.Local().Format("01-02 15:04:05"), 11.5, pal.T3).Width(110)
			ui.Text(c, actionLabel(r.Action)).FontSize(12).TextColor(pal.T1).Width(44)
			pal.badge(c, sourceLabel(r.Source), sourceTone(r.Source))
			ui.Text(c, detailLabel(r.Detail)).FontSize(11.5).TextColor(pal.T3).SingleLine().Grow(1).MinWidth(0)
			pal.mono(c, durText(time.Duration(r.Duration)*time.Millisecond), 11.5, pal.T3)
		})
	}
}

func actionLabel(a string) string {
	if s, ok := strs["act_"+a]; ok {
		return s
	}
	return a
}

func sourceLabel(src string) string {
	if s, ok := strs["src_"+src]; ok {
		return s
	}
	return src
}

func sourceTone(src string) string {
	if src == string(engine.AI) {
		return "accent"
	}
	return ""
}

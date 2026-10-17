package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/automation"
)

// 快捷键录制：点一下进入录制，按下组合键即保存；Esc 取消，Delete / Backspace 清除。

var keyNames = map[ui.Key]string{
	ui.KeySpace: "space", ui.KeyEnter: "enter", ui.KeyTab: "tab", ui.KeyLeft: "left", ui.KeyRight: "right",
	ui.KeyUp: "up", ui.KeyDown: "down", ui.KeyHome: "home", ui.KeyEnd: "end", ui.KeyPageUp: "pageup", ui.KeyPageDown: "pagedown",
	ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/", ui.KeySemicolon: ";",
	ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
}

func init() {
	letters := []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK, ui.KeyL, ui.KeyM,
		ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ}
	for i, k := range letters {
		keyNames[k] = string(rune('a' + i))
	}
	digits := []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
	for i, k := range digits {
		keyNames[k] = string(rune('0' + i))
	}
	fkeys := []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12}
	for i, k := range fkeys {
		keyNames[k] = fmt.Sprintf("f%d", i+1)
	}
}

// accelFor 把按键事件写成规范形式；不是有效的全局快捷键时返回错误。
func accelFor(mods ui.Modifiers, key ui.Key) (string, error) {
	name, ok := keyNames[key]
	if !ok {
		return "", automation.ErrBadShortcut
	}
	s := ""
	if mods&ui.Super != 0 {
		s += "cmd+"
	}
	if mods&ui.Ctrl != 0 {
		s += "ctrl+"
	}
	if mods&ui.Alt != 0 {
		s += "alt+"
	}
	if mods&ui.Shift != 0 {
		s += "shift+"
	}
	return automation.NormalizeShortcut(s + name)
}

// recorder 是快捷键录制的状态（工作台与设置窗口各有一个）。
type recorder struct {
	recording string            // 正在录制的那个输入
	err       string            // 录到无效组合键的那个输入
	recorded  map[string]string // 录到的快捷键，下一次构建时交给对应的输入
}

// shortcutField 显示并录制快捷键。issue 是这一项的冲突说明（空 = 没有问题）。
// 返回新值与是否改变。
func (w *workbench) shortcutField(c *ui.Context, key, value, issue string) (string, bool) {
	return w.rec.field(c, w.pal, key, value, issue)
}

func (r *recorder) field(c *ui.Context, pal palette, key, value, issue string) (string, bool) {
	if r.recorded == nil {
		r.recorded = map[string]string{}
	}
	out, changed := value, false
	// 按键在事件分发时处理（晚于上一次构建），结果放在 recorded 里，这次构建取出
	if res, ok := r.recorded[key]; ok {
		delete(r.recorded, key)
		out, changed = res, res != value
		value = res
	}
	recording := r.recording == key
	ui.Column(c.Key(key)).FillWidth().Gap(5).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			b := ui.ButtonBase(c.Key("rec")).MinWidth(110).Height(26).Padding(0, 10).Radius(7).Border(1, pal.L3).
				AlignItems(ui.Center).Justify(ui.Center).Label(tr("shortcut"))
			if recording {
				b.Border(1.5, pal.Accent).Background(alpha(pal.Accent, 0.06))
			} else if b.Hovered() {
				b.Background(pal.L1)
			}
			b.Children(func() {
				switch {
				case recording:
					ui.Text(c, tr("sc_press")).FontSize(12).TextColor(pal.T3)
				case value != "":
					pal.mono(c, automation.ShortcutSymbols(value), 12.5, pal.T1)
				default:
					ui.Text(c, tr("sc_record")).FontSize(12).TextColor(pal.T3)
				}
			})
			if recording {
				b.Focus().FocusRing(false)
				b.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind != ui.InputKeyDown || ev.Repeat {
						return false
					}
					switch {
					case ev.Key == ui.KeyEscape && ev.Mods == 0:
						r.recording, r.err = "", ""
					case (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete) && ev.Mods == 0:
						r.recording, r.err = "", ""
						r.recorded[key] = ""
					default:
						a, err := accelFor(ev.Mods, ev.Key)
						if err != nil {
							r.err = key
							return true
						}
						r.recording, r.err = "", ""
						r.recorded[key] = a
					}
					return true
				})
			}
			if b.Clicked() {
				if recording {
					r.recording = ""
				} else {
					r.recording, r.err = key, ""
				}
			}
			if value != "" && !recording {
				if pal.iconBtn(c, "clear", "x", tr("sc_clear"), false).Clicked() {
					out, changed = "", true
				}
			}
		})
		switch {
		case r.err == key:
			ui.Text(c, tr("sc_bad")).FontSize(11).TextColor(pal.Amber).Wrap()
		case issue != "":
			ui.Row(c).Gap(5).AlignItems(ui.Start).Children(func() {
				icon(c, "triangle-alert", 12).TextColor(pal.Amber).Margin(1, 0, 0, 0)
				ui.Text(c, issue).FontSize(11).TextColor(pal.Amber).Wrap().Grow(1).MinWidth(0)
			})
		}
	})
	return out, changed
}

// issueFor 返回某个绑定的冲突说明。
func (w *workbench) issueFor(owner string) string {
	if w.auto == nil {
		return ""
	}
	is, ok := w.auto.ShortcutIssues()[owner]
	if !ok {
		return ""
	}
	if is.Kind == "taken" {
		return tr("sc_taken")
	}
	return fmt.Sprintf(tr("sc_dup"), w.ownerName(is.Owner))
}

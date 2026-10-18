package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/paths"
)

// 退出与崩溃：
//   - 正常退出前，如果有开着的开关，先说明退出后会怎样（常驻开关停止、系统开关保持，下次启动恢复）；
//   - 运行期间数据目录里有一个 session 标记，正常退出时删除。启动时标记还在，说明上次是崩溃或被强杀：
//     遗留的常驻进程已由守护进程（或启动时的账本清理）结束，这里再告诉用户恢复了哪些开关。

const sessionFile = "session"

func sessionPath() string { return filepath.Join(paths.Data(), sessionFile) }

// beginSession 写下 session 标记，返回上次是否没有正常退出。
func beginSession() (unclean bool) {
	path := sessionPath()
	if _, err := os.Stat(path); err == nil {
		unclean = true
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
	return unclean
}

func endSession() { _ = os.Remove(sessionPath()) }

// onSwitches 返回开着的常驻开关与开关（不含按钮）。
func onSwitches(views []engine.View) (daemons, toggles []string) {
	for _, v := range views {
		if v.State != engine.On {
			continue
		}
		switch v.Type {
		case contract.Daemon:
			daemons = append(daemons, v.Name)
		case contract.Toggle:
			toggles = append(toggles, v.Name)
		}
	}
	return daemons, toggles
}

// quitDetail 是退出确认里的说明。
func quitDetail(daemons, toggles []string, restore bool) string {
	var parts []string
	if len(daemons) > 0 {
		s := fmt.Sprintf(tr("quit_daemons"), strings.Join(daemons, tr("list_sep")))
		if restore {
			s += tr("quit_daemons_restore")
		} else {
			s += tr("quit_daemons_norestore")
		}
		parts = append(parts, s)
	}
	if len(toggles) > 0 {
		parts = append(parts, fmt.Sprintf(tr("quit_toggles"), strings.Join(toggles, tr("list_sep"))))
	}
	return strings.Join(parts, "\n\n")
}

// requestQuit 是面板与右键菜单里的「退出」。
func (c *controller) requestQuit() {
	daemons, toggles := onSwitches(c.eng.List())
	if len(daemons)+len(toggles) == 0 || c.st.Setting(keyConfirmQuit) == "false" {
		mygo.App.Quit()
		return
	}
	if c.flyout != nil {
		c.flyout.Close()
	}
	detail := quitDetail(daemons, toggles, c.st.Setting(keyRestore) != "false")
	mygo.App.Focus() // 菜单栏 app 没有前台窗口：先把 app 带到前台，对话框才不会躲在别的窗口后面
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Type: mygo.MessageWarning, Message: tr("quit_title"), Detail: detail,
			Buttons: []string{tr("quit"), tr("cancel")}, DefaultButton: 0, CancelButton: 1,
			CheckboxLabel: tr("quit_dont_ask"),
		})
		if err != nil || res.Button != 0 {
			return
		}
		if res.CheckboxChecked {
			_ = c.st.SetSetting(keyConfirmQuit, "false")
		}
		mygo.RunOnMain(mygo.App.Quit)
	}()
}

// reportUncleanExit 在上次没有正常退出时发一条通知：说明遗留进程已清理、恢复了哪些开关。
// 等几秒再看，让启动时的恢复（开关要先查一次状态）有时间完成。
func (c *controller) reportUncleanExit(desired []string) {
	time.AfterFunc(4*time.Second, func() {
		body := tr("crash_body_none")
		if len(desired) > 0 {
			if c.st.Setting(keyRestore) == "false" {
				body = fmt.Sprintf(tr("crash_body_norestore"), strings.Join(desired, tr("list_sep")))
			} else {
				body = fmt.Sprintf(tr("crash_body_restored"), strings.Join(desired, tr("list_sep")))
			}
		}
		mygo.RunOnMain(func() {
			n := mygo.NewNotification(mygo.NotificationOptions{Title: tr("crash_title"), Body: body})
			n.OnClick(func() { mygo.RunOnMain(func() { c.openWorkbench(secActivity, "") }) })
			_ = n.Show()
		})
	})
}

// desiredNames 是期望开着的开关的名字（启动时、恢复之前读取）。
func desiredNames(eng *engine.Engine, desired map[string]bool) []string {
	var out []string
	for _, v := range eng.List() {
		if desired[v.ID] && v.Type != contract.Action {
			out = append(out, v.Name)
		}
	}
	return out
}

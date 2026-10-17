package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// TestSnapshots 把工作台的几个状态渲染成 PNG，供人工核对视觉（默认跳过）：
//
//	OT_SNAPSHOT_DIR=/tmp/shots go test -run TestSnapshots ./app/
func TestSnapshots(t *testing.T) {
	dir := os.Getenv("OT_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set OT_SNAPSHOT_DIR to render snapshots")
	}
	save := func(tt *ui.Tester, name string) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tt.Image())
	}
	for _, dark := range []bool{false, true} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		w, b, tt, q := newTestWorkbench(t)
		tt.SetDark(dark)
		tt.Frame()
		// 参数展开 + 图标选择
		w.paramOpen = 0
		tt.Frame()
		save(tt, "params"+suffix)
		tt.Click(tr("f_icon"))
		tt.Frame()
		save(tt, "icons"+suffix)
		w.iconOpen = false
		// 有错误的脚本 + 问题页
		d := w.docs["awake.sh"]
		d.apply(d.text() + "\n")
		d.apply(replaceOnce(d.text(), "# <switch.type> daemon", "# <switch.type> daemn"))
		w.bottomTab = 1
		tt.Frame()
		save(tt, "problems"+suffix)
		// 按钮测试输出
		w.open(secSwitches, "flush.sh")
		tt.Frame()
		tt.Click(tr("run"))
		tt.Frame()
		(<-q)()
		tt.Frame()
		save(tt, "output"+suffix)
		// 脚本库 + 抽屉
		w.open(secLibrary, "")
		w.libSel = "pomodoro.sh"
		tt.Frame()
		tt.Frame()
		save(tt, "library"+suffix)
		// 活动
		b.mu.Lock()
		b.runs = []store.Run{
			{ID: 1, Switch: "flush.sh", Name: "Flush DNS", Action: "run", Source: "manual", OK: true, Duration: 182, Output: "DNS cache flushed"},
			{ID: 2, Switch: "awake.sh", Name: "Keep Awake", Action: "start", Source: "ai", Detail: "mcp/claude-code", OK: true},
			{ID: 3, Switch: "awake.sh", Name: "Keep Awake", Action: "exit", Source: "system", Detail: "exited unexpectedly", ExitCode: 1, Output: "caffeinate: invalid option -- x\nusage: caffeinate [-disu] [-t timeout]"},
		}
		b.mu.Unlock()
		w.handle(engine.Event{Kind: engine.EventRun})
		w.open(secActivity, "")
		w.runSel = 1
		tt.Frame()
		save(tt, "activity"+suffix)
	}
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

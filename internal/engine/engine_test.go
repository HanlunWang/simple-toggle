package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/HanlunWang/open-toggle/internal/store"
)

type fixture struct {
	t       *testing.T
	dir     string // 脚本目录
	data    string // 状态目录
	scratch string // 脚本写标记文件的地方
	st      *store.Store
	e       *Engine
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, dir: filepath.Join(root, "switches"), data: filepath.Join(root, "data"), scratch: filepath.Join(root, "scratch")}
	os.MkdirAll(f.dir, 0o755)
	os.MkdirAll(f.scratch, 0o755)
	f.open()
	return f
}

func (f *fixture) open() {
	st, err := store.Open(f.data)
	if err != nil {
		f.t.Fatal(err)
	}
	f.st = st
	f.e = New(Config{
		ScriptsDir: f.dir, LedgerPath: filepath.Join(f.data, "daemons.json"), Bin: "/usr/bin/true",
		Store: st, DefaultTimeout: 5 * time.Second, PollHidden: time.Hour, PollVisible: time.Hour,
	})
}

func (f *fixture) write(name, body string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

// toggle：标记文件存在即为 on；on 时把参数值写进去，便于检查注入。
func (f *fixture) toggle(name string) string {
	mark := filepath.Join(f.scratch, name+".on")
	f.write(name, fmt.Sprintf(`#!/bin/sh
# <switch.name> %s
# <switch.param> key=level type=number default=1
case "$1" in
  on) echo "$SWITCH_LEVEL $OPENTOGGLE_TRIGGER" > %q ;;
  off) rm -f %q ;;
  status) [ -f %q ] && echo on || echo off ;;
esac
`, name, mark, mark, mark))
	return mark
}

func (f *fixture) waitState(id string, want State) View {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, ok := f.e.Get(id)
		if ok && v.State == want {
			return v
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("%s: state %q, want %q", id, v.State, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestToggleOnOffAndParams(t *testing.T) {
	f := newFixture(t)
	mark := f.toggle("lamp.sh")
	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}
	defer f.e.Shutdown()
	f.waitState("lamp.sh", Off)

	done, err := f.e.SetSwitch("lamp", true, CLI, "") // 省略 .sh 也能找到
	if err != nil || !Wait(done, 5*time.Second) {
		t.Fatal("on did not finish", err)
	}
	v := f.waitState("lamp.sh", On)
	if data, _ := os.ReadFile(mark); strings.TrimSpace(string(data)) != "1 cli" {
		t.Fatalf("env injection: %q", data)
	}
	if v.ActivatedAt.IsZero() || !f.st.Desired("lamp.sh") {
		t.Fatal("activation / desired not recorded")
	}
	// 改参数：开着时立即用新值重跑 on
	if err := f.e.SetParams("lamp.sh", map[string]string{"level": "7"}, AI); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if data, _ := os.ReadFile(mark); strings.TrimSpace(string(data)) == "7 ai" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("params change did not re-run on")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := f.e.SetParams("lamp.sh", map[string]string{"nope": "1"}, AI); err == nil {
		t.Fatal("unknown param accepted")
	}
	// 隐藏只影响可见性
	f.e.SetHidden("lamp.sh", true)
	if v, _ := f.e.Get("lamp.sh"); !v.Hidden || v.State != On {
		t.Fatalf("hidden changed state: %+v", v)
	}
	done, _ = f.e.SetSwitch("lamp.sh", false, Manual, "")
	Wait(done, 5*time.Second)
	f.waitState("lamp.sh", Off)
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Fatal("off did not run")
	}
	runs := f.st.Runs(store.Filter{Switch: "lamp.sh"})
	if len(runs) < 3 || runs[0].Action != "off" || runs[0].Source != "manual" {
		t.Fatalf("runs %+v", runs)
	}
}

func TestToggleErrorAndUnknown(t *testing.T) {
	f := newFixture(t)
	f.write("bad.sh", "#!/bin/sh\n# <switch.name> Bad\ncase \"$1\" in on) echo boom >&2; exit 2 ;; status) exit 1 ;; esac\n")
	f.e.Start()
	defer f.e.Shutdown()
	f.waitState("bad.sh", Unknown)
	done, _ := f.e.SetSwitch("bad.sh", true, Manual, "")
	Wait(done, 5*time.Second)
	f.waitState("bad.sh", Error)
	if r := f.st.Runs(store.Filter{Switch: "bad.sh"})[0]; r.OK || r.ExitCode != 2 || !strings.Contains(r.Output, "boom") {
		t.Fatalf("run record %+v", r)
	}
}

func TestDaemonLifecycle(t *testing.T) {
	f := newFixture(t)
	f.write("serve.sh", "#!/bin/sh\n# <switch.name> Serve\n# <switch.type> daemon\n# <switch.param> key=duration type=number default=10\n# <switch.menubar> mode=add icon=x countdown=on\nexec sleep 30\n")
	f.write("brief.sh", "#!/bin/sh\n# <switch.name> Brief\n# <switch.type> daemon\nsleep 0.2\nexit 0\n")
	f.write("crash.sh", "#!/bin/sh\n# <switch.name> Crash\n# <switch.type> daemon\nsleep 0.2\nexit 3\n")
	f.e.Start()
	defer f.e.Shutdown()

	done, _ := f.e.SetSwitch("serve.sh", true, Manual, "")
	Wait(done, 5*time.Second)
	v := f.waitState("serve.sh", On)
	if v.Remaining == nil || *v.Remaining < 590 || *v.Remaining > 600 {
		t.Fatalf("countdown %v", v.Remaining)
	}
	ledger := readLedger(filepath.Join(f.data, "daemons.json"))
	pid := ledger["serve.sh"].PID
	if pid == 0 || syscall.Kill(pid, 0) != nil {
		t.Fatalf("ledger %v", ledger)
	}
	done, _ = f.e.SetSwitch("serve.sh", false, Manual, "")
	Wait(done, 5*time.Second)
	f.waitState("serve.sh", Off)
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("daemon still alive after off")
	}

	// 自然结束：开关归位，期望状态清除
	done, _ = f.e.SetSwitch("brief.sh", true, Manual, "")
	Wait(done, 5*time.Second)
	f.waitState("brief.sh", Off)
	if f.st.Desired("brief.sh") {
		t.Fatal("natural exit should clear desired state")
	}
	// 异常退出：error，期望状态保留
	done, _ = f.e.SetSwitch("crash.sh", true, Manual, "")
	Wait(done, 5*time.Second)
	f.waitState("crash.sh", Error)
	if !f.st.Desired("crash.sh") {
		t.Fatal("crash should keep desired state")
	}
}

func TestAction(t *testing.T) {
	f := newFixture(t)
	f.write("flush.sh", "#!/bin/sh\n# <switch.name> Flush\n# <switch.type> action\nsleep 0.3\necho \"flushed $OPENTOGGLE_TRIGGER\"\n")
	f.write("fail.sh", "#!/bin/sh\n# <switch.name> Fail\n# <switch.type> action\nexit 4\n")
	f.e.Start()
	defer f.e.Shutdown()
	if _, err := f.e.SetSwitch("flush.sh", true, Manual, ""); err != ErrNotToggle {
		t.Fatal("action accepted on/off")
	}
	ch, err := f.e.Run("flush.sh", Shortcut, "ctrl+opt+f")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.e.Get("flush.sh"); v.State != Running {
		t.Fatalf("state while running %q", v.State)
	}
	if _, err := f.e.Run("flush.sh", Manual, ""); err != ErrBusy {
		t.Fatal("second run while busy should fail")
	}
	rec := <-ch
	if !rec.OK || !strings.Contains(rec.Output, "flushed shortcut") || rec.Detail != "ctrl+opt+f" {
		t.Fatalf("record %+v", rec)
	}
	f.waitState("flush.sh", Idle)
	ch, _ = f.e.Run("fail.sh", Manual, "")
	if rec := <-ch; rec.OK || rec.ExitCode != 4 {
		t.Fatalf("fail record %+v", rec)
	}
	if _, err := f.e.Run("nope", Manual, ""); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestStalePollDiscarded(t *testing.T) {
	f := newFixture(t)
	gate := filepath.Join(f.scratch, "gate")
	mark := filepath.Join(f.scratch, "slow.on")
	// status 很慢，且在用户操作之前读取状态：结果必须作废，不能覆盖刚执行的 on
	f.write("slow.sh", fmt.Sprintf(`#!/bin/sh
# <switch.name> Slow
case "$1" in
  on) touch %q ;;
  off) rm -f %q ;;
  status) if [ -f %q ]; then s=on; else s=off; fi; [ -f %q ] && sleep 1; echo $s ;;
esac
`, mark, mark, mark, gate))
	f.e.Start()
	defer f.e.Shutdown()
	f.waitState("slow.sh", Off)
	os.WriteFile(gate, nil, 0o644)
	go f.e.refresh() // 慢速轮询：读到 off 后等 1 秒
	time.Sleep(200 * time.Millisecond)
	os.Remove(gate)
	done, _ := f.e.SetSwitch("slow.sh", true, Manual, "")
	Wait(done, 5*time.Second)
	f.waitState("slow.sh", On)
	time.Sleep(1200 * time.Millisecond) // 等慢速轮询返回
	if v, _ := f.e.Get("slow.sh"); v.State != On {
		t.Fatalf("stale poll overwrote state: %q", v.State)
	}
}

func TestRestoreAndOrphanSweep(t *testing.T) {
	f := newFixture(t)
	mark := f.toggle("lamp.sh")
	f.write("serve.sh", "#!/bin/sh\n# <switch.name> Serve\n# <switch.type> daemon\nexec sleep 30\n")
	f.st.SetDesired("lamp.sh", true)
	f.st.SetDesired("serve.sh", true)

	// 上一代 app 崩溃时留下的进程：脚本 exec 成了别的程序，命令行已不含脚本目录，
	// 只能靠启动时间认出它（v1 的命令行检查在这种情况下会漏掉）
	orphan := exec.Command("/bin/sh", "-c", "exec sleep 30")
	orphan.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := orphan.Start(); err != nil {
		t.Fatal(err)
	}
	go orphan.Wait()
	// 无关进程：PID 在账本里，但启动时间对不上（模拟 PID 被复用），不能误杀
	bystander := exec.Command("/bin/sleep", "30")
	if err := bystander.Start(); err != nil {
		t.Fatal(err)
	}
	go bystander.Wait()
	defer bystander.Process.Kill()
	ledger := fmt.Sprintf(`{"old.sh":{"pid":%d,"started":%d},"reused.sh":{"pid":%d,"started":%d}}`,
		orphan.Process.Pid, time.Now().Unix(), bystander.Process.Pid, time.Now().Add(-time.Hour).Unix())
	os.WriteFile(filepath.Join(f.data, "daemons.json"), []byte(ledger), 0o644)

	f.e.Start()
	defer f.e.Shutdown()
	f.waitState("lamp.sh", On)
	f.waitState("serve.sh", On)
	if _, err := os.Stat(mark); err != nil {
		t.Fatal("restore did not run on")
	}
	time.Sleep(200 * time.Millisecond)
	if syscall.Kill(orphan.Process.Pid, 0) == nil {
		syscall.Kill(orphan.Process.Pid, syscall.SIGKILL)
		t.Fatal("orphan not swept")
	}
	if syscall.Kill(bystander.Process.Pid, 0) != nil {
		t.Fatal("unrelated process with a reused pid was killed")
	}
}

func TestSweepV1Ledger(t *testing.T) {
	f := newFixture(t)
	// v1 账本只有 {id: pid}：命令行指向脚本目录的才清理
	ours := exec.Command("/bin/sh", "-c", "sleep 30; :", filepath.Join(f.dir, "v1.sh"))
	ours.Start()
	go ours.Wait()
	os.WriteFile(filepath.Join(f.data, "daemons.json"), []byte(`{"v1.sh":`+strconv.Itoa(ours.Process.Pid)+`}`), 0o644)
	f.e.Start()
	defer f.e.Shutdown()
	time.Sleep(200 * time.Millisecond)
	if syscall.Kill(ours.Process.Pid, 0) == nil {
		ours.Process.Kill()
		t.Fatal("v1 orphan not swept")
	}
}

func TestScriptsCRUDAndSeed(t *testing.T) {
	f := newFixture(t)
	var trashed []string
	f.e.cfg.Trash = func(p string) error { trashed = append(trashed, p); return os.Remove(p) }
	f.e.Start()
	defer f.e.Shutdown()

	src := "#!/bin/sh\n# <switch.name> Keep Awake\n# <switch.type> action\necho hi\n"
	id, _, err := f.e.CreateScript(src)
	if err != nil || id != "keep-awake.sh" {
		t.Fatal(id, err)
	}
	id2, _, _ := f.e.CreateScript(src)
	if id2 != "keep-awake-2.sh" {
		t.Fatalf("collision name %q", id2)
	}
	if fi, _ := os.Stat(filepath.Join(f.dir, id)); fi.Mode()&0o111 == 0 {
		t.Fatal("script not executable")
	}
	if _, err := f.e.UpdateScript(id, "#!/bin/sh\necho no-name\n", Manual); err == nil {
		t.Fatal("lint errors accepted")
	}
	if _, err := f.e.UpdateScript(id, strings.Replace(src, "Keep Awake", "Stay Up", 1), Manual); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.e.Get(id); v.Name != "Stay Up" {
		t.Fatal("update not reloaded")
	}
	if err := f.e.DeleteScript(id2); err != nil || len(trashed) != 1 {
		t.Fatal("delete", err)
	}
	if _, ok := f.e.Get(id2); ok {
		t.Fatal("deleted switch still listed")
	}

	ex := []Example{{File: "a.sh", Content: src, ShownByDefault: true}, {File: "b.sh", Content: src}}
	f.e.Seed(ex)
	os.Remove(filepath.Join(f.dir, "a.sh")) // 用户删掉了
	f.e.Seed(ex)
	if _, err := os.Stat(filepath.Join(f.dir, "a.sh")); !os.IsNotExist(err) {
		t.Fatal("deleted preset came back")
	}
	if !f.st.Hidden("b.sh") || f.st.Hidden("a.sh") {
		t.Fatal("default visibility")
	}
}

func TestEventsAndWatch(t *testing.T) {
	f := newFixture(t)
	f.e.cfg.WatchInterval = 50 * time.Millisecond
	ch, cancel := f.e.Subscribe()
	defer cancel()
	f.e.Start()
	defer f.e.Shutdown()
	f.toggle("late.sh") // 脚本目录里出现新脚本
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-ch:
			if _, ok := f.e.Get("late.sh"); ok {
				return
			}
		case <-deadline:
			t.Fatal("new script not picked up")
		}
	}
}

package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func script(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.sh")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunStatusAndExitCodes(t *testing.T) {
	p := script(t, "#!/bin/sh\ncase \"$1\" in status) echo '  on '; echo warn >&2 ;; off) exit 3 ;; esac\n", 0o755)
	r := Run(context.Background(), Spec{Path: p, Args: []string{"status"}, Env: os.Environ()})
	if r.ExitCode != 0 || r.Stdout != "on" || !strings.Contains(r.Output, "warn") {
		t.Fatalf("status: %+v", r)
	}
	if r := Run(context.Background(), Spec{Path: p, Args: []string{"off"}}); r.ExitCode != 3 {
		t.Fatalf("off: %+v", r)
	}
}

func TestRunWithoutShebangOrExecBit(t *testing.T) {
	p := script(t, "echo \"$1-$SWITCH_X\"\n", 0o644)
	r := Run(context.Background(), Spec{Path: p, Args: []string{"status"}, Env: []string{"SWITCH_X=1"}})
	if r.ExitCode != 0 || r.Stdout != "status-1" {
		t.Fatalf("%+v", r)
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child.pid")
	p := script(t, "#!/bin/sh\nsleep 30 &\necho $! > "+marker+"\nsleep 30\n", 0o755)
	r := Run(context.Background(), Spec{Path: p, Timeout: 300 * time.Millisecond})
	if !r.TimedOut || r.ExitCode != -1 {
		t.Fatalf("%+v", r)
	}
	data, _ := os.ReadFile(marker)
	var pid int
	if _, err := fmtSscan(strings.TrimSpace(string(data)), &pid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("background child survived the timeout")
	}
}

func TestBackgroundChildDoesNotBlock(t *testing.T) {
	// on 分支启动后台进程后立即退出：不能等到后台进程结束
	p := script(t, "#!/bin/sh\nsleep 5 &\necho started\n", 0o755)
	start := time.Now()
	r := Run(context.Background(), Spec{Path: p})
	if r.ExitCode != 0 || r.Stdout != "started" || time.Since(start) > 3*time.Second {
		t.Fatalf("%+v after %v", r, time.Since(start))
	}
}

func TestDaemonStopAndNaturalExit(t *testing.T) {
	p := script(t, "#!/bin/sh\ntrap 'exit 0' TERM\necho up\nwhile :; do sleep 0.1; done\n", 0o755)
	proc, err := Start(Spec{Path: p, Args: []string{"run"}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if !proc.Running() || !strings.Contains(proc.Output.String(), "up") {
		t.Fatal("daemon not running")
	}
	proc.Stop()
	if proc.Running() || !proc.Exit().Stopped {
		t.Fatalf("stop: %+v", proc.Exit())
	}

	q, _ := Start(Spec{Path: script(t, "#!/bin/sh\nexit 0\n", 0o755)})
	<-q.Done()
	if e := q.Exit(); e.Code != 0 || e.Signaled || e.Stopped {
		t.Fatalf("natural exit: %+v", e)
	}
	r, _ := Start(Spec{Path: script(t, "#!/bin/sh\nexit 7\n", 0o755)})
	<-r.Done()
	if r.Exit().Code != 7 {
		t.Fatalf("exit 7: %+v", r.Exit())
	}
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		data, _ := os.ReadFile(path)
		fmtSscan(strings.TrimSpace(string(data)), &pid)
		if pid == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if pid == 0 {
		t.Fatal("no pid written")
	}
	return pid
}

func TestDaemonExitKillsBackgroundChildren(t *testing.T) {
	// 常驻进程自然结束时留下的后台子进程要一并结束
	marker := filepath.Join(t.TempDir(), "child.pid")
	p, err := Start(Spec{Path: script(t, "#!/bin/sh\nsleep 30 &\necho $! > "+marker+"\nexit 0\n", 0o755)})
	if err != nil {
		t.Fatal(err)
	}
	<-p.Done()
	pid := readPid(t, marker)
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("background child survived the daemon's exit")
	}
	if e := p.Exit(); e.Code != 0 || e.Signaled {
		t.Fatalf("exit: %+v", e)
	}
	p.Stop() // 组长与子进程都已结束：不报错、不阻塞
}

func TestStopKillsChildrenThatIgnoreTerm(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child.pid")
	p, err := Start(Spec{Path: script(t, "#!/bin/sh\nsh -c 'trap \"\" TERM; while :; do sleep 0.1; done' &\necho $! > "+marker+"\nwhile :; do sleep 0.1; done\n", 0o755)})
	if err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, marker)
	p.Stop()
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("child ignoring SIGTERM survived Stop")
	}
}

func TestRingKeepsTail(t *testing.T) {
	var r Ring
	r.Write([]byte(strings.Repeat("a", MaxOutput)))
	r.Write([]byte("END"))
	if s := r.String(); len(s) != MaxOutput || !strings.HasSuffix(s, "END") {
		t.Fatalf("len %d", len(s))
	}
}

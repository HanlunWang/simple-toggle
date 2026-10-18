package engine

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// 守护进程：app 崩溃或被强杀时 Shutdown 不会执行，常驻进程（如 caffeinate）会一直运行到
// 下次启动才被清理；用户不再打开 app 的话就一直运行下去。所以第一个常驻进程启动时，
// 顺带拉起一个极小的守护进程（同一个二进制的 `__guard` 子命令）：它只是等 app 退出，
// 不占 CPU；app 正常退出前会删除账本并结束它，意外退出时它按账本结束遗留的常驻进程。

// GuardCommand 是守护进程的子命令名（cmd/simpletoggle 据此分派）。
const GuardCommand = "__guard"

// ensureGuard 在需要时拉起守护进程（已有则不重复）。
func (e *Engine) ensureGuard() {
	if !e.cfg.Guard || e.cfg.Bin == "" || e.cfg.LedgerPath == "" {
		return
	}
	e.guardMu.Lock()
	defer e.guardMu.Unlock()
	if e.guard != nil {
		return
	}
	cmd := exec.Command(e.cfg.Bin, GuardCommand, strconv.Itoa(os.Getpid()), e.cfg.LedgerPath, e.cfg.ScriptsDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // 自成进程组：不随 app 的进程组一起收到信号
	if err := cmd.Start(); err != nil {
		return
	}
	e.guard = cmd
	go func() { _ = cmd.Wait() }() // 回收，避免僵尸进程
}

// stopGuard 在正常退出时结束守护进程（账本已删除，它无事可做）。
func (e *Engine) stopGuard() {
	e.guardMu.Lock()
	defer e.guardMu.Unlock()
	if e.guard != nil && e.guard.Process != nil {
		_ = e.guard.Process.Signal(syscall.SIGTERM)
	}
	e.guard = nil
}

// RunGuard 是 `simpletoggle __guard <pid> <账本> <脚本目录>`：等 pid 退出，然后清理账本里的常驻进程。
func RunGuard(args []string) int {
	if len(args) != 3 {
		return 2
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil || pid <= 1 {
		return 2
	}
	waitExit(pid)
	SweepLedger(args[1], args[2])
	return 0
}

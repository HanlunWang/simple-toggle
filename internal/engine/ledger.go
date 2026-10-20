package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/runner"
)

// ledgerEntry 是 PID 账本的一项。v1 的账本只有 {id: pid}，读取时两种格式都认。
type ledgerEntry struct {
	PID     int   `json:"pid"`
	Started int64 `json:"started"`
}

// writeLedger 记下存活的常驻进程。app 崩溃或被强杀时 Shutdown 不会执行，
// 下次启动据此清理遗留进程（否则 caffeinate 之类会让屏幕一直亮着）。
// spawn、stopDaemon、superviseDaemon 会并发调用：快照与写入在 ledgerMu 下整体串行，
// 保证最后落盘的是最新的快照。
func (e *Engine) writeLedger() {
	if e.cfg.LedgerPath == "" {
		return
	}
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	e.mu.Lock()
	if e.closing {
		// Shutdown 结束常驻进程后会删除账本；这时再写会把账本留下
		e.mu.Unlock()
		return
	}
	entries := map[string]ledgerEntry{}
	for id, r := range e.rts {
		if r.daemon != nil && r.daemon.Running() {
			entries[id] = ledgerEntry{PID: r.daemon.Pid, Started: r.daemon.Started.Unix()}
		}
	}
	e.mu.Unlock()
	if len(entries) == 0 {
		_ = os.Remove(e.cfg.LedgerPath)
		return
	}
	data, _ := json.Marshal(entries)
	_ = writeFileAtomic(e.cfg.LedgerPath, data, 0o644)
}

// writeFileAtomic 写到同目录的临时文件、落盘后改名：读者只会看到完整的旧文件或新文件。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func readLedger(path string) map[string]ledgerEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v2 map[string]ledgerEntry
	if json.Unmarshal(data, &v2) == nil {
		return v2
	}
	var v1 map[string]int
	if json.Unmarshal(data, &v1) == nil {
		out := map[string]ledgerEntry{}
		for id, pid := range v1 {
			out[id] = ledgerEntry{PID: pid}
		}
		return out
	}
	return nil
}

// ownedByUs 判断 pid 仍是账本记下的那个进程，防止 PID 被系统复用后误杀无关进程。
// v2 账本比对进程启动时间：脚本 exec 成别的程序（如 caffeinate）后命令行会变，
// 但启动时间不变。v1 账本没有启动时间，只能检查命令行是否指向脚本目录。
func ownedByUs(ent ledgerEntry, scriptsDir string) bool {
	if ent.Started > 0 {
		out, err := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(ent.PID)).Output()
		if err != nil {
			return false
		}
		t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.Join(strings.Fields(string(out)), " "), time.Local)
		if err != nil {
			return false
		}
		d := t.Unix() - ent.Started
		return d >= -2 && d <= 2
	}
	out, err := exec.Command("/bin/ps", "-o", "command=", "-p", strconv.Itoa(ent.PID)).Output()
	return err == nil && strings.Contains(string(out), scriptsDir)
}

// sweepOrphans 终止账本里的上一代常驻进程（app 崩溃或被强杀时遗留的）。
func (e *Engine) sweepOrphans() { SweepLedger(e.cfg.LedgerPath, e.cfg.ScriptsDir) }

// SweepLedger 终止账本里记下的常驻进程并删除账本，返回终止了几个。
// 启动时（清理上次崩溃的遗留）和守护进程（app 意外退出的那一刻）都用它。
// 先 SIGTERM，宽限期（runner.KillGrace）后仍在的 SIGKILL。
func SweepLedger(path, scriptsDir string) int {
	entries := readLedger(path)
	// target 是信号目标：负数为整个进程组
	var targets []int
	for _, ent := range entries {
		if ent.PID <= 1 || !ownedByUs(ent, scriptsDir) {
			continue
		}
		// v2 启动的进程自成进程组，整组终止；v1 的不是组长，只能终止进程本身
		target := -ent.PID
		if syscall.Kill(target, syscall.SIGTERM) != nil {
			target = ent.PID
			_ = syscall.Kill(target, syscall.SIGTERM)
		}
		targets = append(targets, target)
	}
	n := len(targets)
	if entries != nil {
		_ = os.Remove(path)
	}
	deadline := time.Now().Add(runner.KillGrace)
	for len(targets) > 0 {
		alive := targets[:0]
		for _, t := range targets {
			if syscall.Kill(t, 0) == nil {
				alive = append(alive, t)
			}
		}
		targets = alive
		if len(targets) == 0 {
			break
		}
		if time.Now().After(deadline) {
			for _, t := range targets {
				_ = syscall.Kill(t, syscall.SIGKILL)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return n
}

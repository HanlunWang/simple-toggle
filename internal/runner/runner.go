// Package runner 执行开关脚本：短命令（on / off / status / action）与常驻进程（daemon）。
//
// 每个脚本在自己的进程组里运行，停止或超时时整组终止（先 SIGTERM，宽限后 SIGKILL），
// 从机制上避免脚本派生的子进程（如 caffeinate）变成孤儿。
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// MaxOutput 是每次执行保留的输出上限（stdout 与 stderr 合并后的末尾部分）。
	MaxOutput = 64 << 10
	// IOGrace 是进程退出后等待输出管道关闭的上限：脚本把后台子进程留着时，
	// 管道不会关闭，不能无限等下去。
	IOGrace = time.Second
	// KillGrace 是 SIGTERM 之后到 SIGKILL 的宽限期。
	KillGrace = 3 * time.Second
)

// Spec 描述一次执行。
type Spec struct {
	Path    string   // 脚本路径
	Args    []string // on / off / status / run；action 为空
	Env     []string // 完整环境（os.Environ() 加注入的变量）
	Shell   string   // 没有 shebang 时用来执行脚本的 shell，默认 /bin/sh
	Timeout time.Duration
}

// Result 是一次短命令的结果。
type Result struct {
	ExitCode int           // 无法启动时为 127，超时为 -1
	Stdout   string        // 去首尾空白
	Output   string        // stdout 与 stderr 合并的末尾 MaxOutput 字节
	Duration time.Duration //
	TimedOut bool
	Err      error // 启动失败等
}

func command(s Spec) *exec.Cmd {
	var cmd *exec.Cmd
	if hasShebang(s.Path) && isExecutable(s.Path) {
		cmd = exec.Command(s.Path, s.Args...)
	} else {
		sh := s.Shell
		if sh == "" {
			sh = "/bin/sh"
		}
		cmd = exec.Command(sh, append([]string{s.Path}, s.Args...)...)
	}
	cmd.Env = s.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	return cmd
}

func hasShebang(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 2)
	n, _ := io.ReadFull(f, b)
	return n == 2 && b[0] == '#' && b[1] == '!'
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode()&0o111 != 0
}

// Run 执行短命令并等待结束。ctx 取消或超时都会终止整个进程组。
func Run(ctx context.Context, s Spec) Result {
	start := time.Now()
	cmd := command(s)
	var stdout capBuffer
	tail := &Ring{}
	cmd.Stdout = io.MultiWriter(&stdout, tail)
	cmd.Stderr = tail
	cmd.WaitDelay = IOGrace
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: 127, Err: err, Duration: time.Since(start)}
	}
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	timedOut := false
	select {
	case err = <-done:
	case <-ctx.Done():
		timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		pid := cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		select {
		case err = <-done:
		case <-time.After(KillGrace):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			err = <-done
		}
	}
	r := Result{
		Stdout:   strings.TrimSpace(stdout.String()),
		Output:   tail.String(),
		Duration: time.Since(start),
		TimedOut: timedOut,
	}
	switch {
	case timedOut:
		r.ExitCode = -1
	case err == nil:
		r.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			r.ExitCode = exitErr.ExitCode()
		} else if errors.Is(err, exec.ErrWaitDelay) {
			r.ExitCode = cmd.ProcessState.ExitCode() // 正常退出，只是后台子进程还占着管道
		} else {
			r.ExitCode, r.Err = 127, err
		}
	}
	return r
}

// Proc 是一个常驻进程（daemon）。
type Proc struct {
	Pid     int
	Started time.Time
	Output  *Ring

	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	exit Exit
}

// Exit 描述常驻进程的结束方式。
type Exit struct {
	Code     int  // 被信号终止时为 -1
	Signaled bool // 被信号终止（包括我们发出的 SIGTERM）
	Stopped  bool // 由 Stop 结束
}

// Start 启动常驻进程，不等待结束。
func Start(s Spec) (*Proc, error) {
	cmd := command(s)
	ring := &Ring{}
	cmd.Stdout, cmd.Stderr = ring, ring
	cmd.WaitDelay = IOGrace
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Proc{Pid: cmd.Process.Pid, Started: time.Now(), Output: ring, cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			p.exit.Code, p.exit.Signaled = -1, true
		} else {
			p.exit.Code = cmd.ProcessState.ExitCode()
		}
		_ = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

// Done 在进程结束后关闭。
func (p *Proc) Done() <-chan struct{} { return p.done }

// Exit 返回结束方式；进程未结束时结果无意义。
func (p *Proc) Exit() Exit {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exit
}

// Running 报告进程是否还在运行。
func (p *Proc) Running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Stop 终止整个进程组并等待结束（最多 KillGrace 后强杀）。
func (p *Proc) Stop() {
	p.mu.Lock()
	p.exit.Stopped = true
	p.mu.Unlock()
	if !p.Running() {
		return
	}
	_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(KillGrace):
		_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
		<-p.done
	}
}

// ───────── 输出缓冲 ─────────

// capBuffer 只保留前 MaxOutput 字节（status 的输出只需要开头）。
type capBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := MaxOutput - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (c *capBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Ring 只保留最后 MaxOutput 字节，并发安全。
type Ring struct {
	mu  sync.Mutex
	buf []byte
}

func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if over := len(r.buf) - MaxOutput; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	return len(p), nil
}

func (r *Ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

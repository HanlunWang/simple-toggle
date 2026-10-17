package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/runner"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// 工作台用到的命令：立即检查状态、读取常驻进程输出、运行记录、单开关设置、从脚本库安装。

var (
	ErrNoStatus = errors.New("only toggle switches have a status branch")
	ErrExists   = errors.New("a script with this file name already exists")
)

// Status 立即执行一次 `<script> status`，按结果更新状态并返回本次执行的结果。
// 与轮询不同，它会等待正在进行的 on / off 结束，保证读到的是操作之后的状态。
func (e *Engine) Status(id string) (runner.Result, error) {
	sw, r, err := e.lookup(id)
	if err != nil {
		return runner.Result{}, err
	}
	if sw.Header.Type != contract.Toggle {
		return runner.Result{}, ErrNoStatus
	}
	r.op.Lock()
	defer r.op.Unlock()
	res := runner.Run(context.Background(), e.spec(sw, Manual, "status"))
	st := Unknown
	if res.ExitCode == 0 {
		st = Off
		if res.Stdout == "on" {
			st = On
		}
	}
	e.setState(sw.ID, st, time.Now())
	return res, nil
}

// DaemonOutput 返回常驻进程到目前为止的输出（末尾 64 KB）；进程不在运行时 ok=false。
func (e *Engine) DaemonOutput(id string) (out string, ok bool) {
	id, found := e.Resolve(id)
	if !found {
		return "", false
	}
	e.mu.Lock()
	r := e.rts[id]
	var p *runner.Proc
	if r != nil {
		p = r.daemon
	}
	e.mu.Unlock()
	if p == nil {
		return "", false
	}
	return p.Output.String(), true
}

// Runs 按时间倒序返回运行记录。
func (e *Engine) Runs(f store.Filter) []store.Run { return e.cfg.Store.Runs(f) }

// ClearRuns 清空运行记录。
func (e *Engine) ClearRuns() error { return e.cfg.Store.ClearRuns() }

// SetMeta 保存「用户怎么用这个开关」的设置（分组、排序、确认、超时等）。
func (e *Engine) SetMeta(id string, m store.Meta) error {
	sw, _, err := e.lookup(id)
	if err != nil {
		return err
	}
	if err := e.cfg.Store.SetMeta(sw.ID, m); err != nil {
		return err
	}
	e.emitSwitch(EventState, sw.ID)
	return nil
}

// InstallScript 把脚本库里的脚本按原文件名写进脚本目录，并确保它在面板中可见。
// 同名文件已存在时返回 ErrExists，不覆盖用户的版本。
func (e *Engine) InstallScript(file, content string) error {
	if file != filepath.Base(file) || filepath.Ext(file) == "" {
		return errors.New("invalid script file name")
	}
	issues := contract.Lint(content)
	if contract.HasErrors(issues) {
		return &LintError{issues}
	}
	path := filepath.Join(e.cfg.ScriptsDir, file)
	if _, err := os.Lstat(path); err == nil {
		return ErrExists
	}
	if err := os.MkdirAll(e.cfg.ScriptsDir, 0o755); err != nil {
		return err
	}
	if err := writeScript(path, content); err != nil {
		return err
	}
	_ = e.cfg.Store.SetHidden(file, false)
	_ = e.cfg.Store.MarkSeeded(file)
	e.reload()
	e.kick()
	return nil
}

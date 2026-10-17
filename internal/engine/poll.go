package engine

import (
	"context"
	"sync"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/runner"
)

// 同时执行的 status 检查上限：脚本各自起子进程（networksetup、osascript 都不便宜）。
const pollParallelism = 4

// PanelVisible 告诉引擎面板（或管理窗口）是否可见：可见时轮询更频繁，
// 打开的瞬间立即刷新一次（与 v1 的自适应轮询一致）。
func (e *Engine) PanelVisible(visible bool) {
	e.mu.Lock()
	if visible {
		e.visible++
	} else if e.visible > 0 {
		e.visible--
	}
	e.mu.Unlock()
	if visible {
		e.kick()
	}
}

// Refresh 立即检查一次全部状态。
func (e *Engine) Refresh() { e.kick() }

func (e *Engine) kick() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) interval() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.visible > 0 {
		return e.cfg.PollVisible
	}
	return e.cfg.PollHidden
}

func (e *Engine) loop() {
	defer e.wg.Done()
	e.refresh()
	var watch <-chan time.Time
	if e.cfg.WatchInterval > 0 {
		t := time.NewTicker(e.cfg.WatchInterval)
		defer t.Stop()
		watch = t.C
	}
	for {
		timer := time.NewTimer(e.interval())
		select {
		case <-e.stopCh:
			timer.Stop()
			return
		case <-e.wake:
			timer.Stop()
			e.refresh()
		case <-timer.C:
			e.refresh()
		case <-watch:
			timer.Stop()
			e.mu.Lock()
			changed := e.signature() != e.dirSig
			e.mu.Unlock()
			if changed {
				e.reload()
				e.refresh()
			}
		}
	}
}

// refresh 检查需要轮询的开关：可见的，以及开着或 daemon 存活的隐藏开关。
// 检查期间如果发生了用户操作（世代号变化），整批结果作废，下一轮会校正。
func (e *Engine) refresh() {
	e.mu.Lock()
	if e.polling {
		e.mu.Unlock()
		return
	}
	e.polling = true
	gen := e.generation
	type job struct {
		sw *Switch
		r  *rt
	}
	var jobs []job
	for id, sw := range e.switches {
		r := e.rts[id]
		if sw.Header.Type == contract.Action {
			continue
		}
		if e.cfg.Store.Hidden(id) && r.state != On && r.daemon == nil {
			continue
		}
		jobs = append(jobs, job{sw, r})
	}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.polling = false
		e.mu.Unlock()
	}()

	results := map[string]State{}
	var mu sync.Mutex
	sem := make(chan struct{}, pollParallelism)
	var wg sync.WaitGroup
	for _, j := range jobs {
		if j.sw.Header.Type == contract.Daemon {
			e.mu.Lock()
			alive := j.r.daemon != nil && j.r.daemon.Running()
			e.mu.Unlock()
			st := Off
			if alive {
				st = On
			}
			mu.Lock()
			results[j.sw.ID] = st
			mu.Unlock()
			continue
		}
		// 正在执行 on / off 的开关跳过，结果由那次操作给出
		if !j.r.op.TryLock() {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			defer j.r.op.Unlock()
			res := runner.Run(context.Background(), e.spec(j.sw, "poll", "status"))
			st := Unknown
			if res.ExitCode == 0 {
				st = Off
				if res.Stdout == "on" {
					st = On
				}
			}
			mu.Lock()
			results[j.sw.ID] = st
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	e.mu.Lock()
	stale := e.generation != gen
	e.mu.Unlock()
	if stale {
		return
	}
	now := time.Now()
	for id, st := range results {
		e.mu.Lock()
		r := e.rts[id]
		skip := r == nil || r.state == Pending || (r.state == Error && st == Off)
		e.mu.Unlock()
		if skip {
			continue // daemon 的 error（意外退出）由监管协程标记，轮询不覆盖
		}
		e.setState(id, st, now)
	}
}

package engine

import (
	"time"

	"github.com/HanlunWang/open-toggle/internal/store"
)

// EventKind 是事件类型。
type EventKind string

const (
	EventState    EventKind = "state"    // 开关状态、可见性或参数变化
	EventRun      EventKind = "run"      // 一次执行结束，带运行记录
	EventReloaded EventKind = "reloaded" // 脚本目录重新扫描
)

// Event 是引擎发出的事件。View 在 EventState 时有值。
type Event struct {
	Kind   EventKind
	Switch string
	View   *View
	Run    *store.Run
}

// Subscribe 订阅事件。缓冲满时丢弃最旧的事件（订阅者应当只把事件当作「该刷新了」的提示）。
// 返回的函数取消订阅。
func (e *Engine) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	e.subMu.Lock()
	id := e.nextID
	e.nextID++
	e.subs[id] = ch
	e.subMu.Unlock()
	return ch, func() {
		e.subMu.Lock()
		if c, ok := e.subs[id]; ok {
			delete(e.subs, id)
			close(c)
		}
		e.subMu.Unlock()
	}
}

func (e *Engine) emit(ev Event) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	for _, ch := range e.subs {
		select {
		case ch <- ev:
		default:
			select { // 满了：丢最旧的一条再放
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

func (e *Engine) emitSwitch(kind EventKind, id string) {
	e.mu.Lock()
	if _, ok := e.switches[id]; !ok {
		e.mu.Unlock()
		return
	}
	v := e.viewLocked(id)
	e.mu.Unlock()
	e.emit(Event{Kind: kind, Switch: id, View: &v})
}

// Wait 等 done 关闭，最多 d；返回是否在时限内完成。
func Wait(done <-chan struct{}, d time.Duration) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

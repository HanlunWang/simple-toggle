package engine

import (
	"time"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// ParamView 是一个参数及其当前值。
type ParamView struct {
	contract.Param
	Value string
}

// View 是开关在某一时刻的快照，供界面与 API 使用。
type View struct {
	ID          string
	Name        string
	Icon        string
	Type        contract.Type
	Description string
	State       State
	Hidden      bool
	Params      []ParamView
	MenuBar     *contract.MenuBar
	ActivatedAt time.Time // 开着时有值
	Remaining   *int      // 倒计时剩余秒数
	RunningFor  time.Duration
	LastRun     *store.Run
	Meta        store.Meta
}

// V1State 把内部状态映射为 v1 API 的四种状态之一（on / off / error / unknown）。
func (v View) V1State() string {
	switch v.State {
	case On, Off, Error:
		return string(v.State)
	case Idle:
		return string(Off)
	case Running:
		return string(On)
	}
	return string(Unknown)
}

func (e *Engine) viewLocked(id string) View {
	sw, r := e.switches[id], e.rts[id]
	v := View{
		ID: sw.ID, Name: sw.Header.Name, Icon: sw.Header.Icon, Type: sw.Header.Type,
		Description: sw.Header.Description, MenuBar: sw.Header.MenuBar,
		State: r.state, ActivatedAt: r.activatedAt, LastRun: r.lastRun,
		Hidden: e.cfg.Store.Hidden(id), Meta: e.cfg.Store.Meta(id),
	}
	for _, p := range sw.Header.Params {
		v.Params = append(v.Params, ParamView{Param: p, Value: e.paramValue(sw, p)})
	}
	v.Remaining = e.remainingLocked(sw, r)
	if !r.busySince.IsZero() {
		v.RunningFor = time.Since(r.busySince)
	}
	// 重启后第一次需要时从记录里找一次，结果记在 rt 上：不必每次 List 都扫一遍几千条记录
	if v.LastRun == nil && !r.lastRunLoaded {
		r.lastRunLoaded = true
		if runs := e.cfg.Store.Runs(store.Filter{Switch: id, Limit: 1}); len(runs) > 0 {
			r.lastRun = &runs[0]
			v.LastRun = r.lastRun
		}
	}
	return v
}

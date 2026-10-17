package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// 场景：按顺序执行的一组步骤。激活时记下各成员激活前的状态，开启「恢复」时再点一次
// 就按记录恢复。「激活中」是推导出来的：所有开关与常驻成员仍处于目标状态时才算；
// 用户改动了任何一个成员，场景就自动退出激活状态。

// SceneStepGap 是两步之间的间隔。
const SceneStepGap = 120 * time.Millisecond

var (
	ErrNoScene    = errors.New("no such scene")
	ErrSceneEmpty = errors.New("scene has no steps")
)

type sceneState struct {
	prev   map[string]bool // 激活前：成员是否开着
	target map[string]bool // 场景要求的状态
}

// SceneView 是场景及其当前状态。
type SceneView struct {
	store.Scene
	Active bool
}

// SceneRunID 是场景的运行记录使用的「开关 id」。
func SceneRunID(id string) string { return "scene:" + id }

// Scenes 返回全部场景与激活状态。
func (e *Engine) Scenes() []SceneView {
	scenes := e.cfg.Store.Scenes()
	out := make([]SceneView, len(scenes))
	for i, sc := range scenes {
		out[i] = SceneView{Scene: sc, Active: e.sceneActive(sc.ID)}
	}
	return out
}

// Scene 按 id 或名称（不区分大小写）找场景。
func (e *Engine) Scene(idOrName string) (SceneView, bool) {
	for _, sv := range e.Scenes() {
		if sv.ID == idOrName || strings.EqualFold(sv.Name, idOrName) {
			return sv, true
		}
	}
	return SceneView{}, false
}

// sceneActive 检查记录的目标状态是否仍然成立；不成立时丢掉记录。
func (e *Engine) sceneActive(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.sceneState[id]
	if st == nil {
		return false
	}
	for sw, want := range st.target {
		r := e.rts[sw]
		if r == nil {
			continue // 成员被删除：不影响其余成员
		}
		switch r.state {
		case Pending, Unknown:
			continue // 正在切换或还没读到：暂不判定
		case On:
			if !want {
				delete(e.sceneState, id)
				return false
			}
		default:
			if want {
				delete(e.sceneState, id)
				return false
			}
		}
	}
	return true
}

// SaveScene 新增或修改场景；id 为空时按名称生成。
func (e *Engine) SaveScene(sc store.Scene) (store.Scene, error) {
	sc.Name = strings.TrimSpace(sc.Name)
	if sc.Name == "" {
		return sc, errors.New("scene name is required")
	}
	if sc.ID == "" {
		base := strings.TrimSuffix(contract.Slug(sc.Name), ".sh")
		sc.ID = base
		for n := 2; ; n++ {
			if _, taken := e.cfg.Store.Scene(sc.ID); !taken {
				break
			}
			sc.ID = fmt.Sprintf("%s-%d", base, n)
		}
	}
	for i := range sc.Steps {
		switch sc.Steps[i].Action {
		case "on", "off", "run":
		default:
			return sc, fmt.Errorf("step %d: action must be on, off or run", i+1)
		}
	}
	if err := e.cfg.Store.PutScene(sc); err != nil {
		return sc, err
	}
	e.emit(Event{Kind: EventScene, Switch: sc.ID})
	return sc, nil
}

// DeleteScene 删除场景（不改动任何开关）。
func (e *Engine) DeleteScene(id string) error {
	if err := e.cfg.Store.DeleteScene(id); err != nil {
		return ErrNoScene
	}
	e.mu.Lock()
	delete(e.sceneState, id)
	e.mu.Unlock()
	e.emit(Event{Kind: EventScene, Switch: id})
	return nil
}

// ToggleScene 是面板与快捷键的行为：激活中且开启了恢复时恢复，否则激活。
func (e *Engine) ToggleScene(id string, src Source, detail string) (<-chan store.Run, error) {
	sv, ok := e.Scene(id)
	if !ok {
		return nil, ErrNoScene
	}
	if sv.Active && sv.Restore {
		return e.RestoreScene(sv.ID, src, detail)
	}
	return e.ActivateScene(sv.ID, src, detail)
}

// ActivateScene 按顺序执行场景的步骤，立即返回；完成后通过通道与运行记录给出结果。
func (e *Engine) ActivateScene(id string, src Source, detail string) (<-chan store.Run, error) {
	sv, ok := e.Scene(id)
	if !ok {
		return nil, ErrNoScene
	}
	sc := sv.Scene
	if len(sc.Steps) == 0 {
		return nil, ErrSceneEmpty
	}
	out := make(chan store.Run, 1)
	go func() {
		e.sceneOp.Lock()
		defer e.sceneOp.Unlock()
		start := time.Now()
		st := &sceneState{prev: map[string]bool{}, target: map[string]bool{}}
		var lines []string
		ok := true
		for i, step := range sc.Steps {
			if i > 0 {
				time.Sleep(SceneStepGap)
			}
			line, good := e.runStep(sc, step, st, src)
			lines = append(lines, line)
			ok = ok && good
		}
		e.mu.Lock()
		if len(st.target) > 0 {
			e.sceneState[sc.ID] = st
		}
		e.mu.Unlock()
		out <- e.recordScene(sc, "scene", src, detail, lines, ok, start)
	}()
	return out, nil
}

// runStep 执行一步并返回一行结果。
func (e *Engine) runStep(sc store.Scene, step store.SceneStep, st *sceneState, src Source) (string, bool) {
	sw, r, err := e.lookup(step.Switch)
	if err != nil {
		return "✗ " + step.Switch + ": not found", false
	}
	label := sw.ID + " " + step.Action
	if step.Action == "run" || sw.Header.Type == contract.Action {
		ch, err := e.Run(sw.ID, Scene, sc.Name)
		if err != nil {
			return "✗ " + label + ": " + err.Error(), false
		}
		select {
		case rec := <-ch:
			if !rec.OK {
				return fmt.Sprintf("✗ %s: exit %d", label, rec.ExitCode), false
			}
			return "✓ " + label, true
		case <-time.After(e.stepTimeout()):
			return "✗ " + label + ": timed out", false
		}
	}
	want := step.Action == "on"
	e.mu.Lock()
	wasOn := r.state == On
	e.mu.Unlock()
	st.prev[sw.ID] = wasOn
	st.target[sw.ID] = want
	if IsUser(src) {
		e.markUser(r, Manual)
	}
	if wasOn == want {
		return "= " + label, true
	}
	done, err := e.SetSwitch(sw.ID, want, Scene, sc.Name)
	if err != nil {
		return "✗ " + label + ": " + err.Error(), false
	}
	if !Wait(done, e.stepTimeout()) {
		return "✗ " + label + ": timed out", false
	}
	if v, _ := e.Get(sw.ID); (v.State == On) != want {
		return "✗ " + label + ": state is " + string(v.State), false
	}
	return "✓ " + label, true
}

// RestoreScene 把场景成员恢复到激活前的状态（倒序执行），并结束激活状态。
func (e *Engine) RestoreScene(id string, src Source, detail string) (<-chan store.Run, error) {
	sv, ok := e.Scene(id)
	if !ok {
		return nil, ErrNoScene
	}
	e.mu.Lock()
	st := e.sceneState[sv.ID]
	delete(e.sceneState, sv.ID)
	e.mu.Unlock()
	out := make(chan store.Run, 1)
	go func() {
		e.sceneOp.Lock()
		defer e.sceneOp.Unlock()
		start := time.Now()
		var lines []string
		ok := true
		if st != nil {
			for i := len(sv.Steps) - 1; i >= 0; i-- {
				id := sv.Steps[i].Switch
				was, recorded := st.prev[id]
				if !recorded {
					continue
				}
				delete(st.prev, id) // 同一个开关出现多次时只恢复一次
				label := fmt.Sprintf("%s %s", id, map[bool]string{true: "on", false: "off"}[was])
				v, found := e.Get(id)
				if !found {
					lines = append(lines, "✗ "+id+": not found")
					ok = false
					continue
				}
				if (v.State == On) == was {
					lines = append(lines, "= "+label)
					continue
				}
				done, err := e.SetSwitch(id, was, Scene, sv.Name)
				if err != nil || !Wait(done, e.stepTimeout()) {
					lines = append(lines, "✗ "+label)
					ok = false
					continue
				}
				lines = append(lines, "✓ "+label)
				time.Sleep(SceneStepGap)
			}
		}
		out <- e.recordScene(sv.Scene, "restore", src, detail, lines, ok, start)
	}()
	return out, nil
}

func (e *Engine) recordScene(sc store.Scene, action string, src Source, detail string, lines []string, ok bool, start time.Time) store.Run {
	code := 0
	if !ok {
		code = 1
	}
	rec, _ := e.cfg.Store.AppendRun(store.Run{
		Switch: SceneRunID(sc.ID), Name: sc.Name, Action: action, Source: string(src), Detail: detail,
		Duration: time.Since(start).Milliseconds(), ExitCode: code, OK: ok, Output: strings.Join(lines, "\n"),
	})
	e.emit(Event{Kind: EventRun, Switch: SceneRunID(sc.ID), Run: &rec})
	e.emit(Event{Kind: EventScene, Switch: sc.ID})
	return rec
}

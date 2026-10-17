package app

import (
	"errors"
	"strconv"
	"time"

	"github.com/HanlunWang/open-toggle/internal/automation"
	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/runner"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// fakeBackend 的工作台部分：脚本保存在内存里，解析后同步到 views。

func (f *fakeBackend) Script(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.scripts[id]; ok {
		return s, nil
	}
	return "", engine.ErrNotFound
}

func (f *fakeBackend) setScriptLocked(id, content string) {
	if f.scripts == nil {
		f.scripts = map[string]string{}
	}
	f.scripts[id] = content
	h, _ := contract.Parse(content)
	for i := range f.views {
		if f.views[i].ID == id {
			f.views[i].Name, f.views[i].Icon, f.views[i].Type = h.Name, h.Icon, h.Type
			return
		}
	}
	f.views = append(f.views, engine.View{ID: id, Name: h.Name, Icon: h.Icon, Type: h.Type, State: engine.Off})
}

func (f *fakeBackend) UpdateScript(id, content string, src engine.Source) ([]contract.Issue, error) {
	issues := contract.Lint(content)
	if contract.HasErrors(issues) {
		return issues, &engine.LintError{Issues: issues}
	}
	f.log("save " + id)
	f.mu.Lock()
	f.setScriptLocked(id, content)
	f.mu.Unlock()
	return issues, nil
}

func (f *fakeBackend) CreateScript(content string) (string, []contract.Issue, error) {
	h, _ := contract.Parse(content)
	id := contract.Slug(h.Name)
	f.log("create " + id)
	f.mu.Lock()
	f.setScriptLocked(id, content)
	f.mu.Unlock()
	return id, nil, nil
}

func (f *fakeBackend) DeleteScript(id string) error {
	f.log("delete " + id)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.scripts, id)
	for i := range f.views {
		if f.views[i].ID == id {
			f.views = append(f.views[:i], f.views[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeBackend) InstallScript(file, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.scripts[file]; ok {
		return engine.ErrExists
	}
	f.calls = append(f.calls, "install "+file)
	f.setScriptLocked(file, content)
	return nil
}

func (f *fakeBackend) Status(id string) (runner.Result, error) {
	f.log("status " + id)
	return runner.Result{ExitCode: 0, Stdout: "off", Output: "off\n", Duration: 12 * time.Millisecond}, nil
}

func (f *fakeBackend) DaemonOutput(string) (string, bool) { return "", false }

func (f *fakeBackend) Runs(flt store.Filter) []store.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Run
	for i := len(f.runs) - 1; i >= 0; i-- {
		r := f.runs[i]
		if flt.Switch != "" && r.Switch != flt.Switch {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (f *fakeBackend) ClearRuns() error {
	f.log("clear runs")
	f.mu.Lock()
	f.runs = nil
	f.mu.Unlock()
	return nil
}

func (f *fakeBackend) SetMeta(id string, m store.Meta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.views {
		if f.views[i].ID == id {
			f.views[i].Meta = m
			f.calls = append(f.calls, "meta "+id+" group="+m.Group)
			return nil
		}
	}
	return errors.New("no such switch")
}

// ───────── 场景 ─────────

func (f *fakeBackend) Scenes() []engine.SceneView {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]engine.SceneView(nil), f.scenes...)
}

func (f *fakeBackend) SaveScene(sc store.Scene) (store.Scene, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sc.ID == "" {
		sc.ID = "scene-" + strconv.Itoa(len(f.scenes)+1)
	}
	f.calls = append(f.calls, "save-scene "+sc.ID+" "+sc.Name)
	for i := range f.scenes {
		if f.scenes[i].ID == sc.ID {
			f.scenes[i].Scene = sc
			return sc, nil
		}
	}
	f.scenes = append(f.scenes, engine.SceneView{Scene: sc})
	return sc, nil
}

func (f *fakeBackend) DeleteScene(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete-scene "+id)
	for i := range f.scenes {
		if f.scenes[i].ID == id {
			f.scenes = append(f.scenes[:i], f.scenes[i+1:]...)
			return nil
		}
	}
	return engine.ErrNoScene
}

func (f *fakeBackend) ToggleScene(id string, src engine.Source, detail string) (<-chan store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "toggle-scene "+id)
	for i := range f.scenes {
		if f.scenes[i].ID == id {
			f.scenes[i].Active = !f.scenes[i].Active
		}
	}
	ch := make(chan store.Run, 1)
	ch <- store.Run{OK: true}
	return ch, nil
}

func (f *fakeBackend) ActivateScene(id string, src engine.Source, detail string) (<-chan store.Run, error) {
	return f.ToggleScene(id, src, detail)
}

// ───────── 自动化 ─────────

type fakeAuto struct {
	rules    []store.Rule
	issues   map[string]automation.ShortcutIssue
	paused   time.Time
	isPaused bool
	calls    []string
}

func (a *fakeAuto) Rules() []store.Rule { return append([]store.Rule(nil), a.rules...) }
func (a *fakeAuto) SaveRule(r store.Rule) (store.Rule, error) {
	if err := automation.Validate(r); err != nil {
		return r, err
	}
	if r.ID == "" {
		r.ID = "rule-" + strconv.Itoa(len(a.rules)+1)
	}
	a.calls = append(a.calls, "save-rule "+r.ID)
	for i := range a.rules {
		if a.rules[i].ID == r.ID {
			a.rules[i] = r
			return r, nil
		}
	}
	a.rules = append(a.rules, r)
	return r, nil
}
func (a *fakeAuto) DeleteRule(id string) error {
	a.calls = append(a.calls, "delete-rule "+id)
	for i := range a.rules {
		if a.rules[i].ID == id {
			a.rules = append(a.rules[:i], a.rules[i+1:]...)
		}
	}
	return nil
}
func (a *fakeAuto) Test(id string) error            { a.calls = append(a.calls, "test "+id); return nil }
func (a *fakeAuto) Paused() (bool, time.Time)       { return a.isPaused, a.paused }
func (a *fakeAuto) Pause(until time.Time) error     { a.isPaused, a.paused = true, until; return nil }
func (a *fakeAuto) Resume() error                   { a.isPaused = false; return nil }
func (a *fakeAuto) CurrentNetwork() (string, error) { return "net-key", nil }
func (a *fakeAuto) Reload()                         { a.calls = append(a.calls, "reload") }
func (a *fakeAuto) ShortcutIssues() map[string]automation.ShortcutIssue {
	return a.issues
}

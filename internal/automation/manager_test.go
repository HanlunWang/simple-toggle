package automation

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/platform/darwin"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// ───────── 替身 ─────────

type fakeEngine struct {
	mu     sync.Mutex
	views  map[string]*engine.View
	scenes []engine.SceneView
	user   map[string]time.Time
	calls  []string
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{views: map[string]*engine.View{
		"lamp.sh": {ID: "lamp.sh", Name: "Lamp", Type: contract.Toggle, State: engine.Off},
		"ping.sh": {ID: "ping.sh", Name: "Ping", Type: contract.Action, State: engine.Idle},
	}, user: map[string]time.Time{}}
}

func (f *fakeEngine) log(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeEngine) SetSwitch(id string, on bool, src engine.Source, detail string) (<-chan struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.views[id]
	if !ok {
		return nil, engine.ErrNotFound
	}
	v.State = map[bool]engine.State{true: engine.On, false: engine.Off}[on]
	f.calls = append(f.calls, "set "+id+" "+string(v.State)+" "+string(src))
	ch := make(chan struct{})
	close(ch)
	return ch, nil
}
func (f *fakeEngine) Run(id string, src engine.Source, detail string) (<-chan store.Run, error) {
	f.log("run " + id + " " + string(src))
	ch := make(chan store.Run, 1)
	ch <- store.Run{OK: true}
	return ch, nil
}
func (f *fakeEngine) ActivateScene(id string, src engine.Source, detail string) (<-chan store.Run, error) {
	f.log("scene " + id + " " + string(src))
	return make(chan store.Run, 1), nil
}
func (f *fakeEngine) ToggleScene(id string, src engine.Source, detail string) (<-chan store.Run, error) {
	f.log("toggle-scene " + id + " " + string(src))
	return make(chan store.Run, 1), nil
}
func (f *fakeEngine) Get(id string) (engine.View, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.views[id]; ok {
		return *v, true
	}
	return engine.View{}, false
}
func (f *fakeEngine) Scenes() []engine.SceneView { return f.scenes }
func (f *fakeEngine) List() []engine.View {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []engine.View
	for _, v := range f.views {
		out = append(out, *v)
	}
	return out
}
func (f *fakeEngine) UserChangedAt(id string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.user[id]
}
func (f *fakeEngine) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

type fakeHost struct {
	keys    map[string]func()
	taken   map[string]bool
	watches map[string]func()
	notes   []string
}

func (h *fakeHost) RegisterShortcut(a string, fn func()) error {
	if h.taken[a] {
		return errors.New("taken")
	}
	h.keys[a] = fn
	return nil
}
func (h *fakeHost) UnregisterShortcut(a string) { delete(h.keys, a) }
func (h *fakeHost) Watch(p string, fn func()) (func(), error) {
	h.watches[p] = fn
	return func() { delete(h.watches, p) }, nil
}
func (h *fakeHost) Notify(_, body string) { h.notes = append(h.notes, body) }

type rig struct {
	m     *Manager
	e     *fakeEngine
	h     *fakeHost
	st    *store.Store
	now   time.Time
	power darwin.Power
	apps  []darwin.App
	net   string
}

// 2026-10-19 是星期一
func newRig(t *testing.T, rules ...store.Rule) *rig {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{e: newFakeEngine(), h: &fakeHost{keys: map[string]func(){}, taken: map[string]bool{}, watches: map[string]func(){}},
		st: st, now: time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local), power: darwin.Power{OnAC: true, HasBattery: true, Percent: 80}, net: "home"}
	r.m = New(Config{
		Engine: r.e, Store: st, Host: r.h, Now: func() time.Time { return r.now },
		Platform: Platform{
			Power:   func() (darwin.Power, error) { return r.power, nil },
			Apps:    func() ([]darwin.App, error) { return r.apps, nil },
			Network: func() (string, error) { return r.net, nil },
		},
	})
	for _, rule := range rules {
		if _, err := r.m.SaveRule(rule); err != nil {
			t.Fatal(err)
		}
	}
	r.m.apply()
	r.m.poll(true)
	r.e.took()
	return r
}

// drain 处理队列里的事件（快捷键、文件监听送进来的）。
func (r *rig) drain() {
	for {
		select {
		case ev := <-r.m.events:
			r.m.handle(ev)
		default:
			return
		}
	}
}

func (r *rig) advance(d time.Duration) { r.now = r.now.Add(d) }

func rule(kind, event, value, verb, target string) store.Rule {
	return store.Rule{Enabled: true, Trigger: store.Trigger{Kind: kind, Event: event, Value: value}, Action: store.RuleAction{Verb: verb, Target: target}}
}

func expect(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("calls %q, want %q", got, want)
	}
}

// ───────── 纯函数 ─────────

func TestNormalizeShortcut(t *testing.T) {
	cases := map[string]string{"Ctrl+Alt+P": "ctrl+alt+p", "option+command+1": "cmd+alt+1", "shift+ctrl+K": "ctrl+shift+k", "f13": "f13", "": ""}
	for in, want := range cases {
		if got, err := NormalizeShortcut(in); err != nil || got != want {
			t.Errorf("%q → %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"p", "shift+p", "ctrl+", "ctrl+shift", "hyper+p"} {
		if _, err := NormalizeShortcut(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if s := ShortcutSymbols("ctrl+alt+shift+space"); s != "⌃⌥⇧Space" {
		t.Errorf("symbols %q", s)
	}
}

func TestConditionMet(t *testing.T) {
	mon9 := time.Date(2026, 10, 19, 9, 30, 0, 0, time.Local)
	sun23 := time.Date(2026, 10, 18, 23, 30, 0, 0, time.Local)
	cases := []struct {
		c    *store.Condition
		at   time.Time
		want bool
	}{
		{nil, mon9, true},
		{&store.Condition{Days: []int{1, 2, 3, 4, 5}}, mon9, true},
		{&store.Condition{Days: []int{1, 2, 3, 4, 5}}, sun23, false},
		{&store.Condition{From: "09:00", To: "18:00"}, mon9, true},
		{&store.Condition{From: "10:00", To: "18:00"}, mon9, false},
		{&store.Condition{From: "22:00", To: "07:00"}, sun23, true}, // 跨午夜
		{&store.Condition{From: "22:00", To: "07:00"}, mon9, false},
	}
	for i, c := range cases {
		if got := ConditionMet(c.c, c.at); got != c.want {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
}

func TestValidateAndSave(t *testing.T) {
	r := newRig(t)
	if _, err := r.m.SaveRule(rule(KindTime, "", "25:00", "on", "lamp.sh")); err == nil {
		t.Fatal("bad time accepted")
	}
	if _, err := r.m.SaveRule(rule(KindPower, "sideways", "", "on", "lamp.sh")); err == nil {
		t.Fatal("bad power event accepted")
	}
	saved, err := r.m.SaveRule(rule(KindShortcut, "", "Option+Ctrl+L", "toggle", "lamp.sh"))
	if err != nil || saved.ID == "" || saved.Trigger.Value != "ctrl+alt+l" {
		t.Fatalf("save: %+v %v", saved, err)
	}
	// 重新启用会清掉自动停用的原因
	r.st.Suspend(saved.ID, SuspendFlapping)
	saved.Enabled = true
	saved.Suspended = ""
	again, _ := r.m.SaveRule(saved)
	if again.Suspended != "" {
		t.Fatal("suspension not cleared on re-enable")
	}
}

// ───────── 八类触发器 ─────────

func TestTimeTrigger(t *testing.T) {
	tr := rule(KindTime, "", "18:30", "on", "lamp.sh")
	tr.Trigger.Days = []int{1, 2, 3, 4, 5}
	r := newRig(t, tr)
	r.m.tick(time.Date(2026, 10, 19, 18, 29, 59, 0, time.Local))
	expect(t, r.e.took())
	r.now = time.Date(2026, 10, 19, 18, 30, 0, 0, time.Local)
	r.m.tick(r.now)
	r.m.tick(r.now.Add(20 * time.Second)) // 同一分钟只触发一次
	expect(t, r.e.took(), "set lamp.sh on automation")
	r.e.SetSwitch("lamp.sh", false, engine.Manual, "")
	r.e.took()
	r.now = time.Date(2026, 10, 18, 18, 30, 0, 0, time.Local) // 星期日
	r.m.tick(r.now)
	expect(t, r.e.took())
	if rr, _ := r.st.Rule("rule-1"); rr.LastFired.IsZero() {
		t.Fatal("last fired not recorded")
	}
}

func TestWiFiTrigger(t *testing.T) {
	r := newRig(t, rule(KindWiFi, "join", "office", "on", "lamp.sh"), rule(KindWiFi, "leave", "office", "off", "lamp.sh"))
	r.net = "office"
	r.m.handle(Event{Kind: "network"}) // 界面层的「网络变了」提示
	expect(t, r.e.took(), "set lamp.sh on automation")
	r.net = "home"
	r.advance(time.Minute)
	r.m.poll(true)
	expect(t, r.e.took(), "set lamp.sh off automation")
}

func TestPowerTrigger(t *testing.T) {
	low := rule(KindPower, "low", "20", "run", "ping.sh")
	r := newRig(t, rule(KindPower, "unplug", "", "on", "lamp.sh"), rule(KindPower, "plug", "", "off", "lamp.sh"), low)
	r.power = darwin.Power{OnAC: false, HasBattery: true, Percent: 80}
	r.m.poll(true)
	expect(t, r.e.took(), "set lamp.sh on automation")
	r.power.Percent = 21
	r.advance(time.Minute)
	r.m.poll(true)
	expect(t, r.e.took())
	r.power.Percent = 19 // 跌破 20%
	r.advance(time.Minute)
	r.m.poll(true)
	r.power.Percent = 17 // 继续下降不再触发
	r.advance(time.Minute)
	r.m.poll(true)
	expect(t, r.e.took(), "run ping.sh automation")
	r.power.OnAC = true
	r.advance(time.Minute)
	r.m.poll(true)
	expect(t, r.e.took(), "set lamp.sh off automation")
}

func TestAppTrigger(t *testing.T) {
	r := newRig(t, rule(KindApp, "launch", "com.apple.Keynote", "on", "lamp.sh"), rule(KindApp, "quit", "com.apple.keynote", "off", "lamp.sh"))
	r.apps = []darwin.App{{BundleID: "com.apple.Keynote", Name: "Keynote"}}
	r.advance(5 * time.Second)
	r.m.poll(false)
	expect(t, r.e.took(), "set lamp.sh on automation")
	r.apps = nil
	r.advance(5 * time.Second)
	r.m.poll(false)
	expect(t, r.e.took(), "set lamp.sh off automation")
}

func TestDisplayTrigger(t *testing.T) {
	r := newRig(t, rule(KindDisplay, "connect", "", "on", "lamp.sh"), rule(KindDisplay, "disconnect", "", "off", "lamp.sh"))
	r.m.handle(Event{Kind: "displays", Value: "1"}) // 启动时的基线：不触发
	expect(t, r.e.took())
	r.m.handle(Event{Kind: "displays", Value: "2"})
	expect(t, r.e.took(), "set lamp.sh on automation")
	r.advance(10 * time.Second)
	r.m.handle(Event{Kind: "displays", Value: "1"})
	expect(t, r.e.took(), "set lamp.sh off automation")
}

func TestWakeTrigger(t *testing.T) {
	r := newRig(t, rule(KindWake, "login", "", "run", "ping.sh"), rule(KindWake, "wake", "", "on", "lamp.sh"), rule(KindWake, "unlock", "", "off", "lamp.sh"))
	r.m.handle(Event{Kind: KindWake, Event: "login"})
	expect(t, r.e.took(), "run ping.sh automation")
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	expect(t, r.e.took(), "set lamp.sh on automation")
	r.m.handle(Event{Kind: KindWake, Event: "unlock"})
	expect(t, r.e.took(), "set lamp.sh off automation")
}

func TestFileTrigger(t *testing.T) {
	r := newRig(t, rule(KindFile, "", "/tmp/inbox", "run", "ping.sh"))
	fn, ok := r.h.watches["/tmp/inbox"]
	if !ok {
		t.Fatalf("path not watched: %v", r.h.watches)
	}
	fn()
	r.drain()
	expect(t, r.e.took(), "run ping.sh automation")
	r.m.DeleteRule("rule-1")
	r.m.apply()
	if len(r.h.watches) != 0 {
		t.Fatal("watch not removed with its rule")
	}
}

func TestShortcutTriggerAndBindings(t *testing.T) {
	r := newRig(t)
	r.e.views["lamp.sh"].Meta.Shortcut = "ctrl+alt+l"
	r.e.views["ping.sh"].Meta.Shortcut = "ctrl+alt+p"
	r.e.scenes = []engine.SceneView{{Scene: store.Scene{ID: "focus", Shortcut: "ctrl+alt+1"}}}
	r.m.SaveRule(rule(KindShortcut, "", "ctrl+alt+2", "scene", "focus"))
	r.m.SaveRule(rule(KindShortcut, "", "ctrl+alt+l", "run", "ping.sh")) // 与开关的快捷键重复
	r.h.taken["ctrl+alt+p"] = true                                       // 被其他 app 占用
	r.m.apply()
	for _, k := range []string{"ctrl+alt+l", "ctrl+alt+1", "ctrl+alt+2"} {
		r.h.keys[k]()
		r.drain()
	}
	expect(t, r.e.took(), "set lamp.sh on shortcut", "toggle-scene focus shortcut", "scene focus automation")
	issues := r.m.ShortcutIssues()
	if issues["rule:rule-2"].Kind != "duplicate" || issues["rule:rule-2"].Owner != "switch:lamp.sh" || issues["switch:ping.sh"].Kind != "taken" {
		t.Fatalf("issues %+v", issues)
	}
	r.m.Stop()
	if len(r.h.keys) != 0 {
		t.Fatalf("shortcuts left registered: %v", r.h.keys)
	}
}

// ───────── 条件、冲突、限流、暂停 ─────────

func TestConditionsAndConflicts(t *testing.T) {
	gated := rule(KindWake, "wake", "", "on", "lamp.sh")
	gated.When = &store.Condition{From: "10:00", To: "12:00"}
	r := newRig(t, gated)
	r.m.handle(Event{Kind: KindWake, Event: "wake"}) // 9:00，不在时间段内
	expect(t, r.e.took())

	r.now = time.Date(2026, 10, 19, 10, 30, 0, 0, time.Local)
	r.e.user["lamp.sh"] = r.now.Add(-5 * time.Minute) // 用户 5 分钟前手动改过
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	expect(t, r.e.took())

	r.advance(time.Minute)
	r.st.SetSetting(SettingConflict, "rule") // 总是执行规则
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	expect(t, r.e.took(), "set lamp.sh on automation")

	// 已经是目标状态：不重复执行
	r.advance(time.Minute)
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	expect(t, r.e.took())

	// 立即测试：忽略条件与冲突策略
	r.e.SetSwitch("lamp.sh", false, engine.Manual, "")
	r.e.took()
	r.now = time.Date(2026, 10, 19, 20, 0, 0, 0, time.Local)
	if err := r.m.Test("rule-1"); err != nil {
		t.Fatal(err)
	}
	expect(t, r.e.took(), "set lamp.sh on automation")
}

func TestCooldownFlapAndPause(t *testing.T) {
	notify := rule(KindWake, "wake", "", "toggle", "lamp.sh")
	notify.Notify = true
	r := newRig(t, notify)
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	r.advance(2 * time.Second)
	r.m.handle(Event{Kind: KindWake, Event: "wake"}) // 5 秒内的第二次被限流
	expect(t, r.e.took(), "set lamp.sh on automation")
	if len(r.h.notes) != 1 {
		t.Fatalf("notifications %q", r.h.notes)
	}
	// 一分钟内来回切换超过 3 次：停用规则并通知
	for i := 0; i < 3; i++ {
		r.advance(6 * time.Second)
		r.m.handle(Event{Kind: KindWake, Event: "wake"})
	}
	rr, _ := r.st.Rule("rule-1")
	if rr.Enabled || rr.Suspended != SuspendFlapping || !strings.Contains(r.h.notes[len(r.h.notes)-1], "more than 3 times") {
		t.Fatalf("not suspended: %+v %q", rr, r.h.notes)
	}
	r.m.apply()
	r.e.took()
	r.advance(time.Minute)
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	expect(t, r.e.took())

	// 暂停全部
	rr.Enabled = true
	r.m.SaveRule(rr)
	r.m.apply()
	r.m.Pause(r.now.Add(time.Hour))
	r.advance(time.Minute)
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	expect(t, r.e.took())
	if paused, until := r.m.Paused(); !paused || until.IsZero() {
		t.Fatal("pause not reported")
	}
	r.advance(2 * time.Hour) // 到时自动恢复
	r.m.handle(Event{Kind: KindWake, Event: "wake"})
	if len(r.e.took()) != 1 {
		t.Fatal("rule did not resume after the pause expired")
	}
	r.m.Pause(time.Time{})
	if paused, until := r.m.Paused(); !paused || !until.IsZero() {
		t.Fatal("pause forever")
	}
	r.m.Resume()
	if paused, _ := r.m.Paused(); paused {
		t.Fatal("resume")
	}
}

func TestLoop(t *testing.T) {
	r := newRig(t, rule(KindWake, "wake", "", "on", "lamp.sh"))
	r.m.Start()
	defer r.m.Stop()
	r.m.Fire(Event{Kind: KindWake, Event: "wake"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if v, _ := r.e.Get("lamp.sh"); v.State == engine.On {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event not handled by the loop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

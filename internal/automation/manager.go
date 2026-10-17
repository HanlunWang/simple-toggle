// Package automation 求值自动化规则并执行动作，同时管理全局快捷键。
//
// 它不依赖 mygo：需要系统界面框架的事件（显示器、唤醒、网络变化）由界面层通过 Fire
// 送进来；快捷键注册、文件监听与通知通过 Host 接口由界面层实现。电源、应用、Wi-Fi
// 与时间由这里自己检查（只在存在对应规则时）。
package automation

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/platform/darwin"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// 设置项（store settings）。
const (
	SettingPausedUntil = "automation.pausedUntil" // 空 = 未暂停；"forever"；或 RFC 3339 时间
	SettingConflict    = "automation.conflict"    // "manual"（默认）| "rule"

	SuspendFlapping = "flapping" // store.Rule.Suspended：规则来回切换同一个开关

	ConflictWindow = 10 * time.Minute // 用户手动改动后，规则在这段时间内不改动它
	RuleCooldown   = 5 * time.Second  // 同一条规则的最短触发间隔
	FlapWindow     = time.Minute      // 一分钟内被规则改动超过 FlapLimit 次即停用规则
	FlapLimit      = 3
)

// Engine 是规则动作需要的引擎能力（*engine.Engine 实现它）。
type Engine interface {
	SetSwitch(id string, on bool, src engine.Source, detail string) (<-chan struct{}, error)
	Run(id string, src engine.Source, detail string) (<-chan store.Run, error)
	ActivateScene(id string, src engine.Source, detail string) (<-chan store.Run, error)
	ToggleScene(id string, src engine.Source, detail string) (<-chan store.Run, error)
	Get(id string) (engine.View, bool)
	Scenes() []engine.SceneView
	List() []engine.View
	UserChangedAt(id string) time.Time
}

// Host 是界面层提供的系统能力。
type Host interface {
	RegisterShortcut(accel string, fn func()) error
	UnregisterShortcut(accel string)
	Watch(path string, fn func()) (stop func(), err error)
	Notify(title, body string)
}

// Platform 是这里自己检查的系统状态（测试里替换）。
type Platform struct {
	Power   func() (darwin.Power, error)
	Apps    func() ([]darwin.App, error)
	Network func() (key string, err error)
}

// Config 配置 Manager。
type Config struct {
	Engine   Engine
	Store    *store.Store
	Host     Host
	Platform Platform
	Now      func() time.Time
	// Describe 返回规则的说明（运行记录与通知里显示），界面层按语言提供。
	Describe func(store.Rule) string
	// FlapNotice 是规则因来回切换被停用时的通知正文。
	FlapNotice func(store.Rule) string

	PollPower   time.Duration // 默认 30 秒
	PollApps    time.Duration // 默认 2 秒
	PollNetwork time.Duration // 默认 30 秒
}

// ShortcutIssue 描述一个没能生效的快捷键。
type ShortcutIssue struct {
	Kind  string // "duplicate"（与另一项重复）| "taken"（系统或其他 app 占用）
	Owner string // duplicate 时：已占用它的那一项
}

type binding struct {
	accel string
	owner string // "switch:<id>"、"scene:<id>"、"rule:<id>"
	fire  func()
}

// Manager 求值规则、执行动作、管理快捷键。
type Manager struct {
	cfg    Config
	events chan Event
	reload chan struct{}
	stop   chan struct{}
	wg     sync.WaitGroup

	mu        sync.Mutex
	rules     []store.Rule
	issues    map[string]ShortcutIssue
	reg       []string // 已注册的快捷键
	regKey    string   // 上次注册时绑定列表的指纹：没变就不重新注册
	watches   map[string]func()
	lastFire  map[string]time.Time // 规则 id → 最近一次执行（限流）
	minuteKey map[string]string    // 时间规则 id → 已触发的那一分钟
	flaps     map[string][]time.Time

	// 轮询状态（只在 loop 里读写）
	lastPoll    map[string]time.Time
	power       *darwin.Power
	apps        map[string]string // bundle id → 名称
	network     string
	haveNetwork bool
	displays    int
}

// New 创建 Manager；Start 之后才开始工作。
func New(cfg Config) *Manager {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Describe == nil {
		cfg.Describe = func(r store.Rule) string { return r.Trigger.Kind + " → " + r.Action.Verb + " " + r.Action.Target }
	}
	if cfg.FlapNotice == nil {
		cfg.FlapNotice = func(r store.Rule) string {
			return fmt.Sprintf("Paused “%s”: it changed %s more than %d times in a minute", cfg.Describe(r), r.Action.Target, FlapLimit)
		}
	}
	if cfg.PollPower == 0 {
		cfg.PollPower = 30 * time.Second
	}
	if cfg.PollApps == 0 {
		cfg.PollApps = 2 * time.Second
	}
	if cfg.PollNetwork == 0 {
		cfg.PollNetwork = 30 * time.Second
	}
	if cfg.Platform.Power == nil {
		cfg.Platform.Power = darwin.PowerStatus
	}
	if cfg.Platform.Apps == nil {
		cfg.Platform.Apps = darwin.RunningApps
	}
	if cfg.Platform.Network == nil {
		cfg.Platform.Network = func() (string, error) {
			fp, err := darwin.NetworkFingerprint()
			return fp.Key(), err
		}
	}
	return &Manager{
		cfg: cfg, events: make(chan Event, 64), reload: make(chan struct{}, 1), stop: make(chan struct{}),
		issues: map[string]ShortcutIssue{}, watches: map[string]func(){}, lastFire: map[string]time.Time{},
		minuteKey: map[string]string{}, flaps: map[string][]time.Time{}, lastPoll: map[string]time.Time{},
		displays: -1,
	}
}

// Start 载入规则、注册快捷键并开始监听。
func (m *Manager) Start() {
	m.apply()
	m.wg.Add(1)
	go m.loop()
}

// Stop 停止监听并注销快捷键与文件监听。
func (m *Manager) Stop() {
	select {
	case <-m.stop:
		return
	default:
		close(m.stop)
	}
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.reg {
		m.cfg.Host.UnregisterShortcut(a)
	}
	m.reg, m.regKey = nil, ""
	for p, stop := range m.watches {
		stop()
		delete(m.watches, p)
	}
}

// Reload 在规则、场景或开关的快捷键变化后调用。
func (m *Manager) Reload() {
	select {
	case m.reload <- struct{}{}:
	default:
	}
}

// Fire 送入一个外部事件（显示器、唤醒、网络变化、文件变化、快捷键）。不会阻塞。
func (m *Manager) Fire(ev Event) {
	select {
	case m.events <- ev:
	default: // 队列满时丢弃：这些事件都是「状态变了」的提示
	}
}

func (m *Manager) loop() {
	defer m.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	m.poll(true)
	for {
		select {
		case <-m.stop:
			return
		case <-m.reload:
			m.apply()
			m.poll(true)
		case ev := <-m.events:
			m.handle(ev)
		case <-tick.C:
			m.tick(m.cfg.Now())
			m.poll(false)
		}
	}
}

// ───────── 规则与绑定 ─────────

// apply 重新读取规则，按需要注册快捷键与文件监听。
func (m *Manager) apply() {
	rules := m.cfg.Store.Rules()
	var binds []binding
	for _, v := range m.cfg.Engine.List() {
		if v.Meta.Shortcut == "" {
			continue
		}
		id := v.ID
		binds = append(binds, binding{v.Meta.Shortcut, "switch:" + id, func() { m.Fire(Event{Kind: "bind", Value: "switch:" + id}) }})
	}
	for _, sv := range m.cfg.Engine.Scenes() {
		if sv.Shortcut == "" {
			continue
		}
		id := sv.ID
		binds = append(binds, binding{sv.Shortcut, "scene:" + id, func() { m.Fire(Event{Kind: "bind", Value: "scene:" + id}) }})
	}
	paths := map[string]bool{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Trigger.Kind {
		case KindShortcut:
			accel, err := NormalizeShortcut(r.Trigger.Value)
			if err == nil && accel != "" {
				binds = append(binds, binding{accel, "rule:" + r.ID, func() { m.Fire(Event{Kind: KindShortcut, Value: accel}) }})
			}
		case KindFile:
			paths[r.Trigger.Value] = true
		}
	}

	var fp strings.Builder
	for _, b := range binds {
		fp.WriteString(b.accel + "=" + b.owner + ";")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = rules
	m.applyWatches(paths)
	if fp.String() == m.regKey {
		return
	}
	m.regKey = fp.String()
	// 快捷键：全部注销后按新列表注册；重复的只注册第一个
	for _, a := range m.reg {
		m.cfg.Host.UnregisterShortcut(a)
	}
	m.reg = nil
	m.issues = map[string]ShortcutIssue{}
	owners := map[string]string{}
	for _, b := range binds {
		accel, err := NormalizeShortcut(b.accel)
		if err != nil || accel == "" {
			continue
		}
		if first, dup := owners[accel]; dup {
			if first != b.owner {
				m.issues[b.owner] = ShortcutIssue{Kind: "duplicate", Owner: first}
			}
			continue
		}
		owners[accel] = b.owner
		if err := m.cfg.Host.RegisterShortcut(accel, b.fire); err != nil {
			m.issues[b.owner] = ShortcutIssue{Kind: "taken"}
			continue
		}
		m.reg = append(m.reg, accel)
	}
}

// applyWatches 按差异增删文件监听；调用方持有锁。
func (m *Manager) applyWatches(paths map[string]bool) {
	for p, stop := range m.watches {
		if !paths[p] {
			stop()
			delete(m.watches, p)
		}
	}
	for p := range paths {
		if _, ok := m.watches[p]; ok {
			continue
		}
		path := p
		stop, err := m.cfg.Host.Watch(path, func() { m.Fire(Event{Kind: KindFile, Value: path}) })
		if err == nil {
			m.watches[path] = stop
		}
	}
}

// ShortcutIssues 返回没能生效的快捷键：键是 "switch:<id>"、"scene:<id>" 或 "rule:<id>"。
func (m *Manager) ShortcutIssues() map[string]ShortcutIssue {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]ShortcutIssue, len(m.issues))
	for k, v := range m.issues {
		out[k] = v
	}
	return out
}

func (m *Manager) needs(kind string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules {
		if r.Enabled && r.Trigger.Kind == kind {
			return true
		}
	}
	return false
}

// ───────── 事件源 ─────────

func (m *Manager) due(name string, every time.Duration, force bool) bool {
	now := m.cfg.Now()
	if !force && now.Sub(m.lastPoll[name]) < every {
		return false
	}
	m.lastPoll[name] = now
	return true
}

// poll 检查电源、应用与网络（只在存在对应规则时）。
func (m *Manager) poll(force bool) {
	if m.needs(KindPower) && m.due("power", m.cfg.PollPower, force) {
		if p, err := m.cfg.Platform.Power(); err == nil {
			if prev := m.power; prev != nil {
				if prev.OnAC != p.OnAC {
					ev := map[bool]string{true: "plug", false: "unplug"}[p.OnAC]
					m.handle(Event{Kind: KindPower, Event: ev})
				}
				if p.HasBattery && p.Percent != prev.Percent && !p.OnAC {
					m.handle(Event{Kind: KindPower, Event: "level", Prev: prev.Percent, Cur: p.Percent})
				}
			}
			m.power = &p
		}
	} else if !m.needs(KindPower) {
		m.power = nil
	}
	if m.needs(KindApp) && m.due("apps", m.cfg.PollApps, force) {
		if list, err := m.cfg.Platform.Apps(); err == nil {
			now := map[string]string{}
			for _, a := range list {
				now[a.BundleID] = a.Name
			}
			if m.apps != nil {
				for id, name := range now {
					if _, was := m.apps[id]; !was {
						m.handle(Event{Kind: KindApp, Event: "launch", Value: id, Label: name})
					}
				}
				for id, name := range m.apps {
					if _, still := now[id]; !still {
						m.handle(Event{Kind: KindApp, Event: "quit", Value: id, Label: name})
					}
				}
			}
			m.apps = now
		}
	} else if !m.needs(KindApp) {
		m.apps = nil
	}
	if m.needs(KindWiFi) && m.due("network", m.cfg.PollNetwork, force) {
		m.checkNetwork()
	} else if !m.needs(KindWiFi) {
		m.haveNetwork = false
	}
}

func (m *Manager) checkNetwork() {
	key, err := m.cfg.Platform.Network()
	if err != nil {
		key = "" // 离线
	}
	if m.haveNetwork && key != m.network {
		if m.network != "" {
			m.handle(Event{Kind: KindWiFi, Event: "leave", Value: m.network})
		}
		if key != "" {
			m.handle(Event{Kind: KindWiFi, Event: "join", Value: key})
		}
	}
	m.network, m.haveNetwork = key, true
}

// tick 每秒检查一次时间规则。
func (m *Manager) tick(now time.Time) {
	m.mu.Lock()
	var due []store.Rule
	minute := now.Format("2006-01-02T15:04")
	for _, r := range m.rules {
		if r.Enabled && r.Trigger.Kind == KindTime && TimeDue(r.Trigger, now) && m.minuteKey[r.ID] != minute {
			m.minuteKey[r.ID] = minute
			due = append(due, r)
		}
	}
	m.mu.Unlock()
	for _, r := range due {
		m.execute(r, false)
	}
}

// handle 处理一个事件：内部事件（网络变化提示、显示器数量、快捷键绑定）先翻译，再匹配规则。
func (m *Manager) handle(ev Event) {
	switch ev.Kind {
	case "network": // 界面层的提示：网络可能变了
		if m.needs(KindWiFi) {
			m.checkNetwork()
		}
		return
	case "displays": // 界面层送来当前显示器数量
		n, _ := strconv.Atoi(ev.Value)
		prev := m.displays
		m.displays = n
		switch {
		case prev < 0:
		case n > prev:
			m.handle(Event{Kind: KindDisplay, Event: "connect"})
		case n < prev:
			m.handle(Event{Kind: KindDisplay, Event: "disconnect"})
		}
		return
	case "bind":
		m.fireBinding(ev.Value)
		return
	case KindWake:
		if ev.Event == "wake" {
			m.lastPoll = map[string]time.Time{} // 唤醒后立即重新检查电源、网络
		}
	}
	m.mu.Lock()
	var hit []store.Rule
	for _, r := range m.rules {
		if r.Enabled && Matches(r.Trigger, ev) {
			hit = append(hit, r)
		}
	}
	m.mu.Unlock()
	for _, r := range hit {
		m.execute(r, false)
	}
}

// fireBinding 执行开关或场景的快捷键。
func (m *Manager) fireBinding(owner string) {
	kind, id, _ := strings.Cut(owner, ":")
	switch kind {
	case "scene":
		_, _ = m.cfg.Engine.ToggleScene(id, engine.Shortcut, m.shortcutOf(owner))
	case "switch":
		v, ok := m.cfg.Engine.Get(id)
		if !ok {
			return
		}
		if v.Type == contract.Action {
			_, _ = m.cfg.Engine.Run(id, engine.Shortcut, m.shortcutOf(owner))
		} else {
			_, _ = m.cfg.Engine.SetSwitch(id, v.State != engine.On, engine.Shortcut, m.shortcutOf(owner))
		}
	}
}

func (m *Manager) shortcutOf(owner string) string {
	kind, id, _ := strings.Cut(owner, ":")
	switch kind {
	case "scene":
		for _, sv := range m.cfg.Engine.Scenes() {
			if sv.ID == id {
				return ShortcutSymbols(sv.Shortcut)
			}
		}
	case "switch":
		if v, ok := m.cfg.Engine.Get(id); ok {
			return ShortcutSymbols(v.Meta.Shortcut)
		}
	}
	return ""
}

// ───────── 执行 ─────────

// Paused 返回是否暂停，以及恢复的时间（零值表示直到手动恢复）。
func (m *Manager) Paused() (bool, time.Time) {
	v := m.cfg.Store.Setting(SettingPausedUntil)
	switch v {
	case "":
		return false, time.Time{}
	case "forever":
		return true, time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil || !m.cfg.Now().Before(t) {
		return false, time.Time{}
	}
	return true, t
}

// Pause 暂停全部规则到 until（零值表示直到手动恢复）。快捷键照常工作。
func (m *Manager) Pause(until time.Time) error {
	v := "forever"
	if !until.IsZero() {
		v = until.Format(time.RFC3339)
	}
	return m.cfg.Store.SetSetting(SettingPausedUntil, v)
}

// Resume 恢复全部规则。
func (m *Manager) Resume() error { return m.cfg.Store.SetSetting(SettingPausedUntil, "") }

// Test 立即执行规则的动作（忽略暂停、「仅当」、限流与冲突策略），用于「立即测试」。
func (m *Manager) Test(id string) error {
	r, ok := m.cfg.Store.Rule(id)
	if !ok {
		return store.ErrNoSuchItem
	}
	return m.perform(r, true)
}

// execute 按暂停、条件、限流与冲突策略决定是否执行规则。
func (m *Manager) execute(r store.Rule, force bool) {
	now := m.cfg.Now()
	if paused, _ := m.Paused(); paused {
		return
	}
	if !ConditionMet(r.When, now) {
		return
	}
	m.mu.Lock()
	if last, ok := m.lastFire[r.ID]; ok && now.Sub(last) < RuleCooldown {
		m.mu.Unlock()
		return
	}
	m.lastFire[r.ID] = now
	m.mu.Unlock()
	_ = m.perform(r, force)
}

func (m *Manager) perform(r store.Rule, force bool) error {
	now := m.cfg.Now()
	detail := m.cfg.Describe(r)
	src := engine.Automation
	act := r.Action
	changed := false
	switch act.Verb {
	case "scene":
		if _, err := m.cfg.Engine.ActivateScene(act.Target, src, detail); err != nil {
			return err
		}
	case "run":
		if _, err := m.cfg.Engine.Run(act.Target, src, detail); err != nil {
			return err
		}
	default:
		v, ok := m.cfg.Engine.Get(act.Target)
		if !ok {
			return engine.ErrNotFound
		}
		if v.Type == contract.Action {
			if _, err := m.cfg.Engine.Run(act.Target, src, detail); err != nil {
				return err
			}
			break
		}
		want := act.Verb == "on" || (act.Verb == "toggle" && v.State != engine.On)
		if (v.State == engine.On) == want && act.Verb != "toggle" {
			break // 已经是目标状态
		}
		if !force && m.cfg.Store.Setting(SettingConflict) != "rule" {
			if t := m.cfg.Engine.UserChangedAt(act.Target); !t.IsZero() && now.Sub(t) < ConflictWindow {
				return nil // 用户刚手动改过：以手动为准
			}
		}
		if _, err := m.cfg.Engine.SetSwitch(act.Target, want, src, detail); err != nil {
			return err
		}
		changed = true
	}
	_ = m.cfg.Store.MarkFired(r.ID, now)
	if r.Notify {
		m.cfg.Host.Notify("OpenToggle", detail)
	}
	if changed && !force {
		m.checkFlap(r, act.Target, now)
	}
	return nil
}

// checkFlap 发现规则在一分钟内来回改同一个开关超过 3 次时停用它并通知。
func (m *Manager) checkFlap(r store.Rule, target string, now time.Time) {
	m.mu.Lock()
	times := append(m.flaps[target], now)
	times = slices.DeleteFunc(times, func(t time.Time) bool { return now.Sub(t) > FlapWindow })
	m.flaps[target] = times
	flapping := len(times) > FlapLimit
	if flapping {
		delete(m.flaps, target)
	}
	m.mu.Unlock()
	if !flapping {
		return
	}
	_ = m.cfg.Store.Suspend(r.ID, SuspendFlapping)
	m.cfg.Host.Notify("OpenToggle", m.cfg.FlapNotice(r))
	m.Reload()
}

// ───────── 规则的增删改 ─────────

// SaveRule 校验后新增或修改规则；id 为空时自动生成。重新启用时清空自动停用的原因。
func (m *Manager) SaveRule(r store.Rule) (store.Rule, error) {
	if err := Validate(r); err != nil {
		return r, err
	}
	if r.Trigger.Kind == KindShortcut {
		r.Trigger.Value, _ = NormalizeShortcut(r.Trigger.Value)
	}
	if r.ID == "" {
		for n := len(m.cfg.Store.Rules()) + 1; ; n++ {
			id := "rule-" + strconv.Itoa(n)
			if _, taken := m.cfg.Store.Rule(id); !taken {
				r.ID = id
				break
			}
		}
	}
	if old, ok := m.cfg.Store.Rule(r.ID); ok {
		r.LastFired = old.LastFired
		if r.Enabled {
			r.Suspended = ""
		} else if r.Suspended == "" {
			r.Suspended = old.Suspended
		}
	}
	if err := m.cfg.Store.PutRule(r); err != nil {
		return r, err
	}
	m.Reload()
	return r, nil
}

// DeleteRule 删除规则。
func (m *Manager) DeleteRule(id string) error {
	if err := m.cfg.Store.DeleteRule(id); err != nil {
		return err
	}
	m.Reload()
	return nil
}

// Rules 返回全部规则。
func (m *Manager) Rules() []store.Rule { return m.cfg.Store.Rules() }

// CurrentNetwork 返回当前网络的指纹（规则编辑器的「当前网络」）。
func (m *Manager) CurrentNetwork() (string, error) { return m.cfg.Platform.Network() }

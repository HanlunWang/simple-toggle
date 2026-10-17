package app

import (
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/store"
)

type fakeBackend struct {
	mu      sync.Mutex
	views   []engine.View
	calls   []string
	params  map[string]string
	scripts map[string]string // 工作台：脚本内容
	runs    []store.Run
	scenes  []engine.SceneView
	changes []store.Change
}

func (f *fakeBackend) List() []engine.View {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]engine.View(nil), f.views...)
}
func (f *fakeBackend) log(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeBackend) SetSwitch(id string, on bool, src engine.Source, detail string) (<-chan struct{}, error) {
	state := "off"
	if on {
		state = "on"
	}
	f.log("set " + id + " " + state)
	ch := make(chan struct{})
	close(ch)
	return ch, nil
}
func (f *fakeBackend) Run(id string, src engine.Source, detail string) (<-chan store.Run, error) {
	f.log("run " + id)
	run := store.Run{OK: true}
	f.mu.Lock()
	for i := range f.views {
		if f.views[i].ID == id {
			f.views[i].LastRun = &run
		}
	}
	f.mu.Unlock()
	ch := make(chan store.Run, 1)
	ch <- run
	return ch, nil
}
func (f *fakeBackend) SetParams(id string, values map[string]string, src engine.Source) error {
	for k, v := range values {
		f.log("param " + id + " " + k + "=" + v)
	}
	return nil
}
func (f *fakeBackend) SetHidden(string, bool) error { return nil }
func (f *fakeBackend) Subscribe() (<-chan engine.Event, func()) {
	return make(chan engine.Event), func() {}
}
func (f *fakeBackend) PanelVisible(bool)  {}
func (f *fakeBackend) Refresh()           { f.log("refresh") }
func (f *fakeBackend) ScriptsDir() string { return "/tmp" }

func (f *fakeBackend) has(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func sampleViews() []engine.View {
	five := 125
	return []engine.View{
		{ID: "dark.sh", Name: "Dark Mode", Icon: "sf:moon.fill", Type: contract.Toggle, State: engine.On},
		{ID: "awake.sh", Name: "Keep Awake", Icon: "☕️", Type: contract.Daemon, State: engine.On, Remaining: &five,
			MenuBar: &contract.MenuBar{Mode: contract.MenuBarAdd, Icon: "☕️", Countdown: true},
			Params: []engine.ParamView{{Param: contract.Param{Key: "mode", Label: "模式", Type: contract.ParamSelect,
				Options: []contract.Option{{Label: "屏幕", Value: "d"}, {Label: "两者", Value: "di"}}}, Value: "d"}}},
		{ID: "wifi.sh", Name: "Wi-Fi", Icon: "sf:wifi", Type: contract.Toggle, State: engine.Off},
		{ID: "flush.sh", Name: "Flush DNS", Icon: "lucide:rotate-cw", Type: contract.Action, State: engine.Idle},
		{ID: "secret.sh", Name: "Secret", Icon: "lucide:lock", Type: contract.Toggle, State: engine.Off, Hidden: true},
	}
}

func newTestPanel(views []engine.View) (*panel, *fakeBackend) {
	b := &fakeBackend{views: views}
	p := newPanel(b, Prefs{Accent: AccentInk, Layout: "list", Density: "comfortable", ShowGroups: true, ShowScenes: true})
	return p, b
}

// mainQueue 代替 mygo.RunOnMain：后台 goroutine 投递的回调由测试 goroutine 执行，
// 和真实环境一样只在「主线程」上修改面板状态
func mainQueue() chan func() {
	q := make(chan func(), 8)
	markFlashOnMain = func(fn func()) { q <- fn }
	return q
}

func TestPanelRendersVisibleSwitches(t *testing.T) {
	p, _ := newTestPanel(sampleViews())
	tt := ui.NewTester(p.view, 340, 520)
	for _, want := range []string{"OpenToggle", "2 ON", "Dark Mode", "Keep Awake", "Wi-Fi", "Flush DNS"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Secret") {
		t.Error("hidden switch shown")
	}
	joined := strings.Join(tt.Texts(), "|")
	if !strings.Contains(joined, "2:05") {
		t.Errorf("countdown not shown: %s", joined)
	}
}

func TestPanelToggleAndRun(t *testing.T) {
	q := mainQueue()
	p, b := newTestPanel(sampleViews())
	tt := ui.NewTester(p.view, 340, 520)
	// 在 Wi-Fi 这一行的右侧（开关控件所在位置）点击
	r, ok := tt.Find("Wi-Fi")
	if !ok {
		t.Fatal("Wi-Fi row not found")
	}
	tt.ClickAt(306, r.Y+r.H/2)
	tt.Frame()
	if !b.has("set wifi.sh on") {
		t.Fatalf("calls %v", b.calls)
	}
	if err := tt.Click(tr("run") + " Flush DNS"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("run flush.sh") {
		t.Fatalf("calls %v", b.calls)
	}
	// 运行结束后回到主线程记录结果，按钮显示「完成」
	(<-q)()
	tt.Frame()
	if !tt.HasText(tr("done")) {
		t.Fatalf("no done flash: %q", tt.Texts())
	}
}

func TestPanelSearch(t *testing.T) {
	p, _ := newTestPanel(sampleViews())
	p.searching, p.query = true, "wi"
	tt := ui.NewTester(p.view, 340, 520)
	if !tt.HasText("Wi-Fi") || tt.HasText("Dark Mode") {
		t.Fatalf("search filter: %q", tt.Texts())
	}
	p.query = "zzz"
	tt.Frame()
	if !tt.HasText(strings.Replace(tr("no_match"), "%s", "zzz", 1)) {
		t.Fatalf("empty search: %q", tt.Texts())
	}
}

func TestPanelParams(t *testing.T) {
	p, b := newTestPanel(sampleViews())
	tt := ui.NewTester(p.view, 340, 620)
	if err := tt.Click("params"); err != nil { // 第一个带参数的开关（Keep Awake）的展开按钮
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("两者") || !tt.HasText("SWITCH_MODE") {
		t.Fatalf("params not expanded: %q", tt.Texts())
	}
	if err := tt.Click("两者"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("param awake.sh mode=di") {
		t.Fatalf("calls %v", b.calls)
	}
}

func TestPanelGroupsAndGrid(t *testing.T) {
	views := sampleViews()
	views[0].Meta.Group = "系统"
	views[2].Meta.Group = "网络"
	p, b := newTestPanel(views)
	tt := ui.NewTester(p.view, 360, 620)
	for _, want := range []string{"系统", "网络", tr("other_group")} {
		if !tt.HasText(want) {
			t.Errorf("missing group %q in %q", want, tt.Texts())
		}
	}
	p.prefs.Layout = "grid"
	tt.Frame()
	if err := tt.Click("Dark Mode"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("set dark.sh off") {
		t.Fatalf("grid tile toggle: %v", b.calls)
	}
}

func TestTrayIconsAndLucide(t *testing.T) {
	for _, s := range []trayStyle{trayPill, trayDot, trayBars} {
		for _, n := range []int{0, 1, 3} {
			if png := trayIconPNG(s, n, n == 1); len(png) < 100 {
				t.Errorf("%s/%d: empty icon", s, n)
			}
		}
	}
	if !HasIcon("coffee") || HasIcon("definitely-not-an-icon") {
		t.Error("lucide lookup")
	}
	if png := templatePNG("coffee", 32); len(png) < 100 {
		t.Error("lucide rasterization failed")
	}
	if svgIcon("coffee") == nil || svgIcon("nope") == nil {
		t.Error("svg parse / fallback")
	}
}

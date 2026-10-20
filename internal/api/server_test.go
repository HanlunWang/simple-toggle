package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/keys"
	"github.com/HanlunWang/simple-toggle/internal/platform/darwin"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

type env struct {
	t    *testing.T
	ts   *httptest.Server
	s    *Server
	dir  string
	disc string
	post darwin.PostResult
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "switches")
	os.MkdirAll(dir, 0o755)
	mark := filepath.Join(root, "lamp.on")
	os.WriteFile(filepath.Join(dir, "lamp.sh"), []byte(`#!/bin/sh
# <switch.name> Lamp
# <switch.icon> sf:moon.fill
# <switch.param> key=mode type=select label=Mode default=a options="A=a|B=b"
case "$1" in on) touch `+mark+` ;; off) rm -f `+mark+` ;; status) [ -f `+mark+` ] && echo on || echo off ;; esac
`), 0o755)
	os.WriteFile(filepath.Join(dir, "摸鱼.sh"), []byte("#!/bin/sh\n# <switch.name> 摸鱼\n# <switch.type> action\necho \"fish $SIMPLETOGGLE_TRIGGER\"\n"), 0o755)
	st, _ := store.Open(filepath.Join(root, "data"))
	eng := engine.New(engine.Config{ScriptsDir: dir, LedgerPath: filepath.Join(root, "daemons.json"), Store: st,
		PollHidden: time.Hour, PollVisible: time.Hour})
	if err := eng.Start(); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, dir: dir, disc: filepath.Join(root, "api.json"), post: darwin.Delivered}
	e.s = New(eng, Options{DiscoveryPath: e.disc, Version: "2.0.0-test", Store: st,
		PostKey: func(keys.Spec) darwin.PostResult { return e.post }, AXTrusted: func() bool { return true }})
	e.ts = httptest.NewServer(e.s.Handler())
	t.Cleanup(func() { e.ts.Close(); eng.Shutdown() })
	return e
}

func (e *env) do(method, path, body string, auth bool, client string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	if auth {
		req.Header.Set("Authorization", "Bearer "+e.s.Token())
	}
	if client != "" {
		req.Header.Set(HeaderClient, client)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAuthAndHost(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do("POST", "/v1/switches/lamp/on", "", false, ""); code != 401 {
		t.Fatalf("write without token: %d", code)
	}
	// 读同样需要令牌：参数值、脚本、运行输出可能含敏感信息
	for _, path := range []string{"/v1/switches", "/v1/switches/lamp", "/v1/switches/lamp/script", "/v2/switches", "/v2/runs", "/v2/events", "/v2/scenes", "/v2/automations", "/v2/changes", "/v1/nope"} {
		if code, _ := e.do("GET", path, "", false, ""); code != 401 {
			t.Fatalf("GET %s without token: %d", path, code)
		}
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/v1/switches", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	if code, _ := e.do("GET", "/v1/switches", "", true, ""); code != 200 {
		t.Fatalf("read with token: %d", code)
	}
	for _, path := range []string{"/v1/ping", "/v2/ping"} {
		if code, _ := e.do("GET", path, "", false, ""); code != 200 {
			t.Fatalf("%s should not need a token: %d", path, code)
		}
	}
	if code, _ := e.do("POST", "/v1/validate", "#!/bin/sh\n# <switch.name> A\necho status\n", false, ""); code != 200 {
		t.Fatalf("validate should not need a token: %d", code)
	}
	req, _ = http.NewRequest("GET", e.ts.URL+"/v1/switches", nil)
	req.Host = "evil.example:43737"
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("foreign Host: %d", resp.StatusCode)
	}
}

func TestV1Compat(t *testing.T) {
	e := newEnv(t)
	code, body := e.do("GET", "/v1/switches", "", true, "")
	var list []map[string]any
	if code != 200 || json.Unmarshal([]byte(body), &list) != nil || len(list) != 2 {
		t.Fatalf("%d %s", code, body)
	}
	lamp := list[0]
	for _, k := range []string{"enabled", "icon", "id", "name", "params", "state", "type"} {
		if _, ok := lamp[k]; !ok {
			t.Errorf("v1 switch DTO missing %q", k)
		}
	}
	param := lamp["params"].([]any)[0].(map[string]any)
	if param["defaultValue"] != "a" || param["value"] != "a" || len(param["options"].([]any)) != 2 {
		t.Errorf("param DTO %v", param)
	}
	if code, body := e.do("POST", "/v1/switches/lamp/on", "", true, "cli"); code != 200 || !strings.Contains(body, `"state": "on"`) {
		t.Fatalf("on: %d %s", code, body)
	}
	if code, body := e.do("PUT", "/v1/switches/lamp.sh/params", `{"mode":"b"}`, true, ""); code != 200 || !strings.Contains(body, `"value": "b"`) {
		t.Fatalf("params: %d %s", code, body)
	}
	if code, body := e.do("PUT", "/v1/switches/lamp/params", `{"nope":1}`, true, ""); code != 422 || !strings.Contains(body, `switch has no param with key \"nope\"`) {
		t.Fatalf("unknown param: %d %s", code, body)
	}
	if code, body := e.do("GET", "/v1/switches/ghost", "", true, ""); code != 404 || !strings.Contains(body, `no switch with id \"ghost\"`) {
		t.Fatalf("404: %d %s", code, body)
	}
	if code, body := e.do("GET", "/v1/switches/"+url.PathEscape("摸鱼.sh"), "", true, ""); code != 200 || !strings.Contains(body, `"type": "action"`) {
		t.Fatalf("CJK id: %d %s", code, body)
	}
	if code, body := e.do("POST", "/v1/switches/lamp/disable", "", true, ""); code != 200 || !strings.Contains(body, `"enabled": false`) {
		t.Fatalf("disable: %d %s", code, body)
	}
	if code, body := e.do("GET", "/v1/switches/lamp/script", "", true, ""); code != 200 || !strings.HasPrefix(body, "#!/bin/sh") {
		t.Fatalf("script: %d", code)
	}
	if code, body := e.do("PUT", "/v1/switches/lamp/script", "#!/bin/sh\necho no name\n", true, ""); code != 422 || !strings.Contains(body, `"valid": false`) {
		t.Fatalf("lint reject: %d %s", code, body)
	}
	newScript := "#!/bin/sh\n# <switch.name> New One\n# <switch.type> action\necho ok\n"
	if code, body := e.do("POST", "/v1/scripts", newScript, true, ""); code != 201 || !strings.Contains(body, `"id": "new-one.sh"`) {
		t.Fatalf("create: %d %s", code, body)
	}
	if code, _ := e.do("DELETE", "/v1/switches/new-one", "", true, ""); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, body := e.do("POST", "/v1/switches/"+url.PathEscape("摸鱼")+"/on", "", true, ""); code != 409 {
		t.Fatalf("on for an action: %d %s", code, body)
	}
	if code, body := e.do("GET", "/v1/ping", "", false, ""); code != 200 || !strings.Contains(body, `"version":"2.0.0-test"`) {
		t.Fatalf("ping: %s", body)
	}
}

func TestV2RunAndRuns(t *testing.T) {
	e := newEnv(t)
	code, body := e.do("POST", "/v2/switches/"+url.PathEscape("摸鱼")+"/run", "", true, "mcp/Claude Code")
	if code != 200 || !strings.Contains(body, "fish ai") || !strings.Contains(body, `"detail": "Claude Code"`) {
		t.Fatalf("run: %d %s", code, body)
	}
	e.do("POST", "/v1/switches/lamp/on", "", true, "cli/agent:Codex")
	code, body = e.do("GET", "/v2/runs?limit=5", "", true, "")
	var runs []map[string]any
	if code != 200 || json.Unmarshal([]byte(body), &runs) != nil || len(runs) < 2 {
		t.Fatalf("runs: %d %s", code, body)
	}
	if runs[0]["source"] != "ai" || runs[0]["detail"] != "Codex · CLI" {
		t.Fatalf("agent CLI source: %v", runs[0])
	}
	code, body = e.do("GET", "/v2/switches/lamp", "", true, "")
	if code != 200 || !strings.Contains(body, `"kind": "lucide"`) || !strings.Contains(body, `"value": "moon"`) || !strings.Contains(body, `"lastRun"`) {
		t.Fatalf("v2 switch: %s", body)
	}
}

func TestPress(t *testing.T) {
	e := newEnv(t)
	if code, body := e.do("POST", "/v1/press", "cmd+shift+k", true, ""); code != 200 || !strings.Contains(body, `"key":"cmd+shift+k"`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := e.do("POST", "/v1/press", "hyper+q", true, ""); code != 400 {
		t.Fatal("bad spec accepted")
	}
	e.post = darwin.NotTrusted
	if code, body := e.do("POST", "/v1/press", "f15", true, ""); code != 403 || !strings.Contains(body, "accessibility permission not granted") {
		t.Fatalf("%d %s", code, body)
	}
}

func TestEventsStream(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("GET", e.ts.URL+"/v2/events", nil)
	req.Header.Set("Authorization", "Bearer "+e.s.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	go func() {
		time.Sleep(100 * time.Millisecond)
		e.do("POST", "/v2/switches/"+url.PathEscape("摸鱼")+"/run", "", true, "")
	}()
	sc := bufio.NewScanner(resp.Body)
	deadline := time.After(5 * time.Second)
	lines := make(chan string)
	go func() {
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	for {
		select {
		case l := <-lines:
			if l == "event: run" {
				return
			}
		case <-deadline:
			t.Fatal("no run event")
		}
	}
}

func TestDiscoveryFile(t *testing.T) {
	e := newEnv(t)
	e.s.opts.Port = 0 // 测试中不真正监听固定端口
	if err := e.s.writeDiscovery(); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(e.disc)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("discovery file mode %v", fi.Mode().Perm())
	}
	d, _ := ReadDiscovery(e.disc)
	if d.Token != e.s.Token() || !strings.HasPrefix(d.Token, "st_") {
		t.Fatalf("token not written: %q", d.Token)
	}
	// 重启后沿用同一令牌
	again := New(nil, Options{DiscoveryPath: e.disc})
	if again.Token() != e.s.Token() {
		t.Fatal("token not reused")
	}
	// 更名前（ot_ 前缀）的令牌照常沿用
	old := "ot_" + strings.Repeat("ab", 24)
	os.WriteFile(e.disc, []byte(`{"port":1,"pid":1,"token":"`+old+`"}`), 0o600)
	if New(nil, Options{DiscoveryPath: e.disc}).Token() != old {
		t.Fatal("legacy token not reused")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestTokenSurvivesRestart(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "api.json")
	s := New(nil, Options{Port: freePort(t), DiscoveryPath: disc, Version: "2.0.0-test"})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	d, _ := ReadDiscovery(disc)
	if d.Port != s.Port() || d.PID != os.Getpid() || d.Token != s.Token() {
		t.Fatalf("running discovery: %+v", d)
	}
	tok := s.Token()
	s.Close()
	// 退出后只留令牌：客户端据此知道 app 没在运行，下次启动沿用令牌
	d, err := ReadDiscovery(disc)
	if err != nil || d.Port != 0 || d.PID != 0 || d.Token != tok {
		t.Fatalf("after close: %+v %v", d, err)
	}
	s2 := New(nil, Options{Port: freePort(t), DiscoveryPath: disc})
	if s2.Token() != tok {
		t.Fatal("token changed across restart")
	}
	if err := s2.Start(); err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	d, _ = ReadDiscovery(disc)
	if s2.Token() == tok || d.Token != s2.Token() || d.Port != s2.Port() {
		t.Fatalf("regenerate: %+v", d)
	}
	if fi, _ := os.Stat(disc); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestRegenerateTokenWhileServing(t *testing.T) {
	// 令牌在处理请求的同时被换掉：-race 下不能有数据竞争，旧令牌随即失效
	e := newEnv(t)
	old := e.s.Token()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			e.s.RegenerateToken()
		}
	}()
	for i := 0; i < 20; i++ {
		e.do("GET", "/v1/switches", "", true, "")
	}
	<-done
	req, _ := http.NewRequest("GET", e.ts.URL+"/v1/switches", nil)
	req.Header.Set("Authorization", "Bearer "+old)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("old token still accepted: %d", resp.StatusCode)
	}
}

func TestBadParamValues(t *testing.T) {
	e := newEnv(t)
	if code, body := e.do("PUT", "/v1/switches/lamp/params", `{"mode":"zzz"}`, true, ""); code != 400 || !strings.Contains(body, "must be one of") {
		t.Fatalf("bad select: %d %s", code, body)
	}
	if code, _ := e.do("PUT", "/v1/switches/lamp/params", `{"mode":"b"}`, true, ""); code != 200 {
		t.Fatalf("good select: %d", code)
	}
}

func TestScenesAPI(t *testing.T) {
	e := newEnv(t)
	st := e.s.opts.Store
	st.PutScene(store.Scene{ID: "work", Name: "Work", Restore: true, Steps: []store.SceneStep{{Switch: "lamp.sh", Action: "on"}, {Switch: "摸鱼.sh", Action: "run"}}})
	code, body := e.do("GET", "/v2/scenes", "", true, "")
	if code != 200 || !strings.Contains(body, `"id": "work"`) || !strings.Contains(body, `"active": false`) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, _ := e.do("POST", "/v2/scenes/work/activate", "", false, ""); code != 401 {
		t.Fatalf("activate without a token: %d", code)
	}
	code, body = e.do("POST", "/v2/scenes/Work/toggle", "", true, "cli")
	if code != 200 || !strings.Contains(body, `"ok": true`) || !strings.Contains(body, `"active": true`) || !strings.Contains(body, `"source": "cli"`) {
		t.Fatalf("toggle: %d %s", code, body)
	}
	code, body = e.do("POST", "/v2/scenes/work/toggle", "", true, "")
	if code != 200 || !strings.Contains(body, `"action": "restore"`) {
		t.Fatalf("restore: %d %s", code, body)
	}
	if code, _ := e.do("POST", "/v2/scenes/ghost/activate", "", true, ""); code != 404 {
		t.Fatalf("missing scene: %d", code)
	}
	st.PutRule(store.Rule{ID: "r1", Enabled: true, Trigger: store.Trigger{Kind: "wake", Event: "wake"}, Action: store.RuleAction{Verb: "on", Target: "lamp.sh"}})
	if code, body := e.do("GET", "/v2/automations", "", true, ""); code != 200 || !strings.Contains(body, `"kind": "wake"`) {
		t.Fatalf("automations: %d %s", code, body)
	}
}

func TestAIPermissionsAndApproval(t *testing.T) {
	e := newEnv(t)
	st := e.s.opts.Store
	agent := "mcp/claude-code"
	// 默认：AI 可以读、开关、写（需确认），不能删
	if code, _ := e.do("GET", "/v1/switches", "", true, agent); code != 200 {
		t.Fatalf("read: %d", code)
	}
	if code, body := e.do("DELETE", "/v1/switches/lamp", "", true, agent); code != 403 || !strings.Contains(body, "delete scripts") {
		t.Fatalf("delete: %d %s", code, body)
	}
	script := "#!/bin/sh\n# <switch.name> Lamp\n# <switch.param> key=mode type=select label=Mode default=a options=\"A=a|B=b\"\ncase \"$1\" in status) echo off ;; esac\n"
	code, body := e.do("PUT", "/v1/switches/lamp/script", script, true, agent)
	if code != 202 || !strings.Contains(body, `"changeId": "chg-1"`) {
		t.Fatalf("write should wait: %d %s", code, body)
	}
	if code, body := e.do("GET", "/v2/changes/chg-1", "", true, agent); code != 200 || !strings.Contains(body, `"status": "pending"`) {
		t.Fatalf("status: %d %s", code, body)
	}
	if code, _ := e.do("POST", "/v2/changes/chg-1/approve", "", true, agent); code != 404 {
		t.Fatalf("approval must not be possible over the API: %d", code)
	}
	// 用户（非 AI）写脚本不需要确认
	if code, _ := e.do("PUT", "/v1/switches/lamp/script", script, true, "cli"); code != 200 {
		t.Fatalf("user write: %d", code)
	}
	// 关掉确认：AI 直接写；关掉开关权限：拒绝
	st.SetSetting(SettingConfirm, "false")
	if code, _ := e.do("PUT", "/v1/switches/lamp/script", script, true, agent); code != 200 {
		t.Fatalf("write without confirmation: %d", code)
	}
	st.SetSetting("ai.toggle", "false")
	if code, body := e.do("POST", "/v1/switches/lamp/on", "", true, agent); code != 403 || !strings.Contains(body, "turn switches on or off") {
		t.Fatalf("toggle refused: %d %s", code, body)
	}
	if code, _ := e.do("POST", "/v1/switches/lamp/on", "", true, "cli"); code != 200 {
		t.Fatalf("user toggle: %d", code)
	}
	st.SetSetting("ai.read", "false")
	if code, _ := e.do("GET", "/v1/switches", "", true, agent); code != 403 {
		t.Fatalf("read refused: %d", code)
	}
	if code, _ := e.do("POST", "/v1/validate", "#!/bin/sh\n", false, agent); code != 200 {
		t.Fatalf("validate needs no permission: %d", code)
	}
}

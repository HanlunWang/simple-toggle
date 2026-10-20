package main

// M1 验收：真实构建 simpletoggle，在隔离的目录与端口上运行无界面引擎，
// 用 skill 文档里的每条命令与 MCP 客户端驱动它。不碰用户的脚本、偏好设置与正在运行的 app。

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "simpletoggle-e2e-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "simpletoggle")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type instance struct {
	t       *testing.T
	env     []string
	home    string
	scratch string
	serve   *exec.Cmd
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func start(t *testing.T) *instance {
	t.Helper()
	root := t.TempDir()
	in := &instance{t: t, home: filepath.Join(root, "home"), scratch: filepath.Join(root, "scratch")}
	os.MkdirAll(filepath.Join(in.home, "switches"), 0o755)
	os.MkdirAll(in.scratch, 0o755)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SIMPLETOGGLE_") || strings.HasPrefix(kv, "CLAUDECODE=") {
			continue // 测试自己控制调用方身份
		}
		in.env = append(in.env, kv)
	}
	in.env = append(in.env, "SIMPLETOGGLE_HOME="+in.home, "SIMPLETOGGLE_DATA="+filepath.Join(root, "data"),
		"SIMPLETOGGLE_PORT="+strconv.Itoa(freePort(t)))

	mark := filepath.Join(in.scratch, "lamp.on")
	in.writeScript("lamp.sh", fmt.Sprintf(`#!/bin/sh
# <switch.name> Lamp
# <switch.icon> sf:moon.fill
# <switch.param> key=mode type=select label=Mode default=a options="Alpha=a|Beta=b"
case "$1" in
  on) echo "$SWITCH_MODE" > %q ;;
  off) rm -f %q ;;
  status) [ -f %q ] && echo on || echo off ;;
esac
`, mark, mark, mark))
	in.writeScript("serve.sh", "#!/bin/sh\n# <switch.name> Serve\n# <switch.type> daemon\nexec sleep 60\n")
	in.writeScript("flush.sh", "#!/bin/sh\n# <switch.name> Flush\n# <switch.type> action\necho flushed\n")

	in.serve = exec.Command(bin, "serve", "--no-seed", "--no-import")
	in.serve.Env = in.env
	if err := in.serve.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.serve.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { in.serve.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			in.serve.Process.Kill()
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, _, code := in.run("list"); code == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("headless engine did not come up")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return in
}

func (in *instance) writeScript(name, body string) {
	if err := os.WriteFile(filepath.Join(in.home, "switches", name), []byte(body), 0o755); err != nil {
		in.t.Fatal(err)
	}
}

func (in *instance) run(args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(bin, args...)
	cmd.Env = in.env
	var o, e strings.Builder
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	code = 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return o.String(), e.String(), code
}

func (in *instance) must(args ...string) string {
	in.t.Helper()
	out, errOut, code := in.run(args...)
	if code != 0 {
		in.t.Fatalf("simpletoggle %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

func TestSkillCommands(t *testing.T) {
	in := start(t)

	if out := in.must("list"); !strings.Contains(out, "lamp.sh") || !strings.Contains(out, "flush.sh") || !strings.Contains(out, "Serve") {
		t.Fatalf("list:\n%s", out)
	}
	var list []map[string]any
	if json.Unmarshal([]byte(in.must("list", "--json")), &list) != nil || len(list) != 3 {
		t.Fatal("list --json")
	}

	in.must("on", "lamp") // .sh 后缀可省略
	if s := strings.TrimSpace(in.must("state", "lamp")); s != "on" {
		t.Fatalf("state after on: %q", s)
	}
	in.must("set", "lamp", "mode=b")
	if out := in.must("params", "lamp"); !strings.Contains(out, "mode = b  (select, default a)  options: Alpha=a | Beta=b") {
		t.Fatalf("params:\n%s", out)
	}
	if _, errOut, code := in.run("set", "lamp", "nope=1"); code != 1 || !strings.Contains(errOut, "no param") {
		t.Fatalf("set unknown param: %d %s", code, errOut)
	}
	in.must("off", "lamp")
	if s := strings.TrimSpace(in.must("state", "lamp")); s != "off" {
		t.Fatalf("state after off: %q", s)
	}

	in.must("disable", "lamp")
	if out := in.must("list"); !strings.Contains(out, "[hidden]") {
		t.Fatalf("hidden marker missing:\n%s", out)
	}
	in.must("enable", "lamp")

	in.must("on", "serve.sh")
	if s := strings.TrimSpace(in.must("state", "serve")); s != "on" {
		t.Fatalf("daemon state: %q", s)
	}
	in.must("off", "serve")

	if out := in.must("run", "flush"); strings.TrimSpace(out) != "flushed" {
		t.Fatalf("run: %q", out)
	}
	if _, errOut, code := in.run("on", "flush"); code != 1 || !strings.Contains(errOut, "action switch") {
		t.Fatalf("on for an action: %d %s", code, errOut)
	}

	if out := in.must("cat", "lamp"); !strings.HasPrefix(out, "#!/bin/sh\n# <switch.name> Lamp") {
		t.Fatalf("cat:\n%s", out)
	}

	draft := filepath.Join(in.scratch, "draft.sh")
	os.WriteFile(draft, []byte("#!/bin/sh\n# <switch.name> My Switch\ncase \"$1\" in status) echo off ;; esac\n"), 0o644)
	if out := in.must("validate", draft); !strings.Contains(out, "ok: no issues") {
		t.Fatalf("validate:\n%s", out)
	}
	if out := in.must("add", draft); !strings.Contains(out, `"id": "my-switch.sh"`) {
		t.Fatalf("add:\n%s", out)
	}
	os.WriteFile(draft, []byte("#!/bin/sh\n# <switch.name> My Switch v2\ncase \"$1\" in status) echo off ;; esac\n"), 0o644)
	in.must("put", "my-switch", draft)
	if out := in.must("list"); !strings.Contains(out, "My Switch v2") {
		t.Fatalf("put not applied:\n%s", out)
	}
	bad := filepath.Join(in.scratch, "bad.sh")
	os.WriteFile(bad, []byte("#!/bin/sh\necho no name\n"), 0o644)
	if out, _, code := in.run("validate", bad); code != 1 || !strings.Contains(out, "error: Missing required directive") {
		t.Fatalf("validate bad: %d %s", code, out)
	}
	if _, _, code := in.run("add", bad); code != 1 {
		t.Fatal("add accepted a script with errors")
	}
	in.must("rm", "my-switch")
	if out := in.must("list"); strings.Contains(out, "my-switch") {
		t.Fatalf("rm:\n%s", out)
	}

	if out := in.must("log", "--limit", "50"); !strings.Contains(out, "flush.sh") || !strings.Contains(out, "lamp.sh") || !strings.Contains(out, "cli") {
		t.Fatalf("log:\n%s", out)
	}
	if _, errOut, code := in.run("press", "hyper+q"); code != 1 || !strings.Contains(errOut, "invalid key spec") {
		t.Fatalf("press invalid: %d %s", code, errOut)
	}

	// 运行记录：带上 agent 身份的 CLI 调用记为 AI 来源
	in.env = append(in.env, "SIMPLETOGGLE_AGENT=TestAgent")
	in.must("run", "flush")
	var runs []map[string]any
	json.Unmarshal([]byte(in.must("log", "flush", "--json")), &runs)
	if len(runs) == 0 || runs[0]["source"] != "ai" || runs[0]["detail"] != "TestAgent · CLI" {
		t.Fatalf("agent provenance: %v", runs)
	}
}

func TestWritesNeedToken(t *testing.T) {
	in := start(t)
	var port int
	for _, kv := range in.env {
		if strings.HasPrefix(kv, "SIMPLETOGGLE_PORT=") {
			port, _ = strconv.Atoi(strings.TrimPrefix(kv, "SIMPLETOGGLE_PORT="))
		}
	}
	out, _ := exec.Command("/usr/bin/curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "-X", "POST",
		fmt.Sprintf("http://127.0.0.1:%d/v1/switches/lamp/on", port)).Output()
	if string(out) != "401" {
		t.Fatalf("write without token: %s", out)
	}
	// 读也需要令牌；ping 不需要
	out, _ = exec.Command("/usr/bin/curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		fmt.Sprintf("http://127.0.0.1:%d/v1/switches", port)).Output()
	if string(out) != "401" {
		t.Fatalf("read without token: %s", out)
	}
	out, _ = exec.Command("/usr/bin/curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		fmt.Sprintf("http://127.0.0.1:%d/v2/ping", port)).Output()
	if string(out) != "200" {
		t.Fatalf("ping without token: %s", out)
	}
	fi, err := os.Stat(filepath.Join(in.home, "api.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("discovery file: %v %v", err, fi)
	}
}

func TestMCP(t *testing.T) {
	in := start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.Command(bin, "mcp")
	cmd.Env = in.env
	client := sdk.NewClient(&sdk.Implementation{Name: "e2e-client", Version: "1"}, nil)
	sess, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"simpletoggle_list", "simpletoggle_set_switch", "simpletoggle_set_params", "simpletoggle_set_enabled",
		"simpletoggle_get_script", "simpletoggle_write_script", "simpletoggle_validate", "simpletoggle_delete",
		"simpletoggle_run_action", "simpletoggle_get_runs", "simpletoggle_list_scenes", "simpletoggle_activate_scene", "simpletoggle_list_automations", "simpletoggle_change_status"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}

	call := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res.Content[0].(*sdk.TextContent).Text, res.IsError
	}
	if text, isErr := call("simpletoggle_list", map[string]any{}); isErr || !strings.Contains(text, "lamp.sh") {
		t.Fatalf("list: %s", text)
	}
	if text, isErr := call("simpletoggle_list_scenes", map[string]any{}); isErr || strings.TrimSpace(text) != "[]" {
		t.Fatalf("list_scenes: %s", text)
	}
	if text, isErr := call("simpletoggle_activate_scene", map[string]any{"id": "ghost"}); !isErr || !strings.Contains(text, "no scene") {
		t.Fatalf("activate missing scene: %s", text)
	}
	if text, isErr := call("simpletoggle_list_automations", map[string]any{}); isErr || !strings.Contains(text, `"rules"`) {
		t.Fatalf("list_automations: %s", text)
	}
	if text, isErr := call("simpletoggle_set_switch", map[string]any{"id": "lamp", "on": true}); isErr || !strings.Contains(text, `"state": "on"`) {
		t.Fatalf("set_switch: %s", text)
	}
	if text, isErr := call("simpletoggle_set_params", map[string]any{"id": "lamp", "params": map[string]any{"mode": "b"}}); isErr || !strings.Contains(text, `"value": "b"`) {
		t.Fatalf("set_params: %s", text)
	}
	if text, isErr := call("simpletoggle_run_action", map[string]any{"id": "flush"}); isErr || !strings.Contains(text, "flushed") {
		t.Fatalf("run_action: %s", text)
	}
	if text, isErr := call("simpletoggle_validate", map[string]any{"content": "#!/bin/sh\necho hi\n"}); isErr || !strings.Contains(text, `"valid": false`) {
		t.Fatalf("validate: %s", text)
	}
	if text, isErr := call("simpletoggle_write_script", map[string]any{"content": "#!/bin/sh\necho no header\n"}); !isErr || !strings.Contains(text, "Missing required directive") {
		t.Fatalf("write_script with errors: %v %s", isErr, text)
	}
	// AI 写脚本：先存成待确认变更，不写文件
	text, isErr := call("simpletoggle_write_script", map[string]any{"content": "#!/bin/sh\n# <switch.name> From AI\n# <switch.type> action\necho hi\n"})
	if isErr || !strings.Contains(text, `"pending": true`) || !strings.Contains(text, "waiting for the user's approval") {
		t.Fatalf("write_script should wait for approval: %v %s", isErr, text)
	}
	if _, err := os.Stat(filepath.Join(in.home, "switches", "from-ai.sh")); err == nil {
		t.Fatal("AI script written before approval")
	}
	if text, isErr := call("simpletoggle_change_status", map[string]any{"id": "chg-1"}); isErr || !strings.Contains(text, `"status": "pending"`) {
		t.Fatalf("change_status: %s", text)
	}
	// 删除脚本默认不允许 AI 执行
	if text, isErr := call("simpletoggle_delete", map[string]any{"id": "lamp"}); !isErr || !strings.Contains(text, "do not allow AI agents to delete scripts") {
		t.Fatalf("delete should be refused: %s", text)
	}
	// 改参数会让开着的开关异步重启，那条记录可能排在最前面，所以找到 run_action 的那一条再核对来源
	text, _ = call("simpletoggle_get_runs", map[string]any{"limit": 20})
	var runs []map[string]any
	if json.Unmarshal([]byte(text), &runs) != nil {
		t.Fatalf("get_runs: %s", text)
	}
	found := false
	for _, r := range runs {
		if r["switch"] == "flush.sh" && r["action"] == "run" {
			found = r["source"] == "ai" && r["detail"] == "e2e-client"
			break
		}
	}
	if !found {
		t.Fatalf("MCP provenance: %s", text)
	}
	if text, isErr := call("simpletoggle_get_script", map[string]any{"id": "ghost"}); !isErr || !strings.Contains(text, "no switch") {
		t.Fatalf("missing switch: %s", text)
	}
}

func TestIsTerminal(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if isTerminal(null) {
		t.Fatal("/dev/null is not a terminal (apps launched from Finder get it as stdout)")
	}
	f, _ := os.CreateTemp(t.TempDir(), "out")
	defer f.Close()
	if isTerminal(f) {
		t.Fatal("a file is not a terminal")
	}
}

// TestGuardStopsDaemonsOnCrash：引擎被强杀（崩溃）时，守护进程立刻结束遗留的常驻进程，
// 不用等到下次启动。
func TestGuardStopsDaemonsOnCrash(t *testing.T) {
	in := start(t)
	in.must("on", "serve")
	ledger := filepath.Join(in.home, "daemons.json")
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		var entries map[string]struct {
			PID int `json:"pid"`
		}
		if data, err := os.ReadFile(ledger); err == nil && json.Unmarshal(data, &entries) == nil {
			pid = entries["serve.sh"].PID
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("daemon not in ledger")
	}
	if err := in.serve.Process.Kill(); err != nil { // SIGKILL：Shutdown 不会执行
		t.Fatal(err)
	}
	_ = in.serve.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("daemon %d still running after the engine was killed", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(ledger); !os.IsNotExist(err) {
		t.Fatalf("ledger not removed: %v", err)
	}
}

func alive(pid int) bool {
	// 已退出但还没被回收的僵尸进程也算结束
	out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && len(out) > 0 && !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// TestGuardExitsOnCleanQuit：正常退出时守护进程随之结束，不留下进程。
func TestGuardExitsOnCleanQuit(t *testing.T) {
	in := start(t)
	in.must("on", "serve")
	pattern := "__guard " + strconv.Itoa(in.serve.Process.Pid) + " "
	deadline := time.Now().Add(5 * time.Second)
	for !guardRunning(pattern) {
		if time.Now().After(deadline) {
			t.Fatal("guard not started with the first daemon")
		}
		time.Sleep(50 * time.Millisecond)
	}
	in.serve.Process.Signal(os.Interrupt)
	_ = in.serve.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for guardRunning(pattern) {
		if time.Now().After(deadline) {
			t.Fatal("guard still running after a clean quit")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func guardRunning(pattern string) bool {
	out, _ := exec.Command("/bin/ps", "-axo", "stat=,command=").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, pattern) && !strings.HasPrefix(strings.TrimSpace(line), "Z") {
			return true
		}
	}
	return false
}

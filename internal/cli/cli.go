// Package cli 是 opentoggle 命令行：除 validate 外都通过本地控制 API 与运行中的 app 通信。
// 命令与输出格式与 v1 一致（skill 依赖它们），另有 v2 新增的 run 与 log。
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/keys"
	"github.com/HanlunWang/open-toggle/internal/paths"
	"github.com/HanlunWang/open-toggle/internal/platform/darwin"
	"github.com/HanlunWang/open-toggle/internal/version"
)

// Env 是 CLI 的输入输出，便于测试。
type Env struct {
	Out, Err io.Writer
	Client   *Client
}

type exit int

func (e *Env) fail(format string, a ...any) exit {
	fmt.Fprintf(e.Err, format+"\n", a...)
	return 1
}

// Run 执行一条命令并返回退出码。
func Run(args []string, env *Env) int {
	if env.Client == nil {
		env.Client = NewClient(CallerID())
	}
	jsonMode := false
	var pos []string
	for _, a := range args {
		switch {
		case a == "--json":
			jsonMode = true
		case strings.HasPrefix(a, "--"):
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		fmt.Fprintln(env.Out, usage())
		return 1
	}
	cmd, rest := pos[0], pos[1:]
	flag := func(name string) bool {
		for _, a := range args {
			if a == "--"+name {
				return true
			}
		}
		return false
	}
	return int(env.dispatch(cmd, rest, jsonMode, flag, args))
}

func (e *Env) dispatch(cmd string, rest []string, jsonMode bool, flag func(string) bool, args []string) exit {
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprintln(e.Out, usage())
		return 0
	case "version":
		fmt.Fprintf(e.Out, "OpenToggle %s\n", version.Version)
		return 0
	case "validate":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle validate <file.sh>")
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return e.fail("cannot read %s", rest[0])
		}
		issues := contract.Lint(string(data))
		e.reportIssues(issues, jsonMode)
		if contract.HasErrors(issues) {
			return 1
		}
		return 0
	case "press":
		return e.press(rest, flag("local"))
	case "doctor":
		e.doctor()
		return 0
	case "list":
		return e.list(jsonMode)
	case "on", "off":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle %s <id>", cmd)
		}
		return e.call("POST", "/v1/switches/"+PathEscape(rest[0])+"/"+cmd, nil)
	case "run":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle run <id>")
		}
		return e.run(rest[0], jsonMode)
	case "state":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle state <id>")
		}
		var sw struct{ State string }
		if code := e.get("/v1/switches/"+PathEscape(rest[0]), &sw); code != 0 {
			return code
		}
		fmt.Fprintln(e.Out, sw.State)
		return 0
	case "enable", "disable":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle %s <id>", cmd)
		}
		return e.call("POST", "/v1/switches/"+PathEscape(rest[0])+"/"+cmd, nil)
	case "params":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle params <id>")
		}
		return e.params(rest[0], jsonMode)
	case "set":
		if len(rest) < 2 {
			return e.fail("usage: opentoggle set <id> <key>=<value> [...]")
		}
		obj := map[string]string{}
		for _, pair := range rest[1:] {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return e.fail("expected key=value, got %q", pair)
			}
			obj[k] = v
		}
		body, _ := json.Marshal(obj)
		return e.call("PUT", "/v1/switches/"+PathEscape(rest[0])+"/params", body)
	case "cat":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle cat <id>")
		}
		return e.call("GET", "/v1/switches/"+PathEscape(rest[0])+"/script", nil)
	case "add", "put":
		update := cmd == "put"
		idx := 0
		if update {
			idx = 1
		}
		if len(rest) <= idx {
			if update {
				return e.fail("usage: opentoggle put <id> <file.sh>")
			}
			return e.fail("usage: opentoggle add <file.sh>")
		}
		data, err := os.ReadFile(rest[idx])
		if err != nil {
			return e.fail("cannot read %s", rest[idx])
		}
		// 先本地检查，错误尽早失败（服务端会再查一次）
		issues := contract.Lint(string(data))
		e.reportIssues(issues, false)
		if contract.HasErrors(issues) {
			return 1
		}
		if update {
			return e.call("PUT", "/v1/switches/"+PathEscape(rest[0])+"/script", data)
		}
		return e.call("POST", "/v1/scripts", data)
	case "rm":
		if len(rest) == 0 {
			return e.fail("usage: opentoggle rm <id>")
		}
		return e.call("DELETE", "/v1/switches/"+PathEscape(rest[0]), nil)
	case "scenes":
		return e.scenes(jsonMode)
	case "scene":
		if len(rest) < 1 {
			return e.fail("usage: opentoggle scene <id|name> [--restore]")
		}
		action := "toggle"
		if flag("restore") {
			action = "restore"
		} else if flag("activate") {
			action = "activate"
		}
		return e.scene(rest[0], action, jsonMode)
	case "log":
		return e.log(rest, jsonMode, args)
	}
	return e.fail("unknown command %q\n\n%s", cmd, usage())
}

// call 发请求并把响应体原样打印；失败时把响应体打到 stderr 并返回 1（与 v1 一致）。
func (e *Env) call(method, path string, body []byte) exit {
	code, data, err := e.Client.Do(method, path, body)
	if err != nil {
		return e.fail("%v", err)
	}
	if code >= 400 {
		e.Err.Write(data)
		fmt.Fprintln(e.Err)
		return 1
	}
	e.Out.Write(data)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		fmt.Fprintln(e.Out)
	}
	return 0
}

func (e *Env) get(path string, v any) exit {
	code, data, err := e.Client.Do("GET", path, nil)
	if err != nil {
		return e.fail("%v", err)
	}
	if code >= 400 {
		e.Err.Write(data)
		fmt.Fprintln(e.Err)
		return 1
	}
	if err := json.Unmarshal(data, v); err != nil {
		return e.fail("unexpected response")
	}
	return 0
}

type switchOut struct {
	ID, Name, State, Type string
	Enabled               bool
	Params                []struct {
		Key, Type, Value string
		DefaultValue     string `json:"defaultValue"`
		Options          []struct{ Label, Value string }
	}
}

func (e *Env) list(jsonMode bool) exit {
	if jsonMode {
		return e.call("GET", "/v1/switches", nil)
	}
	var list []switchOut
	if code := e.get("/v1/switches", &list); code != 0 {
		return code
	}
	dots := map[string]string{"on": "●", "off": "○", "error": "✗", "unknown": "?"}
	for _, sw := range list {
		dot := dots[sw.State]
		if dot == "" {
			dot = "?"
		}
		hidden := ""
		if !sw.Enabled {
			hidden = "  [hidden]"
		}
		fmt.Fprintf(e.Out, "%s %-22s %-8s %-7s %s%s\n", dot, sw.ID, sw.State, sw.Type, sw.Name, hidden)
	}
	return 0
}

func (e *Env) params(id string, jsonMode bool) exit {
	if jsonMode {
		return e.call("GET", "/v1/switches/"+PathEscape(id), nil)
	}
	var sw switchOut
	if code := e.get("/v1/switches/"+PathEscape(id), &sw); code != 0 {
		return code
	}
	for _, p := range sw.Params {
		extra := ""
		if len(p.Options) > 0 {
			var opts []string
			for _, o := range p.Options {
				opts = append(opts, o.Label+"="+o.Value)
			}
			extra = "  options: " + strings.Join(opts, " | ")
		}
		fmt.Fprintf(e.Out, "%s = %s  (%s, default %s)%s\n", p.Key, p.Value, p.Type, p.DefaultValue, extra)
	}
	return 0
}

func (e *Env) run(id string, jsonMode bool) exit {
	code, data, err := e.Client.Do("POST", "/v2/switches/"+PathEscape(id)+"/run", nil)
	if err != nil {
		return e.fail("%v", err)
	}
	if code >= 400 {
		e.Err.Write(data)
		fmt.Fprintln(e.Err)
		return 1
	}
	if jsonMode {
		e.Out.Write(append(data, '\n'))
		return 0
	}
	var res struct {
		Run *struct {
			OK         bool
			ExitCode   int
			DurationMs int64
			Output     string
		}
	}
	_ = json.Unmarshal(data, &res)
	if res.Run == nil {
		fmt.Fprintln(e.Out, "still running (check `opentoggle log`)")
		return 0
	}
	if out := strings.TrimRight(res.Run.Output, "\n"); out != "" {
		fmt.Fprintln(e.Out, out)
	}
	if !res.Run.OK {
		return e.fail("failed: exit %d after %dms", res.Run.ExitCode, res.Run.DurationMs)
	}
	return 0
}

func (e *Env) log(rest []string, jsonMode bool, args []string) exit {
	q := []string{"limit=20"}
	for i, a := range args {
		if a == "--limit" && i+1 < len(args) {
			q[0] = "limit=" + args[i+1]
		}
		if a == "--failed" {
			q = append(q, "failed=1")
		}
	}
	if len(rest) > 0 {
		if _, err := strconv.Atoi(rest[0]); err != nil { // --limit 的值也会落在 rest 里
			q = append(q, "switch="+PathEscape(rest[0]))
		}
	}
	path := "/v2/runs?" + strings.Join(q, "&")
	if jsonMode {
		return e.call("GET", path, nil)
	}
	var runs []struct {
		Time       time.Time
		Switch     string
		Action     string
		Source     string
		Detail     string
		DurationMs int64
		ExitCode   int
		OK         bool
	}
	if code := e.get(path, &runs); code != 0 {
		return code
	}
	for _, r := range runs {
		mark := "✓"
		if !r.OK {
			mark = fmt.Sprintf("✗ exit %d", r.ExitCode)
		}
		src := r.Source
		if r.Detail != "" {
			src += " (" + r.Detail + ")"
		}
		fmt.Fprintf(e.Out, "%s  %-22s %-6s %-28s %6dms  %s\n", r.Time.Local().Format("01-02 15:04:05"), r.Switch, r.Action, src, r.DurationMs, mark)
	}
	return 0
}

func (e *Env) press(rest []string, local bool) exit {
	if len(rest) == 0 {
		return e.fail("usage: opentoggle press <keyspec>   e.g. f15, cmd+shift+k, mouse:middle")
	}
	spec, ok := keys.Parse(rest[0])
	if !ok {
		return e.fail("invalid key spec %q — expected e.g. f15, cmd+shift+k, mouse:middle", rest[0])
	}
	// 优先让运行中的 app 代发：辅助功能授权只需给 app 一个长期进程
	if !local {
		code, data, err := e.Client.Do("POST", "/v1/press", []byte(spec.Canonical()))
		if err == nil {
			if code < 400 {
				return 0
			}
			e.Err.Write(data)
			fmt.Fprintln(e.Err)
			return 1
		}
	}
	var res darwin.PostResult
	if spec.Target == keys.TargetMouse {
		res = darwin.PostMouse(int(spec.Button))
	} else {
		res = darwin.PostKey(spec.Code, spec.Flags())
	}
	switch res {
	case darwin.Delivered:
		return 0
	case darwin.NotTrusted:
		// 不在这里弹授权框：命令行进程短命且身份随调用方变化，授权应当只给 app
		return e.fail("OpenToggle is not running and this process has no Accessibility permission — open the app (it routes key presses) or grant Accessibility to your terminal")
	case darwin.NotDelivered:
		return e.fail("key event was rejected by the system — re-grant Accessibility")
	}
	return e.fail("failed to create the key event")
}

func (e *Env) reportIssues(issues []contract.Issue, jsonMode bool) {
	if jsonMode {
		if issues == nil {
			issues = []contract.Issue{}
		}
		data, _ := json.Marshal(issues)
		e.Out.Write(append(data, '\n'))
		return
	}
	for _, i := range issues {
		fmt.Fprintf(e.Out, "%s: %s\n", i.Severity, i.Message)
	}
	if len(issues) == 0 {
		fmt.Fprintln(e.Out, "ok: no issues")
	}
}

// ───────── doctor ─────────

func (e *Env) doctor() {
	exe, _ := os.Executable()
	fmt.Fprintf(e.Out, "OpenToggle %s\n", version.Version)
	fmt.Fprintf(e.Out, "binary:        %s\n", exe)
	code, data, err := e.Client.Do("GET", "/v2/ping", nil)
	var ping struct {
		Version       string
		Accessibility bool
		PID           int
	}
	running := err == nil && code == 200 && json.Unmarshal(data, &ping) == nil
	if running {
		fmt.Fprintf(e.Out, "app:           running (version %s)\n", ping.Version)
		if cpu := psField(ping.PID, "pcpu="); cpu != "" {
			note := ""
			if f, _ := strconv.ParseFloat(cpu, 64); f > 20 {
				note = "  ← unusually high, please report"
			}
			fmt.Fprintf(e.Out, "app cpu:       %s%%%s\n", cpu, note)
		}
		if ping.Accessibility {
			fmt.Fprintln(e.Out, "accessibility: granted (app process)")
		} else {
			fmt.Fprintln(e.Out, "accessibility: NOT GRANTED for the app process")
			fmt.Fprintln(e.Out, "               → System Settings → Privacy & Security → Accessibility → enable OpenToggle")
		}
	} else {
		fmt.Fprintln(e.Out, "app:           not running (GUI-dependent commands will fail)")
		state := "NOT GRANTED for this CLI process"
		if darwin.AccessibilityTrusted() {
			state = "granted (this CLI process)"
		}
		fmt.Fprintf(e.Out, "accessibility: %s\n", state)
	}
	if idle, ok := darwin.IdleSeconds(); ok {
		fmt.Fprintf(e.Out, "system idle:   %.1fs\n", idle)
	}
	e.reportDaemons(ping.PID)
	if !running {
		return
	}
	fmt.Fprint(e.Out, "press test:    ")
	code, data, err = e.Client.Do("POST", "/v1/press", []byte("f15"))
	switch {
	case err != nil:
		fmt.Fprintln(e.Out, "FAILED — app unreachable")
	case code < 400:
		fmt.Fprintln(e.Out, "OK — F15 delivered via the app process")
	default:
		fmt.Fprintf(e.Out, "FAILED via app — %s\n", strings.TrimSpace(string(data)))
	}
}

// reportDaemons 根据 PID 账本列出常驻进程；父进程不是 app 的为遗留进程。
func (e *Env) reportDaemons(appPID int) {
	data, err := os.ReadFile(paths.Ledger())
	if err == nil {
		var ledger map[string]struct{ PID int }
		if json.Unmarshal(data, &ledger) != nil {
			var v1 map[string]int
			if json.Unmarshal(data, &v1) == nil {
				ledger = map[string]struct{ PID int }{}
				for id, pid := range v1 {
					ledger[id] = struct{ PID int }{pid}
				}
			}
		}
		running := 0
		var orphans []string
		for _, ent := range ledger {
			ppid := psField(ent.PID, "ppid=")
			if ppid == "" {
				continue
			}
			running++
			if p, _ := strconv.Atoi(ppid); p != appPID {
				orphans = append(orphans, strconv.Itoa(ent.PID))
			}
		}
		if running > 0 {
			line := fmt.Sprintf("daemons:       %d running", running)
			if len(orphans) > 0 {
				line += fmt.Sprintf("  ← ORPHANED pid(s) %s not owned by the app; kill with: kill %s", strings.Join(orphans, ", "), strings.Join(orphans, " "))
			}
			fmt.Fprintln(e.Out, line)
		}
	}
	out, _ := exec.Command("/usr/bin/pmset", "-g", "assertions").Output()
	if n := strings.Count(string(out), "caffeinate"); n > 0 {
		fmt.Fprintf(e.Out, "sleep asserts: %d held by caffeinate (screen/system kept awake)\n", n)
	}
}

func psField(pid int, field string) string {
	if pid <= 0 {
		return ""
	}
	out, err := exec.Command("/bin/ps", "-o", field, "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func usage() string {
	return fmt.Sprintf(`OpenToggle %s — script-powered menu bar switches

usage: opentoggle <command> [args] [--json]

  list                     list switches with state
  on|off <id>              turn a switch on/off
  run <id>                 run an action switch and print its output
  state <id>               print on|off|error|unknown
  enable|disable <id>      show/hide in the menu bar panel (visibility only:
                           disable does NOT stop a running switch — use off)
  params <id>              list parameters and current values
  set <id> k=v [...]       set parameter values (restarts a running switch)
  cat <id>                 print the script source
  add <file.sh>            validate + install a new switch script
  put <id> <file.sh>       validate + replace an existing switch's script
  rm <id>                  delete a switch (moves script to Trash)
  scenes                   list scenes (● = active)
  scene <id|name>          activate a scene, or restore it when active;
                           --activate / --restore to force one
  log [id] [--failed] [--limit N]
                           recent runs: source, duration, exit code
  validate <file.sh>       lint a script against the contract (offline)
  press <keyspec>          synthesize a key/mouse event (f15, cmd+shift+k,
                           mouse:middle); routed through the running app so
                           Accessibility is granted once, --local to bypass
  doctor                   diagnose app/permission/key-delivery problems
  mcp                      run as an MCP stdio server (for AI agents)
  version | help

<id> is the script file name (keep-awake.sh; the .sh suffix is optional).
All commands except validate/press/mcp/help require the OpenToggle app running.`, version.Version)
}

func (e *Env) scenes(jsonMode bool) exit {
	if jsonMode {
		return e.call("GET", "/v2/scenes", nil)
	}
	var list []struct {
		ID, Name string
		Active   bool
		Shortcut string
		Steps    []struct{ Switch, Action string }
	}
	if code := e.get("/v2/scenes", &list); code != 0 {
		return code
	}
	if len(list) == 0 {
		fmt.Fprintln(e.Out, "no scenes (create one in the workbench)")
	}
	for _, sc := range list {
		dot := "○"
		if sc.Active {
			dot = "●"
		}
		var steps []string
		for _, st := range sc.Steps {
			steps = append(steps, st.Switch+" "+st.Action)
		}
		fmt.Fprintf(e.Out, "%s %-16s %-14s %s\n", dot, sc.ID, sc.Name, strings.Join(steps, ", "))
	}
	return 0
}

func (e *Env) scene(id, action string, jsonMode bool) exit {
	code, data, err := e.Client.Do("POST", "/v2/scenes/"+PathEscape(id)+"/"+action, nil)
	if err != nil {
		return e.fail("%v", err)
	}
	if code >= 400 {
		e.Err.Write(data)
		fmt.Fprintln(e.Err)
		return 1
	}
	if jsonMode {
		e.Out.Write(append(data, '\n'))
		return 0
	}
	var res struct {
		Run *struct {
			OK     bool
			Action string
			Output string
		}
	}
	_ = json.Unmarshal(data, &res)
	if res.Run == nil {
		fmt.Fprintln(e.Out, "still running (check `opentoggle log`)")
		return 0
	}
	if out := strings.TrimRight(res.Run.Output, "\n"); out != "" {
		fmt.Fprintln(e.Out, out)
	}
	if !res.Run.OK {
		return e.fail("some steps failed")
	}
	return 0
}

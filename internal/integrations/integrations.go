// Package integrations 把 OpenToggle 的 MCP 服务器接入常见的 AI 客户端（Claude Code、
// Cursor、Codex），并安装 agent skill。修改配置文件前先备份成 .bak；界面在执行前
// 展示将要写入的内容（Plan），用户确认后才 Apply。
package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 客户端。
const (
	ClaudeCode = "claude-code"
	Cursor     = "cursor"
	Codex      = "codex"
)

// Clients 是支持一键接入的客户端（界面按这个顺序显示）。
var Clients = []string{ClaudeCode, Cursor, Codex}

// ServerName 是写进各客户端配置的 MCP 服务器名称。
const ServerName = "opentoggle"

var ErrNoCLI = errors.New("the claude command was not found")

// Env 是检测与修改时用到的环境（测试里替换）。
type Env struct {
	Home string
	Bin  string // opentoggle 二进制的绝对路径
	// LookPath 查找命令；默认在 PATH 之外也查 Homebrew 与 ~/.local/bin 等常见位置。
	LookPath func(name string) (string, error)
	// Run 执行命令并返回合并输出。
	Run func(name string, args ...string) ([]byte, error)
	// AppExists 报告 /Applications 下是否有某个 app。
	AppExists func(name string) bool
}

// Default 返回当前用户的环境。
func Default(bin string) Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, Bin: bin}
}

func (e Env) lookPath(name string) (string, error) {
	if e.LookPath != nil {
		return e.LookPath(name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range []string{filepath.Join(e.Home, ".claude", "local"), filepath.Join(e.Home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin"} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func (e Env) run(name string, args ...string) ([]byte, error) {
	if e.Run != nil {
		return e.Run(name, args...)
	}
	return exec.Command(name, args...).CombinedOutput()
}

func (e Env) appExists(name string) bool {
	if e.AppExists != nil {
		return e.AppExists(name)
	}
	_, err := os.Stat(filepath.Join("/Applications", name+".app"))
	return err == nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Status 是一个客户端的接入状态。
type Status struct {
	Client    string
	Present   bool   // 客户端装了
	Connected bool   // 配置里已有 opentoggle
	Command   string // 配置里的命令
	Current   bool   // 配置指向当前的二进制
	File      string // 涉及的配置文件
}

// Detect 返回全部客户端的状态。
func (e Env) Detect() []Status {
	out := make([]Status, 0, len(Clients))
	for _, c := range Clients {
		out = append(out, e.Status(c))
	}
	return out
}

// Status 返回一个客户端的状态。
func (e Env) Status(client string) Status {
	st := Status{Client: client}
	switch client {
	case ClaudeCode:
		st.File = filepath.Join(e.Home, ".claude.json")
		_, err := e.lookPath("claude")
		st.Present = err == nil || exists(filepath.Join(e.Home, ".claude"))
		if data, err := os.ReadFile(st.File); err == nil {
			var cfg struct {
				MCPServers map[string]struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
				} `json:"mcpServers"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				if s, ok := cfg.MCPServers[ServerName]; ok {
					st.Connected, st.Command = true, s.Command
				}
			}
		}
	case Cursor:
		st.File = filepath.Join(e.Home, ".cursor", "mcp.json")
		st.Present = exists(filepath.Join(e.Home, ".cursor")) || e.appExists("Cursor")
		if data, err := os.ReadFile(st.File); err == nil {
			var cfg map[string]any
			if json.Unmarshal(data, &cfg) == nil {
				if servers, ok := cfg["mcpServers"].(map[string]any); ok {
					if s, ok := servers[ServerName].(map[string]any); ok {
						st.Connected = true
						st.Command, _ = s["command"].(string)
					}
				}
			}
		}
	case Codex:
		st.File = filepath.Join(e.Home, ".codex", "config.toml")
		_, err := e.lookPath("codex")
		st.Present = err == nil || exists(filepath.Join(e.Home, ".codex"))
		if data, err := os.ReadFile(st.File); err == nil {
			if sec := codexSection(string(data)); sec != "" {
				st.Connected = true
				if m := reCodexCommand.FindStringSubmatch(sec); m != nil {
					if v, err := strconv.Unquote(m[1]); err == nil {
						st.Command = v
					} else {
						st.Command = strings.Trim(m[1], `"'`)
					}
				}
			}
		}
	}
	st.Current = st.Connected && sameFile(st.Command, e.Bin)
	return st
}

// sameFile 判断两个路径是否指向同一个文件（Homebrew 的软链接与 app 里的二进制算同一个）。
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// Plan 是一次接入要做的事：修改一个文件（Before → After），或执行几条命令。
type Plan struct {
	Client   string
	File     string
	Before   string
	After    string
	Commands [][]string
}

// Describe 返回给用户看的说明（确认框里显示）。
func (p Plan) Describe() string {
	if len(p.Commands) > 0 {
		var lines []string
		for _, c := range p.Commands {
			lines = append(lines, shellJoin(c))
		}
		return strings.Join(lines, "\n")
	}
	return p.After
}

func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"$\\") {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

// Plan 生成接入一个客户端的计划（不修改任何东西）。
func (e Env) Plan(client string) (Plan, error) {
	p := Plan{Client: client}
	switch client {
	case ClaudeCode:
		claude, err := e.lookPath("claude")
		if err != nil {
			return p, ErrNoCLI
		}
		if st := e.Status(ClaudeCode); st.Connected {
			p.Commands = append(p.Commands, []string{claude, "mcp", "remove", "--scope", "user", ServerName})
		}
		p.Commands = append(p.Commands, []string{claude, "mcp", "add", "--scope", "user", ServerName, "--", e.Bin, "mcp"})
		return p, nil
	case Cursor:
		p.File = filepath.Join(e.Home, ".cursor", "mcp.json")
		cfg := map[string]any{}
		if data, err := os.ReadFile(p.File); err == nil {
			p.Before = string(data)
			if len(bytes.TrimSpace(data)) > 0 {
				if err := json.Unmarshal(data, &cfg); err != nil {
					return p, fmt.Errorf("%s is not valid JSON: %w", p.File, err)
				}
			}
		}
		servers, _ := cfg["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		servers[ServerName] = map[string]any{"command": e.Bin, "args": []string{"mcp"}}
		cfg["mcpServers"] = servers
		out, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return p, err
		}
		p.After = string(out) + "\n"
		return p, nil
	case Codex:
		p.File = filepath.Join(e.Home, ".codex", "config.toml")
		if data, err := os.ReadFile(p.File); err == nil {
			p.Before = string(data)
		}
		body := removeCodexSection(p.Before)
		body = strings.TrimRight(body, "\n")
		if body != "" {
			body += "\n\n"
		}
		p.After = body + codexBlock(e.Bin)
		return p, nil
	}
	return p, fmt.Errorf("unknown client %q", client)
}

// Apply 执行计划：写文件前把原文件备份成 .bak。
func (e Env) Apply(p Plan) error {
	if len(p.Commands) > 0 {
		for _, c := range p.Commands {
			out, err := e.run(c[0], c[1:]...)
			if err != nil {
				// remove 在没有旧配置时失败不要紧；add 失败才算失败
				if len(c) > 2 && c[2] == "remove" {
					continue
				}
				return fmt.Errorf("%s: %v: %s", shellJoin(c), err, strings.TrimSpace(string(out)))
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.File), 0o755); err != nil {
		return err
	}
	if p.Before != "" {
		if err := os.WriteFile(p.File+".bak", []byte(p.Before), 0o600); err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(p.File); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(p.File, []byte(p.After), mode)
}

// Snippet 是给其他 MCP 客户端的通用配置。
func Snippet(bin string) string {
	b, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{ServerName: map[string]any{"command": bin, "args": []string{"mcp"}}}}, "", "  ")
	return string(b)
}

// ───────── Codex 的 TOML（只处理我们自己的那一节） ─────────

var reCodexCommand = regexp.MustCompile(`(?m)^\s*command\s*=\s*(".*"|'.*')\s*$`)

func isCodexHeader(line string) bool {
	t := strings.TrimSpace(line)
	return t == "[mcp_servers."+ServerName+"]" || strings.HasPrefix(t, "[mcp_servers."+ServerName+".")
}

// codexSection 返回 [mcp_servers.opentoggle] 一节（含子表）的文本。
func codexSection(toml string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(toml, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = isCodexHeader(line)
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// removeCodexSection 删掉 [mcp_servers.opentoggle] 一节（含子表），其余原样保留。
func removeCodexSection(toml string) string {
	var out []string
	in := false
	for _, line := range strings.Split(toml, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = isCodexHeader(line)
		}
		if !in {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func codexBlock(bin string) string {
	return "[mcp_servers." + ServerName + "]\ncommand = " + strconv.Quote(bin) + "\nargs = [\"mcp\"]\n"
}

// ───────── skill ─────────

// SkillPath 是 Claude Code 读取 skill 的位置。
func (e Env) SkillPath() string { return filepath.Join(e.Home, ".claude", "skills", ServerName) }

// SkillStatus 报告 skill 是否已安装，以及它指向哪里（软链接时）。
func (e Env) SkillStatus() (installed bool, target string) {
	p := e.SkillPath()
	fi, err := os.Lstat(p)
	if err != nil {
		return false, ""
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t, _ := os.Readlink(p)
		return exists(filepath.Join(p, "SKILL.md")), t
	}
	return exists(filepath.Join(p, "SKILL.md")), p
}

// InstallSkill 把 skill 链接到 ~/.claude/skills/opentoggle。原来是一个普通目录时先改名为 .bak。
func (e Env) InstallSkill(src string) error {
	if !exists(filepath.Join(src, "SKILL.md")) {
		return fmt.Errorf("no SKILL.md in %s", src)
	}
	p := e.SkillPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(p); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(p); err != nil {
				return err
			}
		} else if err := os.Rename(p, p+".bak"); err != nil {
			return err
		}
	}
	return os.Symlink(src, p)
}

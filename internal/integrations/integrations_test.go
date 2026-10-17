package integrations

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv(t *testing.T) (Env, *[]string) {
	t.Helper()
	home := t.TempDir()
	var ran []string
	env := Env{
		Home: home, Bin: "/Applications/SimpleToggle.app/Contents/MacOS/simpletoggle",
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return "/opt/homebrew/bin/claude", nil
			}
			return "", exec.ErrNotFound
		},
		Run: func(name string, args ...string) ([]byte, error) {
			ran = append(ran, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		AppExists: func(string) bool { return false },
	}
	return env, &ran
}

func TestCursor(t *testing.T) {
	env, _ := testEnv(t)
	if st := env.Status(Cursor); st.Present || st.Connected {
		t.Fatalf("fresh home: %+v", st)
	}
	dir := filepath.Join(env.Home, ".cursor")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers": {"other": {"command": "x"}}, "keep": true}`), 0o600)
	p, err := env.Plan(Cursor)
	if err != nil || !strings.Contains(p.After, `"simpletoggle"`) || !strings.Contains(p.After, `"other"`) || !strings.Contains(p.After, `"keep": true`) {
		t.Fatalf("plan: %v\n%s", err, p.After)
	}
	if err := env.Apply(p); err != nil {
		t.Fatal(err)
	}
	st := env.Status(Cursor)
	if !st.Present || !st.Connected || !st.Current {
		t.Fatalf("after apply: %+v", st)
	}
	if bak, _ := os.ReadFile(filepath.Join(dir, "mcp.json.bak")); !strings.Contains(string(bak), `"other"`) {
		t.Fatal("no backup")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "mcp.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed: %v", fi.Mode())
	}
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{not json"), 0o600)
	if _, err := env.Plan(Cursor); err == nil {
		t.Fatal("broken JSON should not be overwritten")
	}
}

func TestCodex(t *testing.T) {
	env, _ := testEnv(t)
	dir := filepath.Join(env.Home, ".codex")
	os.MkdirAll(dir, 0o755)
	old := "model = \"gpt-5\"\n\n[mcp_servers.simpletoggle]\ncommand = \"/old/simpletoggle\"\nargs = [\"mcp\"]\n\n[mcp_servers.simpletoggle.env]\nX = \"1\"\n\n[mcp_servers.other]\ncommand = \"y\"\n"
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(old), 0o644)
	st := env.Status(Codex)
	if !st.Connected || st.Command != "/old/simpletoggle" || st.Current {
		t.Fatalf("detect old: %+v", st)
	}
	p, _ := env.Plan(Codex)
	if strings.Contains(p.After, "/old/simpletoggle") || strings.Contains(p.After, "X = \"1\"") || !strings.Contains(p.After, "[mcp_servers.other]") || !strings.Contains(p.After, "model = \"gpt-5\"") {
		t.Fatalf("plan:\n%s", p.After)
	}
	if strings.Count(p.After, "[mcp_servers.simpletoggle]") != 1 {
		t.Fatalf("duplicate section:\n%s", p.After)
	}
	env.Apply(p)
	if st := env.Status(Codex); !st.Current {
		t.Fatalf("after apply: %+v", st)
	}
	// 没有配置文件时新建
	env2, _ := testEnv(t)
	p2, _ := env2.Plan(Codex)
	if p2.After != "[mcp_servers.simpletoggle]\ncommand = \"/Applications/SimpleToggle.app/Contents/MacOS/simpletoggle\"\nargs = [\"mcp\"]\n" {
		t.Fatalf("fresh:\n%q", p2.After)
	}
}

func TestClaudeCode(t *testing.T) {
	env, ran := testEnv(t)
	p, err := env.Plan(ClaudeCode)
	if err != nil || len(p.Commands) != 1 || !strings.Contains(p.Describe(), "mcp add --scope user simpletoggle -- /Applications/SimpleToggle.app/Contents/MacOS/simpletoggle mcp") {
		t.Fatalf("plan: %v %q", err, p.Describe())
	}
	os.WriteFile(filepath.Join(env.Home, ".claude.json"), []byte(`{"mcpServers": {"simpletoggle": {"command": "/Users/x/.local/bin/simpletoggle", "args": ["mcp"]}}}`), 0o600)
	st := env.Status(ClaudeCode)
	if !st.Present || !st.Connected || st.Current {
		t.Fatalf("detect v1: %+v", st)
	}
	p, _ = env.Plan(ClaudeCode)
	if len(p.Commands) != 2 || p.Commands[0][2] != "remove" {
		t.Fatalf("replace plan: %v", p.Commands)
	}
	if err := env.Apply(p); err != nil || len(*ran) != 2 {
		t.Fatalf("apply: %v %v", err, *ran)
	}
	env.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if _, err := env.Plan(ClaudeCode); err != ErrNoCLI {
		t.Fatalf("no CLI: %v", err)
	}
}

func TestSkill(t *testing.T) {
	env, _ := testEnv(t)
	src := filepath.Join(t.TempDir(), "skill")
	os.MkdirAll(src, 0o755)
	if err := env.InstallSkill(src); err == nil {
		t.Fatal("installed a skill without SKILL.md")
	}
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# skill"), 0o644)
	// 已有一个普通目录：改名为 .bak
	os.MkdirAll(env.SkillPath(), 0o755)
	if err := env.InstallSkill(src); err != nil {
		t.Fatal(err)
	}
	if ok, target := env.SkillStatus(); !ok || target != src {
		t.Fatalf("status %v %q", ok, target)
	}
	if _, err := os.Stat(env.SkillPath() + ".bak"); err != nil {
		t.Fatal("existing directory not kept")
	}
	if err := env.InstallSkill(src); err != nil { // 再装一次：替换链接
		t.Fatal(err)
	}
	if !strings.Contains(Snippet("/bin/ot"), `"command": "/bin/ot"`) {
		t.Fatal("snippet")
	}
}

func TestSymlinkCountsAsCurrent(t *testing.T) {
	env, _ := testEnv(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "simpletoggle")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	link := filepath.Join(dir, "link")
	os.Symlink(bin, link)
	env.Bin = bin
	os.MkdirAll(filepath.Join(env.Home, ".cursor"), 0o755)
	os.WriteFile(filepath.Join(env.Home, ".cursor", "mcp.json"), []byte(`{"mcpServers":{"simpletoggle":{"command":"`+link+`"}}}`), 0o600)
	if st := env.Status(Cursor); !st.Current {
		t.Fatalf("symlink to the same binary should count as current: %+v", st)
	}
}

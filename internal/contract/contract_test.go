package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseV1Presets(t *testing.T) {
	files, err := filepath.Glob("../presets/switches/*.sh")
	if err != nil || len(files) == 0 {
		t.Fatalf("no preset scripts found: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		h, ok := Parse(string(data))
		if !ok {
			t.Errorf("%s: not parsed as a switch", filepath.Base(f))
			continue
		}
		if issues := Lint(string(data)); HasErrors(issues) {
			t.Errorf("%s: lint errors %v", filepath.Base(f), issues)
		}
		if _, ok := LucideForSF(strings.TrimPrefix(h.Icon, "sf:")); strings.HasPrefix(h.Icon, "sf:") && !ok {
			t.Errorf("%s: icon %q has no lucide mapping", filepath.Base(f), h.Icon)
		}
	}
}

func TestParseKeepAwake(t *testing.T) {
	src := `#!/bin/zsh
# <switch.name> Keep Awake
# <switch.icon> sf:cup.and.saucer.fill
# <switch.type> daemon
# <switch.param> key=mode type=select label=模式 default=d options="仅保持屏幕=d|屏幕与任务=dis"
# <switch.param> key=duration type=number label="时长 (分钟)" default=0 min=0 max=1440 presets="不限=0|1 小时=60"
# <switch.menubar> mode=add icon=☕️ countdown=on
exec caffeinate -d
`
	h, ok := Parse(src)
	if !ok || h.Name != "Keep Awake" || h.Type != Daemon || len(h.Params) != 2 {
		t.Fatalf("parse = %+v ok=%v", h, ok)
	}
	mode := h.Params[0]
	if mode.Type != ParamSelect || mode.Default != "d" || len(mode.Options) != 2 || mode.Options[1].Value != "dis" || mode.Label != "模式" {
		t.Errorf("mode = %+v", mode)
	}
	dur := h.Params[1]
	if dur.Label != "时长 (分钟)" || dur.Min == nil || *dur.Min != 0 || dur.Max == nil || *dur.Max != 1440 || len(dur.Presets) != 2 || dur.Presets[1].Label != "1 小时" {
		t.Errorf("duration = %+v", dur)
	}
	if h.MenuBar == nil || h.MenuBar.Mode != MenuBarAdd || !h.MenuBar.Countdown || h.MenuBar.Icon != "☕️" {
		t.Errorf("menubar = %+v", h.MenuBar)
	}
	if dur.EnvName() != "SWITCH_DURATION" || EnvName("my-key.x") != "SWITCH_MY_KEY_X" {
		t.Errorf("env names")
	}
	if k, v := ResolveIcon(h.Icon); k != IconLucide || v != "coffee" {
		t.Errorf("resolve icon = %v %q", k, v)
	}
}

func TestParseDefaults(t *testing.T) {
	h, ok := Parse("#!/bin/sh\n# <switch.name> X\n# <switch.type> bogus\n# <switch.param> key=a type=select options=\"A=1|B=2\"\n")
	if !ok || h.Type != Toggle || h.Icon != DefaultIcon || h.Params[0].Default != "1" || h.Params[0].Label != "a" {
		t.Fatalf("%+v", h)
	}
	if _, ok := Parse("#!/bin/sh\necho hi\n"); ok {
		t.Fatal("script without name parsed as a switch")
	}
	// 指令只在前 40 行内有效
	if _, ok := Parse(strings.Repeat("\n", 45) + "# <switch.name> Late\n"); ok {
		t.Fatal("directive after line 40 was honoured")
	}
	if h, _ := Parse("# <switch.name> A\n# <switch.type> action\n"); h.Type != Action {
		t.Fatal("action type not parsed")
	}
}

func TestLintMessages(t *testing.T) {
	cases := []struct {
		src  string
		want string // 必须出现的消息片段
		sev  string
	}{
		{"echo hi\n", "Missing shebang", "warning"},
		{"#!/bin/sh\necho hi\n", "Missing required directive: # <switch.name>", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.type> lamp\necho\n", `Invalid <switch.type> "lamp"`, "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> type=text\necho status\n", "missing required attribute key=", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> key=a\n# <switch.param> key=a\necho status\n", `duplicate param key "a"`, "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> key=a type=select\necho status\n", "type=select requires options", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> key=a type=number min=x\necho status\n", "min= must be an integer", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> key=a type=number min=5 max=1\necho status\n", "min (5) > max (1)", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> key=k type=key default=hyper+q\necho status\n", "is not a valid key spec", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.menubar> mode=add\necho status\n", "missing required attribute icon=", "error"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.menubar> icon=x countdown=on\necho status\n", "countdown=on requires a number param", "warning"},
		{"#!/bin/sh\n# <switch.name> A\necho on\n", `does not reference "status"`, "warning"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.type> daemon\nsleep 9\n", "neither uses `exec` nor traps TERM", "warning"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.type> action\ncase \"$1\" in on) ;; esac\n", "run without arguments", "warning"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.icon> sf:not.a.symbol\necho status\n", "has no OpenToggle icon mapping", "warning"},
		{"#!/bin/sh\n# <switch.name> A\n", "no executable statements", "warning"},
		{"#!/bin/sh\n# <switch.name> A\n# <switch.param> key=a-b\n# <switch.param> key=a_b\necho status\n", "same environment variable", "error"},
	}
	for _, c := range cases {
		issues := Lint(c.src)
		found := false
		for _, i := range issues {
			if strings.Contains(i.Message, c.want) && i.Severity == c.sev {
				found = true
			}
		}
		if !found {
			t.Errorf("want %s %q in %v", c.sev, c.want, issues)
		}
	}
	if issues := Lint("#!/bin/sh\n# <switch.name> A\n# <switch.type> daemon\ntrap 'exit 0' TERM\nwhile :; do sleep 1; done\n"); len(issues) != 0 {
		t.Errorf("trap TERM daemon flagged: %v", issues)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Keep Awake": "keep-awake.sh", "摸鱼模式": "摸鱼模式.sh", "  --Wi-Fi!!  ": "wi-fi.sh", "!!!": "switch.sh",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetDirective(t *testing.T) {
	src := "#!/bin/zsh\n# <switch.name> Old\n  # <switch.type> toggle\n\n# body comment\necho status\n"
	got := SetDirective(src, "name", "New")
	if !strings.Contains(got, "# <switch.name> New\n") || strings.Contains(got, "Old") {
		t.Errorf("rename: %q", got)
	}
	got = SetDirective(src, "type", "action")
	if !strings.Contains(got, "  # <switch.type> action\n") {
		t.Errorf("indent kept: %q", got)
	}
	got = SetDirective(src, "icon", "lucide:zap")
	if !strings.Contains(got, "# <switch.type> toggle\n# <switch.icon> lucide:zap\n") {
		t.Errorf("insert after last directive: %q", got)
	}
	got = SetDirective(src, "type", "")
	if strings.Contains(got, "switch.type") || !strings.Contains(got, "# body comment") {
		t.Errorf("delete: %q", got)
	}
	got = SetDirective("#!/bin/sh\necho hi\n", "name", "A")
	if got != "#!/bin/sh\n# <switch.name> A\necho hi\n" {
		t.Errorf("insert after shebang: %q", got)
	}
	if f, l, ok := HeaderRange(src); !ok || f != 1 || l != 2 {
		t.Errorf("header range %d %d %v", f, l, ok)
	}
}

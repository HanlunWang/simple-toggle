package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/HanlunWang/open-toggle/internal/keys"
)

// Issue 是一条检查结果：error 拒收，warning 放行但随响应返回。
type Issue struct {
	Severity string `json:"severity"` // "error" | "warning"
	Message  string `json:"message"`
}

func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}

var (
	reTrapTerm   = regexp.MustCompile(`trap\b.*\b(SIG)?(TERM|INT)\b`)
	reCaseArg    = regexp.MustCompile(`case\s+"?\$\{?1\}?"?\s+in`)
	reKeyUnusual = regexp.MustCompile(`[^\p{L}\p{N}_-]`)
)

// Lint 检查脚本是否符合契约。错误信息与 v1 保持一致（agent 与 skill 依赖它们）。
func Lint(content string) []Issue {
	var issues []Issue
	errorf := func(format string, a ...any) {
		issues = append(issues, Issue{"error", fmt.Sprintf(format, a...)})
	}
	warnf := func(format string, a ...any) {
		issues = append(issues, Issue{"warning", fmt.Sprintf(format, a...)})
	}

	lines := strings.Split(content, "\n")
	if !strings.HasPrefix(lines[0], "#!") {
		warnf("Missing shebang on line 1; the script will be executed with /bin/sh as a fallback")
	}

	var name, icon, typeRaw string
	var hasName, hasIcon, hasType bool
	var paramSpecs []map[string]string
	var menubarSpec map[string]string
	sawMenubar := false

	for _, raw := range headLines(content) {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		if v, ok := valueAfter("switch.name", line); ok {
			name, hasName = v, true
		}
		if v, ok := valueAfter("switch.icon", line); ok {
			icon, hasIcon = v, true
		}
		if v, ok := valueAfter("switch.type", line); ok {
			typeRaw, hasType = v, true
		}
		if v, ok := valueAfter("switch.param", line); ok {
			paramSpecs = append(paramSpecs, TokenizePairs(v))
		}
		if strings.Contains(line, "<switch.menubar>") {
			sawMenubar = true
			if v, ok := valueAfter("switch.menubar", line); ok {
				menubarSpec = TokenizePairs(v)
			}
		}
	}

	if !hasName || name == "" {
		errorf("Missing required directive: # <switch.name> <display name>")
	}
	typ := Toggle
	if hasType {
		if t := Type(typeRaw); t.Valid() {
			typ = t
		} else {
			errorf("Invalid <switch.type> %q; expected \"toggle\", \"daemon\" or \"action\"", typeRaw)
		}
	}
	if hasIcon {
		checkIcon(icon, "<switch.icon>", warnf)
	}

	// params
	seen := map[string]bool{}
	envSeen := map[string]string{}
	numberKeys := map[string]bool{}
	for i, spec := range paramSpecs {
		where := fmt.Sprintf("<switch.param> #%d", i+1)
		key := spec["key"]
		if key == "" {
			errorf("%s: missing required attribute key=", where)
			continue
		}
		if seen[key] {
			errorf("%s: duplicate param key %q", where, key)
		}
		seen[key] = true
		if reKeyUnusual.MatchString(key) {
			warnf("%s: key %q contains unusual characters; prefer [a-z0-9_]", where, key)
		}
		if other, dup := envSeen[EnvName(key)]; dup && other != key {
			errorf("%s: key %q maps to the same environment variable %s as key %q", where, key, EnvName(key), other)
		}
		envSeen[EnvName(key)] = key

		ptRaw, hasPT := spec["type"]
		pt := ParamType(ptRaw)
		if !hasPT {
			pt = ParamText
		} else if !pt.Valid() {
			errorf("%s: invalid type %q; expected select | number | text | key", where, ptRaw)
			pt = ParamText
		}
		switch pt {
		case ParamSelect:
			opts := ParseOptions(spec["options"])
			if len(opts) == 0 {
				errorf("%s: type=select requires options=\"label=value|…\"", where)
			} else if def := spec["default"]; def != "" && !hasValue(opts, def) {
				warnf("%s: default %q is not among the option values", where, def)
			}
		case ParamNumber:
			numberKeys[key] = true
			minV, minErr := intAttr(spec, "min")
			maxV, maxErr := intAttr(spec, "max")
			if minErr {
				errorf("%s: min= must be an integer", where)
			}
			if maxErr {
				errorf("%s: max= must be an integer", where)
			}
			if minV != nil && maxV != nil && *minV > *maxV {
				errorf("%s: min (%d) > max (%d)", where, *minV, *maxV)
			}
			if def := spec["default"]; def != "" {
				if _, err := strconv.Atoi(def); err != nil {
					warnf("%s: default %q is not an integer", where, def)
				}
			}
		case ParamKey:
			if def := spec["default"]; def != "" {
				if _, ok := keys.Parse(def); !ok {
					errorf("%s: default %q is not a valid key spec (expected e.g. f15, cmd+shift+k, mouse:middle)", where, def)
				}
			}
		}
	}

	// menubar
	if sawMenubar {
		mIcon := menubarSpec["icon"]
		if mIcon == "" {
			errorf("<switch.menubar>: missing required attribute icon=")
			return issues
		}
		checkIcon(mIcon, "<switch.menubar> icon", warnf)
		if mode, has := menubarSpec["mode"]; has && mode != string(MenuBarAdd) && mode != string(MenuBarReplace) {
			warnf("<switch.menubar>: unknown mode %q; expected add | replace (falling back to add)", mode)
		}
		if truthy(menubarSpec["countdown"]) && !numberKeys["duration"] {
			warnf("<switch.menubar>: countdown=on requires a number param with key=duration (minutes); the countdown will not be shown")
		}
	}

	// body
	body := lines
	if strings.HasPrefix(body[0], "#!") {
		body = body[1:]
	}
	var kept []string
	executable := false
	for _, l := range body {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") && strings.Contains(t, "<switch.") {
			continue
		}
		kept = append(kept, l)
		if t != "" && !strings.HasPrefix(t, "#") {
			executable = true
		}
	}
	bodyText := strings.Join(kept, "\n")
	if !executable {
		warnf("Script body contains no executable statements")
		return issues
	}
	switch typ {
	case Toggle:
		if !strings.Contains(bodyText, "status") {
			warnf("toggle script body does not reference \"status\"; the app polls `<script> status` every 5 s and expects \"on\"/\"off\" on stdout")
		}
	case Daemon:
		if !strings.Contains(bodyText, "exec") && !reTrapTerm.MatchString(bodyText) {
			warnf("daemon script body neither uses `exec` nor traps TERM; SIGTERM may hit the wrapper shell instead of your process")
		}
	case Action:
		if reCaseArg.MatchString(bodyText) {
			warnf("action scripts are run without arguments; the `case \"$1\"` branch will never see on/off/status")
		}
	}
	return issues
}

func hasValue(opts []Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

// intAttr 返回整数属性；属性存在但不是整数时 bad=true。
func intAttr(spec map[string]string, k string) (v *int, bad bool) {
	raw, has := spec[k]
	if !has {
		return nil, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, true
	}
	return &n, false
}

func checkIcon(icon, context string, warnf func(string, ...any)) {
	switch {
	case strings.HasPrefix(icon, "sf:"):
		if _, ok := LucideForSF(strings.TrimPrefix(icon, "sf:")); !ok {
			warnf("%s: SF Symbol %q has no OpenToggle icon mapping; a generic icon will be shown (use lucide:<name> or an emoji)", context, strings.TrimPrefix(icon, "sf:"))
		}
	case strings.HasPrefix(icon, "lucide:"):
		if strings.TrimPrefix(icon, "lucide:") == "" {
			warnf("%s: empty lucide icon name", context)
		}
	}
}

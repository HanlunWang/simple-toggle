package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/HanlunWang/open-toggle/internal/keys"
)

// Issue 是一条检查结果：error 拒收，warning 放行但随响应返回。
// Message 是英文（与 v1 一致，agent 与 skill 依赖它）；界面按 Code 与 Args 自行翻译。
type Issue struct {
	Severity string `json:"severity"` // "error" | "warning"
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
	Line     int    `json:"line,omitempty"` // 从 1 开始；0 表示针对整个脚本
	Args     []any  `json:"-"`
}

// Messages 是每个 Code 的英文格式串，参数顺序即 Issue.Args。
var Messages = map[string]string{
	"missing_shebang":               "Missing shebang on line 1; the script will be executed with /bin/sh as a fallback",
	"missing_name":                  "Missing required directive: # <switch.name> <display name>",
	"invalid_type":                  "Invalid <switch.type> %q; expected \"toggle\", \"daemon\" or \"action\"",
	"icon_sf_unmapped":              "<switch.icon>: SF Symbol %q has no OpenToggle icon mapping; a generic icon will be shown (use lucide:<name> or an emoji)",
	"icon_lucide_empty":             "<switch.icon>: empty lucide icon name",
	"param_missing_key":             "<switch.param> #%d: missing required attribute key=",
	"param_duplicate_key":           "<switch.param> #%d: duplicate param key %q",
	"param_key_unusual":             "<switch.param> #%d: key %q contains unusual characters; prefer [a-z0-9_]",
	"param_env_clash":               "<switch.param> #%d: key %q maps to the same environment variable %s as key %q",
	"param_invalid_type":            "<switch.param> #%d: invalid type %q; expected select | number | text | key",
	"param_select_no_options":       "<switch.param> #%d: type=select requires options=\"label=value|…\"",
	"param_select_bad_default":      "<switch.param> #%d: default %q is not among the option values",
	"param_min_not_int":             "<switch.param> #%d: min= must be an integer",
	"param_max_not_int":             "<switch.param> #%d: max= must be an integer",
	"param_min_gt_max":              "<switch.param> #%d: min (%d) > max (%d)",
	"param_number_bad_default":      "<switch.param> #%d: default %q is not an integer",
	"param_key_bad_default":         "<switch.param> #%d: default %q is not a valid key spec (expected e.g. f15, cmd+shift+k, mouse:middle)",
	"menubar_missing_icon":          "<switch.menubar>: missing required attribute icon=",
	"menubar_icon_sf_unmapped":      "<switch.menubar> icon: SF Symbol %q has no OpenToggle icon mapping; a generic icon will be shown (use lucide:<name> or an emoji)",
	"menubar_icon_lucide_empty":     "<switch.menubar> icon: empty lucide icon name",
	"menubar_bad_mode":              "<switch.menubar>: unknown mode %q; expected add | replace (falling back to add)",
	"menubar_countdown_no_duration": "<switch.menubar>: countdown=on requires a number param with key=duration (minutes); the countdown will not be shown",
	"body_empty":                    "Script body contains no executable statements",
	"toggle_no_status":              "toggle script body does not reference \"status\"; the app polls `<script> status` every 5 s and expects \"on\"/\"off\" on stdout",
	"daemon_no_exec":                "daemon script body neither uses `exec` nor traps TERM; SIGTERM may hit the wrapper shell instead of your process",
	"action_case_arg":               "action scripts are run without arguments; the `case \"$1\"` branch will never see on/off/status",
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
	add := func(sev string, line int, code string, args ...any) {
		issues = append(issues, Issue{Severity: sev, Message: fmt.Sprintf(Messages[code], args...), Code: code, Line: line, Args: args})
	}
	errorf := func(line int, code string, args ...any) { add("error", line, code, args...) }
	warnf := func(line int, code string, args ...any) { add("warning", line, code, args...) }

	lines := strings.Split(content, "\n")
	if !strings.HasPrefix(lines[0], "#!") {
		warnf(1, "missing_shebang")
	}

	var name, icon, typeRaw string
	var hasName, hasIcon, hasType bool
	var iconLine, typeLine, menubarLine int
	type paramSpec struct {
		pairs map[string]string
		line  int
	}
	var paramSpecs []paramSpec
	var menubarSpec map[string]string
	sawMenubar := false

	for i, raw := range headLines(content) {
		ln := i + 1
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		if v, ok := valueAfter("switch.name", line); ok {
			name, hasName = v, true
		}
		if v, ok := valueAfter("switch.icon", line); ok {
			icon, hasIcon, iconLine = v, true, ln
		}
		if v, ok := valueAfter("switch.type", line); ok {
			typeRaw, hasType, typeLine = v, true, ln
		}
		if v, ok := valueAfter("switch.param", line); ok {
			paramSpecs = append(paramSpecs, paramSpec{TokenizePairs(v), ln})
		}
		if strings.Contains(line, "<switch.menubar>") {
			sawMenubar, menubarLine = true, ln
			if v, ok := valueAfter("switch.menubar", line); ok {
				menubarSpec = TokenizePairs(v)
			}
		}
	}

	if !hasName || name == "" {
		errorf(0, "missing_name")
	}
	typ := Toggle
	if hasType {
		if t := Type(typeRaw); t.Valid() {
			typ = t
		} else {
			errorf(typeLine, "invalid_type", typeRaw)
		}
	}
	if hasIcon {
		checkIcon(icon, iconLine, "icon", warnf)
	}

	// params
	seen := map[string]bool{}
	envSeen := map[string]string{}
	numberKeys := map[string]bool{}
	for i, ps := range paramSpecs {
		spec, ln, n := ps.pairs, ps.line, i+1
		key := spec["key"]
		if key == "" {
			errorf(ln, "param_missing_key", n)
			continue
		}
		if seen[key] {
			errorf(ln, "param_duplicate_key", n, key)
		}
		seen[key] = true
		if reKeyUnusual.MatchString(key) {
			warnf(ln, "param_key_unusual", n, key)
		}
		if other, dup := envSeen[EnvName(key)]; dup && other != key {
			errorf(ln, "param_env_clash", n, key, EnvName(key), other)
		}
		envSeen[EnvName(key)] = key

		ptRaw, hasPT := spec["type"]
		pt := ParamType(ptRaw)
		if !hasPT {
			pt = ParamText
		} else if !pt.Valid() {
			errorf(ln, "param_invalid_type", n, ptRaw)
			pt = ParamText
		}
		switch pt {
		case ParamSelect:
			opts := ParseOptions(spec["options"])
			if len(opts) == 0 {
				errorf(ln, "param_select_no_options", n)
			} else if def := spec["default"]; def != "" && !hasValue(opts, def) {
				warnf(ln, "param_select_bad_default", n, def)
			}
		case ParamNumber:
			numberKeys[key] = true
			minV, minErr := intAttr(spec, "min")
			maxV, maxErr := intAttr(spec, "max")
			if minErr {
				errorf(ln, "param_min_not_int", n)
			}
			if maxErr {
				errorf(ln, "param_max_not_int", n)
			}
			if minV != nil && maxV != nil && *minV > *maxV {
				errorf(ln, "param_min_gt_max", n, *minV, *maxV)
			}
			if def := spec["default"]; def != "" {
				if _, err := strconv.Atoi(def); err != nil {
					warnf(ln, "param_number_bad_default", n, def)
				}
			}
		case ParamKey:
			if def := spec["default"]; def != "" {
				if _, ok := keys.Parse(def); !ok {
					errorf(ln, "param_key_bad_default", n, def)
				}
			}
		}
	}

	// menubar
	if sawMenubar {
		mIcon := menubarSpec["icon"]
		if mIcon == "" {
			errorf(menubarLine, "menubar_missing_icon")
			return issues
		}
		checkIcon(mIcon, menubarLine, "menubar_icon", warnf)
		if mode, has := menubarSpec["mode"]; has && mode != string(MenuBarAdd) && mode != string(MenuBarReplace) {
			warnf(menubarLine, "menubar_bad_mode", mode)
		}
		if truthy(menubarSpec["countdown"]) && !numberKeys["duration"] {
			warnf(menubarLine, "menubar_countdown_no_duration")
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
		warnf(0, "body_empty")
		return issues
	}
	switch typ {
	case Toggle:
		if !strings.Contains(bodyText, "status") {
			warnf(0, "toggle_no_status")
		}
	case Daemon:
		if !strings.Contains(bodyText, "exec") && !reTrapTerm.MatchString(bodyText) {
			warnf(0, "daemon_no_exec")
		}
	case Action:
		if reCaseArg.MatchString(bodyText) {
			warnf(firstMatchLine(lines, reCaseArg), "action_case_arg")
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

func checkIcon(icon string, line int, prefix string, warnf func(int, string, ...any)) {
	switch {
	case strings.HasPrefix(icon, "sf:"):
		if _, ok := LucideForSF(strings.TrimPrefix(icon, "sf:")); !ok {
			warnf(line, prefix+"_sf_unmapped", strings.TrimPrefix(icon, "sf:"))
		}
	case strings.HasPrefix(icon, "lucide:"):
		if strings.TrimPrefix(icon, "lucide:") == "" {
			warnf(line, prefix+"_lucide_empty")
		}
	}
}

// firstMatchLine 返回第一处匹配所在的行号（从 1 开始），没有时为 0。
func firstMatchLine(lines []string, re *regexp.Regexp) int {
	for i, l := range lines {
		if re.MatchString(l) {
			return i + 1
		}
	}
	return 0
}

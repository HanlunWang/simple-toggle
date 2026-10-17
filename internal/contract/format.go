package contract

import (
	"strconv"
	"strings"
)

// 工作台检查器用规范格式回写指令：参数、菜单栏。值里的双引号会被去掉（契约不支持转义）。

// FormatParam 返回参数指令的值部分（不含 `# <switch.param> `）。
func FormatParam(p Param) string {
	parts := []string{"key=" + quoteValue(p.Key)}
	if p.Type != "" && p.Type != ParamText {
		parts = append(parts, "type="+string(p.Type))
	} else {
		parts = append(parts, "type=text")
	}
	if p.Label != "" && p.Label != p.Key {
		parts = append(parts, "label="+quoteValue(p.Label))
	}
	if p.Default != "" {
		parts = append(parts, "default="+quoteValue(p.Default))
	}
	if p.Type == ParamSelect && len(p.Options) > 0 {
		parts = append(parts, "options="+quoteValue(FormatOptions(p.Options)))
	}
	if len(p.Presets) > 0 && p.Type != ParamSelect {
		parts = append(parts, "presets="+quoteValue(FormatOptions(p.Presets)))
	}
	if p.Type == ParamNumber {
		if p.Min != nil {
			parts = append(parts, "min="+strconv.Itoa(*p.Min))
		}
		if p.Max != nil {
			parts = append(parts, "max="+strconv.Itoa(*p.Max))
		}
	}
	if p.Hint != "" {
		parts = append(parts, "hint="+quoteValue(p.Hint))
	}
	return strings.Join(parts, " ")
}

// FormatOptions 把选项写成 "标签=值|标签=值"；标签与值相同时只写值。
func FormatOptions(opts []Option) string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		label := strings.NewReplacer("|", "/", "=", "-").Replace(o.Label)
		value := strings.NewReplacer("|", "/", "=", "-").Replace(o.Value)
		if label == value || label == "" {
			out = append(out, value)
		} else {
			out = append(out, label+"="+value)
		}
	}
	return strings.Join(out, "|")
}

// FormatMenuBar 返回菜单栏指令的值部分。
func FormatMenuBar(m MenuBar) string {
	mode := m.Mode
	if mode != MenuBarReplace {
		mode = MenuBarAdd
	}
	s := "mode=" + string(mode) + " icon=" + quoteValue(m.Icon)
	if m.Countdown {
		s += " countdown=on"
	}
	return s
}

func quoteValue(v string) string {
	v = strings.ReplaceAll(v, `"`, "")
	if v == "" || strings.ContainsAny(v, " \t") {
		return `"` + v + `"`
	}
	return v
}

// SetParams 用规范格式重写全部参数指令：写在原来第一条参数指令的位置；
// 原来没有参数时写在最后一条指令之后。脚本其余内容原样保留。
func SetParams(content string, params []Param) string {
	lines := strings.Split(content, "\n")
	limit := min(len(lines), HeaderLines)
	at, last := -1, -1
	kept := make([]string, 0, len(lines)+len(params))
	for i, l := range lines {
		if i < limit {
			if m := reDirective.FindStringSubmatch(l); m != nil {
				if m[1] == "param" {
					if at < 0 {
						at = len(kept)
					}
					continue
				}
				last = len(kept)
			}
		}
		kept = append(kept, l)
	}
	if at < 0 {
		at = last + 1
		if last < 0 {
			at = 0
			if len(kept) > 0 && strings.HasPrefix(kept[0], "#!") {
				at = 1
			}
		}
	}
	ins := make([]string, 0, len(params))
	for _, p := range params {
		ins = append(ins, "# <switch.param> "+FormatParam(p))
	}
	out := make([]string, 0, len(kept)+len(ins))
	out = append(out, kept[:at]...)
	out = append(out, ins...)
	out = append(out, kept[at:]...)
	return strings.Join(out, "\n")
}

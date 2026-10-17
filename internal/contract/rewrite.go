package contract

import (
	"regexp"
	"strings"
)

var reDirective = regexp.MustCompile(`^\s*#\s*<switch\.([a-z]+)>`)

// SetDirective 把单值指令（name、icon、type、description、menubar）改为 value：
// 已有则只改写那一行并保留行首缩进；没有则插在最后一条指令之后（没有任何指令时
// 插在 shebang 之后）。value 为空表示删除该指令。脚本其余内容原样保留。
func SetDirective(content, key, value string) string {
	lines := strings.Split(content, "\n")
	limit := min(len(lines), HeaderLines)
	idx, last := -1, -1
	for i := 0; i < limit; i++ {
		m := reDirective.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		last = i
		if m[1] == key && idx < 0 {
			idx = i
		}
	}
	line := "# <switch." + key + "> " + value
	switch {
	case idx >= 0 && value == "":
		return strings.Join(append(lines[:idx:idx], lines[idx+1:]...), "\n")
	case idx >= 0:
		indent := lines[idx][:len(lines[idx])-len(strings.TrimLeft(lines[idx], " \t"))]
		lines[idx] = indent + line
		return strings.Join(lines, "\n")
	case value == "":
		return content
	}
	at := last + 1
	if last < 0 {
		at = 0
		if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
			at = 1
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, line)
	out = append(out, lines[at:]...)
	return strings.Join(out, "\n")
}

// HeaderRange 返回契约指令所在的首尾行号（从 0 开始）；没有指令时 ok=false。
func HeaderRange(content string) (first, last int, ok bool) {
	first, last = -1, -1
	lines := strings.Split(content, "\n")
	for i := 0; i < min(len(lines), HeaderLines); i++ {
		if reDirective.MatchString(lines[i]) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return first, last, first >= 0
}

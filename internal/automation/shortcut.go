package automation

import (
	"errors"
	"slices"
	"strings"
)

// 快捷键统一存成小写的规范形式：修饰键按 cmd、ctrl、alt、shift 排序，按键在最后，
// 例如 "ctrl+alt+p"。这也是 mygo 接受的加速键写法（不区分大小写）。

var modOrder = []string{"cmd", "ctrl", "alt", "shift"}

var modAlias = map[string]string{
	"cmd": "cmd", "command": "cmd", "super": "cmd", "meta": "cmd",
	"ctrl": "ctrl", "control": "ctrl",
	"alt": "alt", "option": "alt", "opt": "alt",
	"shift": "shift",
}

var ErrBadShortcut = errors.New("a shortcut needs at least one of cmd, ctrl or alt plus a key (or an F key)")

// NormalizeShortcut 返回规范形式；不是有效的全局快捷键时返回错误。
func NormalizeShortcut(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	parts := strings.Split(s, "+")
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return "", ErrBadShortcut
	}
	mods := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		m, ok := modAlias[strings.TrimSpace(p)]
		if !ok {
			return "", ErrBadShortcut
		}
		mods[m] = true
	}
	if _, isMod := modAlias[key]; isMod {
		return "", ErrBadShortcut
	}
	fkey := len(key) >= 2 && key[0] == 'f' && strings.Trim(key[1:], "0123456789") == ""
	if !fkey && !mods["cmd"] && !mods["ctrl"] && !mods["alt"] {
		return "", ErrBadShortcut // 只有 Shift 或没有修饰键会吞掉正常输入
	}
	var out []string
	for _, m := range modOrder {
		if mods[m] {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, key), "+"), nil
}

// ShortcutSymbols 把规范形式显示成 macOS 的符号，例如 ⌃⌥P。
func ShortcutSymbols(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "+")
	sym := map[string]string{"cmd": "⌘", "ctrl": "⌃", "alt": "⌥", "shift": "⇧"}
	var b strings.Builder
	for _, m := range modOrder {
		if slices.Contains(parts[:len(parts)-1], m) {
			b.WriteString(sym[m])
		}
	}
	key := parts[len(parts)-1]
	names := map[string]string{"space": "Space", "enter": "↩", "return": "↩", "tab": "⇥", "escape": "⎋", "esc": "⎋",
		"left": "←", "right": "→", "up": "↑", "down": "↓", "backspace": "⌫", "delete": "⌦"}
	if n, ok := names[key]; ok {
		key = n
	} else {
		key = strings.ToUpper(key)
	}
	b.WriteString(key)
	return b.String()
}

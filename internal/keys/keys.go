// Package keys 解析按键描述（key 型参数的值）：
//
//	"[modifier+]*key"           f15、cmd+shift+k、ctrl+opt+space
//	"mouse:left|middle|right"   在当前光标位置点击
//
// 修饰键：cmd shift opt ctrl fn（别名 command meta option alt control）。
// 与 v1（Swift 版 KeySpec）行为一致；事件合成在 platform/darwin。
package keys

import (
	"sort"
	"strings"
)

type Target int

const (
	TargetKey Target = iota
	TargetMouse
)

type MouseButton int

const (
	MouseLeft   MouseButton = 0
	MouseRight  MouseButton = 1
	MouseMiddle MouseButton = 2
)

// CGEventFlags 中各修饰键的掩码。
const (
	FlagShift   uint64 = 1 << 17
	FlagControl uint64 = 1 << 18
	FlagOption  uint64 = 1 << 19
	FlagCommand uint64 = 1 << 20
	FlagFn      uint64 = 1 << 23
)

var modifierOrder = []string{"cmd", "shift", "opt", "ctrl", "fn"}

var modifierFlags = map[string]uint64{
	"cmd": FlagCommand, "shift": FlagShift, "opt": FlagOption, "ctrl": FlagControl, "fn": FlagFn,
}

var modifierAliases = map[string]string{
	"command": "cmd", "meta": "cmd", "option": "opt", "alt": "opt", "control": "ctrl",
}

var mouseButtons = map[string]MouseButton{
	"mouse:left": MouseLeft, "mouse:right": MouseRight, "mouse:middle": MouseMiddle,
}

// KeyCodes 是键名到 macOS 虚拟键码（ANSI 布局，Carbon kVK_*）的映射。
var KeyCodes = map[string]uint16{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
	"b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17,
	"1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "9": 25, "7": 26, "8": 28, "0": 29,
	"o": 31, "u": 32, "i": 34, "p": 35, "l": 37, "j": 38, "k": 40, "n": 45, "m": 46,
	"return": 36, "tab": 48, "space": 49, "delete": 51, "esc": 53,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100,
	"f9": 101, "f10": 109, "f11": 103, "f12": 111, "f13": 105, "f14": 107, "f15": 113,
	"f16": 106, "f17": 64, "f18": 79, "f19": 80, "f20": 90,
	"home": 115, "end": 119, "pageup": 116, "pagedown": 121, "forwarddelete": 117,
	"left": 123, "right": 124, "down": 125, "up": 126,
}

// Spec 是解析后的按键描述。
type Spec struct {
	Modifiers []string // 规范顺序 cmd shift opt ctrl fn
	Key       string   // 规范键名（"f15"、"k"、"mouse:left"）
	Target    Target
	Code      uint16      // TargetKey
	Button    MouseButton // TargetMouse
}

// Parse 解析按键描述；无法识别时 ok=false。
func Parse(raw string) (Spec, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return Spec{}, false
	}
	if b, ok := mouseButtons[s]; ok {
		return Spec{Key: s, Target: TargetMouse, Button: b}, true
	}
	parts := strings.Split(s, "+")
	last := parts[len(parts)-1]
	mods := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		if a, ok := modifierAliases[p]; ok {
			p = a
		}
		if _, ok := modifierFlags[p]; !ok {
			return Spec{}, false
		}
		mods[p] = true
	}
	code, ok := KeyCodes[last]
	if !ok {
		return Spec{}, false
	}
	var ordered []string
	for _, m := range modifierOrder {
		if mods[m] {
			ordered = append(ordered, m)
		}
	}
	return Spec{Modifiers: ordered, Key: last, Target: TargetKey, Code: code}, true
}

// Canonical 是规范写法，如 "cmd+shift+k"。
func (s Spec) Canonical() string {
	return strings.Join(append(append([]string{}, s.Modifiers...), s.Key), "+")
}

// Flags 是修饰键对应的 CGEventFlags。
func (s Spec) Flags() uint64 {
	var f uint64
	for _, m := range s.Modifiers {
		f |= modifierFlags[m]
	}
	return f
}

// Display 是 mac 风格显示（⌘⇧K、F15）；鼠标键返回空串，由界面本地化。
func (s Spec) Display() string {
	if s.Target == TargetMouse {
		return ""
	}
	symbols := map[string]string{"cmd": "⌘", "shift": "⇧", "opt": "⌥", "ctrl": "⌃", "fn": "fn "}
	named := map[string]string{
		"space": "Space", "tab": "⇥", "return": "↩", "esc": "⎋",
		"delete": "⌫", "forwarddelete": "⌦",
		"up": "↑", "down": "↓", "left": "←", "right": "→",
		"home": "↖", "end": "↘", "pageup": "⇞", "pagedown": "⇟",
	}
	var b strings.Builder
	for _, m := range s.Modifiers {
		b.WriteString(symbols[m])
	}
	if n, ok := named[s.Key]; ok {
		b.WriteString(n)
	} else {
		b.WriteString(strings.ToUpper(s.Key))
	}
	return b.String()
}

// BareCharacter 报告是否是无修饰键的单字符键：会真实输入字符，界面应提示。
func (s Spec) BareCharacter() bool {
	return s.Target == TargetKey && len(s.Modifiers) == 0 && len([]rune(s.Key)) == 1
}

// Names 返回全部可用键名（排序），供界面列出常用按键。
func Names() []string {
	out := make([]string, 0, len(KeyCodes))
	for k := range KeyCodes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

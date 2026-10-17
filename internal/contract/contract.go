// Package contract 解析与检查开关脚本的契约头（`# <switch.*>` 指令）。
//
// v2 契约是 v1 的超集：v1 脚本原样可用；新增 type=action、lucide:<名称> 图标
// 与 <switch.description>。解析规则与 v1（Swift 版 SwitchModel）逐条一致。
package contract

import (
	"strconv"
	"strings"
	"unicode"
)

// HeaderLines 是契约头的扫描范围：只看脚本的前 40 行。
const HeaderLines = 40

// DefaultIcon 是未声明图标时显示的图标（与 v1 一致）。
const DefaultIcon = "🔘"

type Type string

const (
	Toggle Type = "toggle" // on / off / status
	Daemon Type = "daemon" // run，app 持有进程
	Action Type = "action" // 无参数执行一次
)

func (t Type) Valid() bool { return t == Toggle || t == Daemon || t == Action }

type ParamType string

const (
	ParamSelect ParamType = "select"
	ParamNumber ParamType = "number"
	ParamText   ParamType = "text"
	ParamKey    ParamType = "key"
)

func (t ParamType) Valid() bool {
	return t == ParamSelect || t == ParamNumber || t == ParamText || t == ParamKey
}

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Param struct {
	Key     string
	Label   string
	Type    ParamType
	Default string
	Options []Option // select 专用
	Presets []Option // number / text 的快捷按钮
	Min     *int     // number 专用
	Max     *int
	Hint    string
}

// EnvName 是参数注入脚本时的环境变量名：SWITCH_<KEY 大写，非字母数字变 _>。
func (p Param) EnvName() string { return EnvName(p.Key) }

func EnvName(key string) string {
	var b strings.Builder
	b.WriteString("SWITCH_")
	for _, r := range strings.ToUpper(key) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

type MenuBarMode string

const (
	MenuBarAdd     MenuBarMode = "add"
	MenuBarReplace MenuBarMode = "replace"
)

type MenuBar struct {
	Mode      MenuBarMode
	Icon      string
	Countdown bool // 需要 key=duration 的 number 参数（分钟，0 = 不限）
}

// Header 是解析后的契约头。
type Header struct {
	Name        string
	Icon        string
	Type        Type
	Description string
	Params      []Param
	MenuBar     *MenuBar
}

// Param 按 key 查找参数。
func (h Header) Param(key string) (Param, bool) {
	for _, p := range h.Params {
		if p.Key == key {
			return p, true
		}
	}
	return Param{}, false
}

// Parse 解析契约头。没有 <switch.name> 时返回 ok=false（不是开关）。
func Parse(content string) (h Header, ok bool) {
	h = Header{Icon: DefaultIcon, Type: Toggle}
	for _, raw := range headLines(content) {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		if v, found := valueAfter("switch.name", line); found {
			h.Name = v
		}
		if v, found := valueAfter("switch.icon", line); found {
			h.Icon = v
		}
		if v, found := valueAfter("switch.type", line); found {
			if t := Type(v); t.Valid() {
				h.Type = t
			}
		}
		if v, found := valueAfter("switch.description", line); found {
			h.Description = v
		}
		if v, found := valueAfter("switch.param", line); found {
			if p, pok := parseParam(v); pok {
				h.Params = append(h.Params, p)
			}
		}
		if v, found := valueAfter("switch.menubar", line); found {
			h.MenuBar = parseMenuBar(v)
		}
	}
	return h, h.Name != ""
}

func headLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > HeaderLines {
		lines = lines[:HeaderLines]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// valueAfter 取行内 `<key>` 之后的值（去首尾空白）；值为空视为未声明。
func valueAfter(key, line string) (string, bool) {
	tag := "<" + key + ">"
	i := strings.Index(line, tag)
	if i < 0 {
		return "", false
	}
	v := strings.TrimSpace(line[i+len(tag):])
	return v, v != ""
}

func parseParam(spec string) (Param, bool) {
	pairs := TokenizePairs(spec)
	key := pairs["key"]
	if key == "" {
		return Param{}, false
	}
	pt := ParamType(pairs["type"])
	if !pt.Valid() {
		pt = ParamText
	}
	options := ParseOptions(pairs["options"])
	def := pairs["default"]
	if pt == ParamSelect && def == "" && len(options) > 0 {
		def = options[0].Value
	}
	label := pairs["label"]
	if _, has := pairs["label"]; !has {
		label = key
	}
	return Param{
		Key: key, Label: label, Type: pt, Default: def,
		Options: options, Presets: ParseOptions(pairs["presets"]),
		Min: atoiPtr(pairs, "min"), Max: atoiPtr(pairs, "max"),
		Hint: pairs["hint"],
	}, true
}

func atoiPtr(pairs map[string]string, k string) *int {
	v, has := pairs[k]
	if !has {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

func parseMenuBar(spec string) *MenuBar {
	pairs := TokenizePairs(spec)
	icon := pairs["icon"]
	if icon == "" {
		return nil
	}
	mode := MenuBarMode(pairs["mode"])
	if mode != MenuBarAdd && mode != MenuBarReplace {
		mode = MenuBarAdd
	}
	return &MenuBar{Mode: mode, Icon: icon, Countdown: truthy(pairs["countdown"])}
}

func truthy(s string) bool {
	switch strings.ToLower(s) {
	case "on", "true", "yes", "1":
		return true
	}
	return false
}

// ParseOptions 把 "标签=值|标签=值" 解析为选项；没有 = 的项标签与值相同。
func ParseOptions(raw string) []Option {
	var out []Option
	for _, part := range strings.Split(raw, "|") {
		if part == "" {
			continue
		}
		label, value, found := strings.Cut(part, "=")
		if !found {
			out = append(out, Option{Label: part, Value: part})
			continue
		}
		out = append(out, Option{Label: strings.TrimSpace(label), Value: strings.TrimSpace(value)})
	}
	return out
}

// TokenizePairs 把 `key=value key2="value with spaces"` 解析成字典：
// 双引号内的空格不分词，引号本身去掉；没有 = 的词忽略。
func TokenizePairs(s string) map[string]string {
	var tokens []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case r == ' ' && !inQuotes:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	pairs := map[string]string{}
	for _, t := range tokens {
		if k, v, found := strings.Cut(t, "="); found {
			pairs[k] = v
		}
	}
	return pairs
}

// Slug 由开关名称生成脚本文件名（kebab-case，保留中文等字母），与 v1 一致。
func Slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		s := b.String()
		if s == "" || strings.HasSuffix(s, "-") {
			continue
		}
		b.WriteByte('-')
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "switch"
	}
	return slug + ".sh"
}

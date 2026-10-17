package app

import (
	"errors"
	"net/url"
	"strings"
)

// opentoggle:// 深链接（快捷指令、Raycast、Alfred 调用）：
//
//	opentoggle://toggle/<id>          切换开关（按钮类则运行）
//	opentoggle://on/<id>  off/<id>    打开 / 关闭
//	opentoggle://run/<id>             运行按钮
//	opentoggle://scene/<id或名称>     激活场景（激活中且可恢复时恢复）
//	opentoggle://panel                打开面板
//	opentoggle://workbench[/<分区>[/<条目>]]
//	opentoggle://settings[/<分区>]

type deepLink struct {
	Verb    string // toggle on off run scene panel workbench settings
	Target  string
	Section string
}

var errBadLink = errors.New("unsupported opentoggle:// link")

func parseDeepLink(raw string) (deepLink, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "opentoggle" {
		return deepLink{}, errBadLink
	}
	// opentoggle://toggle/x：host 是动作，path 是参数
	parts := []string{u.Host}
	for _, p := range strings.Split(strings.Trim(u.EscapedPath(), "/"), "/") {
		if p == "" {
			continue
		}
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
		parts = append(parts, p)
	}
	l := deepLink{Verb: strings.ToLower(parts[0])}
	switch l.Verb {
	case "toggle", "on", "off", "run", "scene":
		if len(parts) != 2 || parts[1] == "" {
			return deepLink{}, errBadLink
		}
		l.Target = parts[1]
	case "panel":
	case "workbench":
		if len(parts) > 1 {
			l.Section = parts[1]
		}
		if len(parts) > 2 {
			l.Target = parts[2]
		}
	case "settings":
		if len(parts) > 1 {
			l.Section = parts[1]
		}
	default:
		return deepLink{}, errBadLink
	}
	return l, nil
}

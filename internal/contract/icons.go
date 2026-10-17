package contract

import "strings"

// sfToLucide 把 v1 的 SF Symbol 名映射到 Lucide 图标名（v2 界面不支持 SF Symbols）。
// 覆盖预置脚本与常见符号；.fill / .circle 等变体先去后缀再查。
var sfToLucide = map[string]string{
	"moon":                             "moon",
	"moon.zzz":                         "moon",
	"sun.max":                          "sun",
	"sun.min":                          "sun-dim",
	"cup.and.saucer":                   "coffee",
	"mug":                              "coffee",
	"eye":                              "eye",
	"eye.slash":                        "eye-off",
	"wifi":                             "wifi",
	"wifi.slash":                       "wifi-off",
	"globe":                            "globe",
	"network":                          "network",
	"network.badge.shield.half.filled": "shield-check",
	"server.rack":                      "server",
	"speaker":                          "volume-2",
	"speaker.wave.2":                   "volume-2",
	"speaker.slash":                    "volume-x",
	"mic":                              "mic",
	"mic.slash":                        "mic-off",
	"dock.rectangle":                   "panel-bottom",
	"rectangle.dashed":                 "square-dashed",
	"menubar.rectangle":                "panel-top",
	"display":                          "monitor",
	"desktopcomputer":                  "monitor",
	"laptopcomputer":                   "laptop",
	"keyboard":                         "keyboard",
	"fish":                             "fish",
	"bolt":                             "zap",
	"bolt.slash":                       "zap-off",
	"bell":                             "bell",
	"bell.slash":                       "bell-off",
	"timer":                            "timer",
	"clock":                            "clock",
	"lock":                             "lock",
	"lock.open":                        "lock-open",
	"shield":                           "shield",
	"power":                            "power",
	"terminal":                         "terminal",
	"trash":                            "trash-2",
	"folder":                           "folder",
	"doc":                              "file",
	"gear":                             "settings",
	"gearshape":                        "settings",
	"arrow.clockwise":                  "rotate-cw",
	"arrow.triangle.2.circlepath":      "refresh-cw",
	"play":                             "play",
	"pause":                            "pause",
	"stop":                             "square",
	"bluetooth":                        "bluetooth",
	"battery.100":                      "battery-full",
	"camera":                           "camera",
	"video":                            "video",
	"video.slash":                      "video-off",
	"music.note":                       "music",
	"cloud":                            "cloud",
	"cpu":                              "cpu",
	"memorychip":                       "memory-stick",
	"hammer":                           "hammer",
	"wrench":                           "wrench",
	"house":                            "house",
	"briefcase":                        "briefcase",
	"airplane":                         "plane",
	"car":                              "car",
	"cart":                             "shopping-cart",
	"star":                             "star",
	"heart":                            "heart",
	"flag":                             "flag",
	"bookmark":                         "bookmark",
	"paperplane":                       "send",
	"envelope":                         "mail",
	"message":                          "message-circle",
	"phone":                            "phone",
	"person":                           "user",
	"magnifyingglass":                  "search",
	"sparkles":                         "sparkles",
	"flame":                            "flame",
	"drop":                             "droplet",
	"leaf":                             "leaf",
	"square.and.arrow.up":              "share",
	"link":                             "link",
	"externaldrive":                    "hard-drive",
	"printer":                          "printer",
	"cursorarrow":                      "mouse-pointer",
	"hand.raised":                      "hand",
}

var sfVariantSuffixes = []string{".fill", ".circle", ".square", ".badge", ".rectangle"}

// LucideForSF 返回 SF Symbol 对应的 Lucide 名。
func LucideForSF(symbol string) (string, bool) {
	if l, ok := sfToLucide[symbol]; ok {
		return l, true
	}
	base := symbol
	for changed := true; changed; {
		changed = false
		for _, suf := range sfVariantSuffixes {
			if strings.HasSuffix(base, suf) {
				base = strings.TrimSuffix(base, suf)
				changed = true
			}
		}
		if l, ok := sfToLucide[base]; ok {
			return l, true
		}
	}
	return "", false
}

// IconKind 描述图标写法。
type IconKind int

const (
	IconEmoji  IconKind = iota // 任意文本（通常是 emoji）
	IconLucide                 // lucide:<名称>
)

// ResolveIcon 把契约里的图标写法统一为界面可用的形式：lucide:<名称> 或 emoji。
// 无法映射的 sf: 名称退回到通用图标。
func ResolveIcon(icon string) (kind IconKind, value string) {
	switch {
	case strings.HasPrefix(icon, "lucide:"):
		return IconLucide, strings.TrimPrefix(icon, "lucide:")
	case strings.HasPrefix(icon, "sf:"):
		if l, ok := LucideForSF(strings.TrimPrefix(icon, "sf:")); ok {
			return IconLucide, l
		}
		return IconLucide, "toggle-right"
	case icon == "":
		return IconEmoji, DefaultIcon
	}
	return IconEmoji, icon
}

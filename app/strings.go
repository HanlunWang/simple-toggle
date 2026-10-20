package app

import "strings"

// 界面文案。M5 接入 mygo 的 i18n 与语言设置前，先按系统语言在中文与英文之间选择。
var zh = map[string]string{
	"on_count":       "%d ON",
	"search":         "搜索开关…",
	"search_tip":     "搜索",
	"no_match":       "没有匹配「%s」的开关",
	"empty":          "还没有开关",
	"empty_hint":     "把脚本放进脚本目录，它就会出现在这里。",
	"open_scripts":   "脚本目录",
	"refresh":        "刷新状态",
	"quit":           "退出",
	"run":            "运行",
	"running":        "运行中…",
	"done":           "完成",
	"failed":         "失败",
	"failed_exit":    "失败 · 退出码 %d",
	"last":           "上次 %s",
	"turning_on":     "正在开启…",
	"turning_off":    "正在关闭…",
	"daemon_running": "运行中",
	"remaining":      "剩 %s",
	"error":          "出错",
	"ax_title":       "按键类开关需要「辅助功能」权限",
	"ax_button":      "去授权",
	"other_group":    "其他",
	"just_now":       "刚刚",
	"minutes_ago":    "%d 分钟前",
	"hours_ago":      "%d 小时前",
	"days_ago":       "%d 天前",
	"open_panel":     "打开面板",
	"api_failed":     "本地 API 未启动：%s",
}

var en = map[string]string{
	"on_count":       "%d ON",
	"search":         "Search switches…",
	"search_tip":     "Search",
	"no_match":       "No switches match “%s”",
	"empty":          "No switches yet",
	"empty_hint":     "Put a script in the scripts folder and it shows up here.",
	"open_scripts":   "Scripts",
	"refresh":        "Refresh",
	"quit":           "Quit",
	"run":            "Run",
	"running":        "Running…",
	"done":           "Done",
	"failed":         "Failed",
	"failed_exit":    "Failed · exit %d",
	"last":           "Last run %s",
	"turning_on":     "Turning on…",
	"turning_off":    "Turning off…",
	"daemon_running": "Running",
	"remaining":      "%s left",
	"error":          "Error",
	"ax_title":       "Switches that press keys need Accessibility permission",
	"ax_button":      "Grant",
	"other_group":    "Other",
	"just_now":       "just now",
	"minutes_ago":    "%d min ago",
	"hours_ago":      "%d h ago",
	"days_ago":       "%d d ago",
	"open_panel":     "Open Panel",
	"api_failed":     "Local API not started: %s",
}

var strs = zh

// english 报告当前界面语言是否是英文（拼接列表、单复数等少数地方需要）。
var english bool

// setLanguage 按语言代码（如 zh-Hans、en）选择文案。
func setLanguage(lang string) {
	if strings.HasPrefix(lang, "zh") {
		strs, english = zh, false
	} else if lang != "" {
		strs, english = en, true
	}
}

// trn 是带数量的文案：英文里数量为 1 时用 key_one（如「1 problem」），其余与 tr 相同。
func trn(key string, n int) string {
	if n == 1 {
		if s, ok := strs[key+"_one"]; ok {
			return s
		}
	}
	return tr(key)
}

func tr(key string) string {
	if s, ok := strs[key]; ok {
		return s
	}
	return key
}

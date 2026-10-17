package darwin

import (
	"regexp"
	"strconv"
)

// Power 是电源状态。
type Power struct {
	OnAC       bool // 接着电源（台式机恒为 true）
	HasBattery bool
	Percent    int // 电量百分比；没有电池时为 100
}

var (
	rePmsetSource  = regexp.MustCompile(`Now drawing from '([^']+)'`)
	rePmsetPercent = regexp.MustCompile(`(?m)InternalBattery.*?\t(\d+)%`)
)

// ParsePmset 解析 `pmset -g batt` 的输出。
func ParsePmset(out string) (Power, bool) {
	m := rePmsetSource.FindStringSubmatch(out)
	if m == nil {
		return Power{}, false
	}
	p := Power{OnAC: m[1] != "Battery Power", Percent: 100}
	if b := rePmsetPercent.FindStringSubmatch(out); b != nil {
		p.HasBattery = true
		p.Percent, _ = strconv.Atoi(b[1])
	}
	return p, true
}

// App 是一个正在运行的（有界面的）应用。
type App struct {
	BundleID string
	Name     string
}

package app

import (
	"strconv"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

// Prefs 是界面偏好，存在 store 的 settings 里（键为 "ui.<名称>"）。
type Prefs struct {
	Theme          string // system | light | dark
	Accent         Accent // 配色方案（ui.palette；2.0 的 ui.accent 会换成对应的配色）
	Layout         string // list | grid
	Density        string // comfortable | compact
	Material       string // glass | solid
	MenubarStyle   trayStyle
	MenubarExtra   string // none | count | countdown
	ShowGroups     bool
	ShowScenes     bool
	BusyAnim       bool
	EditorFontSize float32 // 12 | 13 | 14
	LineNumbers    bool
	ExternalEditor string // 外部编辑器的 bundle id；空 = 系统默认文本编辑器
}

// 其他设置键（不属于界面偏好）。
const (
	keyLanguage          = "language"    // "" 跟随系统 | zh-Hans | en
	keyRestore           = "restore"     // 默认 true
	keyConfirmQuit       = "confirmQuit" // 退出前说明开着的开关会怎样；默认 true
	keyNotify            = "notify"      // never | fail（默认）| always
	keyTimeout           = "advanced.timeout"
	keyShell             = "advanced.shell"
	keyEnv               = "advanced.env" // JSON 对象
	keyScriptsDir        = "advanced.scriptsDir"
	keyRetention         = "log.retentionDays" // 默认 30
	keyAPI               = "api.enabled"       // 默认 true
	keyAutoUpdate        = "updates.auto"      // 默认 true
	keyPanelShortcut     = "shortcut.panel"
	keyWorkbenchShortcut = "shortcut.workbench"
	keyOnboarded         = "onboarded"
)

func loadPrefs(st *store.Store) Prefs {
	get := func(k, def string) string {
		if st == nil {
			return def
		}
		if v := st.Setting("ui." + k); v != "" {
			return v
		}
		return def
	}
	size, _ := strconv.ParseFloat(get("editorFontSize", "13"), 32)
	return Prefs{
		Theme:          get("theme", "system"),
		Accent:         normalizePalette(Accent(get("palette", get("accent", string(PalGraphite))))),
		Layout:         get("layout", "list"),
		Density:        get("density", "comfortable"),
		Material:       get("material", "glass"),
		MenubarStyle:   trayStyle(get("menubarStyle", string(trayPill))),
		MenubarExtra:   get("menubarExtra", "countdown"),
		ShowGroups:     get("showGroups", "true") == "true",
		ShowScenes:     get("showScenes", "true") == "true",
		BusyAnim:       get("busyAnim", "true") == "true",
		EditorFontSize: float32(size),
		LineNumbers:    get("lineNumbers", "true") == "true",
		ExternalEditor: get("externalEditor", ""),
	}
}

func (p Prefs) width() float32 {
	switch {
	case p.Layout == "grid":
		return 360
	case p.Density == "compact":
		return 312
	}
	return 340
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

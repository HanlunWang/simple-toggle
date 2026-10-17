package app

import "github.com/HanlunWang/open-toggle/internal/store"

// Prefs 是界面偏好，存在 store 的 settings 里（设置窗口在 M5，这里先提供默认值与读写）。
type Prefs struct {
	Theme        string // system | light | dark
	Accent       Accent
	Layout       string // list | grid
	Density      string // comfortable | compact
	Material     string // glass | solid
	MenubarStyle trayStyle
	MenubarExtra string // none | count | countdown
	ShowGroups   bool
}

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
	return Prefs{
		Theme:        get("theme", "system"),
		Accent:       Accent(get("accent", string(AccentInk))),
		Layout:       get("layout", "list"),
		Density:      get("density", "comfortable"),
		Material:     get("material", "glass"),
		MenubarStyle: trayStyle(get("menubarStyle", string(trayPill))),
		MenubarExtra: get("menubarExtra", "countdown"),
		ShowGroups:   get("showGroups", "true") == "true",
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

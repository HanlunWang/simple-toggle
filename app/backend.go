package app

import (
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// Backend 是界面对引擎的全部依赖；*engine.Engine 实现它，测试用替身。
type Backend interface {
	List() []engine.View
	SetSwitch(id string, on bool, src engine.Source, detail string) (<-chan struct{}, error)
	Run(id string, src engine.Source, detail string) (<-chan store.Run, error)
	SetParams(id string, values map[string]string, src engine.Source) error
	SetHidden(id string, hidden bool) error
	Subscribe() (<-chan engine.Event, func())
	PanelVisible(visible bool)
	Refresh()
	ScriptsDir() string
}

var _ Backend = (*engine.Engine)(nil)

package app

import (
	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/runner"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// Backend 是界面对引擎的全部依赖；*engine.Engine 实现它，测试用替身。
type Backend interface {
	// 面板
	List() []engine.View
	SetSwitch(id string, on bool, src engine.Source, detail string) (<-chan struct{}, error)
	Run(id string, src engine.Source, detail string) (<-chan store.Run, error)
	SetParams(id string, values map[string]string, src engine.Source) error
	SetHidden(id string, hidden bool) error
	Subscribe() (<-chan engine.Event, func())
	PanelVisible(visible bool)
	Refresh()
	ScriptsDir() string

	// 工作台
	Script(id string) (string, error)
	UpdateScript(id, content string, src engine.Source) ([]contract.Issue, error)
	CreateScript(content string) (string, []contract.Issue, error)
	DeleteScript(id string) error
	InstallScript(file, content string) error
	Status(id string) (runner.Result, error)
	DaemonOutput(id string) (string, bool)
	Runs(f store.Filter) []store.Run
	ClearRuns() error
	SetMeta(id string, m store.Meta) error
}

var _ Backend = (*engine.Engine)(nil)

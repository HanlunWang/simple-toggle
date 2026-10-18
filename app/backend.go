package app

import (
	"time"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/runner"
	"github.com/HanlunWang/simple-toggle/internal/store"
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
	Usage() store.Usage
	SetMeta(id string, m store.Meta) error

	// 场景
	Scenes() []engine.SceneView
	SaveScene(sc store.Scene) (store.Scene, error)
	DeleteScene(id string) error
	ToggleScene(id string, src engine.Source, detail string) (<-chan store.Run, error)
	ActivateScene(id string, src engine.Source, detail string) (<-chan store.Run, error)

	// AI 提交的待确认修改
	Changes() []store.Change
	ApproveChange(id string) (store.Change, error)
	RejectChange(id string) (store.Change, error)
}

// Automation 是自动化规则与快捷键（*automation.Manager 实现它）。
type Automation interface {
	Rules() []store.Rule
	SaveRule(r store.Rule) (store.Rule, error)
	DeleteRule(id string) error
	Test(id string) error
	Paused() (bool, time.Time)
	Pause(until time.Time) error
	Resume() error
	ShortcutIssues() map[string]automation.ShortcutIssue
	CurrentNetwork() (string, error)
	Reload()
}

var (
	_ Backend    = (*engine.Engine)(nil)
	_ Automation = (*automation.Manager)(nil)
)

package api

import (
	"time"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// ───────── /v1：与 v1（Swift 版）返回结构一致，字段按字母序 ─────────

type optionDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type paramDTO struct {
	DefaultValue string      `json:"defaultValue"`
	Hint         *string     `json:"hint,omitempty"`
	Key          string      `json:"key"`
	Label        string      `json:"label"`
	Max          *int        `json:"max,omitempty"`
	Min          *int        `json:"min,omitempty"`
	Options      []optionDTO `json:"options"`
	Presets      []optionDTO `json:"presets"`
	Type         string      `json:"type"`
	Value        string      `json:"value"`
}

type switchDTO struct {
	Enabled bool       `json:"enabled"`
	Icon    string     `json:"icon"`
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Params  []paramDTO `json:"params"`
	State   string     `json:"state"`
	Type    string     `json:"type"`
}

type issuesDTO struct {
	Issues []contract.Issue `json:"issues"`
	Valid  bool             `json:"valid"`
}

type writeDTO struct {
	ID     string           `json:"id"`
	Issues []contract.Issue `json:"issues"`
}

func options(in []contract.Option) []optionDTO {
	out := make([]optionDTO, 0, len(in))
	for _, o := range in {
		out = append(out, optionDTO{Label: o.Label, Value: o.Value})
	}
	return out
}

func paramsDTO(v engine.View) []paramDTO {
	out := make([]paramDTO, 0, len(v.Params))
	for _, p := range v.Params {
		d := paramDTO{
			DefaultValue: p.Default, Key: p.Key, Label: p.Label, Max: p.Max, Min: p.Min,
			Options: options(p.Options), Presets: options(p.Presets), Type: string(p.Type), Value: p.Value,
		}
		if p.Hint != "" {
			hint := p.Hint
			d.Hint = &hint
		}
		out = append(out, d)
	}
	return out
}

func v1Switch(v engine.View) switchDTO {
	return switchDTO{
		Enabled: !v.Hidden, Icon: v.Icon, ID: v.ID, Name: v.Name,
		Params: paramsDTO(v), State: v.V1State(), Type: string(v.Type),
	}
}

func issuesOrEmpty(in []contract.Issue) []contract.Issue {
	if in == nil {
		return []contract.Issue{}
	}
	return in
}

// ───────── /v2 ─────────

type iconDTO struct {
	Kind  string `json:"kind"` // lucide | emoji
	Value string `json:"value"`
}

type menubarDTO struct {
	Countdown bool   `json:"countdown"`
	Icon      string `json:"icon"`
	Mode      string `json:"mode"`
}

type runDTO struct {
	Action     string    `json:"action"`
	Detail     string    `json:"detail,omitempty"`
	DurationMs int64     `json:"durationMs"`
	ExitCode   int       `json:"exitCode"`
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	OK         bool      `json:"ok"`
	Output     string    `json:"output,omitempty"`
	Source     string    `json:"source"`
	Switch     string    `json:"switch"`
	Time       time.Time `json:"time"`
}

type metaDTO struct {
	Confirm  bool   `json:"confirm,omitempty"`
	Group    string `json:"group,omitempty"`
	Notify   string `json:"notify,omitempty"`
	Order    int    `json:"order,omitempty"`
	Shortcut string `json:"shortcut,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
}

type switchV2DTO struct {
	ActivatedAt  *time.Time  `json:"activatedAt,omitempty"`
	Description  string      `json:"description,omitempty"`
	Hidden       bool        `json:"hidden"`
	Icon         string      `json:"icon"`
	IconResolved iconDTO     `json:"iconResolved"`
	ID           string      `json:"id"`
	LastRun      *runDTO     `json:"lastRun,omitempty"`
	Menubar      *menubarDTO `json:"menubar,omitempty"`
	Meta         metaDTO     `json:"meta"`
	Name         string      `json:"name"`
	Params       []paramDTO  `json:"params"`
	Remaining    *int        `json:"remaining,omitempty"`
	RunningForMs int64       `json:"runningForMs,omitempty"`
	State        string      `json:"state"`
	Type         string      `json:"type"`
}

func v2Run(r *store.Run) *runDTO {
	if r == nil {
		return nil
	}
	return &runDTO{
		Action: r.Action, Detail: r.Detail, DurationMs: r.Duration, ExitCode: r.ExitCode, ID: r.ID,
		Name: r.Name, OK: r.OK, Output: r.Output, Source: r.Source, Switch: r.Switch, Time: r.Time,
	}
}

func v2Switch(v engine.View) switchV2DTO {
	kind, val := contract.ResolveIcon(v.Icon)
	k := "emoji"
	if kind == contract.IconLucide {
		k = "lucide"
	}
	d := switchV2DTO{
		Description: v.Description, Hidden: v.Hidden, Icon: v.Icon, IconResolved: iconDTO{k, val},
		ID: v.ID, LastRun: v2Run(v.LastRun), Name: v.Name, Params: paramsDTO(v), Remaining: v.Remaining,
		RunningForMs: v.RunningFor.Milliseconds(), State: string(v.State), Type: string(v.Type),
		Meta: metaDTO{Confirm: v.Meta.Confirm, Group: v.Meta.Group, Notify: v.Meta.Notify, Order: v.Meta.Order, Shortcut: v.Meta.Shortcut, Timeout: v.Meta.Timeout},
	}
	if !v.ActivatedAt.IsZero() {
		t := v.ActivatedAt
		d.ActivatedAt = &t
	}
	if v.MenuBar != nil {
		d.Menubar = &menubarDTO{Countdown: v.MenuBar.Countdown, Icon: v.MenuBar.Icon, Mode: string(v.MenuBar.Mode)}
	}
	return d
}

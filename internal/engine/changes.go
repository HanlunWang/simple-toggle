package engine

import (
	"errors"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// 待确认变更：AI 提交的脚本先存起来，用户在 app 里看过差异后批准才写入文件。

var (
	ErrNoChange      = errors.New("no such change")
	ErrChangeDecided = errors.New("change was already approved or rejected")
)

// EventChange 在有新的待确认变更或变更被处理时发出（Switch 为变更 id）。
const EventChange EventKind = "change"

// ProposeScript 校验脚本后保存为待确认变更。id 为空表示新建。
func (e *Engine) ProposeScript(id, content string, src Source, detail string) (store.Change, []contract.Issue, error) {
	issues := contract.Lint(content)
	if contract.HasErrors(issues) {
		return store.Change{}, issues, &LintError{issues}
	}
	c := store.Change{Kind: "create", Content: content, Source: string(src), Detail: detail}
	if h, ok := contract.Parse(content); ok {
		c.Name = h.Name
	}
	if id != "" {
		sw, _, err := e.lookup(id)
		if err != nil {
			return store.Change{}, issues, err
		}
		prev, err := e.Script(sw.ID)
		if err != nil {
			return store.Change{}, issues, err
		}
		c.Kind, c.Switch, c.Previous = "update", sw.ID, prev
	}
	saved, err := e.cfg.Store.AddChange(c)
	if err != nil {
		return store.Change{}, issues, err
	}
	e.emit(Event{Kind: EventChange, Switch: saved.ID})
	return saved, issues, nil
}

// Changes 返回全部变更（新的在前）。
func (e *Engine) Changes() []store.Change { return e.cfg.Store.Changes() }

// PendingChanges 返回待确认的变更。
func (e *Engine) PendingChanges() []store.Change {
	var out []store.Change
	for _, c := range e.cfg.Store.Changes() {
		if c.Status == "pending" {
			out = append(out, c)
		}
	}
	return out
}

// Change 按 id 返回变更。
func (e *Engine) Change(id string) (store.Change, bool) { return e.cfg.Store.Change(id) }

// ApproveChange 写入脚本（以提交者的来源记录），并记下结果。
func (e *Engine) ApproveChange(id string) (store.Change, error) {
	c, ok := e.cfg.Store.Change(id)
	if !ok {
		return c, ErrNoChange
	}
	if c.Status != "pending" {
		return c, ErrChangeDecided
	}
	var err error
	if c.Kind == "update" {
		_, err = e.UpdateScript(c.Switch, c.Content, Source(c.Source))
	} else {
		c.Switch, _, err = e.CreateScript(c.Content)
	}
	c.Decided = time.Now()
	if err != nil {
		c.Status, c.Error = "failed", err.Error()
	} else {
		c.Status = "approved"
	}
	_ = e.cfg.Store.PutChange(c)
	e.emit(Event{Kind: EventChange, Switch: c.ID})
	return c, err
}

// RejectChange 放弃变更，脚本不变。
func (e *Engine) RejectChange(id string) (store.Change, error) {
	c, ok := e.cfg.Store.Change(id)
	if !ok {
		return c, ErrNoChange
	}
	if c.Status != "pending" {
		return c, ErrChangeDecided
	}
	c.Status, c.Decided = "rejected", time.Now()
	_ = e.cfg.Store.PutChange(c)
	e.emit(Event{Kind: EventChange, Switch: c.ID})
	return c, nil
}

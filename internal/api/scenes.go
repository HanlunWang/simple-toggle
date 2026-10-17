package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// /v2/scenes 与 /v2/automations。场景可以列出并激活、恢复；自动化规则只读（修改走 app）。

type sceneDTO struct {
	Active   bool              `json:"active"`
	ID       string            `json:"id"`
	Icon     string            `json:"icon,omitempty"`
	Name     string            `json:"name"`
	Restore  bool              `json:"restore"`
	Shortcut string            `json:"shortcut,omitempty"`
	Steps    []store.SceneStep `json:"steps"`
}

func sceneOut(sv engine.SceneView) sceneDTO {
	steps := sv.Steps
	if steps == nil {
		steps = []store.SceneStep{}
	}
	return sceneDTO{Active: sv.Active, ID: sv.ID, Icon: sv.Icon, Name: sv.Name, Restore: sv.Restore, Shortcut: sv.Shortcut, Steps: steps}
}

// scenes 处理 /v2/scenes[/<id>[/activate|restore|toggle]]。
func (s *Server) scenes(q *request) {
	m, p := q.r.Method, q.parts
	switch {
	case m == http.MethodGet && len(p) == 2:
		list := s.eng.Scenes()
		out := make([]sceneDTO, 0, len(list))
		for _, sv := range list {
			out = append(out, sceneOut(sv))
		}
		writeJSON(q.w, 200, out, true)
	case m == http.MethodGet && len(p) == 3:
		sv, ok := s.eng.Scene(p[2])
		if !ok {
			writeError(q.w, 404, fmt.Sprintf("no scene with id or name %q", p[2]))
			return
		}
		writeJSON(q.w, 200, sceneOut(sv), true)
	case m == http.MethodPost && len(p) == 4:
		var (
			ch  <-chan store.Run
			err error
		)
		switch p[3] {
		case "activate":
			ch, err = s.eng.ActivateScene(p[2], q.source, q.detail)
		case "restore":
			ch, err = s.eng.RestoreScene(p[2], q.source, q.detail)
		case "toggle":
			ch, err = s.eng.ToggleScene(p[2], q.source, q.detail)
		default:
			writeError(q.w, 404, fmt.Sprintf("unknown scene action %q; expected activate, restore or toggle", p[3]))
			return
		}
		switch {
		case errors.Is(err, engine.ErrNoScene):
			writeError(q.w, 404, fmt.Sprintf("no scene with id or name %q", p[2]))
			return
		case err != nil:
			writeError(q.w, 409, err.Error())
			return
		}
		wait := 30 * time.Second
		if w := q.r.URL.Query().Get("wait"); w != "" {
			if n, err := strconv.Atoi(w); err == nil && n >= 0 {
				wait = time.Duration(n) * time.Second
			}
		}
		select {
		case rec := <-ch:
			sv, _ := s.eng.Scene(p[2])
			writeJSON(q.w, 200, map[string]any{"run": v2Run(&rec), "scene": sceneOut(sv)}, true)
		case <-time.After(wait):
			writeJSON(q.w, 202, map[string]any{"accepted": true}, true)
		}
	default:
		writeError(q.w, 404, fmt.Sprintf("unknown route %s %s", m, q.r.URL.Path))
	}
}

// automations 处理 GET /v2/automations。
func (s *Server) automations(q *request) {
	if q.r.Method != http.MethodGet || len(q.parts) != 2 || s.opts.Store == nil {
		writeError(q.w, 404, fmt.Sprintf("unknown route %s %s", q.r.Method, q.r.URL.Path))
		return
	}
	rules := s.opts.Store.Rules()
	if rules == nil {
		rules = []store.Rule{}
	}
	writeJSON(q.w, 200, map[string]any{
		"pausedUntil": s.opts.Store.Setting("automation.pausedUntil"),
		"rules":       rules,
	}, true)
}

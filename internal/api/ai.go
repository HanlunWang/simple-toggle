package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// AI 权限分级与「改脚本前先确认」。只对标记为 AI 来源的请求生效：这是防止 agent
// 失误的护栏，不是安全边界（本机程序本来就能直接改脚本文件）。

// 权限项；设置键是 "ai.<权限>"，值为 "true" / "false"。
const (
	PermRead   = "read"
	PermToggle = "toggle"
	PermWrite  = "write"
	PermDelete = "delete"

	SettingConfirm = "ai.confirm" // 默认 true
)

// PermDefaults 是未设置时的默认值：删除脚本默认不允许。
var PermDefaults = map[string]bool{PermRead: true, PermToggle: true, PermWrite: true, PermDelete: false}

var permWords = map[string]string{
	PermRead:   "read switches",
	PermToggle: "turn switches on or off, run actions or activate scenes",
	PermWrite:  "create or change scripts",
	PermDelete: "delete scripts",
}

// AIAllowed 读取一项权限。
func AIAllowed(st *store.Store, perm string) bool {
	if st == nil {
		return PermDefaults[perm]
	}
	switch st.Setting("ai." + perm) {
	case "true":
		return true
	case "false":
		return false
	}
	return PermDefaults[perm]
}

// AIConfirm 报告 AI 改脚本前是否需要用户确认。
func AIConfirm(st *store.Store) bool {
	return st == nil || st.Setting(SettingConfirm) != "false"
}

// permFor 返回请求需要的权限；空表示不需要（如 ping、validate）。
func permFor(method string, parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	switch {
	case last == "ping" || (method == http.MethodPost && len(parts) == 2 && last == "validate"):
		return ""
	case method == http.MethodGet:
		return PermRead
	case method == http.MethodDelete:
		return PermDelete
	case method == http.MethodPost && len(parts) == 2 && last == "scripts":
		return PermWrite
	case method == http.MethodPut && last == "script":
		return PermWrite
	}
	return PermToggle
}

// checkAI 在 AI 请求没有权限时写 403 并返回 false。
func (s *Server) checkAI(q *request) bool {
	if q.source != engine.AI {
		return true
	}
	perm := permFor(q.r.Method, q.parts)
	if perm == "" || AIAllowed(s.opts.Store, perm) {
		return true
	}
	writeError(q.w, http.StatusForbidden, fmt.Sprintf(
		"SimpleToggle's settings do not allow AI agents to %s. Ask the user to allow it in Settings → AI & Integrations.", permWords[perm]))
	return false
}

// needsApproval 报告这次写脚本是否要先交给用户确认。
func (s *Server) needsApproval(q *request) bool {
	return q.source == engine.AI && AIConfirm(s.opts.Store)
}

type pendingDTO struct {
	Pending  bool       `json:"pending"`
	ChangeID string     `json:"changeId"`
	Status   string     `json:"status"`
	Issues   any        `json:"issues"`
	Message  string     `json:"message"`
	Change   *changeDTO `json:"change,omitempty"`
}

type changeDTO struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Switch  string `json:"switch,omitempty"`
	Name    string `json:"name,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Created string `json:"created"`
	Decided string `json:"decided,omitempty"`
}

func changeOut(c store.Change) *changeDTO {
	d := &changeDTO{ID: c.ID, Kind: c.Kind, Switch: c.Switch, Name: c.Name, Status: c.Status, Error: c.Error, Created: c.Created.Format("2006-01-02T15:04:05Z07:00")}
	if !c.Decided.IsZero() {
		d.Decided = c.Decided.Format("2006-01-02T15:04:05Z07:00")
	}
	return d
}

// propose 把 AI 写的脚本存成待确认变更，返回 202。
func (s *Server) propose(q *request, id string) {
	c, issues, err := s.eng.ProposeScript(id, string(q.body), q.source, q.detail)
	var lint *engine.LintError
	switch {
	case errors.As(err, &lint):
		writeJSON(q.w, 422, issuesDTO{Issues: issuesOrEmpty(lint.Issues), Valid: false}, true)
		return
	case errors.Is(err, engine.ErrNotFound):
		notFound(q.w, id)
		return
	case err != nil:
		writeError(q.w, 500, err.Error())
		return
	}
	writeJSON(q.w, http.StatusAccepted, pendingDTO{
		Pending: true, ChangeID: c.ID, Status: c.Status, Issues: issuesOrEmpty(issues), Change: changeOut(c),
		Message: fmt.Sprintf("The script is waiting for the user's approval in SimpleToggle; nothing was written yet. "+
			"Tell the user, then check the result with simpletoggle_change_status (GET /v2/changes/%s).", c.ID),
	}, true)
}

// changes 处理 GET /v2/changes[/<id>]。批准与拒绝只能在 app 里做。
func (s *Server) changes(q *request) {
	m, p := q.r.Method, q.parts
	switch {
	case m == http.MethodGet && len(p) == 2:
		list := s.eng.Changes()
		out := make([]*changeDTO, 0, len(list))
		for _, c := range list {
			out = append(out, changeOut(c))
		}
		writeJSON(q.w, 200, out, true)
	case m == http.MethodGet && len(p) == 3:
		c, ok := s.eng.Change(p[2])
		if !ok {
			writeError(q.w, 404, fmt.Sprintf("no change with id %q", p[2]))
			return
		}
		writeJSON(q.w, 200, changeOut(c), true)
	default:
		writeError(q.w, 404, fmt.Sprintf("unknown route %s %s (changes are approved in the SimpleToggle app)", m, q.r.URL.Path))
	}
}

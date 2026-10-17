// Package api 是本地控制 API：只监听 127.0.0.1，CLI、MCP 服务器与 skill 都通过它与 app 通信。
//
//	/v1  与 v1（Swift 版）的路由、状态码、错误文案与返回结构保持一致
//	/v2  新功能：按钮执行、运行记录、事件流（SSE）、更完整的开关信息
//
// 写请求必须带 Authorization: Bearer <令牌>；令牌在发现文件（权限 0600）里，
// CLI 与 MCP 自动读取。Host 头必须是回环地址，挡住浏览器发起的跨站请求与 DNS 重绑定。
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/keys"
	"github.com/HanlunWang/open-toggle/internal/platform/darwin"
	"github.com/HanlunWang/open-toggle/internal/store"
)

const (
	DefaultPort = 43737
	maxBody     = 4 << 20
	// opWait 是 on / off 返回前等待结果的上限；超时就返回当时的状态（与 v1 一样不阻塞）。
	opWait = 5 * time.Second
	// HeaderClient 标识调用方：cli、cli/agent:<名称>、mcp/<客户端名>。
	HeaderClient = "X-OpenToggle-Client"
)

// Discovery 是发现文件的内容。
type Discovery struct {
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
	Token   string `json:"token,omitempty"`
	Version string `json:"version,omitempty"`
}

// ReadDiscovery 读取发现文件。
func ReadDiscovery(path string) (Discovery, error) {
	var d Discovery
	data, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal(data, &d)
}

type Options struct {
	Port          int
	DiscoveryPath string
	Version       string
	Store         *store.Store
	// 以下可在测试中替换
	PostKey   func(keys.Spec) darwin.PostResult
	AXTrusted func() bool
}

type Server struct {
	eng   *engine.Engine
	opts  Options
	token string
	srv   *http.Server
	ln    net.Listener
}

func New(eng *engine.Engine, opts Options) *Server {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	if opts.PostKey == nil {
		opts.PostKey = postSpec
	}
	if opts.AXTrusted == nil {
		opts.AXTrusted = darwin.AccessibilityTrusted
	}
	s := &Server{eng: eng, opts: opts}
	// 沿用已有令牌，CLI 与已配置的客户端不受 app 重启影响
	if d, err := ReadDiscovery(opts.DiscoveryPath); err == nil && len(d.Token) >= 32 {
		s.token = d.Token
	} else {
		s.token = newToken()
	}
	return s
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "ot_" + hex.EncodeToString(b)
}

// Token 返回当前令牌。
func (s *Server) Token() string { return s.token }

// Start 开始监听并写发现文件。
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.opts.Port)))
	if err != nil {
		return fmt.Errorf("control API: %w", err)
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return s.writeDiscovery()
}

// Close 停止服务并删除发现文件。
func (s *Server) Close() {
	if s.srv != nil {
		_ = s.srv.Close()
	}
	if s.opts.DiscoveryPath != "" {
		_ = os.Remove(s.opts.DiscoveryPath)
	}
}

// RegenerateToken 换一个新令牌（设置里的「重新生成」）。
func (s *Server) RegenerateToken() error {
	s.token = newToken()
	return s.writeDiscovery()
}

func (s *Server) writeDiscovery() error {
	if s.opts.DiscoveryPath == "" {
		return nil
	}
	d := Discovery{Port: s.opts.Port, PID: os.Getpid(), Token: s.token, Version: s.opts.Version}
	data, _ := json.Marshal(d)
	if err := os.MkdirAll(filepath.Dir(s.opts.DiscoveryPath), 0o755); err != nil {
		return err
	}
	tmp := s.opts.DiscoveryPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.opts.DiscoveryPath)
}

// ───────── 路由 ─────────

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

type request struct {
	w      http.ResponseWriter
	r      *http.Request
	parts  []string
	body   []byte
	source engine.Source
	detail string
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if !loopbackHost(r.Host) {
		writeError(w, http.StatusForbidden, "requests must target 127.0.0.1 or localhost")
		return
	}
	// 先按 "/" 切分再逐段解码：编码进 id 的 %2F 不能变成路径分隔符；中文 id 需要解码
	raw := strings.Trim(r.URL.EscapedPath(), "/")
	var parts []string
	for _, p := range strings.Split(raw, "/") {
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
		if p != "" {
			parts = append(parts, p)
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	req := &request{w: w, r: r, parts: parts, body: body}
	req.source, req.detail = sourceOf(r.Header.Get(HeaderClient))

	if r.Method != http.MethodGet && !s.authorized(r, parts) {
		writeError(w, http.StatusUnauthorized, "missing or invalid token; read it from ~/.config/open-toggle/api.json (the opentoggle CLI and MCP server do this automatically)")
		return
	}
	switch {
	case len(parts) >= 1 && parts[0] == "v1":
		s.v1(req)
	case len(parts) >= 1 && parts[0] == "v2":
		s.v2(req)
	default:
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown route %s %s", r.Method, r.URL.Path))
	}
}

// authorized 检查令牌。纯计算的 validate 不需要令牌。
func (s *Server) authorized(r *http.Request, parts []string) bool {
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "validate" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return got != "" && subtleEqual(got, s.token)
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func loopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// sourceOf 把调用方标识映射为运行记录的来源。
func sourceOf(client string) (engine.Source, string) {
	switch {
	case strings.HasPrefix(client, "mcp/"):
		return engine.AI, strings.TrimPrefix(client, "mcp/")
	case strings.HasPrefix(client, "cli/agent:"):
		return engine.AI, strings.TrimPrefix(client, "cli/agent:") + " · CLI"
	case client == "cli":
		return engine.CLI, ""
	}
	return engine.API, client
}

func (s *Server) v1(q *request) {
	m, p := q.r.Method, q.parts
	switch {
	case m == http.MethodGet && len(p) == 2 && p[1] == "ping":
		writeJSON(q.w, 200, map[string]any{"app": "OpenToggle", "version": s.opts.Version, "accessibility": s.opts.AXTrusted()}, false)
	case m == http.MethodGet && len(p) == 2 && p[1] == "switches":
		views := s.eng.List()
		out := make([]switchDTO, 0, len(views))
		for _, v := range views {
			out = append(out, v1Switch(v))
		}
		writeJSON(q.w, 200, out, true)
	case m == http.MethodPost && len(p) == 2 && p[1] == "scripts":
		s.createScript(q)
	case m == http.MethodPost && len(p) == 2 && p[1] == "press":
		s.press(q)
	case m == http.MethodPost && len(p) == 2 && p[1] == "validate":
		issues := contract.Lint(string(q.body))
		writeJSON(q.w, 200, issuesDTO{Issues: issuesOrEmpty(issues), Valid: !contract.HasErrors(issues)}, true)
	case len(p) == 3 && p[1] == "switches":
		s.switchResource(q, p[2], false)
	case len(p) == 4 && p[1] == "switches":
		s.switchAction(q, p[2], p[3], false)
	default:
		writeError(q.w, 404, fmt.Sprintf("unknown route %s %s", m, q.r.URL.Path))
	}
}

func (s *Server) v2(q *request) {
	m, p := q.r.Method, q.parts
	switch {
	case m == http.MethodGet && len(p) == 2 && p[1] == "ping":
		writeJSON(q.w, 200, map[string]any{"app": "OpenToggle", "version": s.opts.Version, "accessibility": s.opts.AXTrusted(), "pid": os.Getpid()}, false)
	case m == http.MethodGet && len(p) == 2 && p[1] == "switches":
		views := s.eng.List()
		out := make([]switchV2DTO, 0, len(views))
		for _, v := range views {
			out = append(out, v2Switch(v))
		}
		writeJSON(q.w, 200, out, true)
	case m == http.MethodGet && len(p) == 2 && p[1] == "runs":
		s.runs(q)
	case m == http.MethodDelete && len(p) == 2 && p[1] == "runs":
		if s.opts.Store == nil {
			writeError(q.w, 500, "no store")
			return
		}
		if err := s.opts.Store.ClearRuns(); err != nil {
			writeError(q.w, 500, err.Error())
			return
		}
		writeJSON(q.w, 200, map[string]any{"ok": true}, false)
	case m == http.MethodGet && len(p) == 2 && p[1] == "events":
		s.events(q)
	case len(p) >= 2 && p[1] == "scenes":
		s.scenes(q)
	case len(p) >= 2 && p[1] == "automations":
		s.automations(q)
	case len(p) == 3 && p[1] == "switches":
		s.switchResource(q, p[2], true)
	case len(p) == 4 && p[1] == "switches":
		s.switchAction(q, p[2], p[3], true)
	default:
		writeError(q.w, 404, fmt.Sprintf("unknown route %s %s", m, q.r.URL.Path))
	}
}

func (s *Server) view(id string, v2 bool) any {
	v, _ := s.eng.Get(id)
	if v2 {
		return v2Switch(v)
	}
	return v1Switch(v)
}

func notFound(w http.ResponseWriter, id string) {
	writeError(w, 404, fmt.Sprintf("no switch with id %q", id))
}

func (s *Server) switchResource(q *request, rawID string, v2 bool) {
	id, ok := s.eng.Resolve(rawID)
	if !ok {
		notFound(q.w, rawID)
		return
	}
	switch q.r.Method {
	case http.MethodGet:
		writeJSON(q.w, 200, s.view(id, v2), true)
	case http.MethodDelete:
		if err := s.eng.DeleteScript(id); err != nil {
			writeError(q.w, 500, "delete failed: "+err.Error())
			return
		}
		writeJSON(q.w, 200, map[string]any{"ok": true}, false)
	default:
		writeError(q.w, 405, "method not allowed")
	}
}

func (s *Server) switchAction(q *request, rawID, action string, v2 bool) {
	id, ok := s.eng.Resolve(rawID)
	if !ok {
		notFound(q.w, rawID)
		return
	}
	m := q.r.Method
	switch {
	case m == http.MethodPost && (action == "on" || action == "off"):
		done, err := s.eng.SetSwitch(id, action == "on", q.source, q.detail)
		if errors.Is(err, engine.ErrNotToggle) {
			writeError(q.w, 409, fmt.Sprintf("%q is an action switch; use POST /v2/switches/%s/run", id, id))
			return
		}
		if err != nil {
			writeError(q.w, 500, err.Error())
			return
		}
		engine.Wait(done, opWait)
		writeJSON(q.w, 200, s.view(id, v2), true)
	case m == http.MethodPost && (action == "enable" || action == "disable"):
		if err := s.eng.SetHidden(id, action == "disable"); err != nil {
			writeError(q.w, 500, err.Error())
			return
		}
		writeJSON(q.w, 200, s.view(id, v2), true)
	case m == http.MethodPut && action == "params":
		var obj map[string]any
		if err := json.Unmarshal(q.body, &obj); err != nil || obj == nil {
			writeError(q.w, 400, "body must be a JSON object of {key: value}")
			return
		}
		values := map[string]string{}
		for k, v := range obj {
			values[k] = stringify(v)
		}
		if err := s.eng.SetParams(id, values, q.source); err != nil {
			if errors.Is(err, engine.ErrNoParam) {
				key := strings.TrimSpace(strings.TrimPrefix(err.Error(), engine.ErrNoParam.Error()))
				writeError(q.w, 422, fmt.Sprintf("switch has no param with key %s", key))
				return
			}
			writeError(q.w, 500, err.Error())
			return
		}
		writeJSON(q.w, 200, s.view(id, v2), true)
	case m == http.MethodGet && action == "script":
		content, err := s.eng.Script(id)
		if err != nil {
			writeError(q.w, 500, "failed to read script file")
			return
		}
		q.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		q.w.WriteHeader(200)
		_, _ = io.WriteString(q.w, content)
	case m == http.MethodPut && action == "script":
		issues, err := s.eng.UpdateScript(id, string(q.body), q.source)
		var lint *engine.LintError
		if errors.As(err, &lint) {
			writeJSON(q.w, 422, issuesDTO{Issues: issuesOrEmpty(lint.Issues), Valid: false}, true)
			return
		}
		if err != nil {
			writeError(q.w, 500, "save failed: "+err.Error())
			return
		}
		writeJSON(q.w, 200, writeDTO{ID: id, Issues: issuesOrEmpty(issues)}, true)
	case v2 && m == http.MethodPost && action == "run":
		s.run(q, id)
	default:
		writeError(q.w, 404, fmt.Sprintf("unknown action %q", action))
	}
}

func (s *Server) createScript(q *request) {
	id, issues, err := s.eng.CreateScript(string(q.body))
	var lint *engine.LintError
	if errors.As(err, &lint) {
		writeJSON(q.w, 422, issuesDTO{Issues: issuesOrEmpty(lint.Issues), Valid: false}, true)
		return
	}
	if err != nil {
		writeError(q.w, 500, "save failed: "+err.Error())
		return
	}
	writeJSON(q.w, 201, writeDTO{ID: id, Issues: issuesOrEmpty(issues)}, true)
}

// run 执行按钮型开关；?wait=秒 决定最多等多久（默认 30，0 表示不等）。
func (s *Server) run(q *request, id string) {
	wait := 30 * time.Second
	if w := q.r.URL.Query().Get("wait"); w != "" {
		if n, err := strconv.Atoi(w); err == nil && n >= 0 {
			wait = time.Duration(n) * time.Second
		}
	}
	ch, err := s.eng.Run(id, q.source, q.detail)
	switch {
	case errors.Is(err, engine.ErrNotAction):
		writeError(q.w, 409, fmt.Sprintf("%q is not an action switch; use on/off", id))
		return
	case errors.Is(err, engine.ErrBusy):
		writeError(q.w, 409, fmt.Sprintf("%q is already running", id))
		return
	case err != nil:
		writeError(q.w, 500, err.Error())
		return
	}
	select {
	case rec := <-ch:
		writeJSON(q.w, 200, map[string]any{"run": v2Run(&rec), "switch": s.view(id, true)}, true)
	case <-time.After(wait):
		writeJSON(q.w, 202, map[string]any{"switch": s.view(id, true)}, true)
	}
}

func (s *Server) runs(q *request) {
	if s.opts.Store == nil {
		writeJSON(q.w, 200, []runDTO{}, true)
		return
	}
	qs := q.r.URL.Query()
	f := store.Filter{Source: qs.Get("source"), FailedOnly: qs.Get("failed") == "1" || qs.Get("failed") == "true", Limit: 50}
	if id := qs.Get("switch"); id != "" {
		if resolved, ok := s.eng.Resolve(id); ok {
			id = resolved
		}
		f.Switch = id
	}
	if n, err := strconv.Atoi(qs.Get("limit")); err == nil && n > 0 {
		f.Limit = min(n, store.MaxRecentRuns)
	}
	runs := s.opts.Store.Runs(f)
	out := make([]*runDTO, 0, len(runs))
	for i := range runs {
		out = append(out, v2Run(&runs[i]))
	}
	writeJSON(q.w, 200, out, true)
}

// events 推送 SSE：event 为 state / run / reloaded，data 为 JSON。
func (s *Server) events(q *request) {
	fl, ok := q.w.(http.Flusher)
	if !ok {
		writeError(q.w, 500, "streaming unsupported")
		return
	}
	ch, cancel := s.eng.Subscribe()
	defer cancel()
	h := q.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	q.w.WriteHeader(200)
	fmt.Fprint(q.w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-q.r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(q.w, ": ping\n\n")
			fl.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			payload := map[string]any{"switch": ev.Switch}
			if ev.View != nil {
				payload["view"] = v2Switch(*ev.View)
			}
			if ev.Run != nil {
				payload["run"] = v2Run(ev.Run)
			}
			data, _ := json.Marshal(payload)
			fmt.Fprintf(q.w, "event: %s\ndata: %s\n\n", ev.Kind, data)
			fl.Flush()
		}
	}
}

// press 由 app 进程代发按键：辅助功能授权只需给 app 这一个长期进程。
func (s *Server) press(q *request) {
	spec, ok := keys.Parse(strings.TrimSpace(string(q.body)))
	if !ok {
		writeError(q.w, 400, "body must be a valid key spec (e.g. f15, cmd+shift+k, mouse:middle)")
		return
	}
	res := s.opts.PostKey(spec)
	if res == darwin.NotDelivered {
		// app 刚启动时首次合成偶尔被丢（事件通道未就绪），重试一次再下结论
		time.Sleep(150 * time.Millisecond)
		res = s.opts.PostKey(spec)
	}
	switch res {
	case darwin.Delivered:
		writeJSON(q.w, 200, map[string]any{"ok": true, "key": spec.Canonical()}, false)
	case darwin.NotTrusted:
		writeError(q.w, 403, "accessibility permission not granted for OpenToggle — System Settings → Privacy & Security → Accessibility")
	case darwin.NotDelivered:
		writeError(q.w, 403, "key event was rejected by the system — re-grant Accessibility for OpenToggle")
	default:
		writeError(q.w, 500, "failed to create the key event")
	}
}

func postSpec(spec keys.Spec) darwin.PostResult {
	if spec.Target == keys.TargetMouse {
		return darwin.PostMouse(int(spec.Button))
	}
	return darwin.PostKey(spec.Code, spec.Flags())
}

// ───────── 输出 ─────────

func stringify(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, v any, pretty bool) {
	var data []byte
	if pretty {
		data, _ = json.MarshalIndent(v, "", "  ")
	} else {
		data, _ = json.Marshal(v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg}, false)
}

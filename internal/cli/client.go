package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/api"
	"github.com/HanlunWang/simple-toggle/internal/paths"
)

// ErrNotRunning 表示连不上 app。
var ErrNotRunning = errors.New("SimpleToggle is not running — open the app first")

// Client 是控制 API 的客户端（CLI 与 MCP 共用）。
type Client struct {
	Base   string
	Token  string
	Caller string // X-SimpleToggle-Client
	HTTP   *http.Client
}

// NewClient 从发现文件读取端口与令牌；读不到时用默认端口。
func NewClient(caller string) *Client {
	port := paths.Port()
	token := ""
	if d, err := api.ReadDiscovery(paths.Discovery()); err == nil {
		if d.Port > 0 {
			port = d.Port
		}
		token = d.Token
	}
	return &Client{
		Base: fmt.Sprintf("http://127.0.0.1:%d", port), Token: token, Caller: caller,
		HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

// Do 发请求，返回状态码与响应体；连不上时返回 ErrNotRunning。
func (c *Client) Do(method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, c.Base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Caller != "" {
		req.Header.Set(api.HeaderClient, c.Caller)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, ErrNotRunning
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

// AgentName 识别由 AI agent 调用的情况，用于运行记录的来源。
// SIMPLETOGGLE_AGENT 可由任何 agent 自报名称；CLAUDECODE 由 Claude Code 设置。
func AgentName() string {
	if v := strings.TrimSpace(os.Getenv("SIMPLETOGGLE_AGENT")); v != "" {
		return v
	}
	if os.Getenv("CLAUDECODE") != "" {
		return "Claude Code"
	}
	return ""
}

// CallerID 是 CLI 发请求时的调用方标识。
func CallerID() string {
	if a := AgentName(); a != "" {
		return "cli/agent:" + a
	}
	return "cli"
}

// PathEscape 对 id 做路径转义（中文等非 ASCII 字符）。
func PathEscape(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r == '/' || r == '?' || r == '#' || r == '%' || r == ' ' || r > 127 || r < 32:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

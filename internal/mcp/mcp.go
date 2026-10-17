// Package mcp 是 `opentoggle mcp`：MCP stdio 服务器，工具调用转发到本地控制 API
// （validate 除外，离线执行）。v1 的 8 个工具名称、描述与参数保持不变。
//
// 接入 Claude Code：claude mcp add --scope user opentoggle -- <opentoggle 路径> mcp
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/HanlunWang/open-toggle/internal/cli"
	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/version"
)

type idArg struct {
	ID string `json:"id" jsonschema:"Switch id (script file name, e.g. keep-awake.sh)"`
}

type setSwitchArgs struct {
	ID string `json:"id" jsonschema:"Switch id (script file name, e.g. keep-awake.sh)"`
	On bool   `json:"on" jsonschema:"true = turn on, false = turn off"`
}

type setParamsArgs struct {
	ID     string         `json:"id" jsonschema:"Switch id"`
	Params map[string]any `json:"params" jsonschema:"Map of param key to new value (values are strings)"`
}

type setEnabledArgs struct {
	ID      string `json:"id" jsonschema:"Switch id"`
	Enabled bool   `json:"enabled" jsonschema:"true = show in menu bar, false = hide (does NOT stop the switch)"`
}

type writeScriptArgs struct {
	ID      string `json:"id,omitempty" jsonschema:"Existing switch id to update; omit to create a new switch"`
	Content string `json:"content" jsonschema:"Full script content including shebang and contract header"`
}

type contentArg struct {
	Content string `json:"content" jsonschema:"Full script content to validate"`
}

type getRunsArgs struct {
	ID         string `json:"id,omitempty" jsonschema:"Only runs of this switch; omit for all switches"`
	FailedOnly bool   `json:"failed_only,omitempty" jsonschema:"Only failed runs"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of runs to return (default 20)"`
}

type sceneArgs struct {
	ID     string `json:"id" jsonschema:"Scene id or name"`
	Action string `json:"action,omitempty" jsonschema:"activate, restore, or toggle (default: activate, or restore when already active)"`
}

type changeArg struct {
	ID string `json:"id" jsonschema:"Change id returned by opentoggle_write_script (e.g. chg-3)"`
}

type empty struct{}

// Run 在 stdin / stdout 上运行 MCP 服务器，直到对端断开。
func Run(ctx context.Context) error {
	s := NewServer()
	return s.Run(ctx, &sdk.StdioTransport{})
}

// NewServer 创建注册好全部工具的服务器（测试用内存传输连接它）。
func NewServer() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "opentoggle", Version: version.Version}, nil)

	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_list",
		Description: "List all OpenToggle switches with id, name, type (toggle, daemon or action), current state (on/off/error/unknown), enabled flag, and parameters with current values."},
		func(ctx context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, any, error) {
			return forward(req, "GET", "/v1/switches", nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_set_switch",
		Description: "Turn a toggle or daemon switch on or off. Works on hidden switches too. Action switches have no on/off state; use opentoggle_run_action."},
		func(ctx context.Context, req *sdk.CallToolRequest, in setSwitchArgs) (*sdk.CallToolResult, any, error) {
			action := "off"
			if in.On {
				action = "on"
			}
			return forward(req, "POST", "/v1/switches/"+cli.PathEscape(in.ID)+"/"+action, nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_run_action",
		Description: "Run an action switch (a one-shot command such as Flush DNS) and return its exit code, duration and output. Waits up to 30 seconds."},
		func(ctx context.Context, req *sdk.CallToolRequest, in idArg) (*sdk.CallToolResult, any, error) {
			return forward(req, "POST", "/v2/switches/"+cli.PathEscape(in.ID)+"/run", nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_set_params",
		Description: "Set one or more parameter values on a switch. A running switch restarts with the new values."},
		func(ctx context.Context, req *sdk.CallToolRequest, in setParamsArgs) (*sdk.CallToolResult, any, error) {
			body, _ := json.Marshal(in.Params)
			return forward(req, "PUT", "/v1/switches/"+cli.PathEscape(in.ID)+"/params", body)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_set_enabled",
		Description: "Show or hide a switch in the menu bar panel. Hiding is visibility-only: a RUNNING switch keeps running (and keeps restoring at launch) — call opentoggle_set_switch with on=false first if you intend to stop it."},
		func(ctx context.Context, req *sdk.CallToolRequest, in setEnabledArgs) (*sdk.CallToolResult, any, error) {
			action := "disable"
			if in.Enabled {
				action = "enable"
			}
			return forward(req, "POST", "/v1/switches/"+cli.PathEscape(in.ID)+"/"+action, nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_get_script",
		Description: "Read the full script source of a switch."},
		func(ctx context.Context, req *sdk.CallToolRequest, in idArg) (*sdk.CallToolResult, any, error) {
			return forward(req, "GET", "/v1/switches/"+cli.PathEscape(in.ID)+"/script", nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_write_script",
		Description: "Create a new switch script (omit id) or replace an existing switch's script (pass id). Content must follow the OpenToggle contract (# <switch.*> directives); it is linted and rejected on errors — fix and retry. Warnings are returned but do not block."},
		func(ctx context.Context, req *sdk.CallToolRequest, in writeScriptArgs) (*sdk.CallToolResult, any, error) {
			if in.ID != "" {
				return forward(req, "PUT", "/v1/switches/"+cli.PathEscape(in.ID)+"/script", []byte(in.Content))
			}
			return forward(req, "POST", "/v1/scripts", []byte(in.Content))
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_validate",
		Description: "Lint script content against the OpenToggle contract without saving. Works even when the app is not running."},
		func(ctx context.Context, req *sdk.CallToolRequest, in contentArg) (*sdk.CallToolResult, any, error) {
			issues := contract.Lint(in.Content)
			if issues == nil {
				issues = []contract.Issue{}
			}
			data, _ := json.MarshalIndent(map[string]any{"valid": !contract.HasErrors(issues), "issues": issues}, "", "  ")
			return text(string(data), false), nil, nil
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_delete",
		Description: "Delete a switch (its script is moved to the Trash)."},
		func(ctx context.Context, req *sdk.CallToolRequest, in idArg) (*sdk.CallToolResult, any, error) {
			return forward(req, "DELETE", "/v1/switches/"+cli.PathEscape(in.ID), nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_get_runs",
		Description: "Read recent runs (newest first): which switch, action, source (manual, shortcut, automation, ai, cli…), duration, exit code and output. Use it to debug a script that fails."},
		func(ctx context.Context, req *sdk.CallToolRequest, in getRunsArgs) (*sdk.CallToolResult, any, error) {
			q := url.Values{}
			limit := in.Limit
			if limit <= 0 {
				limit = 20
			}
			q.Set("limit", fmt.Sprint(limit))
			if in.ID != "" {
				q.Set("switch", in.ID)
			}
			if in.FailedOnly {
				q.Set("failed", "1")
			}
			return forward(req, "GET", "/v2/runs?"+q.Encode(), nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_change_status",
		Description: "Check a script change that is waiting for the user's approval (opentoggle_write_script returns its id when OpenToggle asks the user to confirm AI edits). Status is pending, approved, rejected or failed; approved changes are written to disk."},
		func(ctx context.Context, req *sdk.CallToolRequest, in changeArg) (*sdk.CallToolResult, any, error) {
			return forward(req, "GET", "/v2/changes/"+cli.PathEscape(in.ID), nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_list_scenes",
		Description: "List scenes (one-click groups of switches): id, name, steps (switch + on/off/run), whether it is active, and its shortcut."},
		func(ctx context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, any, error) {
			return forward(req, "GET", "/v2/scenes", nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_activate_scene",
		Description: "Activate a scene (runs its steps in order), or restore the switches to how they were before it was activated. Returns the result of each step."},
		func(ctx context.Context, req *sdk.CallToolRequest, in sceneArgs) (*sdk.CallToolResult, any, error) {
			action := in.Action
			switch action {
			case "":
				action = "toggle"
			case "activate", "restore", "toggle":
			default:
				return text("action must be activate, restore or toggle", true), nil, nil
			}
			return forward(req, "POST", "/v2/scenes/"+cli.PathEscape(in.ID)+"/"+action, nil)
		})
	sdk.AddTool(s, &sdk.Tool{Name: "opentoggle_list_automations",
		Description: "List automation rules (trigger such as time, Wi-Fi, power, app, display, wake, file or shortcut → action on a switch or scene) and whether automations are paused. Read-only: the user edits rules in the app."},
		func(ctx context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, any, error) {
			return forward(req, "GET", "/v2/automations", nil)
		})
	return s
}

// forward 把工具调用转成 API 请求；连接失败与 4xx/5xx 都作为工具错误返回，不让会话中断。
func forward(req *sdk.CallToolRequest, method, path string, body []byte) (*sdk.CallToolResult, any, error) {
	c := cli.NewClient("mcp/" + clientName(req))
	code, data, err := c.Do(method, path, body)
	if err != nil {
		return text("OpenToggle app is not running — ask the user to start it first", true), nil, nil
	}
	return text(string(data), code >= 400), nil, nil
}

func clientName(req *sdk.CallToolRequest) string {
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
			return p.ClientInfo.Name
		}
	}
	if n := os.Getenv("OPENTOGGLE_AGENT"); n != "" {
		return n
	}
	return "unknown"
}

func text(s string, isError bool) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}, IsError: isError}
}

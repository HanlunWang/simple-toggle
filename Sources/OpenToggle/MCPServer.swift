import Foundation

/// MCP stdio server（`opentoggle mcp`）：newline-delimited JSON-RPC 2.0。
/// 工具调用转发到本地控制 API（validate 除外，离线执行）。
/// 接入 Claude Code：claude mcp add opentoggle -- <path-to-binary> mcp
enum MCPServer {
    static func run() -> Never {
        while let line = readLine(strippingNewline: true) {
            guard !line.isEmpty,
                  let data = line.data(using: .utf8),
                  let message = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else { continue }

            let method = message["method"] as? String ?? ""
            let id = message["id"]

            // 通知（无 id）不需要响应
            guard id != nil else { continue }

            switch method {
            case "initialize":
                reply(id: id, result: [
                    "protocolVersion": "2024-11-05",
                    "capabilities": ["tools": [:] as [String: Any]],
                    "serverInfo": ["name": "opentoggle", "version": appVersion],
                ])
            case "tools/list":
                reply(id: id, result: ["tools": toolDefinitions])
            case "tools/call":
                let params = message["params"] as? [String: Any] ?? [:]
                let name = params["name"] as? String ?? ""
                let args = params["arguments"] as? [String: Any] ?? [:]
                let (text, isError) = call(name, args)
                reply(id: id, result: [
                    "content": [["type": "text", "text": text]],
                    "isError": isError,
                ])
            case "ping":
                reply(id: id, result: [:] as [String: Any])
            default:
                replyError(id: id, code: -32601, message: "method not found: \(method)")
            }
        }
        exit(0)
    }

    // MARK: - tools

    private static let toolDefinitions: [[String: Any]] = [
        tool("opentoggle_list",
             "List all OpenToggle switches with id, name, type, current state (on/off/error/unknown), enabled flag, and parameters with current values.",
             properties: [:], required: []),
        tool("opentoggle_set_switch",
             "Turn a switch on or off. The switch must be enabled.",
             properties: ["id": strProp("Switch id (script file name, e.g. keep-awake.sh)"),
                          "on": ["type": "boolean", "description": "true = turn on, false = turn off"]],
             required: ["id", "on"]),
        tool("opentoggle_set_params",
             "Set one or more parameter values on a switch. A running switch restarts with the new values.",
             properties: ["id": strProp("Switch id"),
                          "params": ["type": "object", "description": "Map of param key to new value (values are strings)"]],
             required: ["id", "params"]),
        tool("opentoggle_set_enabled",
             "Enable (show in menu bar) or disable (hide, keep script) a switch.",
             properties: ["id": strProp("Switch id"),
                          "enabled": ["type": "boolean"]],
             required: ["id", "enabled"]),
        tool("opentoggle_get_script",
             "Read the full script source of a switch.",
             properties: ["id": strProp("Switch id")],
             required: ["id"]),
        tool("opentoggle_write_script",
             "Create a new switch script (omit id) or replace an existing switch's script (pass id). Content must follow the OpenToggle contract (# <switch.*> directives); it is linted and rejected on errors — fix and retry. Warnings are returned but do not block.",
             properties: ["id": strProp("Existing switch id to update; omit to create a new switch"),
                          "content": strProp("Full script content including shebang and contract header")],
             required: ["content"]),
        tool("opentoggle_validate",
             "Lint script content against the OpenToggle contract without saving. Works even when the app is not running.",
             properties: ["content": strProp("Full script content to validate")],
             required: ["content"]),
        tool("opentoggle_delete",
             "Delete a switch (its script is moved to the Trash).",
             properties: ["id": strProp("Switch id")],
             required: ["id"]),
    ]

    private static func call(_ name: String, _ args: [String: Any]) -> (String, Bool) {
        switch name {
        case "opentoggle_list":
            return apiCall("GET", "/v1/switches")
        case "opentoggle_set_switch":
            guard let id = args["id"] as? String, let on = args["on"] as? Bool else {
                return ("missing required arguments: id, on", true)
            }
            return apiCall("POST", "/v1/switches/\(id)/\(on ? "on" : "off")")
        case "opentoggle_set_params":
            guard let id = args["id"] as? String, let params = args["params"] as? [String: Any] else {
                return ("missing required arguments: id, params", true)
            }
            let body = (try? JSONSerialization.data(withJSONObject: params)) ?? Data()
            return apiCall("PUT", "/v1/switches/\(id)/params", body: body)
        case "opentoggle_set_enabled":
            guard let id = args["id"] as? String, let enabled = args["enabled"] as? Bool else {
                return ("missing required arguments: id, enabled", true)
            }
            return apiCall("POST", "/v1/switches/\(id)/\(enabled ? "enable" : "disable")")
        case "opentoggle_get_script":
            guard let id = args["id"] as? String else { return ("missing required argument: id", true) }
            return apiCall("GET", "/v1/switches/\(id)/script")
        case "opentoggle_write_script":
            guard let content = args["content"] as? String else {
                return ("missing required argument: content", true)
            }
            if let id = args["id"] as? String, !id.isEmpty {
                return apiCall("PUT", "/v1/switches/\(id)/script", body: Data(content.utf8))
            }
            return apiCall("POST", "/v1/scripts", body: Data(content.utf8))
        case "opentoggle_validate":
            guard let content = args["content"] as? String else {
                return ("missing required argument: content", true)
            }
            let issues = ContractLinter.lint(content)
            let response: [String: Any] = [
                "valid": !ContractLinter.hasErrors(issues),
                "issues": issues.map { ["severity": $0.severity, "message": $0.message] },
            ]
            let data = (try? JSONSerialization.data(withJSONObject: response, options: [.prettyPrinted, .sortedKeys])) ?? Data()
            return (String(data: data, encoding: .utf8) ?? "{}", false)
        case "opentoggle_delete":
            guard let id = args["id"] as? String else { return ("missing required argument: id", true) }
            return apiCall("DELETE", "/v1/switches/\(id)")
        default:
            return ("unknown tool: \(name)", true)
        }
    }

    /// 转发到控制 API；与 CLI.api 不同：失败不退出进程，转成工具错误返回
    private static func apiCall(_ method: String, _ path: String, body: Data? = nil) -> (String, Bool) {
        var request = URLRequest(url: URL(string: "http://127.0.0.1:\(ControlServer.defaultPort)\(path)")!)
        request.httpMethod = method
        request.httpBody = body
        request.timeoutInterval = 15

        var result: Data?
        var failure: Error?
        var statusCode = 0
        let semaphore = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: request) { data, response, error in
            result = data
            failure = error
            statusCode = (response as? HTTPURLResponse)?.statusCode ?? 0
            semaphore.signal()
        }.resume()
        semaphore.wait()

        if failure != nil {
            return ("OpenToggle app is not running — ask the user to start it first", true)
        }
        let text = String(data: result ?? Data(), encoding: .utf8) ?? ""
        return (text, statusCode >= 400)
    }

    // MARK: - JSON-RPC plumbing

    private static func reply(id: Any?, result: [String: Any]) {
        write(["jsonrpc": "2.0", "id": id ?? NSNull(), "result": result])
    }

    private static func replyError(id: Any?, code: Int, message: String) {
        write(["jsonrpc": "2.0", "id": id ?? NSNull(), "error": ["code": code, "message": message]])
    }

    private static func write(_ obj: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: obj) else { return }
        FileHandle.standardOutput.write(data)
        FileHandle.standardOutput.write(Data("\n".utf8))
    }

    private static func tool(_ name: String, _ description: String,
                             properties: [String: Any], required: [String]) -> [String: Any] {
        [
            "name": name,
            "description": description,
            "inputSchema": [
                "type": "object",
                "properties": properties,
                "required": required,
            ],
        ]
    }

    private static func strProp(_ description: String) -> [String: Any] {
        ["type": "string", "description": description]
    }
}

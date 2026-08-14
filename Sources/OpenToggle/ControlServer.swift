import Foundation
import Network

let appVersion = "0.5.0"

// MARK: - API DTO

struct OptionDTO: Codable {
    let label: String
    let value: String
}

struct ParamDTO: Codable {
    let key: String
    let label: String
    let type: String
    let value: String
    let defaultValue: String
    let options: [OptionDTO]
    let presets: [OptionDTO]
    let min: Int?
    let max: Int?
    let hint: String?
}

struct SwitchDTO: Codable {
    let id: String
    let name: String
    let icon: String
    let type: String
    let state: String
    let enabled: Bool
    let params: [ParamDTO]
}

// MARK: - 本地控制 API（127.0.0.1:43737，仅回环）
//
// GET    /v1/ping                      → {app, version}
// GET    /v1/switches                  → [SwitchDTO]
// GET    /v1/switches/{id}             → SwitchDTO
// POST   /v1/switches/{id}/on|off      → SwitchDTO
// POST   /v1/switches/{id}/enable|disable → SwitchDTO
// PUT    /v1/switches/{id}/params      → body {"key":"value",…} → SwitchDTO
// GET    /v1/switches/{id}/script      → text/plain 脚本原文
// PUT    /v1/switches/{id}/script      → body 脚本原文；lint error → 422 {issues}
// POST   /v1/scripts                   → body 脚本原文；新建（文件名派生自 <switch.name>）
// DELETE /v1/switches/{id}             → 移到废纸篓
// POST   /v1/validate                  → body 脚本原文 → {valid, issues}
@MainActor
final class ControlServer {
    static let shared = ControlServer()
    nonisolated static let defaultPort: UInt16 = 43737

    private var listener: NWListener?

    func start() {
        let params = NWParameters.tcp
        params.allowLocalEndpointReuse = true
        params.requiredLocalEndpoint = NWEndpoint.hostPort(
            host: .ipv4(.loopback),
            port: NWEndpoint.Port(rawValue: Self.defaultPort)!
        )
        do {
            listener = try NWListener(using: params)
        } catch {
            NSLog("OpenToggle control API failed to start on port \(Self.defaultPort): \(error)")
            return
        }
        listener?.newConnectionHandler = { conn in
            HTTPConnection.serve(conn)
        }
        listener?.start(queue: .global())
        writeDiscoveryFile()
    }

    private func writeDiscoveryFile() {
        let dir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let info: [String: Any] = ["port": Int(Self.defaultPort), "pid": Int(ProcessInfo.processInfo.processIdentifier)]
        if let data = try? JSONSerialization.data(withJSONObject: info) {
            try? data.write(to: dir.appendingPathComponent("api.json"))
        }
    }
}

// MARK: - 最小 HTTP/1.1 处理

private enum HTTPConnection {
    struct Request {
        let method: String
        let path: String
        let body: Data
    }

    nonisolated static func serve(_ conn: NWConnection) {
        conn.start(queue: .global())
        // 空闲超时：半开/慢速连接 20 秒后强制关闭，防止连接常年累积
        DispatchQueue.global().asyncAfter(deadline: .now() + 20) {
            conn.cancel() // 已完成的连接重复 cancel 无害
        }
        receive(conn, Data())
    }

    private nonisolated static func receive(_ conn: NWConnection, _ buffer: Data) {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 1 << 16) { data, _, isComplete, error in
            var buf = buffer
            if let data { buf.append(data) }
            if let request = parse(buf) {
                Task { @MainActor in
                    let (status, contentType, body) = await Router.route(request)
                    respond(conn, status: status, contentType: contentType, body: body)
                }
            } else if isComplete || error != nil || buf.count > 1 << 22 {
                conn.cancel()
            } else {
                receive(conn, buf)
            }
        }
    }

    private nonisolated static func parse(_ data: Data) -> Request? {
        guard let headerEnd = data.range(of: Data("\r\n\r\n".utf8)) else { return nil }
        guard let head = String(data: data[..<headerEnd.lowerBound], encoding: .utf8) else { return nil }
        let headLines = head.components(separatedBy: "\r\n")
        let requestParts = headLines[0].split(separator: " ")
        guard requestParts.count >= 2 else { return nil }
        var contentLength = 0
        for line in headLines.dropFirst() {
            let kv = line.split(separator: ":", maxSplits: 1)
            if kv.count == 2, kv[0].lowercased() == "content-length" {
                // 钳制到 [0, 4MB]：负值会让 subdata 范围崩溃，超大值会挂死连接
                contentLength = min(max(Int(kv[1].trimmingCharacters(in: .whitespaces)) ?? 0, 0), 1 << 22)
            }
        }
        let bodyStart = headerEnd.upperBound
        guard data.count - bodyStart >= contentLength else { return nil } // 等 body 收齐
        let body = data.subdata(in: bodyStart..<(bodyStart + contentLength))
        return Request(method: String(requestParts[0]), path: String(requestParts[1]), body: body)
    }

    private nonisolated static func respond(_ conn: NWConnection, status: Int, contentType: String, body: Data) {
        let reasons = [200: "OK", 201: "Created", 400: "Bad Request", 404: "Not Found",
                       405: "Method Not Allowed", 422: "Unprocessable Entity", 500: "Internal Server Error"]
        let head = "HTTP/1.1 \(status) \(reasons[status] ?? "OK")\r\n"
            + "Content-Type: \(contentType)\r\n"
            + "Content-Length: \(body.count)\r\n"
            + "Connection: close\r\n\r\n"
        var data = Data(head.utf8)
        data.append(body)
        conn.send(content: data, completion: .contentProcessed { _ in conn.cancel() })
    }
}

// MARK: - 路由（MainActor：直接操作 SwitchManager）

@MainActor
private enum Router {
    typealias Response = (Int, String, Data)

    static func route(_ req: HTTPConnection.Request) async -> Response {
        // 客户端用 URL(string:) 构造请求，非 ASCII 的 id（如中文名）会被 percent-encode，
        // 这里必须解码回来才能匹配上 switch。先按 "/" 切分再逐段解码——反过来会让
        // 编码进 id 的 %2F 变成路径分隔符。
        let parts = req.path.split(separator: "?")[0].split(separator: "/")
            .map { String($0).removingPercentEncoding ?? String($0) }
        let manager = SwitchManager.shared

        switch (req.method, parts.count) {
        case ("GET", 2) where parts == ["v1", "ping"]:
            return json(200, [
                "app": "OpenToggle",
                "version": appVersion,
                "accessibility": KeySpec.checkAccessibility(prompt: false),
            ])

        case ("GET", 2) where parts == ["v1", "switches"]:
            return encode(200, manager.switches.map { dto($0) })

        case ("POST", 2) where parts == ["v1", "scripts"]:
            return createScript(req.body)

        case ("POST", 2) where parts == ["v1", "press"]:
            // 由 app 进程代发按键：权限只需授予 app 这一个长期进程，
            // 而不是每次敲键新起的短命子进程。
            guard let specString = String(data: req.body, encoding: .utf8)?
                .trimmingCharacters(in: .whitespacesAndNewlines),
                  let spec = KeySpec.parse(specString) else {
                return jsonError(400, "body must be a valid key spec (e.g. f15, cmd+shift+k, mouse:middle)")
            }
            // post() 内含 50ms 的送达验证等待，放到后台线程，不阻塞主 actor。
            // app 刚启动时首次 post 偶发被丢（事件通道未就绪），重试一次再下结论。
            var result = await Task.detached { spec.post() }.value
            if result == .notDelivered {
                try? await Task.sleep(for: .milliseconds(150))
                result = await Task.detached { spec.post() }.value
            }
            switch result {
            case .delivered:
                return json(200, ["ok": true, "key": spec.canonical])
            case .notTrusted:
                AccessibilityStatus.shared.markDenied()
                return jsonError(403, "accessibility permission not granted for OpenToggle — System Settings → Privacy & Security → Accessibility")
            case .notDelivered:
                AccessibilityStatus.shared.markDenied()
                return jsonError(403, "key event was rejected by the system — re-grant Accessibility for OpenToggle (a rebuild invalidates the previous grant)")
            case .eventCreationFailed:
                return jsonError(500, "failed to create the key event")
            }

        case ("POST", 2) where parts == ["v1", "validate"]:
            guard let content = String(data: req.body, encoding: .utf8) else {
                return jsonError(400, "body must be UTF-8 script content")
            }
            let issues = ContractLinter.lint(content)
            return encode(200, ValidateResponse(valid: !ContractLinter.hasErrors(issues), issues: issues))

        case (_, 3) where parts[0] == "v1" && parts[1] == "switches":
            guard let sw = find(parts[2]) else { return jsonError(404, "no switch with id \"\(parts[2])\"") }
            switch req.method {
            case "GET": return encode(200, dto(sw))
            case "DELETE":
                manager.deleteScript(sw)
                return json(200, ["ok": true])
            default: return jsonError(405, "method not allowed")
            }

        case (_, 4) where parts[0] == "v1" && parts[1] == "switches":
            guard let sw = find(parts[2]) else { return jsonError(404, "no switch with id \"\(parts[2])\"") }
            return subresource(sw, action: parts[3], req: req)

        default:
            return jsonError(404, "unknown route \(req.method) \(req.path)")
        }
    }

    private static func subresource(_ sw: SwitchScript, action: String, req: HTTPConnection.Request) -> Response {
        let manager = SwitchManager.shared
        switch (req.method, action) {
        case ("POST", "on"), ("POST", "off"):
            // 隐藏只是面板可见性，隐藏中的开关必须照常可开可关
            //（否则"保持运行并隐藏"的开关会变成全 API 都关不掉）
            manager.setSwitch(sw, to: action == "on")
            return encode(200, dto(sw))
        case ("POST", "enable"), ("POST", "disable"):
            manager.setEnabled(sw, action == "enable")
            return encode(200, dto(sw))
        case ("PUT", "params"):
            guard let obj = try? JSONSerialization.jsonObject(with: req.body) as? [String: Any] else {
                return jsonError(400, "body must be a JSON object of {key: value}")
            }
            for (key, anyValue) in obj {
                guard let param = sw.params.first(where: { $0.key == key }) else {
                    return jsonError(422, "switch has no param with key \"\(key)\"")
                }
                manager.setValue(stringify(anyValue), of: param, in: sw)
            }
            return encode(200, dto(sw))
        case ("GET", "script"):
            guard let content = try? String(contentsOf: sw.url, encoding: .utf8) else {
                return jsonError(500, "failed to read script file")
            }
            return (200, "text/plain; charset=utf-8", Data(content.utf8))
        case ("PUT", "script"):
            guard let content = String(data: req.body, encoding: .utf8) else {
                return jsonError(400, "body must be UTF-8 script content")
            }
            let issues = ContractLinter.lint(content)
            if ContractLinter.hasErrors(issues) {
                return encode(422, ValidateResponse(valid: false, issues: issues))
            }
            do {
                try manager.updateScript(at: sw.url, content: content)
                if let updated = manager.switches.first(where: { $0.id == sw.id }) {
                    manager.restartIfRunning(updated)
                }
                return encode(200, WriteResponse(id: sw.id, issues: issues))
            } catch {
                return jsonError(500, "save failed: \(error.localizedDescription)")
            }
        default:
            return jsonError(404, "unknown action \"\(action)\"")
        }
    }

    private static func createScript(_ body: Data) -> Response {
        guard let content = String(data: body, encoding: .utf8) else {
            return jsonError(400, "body must be UTF-8 script content")
        }
        let issues = ContractLinter.lint(content)
        if ContractLinter.hasErrors(issues) {
            return encode(422, ValidateResponse(valid: false, issues: issues))
        }
        // 文件名派生自 <switch.name>（kebab-case；冲突自动加序号，见 saveScript）
        var draftName = "switch"
        for line in content.components(separatedBy: "\n").prefix(40) {
            if let range = line.range(of: "<switch.name>") {
                let v = String(line[range.upperBound...]).trimmingCharacters(in: .whitespaces)
                if !v.isEmpty { draftName = v }
            }
        }
        var draft = ScriptDraft()
        draft.name = draftName
        do {
            let url = try SwitchManager.shared.saveScript(fileName: draft.fileName, content: content)
            return encode(201, WriteResponse(id: url.lastPathComponent, issues: issues))
        } catch {
            return jsonError(500, "save failed: \(error.localizedDescription)")
        }
    }

    // MARK: helpers

    struct ValidateResponse: Codable {
        let valid: Bool
        let issues: [LintIssue]
    }

    struct WriteResponse: Codable {
        let id: String
        let issues: [LintIssue]
    }

    private static func find(_ id: String) -> SwitchScript? {
        let manager = SwitchManager.shared
        return manager.switches.first { $0.id == id }
            ?? manager.switches.first { $0.id == id + ".sh" }
    }

    private static func stringify(_ value: Any) -> String {
        switch value {
        case let s as String: return s
        case let b as Bool: return b ? "true" : "false"
        case let n as NSNumber: return n.stringValue
        default: return "\(value)"
        }
    }

    private static func stateString(_ state: SwitchState?) -> String {
        switch state {
        case .on: "on"
        case .off: "off"
        case .error: "error"
        case .unknown, .none: "unknown"
        }
    }

    private static func dto(_ sw: SwitchScript) -> SwitchDTO {
        let manager = SwitchManager.shared
        return SwitchDTO(
            id: sw.id,
            name: sw.name,
            icon: sw.icon,
            type: sw.type.rawValue,
            state: stateString(manager.states[sw.id]),
            enabled: manager.isEnabled(sw),
            params: sw.params.map { p in
                ParamDTO(
                    key: p.key,
                    label: p.label,
                    type: p.type.rawValue,
                    value: manager.value(of: p, in: sw),
                    defaultValue: p.defaultValue,
                    options: p.options.map { OptionDTO(label: $0.label, value: $0.value) },
                    presets: p.presets.map { OptionDTO(label: $0.label, value: $0.value) },
                    min: p.minValue,
                    max: p.maxValue,
                    hint: p.hint
                )
            }
        )
    }

    private static func encode<T: Encodable>(_ status: Int, _ value: T) -> Response {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = (try? encoder.encode(value)) ?? Data("{}".utf8)
        return (status, "application/json", data)
    }

    private static func json(_ status: Int, _ obj: [String: Any]) -> Response {
        let data = (try? JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])) ?? Data("{}".utf8)
        return (status, "application/json", data)
    }

    private static func jsonError(_ status: Int, _ message: String) -> Response {
        json(status, ["error": message])
    }
}

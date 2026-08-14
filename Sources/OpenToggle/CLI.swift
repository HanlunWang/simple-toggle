import Foundation

/// 命令行模式：同一个二进制，带参数即 CLI（无参数启动 GUI，见 Main.swift）。
/// 除 validate / help 外均通过本地控制 API 与运行中的 app 通信。
enum CLI {
    static func run(_ args: [String]) -> Never {
        let jsonMode = args.contains("--json")
        let positional = args.filter { !$0.hasPrefix("--") }
        guard let command = positional.first else { exitWithUsage() }
        let rest = Array(positional.dropFirst())

        switch command {
        case "help", "-h", "--help":
            print(usage)
            exit(0)

        case "version":
            print("OpenToggle \(appVersion)")
            exit(0)

        case "validate":
            guard let path = rest.first else { fail("usage: opentoggle validate <file.sh>") }
            guard let content = try? String(contentsOfFile: path, encoding: .utf8) else {
                fail("cannot read \(path)")
            }
            let issues = ContractLinter.lint(content)
            reportIssues(issues, jsonMode: jsonMode)
            exit(ContractLinter.hasErrors(issues) ? 1 : 0)

        case "mcp":
            MCPServer.run() // never returns

        case "press":
            guard let specString = rest.first else {
                fail("usage: opentoggle press <keyspec>   e.g. f15, cmd+shift+k, mouse:middle")
            }
            guard let spec = KeySpec.parse(specString) else {
                fail("invalid key spec \"\(specString)\" — expected e.g. f15, cmd+shift+k, mouse:middle")
            }
            // 优先让运行中的 app 代发：辅助功能权限只需授予 app 这一个长期进程，
            // 而不是每次敲键都新起、每次重新构建都换身份的短命子进程。
            if !args.contains("--local"), let (status, body) = tryAPI("POST", "/v1/press", body: Data(spec.canonical.utf8)) {
                if status < 400 { exit(0) }
                FileHandle.standardError.write(body)
                FileHandle.standardError.write(Data("\n".utf8))
                exit(1)
            }
            // app 未运行：本地合成（终端直接调用时走这条）
            switch spec.post() {
            case .delivered:
                exit(0)
            case .notTrusted:
                _ = KeySpec.checkAccessibility(prompt: true)
                fail("accessibility permission required: System Settings → Privacy & Security → Accessibility → allow this binary")
            case .notDelivered:
                fail("key event was rejected by the system — re-grant Accessibility (rebuilding the binary invalidates the previous grant)")
            case .eventCreationFailed:
                fail("failed to create the key event")
            }

        case "doctor":
            runDoctor()
            exit(0)

        case "list":
            let data = api("GET", "/v1/switches")
            if jsonMode { printData(data); exit(0) }
            guard let switches = try? JSONDecoder().decode([SwitchDTO].self, from: data) else {
                fail("unexpected response")
            }
            for sw in switches {
                let dot = ["on": "●", "off": "○", "error": "✗", "unknown": "?"][sw.state] ?? "?"
                let enabled = sw.enabled ? "" : "  [deactivated]"
                print("\(dot) \(sw.id.padding(toLength: 22, withPad: " ", startingAt: 0)) \(sw.state.padding(toLength: 8, withPad: " ", startingAt: 0)) \(sw.type.padding(toLength: 7, withPad: " ", startingAt: 0)) \(sw.name)\(enabled)")
            }
            exit(0)

        case "on", "off":
            guard let id = rest.first else { fail("usage: opentoggle \(command) <id>") }
            printData(api("POST", "/v1/switches/\(id)/\(command)"), pretty: !jsonMode)
            exit(0)

        case "state":
            guard let id = rest.first else { fail("usage: opentoggle state <id>") }
            let data = api("GET", "/v1/switches/\(id)")
            guard let sw = try? JSONDecoder().decode(SwitchDTO.self, from: data) else { fail("unexpected response") }
            print(sw.state)
            exit(0)

        case "enable", "disable":
            guard let id = rest.first else { fail("usage: opentoggle \(command) <id>") }
            printData(api("POST", "/v1/switches/\(id)/\(command)"), pretty: !jsonMode)
            exit(0)

        case "params":
            guard let id = rest.first else { fail("usage: opentoggle params <id>") }
            let data = api("GET", "/v1/switches/\(id)")
            if jsonMode { printData(data); exit(0) }
            guard let sw = try? JSONDecoder().decode(SwitchDTO.self, from: data) else { fail("unexpected response") }
            for p in sw.params {
                var extra = ""
                if !p.options.isEmpty { extra = "  options: " + p.options.map { "\($0.label)=\($0.value)" }.joined(separator: " | ") }
                print("\(p.key) = \(p.value)  (\(p.type), default \(p.defaultValue))\(extra)")
            }
            exit(0)

        case "set":
            guard rest.count >= 2, let id = rest.first else {
                fail("usage: opentoggle set <id> <key>=<value> [...]")
            }
            var obj: [String: String] = [:]
            for pair in rest.dropFirst() {
                guard let eq = pair.firstIndex(of: "=") else { fail("expected key=value, got \"\(pair)\"") }
                obj[String(pair[..<eq])] = String(pair[pair.index(after: eq)...])
            }
            let body = try! JSONSerialization.data(withJSONObject: obj)
            printData(api("PUT", "/v1/switches/\(id)/params", body: body), pretty: !jsonMode)
            exit(0)

        case "cat":
            guard let id = rest.first else { fail("usage: opentoggle cat <id>") }
            printData(api("GET", "/v1/switches/\(id)/script"), raw: true)
            exit(0)

        case "add", "put":
            let isUpdate = command == "put"
            let pathIndex = isUpdate ? 1 : 0
            guard rest.count > pathIndex else {
                fail(isUpdate ? "usage: opentoggle put <id> <file.sh>" : "usage: opentoggle add <file.sh>")
            }
            guard let content = try? String(contentsOfFile: rest[pathIndex], encoding: .utf8) else {
                fail("cannot read \(rest[pathIndex])")
            }
            // 先本地 lint，错误尽早失败（服务端会再校验一次）
            let issues = ContractLinter.lint(content)
            reportIssues(issues, jsonMode: false)
            if ContractLinter.hasErrors(issues) { exit(1) }
            let endpoint = isUpdate ? "/v1/switches/\(rest[0])/script" : "/v1/scripts"
            printData(api(isUpdate ? "PUT" : "POST", endpoint, body: Data(content.utf8)), pretty: !jsonMode)
            exit(0)

        case "rm":
            guard let id = rest.first else { fail("usage: opentoggle rm <id>") }
            printData(api("DELETE", "/v1/switches/\(id)"), pretty: !jsonMode)
            exit(0)

        default:
            fail("unknown command \"\(command)\"\n\n\(usage)")
        }
    }

    // MARK: - HTTP client

    private static var port: UInt16 {
        let url = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/api.json")
        if let data = try? Data(contentsOf: url),
           let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let p = obj["port"] as? Int {
            return UInt16(p)
        }
        return ControlServer.defaultPort
    }

    /// 尝试请求；app 未运行时返回 nil（不退出进程）
    static func tryAPI(_ method: String, _ path: String, body: Data? = nil) -> (Int, Data)? {
        var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)\(path)")!)
        request.httpMethod = method
        request.httpBody = body
        request.timeoutInterval = 10

        var result: Data?
        var failed = false
        var statusCode = 0
        let semaphore = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: request) { data, response, error in
            result = data
            failed = error != nil
            statusCode = (response as? HTTPURLResponse)?.statusCode ?? 0
            semaphore.signal()
        }.resume()
        semaphore.wait()
        return failed ? nil : (statusCode, result ?? Data())
    }

    /// 环境自检：定位"开关亮着却没反应"这类问题
    private static func runDoctor() {
        print("OpenToggle \(appVersion)")
        print("binary:        \(Bundle.main.executablePath ?? CommandLine.arguments[0])")

        if let (_, body) = tryAPI("GET", "/v1/ping"),
           let obj = try? JSONSerialization.jsonObject(with: body) as? [String: Any] {
            let axOK = obj["accessibility"] as? Bool ?? false
            print("app:           running (version \(obj["version"] as? String ?? "?"))")
            if let cpu = appCPUPercent() {
                print("app cpu:       \(cpu)%\(Double(cpu) ?? 0 > 20 ? "  ← unusually high, please report" : "")")
            }
            print("accessibility: \(axOK ? "granted (app process)" : "NOT GRANTED for the app process")")
            if !axOK {
                print("               → System Settings → Privacy & Security → Accessibility → enable OpenToggle")
                print("               → note: rebuilding the binary invalidates a previous grant; remove the")
                print("                 stale entry with “−” and add it again")
            }
        } else {
            print("app:           not running (GUI-dependent commands will fail)")
            print("accessibility: \(KeySpec.checkAccessibility(prompt: false) ? "granted (this CLI process)" : "NOT GRANTED for this CLI process")")
        }

        if let idle = KeySpec.systemIdleSeconds() {
            print("system idle:   \(String(format: "%.1f", idle))s")
        }

        // 孤儿 daemon 检测：脚本目录下的进程若父进程不是运行中的 app，
        // 说明上一代 app 未清理干净（会带着 caffeinate 常亮屏幕）
        reportDaemonProcesses()

        // 真发一次 F15，用空闲计时器验证是否落地
        print("press test:    ", terminator: "")
        guard let spec = KeySpec.parse("f15") else { print("internal error"); return }
        if let (status, body) = tryAPI("POST", "/v1/press", body: Data("f15".utf8)) {
            if status < 400 {
                print("OK — F15 delivered via the app process")
            } else {
                print("FAILED via app — \(String(data: body, encoding: .utf8) ?? "")")
            }
        } else {
            switch spec.post() {
            case .delivered: print("OK — F15 delivered locally")
            case .notTrusted: print("FAILED — no accessibility permission")
            case .notDelivered: print("FAILED — event rejected by the system (stale permission grant)")
            case .eventCreationFailed: print("FAILED — could not create the event")
            }
        }
    }

    /// 列出脚本目录相关进程，标记孤儿；并报告 caffeinate 防睡眠断言数量
    private static func reportDaemonProcesses() {
        let appPid: Int? = {
            let url = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".config/open-toggle/api.json")
            guard let data = try? Data(contentsOf: url),
                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else { return nil }
            return obj["pid"] as? Int
        }()
        let switchesPath = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/switches").path

        func shellLines(_ launchPath: String, _ args: [String]) -> [String] {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: launchPath)
            process.arguments = args
            let pipe = Pipe()
            process.standardOutput = pipe
            guard (try? process.run()) != nil else { return [] }
            // 必须先读到 EOF 再 waitUntilExit：反过来在输出超过管道缓冲(64KB)时
            // 会死锁（如 ps -eo 全进程列表）
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            let out = String(data: data, encoding: .utf8) ?? ""
            return out.components(separatedBy: "\n").filter { !$0.isEmpty }
        }

        let daemonLines = shellLines("/bin/ps", ["-eo", "pid=,ppid=,command="])
            .filter { $0.contains(switchesPath) }
        var running = 0
        var orphans: [String] = []
        for line in daemonLines {
            let cols = line.split(separator: " ", maxSplits: 2).map(String.init)
            guard cols.count >= 2, let ppid = Int(cols[1]) else { continue }
            running += 1
            if ppid != (appPid ?? -1) {
                orphans.append(cols[0])
            }
        }
        if running > 0 || !orphans.isEmpty {
            var lineOut = "daemons:       \(running) running"
            if !orphans.isEmpty {
                lineOut += "  ← ORPHANED pid(s) \(orphans.joined(separator: ", ")) not owned by the app; kill with: kill \(orphans.joined(separator: " "))"
            }
            print(lineOut)
        }

        let assertions = shellLines("/usr/bin/pmset", ["-g", "assertions"])
            .filter { $0.contains("caffeinate") }.count
        if assertions > 0 {
            print("sleep asserts: \(assertions) held by caffeinate (screen/system kept awake)")
        }
    }

    /// 读运行中 app 的瞬时 CPU 占用（pid 来自发现文件）
    private static func appCPUPercent() -> String? {
        let url = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/api.json")
        guard let data = try? Data(contentsOf: url),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let pid = obj["pid"] as? Int else { return nil }
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/ps")
        process.arguments = ["-o", "pcpu=", "-p", String(pid)]
        let pipe = Pipe()
        process.standardOutput = pipe
        guard (try? process.run()) != nil else { return nil }
        process.waitUntilExit()
        let out = String(data: pipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return (out?.isEmpty == false) ? out : nil
    }

    /// 同步请求；连接失败 → 提示 app 未运行并退出
    static func api(_ method: String, _ path: String, body: Data? = nil) -> Data {
        var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)\(path)")!)
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
            fail("OpenToggle app is not running — start it first (open the app or `swift run`)")
        }
        let data = result ?? Data()
        if statusCode >= 400 {
            FileHandle.standardError.write(data)
            FileHandle.standardError.write(Data("\n".utf8))
            exit(1)
        }
        return data
    }

    // MARK: - output helpers

    private static func reportIssues(_ issues: [LintIssue], jsonMode: Bool) {
        if jsonMode {
            if let data = try? JSONEncoder().encode(issues) { printData(data) }
            return
        }
        for issue in issues {
            print("\(issue.severity == "error" ? "error" : "warning"): \(issue.message)")
        }
        if issues.isEmpty { print("ok: no issues") }
    }

    private static func printData(_ data: Data, pretty: Bool = false, raw: Bool = false) {
        if raw || !pretty {
            FileHandle.standardOutput.write(data)
            if data.last != UInt8(ascii: "\n") { print() }
            return
        }
        FileHandle.standardOutput.write(data)
        if data.last != UInt8(ascii: "\n") { print() }
    }

    private static func fail(_ message: String) -> Never {
        FileHandle.standardError.write(Data((message + "\n").utf8))
        exit(1)
    }

    private static func exitWithUsage() -> Never {
        print(usage)
        exit(1)
    }

    private static let usage = """
    OpenToggle \(appVersion) — script-powered menu bar switches

    usage: opentoggle <command> [args] [--json]

      list                     list switches with state
      on|off <id>              turn a switch on/off
      state <id>               print on|off|error|unknown
      enable|disable <id>      show/hide a switch in the menu bar
      params <id>              list parameters and current values
      set <id> k=v [...]       set parameter values (restarts a running switch)
      cat <id>                 print the script source
      add <file.sh>            validate + install a new switch script
      put <id> <file.sh>       validate + replace an existing switch's script
      rm <id>                  delete a switch (moves script to Trash)
      validate <file.sh>       lint a script against the contract (offline)
      press <keyspec>          synthesize a key/mouse event (f15, cmd+shift+k,
                               mouse:middle); routed through the running app so
                               Accessibility is granted once, --local to bypass
      doctor                   diagnose app/permission/key-delivery problems
      mcp                      run as an MCP stdio server (for AI agents)
      version | help

    <id> is the script file name (keep-awake.sh; the .sh suffix is optional).
    All commands except validate/press/mcp/help require the OpenToggle app running.
    """
}

import Foundation
import AppKit

/// 契约静态检查：所有通过 CLI / API / MCP 提交的脚本先过这一关。
/// error = 拒收；warning = 放行但随响应返回。
struct LintIssue: Codable {
    let severity: String // "error" | "warning"
    let message: String
}

enum ContractLinter {
    static func lint(_ content: String) -> [LintIssue] {
        var issues: [LintIssue] = []
        func error(_ m: String) { issues.append(LintIssue(severity: "error", message: m)) }
        func warning(_ m: String) { issues.append(LintIssue(severity: "warning", message: m)) }

        let lines = content.components(separatedBy: "\n")

        // shebang
        if !(lines.first?.hasPrefix("#!") ?? false) {
            warning("Missing shebang on line 1; the script will be executed with /bin/sh as a fallback")
        }

        // 契约头（前 40 行）
        var name: String?
        var icon: String?
        var typeRaw: String?
        var paramSpecs: [[String: String]] = []
        var menubarSpec: [String: String]?
        var sawMenubarDirective = false

        for rawLine in lines.prefix(40) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard line.hasPrefix("#") else { continue }
            if let v = value(after: "switch.name", in: line) { name = v }
            if let v = value(after: "switch.icon", in: line) { icon = v }
            if let v = value(after: "switch.type", in: line) { typeRaw = v }
            if let v = value(after: "switch.param", in: line) {
                paramSpecs.append(SwitchScript.tokenizePairs(v))
            }
            if line.contains("<switch.menubar>") {
                sawMenubarDirective = true
                if let v = value(after: "switch.menubar", in: line) {
                    menubarSpec = SwitchScript.tokenizePairs(v)
                }
            }
        }

        // name / type
        if name == nil || name?.isEmpty == true {
            error("Missing required directive: # <switch.name> <display name>")
        }
        let type: SwitchType
        if let typeRaw {
            if let t = SwitchType(rawValue: typeRaw) {
                type = t
            } else {
                error("Invalid <switch.type> \"\(typeRaw)\"; expected \"toggle\" or \"daemon\"")
                type = .toggle
            }
        } else {
            type = .toggle
        }

        // icon
        if let icon { checkIcon(icon, context: "<switch.icon>", warning: warning) }

        // params
        var seenKeys = Set<String>()
        var numberParamKeys = Set<String>()
        for (index, spec) in paramSpecs.enumerated() {
            let where_ = "<switch.param> #\(index + 1)"
            guard let key = spec["key"], !key.isEmpty else {
                error("\(where_): missing required attribute key=")
                continue
            }
            if !seenKeys.insert(key).inserted {
                error("\(where_): duplicate param key \"\(key)\"")
            }
            if key.rangeOfCharacter(from: CharacterSet.alphanumerics.union(.init(charactersIn: "_-")).inverted) != nil {
                warning("\(where_): key \"\(key)\" contains unusual characters; prefer [a-z0-9_]")
            }
            let ptype = ParamType(rawValue: spec["type"] ?? "text")
            if spec["type"] != nil, ptype == nil {
                error("\(where_): invalid type \"\(spec["type"]!)\"; expected select | number | text | key")
            }
            switch ptype ?? .text {
            case .select:
                let options = SwitchScript.parseOptions(spec["options"])
                if options.isEmpty {
                    error("\(where_): type=select requires options=\"label=value|…\"")
                } else if let def = spec["default"], !def.isEmpty,
                          !options.contains(where: { $0.value == def }) {
                    warning("\(where_): default \"\(def)\" is not among the option values")
                }
            case .number:
                numberParamKeys.insert(key)
                let minV = spec["min"].flatMap(Int.init)
                let maxV = spec["max"].flatMap(Int.init)
                if spec["min"] != nil, minV == nil { error("\(where_): min= must be an integer") }
                if spec["max"] != nil, maxV == nil { error("\(where_): max= must be an integer") }
                if let minV, let maxV, minV > maxV { error("\(where_): min (\(minV)) > max (\(maxV))") }
                if let def = spec["default"], !def.isEmpty, Int(def) == nil {
                    warning("\(where_): default \"\(def)\" is not an integer")
                }
            case .key:
                if let def = spec["default"], !def.isEmpty, KeySpec.parse(def) == nil {
                    error("\(where_): default \"\(def)\" is not a valid key spec (expected e.g. f15, cmd+shift+k, mouse:middle)")
                }
            case .text:
                break
            }
        }

        // menubar
        if sawMenubarDirective {
            guard let menubarSpec, let mIcon = menubarSpec["icon"], !mIcon.isEmpty else {
                error("<switch.menubar>: missing required attribute icon=")
                return issues
            }
            checkIcon(mIcon, context: "<switch.menubar> icon", warning: warning)
            if let mode = menubarSpec["mode"], MenuBarMode(rawValue: mode) == nil {
                warning("<switch.menubar>: unknown mode \"\(mode)\"; expected add | replace (falling back to add)")
            }
            let countdownOn = ["on", "true", "yes", "1"].contains((menubarSpec["countdown"] ?? "").lowercased())
            if countdownOn, !numberParamKeys.contains("duration") {
                warning("<switch.menubar>: countdown=on requires a number param with key=duration (minutes); the countdown will not be shown")
            }
        }

        // 脚本体
        var bodyLines = lines
        if bodyLines.first?.hasPrefix("#!") == true { bodyLines.removeFirst() }
        bodyLines.removeAll {
            let t = $0.trimmingCharacters(in: .whitespaces)
            return t.hasPrefix("#") && t.contains("<switch.")
        }
        let body = bodyLines.joined(separator: "\n")
        let executable = bodyLines.contains {
            let t = $0.trimmingCharacters(in: .whitespaces)
            return !t.isEmpty && !t.hasPrefix("#")
        }
        if !executable {
            warning("Script body contains no executable statements")
        } else {
            switch type {
            case .toggle:
                if !body.contains("status") {
                    warning("toggle script body does not reference \"status\"; the app polls `<script> status` every 5 s and expects \"on\"/\"off\" on stdout")
                }
            case .daemon:
                // exec 或 trap TERM 二选一都能保证 SIGTERM 及时生效
                let handlesTerm = body.range(of: #"trap\b.*\b(SIG)?(TERM|INT)\b"#,
                                             options: .regularExpression) != nil
                if !body.contains("exec") && !handlesTerm {
                    warning("daemon script body neither uses `exec` nor traps TERM; SIGTERM may hit the wrapper shell instead of your process")
                }
            }
        }

        return issues
    }

    static func hasErrors(_ issues: [LintIssue]) -> Bool {
        issues.contains { $0.severity == "error" }
    }

    private static func value(after key: String, in line: String) -> String? {
        guard let range = line.range(of: "<\(key)>") else { return nil }
        let v = String(line[range.upperBound...]).trimmingCharacters(in: .whitespaces)
        return v.isEmpty ? nil : v
    }

    private static func checkIcon(_ icon: String, context: String, warning: (String) -> Void) {
        if icon.hasPrefix("sf:") {
            let symbol = String(icon.dropFirst(3))
            if NSImage(systemSymbolName: symbol, accessibilityDescription: nil) == nil {
                warning("\(context): unknown SF Symbol \"\(symbol)\"")
            }
        }
    }
}

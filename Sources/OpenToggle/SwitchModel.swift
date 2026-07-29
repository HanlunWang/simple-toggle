import Foundation

/// 开关的两类形态（见规划文档 §6）
/// - toggle: 命令式，app 调 `script on` / `script off` / `script status`
/// - daemon: 常驻进程，app 以 `script run` 启动并持有进程，关 = SIGTERM
enum SwitchType: String {
    case toggle
    case daemon
}

enum SwitchState {
    case on
    case off
    case error
    case unknown
}

/// 剩余秒数 → "24:31" / "1:02:05"
func formatCountdown(_ seconds: Int) -> String {
    if seconds >= 3600 {
        return String(format: "%d:%02d:%02d", seconds / 3600, (seconds % 3600) / 60, seconds % 60)
    }
    return String(format: "%d:%02d", seconds / 60, seconds % 60)
}

// MARK: - 参数

enum ParamType: String {
    case select // 下拉选择
    case number // 数字（步进器）
    case text   // 自由填写
}

struct ParamOption: Equatable, Hashable {
    let label: String
    let value: String
}

/// 契约：# <switch.param> key=mode type=select label=模式 default=d options="仅保持屏幕=d|屏幕与任务=dis"
/// 参数值在调用脚本时以环境变量注入（SWITCH_<KEY 大写>）
struct SwitchParam: Identifiable, Equatable {
    let key: String
    let label: String
    let type: ParamType
    let defaultValue: String
    let options: [ParamOption] // select 专用
    let presets: [ParamOption] // number/text 的快捷按钮（presets="1小时=60|2小时=120"）
    let minValue: Int?         // number 专用
    let maxValue: Int?         // number 专用
    let hint: String?

    var id: String { key }

    var envName: String {
        "SWITCH_" + String(key.uppercased().map { $0.isLetter || $0.isNumber ? $0 : "_" })
    }
}

// MARK: - 菜单栏行为

enum MenuBarMode: String {
    case add     // 开启时新增一个独立菜单栏图标
    case replace // 开启时替换主图标
}

/// 契约：# <switch.menubar> mode=add icon=☕️ countdown=on
/// icon 支持 emoji 或 "sf:<symbol名>"（SF Symbols）
struct MenuBarConfig: Equatable {
    let mode: MenuBarMode
    let icon: String
    let countdown: Bool // 需要一个名为 duration 的 number 参数（单位分钟，0=无限）
}

// MARK: - 开关

/// 一个"开关" = 一个可执行脚本 + 注释头元数据
struct SwitchScript: Identifiable, Equatable {
    let id: String // 文件名，作为持久化 key
    let name: String
    let icon: String
    let type: SwitchType
    let params: [SwitchParam]
    let menubar: MenuBarConfig?
    let url: URL

    /// 解析注释头契约（前 40 行内）：
    /// # <switch.name> Keep Awake
    /// # <switch.icon> ☕️
    /// # <switch.type> daemon
    /// # <switch.param> key=... type=... label=... default=... options=... min=... max=... hint=...
    /// # <switch.menubar> mode=add icon=☕️ countdown=on
    static func parse(url: URL) -> SwitchScript? {
        guard let content = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        var name: String?
        var icon = "🔘"
        var type = SwitchType.toggle
        var params: [SwitchParam] = []
        var menubar: MenuBarConfig?

        for rawLine in content.components(separatedBy: .newlines).prefix(40) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard line.hasPrefix("#") else { continue }
            if let v = value(after: "switch.name", in: line) { name = v }
            if let v = value(after: "switch.icon", in: line) { icon = v }
            if let v = value(after: "switch.type", in: line), let t = SwitchType(rawValue: v) { type = t }
            if let v = value(after: "switch.param", in: line), let p = parseParam(v) { params.append(p) }
            if let v = value(after: "switch.menubar", in: line) { menubar = parseMenuBar(v) }
        }
        guard let name, !name.isEmpty else { return nil }
        return SwitchScript(
            id: url.lastPathComponent, name: name, icon: icon, type: type,
            params: params, menubar: menubar, url: url
        )
    }

    private static func value(after key: String, in line: String) -> String? {
        guard let range = line.range(of: "<\(key)>") else { return nil }
        let v = String(line[range.upperBound...]).trimmingCharacters(in: .whitespaces)
        return v.isEmpty ? nil : v
    }

    private static func parseParam(_ spec: String) -> SwitchParam? {
        let pairs = tokenizePairs(spec)
        guard let key = pairs["key"], !key.isEmpty else { return nil }
        let type = ParamType(rawValue: pairs["type"] ?? "") ?? .text
        let options = parseOptions(pairs["options"])
        var defaultValue = pairs["default"] ?? ""
        if type == .select, defaultValue.isEmpty, let first = options.first {
            defaultValue = first.value
        }
        return SwitchParam(
            key: key,
            label: pairs["label"] ?? key,
            type: type,
            defaultValue: defaultValue,
            options: options,
            presets: parseOptions(pairs["presets"]),
            minValue: pairs["min"].flatMap(Int.init),
            maxValue: pairs["max"].flatMap(Int.init),
            hint: pairs["hint"]
        )
    }

    /// "标签=值|标签=值" → [ParamOption]（用于 options 和 presets）
    static func parseOptions(_ raw: String?) -> [ParamOption] {
        (raw ?? "")
            .split(separator: "|")
            .compactMap { part in
                let s = String(part)
                guard let eq = s.firstIndex(of: "=") else {
                    return ParamOption(label: s, value: s)
                }
                return ParamOption(
                    label: String(s[..<eq]).trimmingCharacters(in: .whitespaces),
                    value: String(s[s.index(after: eq)...]).trimmingCharacters(in: .whitespaces)
                )
            }
    }

    private static func parseMenuBar(_ spec: String) -> MenuBarConfig? {
        let pairs = tokenizePairs(spec)
        guard let icon = pairs["icon"], !icon.isEmpty else { return nil }
        let mode = MenuBarMode(rawValue: pairs["mode"] ?? "") ?? .add
        let countdown = ["on", "true", "yes", "1"].contains((pairs["countdown"] ?? "").lowercased())
        return MenuBarConfig(mode: mode, icon: icon, countdown: countdown)
    }

    /// 把 `key=value key2="value with spaces"` 解析成字典（双引号内的空格不分词）
    static func tokenizePairs(_ s: String) -> [String: String] {
        var tokens: [String] = []
        var current = ""
        var inQuotes = false
        for ch in s {
            if ch == "\"" { inQuotes.toggle(); continue }
            if ch == " " && !inQuotes {
                if !current.isEmpty { tokens.append(current); current = "" }
            } else {
                current.append(ch)
            }
        }
        if !current.isEmpty { tokens.append(current) }

        var pairs: [String: String] = [:]
        for token in tokens {
            guard let eq = token.firstIndex(of: "=") else { continue }
            pairs[String(token[..<eq])] = String(token[token.index(after: eq)...])
        }
        return pairs
    }
}

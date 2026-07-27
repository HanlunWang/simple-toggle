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

/// 一个"开关" = 一个可执行脚本 + 注释头元数据
struct SwitchScript: Identifiable, Equatable {
    let id: String // 文件名，作为持久化 key
    let name: String
    let icon: String
    let type: SwitchType
    let url: URL

    /// 解析注释头契约：
    /// # <switch.name> Keep Awake
    /// # <switch.icon> ☕️
    /// # <switch.type> daemon
    static func parse(url: URL) -> SwitchScript? {
        guard let content = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        var name: String?
        var icon = "🔘"
        var type = SwitchType.toggle
        for rawLine in content.components(separatedBy: .newlines).prefix(20) {
            let line = rawLine.trimmingCharacters(in: .whitespaces)
            guard line.hasPrefix("#") else { continue }
            if let v = value(of: "switch.name", in: line) { name = v }
            if let v = value(of: "switch.icon", in: line) { icon = v }
            if let v = value(of: "switch.type", in: line), let t = SwitchType(rawValue: v) { type = t }
        }
        guard let name, !name.isEmpty else { return nil }
        return SwitchScript(id: url.lastPathComponent, name: name, icon: icon, type: type, url: url)
    }

    private static func value(of key: String, in line: String) -> String? {
        guard let range = line.range(of: "<\(key)>") else { return nil }
        let v = String(line[range.upperBound...]).trimmingCharacters(in: .whitespaces)
        return v.isEmpty ? nil : v
    }
}

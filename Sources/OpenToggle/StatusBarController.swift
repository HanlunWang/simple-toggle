import AppKit

/// 管理脚本声明的额外菜单栏图标（<switch.menubar> mode=add）。
/// 图标 = emoji 或 "sf:<symbol名>"；可选倒计时文本；点击弹出"关闭"菜单。
///
/// 性能约定：sync 由倒计时 tick 每秒调用——item/菜单只在开关集合、图标或语言
/// 变化时重建，每秒路径上只做标题字符串比较 + 必要时的赋值。
@MainActor
final class StatusBarController: NSObject {
    static let shared = StatusBarController()

    private struct Entry {
        let item: NSStatusItem
        var configKey: String // 图标+名称+语言的指纹，变了才重建按钮与菜单
        var lastTitle: String
    }

    private var entries: [String: Entry] = [:]

    /// 由 SwitchManager 在状态变化及倒计时每秒刷新时调用
    func sync(with manager: SwitchManager) {
        let active = manager.switches.filter {
            $0.menubar?.mode == .add && manager.states[$0.id] == .on
        }

        // 移除已关闭的
        for (id, entry) in entries where !active.contains(where: { $0.id == id }) {
            NSStatusBar.system.removeStatusItem(entry.item)
            entries[id] = nil
        }

        for sw in active {
            guard let config = sw.menubar else { continue }
            let key = "\(config.icon)|\(sw.name)|\(Loc.shared.language.rawValue)"

            if entries[sw.id]?.configKey != key {
                // 新开关，或图标/名称/语言变化 → （重）建按钮与菜单
                let item = entries[sw.id]?.item
                    ?? NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
                configure(item, for: sw, config: config)
                entries[sw.id] = Entry(item: item, configKey: key, lastTitle: "")
            }

            // 每秒热路径：只有标题（倒计时）变化才碰 AppKit
            guard var entry = entries[sw.id] else { continue }
            var title = config.icon.hasPrefix("sf:") ? "" : config.icon
            if let remaining = manager.remainingSeconds(for: sw) {
                title += (title.isEmpty ? "" : " ") + formatCountdown(remaining)
            }
            if entry.lastTitle != title {
                entry.item.button?.title = title
                entry.lastTitle = title
                entries[sw.id] = entry
            }
        }
    }

    private func configure(_ item: NSStatusItem, for sw: SwitchScript, config: MenuBarConfig) {
        guard let button = item.button else { return }
        if config.icon.hasPrefix("sf:") {
            let symbol = String(config.icon.dropFirst(3))
            button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: sw.name)
        } else {
            button.image = nil
        }
        button.toolTip = "\(sw.name) — OpenToggle"

        let menu = NSMenu()
        menu.autoenablesItems = false
        let info = NSMenuItem(title: sw.name, action: nil, keyEquivalent: "")
        info.isEnabled = false
        menu.addItem(info)
        menu.addItem(.separator())
        let off = NSMenuItem(
            title: String(format: Loc.shared.s.turnOffFormat, sw.name),
            action: #selector(turnOff(_:)),
            keyEquivalent: ""
        )
        off.target = self
        off.representedObject = sw.id
        menu.addItem(off)
        item.menu = menu
    }

    @objc private func turnOff(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String,
              let sw = SwitchManager.shared.switches.first(where: { $0.id == id })
        else { return }
        SwitchManager.shared.setSwitch(sw, to: false)
    }
}

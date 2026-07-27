import AppKit

/// 管理脚本声明的额外菜单栏图标（<switch.menubar> mode=add）。
/// 图标 = emoji 或 "sf:<symbol名>"；可选倒计时文本；点击弹出"关闭"菜单。
@MainActor
final class StatusBarController: NSObject {
    static let shared = StatusBarController()

    private var items: [String: NSStatusItem] = [:]
    private var tickTimer: Timer?

    func sync(with manager: SwitchManager) {
        let active = manager.switches.filter {
            $0.menubar?.mode == .add && manager.states[$0.id] == .on
        }

        // 移除已关闭的
        for (id, item) in items where !active.contains(where: { $0.id == id }) {
            NSStatusBar.system.removeStatusItem(item)
            items[id] = nil
        }
        // 创建/更新开启中的
        for sw in active {
            let item = items[sw.id] ?? NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
            items[sw.id] = item
            configure(item, for: sw, manager: manager)
        }

        // 有倒计时在跑时每秒刷新一次标题
        let needsTick = active.contains { manager.remainingSeconds(for: $0) != nil }
        if needsTick, tickTimer == nil {
            tickTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { _ in
                Task { @MainActor in
                    StatusBarController.shared.sync(with: SwitchManager.shared)
                }
            }
        } else if !needsTick {
            tickTimer?.invalidate()
            tickTimer = nil
        }
    }

    private func configure(_ item: NSStatusItem, for sw: SwitchScript, manager: SwitchManager) {
        guard let button = item.button, let config = sw.menubar else { return }

        var title = ""
        if config.icon.hasPrefix("sf:") {
            let symbol = String(config.icon.dropFirst(3))
            button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: sw.name)
        } else {
            button.image = nil
            title = config.icon
        }
        if let remaining = manager.remainingSeconds(for: sw) {
            title += (title.isEmpty ? "" : " ") + Self.format(remaining)
        }
        button.title = title
        button.toolTip = "\(sw.name)（OpenToggle）"

        let menu = NSMenu()
        menu.autoenablesItems = false
        let info = NSMenuItem(title: sw.name, action: nil, keyEquivalent: "")
        info.isEnabled = false
        menu.addItem(info)
        menu.addItem(.separator())
        let off = NSMenuItem(title: "关闭「\(sw.name)」", action: #selector(turnOff(_:)), keyEquivalent: "")
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

    private static func format(_ seconds: Int) -> String {
        if seconds >= 3600 {
            return String(format: "%d:%02d:%02d", seconds / 3600, (seconds % 3600) / 60, seconds % 60)
        }
        return String(format: "%d:%02d", seconds / 60, seconds % 60)
    }
}

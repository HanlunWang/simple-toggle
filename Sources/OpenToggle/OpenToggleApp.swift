import SwiftUI
import AppKit

@main
struct OpenToggleApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var manager = SwitchManager.shared

    var body: some Scene {
        MenuBarExtra {
            MenuView(manager: manager)
        } label: {
            MenuBarLabel(manager: manager)
        }
        .menuBarExtraStyle(.window)

        Window("脚本管理 — OpenToggle", id: "manager") {
            ManagerView(manager: manager)
        }
        .defaultSize(width: 960, height: 600)
    }
}

/// 主图标：默认 switch.2；有 mode=replace 的开关开启时换成它声明的图标（可带倒计时）
private struct MenuBarLabel: View {
    @ObservedObject var manager: SwitchManager

    var body: some View {
        HStack(spacing: 3) {
            if let icon = manager.iconOverride {
                if icon.hasPrefix("sf:") {
                    Image(systemName: String(icon.dropFirst(3)))
                } else {
                    Text(icon)
                }
            } else {
                Image(systemName: "switch.2")
            }
            if let countdown = manager.iconOverrideCountdown {
                Text(countdown)
                    .monospacedDigit()
            }
        }
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        // 无 Dock 图标（等价于 app bundle 里的 LSUIElement=1）
        NSApp.setActivationPolicy(.accessory)
        SwitchManager.shared.start()
    }

    /// 脚本管理器有未保存修改时，退出前确认（覆盖面板"退出"按钮和 Cmd+Q）
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard EditorState.shared.isDirty else { return .terminateNow }
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.messageText = "脚本管理器里有未保存的修改"
        alert.informativeText = "现在退出将丢失这些修改。"
        alert.alertStyle = .warning
        alert.addButton(withTitle: "仍要退出")
        alert.addButton(withTitle: "取消")
        return alert.runModal() == .alertFirstButtonReturn ? .terminateNow : .terminateCancel
    }

    func applicationWillTerminate(_ notification: Notification) {
        // 退出时统一清理 daemon 子进程，避免孤儿进程
        SwitchManager.shared.shutdown()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false // 编辑器窗口关掉后 app 留在菜单栏
    }
}

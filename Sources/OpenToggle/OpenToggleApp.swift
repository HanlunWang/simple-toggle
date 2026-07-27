import SwiftUI
import AppKit

@main
struct OpenToggleApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var manager = SwitchManager.shared
    @ObservedObject private var loc = Loc.shared

    var body: some Scene {
        MenuBarExtra {
            MenuView(manager: manager)
        } label: {
            MenuBarLabel(manager: manager)
        }
        .menuBarExtraStyle(.window)

        Window(loc.s.managerWindowTitle, id: "manager") {
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
        let s = Loc.shared.s
        let alert = NSAlert()
        alert.messageText = s.quitAlertTitle
        alert.informativeText = s.quitAlertMessage
        alert.alertStyle = .warning
        alert.addButton(withTitle: s.quitAnyway)
        alert.addButton(withTitle: s.cancel)
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

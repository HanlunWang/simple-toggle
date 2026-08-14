import SwiftUI
import AppKit

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

/// 主图标：程序化绘制的迷你开关胶囊（MenuBarIcon，模板样式随系统着色）。
/// 有开关运行 → 实心旋钮在右；全关 → 空心旋钮在左。
/// 有 mode=replace 的开关开启时换成它声明的图标（可带倒计时）。
private struct MenuBarLabel: View {
    @ObservedObject var manager: SwitchManager

    private var anyOn: Bool {
        // 含隐藏开关：主图标回答的是"有没有东西在跑"，隐藏的也在跑
        manager.switches.contains { manager.states[$0.id] == .on }
    }

    var body: some View {
        HStack(spacing: 3) {
            if let icon = manager.iconOverride {
                if icon.hasPrefix("sf:") {
                    Image(systemName: String(icon.dropFirst(3)))
                } else {
                    Text(icon)
                }
            } else {
                Image(nsImage: anyOn ? MenuBarIcon.on : MenuBarIcon.off)
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
        // 黑白 liquid glass 是深色语言：全 app 强制深色外观。
        // 必须在 app 级设置——preferredColorScheme 只作用于视图所在 presentation，
        // popover/confirmationDialog/NSAlert 在浅色系统下会渲染成白底白字
        NSApp.appearance = NSAppearance(named: .darkAqua)
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

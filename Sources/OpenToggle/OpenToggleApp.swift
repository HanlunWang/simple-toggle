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

/// 主图标：默认 switch.2；有 mode=replace 的开关开启时换成它声明的图标
private struct MenuBarLabel: View {
    @ObservedObject var manager: SwitchManager

    var body: some View {
        if let icon = manager.iconOverride {
            if icon.hasPrefix("sf:") {
                Image(systemName: String(icon.dropFirst(3)))
            } else {
                Text(icon)
            }
        } else {
            Image(systemName: "switch.2")
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        // 无 Dock 图标（等价于 app bundle 里的 LSUIElement=1）
        NSApp.setActivationPolicy(.accessory)
        SwitchManager.shared.start()
    }

    func applicationWillTerminate(_ notification: Notification) {
        // 退出时统一清理 daemon 子进程，避免孤儿进程
        SwitchManager.shared.shutdown()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false // 编辑器窗口关掉后 app 留在菜单栏
    }
}

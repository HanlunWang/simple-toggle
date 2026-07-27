import SwiftUI
import AppKit

@main
struct OpenToggleApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var manager = SwitchManager.shared

    var body: some Scene {
        MenuBarExtra("OpenToggle", systemImage: "switch.2") {
            MenuView(manager: manager)
        }
        .menuBarExtraStyle(.window)
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
}

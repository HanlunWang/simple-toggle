import SwiftUI
import AppKit

/// 辅助功能权限状态：按键类开关依赖它，未授权时事件会被系统静默丢弃，
/// 所以必须在 UI 中显式暴露，而不是让用户面对一个"亮着却没反应"的开关。
@MainActor
final class AccessibilityStatus: ObservableObject {
    static let shared = AccessibilityStatus()

    /// 是否有开关用到按键参数（没有就完全不提这件事，保持界面干净）
    @Published private(set) var needed = false
    @Published private(set) var trusted = true

    private var timer: Timer?

    func start() {
        refresh()
    }

    func refresh() {
        // 等值守卫：避免无变化的 @Published 写入触发视图失效
        let newNeeded = SwitchManager.shared.switches.contains { sw in
            SwitchManager.shared.isEnabled(sw) && sw.params.contains { $0.type == .key }
        }
        if needed != newNeeded { needed = newNeeded }
        let newTrusted = KeySpec.checkAccessibility(prompt: false)
        if trusted != newTrusted { trusted = newTrusted }
        updateTimer()
    }

    /// 只在横幅显示期间轮询（等用户去系统设置授权，授权后横幅自动消失并停表）；
    /// 其余时间零定时器。
    private func updateTimer() {
        let shouldPoll = needed && !trusted
        if shouldPoll, timer == nil {
            let t = Timer.scheduledTimer(withTimeInterval: 3, repeats: true) { [weak self] _ in
                Task { @MainActor in self?.refresh() }
            }
            t.tolerance = 1
            timer = t
        } else if !shouldPoll {
            timer?.invalidate()
            timer = nil
        }
    }

    /// 实际发送被拒时调用：立即翻转状态，不必等下一次轮询
    func markDenied() {
        if trusted { trusted = false }
        updateTimer()
    }

    /// 触发系统授权弹窗，并打开设置面板
    func requestAccess() {
        _ = KeySpec.checkAccessibility(prompt: true)
        if let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") {
            NSWorkspace.shared.open(url)
        }
    }
}

/// 面板顶部的权限提示条（仅在有按键开关且未授权时出现）
struct AccessibilityBanner: View {
    @ObservedObject private var status = AccessibilityStatus.shared
    @ObservedObject private var loc = Loc.shared

    var body: some View {
        if status.needed && !status.trusted {
            VStack(alignment: .leading, spacing: 6) {
                Label(loc.s.axTitle, systemImage: "exclamationmark.triangle.fill")
                    .font(.callout.weight(.medium))
                    .foregroundStyle(.orange)
                Text(loc.s.axDetail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Button(loc.s.axGrant) {
                    status.requestAccess()
                }
                .controlSize(.small)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.orange.opacity(0.12))
            Divider()
        }
    }
}

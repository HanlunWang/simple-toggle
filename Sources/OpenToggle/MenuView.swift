import SwiftUI
import AppKit

struct MenuView: View {
    @ObservedObject var manager: SwitchManager

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("OpenToggle")
                .font(.headline)
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
            Divider()

            if manager.switches.isEmpty {
                Text("没有找到开关脚本\n把脚本放进 ~/.config/open-toggle/switches/")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(12)
            } else {
                ForEach(manager.switches) { sw in
                    SwitchRow(
                        script: sw,
                        state: manager.states[sw.id] ?? .unknown,
                        isOn: Binding(
                            get: { manager.states[sw.id] == .on },
                            set: { manager.setSwitch(sw, to: $0) }
                        )
                    )
                }
                .padding(.vertical, 4)
            }

            Divider()
            HStack {
                Button("打开脚本目录") {
                    NSWorkspace.shared.open(manager.scriptsDirectory)
                }
                Button("重新加载") {
                    manager.reload()
                }
                Spacer()
                Button("退出") {
                    NSApp.terminate(nil)
                }
            }
            .buttonStyle(.borderless)
            .font(.callout)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .frame(width: 300)
    }
}

private struct SwitchRow: View {
    let script: SwitchScript
    let state: SwitchState
    @Binding var isOn: Bool

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
            Text("\(script.icon) \(script.name)")
                .lineLimit(1)
            Spacer()
            Toggle("", isOn: $isOn)
                .toggleStyle(.switch)
                .controlSize(.small)
                .labelsHidden()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 5)
        .help(script.type == .daemon ? "daemon 型：app 持有常驻进程" : "命令式 toggle：on/off/status")
    }

    private var statusColor: Color {
        switch state {
        case .on: .green
        case .off: Color(nsColor: .tertiaryLabelColor)
        case .error: .red
        case .unknown: .orange
        }
    }
}

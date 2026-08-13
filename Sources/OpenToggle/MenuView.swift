import SwiftUI
import AppKit

struct MenuView: View {
    @ObservedObject var manager: SwitchManager
    @ObservedObject private var loc = Loc.shared
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("OpenToggle")
                .font(.headline)
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
            Divider()

            AccessibilityBanner()

            if manager.visibleSwitches.isEmpty {
                Text(loc.s.panelEmpty)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(12)
            } else {
                VStack(spacing: 0) {
                    ForEach(manager.visibleSwitches) { sw in
                        SwitchRow(manager: manager, script: sw)
                    }
                }
                .padding(.vertical, 4)
            }

            Divider()
            HStack(spacing: 12) {
                Button {
                    openWindow(id: "manager")
                    NSApp.activate(ignoringOtherApps: true)
                } label: {
                    Label(loc.s.manageScripts, systemImage: "slider.horizontal.3")
                }
                .help(loc.s.manageScriptsHelp)
                Button {
                    manager.reload()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .help(loc.s.reloadHelp)
                Spacer()
                Button(loc.s.quit) {
                    NSApp.terminate(nil)
                }
            }
            .buttonStyle(.borderless)
            .font(.callout)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .frame(width: 320)
        // 面板可见性驱动自适应轮询：打开 5s 一轮，收起降到 30s
        .onAppear { manager.panelDidAppear() }
        .onDisappear { manager.panelDidDisappear() }
    }
}

// MARK: - 开关行（可展开参数配置）

private struct SwitchRow: View {
    @ObservedObject var manager: SwitchManager
    let script: SwitchScript
    @ObservedObject private var loc = Loc.shared
    @State private var expanded = false

    private var state: SwitchState { manager.states[script.id] ?? .unknown }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Circle()
                    .fill(statusColor)
                    .frame(width: 8, height: 8)
                IconView(icon: script.icon)
                    .frame(width: 20)
                Text(script.name)
                    .lineLimit(1)
                if let remaining = manager.remainingSeconds(for: script) {
                    Text(formatCountdown(remaining))
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if !script.params.isEmpty {
                    Button {
                        withAnimation(.easeInOut(duration: 0.15)) { expanded.toggle() }
                    } label: {
                        Image(systemName: "chevron.down")
                            .font(.caption2.weight(.semibold))
                            .rotationEffect(.degrees(expanded ? 180 : 0))
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
                Toggle("", isOn: Binding(
                    get: { state == .on },
                    set: { manager.setSwitch(script, to: $0) }
                ))
                .toggleStyle(.switch)
                .controlSize(.small)
                .labelsHidden()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 5)
            .contentShape(Rectangle())
            .contextMenu {
                Button(loc.s.editScriptFile) { NSWorkspace.shared.open(script.url) }
                Button(loc.s.revealInFinder) {
                    NSWorkspace.shared.activateFileViewerSelecting([script.url])
                }
            }
            .help(script.type == .daemon ? loc.s.daemonRowHelp : loc.s.toggleRowHelp)

            if expanded {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(script.params) { param in
                        ParamControl(manager: manager, script: script, param: param)
                    }
                    if state == .on {
                        Text(loc.s.paramLiveNote)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .padding(.leading, 28)
                .padding(.trailing, 12)
                .padding(.bottom, 8)
            }
        }
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

// MARK: - 参数控件（select / number / text + 快捷按钮）

private struct ParamControl: View {
    @ObservedObject var manager: SwitchManager
    let script: SwitchScript
    let param: SwitchParam

    private var binding: Binding<String> {
        Binding(
            get: { manager.value(of: param, in: script) },
            set: { manager.setValue($0, of: param, in: script) }
        )
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Text(param.label)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                Spacer()
                switch param.type {
                case .select:
                    Picker("", selection: binding) {
                        ForEach(param.options, id: \.value) { option in
                            Text(option.label).tag(option.value)
                        }
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .fixedSize()
                case .number:
                    NumberField(binding: binding, min: param.minValue, max: param.maxValue)
                case .text:
                    TextField(param.hint ?? "", text: binding)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 140)
                case .key:
                    KeyPickerButton(value: binding)
                }
            }
            if !param.presets.isEmpty {
                PresetButtons(presets: param.presets, selection: binding)
            }
        }
        .help(param.hint ?? "")
    }
}

/// 常用值快捷按钮（与输入框并存；当前值命中的高亮）
struct PresetButtons: View {
    let presets: [ParamOption]
    let selection: Binding<String>

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 54), spacing: 4)],
                  alignment: .leading, spacing: 4) {
            ForEach(presets, id: \.value) { preset in
                if selection.wrappedValue == preset.value {
                    Button(preset.label) {}
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)
                } else {
                    Button(preset.label) { selection.wrappedValue = preset.value }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                }
            }
        }
    }
}

private struct NumberField: View {
    let binding: Binding<String>
    let min: Int?
    let max: Int?

    private var intBinding: Binding<Int> {
        Binding(
            get: { Int(binding.wrappedValue) ?? 0 },
            set: { newValue in
                var v = newValue
                if let min { v = Swift.max(min, v) }
                if let max { v = Swift.min(max, v) }
                binding.wrappedValue = String(v)
            }
        )
    }

    var body: some View {
        HStack(spacing: 4) {
            TextField("", value: intBinding, format: .number)
                .textFieldStyle(.roundedBorder)
                .multilineTextAlignment(.trailing)
                .frame(width: 56)
            // 上限缺省给一个有限值，避免步进到 Int.max 附近的溢出 trap
            Stepper("", value: intBinding, in: (min ?? 0)...(max ?? 1_000_000))
                .labelsHidden()
        }
    }
}

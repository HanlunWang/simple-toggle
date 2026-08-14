import SwiftUI
import AppKit

private struct ListHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}

struct MenuView: View {
    @ObservedObject var manager: SwitchManager
    @ObservedObject private var loc = Loc.shared
    @Environment(\.openWindow) private var openWindow
    @State private var listHeight: CGFloat = 0

    /// 列表封顶高度：屏幕可见高度减去头部/横幅/底栏与菜单栏的余量
    private var listMaxHeight: CGFloat {
        max(240, (NSScreen.main?.visibleFrame.height ?? 800) - 220)
    }

    private var onCount: Int {
        manager.visibleSwitches.filter { manager.states[$0.id] == .on }.count
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // 头部：❯ OpenToggle · N ON
            HStack(spacing: 8) {
                Text("❯")
                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                    .foregroundStyle(OT.txt2)
                Text("OpenToggle")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(OT.txt)
                Spacer()
                if onCount > 0 {
                    Text("\(onCount) ON")
                        .font(.system(size: 10.5, design: .monospaced))
                        .foregroundStyle(OT.txt2)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 11)
            OTDivider()

            AccessibilityBanner()

            if manager.visibleSwitches.isEmpty {
                emptyState
            } else {
                // 测高滚动：内容短则面板贴合内容高度；开关/展开参数多到超出屏幕时
                // 封顶并在内部滚动，头部与底栏始终可见
                ScrollView {
                    VStack(spacing: 0) {
                        ForEach(manager.visibleSwitches) { sw in
                            SwitchRow(manager: manager, script: sw)
                        }
                    }
                    .padding(.vertical, 5)
                    .background(GeometryReader { geo in
                        Color.clear.preference(key: ListHeightKey.self, value: geo.size.height)
                    })
                }
                // 首帧尚未测得高度时用自然尺寸，避免打开面板时的高度跳变
                .frame(height: listHeight > 0 ? min(listHeight, listMaxHeight) : nil)
                .onPreferenceChange(ListHeightKey.self) { listHeight = $0 }
                .scrollBounceBehavior(.basedOnSize)
            }

            OTDivider()
            HStack(spacing: 6) {
                GhostButton(icon: "slider.horizontal.3", label: loc.s.manageScripts) {
                    openWindow(id: "manager")
                    NSApp.activate(ignoringOtherApps: true)
                }
                .help(loc.s.manageScriptsHelp)
                GhostButton(icon: "arrow.clockwise", label: nil) {
                    manager.reload()
                }
                .help(loc.s.reloadHelp)
                Spacer()
                GhostButton(icon: nil, label: loc.s.quit) {
                    NSApp.terminate(nil)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .frame(width: 320)
        .otSurface()
        // 面板可见性驱动自适应轮询：打开 5s 一轮，收起降到 30s
        .onAppear { manager.panelDidAppear() }
        .onDisappear { manager.panelDidDisappear() }
    }

    private var emptyState: some View {
        VStack(spacing: 4) {
            Text(">_")
                .font(.system(size: 19, design: .monospaced))
                .foregroundStyle(OT.txt3)
                .padding(.bottom, 3)
            Text(loc.s.panelEmpty)
                .font(.system(size: 11))
                .foregroundStyle(OT.txt2)
                .multilineTextAlignment(.center)
            OTChip(label: loc.s.manageScripts, selected: true) {
                openWindow(id: "manager")
                NSApp.activate(ignoringOtherApps: true)
            }
            .padding(.top, 7)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 18)
    }
}

// MARK: - 底栏幽灵按钮

private struct GhostButton: View {
    let icon: String?
    let label: String?
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 5) {
                if let icon {
                    Image(systemName: icon).font(.system(size: 10.5))
                }
                if let label {
                    Text(label).font(.system(size: 11.5))
                }
            }
            .foregroundStyle(hovering ? OT.txt : OT.txt2)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(RoundedRectangle(cornerRadius: 8).fill(Color.white.opacity(hovering ? 0.04 : 0)))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(hovering ? OT.line : .clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

// MARK: - 开关行（可展开参数配置）

private struct SwitchRow: View {
    @ObservedObject var manager: SwitchManager
    let script: SwitchScript
    @ObservedObject private var loc = Loc.shared
    @State private var expanded = false

    private var state: SwitchState { manager.states[script.id] ?? .unknown }

    private func toggleExpanded() {
        guard !script.params.isEmpty else { return }
        withAnimation(.easeInOut(duration: 0.15)) { expanded.toggle() }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 9) {
                OTStatusMark(state: state)
                IconView(icon: script.icon)
                    .font(.system(size: 13))
                    .foregroundStyle(OT.txt)
                    .frame(width: 20)
                Text(script.name)
                    .font(.system(size: 12.5))
                    .foregroundStyle(OT.txt)
                    .lineLimit(1)
                if let remaining = manager.remainingSeconds(for: script) {
                    Text(formatCountdown(remaining))
                        .font(.system(size: 11, design: .monospaced))
                        .monospacedDigit()
                        .foregroundStyle(OT.txt2)
                }
                Spacer(minLength: 4)
                if !script.params.isEmpty {
                    Button(action: toggleExpanded) {
                        Image(systemName: "chevron.down")
                            .font(.system(size: 8.5, weight: .semibold))
                            .foregroundStyle(OT.txt3)
                            .rotationEffect(.degrees(expanded ? 180 : 0))
                            // 可点热区放大到 24×24（视觉尺寸不变）
                            .frame(width: 24, height: 24)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
                Toggle("", isOn: Binding(
                    get: { state == .on },
                    set: { manager.setSwitch(script, to: $0) }
                ))
                .toggleStyle(OTGlowToggleStyle())
                .labelsHidden()
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 6)
            .contentShape(Rectangle())
            // 整行皆可点击展开/收起（拨杆和箭头按钮优先级更高，不受影响）；
            // 箭头从"唯一入口"降级为指示器
            .onTapGesture(perform: toggleExpanded)
            .contextMenu {
                Button(loc.s.editScriptFile) { NSWorkspace.shared.open(script.url) }
                Button(loc.s.revealInFinder) {
                    NSWorkspace.shared.activateFileViewerSelecting([script.url])
                }
            }
            .help(script.type == .daemon ? loc.s.daemonRowHelp : loc.s.toggleRowHelp)

            if expanded {
                VStack(alignment: .leading, spacing: 9) {
                    ForEach(script.params) { param in
                        ParamControl(manager: manager, script: script, param: param)
                    }
                    if state == .on {
                        Text(loc.s.paramLiveNote)
                            .font(.system(size: 9.5))
                            .foregroundStyle(OT.txt3)
                    }
                }
                .padding(.leading, 37)
                .padding(.trailing, 14)
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(
                    Rectangle().fill(Color.black.opacity(0.22))
                        .overlay(VStack { OTDivider(); Spacer(); OTDivider() })
                )
            }
        }
    }
}

// MARK: - 参数控件（select 单选 chips / number / text / key + 快捷按钮）

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
        VStack(alignment: .leading, spacing: 5) {
            Text(param.label)
                .font(.system(size: 10))
                .foregroundStyle(OT.txt3)
            switch param.type {
            case .select:
                // 设计稿 A1：下拉改为单选 chips，选中反白 —— 状态一眼可见且不会溢出（自动换行）
                WrapLayout(spacing: 5) {
                    ForEach(param.options, id: \.value) { option in
                        OTChip(label: option.label, selected: binding.wrappedValue == option.value) {
                            binding.wrappedValue = option.value
                        }
                    }
                }
            case .number:
                WrapLayout(spacing: 5) {
                    ForEach(param.presets, id: \.value) { preset in
                        OTChip(label: preset.label, selected: binding.wrappedValue == preset.value) {
                            binding.wrappedValue = preset.value
                        }
                    }
                    NumberField(binding: binding, min: param.minValue, max: param.maxValue)
                }
            case .text:
                VStack(alignment: .leading, spacing: 5) {
                    if !param.presets.isEmpty {
                        WrapLayout(spacing: 5) {
                            ForEach(param.presets, id: \.value) { preset in
                                OTChip(label: preset.label, selected: binding.wrappedValue == preset.value) {
                                    binding.wrappedValue = preset.value
                                }
                            }
                        }
                    }
                    TextField(param.hint ?? "", text: binding)
                        .textFieldStyle(.plain)
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(OT.txt)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(RoundedRectangle(cornerRadius: 7).fill(Color.black.opacity(0.28)))
                        .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(OT.line, lineWidth: 1))
                        .frame(maxWidth: 170)
                }
            case .key:
                // 快捷按钮对 key 参数同样有效（如 F15/F16 预设一键切换）
                VStack(alignment: .leading, spacing: 5) {
                    if !param.presets.isEmpty {
                        WrapLayout(spacing: 5) {
                            ForEach(param.presets, id: \.value) { preset in
                                OTChip(label: preset.label, selected: binding.wrappedValue == preset.value) {
                                    binding.wrappedValue = preset.value
                                }
                            }
                        }
                    }
                    KeyPickerButton(value: binding)
                }
            }
        }
        .help(param.hint ?? "")
    }
}

/// number 参数的手动输入（快捷按钮之外的自定义值）
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
        HStack(spacing: 3) {
            TextField("", value: intBinding, format: .number)
                .textFieldStyle(.plain)
                .font(.system(size: 11, design: .monospaced))
                .monospacedDigit()
                .foregroundStyle(OT.txt)
                .multilineTextAlignment(.trailing)
                .frame(width: 44)
                .padding(.horizontal, 6)
                .padding(.vertical, 3)
                .background(RoundedRectangle(cornerRadius: 7).fill(Color.black.opacity(0.28)))
                .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(OT.line, lineWidth: 1))
            // 上限缺省给一个有限值，避免步进到 Int.max 附近的溢出 trap
            Stepper("", value: intBinding, in: (min ?? 0)...(max ?? 1_000_000))
                .labelsHidden()
                .controlSize(.mini)
        }
    }
}

import SwiftUI
import AppKit

/// 按键选择按钮：显示当前键位（⌘⇧K / F15 / 鼠标中键），点开选择器。
/// 三种录入方式：按键捕获（按下即录）、常用键网格、鼠标键。
struct KeyPickerButton: View {
    @Binding var value: String // 规范 keyspec 字符串
    @ObservedObject private var loc = Loc.shared
    @State private var showingPicker = false

    var body: some View {
        Button {
            showingPicker.toggle()
        } label: {
            HStack(spacing: 4) {
                Text(displayLabel)
                    .font(.system(.callout, design: .rounded).weight(.medium))
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
        }
        .buttonStyle(.bordered)
        .popover(isPresented: $showingPicker, arrowEdge: .bottom) {
            KeyPickerPopover(value: $value) { showingPicker = false }
        }
    }

    private var displayLabel: String {
        guard let spec = KeySpec.parse(value) else { return loc.s.chooseKey }
        if let text = spec.displayText { return text }
        switch spec.keyName {
        case "mouse:left": return loc.s.mouseLeft
        case "mouse:middle": return loc.s.mouseMiddle
        case "mouse:right": return loc.s.mouseRight
        default: return spec.canonical
        }
    }
}

private struct KeyPickerPopover: View {
    @Binding var value: String
    let dismiss: () -> Void

    @ObservedObject private var loc = Loc.shared
    @State private var tab = 0

    private static let commonKeys = [
        "f13", "f14", "f15", "f16", "f17", "f18", "f19",
        "esc", "tab", "space", "return", "delete",
        "up", "down", "left", "right",
        "pageup", "pagedown", "home", "end",
    ]

    var body: some View {
        VStack(spacing: 10) {
            Picker("", selection: $tab) {
                Text(loc.s.tabCapture).tag(0)
                Text(loc.s.tabCommonKeys).tag(1)
                Text(loc.s.tabMouse).tag(2)
            }
            .pickerStyle(.segmented)
            .labelsHidden()

            switch tab {
            case 0:
                CaptureArea(value: $value, onCaptured: dismiss)
            case 1:
                ScrollView {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 64), spacing: 4)], spacing: 4) {
                        ForEach(Self.commonKeys, id: \.self) { key in
                            keyCell(key)
                        }
                    }
                }
            default:
                VStack(spacing: 6) {
                    mouseCell("mouse:left", loc.s.mouseLeft)
                    mouseCell("mouse:middle", loc.s.mouseMiddle)
                    mouseCell("mouse:right", loc.s.mouseRight)
                    Text(loc.s.mouseNote)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Spacer()
                }
            }

            if KeySpec.parse(value)?.isBareCharacterKey == true {
                Label(loc.s.bareKeyWarning, systemImage: "exclamationmark.triangle")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(12)
        .frame(width: 340, height: 300)
    }

    private func keyCell(_ key: String) -> some View {
        let selected = KeySpec.parse(value)?.canonical == key
        return Button {
            value = key
            dismiss()
        } label: {
            Text(KeySpec.parse(key)?.displayText ?? key)
                .font(.system(.callout, design: .rounded))
                .frame(maxWidth: .infinity, minHeight: 26)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(selected ? Color.accentColor.opacity(0.25) : Color(nsColor: .quaternaryLabelColor).opacity(0.4))
                )
        }
        .buttonStyle(.plain)
    }

    private func mouseCell(_ spec: String, _ label: String) -> some View {
        let selected = value == spec
        return Button {
            value = spec
            dismiss()
        } label: {
            HStack {
                Image(systemName: "computermouse")
                Text(label)
                Spacer()
                if selected { Image(systemName: "checkmark") }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 6)
                    .fill(selected ? Color.accentColor.opacity(0.25) : Color(nsColor: .quaternaryLabelColor).opacity(0.4))
            )
        }
        .buttonStyle(.plain)
    }
}

/// 按键捕获区：点击进入监听，按下任意键/组合键直接录入
private struct CaptureArea: View {
    @Binding var value: String
    let onCaptured: () -> Void

    @ObservedObject private var loc = Loc.shared
    @State private var capturing = false
    @State private var monitor: Any?

    var body: some View {
        Button {
            capturing ? stop() : start()
        } label: {
            VStack(spacing: 6) {
                Image(systemName: capturing ? "keyboard.fill" : "keyboard")
                    .font(.title2)
                    .foregroundStyle(capturing ? Color.accentColor : Color.secondary)
                Text(capturing ? loc.s.captureActive : loc.s.captureIdle)
                    .font(.callout)
                    .foregroundStyle(capturing ? Color.primary : Color.secondary)
                    .multilineTextAlignment(.center)
                if let spec = KeySpec.parse(value), let text = spec.displayText {
                    Text(text)
                        .font(.title3.weight(.semibold))
                }
            }
            .frame(maxWidth: .infinity, minHeight: 130)
            .background(
                RoundedRectangle(cornerRadius: 8)
                    .fill(capturing ? Color.accentColor.opacity(0.12)
                                    : Color(nsColor: .quaternaryLabelColor).opacity(0.3))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(capturing ? Color.accentColor : Color.clear,
                                  style: StrokeStyle(lineWidth: 1.5, dash: [5, 3]))
            )
        }
        .buttonStyle(.plain)
        .onDisappear { stop() }
    }

    private func start() {
        capturing = true
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            if let spec = KeySpec.from(event: event) {
                value = spec.canonical
                stop()
                onCaptured()
            }
            return nil // 吞掉事件，避免触发窗口快捷键
        }
    }

    private func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        capturing = false
    }
}

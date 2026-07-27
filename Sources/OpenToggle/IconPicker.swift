import SwiftUI

/// 统一渲染图标值："sf:<symbol>" → SF Symbol，否则按 emoji/文本显示
struct IconView: View {
    let icon: String

    var body: some View {
        if icon.hasPrefix("sf:") {
            Image(systemName: String(icon.dropFirst(3)))
        } else {
            Text(icon)
        }
    }
}

/// 图标选择按钮：点开一个网格选择器（emoji / SF Symbols / 自定义），不用手打 sf:xxx
struct IconPickerButton: View {
    @Binding var icon: String
    @State private var showingPicker = false

    var body: some View {
        Button {
            showingPicker.toggle()
        } label: {
            HStack(spacing: 4) {
                IconView(icon: icon)
                    .font(.title3)
                    .frame(width: 24)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 3)
        }
        .buttonStyle(.bordered)
        .popover(isPresented: $showingPicker, arrowEdge: .bottom) {
            IconPickerPopover(icon: $icon) { showingPicker = false }
        }
    }
}

private struct IconPickerPopover: View {
    @Binding var icon: String
    let dismiss: () -> Void

    @State private var tab = 0
    @State private var custom = ""

    private let columns = Array(repeating: GridItem(.fixed(34), spacing: 4), count: 8)

    var body: some View {
        VStack(spacing: 10) {
            Picker("", selection: $tab) {
                Text("Emoji").tag(0)
                Text("图标").tag(1)
                Text("自定义").tag(2)
            }
            .pickerStyle(.segmented)
            .labelsHidden()

            switch tab {
            case 0:
                grid(IconCatalog.emoji) { emoji in
                    cell { Text(emoji).font(.title2) } value: { emoji }
                }
            case 1:
                grid(IconCatalog.sfSymbols) { symbol in
                    cell { Image(systemName: symbol).font(.title3) } value: { "sf:" + symbol }
                }
            default:
                VStack(alignment: .leading, spacing: 8) {
                    TextField("emoji 或 SF Symbol 名（如 cup.and.saucer）", text: $custom)
                        .textFieldStyle(.roundedBorder)
                        .onSubmit { applyCustom() }
                    HStack {
                        Text("预览：")
                            .foregroundStyle(.secondary)
                        IconView(icon: normalizedCustom).font(.title2)
                        Spacer()
                        Button("使用") { applyCustom() }
                            .disabled(custom.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                    Text("SF Symbol 名可在系统「SF Symbols」app 里查")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                    Spacer()
                }
                .padding(.top, 4)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(12)
        .frame(width: 330, height: 320)
    }

    private var normalizedCustom: String {
        let trimmed = custom.trimmingCharacters(in: .whitespaces)
        // 输入的是纯符号名（含点、无 emoji）就自动按 SF Symbol 处理
        if !trimmed.isEmpty, trimmed.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "." }), trimmed.contains(".") {
            return "sf:" + trimmed
        }
        return trimmed
    }

    private func applyCustom() {
        guard !normalizedCustom.isEmpty else { return }
        icon = normalizedCustom
        dismiss()
    }

    private func grid<Item: Hashable, Content: View>(
        _ items: [Item],
        @ViewBuilder content: @escaping (Item) -> Content
    ) -> some View {
        ScrollView {
            LazyVGrid(columns: columns, spacing: 4) {
                ForEach(items, id: \.self) { item in
                    content(item)
                }
            }
        }
    }

    private func cell<Content: View>(
        @ViewBuilder _ label: () -> Content,
        value: () -> String
    ) -> some View {
        let v = value()
        return Button {
            icon = v
            dismiss()
        } label: {
            label()
                .frame(width: 32, height: 32)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(icon == v ? Color.accentColor.opacity(0.25) : Color.clear)
                )
        }
        .buttonStyle(.plain)
        .help(v)
    }
}

/// 精选图标库（demo 级：常用自动化场景）
enum IconCatalog {
    static let emoji: [String] = [
        "☕️", "🍵", "🌙", "💤", "🖥️", "💻", "⌨️", "🖱️",
        "🔒", "🔓", "🌐", "📶", "🚀", "🔥", "💡", "👁️",
        "🎧", "🎵", "🔔", "🔕", "📷", "🛡️", "🧹", "⏰",
        "⏳", "⚡️", "🔋", "🔌", "🌗", "🎯", "🧪", "📦",
        "🔧", "🔨", "🐳", "🐍", "☁️", "✈️", "❄️", "🌈",
        "🎮", "📺", "📡", "🗂️", "📝", "🧊", "🎨", "🔍",
    ]

    static let sfSymbols: [String] = [
        "cup.and.saucer.fill", "moon.zzz.fill", "display", "keyboard",
        "computermouse.fill", "lock.fill", "lock.open.fill", "network",
        "wifi", "antenna.radiowaves.left.and.right", "bolt.fill", "battery.100percent",
        "lightbulb.fill", "eye.fill", "eye.slash.fill", "bell.fill",
        "bell.slash.fill", "speaker.wave.2.fill", "headphones", "music.note",
        "camera.fill", "shield.fill", "timer", "clock.fill",
        "alarm.fill", "calendar", "folder.fill", "terminal.fill",
        "gearshape.fill", "wrench.and.screwdriver.fill", "hammer.fill", "paintbrush.fill",
        "airplane", "globe", "cloud.fill", "snowflake",
        "flame.fill", "drop.fill", "leaf.fill", "star.fill",
        "flag.fill", "pin.fill", "location.fill", "arrow.triangle.2.circlepath",
        "play.fill", "pause.fill", "power", "sparkles",
    ]
}

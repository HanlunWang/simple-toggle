import SwiftUI

// 方案 A · Refined Glass —— 黑白 liquid glass 设计令牌与基础组件
// 源自 app 图标语言：发丝描线承担结构，实心发光白是唯一"强调色"（只给 ON 态与焦点）。
// 设计稿：designs/ui-redesign/（A3–A7）

enum OT {
    static let bg = Color(red: 0.039, green: 0.043, blue: 0.051)           // #0A0B0D
    static let glassHi = Color(red: 0.086, green: 0.094, blue: 0.110)      // #16181C
    static let glassLo = Color(red: 0.055, green: 0.063, blue: 0.075)      // #0E1013
    static let line = Color.white.opacity(0.13)
    static let lineStrong = Color.white.opacity(0.34)
    static let txt = Color.white.opacity(0.92)
    static let txt2 = Color.white.opacity(0.55)
    static let txt3 = Color.white.opacity(0.30)
}

// MARK: - 玻璃基底

extension View {
    /// 面板/窗口内容的玻璃底：烟灰渐层 + 左上冷光斜射
    func otSurface() -> some View {
        background(
            ZStack {
                LinearGradient(colors: [OT.glassHi, OT.glassLo],
                               startPoint: .topLeading, endPoint: .bottomTrailing)
                RadialGradient(colors: [.white.opacity(0.07), .clear],
                               center: UnitPoint(x: 0.2, y: -0.1),
                               startRadius: 0, endRadius: 520)
            }
            .ignoresSafeArea()
        )
        .preferredColorScheme(.dark)
    }
}

// MARK: - 发光拨杆（描线胶囊 + 实心发光旋钮）

struct OTGlowToggleStyle: ToggleStyle {
    var small = false

    func makeBody(configuration: Configuration) -> some View {
        let w: CGFloat = small ? 30 : 38
        let h: CGFloat = small ? 17 : 21
        let knob: CGFloat = small ? 11 : 15
        return Button {
            configuration.isOn.toggle()
        } label: {
            ZStack(alignment: configuration.isOn ? .trailing : .leading) {
                Capsule()
                    .fill(Color.white.opacity(configuration.isOn ? 0.05 : 0.02))
                    .overlay(Capsule().strokeBorder(
                        configuration.isOn ? Color.white.opacity(0.65) : OT.lineStrong,
                        lineWidth: 1))
                    .shadow(color: .white.opacity(configuration.isOn ? 0.15 : 0), radius: 5)
                Group {
                    if configuration.isOn {
                        Circle().fill(Color.white)
                            .shadow(color: .white.opacity(0.85), radius: 4)
                            .shadow(color: .white.opacity(0.40), radius: 12)
                    } else {
                        Circle().strokeBorder(OT.lineStrong, lineWidth: 1)
                    }
                }
                .frame(width: knob, height: knob)
                .padding(.horizontal, 2.5)
            }
            .frame(width: w, height: h)
            .contentShape(Rectangle())
            .animation(.easeOut(duration: 0.16), value: configuration.isOn)
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 状态记号（全黑白语义：见设计稿 A6）

struct OTStatusMark: View {
    let state: SwitchState

    var body: some View {
        Group {
            switch state {
            case .on:
                Circle().fill(Color.white)
                    .frame(width: 6, height: 6)
                    .shadow(color: .white.opacity(0.9), radius: 4)
            case .off:
                Circle().strokeBorder(OT.txt3, lineWidth: 1)
                    .frame(width: 6, height: 6)
            case .error:
                Text("✕").font(.system(size: 9, weight: .bold))
                    .foregroundStyle(.white)
                    .shadow(color: .white.opacity(0.6), radius: 4)
            case .unknown:
                Text("?").font(.system(size: 9, weight: .semibold, design: .monospaced))
                    .foregroundStyle(OT.txt3)
            }
        }
        .frame(width: 14, alignment: .center)
    }
}

// MARK: - Chip（快捷按钮 / 单选段 / 幽灵按钮共用形态）

struct OTChip: View {
    let label: String
    var selected = false
    var dashed = false
    var mono = false
    var action: () -> Void = {}

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: 11, weight: selected ? .semibold : .regular,
                              design: mono ? .monospaced : .default))
                .foregroundStyle(selected ? Color.black.opacity(0.88) : OT.txt2)
                .lineLimit(1)
                .padding(.horizontal, 9)
                .padding(.vertical, 3.5)
                .background(
                    RoundedRectangle(cornerRadius: 7)
                        .fill(selected ? Color.white.opacity(0.92) : Color.white.opacity(0.02))
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 7)
                        .strokeBorder(selected ? Color.clear : OT.line,
                                      style: StrokeStyle(lineWidth: 1, dash: dashed ? [4, 3] : []))
                )
                .shadow(color: .white.opacity(selected ? 0.25 : 0), radius: 6)
        }
        .buttonStyle(.plain)
    }
}

/// 等宽白色胶囊分段选择（类型 / 菜单栏模式）
struct OTSegmented: View {
    let options: [String]
    @Binding var selection: Int

    var body: some View {
        HStack(spacing: 0) {
            ForEach(options.indices, id: \.self) { i in
                Button {
                    selection = i
                } label: {
                    Text(options[i])
                        .font(.system(size: 11, weight: selection == i ? .semibold : .regular))
                        .foregroundStyle(selection == i ? Color.black.opacity(0.88) : OT.txt2)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 5)
                        .background(selection == i ? Color.white.opacity(0.92) : Color.clear)
                }
                .buttonStyle(.plain)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(OT.line, lineWidth: 1))
        .animation(.easeOut(duration: 0.15), value: selection)
    }
}

// MARK: - 流式换行布局（chips 防溢出的关键：窗口再窄也换行、绝不溢出）

struct WrapLayout: Layout {
    var spacing: CGFloat = 5

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxWidth = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x > 0, x + size.width > maxWidth {
                x = 0; y += rowHeight + spacing; rowHeight = 0
            }
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return CGSize(width: maxWidth.isFinite ? maxWidth : max(0, x - spacing),
                      height: y + rowHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x > bounds.minX, x + size.width > bounds.maxX {
                x = bounds.minX; y += rowHeight + spacing; rowHeight = 0
            }
            subview.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
    }
}

// MARK: - 分区卡片（管理器表单的 hairline 分组）

struct OTSection<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text(title)
                .font(.system(size: 11.5, weight: .bold))
                .foregroundStyle(OT.txt)
            content
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 13).fill(Color.white.opacity(0.02)))
        .overlay(RoundedRectangle(cornerRadius: 13).strokeBorder(OT.line, lineWidth: 1))
    }
}

// MARK: - 发丝分隔线

struct OTDivider: View {
    var body: some View {
        LinearGradient(colors: [.clear, OT.line, OT.line, .clear],
                       startPoint: .leading, endPoint: .trailing)
            .frame(height: 1)
    }
}

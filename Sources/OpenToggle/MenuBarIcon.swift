import AppKit

/// 菜单栏主图标：程序化绘制的迷你开关胶囊（与 app 图标同构）。
/// isTemplate = true —— 系统按菜单栏明暗自动着色，任意 Retina 倍率下矢量锐利。
/// 状态语义：有开关运行 → 实心旋钮在右；全部关闭 → 空心旋钮在左。
enum MenuBarIcon {
    static let on = make(anyOn: true)
    static let off = make(anyOn: false)

    private static func make(anyOn: Bool) -> NSImage {
        let size = NSSize(width: 25, height: 16)
        let image = NSImage(size: size, flipped: false) { _ in
            let stroke: CGFloat = 1.6
            NSColor.black.setStroke()
            NSColor.black.setFill()

            // 胶囊壳（描边）
            let pillRect = NSRect(x: stroke / 2, y: 1.6, width: size.width - stroke, height: 12.8)
            let pill = NSBezierPath(roundedRect: pillRect,
                                    xRadius: pillRect.height / 2,
                                    yRadius: pillRect.height / 2)
            pill.lineWidth = stroke
            pill.stroke()

            // 旋钮
            let knobRadius: CGFloat = 3.6
            let knobCenterX = anyOn
                ? pillRect.maxX - knobRadius - 2.6
                : pillRect.minX + knobRadius + 2.6
            let knobRect = NSRect(x: knobCenterX - knobRadius,
                                  y: pillRect.midY - knobRadius,
                                  width: knobRadius * 2,
                                  height: knobRadius * 2)
            if anyOn {
                NSBezierPath(ovalIn: knobRect).fill()
            } else {
                let knob = NSBezierPath(ovalIn: knobRect.insetBy(dx: 0.6, dy: 0.6))
                knob.lineWidth = 1.2
                knob.stroke()
            }
            return true
        }
        image.isTemplate = true
        return image
    }
}

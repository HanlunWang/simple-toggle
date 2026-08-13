import Foundation
import CoreGraphics
import AppKit
import ApplicationServices
import IOKit

/// 规范化按键描述（key 型参数的值格式）：
///   "[modifier+]*key"  →  "f15"、"cmd+shift+k"、"ctrl+opt+space"
///   "mouse:left|middle|right"（鼠标键，当前光标位置点击）
/// 修饰键：cmd / shift / opt / ctrl / fn（别名 command、option、alt、control）
struct KeySpec: Equatable, Sendable {
    enum Target: Equatable, Sendable {
        case key(CGKeyCode)
        case mouse(CGMouseButton)
    }

    let modifierNames: [String] // 规范顺序 cmd, shift, opt, ctrl, fn
    let keyName: String         // 规范键名（"f15" / "k" / "mouse:left"）
    let target: Target

    static let modifierOrder = ["cmd", "shift", "opt", "ctrl", "fn"]

    static let modifierFlags: [String: CGEventFlags] = [
        "cmd": .maskCommand, "shift": .maskShift, "opt": .maskAlternate,
        "ctrl": .maskControl, "fn": .maskSecondaryFn,
    ]

    private static let modifierAliases = [
        "command": "cmd", "meta": "cmd", "option": "opt", "alt": "opt", "control": "ctrl",
    ]

    static let mouseButtons: [String: CGMouseButton] = [
        "mouse:left": .left, "mouse:right": .right, "mouse:middle": .center,
    ]

    /// 键名 → macOS virtual key code（ANSI 布局，Carbon kVK_* 常量值）
    static let keyCodes: [String: CGKeyCode] = [
        "a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
        "b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17,
        "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "9": 25, "7": 26, "8": 28, "0": 29,
        "o": 31, "u": 32, "i": 34, "p": 35, "l": 37, "j": 38, "k": 40, "n": 45, "m": 46,
        "return": 36, "tab": 48, "space": 49, "delete": 51, "esc": 53,
        "f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100,
        "f9": 101, "f10": 109, "f11": 103, "f12": 111, "f13": 105, "f14": 107, "f15": 113,
        "f16": 106, "f17": 64, "f18": 79, "f19": 80, "f20": 90,
        "home": 115, "end": 119, "pageup": 116, "pagedown": 121, "forwarddelete": 117,
        "left": 123, "right": 124, "down": 125, "up": 126,
    ]

    // MARK: - 解析 / 格式化

    static func parse(_ raw: String) -> KeySpec? {
        let s = raw.trimmingCharacters(in: .whitespaces).lowercased()
        guard !s.isEmpty else { return nil }
        if let button = mouseButtons[s] {
            return KeySpec(modifierNames: [], keyName: s, target: .mouse(button))
        }
        var parts = s.split(separator: "+").map(String.init)
        guard let last = parts.popLast() else { return nil }
        var mods = Set<String>()
        for part in parts {
            let name = modifierAliases[part] ?? part
            guard modifierFlags[name] != nil else { return nil }
            mods.insert(name)
        }
        guard let code = keyCodes[last] else { return nil }
        return KeySpec(
            modifierNames: modifierOrder.filter(mods.contains),
            keyName: last,
            target: .key(code)
        )
    }

    var canonical: String {
        (modifierNames + [keyName]).joined(separator: "+")
    }

    var flags: CGEventFlags {
        modifierNames.reduce(CGEventFlags()) { $0.union(Self.modifierFlags[$1] ?? []) }
    }

    /// mac 风格显示（⌘⇧K、F15）。鼠标键返回 nil，由 UI 层用本地化文案渲染。
    var displayText: String? {
        if case .mouse = target { return nil }
        let symbols = ["cmd": "⌘", "shift": "⇧", "opt": "⌥", "ctrl": "⌃", "fn": "fn "]
        let named = [
            "space": "Space", "tab": "⇥", "return": "↩", "esc": "⎋",
            "delete": "⌫", "forwarddelete": "⌦",
            "up": "↑", "down": "↓", "left": "←", "right": "→",
            "home": "↖", "end": "↘", "pageup": "⇞", "pagedown": "⇟",
        ]
        let key = named[keyName] ?? keyName.uppercased()
        return modifierNames.map { symbols[$0] ?? $0 }.joined() + key
    }

    /// 无修饰键的单字符键：会真实输入字符，UI 应提示可能干扰正常输入
    var isBareCharacterKey: Bool {
        guard case .key = target else { return false }
        return modifierNames.isEmpty && keyName.count == 1
    }

    // MARK: - 捕获（NSEvent → KeySpec）

    static func from(event: NSEvent) -> KeySpec? {
        guard let name = keyCodes.first(where: { $0.value == CGKeyCode(event.keyCode) })?.key,
              let code = keyCodes[name] else { return nil }
        var mods: Set<String> = []
        let f = event.modifierFlags
        if f.contains(.command) { mods.insert("cmd") }
        if f.contains(.shift) { mods.insert("shift") }
        if f.contains(.option) { mods.insert("opt") }
        if f.contains(.control) { mods.insert("ctrl") }
        // .function 不采集：F 键/方向键自动携带该标志，并不代表用户按住了 fn
        return KeySpec(
            modifierNames: modifierOrder.filter(mods.contains),
            keyName: name,
            target: .key(code)
        )
    }

    // MARK: - 事件合成（需要辅助功能权限）

    static func checkAccessibility(prompt: Bool) -> Bool {
        let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: prompt] as CFDictionary
        return AXIsProcessTrustedWithOptions(options)
    }

    /// 系统空闲秒数（距上次 HID 输入）。这是 Teams 等应用判定"离开"的依据，
    /// 也是我们验证事件是否真正送达系统的唯一可靠标尺。
    static func systemIdleSeconds() -> Double? {
        let service = IOServiceGetMatchingService(kIOMainPortDefault, IOServiceMatching("IOHIDSystem"))
        guard service != 0 else { return nil }
        defer { IOObjectRelease(service) }
        guard let property = IORegistryEntryCreateCFProperty(
            service, "HIDIdleTime" as CFString, kCFAllocatorDefault, 0
        )?.takeRetainedValue() as? NSNumber else { return nil }
        return property.doubleValue / 1_000_000_000
    }

    enum PostResult: Equatable {
        case delivered
        case notTrusted        // 未获辅助功能授权
        case notDelivered      // 已授权但事件未落地（签名失效/系统拒绝）
        case eventCreationFailed
    }

    /// 发送事件并**验证是否真正送达**。
    /// CGEvent.post() 无返回值，被系统丢弃时也毫无声响——只看它会造成"开关亮着却什么都没做"。
    /// 因此这里以 HID 空闲计时器归零作为送达证据。
    @discardableResult
    func post() -> PostResult {
        guard Self.checkAccessibility(prompt: false) else { return .notTrusted }

        let idleBefore = Self.systemIdleSeconds()

        switch target {
        case .key(let code):
            let source = CGEventSource(stateID: .hidSystemState)
            guard let down = CGEvent(keyboardEventSource: source, virtualKey: code, keyDown: true),
                  let up = CGEvent(keyboardEventSource: source, virtualKey: code, keyDown: false)
            else { return .eventCreationFailed }
            down.flags = flags
            up.flags = flags
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)

        case .mouse(let button):
            let location = CGEvent(source: nil)?.location ?? .zero
            let types: (down: CGEventType, up: CGEventType) = switch button {
            case .left: (.leftMouseDown, .leftMouseUp)
            case .right: (.rightMouseDown, .rightMouseUp)
            default: (.otherMouseDown, .otherMouseUp)
            }
            guard let down = CGEvent(mouseEventSource: nil, mouseType: types.down,
                                     mouseCursorPosition: location, mouseButton: button),
                  let up = CGEvent(mouseEventSource: nil, mouseType: types.up,
                                   mouseCursorPosition: location, mouseButton: button)
            else { return .eventCreationFailed }
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)
        }

        // 事件进入 HID 层需要极短时间
        Thread.sleep(forTimeInterval: 0.05)

        // 空闲计时器归零 = 事件确实落地。
        // 空闲本来就接近 0（用户正在操作）时无法区分，按送达处理。
        guard let idleBefore, let idleAfter = Self.systemIdleSeconds() else { return .delivered }
        if idleAfter < idleBefore || idleAfter < 0.5 { return .delivered }
        return .notDelivered
    }
}

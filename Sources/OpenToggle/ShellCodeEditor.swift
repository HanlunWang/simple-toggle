import SwiftUI
import AppKit

/// Shell 脚本编辑器：NSTextView + 轻量正则高亮。
/// 高亮遵循黑白设计语言——不引入色相，用白色的透明度与字重分层：
///   正文 92% · 变量 80%(medium) · 字符串 58% · 注释 32% · 关键字 100%(bold)
struct ShellCodeEditor: NSViewRepresentable {
    @Binding var text: String

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSTextView.scrollableTextView()
        let textView = scroll.documentView as! NSTextView

        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        textView.drawsBackground = false
        textView.isRichText = false      // 粘贴富文本/附件会把 U+FFFC 垃圾字节写进脚本
        textView.usesFindBar = true      // Cmd-F 查找
        textView.font = Self.font
        textView.textColor = NSColor.white.withAlphaComponent(0.92)
        textView.insertionPointColor = .white
        textView.selectedTextAttributes = [
            .backgroundColor: NSColor.white.withAlphaComponent(0.22),
        ]
        textView.textContainerInset = NSSize(width: 8, height: 9)
        textView.allowsUndo = true
        // 代码编辑必须关掉所有智能替换（弯引号会毁掉脚本）
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false
        textView.isAutomaticSpellingCorrectionEnabled = false
        textView.isContinuousSpellCheckingEnabled = false
        textView.isGrammarCheckingEnabled = false

        textView.delegate = context.coordinator
        textView.string = text
        Self.highlight(textView)
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        context.coordinator.parent = self // 防止 Coordinator 持有过期 binding
        guard let textView = scroll.documentView as? NSTextView else { return }
        if textView.string != text {
            textView.string = text
            // 外部整体替换（切换脚本/切换类型模板）后必须清空 undo 栈：
            // 旧的按范围记录的 undo 回放到新文本上会拼接出错乱内容甚至越界崩溃
            textView.undoManager?.removeAllActions()
            Self.highlight(textView)
        }
    }

    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: ShellCodeEditor
        init(_ parent: ShellCodeEditor) { self.parent = parent }

        func textDidChange(_ notification: Notification) {
            guard let textView = notification.object as? NSTextView else { return }
            // 输入法组字期间（拼音未上屏）不动属性也不发布 binding：
            // setAttributes 会剥掉组字下划线、发布半成品还会触发回写打断组字
            guard !textView.hasMarkedText() else { return }
            parent.text = textView.string
            ShellCodeEditor.highlight(textView)
        }
    }

    // MARK: - 高亮

    private static let font = NSFont.monospacedSystemFont(ofSize: 12.5, weight: .regular)
    private static let boldFont = NSFont.monospacedSystemFont(ofSize: 12.5, weight: .bold)
    private static let mediumFont = NSFont.monospacedSystemFont(ofSize: 12.5, weight: .medium)

    private struct Rule {
        let regex: NSRegularExpression
        let attributes: [NSAttributedString.Key: Any]
    }

    // 顺序即优先级（后应用者覆盖）：变量 → 关键字 → 字符串 → 注释
    private static let rules: [Rule] = {
        func rule(_ pattern: String, _ alpha: CGFloat, font: NSFont = font) -> Rule {
            Rule(regex: try! NSRegularExpression(pattern: pattern, options: [.anchorsMatchLines]),
                 attributes: [.foregroundColor: NSColor.white.withAlphaComponent(alpha), .font: font])
        }
        return [
            // 变量：$VAR / ${...} / $1 $@ $$ $! $? $#
            rule(#"\$\{[^}\n]*\}|\$[A-Za-z_][A-Za-z0-9_]*|\$[0-9@#?$!*]"#, 0.80, font: mediumFont),
            // 关键字/内建
            rule(#"(?<![\w-])(if|then|else|elif|fi|for|while|until|do|done|case|esac|in|function|exec|exit|return|trap|local|export|readonly|set|shift|break|continue|echo|printf|source)(?![\w-])"#,
                 1.0, font: boldFont),
            // 字符串（单引号不跨行，避免撇号污染整段）
            rule(#""(\\.|[^"\\])*"|'[^'\n]*'"#, 0.58),
            // 注释最后应用：覆盖误匹配进注释的字符串/变量色。
            // 仅行首或空白后的 # 算注释，${#VAR}、URL 锚点不受污染
            rule(#"(?<![^\s])#.*$"#, 0.32),
        ]
    }()

    static func highlight(_ textView: NSTextView) {
        guard let storage = textView.textStorage else { return }
        let full = NSRange(location: 0, length: (textView.string as NSString).length)
        storage.beginEditing()
        storage.setAttributes([
            .foregroundColor: NSColor.white.withAlphaComponent(0.92),
            .font: font,
        ], range: full)
        for rule in rules {
            rule.regex.enumerateMatches(in: textView.string, range: full) { match, _, _ in
                if let range = match?.range {
                    storage.addAttributes(rule.attributes, range: range)
                }
            }
        }
        storage.endEditing()
    }
}

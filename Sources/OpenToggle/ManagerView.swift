import SwiftUI
import AppKit

// MARK: - 脚本草稿（表单 ↔ 脚本文件 的中间表示）

struct ScriptDraft {
    var name = ""
    var icon = "🚀"
    var type: SwitchType = .toggle
    var params: [ParamDraft] = []
    var menubarMode = "none" // none | add | replace
    var menubarIcon = ""
    var countdown = false
    var scriptBody: String = ScriptDraft.toggleTemplate
    var shebang = "#!/bin/bash"
    var sourceURL: URL? // nil = 新建

    static let toggleTemplate = """
    case "$1" in
      on)
        # command(s) to turn the switch on
        ;;
      off)
        # command(s) to turn the switch off
        ;;
      status)
        # print "on" or "off" to stdout
        echo off
        ;;
    esac
    """

    static let daemonTemplate = """
    # Long-running foreground process.
    # Use `exec` so SIGTERM reaches it directly.
    exec sleep 999999
    """

    /// 从已有开关加载：元数据来自契约解析，脚本体 = 去掉 shebang 和契约行后的其余内容
    init(from sw: SwitchScript) {
        name = sw.name
        icon = sw.icon
        type = sw.type
        params = sw.params.map(ParamDraft.init(from:))
        if let mb = sw.menubar {
            menubarMode = mb.mode.rawValue
            menubarIcon = mb.icon
            countdown = mb.countdown
        }
        sourceURL = sw.url

        if let content = try? String(contentsOf: sw.url, encoding: .utf8) {
            var lines = content.components(separatedBy: "\n")
            if let first = lines.first, first.hasPrefix("#!") {
                shebang = first
                lines.removeFirst()
            }
            lines.removeAll {
                let t = $0.trimmingCharacters(in: .whitespaces)
                return t.hasPrefix("#") && t.contains("<switch.")
            }
            while lines.first?.trimmingCharacters(in: .whitespaces).isEmpty == true {
                lines.removeFirst()
            }
            while lines.last?.trimmingCharacters(in: .whitespaces).isEmpty == true {
                lines.removeLast()
            }
            scriptBody = lines.joined(separator: "\n")
        }
    }

    init() {}

    /// 自动生成的契约头（只读区域）
    func headerLines() -> [String] {
        var lines = [shebang]
        lines.append("# <switch.name> \(sanitize(name.isEmpty ? "Untitled Switch" : name))")
        lines.append("# <switch.icon> \(sanitize(icon.isEmpty ? "🔘" : icon))")
        lines.append("# <switch.type> \(type.rawValue)")
        for param in params where !param.key.isEmpty {
            lines.append("# <switch.param> \(param.contractLine())")
        }
        if menubarMode != "none", !menubarIcon.isEmpty {
            var spec = "mode=\(menubarMode) icon=\(sanitize(menubarIcon))"
            if countdown { spec += " countdown=on" }
            lines.append("# <switch.menubar> \(spec)")
        }
        return lines
    }

    func fullScript() -> String {
        (headerLines() + [""] + [scriptBody]).joined(separator: "\n") + "\n"
    }

    var fileName: String {
        if let sourceURL { return sourceURL.lastPathComponent }
        let slug = name.lowercased()
            .map { $0.isLetter || $0.isNumber ? $0 : "-" }
            .reduce(into: "") { result, ch in
                if ch == "-" && (result.isEmpty || result.hasSuffix("-")) { return }
                result.append(ch)
            }
            .trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        return (slug.isEmpty ? "switch" : slug) + ".sh"
    }

    var envNames: [String] {
        params.filter { !$0.key.isEmpty }.map {
            "SWITCH_" + String($0.key.uppercased().map { c in c.isLetter || c.isNumber ? c : "_" })
        }
    }

    private func sanitize(_ s: String) -> String {
        s.replacingOccurrences(of: "\"", with: "'").trimmingCharacters(in: .whitespaces)
    }
}

struct ParamDraft: Identifiable {
    let id = UUID()
    var key = ""
    var label = ""
    var type: ParamType = .select
    var defaultValue = ""
    var options = "" // "label=value|label=value"
    var presets = "" // 快捷按钮："1 h=60|2 h=120"
    var minValue = ""
    var maxValue = ""
    var hint = ""

    init() {}

    init(from p: SwitchParam) {
        key = p.key
        label = p.label
        type = p.type
        defaultValue = p.defaultValue
        options = p.options.map { "\($0.label)=\($0.value)" }.joined(separator: "|")
        presets = p.presets.map { "\($0.label)=\($0.value)" }.joined(separator: "|")
        minValue = p.minValue.map(String.init) ?? ""
        maxValue = p.maxValue.map(String.init) ?? ""
        hint = p.hint ?? ""
    }

    func contractLine() -> String {
        var parts = ["key=\(key)", "type=\(type.rawValue)"]
        if !label.isEmpty { parts.append(quoted("label", label)) }
        if !defaultValue.isEmpty { parts.append(quoted("default", defaultValue)) }
        if type == .select, !options.isEmpty { parts.append(quoted("options", options)) }
        if type != .select, !presets.isEmpty { parts.append(quoted("presets", presets)) }
        if type == .number {
            if !minValue.isEmpty { parts.append("min=\(minValue)") }
            if !maxValue.isEmpty { parts.append("max=\(maxValue)") }
        }
        if !hint.isEmpty { parts.append(quoted("hint", hint)) }
        return parts.joined(separator: " ")
    }

    private func quoted(_ key: String, _ value: String) -> String {
        let clean = value.replacingOccurrences(of: "\"", with: "'")
        return clean.contains(" ") ? "\(key)=\"\(clean)\"" : "\(key)=\(clean)"
    }
}

// MARK: - 未保存状态（供 AppDelegate 退出前检查）

@MainActor
final class EditorState: ObservableObject {
    static let shared = EditorState()
    @Published var isDirty = false
}

// MARK: - 窗口关闭拦截（红色关闭按钮 / Cmd+W）

/// 接管窗口 delegate 拦截 windowShouldClose，其余消息全部转发给 SwiftUI 原有的 delegate
@MainActor
final class WindowCloseGuard: NSObject, NSWindowDelegate {
    static let shared = WindowCloseGuard()
    private weak var previousDelegate: NSWindowDelegate?

    func attach(to window: NSWindow) {
        guard window.delegate !== self else { return }
        previousDelegate = window.delegate
        window.delegate = self
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        if EditorState.shared.isDirty {
            let s = Loc.shared.s
            let alert = NSAlert()
            alert.messageText = s.closeAlertTitle
            alert.informativeText = s.closeAlertMessage
            alert.alertStyle = .warning
            alert.addButton(withTitle: s.closeAnyway)
            alert.addButton(withTitle: s.cancel)
            guard alert.runModal() == .alertFirstButtonReturn else { return false }
            EditorState.shared.isDirty = false
        }
        if let previous = previousDelegate,
           previous.responds(to: #selector(NSWindowDelegate.windowShouldClose(_:))) {
            return previous.windowShouldClose?(sender) ?? true
        }
        return true
    }

    // 其余 delegate 消息交还给 SwiftUI 的原 delegate，避免破坏窗口行为
    override func responds(to aSelector: Selector!) -> Bool {
        super.responds(to: aSelector) || previousDelegate?.responds(to: aSelector) == true
    }

    override func forwardingTarget(for aSelector: Selector!) -> Any? {
        if previousDelegate?.responds(to: aSelector) == true {
            return previousDelegate
        }
        return super.forwardingTarget(for: aSelector)
    }
}

/// 拿到 SwiftUI 视图所在的 NSWindow
private struct WindowAccessor: NSViewRepresentable {
    let onWindow: (NSWindow) -> Void

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async {
            if let window = view.window { onWindow(window) }
        }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        DispatchQueue.main.async {
            if let window = nsView.window { onWindow(window) }
        }
    }
}

// MARK: - 脚本管理窗口（侧边栏：所有开关；右侧：编辑器）

struct ManagerView: View {
    @ObservedObject var manager: SwitchManager
    @ObservedObject private var loc = Loc.shared
    @State private var selection: String?
    @State private var draft: ScriptDraft?
    /// 上次加载/保存时的完整脚本内容，用于脏状态判断
    @State private var savedSnapshot = ""
    @State private var showDiscardDialog = false
    @State private var pendingAction: (() -> Void)?
    @State private var suppressSelectionChange = false
    /// 待确认的"隐藏运行中开关"操作
    @State private var pendingHide: SwitchScript?

    private var isDirty: Bool {
        guard let draft else { return false }
        return draft.fullScript() != savedSnapshot
    }

    var body: some View {
        NavigationSplitView {
            sidebar
                // max 上限防止侧边栏拉太宽、挤爆右侧编辑区的最小宽度
                .navigationSplitViewColumnWidth(min: 170, ideal: 200, max: 240)
        } detail: {
            if draft != nil {
                EditorForm(
                    draft: Binding($draft)!,
                    manager: manager,
                    savedSnapshot: $savedSnapshot,
                    onSaved: { id in
                        // 只有 selection 真的会变时才需要抑制 onChange
                        //（值不变 onChange 不触发，标志位会残留并吞掉下一次切换）
                        if selection != id {
                            suppressSelectionChange = true
                            selection = id
                        }
                        loadDraft(id: id)
                    }
                )
            } else {
                VStack(spacing: 6) {
                    Text(">_")
                        .font(.system(size: 26, design: .monospaced))
                        .foregroundStyle(OT.txt3)
                    Text(loc.s.selectPromptTitle)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(OT.txt)
                    Text(loc.s.selectPromptDetail)
                        .font(.system(size: 11))
                        .foregroundStyle(OT.txt2)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .otSurface()
            }
        }
        // 920 = 侧边栏最大 240 + 编辑区两栏最小 (330 + 340) 留有余量，任何拖拽组合都不溢出
        .frame(minWidth: 920, minHeight: 560)
        .background(WindowAccessor { window in
            WindowCloseGuard.shared.attach(to: window)
        })
        .onAppear {
            if selection == nil, let first = manager.switches.first {
                selection = first.id
                loadDraft(id: first.id)
            }
        }
        .onChange(of: selection) { old, new in
            if suppressSelectionChange {
                suppressSelectionChange = false
                return
            }
            guard old != new else { return }
            if isDirty {
                // 先弹回原选择，等用户决定
                pendingAction = {
                    suppressSelectionChange = true
                    selection = new
                    loadDraft(id: new)
                }
                suppressSelectionChange = true
                selection = old
                showDiscardDialog = true
            } else {
                loadDraft(id: new)
            }
        }
        .onChange(of: draft?.fullScript() ?? "") { _, _ in
            EditorState.shared.isDirty = isDirty
        }
        .onDisappear {
            EditorState.shared.isDirty = false
            manager.panelDidDisappear()
        }
        .onAppear {
            manager.panelDidAppear()
        }
        // 隐藏运行中的开关：状态即将失去可见性，由用户决定是否先停止
        //（如 Keep Awake 设了"不限"，静默隐藏会导致屏幕永远不睡且无处可见）
        .confirmationDialog(
            String(format: loc.s.hideRunningTitleFormat, pendingHide?.name ?? ""),
            isPresented: hidePresented
        ) {
            hideDialogButtons
        } message: {
            Text(loc.s.hideRunningMessage)
        }
        .confirmationDialog(loc.s.discardTitle, isPresented: $showDiscardDialog) {
            Button(loc.s.discardConfirm, role: .destructive) {
                pendingAction?()
                pendingAction = nil
            }
            Button(loc.s.keepEditing, role: .cancel) {
                pendingAction = nil
            }
        } message: {
            Text(loc.s.discardMessage)
        }
    }

    /// 有未保存修改时先确认，否则直接执行
    private func attempt(_ action: @escaping () -> Void) {
        if isDirty {
            pendingAction = action
            showDiscardDialog = true
        } else {
            action()
        }
    }

    private var sidebar: some View {
        // 自绘侧栏（不用 List）：宽度行为完全可控，杜绝系统 List 在
        // 自定义行 + 弹性宽度组合下的头部裁切怪癖；样式也更贴设计稿 A3。
        ScrollView {
            VStack(spacing: 1) {
                ForEach(manager.switches) { sw in
                    SidebarRow(
                        manager: manager,
                        sw: sw,
                        selected: selection == sw.id,
                        onSelect: { selection = sw.id },
                        onToggleVisibility: { requestVisibilityToggle(sw) },
                        onDelete: { delete(sw) }
                    )
                }
            }
            .padding(.vertical, 6)
            .padding(.horizontal, 6)
        }
        .background(Color.black.opacity(0.18))
        .safeAreaInset(edge: .bottom) {
            HStack(spacing: 6) {
                Button {
                    attempt {
                        if selection != nil {
                            suppressSelectionChange = true
                            selection = nil
                        }
                        let newDraft = ScriptDraft()
                        draft = newDraft
                        savedSnapshot = newDraft.fullScript()
                        EditorState.shared.isDirty = false
                    }
                } label: {
                    // 侧栏拖窄时自动降级为纯图标，避免底栏溢出
                    ViewThatFits(in: .horizontal) {
                        Label(loc.s.newSwitch, systemImage: "plus")
                        Image(systemName: "plus")
                    }
                }
                .help(loc.s.newSwitch)
                Spacer(minLength: 0)
                LanguageMenu()
                Button {
                    NSWorkspace.shared.open(manager.scriptsDirectory)
                } label: {
                    Image(systemName: "folder")
                }
                .help(loc.s.openFolderHelp)
            }
            .buttonStyle(.borderless)
            .padding(.horizontal, 10)
            .padding(.vertical, 9)
            .background(.bar)
        }
    }

    // 拆出弹窗子表达式，缓解 body 的类型检查负担（SourceKit 编辑器体验）
    private var hidePresented: Binding<Bool> {
        Binding(get: { pendingHide != nil },
                set: { if !$0 { pendingHide = nil } })
    }

    @ViewBuilder
    private var hideDialogButtons: some View {
        Button(loc.s.hideAndStop) {
            if let sw = pendingHide {
                manager.setSwitch(sw, to: false)
                manager.setEnabled(sw, false)
            }
            pendingHide = nil
        }
        Button(loc.s.hideKeepRunning) {
            if let sw = pendingHide {
                manager.setEnabled(sw, false)
            }
            pendingHide = nil
        }
        Button(loc.s.cancel, role: .cancel) { pendingHide = nil }
    }

    /// 隐藏入口：开关运行中 → 弹确认；关着 → 直接隐藏；显示 → 直接恢复
    private func requestVisibilityToggle(_ sw: SwitchScript) {
        if manager.isEnabled(sw) {
            if manager.states[sw.id] == .on {
                pendingHide = sw
            } else {
                manager.setEnabled(sw, false)
            }
        } else {
            manager.setEnabled(sw, true)
        }
    }

    private struct SidebarRow: View {
        @ObservedObject var manager: SwitchManager
        let sw: SwitchScript
        let selected: Bool
        let onSelect: () -> Void
        let onToggleVisibility: () -> Void
        let onDelete: () -> Void
        @ObservedObject private var loc = Loc.shared
        @State private var hovering = false

        var body: some View {
            let enabled = manager.isEnabled(sw)
            HStack(spacing: 7) {
                OTStatusMark(state: manager.states[sw.id] ?? .unknown)
                    .frame(width: 10)
                IconView(icon: sw.icon)
                    .font(.system(size: 12))
                    .frame(width: 17)
                Text(sw.name)
                    .font(.system(size: 11.5))
                    .foregroundStyle(enabled ? OT.txt : OT.txt3)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 2)
                Text(sw.type == .daemon ? "D" : "T")
                    .font(.system(size: 8.5, design: .monospaced))
                    .foregroundStyle(OT.txt3)
                    .help(sw.type == .daemon ? "daemon" : "toggle")
                Button(action: onToggleVisibility) {
                    Image(systemName: enabled ? "eye" : "eye.slash")
                        .font(.system(size: 9.5))
                        .foregroundStyle(enabled ? OT.txt2 : OT.txt3)
                        .frame(width: 16, height: 16)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(enabled ? loc.s.hideFromMenuBar : loc.s.showInMenuBar)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 5.5)
            .background(
                RoundedRectangle(cornerRadius: 8)
                    .fill(selected ? Color.white.opacity(0.08)
                          : hovering ? Color.white.opacity(0.035) : .clear)
            )
            .overlay(alignment: .leading) {
                if selected {
                    RoundedRectangle(cornerRadius: 1)
                        .fill(Color.white.opacity(0.8))
                        .frame(width: 2, height: 16)
                        .shadow(color: .white.opacity(0.4), radius: 3)
                }
            }
            .contentShape(Rectangle())
            .onTapGesture(perform: onSelect)
            .onHover { hovering = $0 }
            // 自绘行的无障碍语义（List 原生提供，这里手动补上）
            .accessibilityElement(children: .combine)
            .accessibilityLabel(sw.name)
            .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
            .contextMenu {
                Button(enabled ? loc.s.hideFromMenuBar : loc.s.showInMenuBar,
                       action: onToggleVisibility)
                Divider()
                Button(loc.s.deleteToTrash, role: .destructive, action: onDelete)
            }
        }
    }

    private func loadDraft(id: String?) {
        guard let id, let sw = manager.switches.first(where: { $0.id == id }) else { return }
        let loaded = ScriptDraft(from: sw)
        draft = loaded
        savedSnapshot = loaded.fullScript()
        EditorState.shared.isDirty = false
    }

    private func delete(_ sw: SwitchScript) {
        manager.deleteScript(sw)
        if selection == sw.id {
            selection = nil
            draft = nil
        }
    }
}

// MARK: - 语言切换菜单

struct LanguageMenu: View {
    @ObservedObject private var loc = Loc.shared

    var body: some View {
        Menu {
            Picker("", selection: $loc.language) {
                Text(loc.s.langSystem).tag(AppLanguage.system)
                Text("English").tag(AppLanguage.english)
                Text("简体中文").tag(AppLanguage.simplifiedChinese)
            }
            .pickerStyle(.inline)
            .labelsHidden()
        } label: {
            Image(systemName: "globe")
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .help(loc.s.languageHelp)
    }
}

// MARK: - 编辑表单（左：配置；右：脚本文件视图）

private struct EditorForm: View {
    @Binding var draft: ScriptDraft
    @ObservedObject var manager: SwitchManager
    @Binding var savedSnapshot: String
    let onSaved: (String) -> Void

    @ObservedObject private var loc = Loc.shared
    @State private var statusMessage: String?
    @State private var statusIsError = false
    @State private var dismissTask: Task<Void, Never>?
    @State private var headerExpanded = false

    private var isDirty: Bool { draft.fullScript() != savedSnapshot }
    private var s: S { loc.s }

    var body: some View {
        // footer 单独占一行，避免 safeAreaInset 在 HSplitView 上不生效、盖住表单底部
        VStack(spacing: 0) {
            HSplitView {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        basicsSection
                        paramsSection
                        menubarSection
                    }
                    .padding(14)
                }
                .frame(minWidth: 330, idealWidth: 400)

                filePane
                    .frame(minWidth: 340)
            }
            Rectangle().fill(OT.line).frame(height: 1)
            footer
        }
        .otSurface()
    }

    // MARK: 基本信息

    private var typeIndex: Binding<Int> {
        Binding(
            get: { draft.type == .toggle ? 0 : 1 },
            set: { newIndex in
                let old = draft.type
                let new: SwitchType = newIndex == 0 ? .toggle : .daemon
                guard old != new else { return }
                draft.type = new
                // 模板没被动过时，跟着类型切换模板
                let oldTemplate = old == .toggle ? ScriptDraft.toggleTemplate : ScriptDraft.daemonTemplate
                if draft.scriptBody == oldTemplate {
                    draft.scriptBody = new == .toggle ? ScriptDraft.toggleTemplate : ScriptDraft.daemonTemplate
                }
            }
        )
    }

    private var basicsSection: some View {
        OTSection(title: s.basicsTitle) {
            HStack(alignment: .bottom, spacing: 10) {
                FieldColumn(s.nameLabel, detail: s.nameHelp) {
                    TextField(s.namePlaceholder, text: $draft.name)
                        .textFieldStyle(.roundedBorder)
                }
                FieldColumn(s.iconLabel, detail: s.iconHelp) {
                    IconPickerButton(icon: $draft.icon)
                }
            }
            FieldColumn(s.typeLabel, detail: s.typeHelp) {
                OTSegmented(options: [s.typeToggle, s.typeDaemon], selection: typeIndex)
            }
            Text(draft.type == .toggle ? s.toggleFootnote : s.daemonFootnote)
                .font(.system(size: 10))
                .foregroundStyle(OT.txt3)
        }
    }

    // MARK: 参数

    private var paramsSection: some View {
        OTSection(title: s.paramsTitle) {
            if draft.params.isEmpty {
                Text(s.paramsEmpty)
                    .font(.system(size: 10))
                    .foregroundStyle(OT.txt3)
            }
            ForEach($draft.params) { $param in
                ParamDraftRow(param: $param) {
                    draft.params.removeAll { $0.id == param.id }
                }
                .padding(10)
                .background(RoundedRectangle(cornerRadius: 10).fill(Color.black.opacity(0.18)))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(OT.line, lineWidth: 1))
            }
            OTChip(label: "＋ " + s.addParam, dashed: true) {
                draft.params.append(ParamDraft())
            }
            .frame(maxWidth: .infinity)
        }
    }

    // MARK: 菜单栏行为

    private var menubarModeIndex: Binding<Int> {
        Binding(
            get: { ["none", "add", "replace"].firstIndex(of: draft.menubarMode) ?? 0 },
            set: { newIndex in
                draft.menubarMode = ["none", "add", "replace"][newIndex]
                if draft.menubarMode != "none", draft.menubarIcon.isEmpty {
                    draft.menubarIcon = draft.icon
                }
            }
        )
    }

    private var menubarSection: some View {
        OTSection(title: s.menubarTitle) {
            HStack(spacing: 4) {
                OTSegmented(options: [s.modeNone, s.modeAdd, s.modeReplace], selection: menubarModeIndex)
                HelpButton(title: s.menubarTitle, text: s.modeHelp)
            }
            if draft.menubarMode != "none" {
                HStack(spacing: 12) {
                    FieldColumn(s.menubarIconLabel, detail: s.iconHelp) {
                        IconPickerButton(icon: $draft.menubarIcon)
                    }
                    VStack(alignment: .leading, spacing: 3) {
                        Spacer(minLength: 0)
                        HStack(spacing: 6) {
                            Toggle("", isOn: $draft.countdown)
                                .toggleStyle(OTGlowToggleStyle(small: true))
                                .labelsHidden()
                            Text(s.countdownToggle)
                                .font(.system(size: 11))
                                .foregroundStyle(OT.txt2)
                            HelpButton(title: s.countdownToggle, text: s.countdownHelp)
                        }
                    }
                }
                if draft.countdown {
                    let hasDuration = draft.params.contains { $0.key == "duration" && $0.type == .number }
                    // 缺依赖的警示：反白 ! 强调，不用色彩
                    HStack(spacing: 5) {
                        Text(hasDuration ? "✓" : "!")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(hasDuration ? OT.txt3 : .white)
                            .shadow(color: .white.opacity(hasDuration ? 0 : 0.5), radius: 3)
                        Text(hasDuration ? s.countdownOK : s.countdownMissing)
                            .font(.system(size: 10))
                            .foregroundStyle(hasDuration ? OT.txt3 : OT.txt2)
                    }
                }
            }
        }
    }

    // MARK: 脚本文件视图（自动契约头 + 可编辑代码区）

    private var filePane: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: "doc.plaintext")
                    .font(.system(size: 11))
                    .foregroundStyle(OT.txt3)
                Text(draft.fileName)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(OT.txt2)
                    .lineLimit(1)
                HelpButton(title: draft.fileName, text: s.scriptFileHelp)
                Spacer(minLength: 0)
            }
            // 注入变量：流式换行，窗口再窄也不溢出
            if !draft.envNames.isEmpty {
                WrapLayout(spacing: 4) {
                    Text(s.envVarsLabel)
                        .font(.system(size: 9))
                        .foregroundStyle(OT.txt3)
                        .padding(.vertical, 2)
                    ForEach(draft.envNames, id: \.self) { env in
                        Text("$" + env)
                            .font(.system(size: 9, design: .monospaced))
                            .foregroundStyle(OT.txt3)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .overlay(RoundedRectangle(cornerRadius: 5).strokeBorder(OT.line, lineWidth: 1))
                    }
                }
                .help(s.envVarsHelp)
            }

            VStack(alignment: .leading, spacing: 0) {
                // 契约头：由左侧表单自动生成，只读。
                // 可折叠（参数多时会很长）；展开态内部滚动、封顶高度，不挤压代码区。
                Button {
                    withAnimation(.easeInOut(duration: 0.15)) { headerExpanded.toggle() }
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "chevron.right")
                            .font(.system(size: 8, weight: .semibold))
                            .foregroundStyle(OT.txt3)
                            .rotationEffect(.degrees(headerExpanded ? 90 : 0))
                        Text(String(format: s.contractHeaderFormat, draft.headerLines().count))
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(OT.txt3)
                            .lineLimit(1)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .background(Color.black.opacity(0.42))

                if headerExpanded {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 2) {
                            ForEach(Array(draft.headerLines().enumerated()), id: \.offset) { _, line in
                                Text(line)
                                    .font(.system(size: 11.5, design: .monospaced))
                                    .foregroundColor(line.hasPrefix("#!") ? OT.txt2 : OT.txt3)
                                    .fixedSize(horizontal: false, vertical: true)
                                    .textSelection(.enabled)
                            }
                        }
                        .padding(.horizontal, 10)
                        .padding(.bottom, 8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .frame(maxHeight: 150)
                    .background(Color.black.opacity(0.42))
                }

                Rectangle().fill(OT.line).frame(height: 1)

                // 脚本体：单色分层语法高亮（注释 32% / 字符串 58% / 变量 80% / 关键字加粗全白）
                ShellCodeEditor(text: $draft.scriptBody)
                    .frame(minHeight: 200)
            }
            .background(Color.black.opacity(0.30))
            .clipShape(RoundedRectangle(cornerRadius: 11))
            .overlay(
                RoundedRectangle(cornerRadius: 11)
                    .strokeBorder(OT.line, lineWidth: 1)
            )
        }
        .padding(14)
    }

    // MARK: 底栏

    private var canSave: Bool {
        !draft.name.trimmingCharacters(in: .whitespaces).isEmpty && isDirty
    }

    private var footer: some View {
        HStack(spacing: 9) {
            if let statusMessage {
                HStack(spacing: 5) {
                    Text(statusIsError ? "!" : "✓")
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(.white)
                        .shadow(color: .white.opacity(0.5), radius: 3)
                    Text(statusMessage)
                        .font(.system(size: 11.5))
                        .foregroundStyle(OT.txt2)
                        .lineLimit(1)
                }
                .transition(.opacity)
            } else if isDirty, draft.sourceURL != nil {
                Text("● " + s.unsavedHint)
                    .font(.system(size: 10.5))
                    .foregroundStyle(OT.txt3)
            }
            Spacer(minLength: 8)
            if let url = draft.sourceURL {
                Button(s.revealInFinder) {
                    NSWorkspace.shared.activateFileViewerSelecting([url])
                }
                .buttonStyle(.plain)
                .font(.system(size: 11))
                .foregroundStyle(OT.txt2)
            }
            Button { save() } label: {
                Text(draft.sourceURL == nil ? s.createButton : s.saveButton)
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundStyle(Color.black.opacity(0.88))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 5)
                    .background(RoundedRectangle(cornerRadius: 8).fill(Color.white.opacity(0.92)))
                    .shadow(color: .white.opacity(0.25), radius: 6)
            }
            .buttonStyle(.plain)
            .keyboardShortcut(.defaultAction)
            .disabled(!canSave)
            .opacity(canSave ? 1 : 0.35)
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 10)
        .background(Color.black.opacity(0.25))
        .animation(.easeInOut(duration: 0.2), value: statusMessage)
    }

    private func save() {
        do {
            let savedURL: URL
            if let url = draft.sourceURL {
                try manager.updateScript(at: url, content: draft.fullScript())
                savedURL = url
            } else {
                savedURL = try manager.saveScript(fileName: draft.fileName, content: draft.fullScript())
                draft.sourceURL = savedURL
            }
            let id = savedURL.lastPathComponent
            // 正在运行的开关换了脚本/配置 → 立即用新内容重启
            if let sw = manager.switches.first(where: { $0.id == id }) {
                manager.restartIfRunning(sw)
            }
            showStatus(String(format: s.savedToastFormat, id), isError: false)
            onSaved(id)
        } catch {
            showStatus(String(format: s.saveFailedFormat, error.localizedDescription), isError: true)
        }
    }

    /// 显示提示并在几秒后自动消失
    private func showStatus(_ message: String, isError: Bool) {
        statusIsError = isError
        statusMessage = message
        dismissTask?.cancel()
        dismissTask = Task {
            try? await Task.sleep(for: .seconds(isError ? 8 : 3))
            guard !Task.isCancelled else { return }
            statusMessage = nil
        }
    }
}

// MARK: - 参数行

private struct ParamDraftRow: View {
    @Binding var param: ParamDraft
    let onDelete: () -> Void
    @ObservedObject private var loc = Loc.shared

    private var s: S { loc.s }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .bottom, spacing: 10) {
                FieldColumn(s.keyLabel, detail: s.keyHelp) {
                    TextField("mode", text: $param.key)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                        .frame(width: 110)
                }
                FieldColumn(s.displayNameLabel, detail: s.displayNameHelp) {
                    TextField(s.displayNamePlaceholder, text: $param.label)
                        .textFieldStyle(.roundedBorder)
                }
                FieldColumn(s.controlTypeLabel, detail: s.controlTypeHelp) {
                    Picker("", selection: $param.type) {
                        Text(s.ptSelect).tag(ParamType.select)
                        Text(s.ptNumber).tag(ParamType.number)
                        Text(s.ptText).tag(ParamType.text)
                        Text(s.ptKey).tag(ParamType.key)
                    }
                    .labelsHidden()
                    .fixedSize()
                }
                Button(role: .destructive) { onDelete() } label: {
                    Image(systemName: "minus.circle")
                }
                .buttonStyle(.borderless)
                .help(s.deleteParamHelp)
                .padding(.bottom, 4)
            }

            switch param.type {
            case .select:
                FieldColumn(s.optionsLabel, detail: s.optionsHelp) {
                    TextField("Display only=d|Display & system=dis", text: $param.options)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                }
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn(s.defaultLabel, detail: s.defaultHelp) {
                        TextField("d", text: $param.defaultValue)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 160)
                    }
                    FieldColumn(s.hintLabel, detail: s.hintHelp) {
                        TextField("", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
            case .number:
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn(s.minLabel, detail: s.minMaxHelp) {
                        TextField("0", text: $param.minValue)
                            .textFieldStyle(.roundedBorder).frame(width: 64)
                    }
                    FieldColumn(s.maxLabel, detail: s.minMaxHelp) {
                        TextField("1440", text: $param.maxValue)
                            .textFieldStyle(.roundedBorder).frame(width: 64)
                    }
                    FieldColumn(s.defaultLabel, detail: s.defaultHelp) {
                        TextField("0", text: $param.defaultValue)
                            .textFieldStyle(.roundedBorder).frame(width: 64)
                    }
                    FieldColumn(s.hintLabel, detail: s.hintHelp) {
                        TextField("", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
                FieldColumn(s.presetsLabel, detail: s.presetsHelp) {
                    TextField("1 h=60|2 h=120|4 h=240|8 h=480", text: $param.presets)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                }
            case .text:
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn(s.defaultLabel, detail: s.defaultHelp) {
                        TextField("", text: $param.defaultValue)
                            .textFieldStyle(.roundedBorder).frame(width: 140)
                    }
                    FieldColumn(s.hintLabel, detail: s.hintHelp) {
                        TextField("", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
                FieldColumn(s.presetsLabel, detail: s.presetsHelp) {
                    TextField("Corp proxy=http://proxy:8080|Direct=", text: $param.presets)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                }
            case .key:
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn(s.defaultLabel, detail: s.keyParamHelp) {
                        KeyPickerButton(value: $param.defaultValue)
                    }
                    FieldColumn(s.hintLabel, detail: s.hintHelp) {
                        TextField("", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
            }
        }
    }
}

// MARK: - 通用小组件：字段标题列 / 详解按钮

/// 输入控件上方的常驻小标题；detail 提供该字段的详细说明（ⓘ 弹窗）
struct FieldColumn<Content: View>: View {
    let title: String
    var detail: String?
    @ViewBuilder let content: Content

    init(_ title: String, detail: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title
        self.detail = detail
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if !title.isEmpty || detail != nil {
                HStack(spacing: 4) {
                    if !title.isEmpty {
                        Text(title)
                            .font(.system(size: 10))
                            .foregroundStyle(OT.txt3)
                    }
                    if let detail {
                        HelpButton(title: title, text: detail)
                            .controlSize(.small)
                    }
                }
            }
            content
        }
    }
}

/// "?" 按钮，点开显示详细说明（支持 Markdown 粗体、代码等）
struct HelpButton: View {
    let title: String
    let text: String
    @State private var showing = false

    var body: some View {
        Button {
            showing.toggle()
        } label: {
            Image(systemName: "questionmark.circle")
                .font(.caption)
                .foregroundStyle(.tertiary)
        }
        .buttonStyle(.borderless)
        .popover(isPresented: $showing, arrowEdge: .bottom) {
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    if !title.isEmpty {
                        Text(title)
                            .font(.headline)
                    }
                    Text(LocalizedStringKey(text))
                        .font(.callout)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(14)
            }
            .frame(width: 400)
            .frame(maxHeight: 440)
        }
    }
}

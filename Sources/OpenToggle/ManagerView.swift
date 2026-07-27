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
                ContentUnavailableView(
                    loc.s.selectPromptTitle,
                    systemImage: "switch.2",
                    description: Text(loc.s.selectPromptDetail)
                )
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
        List(selection: $selection) {
            ForEach(manager.switches) { sw in
                let enabled = manager.isEnabled(sw)
                HStack(spacing: 8) {
                    Circle()
                        .fill(manager.states[sw.id] == .on ? Color.green : Color(nsColor: .tertiaryLabelColor))
                        .frame(width: 6, height: 6)
                    IconView(icon: sw.icon)
                        .frame(width: 20)
                    Text(sw.name)
                        .lineLimit(1)
                    Spacer()
                    Text(sw.type == .daemon ? "daemon" : "toggle")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                    Button {
                        manager.setEnabled(sw, !enabled)
                    } label: {
                        Image(systemName: enabled ? "eye" : "eye.slash")
                            .font(.caption)
                            .foregroundStyle(enabled ? AnyShapeStyle(.secondary) : AnyShapeStyle(.tertiary))
                    }
                    .buttonStyle(.borderless)
                    .help(enabled ? loc.s.hideFromMenuBar : loc.s.showInMenuBar)
                }
                .opacity(enabled ? 1 : 0.45)
                .tag(sw.id)
                .contextMenu {
                    Button(enabled ? loc.s.hideFromMenuBar : loc.s.showInMenuBar) {
                        manager.setEnabled(sw, !enabled)
                    }
                    Divider()
                    Button(loc.s.deleteToTrash, role: .destructive) {
                        delete(sw)
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            HStack {
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
                    Label(loc.s.newSwitch, systemImage: "plus")
                }
                Spacer()
                LanguageMenu()
                Button {
                    NSWorkspace.shared.open(manager.scriptsDirectory)
                } label: {
                    Image(systemName: "folder")
                }
                .help(loc.s.openFolderHelp)
            }
            .buttonStyle(.borderless)
            .padding(10)
            .background(.bar)
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

    private var isDirty: Bool { draft.fullScript() != savedSnapshot }
    private var s: S { loc.s }

    var body: some View {
        // footer 单独占一行，避免 safeAreaInset 在 HSplitView 上不生效、盖住表单底部
        VStack(spacing: 0) {
            HSplitView {
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        basicsSection
                        paramsSection
                        menubarSection
                    }
                    .padding(16)
                }
                .frame(minWidth: 330, idealWidth: 400)

                filePane
                    .frame(minWidth: 340)
            }
            Divider()
            footer
        }
    }

    // MARK: 基本信息

    private var basicsSection: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 10) {
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
                    Picker("", selection: $draft.type) {
                        Text(s.typeToggle).tag(SwitchType.toggle)
                        Text(s.typeDaemon).tag(SwitchType.daemon)
                    }
                    .pickerStyle(.radioGroup)
                    .labelsHidden()
                    .onChange(of: draft.type) { old, new in
                        // 模板没被动过时，跟着类型切换模板
                        let oldTemplate = old == .toggle ? ScriptDraft.toggleTemplate : ScriptDraft.daemonTemplate
                        if draft.scriptBody == oldTemplate {
                            draft.scriptBody = new == .toggle ? ScriptDraft.toggleTemplate : ScriptDraft.daemonTemplate
                        }
                    }
                }
                Text(draft.type == .toggle ? s.toggleFootnote : s.daemonFootnote)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(6)
        } label: {
            Text(s.basicsTitle).font(.headline)
        }
    }

    // MARK: 参数

    private var paramsSection: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 10) {
                if draft.params.isEmpty {
                    Text(s.paramsEmpty)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                ForEach($draft.params) { $param in
                    ParamDraftRow(param: $param) {
                        draft.params.removeAll { $0.id == param.id }
                    }
                    if param.id != draft.params.last?.id {
                        Divider()
                    }
                }
                Button {
                    draft.params.append(ParamDraft())
                } label: {
                    Label(s.addParam, systemImage: "plus")
                }
                .buttonStyle(.borderless)
            }
            .padding(6)
        } label: {
            Text(s.paramsTitle).font(.headline)
        }
    }

    // MARK: 菜单栏行为

    private var menubarSection: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 10) {
                FieldColumn("", detail: s.modeHelp) {
                    Picker("", selection: $draft.menubarMode) {
                        Text(s.modeNone).tag("none")
                        Text(s.modeAdd).tag("add")
                        Text(s.modeReplace).tag("replace")
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .onChange(of: draft.menubarMode) { _, new in
                        if new != "none", draft.menubarIcon.isEmpty {
                            draft.menubarIcon = draft.icon
                        }
                    }
                }
                if draft.menubarMode != "none" {
                    FieldColumn(s.menubarIconLabel, detail: s.iconHelp) {
                        IconPickerButton(icon: $draft.menubarIcon)
                    }
                    HStack(spacing: 4) {
                        Toggle(s.countdownToggle, isOn: $draft.countdown)
                            .font(.callout)
                        HelpButton(title: s.countdownToggle, text: s.countdownHelp)
                    }
                    if draft.countdown {
                        let hasDuration = draft.params.contains { $0.key == "duration" && $0.type == .number }
                        Label(hasDuration ? s.countdownOK : s.countdownMissing,
                              systemImage: hasDuration ? "checkmark.circle" : "exclamationmark.triangle")
                            .font(.caption)
                            .foregroundStyle(hasDuration ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.orange))
                    }
                }
            }
            .padding(6)
        } label: {
            Text(s.menubarTitle).font(.headline)
        }
    }

    // MARK: 脚本文件视图（自动契约头 + 可编辑代码区）

    private var filePane: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: "doc.plaintext")
                    .foregroundStyle(.secondary)
                Text(draft.fileName)
                    .font(.system(.callout, design: .monospaced))
                HelpButton(title: draft.fileName, text: s.scriptFileHelp)
                Spacer()
                if !draft.envNames.isEmpty {
                    HStack(spacing: 4) {
                        Text(s.envVarsLabel)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                        ForEach(draft.envNames, id: \.self) { env in
                            Text("$" + env)
                                .font(.caption2.monospaced())
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Capsule().fill(Color.accentColor.opacity(0.15)))
                        }
                    }
                    .help(s.envVarsHelp)
                }
            }

            VStack(alignment: .leading, spacing: 0) {
                // 契约头：由左侧表单自动生成，只读（完整显示，长行自动换行）
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(Array(draft.headerLines().enumerated()), id: \.offset) { _, line in
                        Text(line)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundColor(line.hasPrefix("#!")
                                             ? Color(red: 0.78, green: 0.57, blue: 0.92)
                                             : Color(red: 0.45, green: 0.64, blue: 0.39))
                            .fixedSize(horizontal: false, vertical: true)
                            .textSelection(.enabled)
                    }
                }
                .padding(10)
                .padding(.trailing, 80) // 给"自动生成"角标留位
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(red: 0.09, green: 0.10, blue: 0.12))
                .overlay(alignment: .topTrailing) {
                    Text(s.autoGenerated)
                        .font(.caption2)
                        .foregroundStyle(Color(white: 0.45))
                        .padding(6)
                }

                Divider().overlay(Color(white: 0.25))

                // 脚本体：真正的代码编辑区
                CodeEditor(text: $draft.scriptBody)
                    .frame(minHeight: 220)
            }
            .background(Color(red: 0.12, green: 0.13, blue: 0.16))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(Color(white: 0.3), lineWidth: 1)
            )
        }
        .padding(16)
    }

    // MARK: 底栏

    private var footer: some View {
        HStack {
            if let statusMessage {
                Label(statusMessage,
                      systemImage: statusIsError ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                    .foregroundStyle(statusIsError ? .orange : .green)
                    .font(.callout)
                    .transition(.opacity)
            } else if isDirty, draft.sourceURL != nil {
                Text(s.unsavedHint)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if let url = draft.sourceURL {
                Button(s.revealInFinder) {
                    NSWorkspace.shared.activateFileViewerSelecting([url])
                }
            }
            Button(draft.sourceURL == nil ? s.createButton : s.saveButton) { save() }
                .keyboardShortcut(.defaultAction)
                .disabled(draft.name.trimmingCharacters(in: .whitespaces).isEmpty || !isDirty)
        }
        .padding(12)
        .background(.bar)
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

// MARK: - 深色代码编辑器

private struct CodeEditor: View {
    @Binding var text: String

    var body: some View {
        TextEditor(text: $text)
            .font(.system(size: 12.5, design: .monospaced))
            .foregroundColor(Color(white: 0.92))
            .scrollContentBackground(.hidden)
            .background(Color(red: 0.12, green: 0.13, blue: 0.16))
            .lineSpacing(2)
            .padding(6)
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
        VStack(alignment: .leading, spacing: 3) {
            if !title.isEmpty || detail != nil {
                HStack(spacing: 3) {
                    if !title.isEmpty {
                        Text(title)
                            .font(.caption)
                            .foregroundStyle(.secondary)
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

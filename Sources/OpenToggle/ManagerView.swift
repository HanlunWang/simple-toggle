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
        # 在这里写开启命令
        ;;
      off)
        # 在这里写关闭命令
        ;;
      status)
        # 输出 on 或 off
        echo off
        ;;
    esac
    """

    static let daemonTemplate = """
    # 长跑进程：app 启动并持有它，关 = SIGTERM
    # 用 exec 顶替 shell，让信号直达
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
        lines.append("# <switch.name> \(sanitize(name.isEmpty ? "未命名开关" : name))")
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
    var options = "" // "标签=值|标签=值"
    var presets = "" // 快捷按钮："1小时=60|2小时=120"
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
            let alert = NSAlert()
            alert.messageText = "有未保存的修改"
            alert.informativeText = "关闭窗口将丢失这些修改。"
            alert.alertStyle = .warning
            alert.addButton(withTitle: "仍要关闭")
            alert.addButton(withTitle: "取消")
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
                .navigationSplitViewColumnWidth(min: 170, ideal: 200)
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
                    "选择一个开关，或新建",
                    systemImage: "switch.2",
                    description: Text("左下角 ＋ 新建开关")
                )
            }
        }
        .frame(minWidth: 860, minHeight: 560)
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
        .confirmationDialog("当前开关有未保存的修改", isPresented: $showDiscardDialog) {
            Button("放弃修改", role: .destructive) {
                pendingAction?()
                pendingAction = nil
            }
            Button("继续编辑", role: .cancel) {
                pendingAction = nil
            }
        } message: {
            Text("离开后这些修改将丢失。")
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
                }
                .tag(sw.id)
                .contextMenu {
                    Button("删除（移到废纸篓）", role: .destructive) {
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
                    Label("新建开关", systemImage: "plus")
                }
                Spacer()
                Button {
                    NSWorkspace.shared.open(manager.scriptsDirectory)
                } label: {
                    Image(systemName: "folder")
                }
                .help("在 Finder 中打开脚本目录")
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

// MARK: - 编辑表单（左：配置；右：脚本文件视图）

private struct EditorForm: View {
    @Binding var draft: ScriptDraft
    @ObservedObject var manager: SwitchManager
    @Binding var savedSnapshot: String
    let onSaved: (String) -> Void

    @State private var statusMessage: String?
    @State private var statusIsError = false
    @State private var dismissTask: Task<Void, Never>?

    private var isDirty: Bool { draft.fullScript() != savedSnapshot }

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
                .frame(minWidth: 360, idealWidth: 400)

                filePane
                    .frame(minWidth: 380)
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
                    FieldColumn("名称（面板里的显示名）") {
                        TextField("如：Keep Awake", text: $draft.name)
                            .textFieldStyle(.roundedBorder)
                    }
                    FieldColumn("图标") {
                        IconPickerButton(icon: $draft.icon)
                    }
                }
                FieldColumn("类型") {
                    Picker("", selection: $draft.type) {
                        Text("命令式 toggle").tag(SwitchType.toggle)
                        Text("常驻进程 daemon").tag(SwitchType.daemon)
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
                Text(draft.type == .toggle
                     ? "开/关各执行一次命令；app 每 5 秒调一次 status 判断状态。适合改系统设置类。"
                     : "开 = 启动长跑进程并由 app 持有；关 = 发 SIGTERM；自然退出(exit 0)视为正常关闭。适合 caffeinate、循环类。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(6)
        } label: {
            SectionHeader(title: "基本信息", helpTitle: "基本信息", helpText: """
            **名称**：菜单栏面板和左侧列表里的显示名。新建时也用它生成脚本文件名（创建后文件名不再变）。

            **图标**：点按钮打开选择器，从 Emoji / SF Symbols 网格里选，也可以在「自定义」里输入符号名（如 `cup.and.saucer`）。

            **类型**（决定 app 怎么调用你的脚本）：
            - **命令式 toggle**：开/关时各执行一次 `脚本 on` / `脚本 off`；app 每 5 秒执行 `脚本 status`（需输出 `on` 或 `off`）来点亮状态灯。适合"改一个系统设置"类开关。
            - **常驻进程 daemon**：开时执行 `脚本 run` 并一直持有这个进程；关 = 发 SIGTERM；进程 exit 0 视为自然结束、开关自动归位。适合 caffeinate、轮循脚本这类长跑任务。
            """)
        }
    }

    // MARK: 参数

    private var paramsSection: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 10) {
                if draft.params.isEmpty {
                    Text("暂无参数。加了参数后，面板里该开关可展开出下拉框、数字、快捷按钮、填写框等控件，值会以环境变量传给脚本。")
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
                    Label("添加参数", systemImage: "plus")
                }
                .buttonStyle(.borderless)
            }
            .padding(6)
        } label: {
            SectionHeader(title: "参数 — 面板里的可调选项", helpTitle: "参数", helpText: """
            每个参数会出现在菜单栏面板中该开关的展开区（⌄）里，用户调好的值以**环境变量**传给脚本：key 为 `mode` → 脚本里读 `$SWITCH_MODE`。开关开着时修改参数会立即重启生效。

            **key**：参数标识（英文/数字），决定环境变量名。
            **显示名**：面板里显示的中文标签。
            **控件类型**：
            - **下拉选择**：从固定选项里挑一个，需填「选项」
            - **数字**：数字输入框 + 步进器，可设最小/最大值
            - **填写框**：自由文本

            **选项 / 快捷按钮** 都用 `标签=值|标签=值` 格式，如 `1小时=60|2小时=120`。快捷按钮会显示成一排小按钮，点一下直接把值填进输入框，和手动输入并存。

            **默认值**：用户没调过时的初始值。**提示**：悬停时的说明文字。
            """)
        }
    }

    // MARK: 菜单栏行为

    private var menubarSection: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 10) {
                Picker("", selection: $draft.menubarMode) {
                    Text("不显示").tag("none")
                    Text("新增一个图标").tag("add")
                    Text("替换主图标").tag("replace")
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .onChange(of: draft.menubarMode) { _, new in
                    if new != "none", draft.menubarIcon.isEmpty {
                        draft.menubarIcon = draft.icon
                    }
                }
                if draft.menubarMode != "none" {
                    FieldColumn("菜单栏里显示的图标") {
                        IconPickerButton(icon: $draft.menubarIcon)
                    }
                    Toggle("图标旁显示倒计时", isOn: $draft.countdown)
                        .font(.callout)
                    if draft.countdown {
                        let hasDuration = draft.params.contains { $0.key == "duration" && $0.type == .number }
                        Label(hasDuration
                              ? "倒计时读取「duration」参数（分钟，0 = 不限时不显示）"
                              : "还没有 key 为 duration 的数字参数，倒计时不会显示——在上方「参数」里加一个",
                              systemImage: hasDuration ? "checkmark.circle" : "exclamationmark.triangle")
                            .font(.caption)
                            .foregroundStyle(hasDuration ? .secondary : Color.orange)
                    }
                }
            }
            .padding(6)
        } label: {
            SectionHeader(title: "开启时的菜单栏图标", helpTitle: "菜单栏图标", helpText: """
            控制这个开关**开启期间**在系统菜单栏的存在感（关掉后自动消失）：

            - **新增一个图标**：菜单栏多出一个独立小图标，一眼可见"它在跑"；点击图标可直接关闭这个开关。
            - **替换主图标**：OpenToggle 自己的主图标临时换成这里选的图标。
            - **倒计时**：在图标旁边显示剩余时间（如 ☕️ 24:31），每秒刷新。它读取名为 `duration` 的数字参数（单位分钟，0 = 不限时则不显示倒计时），所以需要先在「参数」里建一个 key 为 `duration` 的数字参数。
            """)
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
                HelpButton(title: "脚本文件", text: """
                右边这块就是将要保存的脚本文件本体，分两段：

                **上半段（自动生成，只读）**：由左侧表单实时生成的"契约头"注释，app 靠它识别名称、图标、参数等。不需要也不能手改——改左边表单即可。

                **下半段（可编辑）**：脚本正文，直接写 shell 代码：
                - **toggle 型**：脚本会被以 `on` / `off` / `status` 参数调用（取 `$1` 判断），`status` 需输出 `on` 或 `off`
                - **daemon 型**：以 `run` 调用，用 `exec` 启动一个长跑进程
                - 参数值从环境变量读取（见上方 `$SWITCH_*` 胶囊）

                保存时上下两段合成完整文件写入脚本目录；正在运行的开关会立即用新内容重启。
                """)
                Spacer()
                if !draft.envNames.isEmpty {
                    HStack(spacing: 4) {
                        Text("可用变量")
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
                    .help("参数值以这些环境变量注入，脚本里直接使用")
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
                .padding(.trailing, 56) // 给"自动生成"角标留位
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(red: 0.09, green: 0.10, blue: 0.12))
                .overlay(alignment: .topTrailing) {
                    Text("自动生成")
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
                Text("有未保存的修改")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if let url = draft.sourceURL {
                Button("在 Finder 中显示") {
                    NSWorkspace.shared.activateFileViewerSelecting([url])
                }
            }
            Button(draft.sourceURL == nil ? "创建开关" : "保存修改") { save() }
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
            showStatus("已保存 \(id)", isError: false)
            onSaved(id)
        } catch {
            showStatus("保存失败：\(error.localizedDescription)", isError: true)
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

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .bottom, spacing: 10) {
                FieldColumn("key（变量名）", help: "英文标识；脚本里读环境变量 $SWITCH_\(param.key.isEmpty ? "KEY" : param.key.uppercased())") {
                    TextField("mode", text: $param.key)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                        .frame(width: 110)
                }
                FieldColumn("面板显示名") {
                    TextField("如：模式", text: $param.label)
                        .textFieldStyle(.roundedBorder)
                }
                FieldColumn("控件类型") {
                    Picker("", selection: $param.type) {
                        Text("下拉选择").tag(ParamType.select)
                        Text("数字").tag(ParamType.number)
                        Text("填写框").tag(ParamType.text)
                    }
                    .labelsHidden()
                    .fixedSize()
                }
                Button(role: .destructive) { onDelete() } label: {
                    Image(systemName: "minus.circle")
                }
                .buttonStyle(.borderless)
                .help("删除该参数")
                .padding(.bottom, 4)
            }

            switch param.type {
            case .select:
                FieldColumn("选项（格式：标签=值，用 | 分隔多个）") {
                    TextField("仅屏幕=d|屏幕与任务=dis", text: $param.options)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                }
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn("默认值（填选项里的\u{201C}值\u{201D}，留空取第一个）") {
                        TextField("d", text: $param.defaultValue)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 160)
                    }
                    FieldColumn("悬停提示（可选）") {
                        TextField("", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
            case .number:
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn("最小值") {
                        TextField("0", text: $param.minValue)
                            .textFieldStyle(.roundedBorder).frame(width: 64)
                    }
                    FieldColumn("最大值") {
                        TextField("1440", text: $param.maxValue)
                            .textFieldStyle(.roundedBorder).frame(width: 64)
                    }
                    FieldColumn("默认值") {
                        TextField("0", text: $param.defaultValue)
                            .textFieldStyle(.roundedBorder).frame(width: 64)
                    }
                    FieldColumn("悬停提示（可选）") {
                        TextField("如：0 = 一直保持", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
                FieldColumn("快捷按钮（可选；格式：标签=值，用 | 分隔，显示为一排小按钮）") {
                    TextField("1小时=60|2小时=120|4小时=240|8小时=480", text: $param.presets)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                }
            case .text:
                HStack(alignment: .bottom, spacing: 10) {
                    FieldColumn("默认值") {
                        TextField("", text: $param.defaultValue)
                            .textFieldStyle(.roundedBorder).frame(width: 140)
                    }
                    FieldColumn("输入框占位提示（可选）") {
                        TextField("", text: $param.hint)
                            .textFieldStyle(.roundedBorder)
                    }
                }
                FieldColumn("快捷按钮（可选；格式：标签=值，用 | 分隔）") {
                    TextField("公司代理=http://proxy:8080|直连=", text: $param.presets)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.callout, design: .monospaced))
                }
            }
        }
    }
}

// MARK: - 通用小组件：字段标题列 / 分组标题 / 详解按钮

/// 给输入控件加一个常驻的小标题（placeholder 一输入就没了，标题不会）
struct FieldColumn<Content: View>: View {
    let title: String
    var help: String?
    @ViewBuilder let content: Content

    init(_ title: String, help: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title
        self.help = help
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            content
        }
        .help(help ?? "")
    }
}

/// GroupBox 标题 + 详解按钮
struct SectionHeader: View {
    let title: String
    let helpTitle: String
    let helpText: String

    var body: some View {
        HStack(spacing: 4) {
            Text(title)
                .font(.headline)
            HelpButton(title: helpTitle, text: helpText)
        }
    }
}

/// "?" 按钮，点开显示详细说明（支持 Markdown 粗体等）
struct HelpButton: View {
    let title: String
    let text: String
    @State private var showing = false

    var body: some View {
        Button {
            showing.toggle()
        } label: {
            Image(systemName: "questionmark.circle")
                .foregroundStyle(.secondary)
        }
        .buttonStyle(.borderless)
        .popover(isPresented: $showing, arrowEdge: .bottom) {
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    Text(title)
                        .font(.headline)
                    Text(LocalizedStringKey(text))
                        .font(.callout)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(14)
            }
            .frame(width: 380)
            .frame(maxHeight: 420)
        }
    }
}

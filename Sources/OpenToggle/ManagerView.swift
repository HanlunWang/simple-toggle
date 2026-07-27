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
        minValue = p.minValue.map(String.init) ?? ""
        maxValue = p.maxValue.map(String.init) ?? ""
        hint = p.hint ?? ""
    }

    func contractLine() -> String {
        var parts = ["key=\(key)", "type=\(type.rawValue)"]
        if !label.isEmpty { parts.append(quoted("label", label)) }
        if !defaultValue.isEmpty { parts.append(quoted("default", defaultValue)) }
        if type == .select, !options.isEmpty { parts.append(quoted("options", options)) }
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

// MARK: - 脚本管理窗口（侧边栏：所有开关；右侧：编辑器）

struct ManagerView: View {
    @ObservedObject var manager: SwitchManager
    @State private var selection: String?
    @State private var draft: ScriptDraft?

    var body: some View {
        NavigationSplitView {
            sidebar
                .navigationSplitViewColumnWidth(min: 170, ideal: 200)
        } detail: {
            if draft != nil {
                EditorForm(
                    draft: Binding($draft)!,
                    manager: manager,
                    onSaved: { id in
                        selection = id
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
        .onAppear {
            if selection == nil, let first = manager.switches.first {
                selection = first.id
                loadDraft(id: first.id)
            }
        }
        .onChange(of: selection) { _, newValue in
            loadDraft(id: newValue)
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
                    selection = nil
                    draft = ScriptDraft()
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
        draft = ScriptDraft(from: sw)
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
    let onSaved: (String) -> Void

    @State private var statusMessage: String?
    @State private var statusIsError = false

    var body: some View {
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
        .safeAreaInset(edge: .bottom) { footer }
    }

    // MARK: 基本信息

    private var basicsSection: some View {
        GroupBox("基本信息") {
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text("名称").frame(width: 56, alignment: .trailing)
                    TextField("如：Keep Awake", text: $draft.name)
                        .textFieldStyle(.roundedBorder)
                    IconPickerButton(icon: $draft.icon)
                        .help("开关图标")
                }
                HStack(alignment: .top) {
                    Text("类型").frame(width: 56, alignment: .trailing)
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
                     ? "开/关各执行一次命令；app 每 5 秒调一次 status 判断状态。"
                     : "开 = 启动长跑进程并由 app 持有；关 = 发 SIGTERM；自然退出(exit 0)视为正常关闭。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.leading, 62)
            }
            .padding(6)
        }
    }

    // MARK: 参数

    private var paramsSection: some View {
        GroupBox("参数 — 面板里的下拉框 / 数字 / 填写框") {
            VStack(alignment: .leading, spacing: 10) {
                if draft.params.isEmpty {
                    Text("暂无参数。参数值会以环境变量注入脚本（如 $SWITCH_MODE）。")
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
        }
    }

    // MARK: 菜单栏行为

    private var menubarSection: some View {
        GroupBox("开启时的菜单栏图标") {
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
                    HStack {
                        Text("图标").frame(width: 56, alignment: .trailing)
                        IconPickerButton(icon: $draft.menubarIcon)
                        Spacer()
                    }
                    Toggle("显示倒计时（需要 key 为 duration 的数字参数，单位分钟，0=无限）",
                           isOn: $draft.countdown)
                        .font(.callout)
                }
            }
            .padding(6)
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
                Spacer()
                if !draft.envNames.isEmpty {
                    HStack(spacing: 4) {
                        ForEach(draft.envNames, id: \.self) { env in
                            Text("$" + env)
                                .font(.caption2.monospaced())
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Capsule().fill(Color.accentColor.opacity(0.15)))
                        }
                    }
                    .help("这些环境变量在脚本里可直接使用")
                }
            }

            VStack(alignment: .leading, spacing: 0) {
                // 契约头：由左侧表单自动生成，只读
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(Array(draft.headerLines().enumerated()), id: \.offset) { _, line in
                        Text(line)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundColor(line.hasPrefix("#!")
                                             ? Color(red: 0.78, green: 0.57, blue: 0.92)
                                             : Color(red: 0.45, green: 0.64, blue: 0.39))
                            .lineLimit(1)
                    }
                }
                .padding(10)
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
            }
            Spacer()
            if let url = draft.sourceURL {
                Button("在 Finder 中显示") {
                    NSWorkspace.shared.activateFileViewerSelecting([url])
                }
            }
            Button(draft.sourceURL == nil ? "创建开关" : "保存修改") { save() }
                .keyboardShortcut(.defaultAction)
                .disabled(draft.name.trimmingCharacters(in: .whitespaces).isEmpty)
        }
        .padding(12)
        .background(.bar)
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
            statusIsError = false
            statusMessage = "已保存 \(id)"
            onSaved(id)
        } catch {
            statusIsError = true
            statusMessage = "保存失败：\(error.localizedDescription)"
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
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                TextField("key（如 mode）", text: $param.key)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(.callout, design: .monospaced))
                    .frame(width: 110)
                TextField("显示名（如 模式）", text: $param.label)
                    .textFieldStyle(.roundedBorder)
                Picker("", selection: $param.type) {
                    Text("下拉选择").tag(ParamType.select)
                    Text("数字").tag(ParamType.number)
                    Text("填写框").tag(ParamType.text)
                }
                .labelsHidden()
                .fixedSize()
                Button(role: .destructive) { onDelete() } label: {
                    Image(systemName: "minus.circle")
                }
                .buttonStyle(.borderless)
                .help("删除该参数")
            }
            switch param.type {
            case .select:
                TextField("选项：标签=值|标签=值（如 仅屏幕=d|屏幕与任务=dis）", text: $param.options)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(.callout, design: .monospaced))
                TextField("默认值（选项里的“值”，留空取第一个）", text: $param.defaultValue)
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 240)
            case .number:
                HStack {
                    TextField("最小", text: $param.minValue)
                        .textFieldStyle(.roundedBorder).frame(width: 64)
                    TextField("最大", text: $param.maxValue)
                        .textFieldStyle(.roundedBorder).frame(width: 64)
                    TextField("默认", text: $param.defaultValue)
                        .textFieldStyle(.roundedBorder).frame(width: 64)
                    TextField("提示（可选）", text: $param.hint)
                        .textFieldStyle(.roundedBorder)
                }
            case .text:
                HStack {
                    TextField("默认值", text: $param.defaultValue)
                        .textFieldStyle(.roundedBorder).frame(width: 140)
                    TextField("提示（可选）", text: $param.hint)
                        .textFieldStyle(.roundedBorder)
                }
            }
        }
    }
}

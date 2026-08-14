import Foundation
import AppKit

/// 核心管理器：扫描脚本目录、维护开关状态、进程生命周期、参数、持久化。
/// 对应规划文档 §8 的 SwitchRegistry + ProcessManager + StatusPoller + PersistenceStore。
@MainActor
final class SwitchManager: ObservableObject {
    static let shared = SwitchManager()

    @Published private(set) var switches: [SwitchScript] = []
    @Published private(set) var states: [String: SwitchState] = [:]
    /// 参数当前值：switchID → (paramKey → value)
    @Published private(set) var paramValues: [String: [String: String]] = [:]
    /// 停用的开关：不出现在面板、不轮询、不随启动恢复；脚本与配置保留
    @Published private(set) var disabledIDs: Set<String> = []
    /// menubar mode=replace 的开关开启时，主图标显示这个（emoji 或 "sf:<name>"）
    @Published private(set) var iconOverride: String?
    /// replace 模式开关的倒计时文本（显示在主图标旁）
    @Published private(set) var iconOverrideCountdown: String?

    /// daemon 型开关持有的常驻进程
    private var daemons: [String: Process] = [:]
    /// 开启时刻（用于倒计时显示）
    private var activatedAt: [String: Date] = [:]
    private var pollTimer: Timer?
    private var countdownTimer: Timer?
    private var isPolling = false
    /// 用户操作的世代号：轮询完成时若已过期，整批结果作废（防止慢速 status
    /// 子进程期间发生的开关操作被陈旧快照覆盖）
    private var stateGeneration = 0

    private let defaults = UserDefaults.standard
    private let desiredKey = "OpenToggle.desiredStates"
    private let paramsKey = "OpenToggle.paramValues"
    private let disabledKey = "OpenToggle.disabledSwitches"
    private let seededKey = "OpenToggle.seededScripts"

    var scriptsDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/switches", isDirectory: true)
    }

    // MARK: - 生命周期

    func start() {
        seedExamplesIfNeeded()
        loadParamValues()
        disabledIDs = Set(defaults.stringArray(forKey: disabledKey) ?? [])
        reload()
        // 控制 API 必须先于状态恢复启动：恢复拉起的 daemon 可能立刻调用
        // `opentoggle press`（经由本 API 代发），此时端口必须已在监听
        ControlServer.shared.start()
        sweepOrphanedDaemons()
        restoreDesiredStates()
        reschedulePolling()
        AccessibilityStatus.shared.start()
    }

    // MARK: - 自适应轮询
    // 状态灯只有面板/管理器打开时才被看到；后台把轮询降到 30s，
    // 省下每 5s 起 N 个子进程（networksetup/osascript 都不便宜）的常驻开销。

    private var visiblePanels = 0

    private var pollInterval: TimeInterval { visiblePanels > 0 ? 5 : 30 }

    func panelDidAppear() {
        visiblePanels += 1
        refreshStates() // 打开立即刷新一次，不等下个周期
        reschedulePolling()
        AccessibilityStatus.shared.refresh()
    }

    func panelDidDisappear() {
        visiblePanels = max(0, visiblePanels - 1)
        reschedulePolling()
    }

    private func reschedulePolling() {
        pollTimer?.invalidate()
        let interval = pollInterval
        let timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refreshStates() }
        }
        timer.tolerance = interval * 0.2 // 允许系统合并唤醒
        pollTimer = timer
    }

    /// app 退出时清理所有持有的 daemon，避免孤儿进程
    func shutdown() {
        for process in daemons.values where process.isRunning {
            process.terminate()
        }
        daemons.removeAll()
        try? FileManager.default.removeItem(at: daemonLedgerURL)
    }

    // MARK: - 孤儿 daemon 防护
    // 正常退出走 shutdown()；但 app 被 kill -9 / 崩溃时 shutdown 不会执行，
    // daemon（连同它的 caffeinate）会变成孤儿——屏幕从此常亮。
    // 对策：spawn 的 PID 记入台账文件，下次启动先清扫上一代残留。

    private var daemonLedgerURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/daemons.json")
    }

    private func writeDaemonLedger() {
        var entries: [String: Int] = [:]
        for (id, process) in daemons where process.isRunning {
            entries[id] = Int(process.processIdentifier)
        }
        if entries.isEmpty {
            try? FileManager.default.removeItem(at: daemonLedgerURL)
        } else if let data = try? JSONSerialization.data(withJSONObject: entries) {
            try? data.write(to: daemonLedgerURL)
        }
    }

    private func sweepOrphanedDaemons() {
        guard let data = try? Data(contentsOf: daemonLedgerURL),
              let entries = try? JSONSerialization.jsonObject(with: data) as? [String: Int]
        else { return }
        for (_, pid) in entries {
            // PID 复用防护：仅当该 pid 的命令行确实指向脚本目录才发信号
            let probe = Process()
            probe.executableURL = URL(fileURLWithPath: "/bin/ps")
            probe.arguments = ["-o", "command=", "-p", String(pid)]
            let pipe = Pipe()
            probe.standardOutput = pipe
            guard (try? probe.run()) != nil else { continue }
            probe.waitUntilExit()
            let command = String(data: pipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
            if command.contains(scriptsDirectory.path) {
                kill(pid_t(pid), SIGTERM)
            }
        }
        try? FileManager.default.removeItem(at: daemonLedgerURL)
    }

    // MARK: - 目录扫描

    func reload() {
        let fm = FileManager.default
        let files = (try? fm.contentsOfDirectory(at: scriptsDirectory, includingPropertiesForKeys: nil))
            ?? []
        switches = files
            .filter { !$0.lastPathComponent.hasPrefix(".") }
            .compactMap { SwitchScript.parse(url: $0) }
            .sorted { $0.name < $1.name }
        for sw in switches where states[sw.id] == nil {
            states[sw.id] = .unknown
        }
        refreshStates()
    }

    /// 增量 seeding：每个预制脚本只安装一次（记录在 UserDefaults），
    /// 用户删除后不会复活；新版本新增的预制脚本能补装到已有安装。
    private func seedExamplesIfNeeded() {
        let fm = FileManager.default
        try? fm.createDirectory(at: scriptsDirectory, withIntermediateDirectories: true)
        var seeded = Set(defaults.stringArray(forKey: seededKey) ?? [])
        var disabled = Set(defaults.stringArray(forKey: disabledKey) ?? [])
        for example in ExampleScripts.all where !seeded.contains(example.fileName) {
            let url = scriptsDirectory.appendingPathComponent(example.fileName)
            if !fm.fileExists(atPath: url.path) {
                try? example.content.write(to: url, atomically: true, encoding: .utf8)
                try? fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
                if !example.enabledByDefault {
                    disabled.insert(example.fileName)
                }
            }
            seeded.insert(example.fileName)
        }
        defaults.set(Array(seeded), forKey: seededKey)
        defaults.set(Array(disabled), forKey: disabledKey)
    }

    /// 编辑器 GUI 保存脚本入口
    func saveScript(fileName: String, content: String) throws -> URL {
        let fm = FileManager.default
        try fm.createDirectory(at: scriptsDirectory, withIntermediateDirectories: true)
        var url = scriptsDirectory.appendingPathComponent(fileName)
        // 避免覆盖已有脚本：自动加序号
        var counter = 2
        let base = (fileName as NSString).deletingPathExtension
        let ext = (fileName as NSString).pathExtension
        while fm.fileExists(atPath: url.path) {
            url = scriptsDirectory.appendingPathComponent("\(base)-\(counter).\(ext)")
            counter += 1
        }
        try content.write(to: url, atomically: true, encoding: .utf8)
        try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        reload()
        return url
    }

    /// 编辑器保存已有脚本
    func updateScript(at url: URL, content: String) throws {
        try content.write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        reload()
    }

    /// 删除脚本（移到废纸篓，可恢复）；开着的先关掉
    func deleteScript(_ sw: SwitchScript) {
        if states[sw.id] == .on {
            setSwitch(sw, to: false)
        }
        try? FileManager.default.trashItem(at: sw.url, resultingItemURL: nil)
        states[sw.id] = nil
        reload()
    }

    /// 脚本内容/配置改动后，若开关正开着则立即用新内容重启
    func restartIfRunning(_ sw: SwitchScript) {
        stateGeneration += 1
        guard states[sw.id] == .on else { return }
        switch sw.type {
        case .daemon:
            daemons[sw.id]?.terminate()
            daemons[sw.id] = nil
            spawnDaemon(sw)
        case .toggle:
            let env = environment(for: sw)
            Task.detached { [weak self] in
                _ = ScriptRunner.run(sw.url, argument: "on", environment: env)
                await MainActor.run { self?.refreshStates() }
            }
        }
    }

    // MARK: - 启用 / 停用

    /// 面板中可见（启用）的开关
    var visibleSwitches: [SwitchScript] {
        switches.filter { !disabledIDs.contains($0.id) }
    }

    func isEnabled(_ sw: SwitchScript) -> Bool {
        !disabledIDs.contains(sw.id)
    }

    /// 停用：从面板隐藏、停止轮询与恢复；开着的先关掉。脚本与配置保留。
    func setEnabled(_ sw: SwitchScript, _ enabled: Bool) {
        if !enabled, states[sw.id] == .on {
            setSwitch(sw, to: false)
        }
        if enabled {
            disabledIDs.remove(sw.id)
        } else {
            disabledIDs.insert(sw.id)
        }
        defaults.set(Array(disabledIDs), forKey: disabledKey)
        refreshStates()
    }

    // MARK: - 参数

    func value(of param: SwitchParam, in sw: SwitchScript) -> String {
        paramValues[sw.id]?[param.key] ?? param.defaultValue
    }

    func value(ofKey key: String, in sw: SwitchScript) -> String? {
        guard let param = sw.params.first(where: { $0.key == key }) else { return nil }
        return value(of: param, in: sw)
    }

    /// 修改参数；开关开着时立即重启/重跑使其生效
    func setValue(_ newValue: String, of param: SwitchParam, in sw: SwitchScript) {
        stateGeneration += 1
        var values = paramValues[sw.id] ?? [:]
        guard values[param.key] != newValue else { return }
        values[param.key] = newValue
        paramValues[sw.id] = values
        defaults.set(paramValues, forKey: paramsKey)

        guard states[sw.id] == .on else { return }
        switch sw.type {
        case .daemon:
            // 热重启：换新参数重新拉起
            daemons[sw.id]?.terminate()
            daemons[sw.id] = nil
            spawnDaemon(sw)
        case .toggle:
            let env = environment(for: sw)
            Task.detached { [weak self] in
                _ = ScriptRunner.run(sw.url, argument: "on", environment: env)
                await MainActor.run { self?.refreshStates() }
            }
        }
    }

    private func loadParamValues() {
        paramValues = defaults.dictionary(forKey: paramsKey) as? [String: [String: String]] ?? [:]
    }

    private func environment(for sw: SwitchScript) -> [String: String] {
        var env: [String: String] = [:]
        for param in sw.params {
            env[param.envName] = value(of: param, in: sw)
        }
        return env
    }

    // MARK: - 倒计时

    /// 剩余秒数；仅当声明了 countdown 且 duration 参数（分钟）> 0 且开关开着时有值
    func remainingSeconds(for sw: SwitchScript) -> Int? {
        guard sw.menubar?.countdown == true,
              states[sw.id] == .on,
              let started = activatedAt[sw.id],
              let rawMinutes = Int(value(ofKey: "duration", in: sw) ?? ""), rawMinutes > 0
        else { return nil }
        // 上限约 1900 年，防脚本声明超大值时 *60 溢出 trap
        let minutes = min(rawMinutes, 1_000_000_000)
        return max(0, minutes * 60 - Int(Date().timeIntervalSince(started)))
    }

    // MARK: - 开关操作

    func setSwitch(_ sw: SwitchScript, to on: Bool) {
        stateGeneration += 1
        var desired = desiredStates()
        desired[sw.id] = on
        defaults.set(desired, forKey: desiredKey)

        switch sw.type {
        case .daemon:
            if on {
                spawnDaemon(sw)
            } else {
                daemons[sw.id]?.terminate()
                daemons[sw.id] = nil
                activatedAt[sw.id] = nil
                states[sw.id] = .off
                writeDaemonLedger()
            }
        case .toggle:
            states[sw.id] = .unknown
            let env = environment(for: sw)
            Task.detached { [weak self] in
                let result = ScriptRunner.run(sw.url, argument: on ? "on" : "off", environment: env)
                let status = ScriptRunner.run(sw.url, argument: "status", environment: env)
                await MainActor.run {
                    guard let self else { return }
                    if result.exitCode != 0 {
                        self.states[sw.id] = .error
                    } else {
                        let isOn = status.output == "on"
                        self.states[sw.id] = isOn ? .on : .off
                        self.activatedAt[sw.id] = isOn ? Date() : nil
                    }
                    self.syncMenuBar()
                }
            }
        }
        syncMenuBar()
    }

    private func spawnDaemon(_ sw: SwitchScript) {
        guard daemons[sw.id]?.isRunning != true else { return }
        guard let process = ScriptRunner.spawnDaemon(sw.url, environment: environment(for: sw)) else {
            states[sw.id] = .error
            return
        }
        process.terminationHandler = { [weak self] proc in
            Task { @MainActor in
                guard let self, self.daemons[sw.id] === proc else { return }
                self.daemons[sw.id] = nil
                self.activatedAt[sw.id] = nil
                self.writeDaemonLedger()
                if proc.terminationReason == .exit && proc.terminationStatus == 0 {
                    // 自然结束（如 caffeinate -t 到时）：视为正常关闭，重启后不再拉起
                    self.states[sw.id] = .off
                    var desired = self.desiredStates()
                    desired[sw.id] = false
                    self.defaults.set(desired, forKey: self.desiredKey)
                } else {
                    // 意外退出 → error
                    self.states[sw.id] = self.desiredStates()[sw.id] == true ? .error : .off
                }
                self.syncMenuBar()
            }
        }
        daemons[sw.id] = process
        activatedAt[sw.id] = Date()
        states[sw.id] = .on
        writeDaemonLedger()
    }

    // MARK: - 状态轮询

    func refreshStates() {
        guard !isPolling else { return }
        isPolling = true
        let generation = stateGeneration
        let toPoll = switches.filter { !disabledIDs.contains($0.id) }
        let daemonAlive = daemons.mapValues { $0.isRunning }
        let envs = Dictionary(uniqueKeysWithValues: toPoll.map { ($0.id, environment(for: $0)) })
        Task.detached { [weak self] in
            var newStates: [String: SwitchState] = [:]
            for sw in toPoll {
                switch sw.type {
                case .daemon:
                    newStates[sw.id] = daemonAlive[sw.id] == true ? .on : .off
                case .toggle:
                    let result = ScriptRunner.run(sw.url, argument: "status", environment: envs[sw.id] ?? [:])
                    if result.exitCode != 0 {
                        newStates[sw.id] = .unknown
                    } else {
                        newStates[sw.id] = result.output == "on" ? .on : .off
                    }
                }
            }
            let resolved = newStates
            await MainActor.run {
                guard let self else { return }
                self.isPolling = false
                // 轮询期间发生过用户操作 → 本批快照已过期，丢弃（下一轮会校正）
                guard self.stateGeneration == generation else { return }
                for (id, state) in resolved {
                    // daemon 的 error 态（意外退出）由 terminationHandler 标记，轮询不覆盖
                    if self.states[id] == .error && state == .off { continue }
                    // toggle 型被外部打开（如终端手动执行）时补记开启时刻
                    if state == .on && self.activatedAt[id] == nil { self.activatedAt[id] = Date() }
                    if state != .on && self.daemons[id] == nil { self.activatedAt[id] = nil }
                    // 等值守卫：值没变不写 @Published，避免无谓的 SwiftUI 失效
                    if self.states[id] != state { self.states[id] = state }
                }
                self.syncMenuBar()
            }
        }
    }

    /// 语言切换后重建非 SwiftUI 的菜单栏元素（NSStatusItem 的菜单文案）
    func languageDidChange() {
        syncMenuBar()
    }

    // MARK: - 菜单栏联动

    private func syncMenuBar() {
        let replaceSwitch = switches.first { $0.menubar?.mode == .replace && states[$0.id] == .on }
        // 等值守卫：倒计时 tick 每秒到来，值没变绝不触发 objectWillChange
        let newOverride = replaceSwitch?.menubar?.icon
        if iconOverride != newOverride { iconOverride = newOverride }
        let newCountdown = replaceSwitch
            .flatMap { remainingSeconds(for: $0) }
            .map(formatCountdown)
        if iconOverrideCountdown != newCountdown { iconOverrideCountdown = newCountdown }
        StatusBarController.shared.sync(with: self)
        updateCountdownTimer()
    }

    /// 任一显示中的倒计时（add 或 replace 模式）都需要每秒刷新一次菜单栏
    private func updateCountdownTimer() {
        let needsTick = switches.contains { remainingSeconds(for: $0) != nil }
        if needsTick, countdownTimer == nil {
            let timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
                Task { @MainActor in self?.syncMenuBar() }
            }
            timer.tolerance = 0.2
            countdownTimer = timer
        } else if !needsTick {
            countdownTimer?.invalidate()
            countdownTimer = nil
        }
    }

    // MARK: - 持久化恢复

    private func desiredStates() -> [String: Bool] {
        defaults.dictionary(forKey: desiredKey) as? [String: Bool] ?? [:]
    }

    /// 启动时恢复上次的开/关状态
    private func restoreDesiredStates() {
        let desired = desiredStates()
        for sw in switches where desired[sw.id] == true && !disabledIDs.contains(sw.id) {
            switch sw.type {
            case .daemon:
                spawnDaemon(sw)
            case .toggle:
                // 命令式开关先查真实状态，已经是 on 就不重复执行（避免无谓的副作用，如重启 Finder）
                let env = environment(for: sw)
                Task.detached { [weak self] in
                    let status = ScriptRunner.run(sw.url, argument: "status", environment: env)
                    if status.output != "on" {
                        _ = ScriptRunner.run(sw.url, argument: "on", environment: env)
                    }
                    await MainActor.run { self?.refreshStates() }
                }
            }
        }
        syncMenuBar()
    }
}

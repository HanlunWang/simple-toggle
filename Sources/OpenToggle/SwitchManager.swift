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
    /// menubar mode=replace 的开关开启时，主图标显示这个（emoji 或 "sf:<name>"）
    @Published private(set) var iconOverride: String?

    /// daemon 型开关持有的常驻进程
    private var daemons: [String: Process] = [:]
    /// 开启时刻（用于倒计时显示）
    private var activatedAt: [String: Date] = [:]
    private var pollTimer: Timer?
    private var isPolling = false

    private let defaults = UserDefaults.standard
    private let desiredKey = "OpenToggle.desiredStates"
    private let paramsKey = "OpenToggle.paramValues"

    var scriptsDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/switches", isDirectory: true)
    }

    // MARK: - 生命周期

    func start() {
        seedExamplesIfNeeded()
        loadParamValues()
        reload()
        restoreDesiredStates()
        pollTimer = Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refreshStates() }
        }
    }

    /// app 退出时清理所有持有的 daemon，避免孤儿进程
    func shutdown() {
        for process in daemons.values where process.isRunning {
            process.terminate()
        }
        daemons.removeAll()
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

    private func seedExamplesIfNeeded() {
        let fm = FileManager.default
        guard !fm.fileExists(atPath: scriptsDirectory.path) else { return }
        try? fm.createDirectory(at: scriptsDirectory, withIntermediateDirectories: true)
        for example in ExampleScripts.all {
            let url = scriptsDirectory.appendingPathComponent(example.fileName)
            try? example.content.write(to: url, atomically: true, encoding: .utf8)
            try? fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        }
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
              let minutes = Int(value(ofKey: "duration", in: sw) ?? ""), minutes > 0
        else { return nil }
        return max(0, minutes * 60 - Int(Date().timeIntervalSince(started)))
    }

    // MARK: - 开关操作

    func setSwitch(_ sw: SwitchScript, to on: Bool) {
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
    }

    // MARK: - 状态轮询

    func refreshStates() {
        guard !isPolling else { return }
        isPolling = true
        let toPoll = switches
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
                for (id, state) in resolved {
                    // daemon 的 error 态（意外退出）由 terminationHandler 标记，轮询不覆盖
                    if self.states[id] == .error && state == .off { continue }
                    // toggle 型被外部打开（如终端手动执行）时补记开启时刻
                    if state == .on && self.activatedAt[id] == nil { self.activatedAt[id] = Date() }
                    if state != .on && self.daemons[id] == nil { self.activatedAt[id] = nil }
                    self.states[id] = state
                }
                self.isPolling = false
                self.syncMenuBar()
            }
        }
    }

    // MARK: - 菜单栏联动

    private func syncMenuBar() {
        iconOverride = switches
            .first { $0.menubar?.mode == .replace && states[$0.id] == .on }?
            .menubar?.icon
        StatusBarController.shared.sync(with: self)
    }

    // MARK: - 持久化恢复

    private func desiredStates() -> [String: Bool] {
        defaults.dictionary(forKey: desiredKey) as? [String: Bool] ?? [:]
    }

    /// 启动时恢复上次的开/关状态
    private func restoreDesiredStates() {
        let desired = desiredStates()
        for sw in switches where desired[sw.id] == true {
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

import Foundation
import AppKit

/// 核心管理器：扫描脚本目录、维护开关状态、进程生命周期、持久化。
/// 对应规划文档 §8 的 SwitchRegistry + ProcessManager + StatusPoller + PersistenceStore。
@MainActor
final class SwitchManager: ObservableObject {
    static let shared = SwitchManager()

    @Published private(set) var switches: [SwitchScript] = []
    @Published private(set) var states: [String: SwitchState] = [:]

    /// daemon 型开关持有的常驻进程
    private var daemons: [String: Process] = [:]
    private var pollTimer: Timer?
    private var isPolling = false

    private let defaults = UserDefaults.standard
    private let desiredKey = "OpenToggle.desiredStates"

    var scriptsDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/open-toggle/switches", isDirectory: true)
    }

    // MARK: - 生命周期

    func start() {
        seedExamplesIfNeeded()
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
                states[sw.id] = .off
            }
        case .toggle:
            states[sw.id] = .unknown
            Task.detached { [weak self] in
                let result = ScriptRunner.run(sw.url, argument: on ? "on" : "off")
                let status = ScriptRunner.run(sw.url, argument: "status")
                await MainActor.run {
                    guard let self else { return }
                    if result.exitCode != 0 {
                        self.states[sw.id] = .error
                    } else {
                        self.states[sw.id] = status.output == "on" ? .on : .off
                    }
                }
            }
        }
    }

    private func spawnDaemon(_ sw: SwitchScript) {
        guard daemons[sw.id]?.isRunning != true else { return }
        guard let process = ScriptRunner.spawnDaemon(sw.url) else {
            states[sw.id] = .error
            return
        }
        process.terminationHandler = { [weak self] _ in
            Task { @MainActor in
                guard let self else { return }
                self.daemons[sw.id] = nil
                // 期望开着却退出了 → error；主动关掉的 → off
                self.states[sw.id] = self.desiredStates()[sw.id] == true ? .error : .off
            }
        }
        daemons[sw.id] = process
        states[sw.id] = .on
    }

    // MARK: - 状态轮询

    func refreshStates() {
        guard !isPolling else { return }
        isPolling = true
        let toPoll = switches
        let daemonAlive = daemons.mapValues { $0.isRunning }
        Task.detached { [weak self] in
            var newStates: [String: SwitchState] = [:]
            for sw in toPoll {
                switch sw.type {
                case .daemon:
                    newStates[sw.id] = daemonAlive[sw.id] == true ? .on : .off
                case .toggle:
                    let result = ScriptRunner.run(sw.url, argument: "status")
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
                    self.states[id] = state
                }
                self.isPolling = false
            }
        }
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
                Task.detached { [weak self] in
                    let status = ScriptRunner.run(sw.url, argument: "status")
                    if status.output != "on" {
                        _ = ScriptRunner.run(sw.url, argument: "on")
                    }
                    await MainActor.run { self?.refreshStates() }
                }
            }
        }
    }
}

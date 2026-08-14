import Foundation

/// 同步执行脚本并等待退出（用于 toggle 型的 on/off/status，都是短命令）。
/// 调用方负责放到后台线程。
enum ScriptRunner {
    struct Result {
        let exitCode: Int32
        let output: String
    }

    /// 脚本环境 = 进程环境 + OPENTOGGLE_BIN（脚本借此调用 `opentoggle press` 等，
    /// 不依赖 PATH——从 Finder 启动时 PATH 里没有 ~/.local/bin）+ 参数变量
    private static func baseEnvironment(merging environment: [String: String]) -> [String: String] {
        var env = ProcessInfo.processInfo.environment
        env["OPENTOGGLE_BIN"] = Bundle.main.executablePath ?? CommandLine.arguments[0]
        return env.merging(environment) { _, new in new }
    }

    static func run(_ url: URL, argument: String, environment: [String: String] = [:]) -> Result {
        let process = Process()
        // 脚本自带 shebang 时直接执行；没有可执行位则退回 /bin/sh
        if FileManager.default.isExecutableFile(atPath: url.path) {
            process.executableURL = url
            process.arguments = [argument]
        } else {
            process.executableURL = URL(fileURLWithPath: "/bin/sh")
            process.arguments = [url.path, argument]
        }
        process.environment = baseEnvironment(merging: environment)
        let stdout = Pipe()
        process.standardOutput = stdout
        // 不能挂一个从不读取的 Pipe：stderr 超过缓冲会反向堵死脚本
        process.standardError = FileHandle.nullDevice
        do {
            try process.run()
        } catch {
            return Result(exitCode: 127, output: "")
        }
        // 先读到 EOF 再等退出：顺序反了会在脚本输出超过管道缓冲(64KB)时死锁，
        // 且卡死的是轮询工作线程（isPolling 永远不复位）
        let data = stdout.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        let output = String(data: data, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return Result(exitCode: process.terminationStatus, output: output)
    }

    /// 启动 daemon 型脚本（`script run`），返回持有的进程，不等待退出。
    static func spawnDaemon(_ url: URL, environment: [String: String] = [:]) -> Process? {
        let process = Process()
        if FileManager.default.isExecutableFile(atPath: url.path) {
            process.executableURL = url
            process.arguments = ["run"]
        } else {
            process.executableURL = URL(fileURLWithPath: "/bin/sh")
            process.arguments = [url.path, "run"]
        }
        process.environment = baseEnvironment(merging: environment)
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        do {
            try process.run()
            return process
        } catch {
            return nil
        }
    }
}

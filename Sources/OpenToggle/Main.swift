import Foundation

/// 入口分流：
///   - 带参数 → CLI 模式（`gui` 子命令除外，用于终端显式启动 GUI）
///   - 无参数 + 终端（TTY）→ 输出帮助（CLI 用户裸敲 `opentoggle` 期望的是用法而非拉起 app）
///   - 无参数 + 非终端（Finder/launchd）→ GUI（菜单栏 app）
@main
enum Main {
    static func main() {
        let args = Array(CommandLine.arguments.dropFirst())
        if args.first == "gui" {
            OpenToggleApp.main()
        }
        if !args.isEmpty {
            CLI.run(args) // never returns
        }
        if isatty(STDOUT_FILENO) == 1 {
            CLI.run(["help"]) // never returns
        }
        OpenToggleApp.main()
    }
}

import Foundation

/// 入口分流：带参数 → CLI 模式（含 `mcp` 子命令）；无参数 → GUI（菜单栏 app）。
@main
enum Main {
    static func main() {
        let args = Array(CommandLine.arguments.dropFirst())
        if !args.isEmpty {
            CLI.run(args) // never returns
        }
        OpenToggleApp.main()
    }
}

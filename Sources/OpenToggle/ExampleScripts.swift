import Foundation

/// 首次运行时写入 ~/.config/open-toggle/switches/ 的示例脚本。
/// 覆盖契约的两类形态：daemon（Keep Awake）+ 命令式 toggle（Show Hidden Files）。
enum ExampleScripts {
    static let all: [(fileName: String, content: String)] = [
        ("keep-awake.sh", keepAwake),
        ("hidden-files.sh", hiddenFiles),
    ]

    private static let keepAwake = """
    #!/bin/bash
    # <switch.name> Keep Awake
    # <switch.icon> ☕️
    # <switch.type> daemon
    #
    # daemon 契约：app 以 `script run` 启动并持有本进程；关 = 收到 SIGTERM。
    # exec 让 caffeinate 直接顶替本 shell，SIGTERM 能直达。
    exec caffeinate -d
    """

    private static let hiddenFiles = """
    #!/bin/bash
    # <switch.name> Show Hidden Files
    # <switch.icon> 👁️
    # <switch.type> toggle
    #
    # toggle 契约：app 调 `script on` / `script off` / `script status`，
    # status 输出 on 或 off。
    case "$1" in
      on)
        defaults write com.apple.finder AppleShowAllFiles -bool true
        killall Finder
        ;;
      off)
        defaults write com.apple.finder AppleShowAllFiles -bool false
        killall Finder
        ;;
      status)
        v="$(defaults read com.apple.finder AppleShowAllFiles 2>/dev/null)"
        case "$v" in
          1|true|TRUE|YES|yes) echo on ;;
          *) echo off ;;
        esac
        ;;
    esac
    """
}

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
    # <switch.param> key=mode type=select label=模式 default=d options="仅保持屏幕=d|仅保持任务=is|屏幕与任务=dis"
    # <switch.param> key=duration type=number label=时长(分钟) default=0 min=0 max=1440 hint="0 = 一直保持"
    # <switch.menubar> mode=add icon=☕️ countdown=on
    #
    # daemon 契约：app 以 `script run` 启动并持有本进程；关 = 收到 SIGTERM。
    # 参数以环境变量注入：$SWITCH_MODE / $SWITCH_DURATION
    # caffeinate: -d 防屏幕睡眠 / -i 防系统空闲睡眠 / -s 防合盖睡眠(仅接电源时有效)
    ARGS="-${SWITCH_MODE:-d}"
    if [ "${SWITCH_DURATION:-0}" -gt 0 ]; then
      ARGS="$ARGS -t $((SWITCH_DURATION * 60))"
    fi
    # exec 让 caffeinate 直接顶替本 shell，SIGTERM 能直达；-t 到时自然退出，app 会把开关归位
    exec caffeinate $ARGS
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

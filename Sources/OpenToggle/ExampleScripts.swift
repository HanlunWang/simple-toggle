import Foundation

/// 预制脚本库：首次遇到时安装进 ~/.config/open-toggle/switches/（增量 seeding，见 SwitchManager）。
/// enabledByDefault=false 的开关安装后先停用——用户在管理器里按需启用，避免面板拥挤。
enum ExampleScripts {
    struct Example {
        let fileName: String
        let content: String
        let enabledByDefault: Bool
    }

    static let all: [Example] = [
        Example(fileName: "keep-awake.sh", content: keepAwake, enabledByDefault: true),
        Example(fileName: "hidden-files.sh", content: hiddenFiles, enabledByDefault: true),
        Example(fileName: "dark-mode.sh", content: darkMode, enabledByDefault: true),
        Example(fileName: "mute-audio.sh", content: muteAudio, enabledByDefault: true),
        Example(fileName: "hide-desktop.sh", content: hideDesktop, enabledByDefault: true),
        Example(fileName: "dock-autohide.sh", content: dockAutohide, enabledByDefault: true),
        Example(fileName: "wifi.sh", content: wifi, enabledByDefault: false),
        Example(fileName: "http-server.sh", content: httpServer, enabledByDefault: false),
        Example(fileName: "web-proxy.sh", content: webProxy, enabledByDefault: false),
    ]

    // MARK: - Keep Awake（daemon + 参数 + 菜单栏倒计时）

    private static let keepAwake = """
    #!/bin/bash
    # <switch.name> Keep Awake
    # <switch.icon> ☕️
    # <switch.type> daemon
    # <switch.param> key=mode type=select label=Mode default=d options="Display only=d|System only=is|Display & system=dis"
    # <switch.param> key=duration type=number label="Duration (min)" default=0 min=0 max=1440 presets="Unlimited=0|1 h=60|2 h=120|4 h=240|8 h=480" hint="0 = no limit"
    # <switch.menubar> mode=add icon=☕️ countdown=on
    #
    # Daemon contract: OpenToggle launches `<script> run` and holds the
    # process; turning the switch off sends SIGTERM. `exec` replaces this
    # shell so the signal reaches caffeinate directly. With -t the process
    # exits naturally (exit 0) and the app resets the switch.
    #
    # Parameters arrive as environment variables: $SWITCH_MODE, $SWITCH_DURATION
    # caffeinate: -d prevent display sleep / -i prevent idle sleep /
    #             -s prevent system sleep (effective on AC power only)
    ARGS="-${SWITCH_MODE:-d}"
    if [ "${SWITCH_DURATION:-0}" -gt 0 ]; then
      ARGS="$ARGS -t $((SWITCH_DURATION * 60))"
    fi
    exec caffeinate $ARGS
    """

    // MARK: - Show Hidden Files（toggle 基础样板）

    private static let hiddenFiles = """
    #!/bin/bash
    # <switch.name> Show Hidden Files
    # <switch.icon> 👁️
    # <switch.type> toggle
    #
    # Toggle contract: OpenToggle invokes `<script> on | off | status`.
    # `status` must print "on" or "off" to stdout; a non-zero exit code
    # marks the switch state as unknown.
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

    // MARK: - Dark Mode

    private static let darkMode = """
    #!/bin/bash
    # <switch.name> Dark Mode
    # <switch.icon> sf:moon.fill
    # <switch.type> toggle
    #
    # Toggles the system appearance via AppleScript. The first invocation
    # triggers a one-time Automation permission prompt (System Events).
    # External changes (Control Center, auto-switching) are picked up by
    # the 5-second status poll.
    case "$1" in
      on)
        osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to true'
        ;;
      off)
        osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to false'
        ;;
      status)
        v="$(osascript -e 'tell application "System Events" to tell appearance preferences to get dark mode' 2>/dev/null)"
        [ "$v" = "true" ] && echo on || echo off
        ;;
    esac
    """

    // MARK: - Mute Audio

    private static let muteAudio = """
    #!/bin/bash
    # <switch.name> Mute Audio
    # <switch.icon> sf:speaker.slash.fill
    # <switch.type> toggle
    #
    # Mutes/unmutes system output volume. No permissions required.
    case "$1" in
      on)
        osascript -e 'set volume output muted true'
        ;;
      off)
        osascript -e 'set volume output muted false'
        ;;
      status)
        v="$(osascript -e 'output muted of (get volume settings)' 2>/dev/null)"
        [ "$v" = "true" ] && echo on || echo off
        ;;
    esac
    """

    // MARK: - Hide Desktop Icons（演示/录屏场景）

    private static let hideDesktop = """
    #!/bin/bash
    # <switch.name> Hide Desktop Icons
    # <switch.icon> sf:rectangle.dashed
    # <switch.type> toggle
    #
    # Hides all desktop icons — useful for screen sharing, recording,
    # and presentations. Files stay untouched in ~/Desktop.
    case "$1" in
      on)
        defaults write com.apple.finder CreateDesktop -bool false
        killall Finder
        ;;
      off)
        defaults write com.apple.finder CreateDesktop -bool true
        killall Finder
        ;;
      status)
        v="$(defaults read com.apple.finder CreateDesktop 2>/dev/null)"
        case "$v" in
          0|false|FALSE|NO|no) echo on ;;
          *) echo off ;;
        esac
        ;;
    esac
    """

    // MARK: - Dock Auto-Hide

    private static let dockAutohide = """
    #!/bin/bash
    # <switch.name> Dock Auto-Hide
    # <switch.icon> sf:dock.rectangle
    # <switch.type> toggle
    #
    # Toggles Dock auto-hiding. Restarting the Dock is required for the
    # setting to take effect; open windows are not affected.
    case "$1" in
      on)
        defaults write com.apple.dock autohide -bool true
        killall Dock
        ;;
      off)
        defaults write com.apple.dock autohide -bool false
        killall Dock
        ;;
      status)
        v="$(defaults read com.apple.dock autohide 2>/dev/null)"
        case "$v" in
          1|true|TRUE|YES|yes) echo on ;;
          *) echo off ;;
        esac
        ;;
    esac
    """

    // MARK: - Wi-Fi（默认停用：误触会断网）

    private static let wifi = """
    #!/bin/bash
    # <switch.name> Wi-Fi
    # <switch.icon> sf:wifi
    # <switch.type> toggle
    #
    # Toggles Wi-Fi power. The device name (usually en0) is resolved
    # dynamically from the hardware port list.
    # Disabled by default: enable it in the script manager when needed.
    DEV="$(networksetup -listallhardwareports | awk '/Wi-Fi|AirPort/{getline; print $2; exit}')"
    [ -n "$DEV" ] || exit 1
    case "$1" in
      on)
        networksetup -setairportpower "$DEV" on
        ;;
      off)
        networksetup -setairportpower "$DEV" off
        ;;
      status)
        networksetup -getairportpower "$DEV" | grep -q ": On" && echo on || echo off
        ;;
    esac
    """

    // MARK: - Local HTTP Server（daemon + 参数：端口/目录）

    private static let httpServer = """
    #!/bin/bash
    # <switch.name> Local HTTP Server
    # <switch.icon> sf:globe
    # <switch.type> daemon
    # <switch.param> key=port type=number label=Port default=8000 min=1024 max=65535 presets="8000|8080|3000|5500"
    # <switch.param> key=dir type=text label=Directory default=~ hint="Directory to serve"
    # <switch.menubar> mode=add icon=sf:globe
    #
    # Serves a local directory over HTTP using Python's built-in server.
    # Handy for sharing files on the LAN or previewing static sites.
    # Disabled by default: enable it in the script manager when needed.
    DIR="${SWITCH_DIR:-~}"
    DIR="${DIR/#\\~/$HOME}"
    exec python3 -m http.server "${SWITCH_PORT:-8000}" --directory "$DIR"
    """

    // MARK: - Web Proxy（参数最全的样板：select + text + number + presets）

    private static let webProxy = """
    #!/bin/bash
    # <switch.name> Web Proxy
    # <switch.icon> sf:network.badge.shield.half.filled
    # <switch.type> toggle
    # <switch.param> key=service type=select label=Service default=Wi-Fi options="Wi-Fi=Wi-Fi|Ethernet=Ethernet"
    # <switch.param> key=host type=text label=Host default=127.0.0.1 presets="127.0.0.1"
    # <switch.param> key=port type=number label=Port default=7890 min=1 max=65535 presets="7890|8080|1080|8888"
    #
    # Sets/unsets the system HTTP + HTTPS proxy for the selected network
    # service. Adjust host/port to match your local proxy client.
    # Disabled by default: enable it in the script manager when needed.
    case "$1" in
      on)
        networksetup -setwebproxy "$SWITCH_SERVICE" "$SWITCH_HOST" "$SWITCH_PORT"
        networksetup -setsecurewebproxy "$SWITCH_SERVICE" "$SWITCH_HOST" "$SWITCH_PORT"
        ;;
      off)
        networksetup -setwebproxystate "$SWITCH_SERVICE" off
        networksetup -setsecurewebproxystate "$SWITCH_SERVICE" off
        ;;
      status)
        networksetup -getwebproxy "$SWITCH_SERVICE" 2>/dev/null | grep -q "^Enabled: Yes" && echo on || echo off
        ;;
    esac
    """
}

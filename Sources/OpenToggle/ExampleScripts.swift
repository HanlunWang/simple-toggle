import Foundation

/// 预制脚本库清单。脚本本体是 Sources/OpenToggle/Switches/ 下的真实 .sh 文件
/// （单一事实来源，仓库里可直接浏览/lint），经 SwiftPM resources 打进构建产物，
/// 首次遇到时安装进 ~/.config/open-toggle/switches/（增量 seeding，见 SwitchManager）。
/// enabledByDefault=false 的开关安装后先停用——用户在管理器里按需启用，避免面板拥挤。
enum ExampleScripts {
    struct Example {
        let fileName: String
        let content: String
        let enabledByDefault: Bool
    }

    private static let manifest: [(fileName: String, enabledByDefault: Bool)] = [
        ("keep-awake.sh", true),
        ("hidden-files.sh", true),
        ("dark-mode.sh", true),
        ("mute-audio.sh", true),
        ("hide-desktop.sh", true),
        ("dock-autohide.sh", true),
        ("wifi.sh", false),
        ("http-server.sh", false),
        ("web-proxy.sh", false),
    ]

    static let all: [Example] = manifest.compactMap { entry in
        guard let url = Bundle.module.url(forResource: entry.fileName,
                                          withExtension: nil,
                                          subdirectory: "Switches"),
              let content = try? String(contentsOf: url, encoding: .utf8)
        else { return nil }
        return Example(fileName: entry.fileName,
                       content: content,
                       enabledByDefault: entry.enabledByDefault)
    }
}

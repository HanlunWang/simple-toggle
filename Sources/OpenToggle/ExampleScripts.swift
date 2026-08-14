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

    /// 资源包解析：不用 SwiftPM 生成的 Bundle.module——它只认"可执行文件同目录"
    /// 和编译机的构建路径，经 brew 的 /opt/homebrew/bin 软链启动时两者皆空，
    /// 且失败即 fatalError。这里自己找（含软链穿透），找不到也绝不崩溃。
    private static let resourceBundle: Bundle? = {
        let bundleName = "OpenToggle_OpenToggle.bundle"
        let realExecDir = Bundle.main.executableURL?
            .resolvingSymlinksInPath()
            .deletingLastPathComponent()
        let candidates: [URL?] = [
            Bundle.main.resourceURL,                                   // app bundle 的 Resources/
            realExecDir,                                               // 真实可执行文件同目录（dev 构建）
            realExecDir?.deletingLastPathComponent()
                .appendingPathComponent("Resources"),                  // MacOS/../Resources（软链场景兜底）
        ]
        for candidate in candidates {
            guard let url = candidate?.appendingPathComponent(bundleName) else { continue }
            if FileManager.default.fileExists(atPath: url.path), let bundle = Bundle(url: url) {
                return bundle
            }
        }
        NSLog("OpenToggle: preset resource bundle not found; skipping example seeding")
        return nil
    }()

    static let all: [Example] = manifest.compactMap { entry in
        guard let url = resourceBundle?.url(forResource: entry.fileName,
                                            withExtension: nil,
                                            subdirectory: "Switches"),
              let content = try? String(contentsOf: url, encoding: .utf8)
        else { return nil }
        return Example(fileName: entry.fileName,
                       content: content,
                       enabledByDefault: entry.enabledByDefault)
    }
}

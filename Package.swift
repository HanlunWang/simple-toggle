// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "OpenToggle",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(name: "OpenToggle", path: "Sources/OpenToggle")
    ]
)

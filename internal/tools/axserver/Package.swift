// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ax_server",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "ax_server",
            path: "Sources"
        ),
    ]
)

import AppKit

struct FocusManager {
    /// Activates an app by name, optionally verifying focus.
    static func focusApp(appName: String, windowTitle: String?, verify: Bool) -> (ActionResult?, ErrorInfo?) {
        guard let app = findApp(named: appName) else {
            return (nil, ErrorInfo(code: -1, message: "App '\(appName)' not found or not running"))
        }

        app.activate()

        if verify {
            // Brief wait for activation
            Thread.sleep(forTimeInterval: 0.3)
            guard let frontmost = NSWorkspace.shared.frontmostApplication,
                  frontmost.processIdentifier == app.processIdentifier else {
                return (nil, ErrorInfo(code: -1, message: "Failed to bring '\(appName)' to front"))
            }
        }

        let pid = Int(app.processIdentifier)
        return (ActionResult(result: "focused \(appName) (pid \(pid))"), nil)
    }

    /// Returns the frontmost app's PID and window title.
    static func frontmost() -> (ActionResult?, ErrorInfo?) {
        guard let app = NSWorkspace.shared.frontmostApplication else {
            return (nil, ErrorInfo(code: -1, message: "Cannot determine frontmost application"))
        }
        let name = app.localizedName ?? "Unknown"
        let pid = Int(app.processIdentifier)

        // Get window title via AX
        let appRef = AXUIElementCreateApplication(Int32(pid))
        var windowTitle = ""
        if let windows = axValue(appRef, "AXWindows") as? [AXUIElement],
           let win = windows.first {
            windowTitle = axString(win, "AXTitle") ?? ""
        }

        struct FrontmostResult: Encodable {
            let app: String
            let pid: Int
            let window: String
        }
        // Return as simple action result with details
        return (ActionResult(result: "\(name) (pid \(pid), window: \(windowTitle))"), nil)
    }

    /// Lists all windows for an app.
    static func listWindows(pid: Int) -> [[String: String]] {
        let appRef = AXUIElementCreateApplication(Int32(pid))
        guard let windows = axValue(appRef, "AXWindows") as? [AXUIElement] else {
            return []
        }
        var result: [[String: String]] = []
        for (i, win) in windows.enumerated() {
            let title = axString(win, "AXTitle") ?? ""
            let role = axString(win, "AXRole") ?? ""
            result.append(["index": "\(i)", "title": title, "role": role])
        }
        return result
    }

    private static func findApp(named name: String) -> NSRunningApplication? {
        let lower = name.lowercased()
        for app in NSWorkspace.shared.runningApplications {
            if let n = app.localizedName, n.lowercased() == lower {
                return app
            }
            if let bundleID = app.bundleIdentifier, bundleID.lowercased().contains(lower) {
                return app
            }
        }
        return nil
    }
}

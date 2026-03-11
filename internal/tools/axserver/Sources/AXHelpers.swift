import ApplicationServices
import AppKit

func axValue(_ el: AXUIElement, _ attr: String) -> CFTypeRef? {
    var val: CFTypeRef?
    let err = AXUIElementCopyAttributeValue(el, attr as CFString, &val)
    return err == .success ? val : nil
}

func axString(_ el: AXUIElement, _ attr: String) -> String? {
    axValue(el, attr) as? String
}

func axBool(_ el: AXUIElement, _ attr: String) -> Bool? {
    guard let val = axValue(el, attr) else { return nil }
    if let num = val as? NSNumber { return num.boolValue }
    return nil
}

func axChildren(_ el: AXUIElement) -> [AXUIElement]? {
    axValue(el, "AXChildren") as? [AXUIElement]
}

/// Resolves an element by path (e.g. "window[0]/AXButton[2]/AXStaticText[0]").
func resolveElement(pid: Int, path: String) -> AXUIElement? {
    let appRef = AXUIElementCreateApplication(Int32(pid))
    guard let windows = axValue(appRef, "AXWindows") as? [AXUIElement] else {
        return nil
    }

    let allParts = path.split(separator: "/")
    guard !allParts.isEmpty else { return nil }

    // Parse window index from "window[N]"
    let winPart = allParts[0]
    var winIndex = 0
    if let bracketStart = winPart.firstIndex(of: "["),
       let bracketEnd = winPart.firstIndex(of: "]") {
        winIndex = Int(winPart[winPart.index(after: bracketStart)..<bracketEnd]) ?? 0
    }
    guard winIndex < windows.count else { return nil }

    let parts = allParts.dropFirst()
    var current: AXUIElement = windows[winIndex]

    for part in parts {
        guard let bracketStart = part.firstIndex(of: "["),
              let bracketEnd = part.firstIndex(of: "]") else { return nil }
        let role = String(part[part.startIndex..<bracketStart])
        guard let index = Int(part[part.index(after: bracketStart)..<bracketEnd]) else { return nil }

        guard let children = axChildren(current) else { return nil }
        var roleCount = 0
        var found = false
        for child in children {
            if axString(child, "AXRole") == role {
                if roleCount == index {
                    current = child
                    found = true
                    break
                }
                roleCount += 1
            }
        }
        if !found { return nil }
    }
    return current
}

/// Resolves an app name to its PID via NSWorkspace.
func resolvePID(appName: String) -> Int? {
    for app in NSWorkspace.shared.runningApplications {
        if let name = app.localizedName, name.lowercased() == appName.lowercased() {
            return Int(app.processIdentifier)
        }
        if let bundleName = app.bundleIdentifier?.split(separator: ".").last,
           bundleName.lowercased() == appName.lowercased() {
            return Int(app.processIdentifier)
        }
    }
    return nil
}

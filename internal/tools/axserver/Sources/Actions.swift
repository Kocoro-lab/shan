import ApplicationServices

func performClick(pid: Int, path: String, expectedRole: String?) -> (ActionResult?, ErrorInfo?) {
    guard let el = resolveElement(pid: pid, path: path) else {
        return (nil, ErrorInfo(code: -1, message: "Element not found at path '\(path)'. UI may have changed — call read_tree to refresh."))
    }
    let role = axString(el, "AXRole") ?? "unknown"
    if let expected = expectedRole, role != expected {
        return (nil, ErrorInfo(code: -1, message: "Expected \(expected) but found \(role) at path. UI may have changed — call read_tree to refresh."))
    }
    let title = axString(el, "AXTitle") ?? ""
    let err = AXUIElementPerformAction(el, "AXPress" as CFString)
    if err == .success {
        return (ActionResult(result: "pressed \(role) '\(title)'", role: role), nil)
    }
    return (nil, ErrorInfo(code: Int(err.rawValue), message: "AXPress failed on \(role) '\(title)' (error \(err.rawValue)). Action may not be supported."))
}

func setValue(pid: Int, path: String, value: String, expectedRole: String?) -> (ActionResult?, ErrorInfo?) {
    guard let el = resolveElement(pid: pid, path: path) else {
        return (nil, ErrorInfo(code: -1, message: "Element not found at path '\(path)'. UI may have changed — call read_tree to refresh."))
    }
    let role = axString(el, "AXRole") ?? "unknown"
    if let expected = expectedRole, role != expected {
        return (nil, ErrorInfo(code: -1, message: "Expected \(expected) but found \(role) at path. UI may have changed — call read_tree to refresh."))
    }
    AXUIElementSetAttributeValue(el, "AXFocused" as CFString, true as CFTypeRef)
    let err = AXUIElementSetAttributeValue(el, "AXValue" as CFString, value as CFTypeRef)
    if err == .success {
        return (ActionResult(result: "set value on \(role) to '\(value)'", role: role), nil)
    }
    return (nil, ErrorInfo(code: Int(err.rawValue), message: "set_value failed on \(role) (error \(err.rawValue)). Element may not be settable."))
}

func getValue(pid: Int, path: String) -> (ActionResult?, ErrorInfo?) {
    guard let el = resolveElement(pid: pid, path: path) else {
        return (nil, ErrorInfo(code: -1, message: "Element not found at path '\(path)'. UI may have changed — call read_tree to refresh."))
    }
    let role = axString(el, "AXRole") ?? "unknown"
    let val = axValue(el, "AXValue")
    let valStr = val != nil ? "\(val!)" : ""
    return (ActionResult(result: valStr, role: role), nil)
}

func performScroll(pid: Int, path: String?, dx: Int, dy: Int) -> (ActionResult?, ErrorInfo?) {
    if let path = path {
        // Verify the element exists before scrolling
        guard resolveElement(pid: pid, path: path) != nil else {
            return (nil, ErrorInfo(code: -1, message: "Element not found at path '\(path)'."))
        }
    }
    InputDriver.scroll(dx: dx, dy: dy)
    return (ActionResult(result: "scrolled dx=\(dx) dy=\(dy)"), nil)
}

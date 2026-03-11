import Foundation
import ApplicationServices
import AppKit

// Check accessibility permission once at startup.
guard AXIsProcessTrusted() else {
    let err = Response(id: 0, error: ErrorInfo(code: -1,
        message: "Accessibility permission not granted. Enable in: System Settings > Privacy & Security > Accessibility. Add your terminal app."))
    writeResponse(err)
    exit(1)
}

let encoder = JSONEncoder()
encoder.outputFormatting = [.sortedKeys]
let decoder = JSONDecoder()

// Persistent stdin read loop — one JSON request per line.
while let line = readLine(strippingNewline: true) {
    guard let data = line.data(using: .utf8) else { continue }

    guard let req = try? decoder.decode(Request.self, from: data) else {
        let resp = Response(id: 0, error: ErrorInfo(code: -1, message: "Invalid JSON request"))
        writeResponse(resp)
        continue
    }

    let params = req.params ?? Params(
        pid: nil, maxDepth: nil, semanticBudget: nil, filter: nil,
        path: nil, expectedRole: nil, value: nil, appName: nil,
        query: nil, role: nil, identifier: nil, type: nil,
        x: nil, y: nil, button: nil, clicks: nil,
        key: nil, modifiers: nil, dx: nil, dy: nil,
        windowTitle: nil, verify: nil, condition: nil,
        timeout: nil, interval: nil
    )

    let response = dispatch(id: req.id, method: req.method, params: params)
    writeResponse(response)
}

// MARK: - Dispatch

func dispatch(id: Int64, method: String, params: Params) -> Response {
    switch method {
    case "ping":
        return Response(id: id, result: AnyCodable(["ok": true]))

    case "read_tree":
        let pid = params.pid ?? frontmostPID()
        guard pid > 0 else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "Cannot determine frontmost application"))
        }
        let budget: Int
        if let sb = params.semanticBudget {
            budget = sb
        } else if let md = params.maxDepth {
            budget = md * 6 // backward compat heuristic
        } else {
            budget = 25
        }
        let filter = params.filter ?? "all"
        guard let result = readTree(pid: pid, budget: budget, filter: filter) else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "No windows found. Is the app running and visible?"))
        }
        return Response(id: id, result: AnyCodable(result))

    case "click", "press":
        guard let pid = params.pid, let path = params.path else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "\(method) requires 'pid' and 'path'"))
        }
        let (result, err) = performClick(pid: pid, path: path, expectedRole: params.expectedRole)
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "set_value":
        guard let pid = params.pid, let path = params.path, let value = params.value else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "set_value requires 'pid', 'path', and 'value'"))
        }
        let (result, err) = setValue(pid: pid, path: path, value: value, expectedRole: params.expectedRole)
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "get_value":
        guard let pid = params.pid, let path = params.path else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "get_value requires 'pid' and 'path'"))
        }
        let (result, err) = getValue(pid: pid, path: path)
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "find":
        let pid = params.pid ?? frontmostPID()
        guard pid > 0 else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "Cannot determine target app"))
        }
        let results = findElements(pid: pid, query: params.query, role: params.role, identifier: params.identifier)
        return Response(id: id, result: AnyCodable(results))

    case "resolve_pid":
        guard let appName = params.appName else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "resolve_pid requires 'app_name'"))
        }
        guard let pid = resolvePID(appName: appName) else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "App '\(appName)' not found or not running"))
        }
        return Response(id: id, result: AnyCodable(["pid": pid]))

    case "mouse_event":
        guard let type = params.type, let x = params.x, let y = params.y else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "mouse_event requires 'type', 'x', 'y'"))
        }
        let (result, err) = InputDriver.mouseEvent(
            type: type, x: x, y: y,
            button: params.button ?? "left",
            clicks: params.clicks ?? 1
        )
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "key_event":
        guard let key = params.key else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "key_event requires 'key'"))
        }
        let (result, err) = InputDriver.keyEvent(key: key, modifiers: params.modifiers ?? [])
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "type_text":
        guard let text = params.value else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "type_text requires 'value'"))
        }
        let (result, err) = InputDriver.typeText(text)
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "scroll":
        let dx = params.dx ?? 0
        let dy = params.dy ?? 0
        let pid = params.pid ?? frontmostPID()
        let (result, err) = performScroll(pid: pid, path: params.path, dx: dx, dy: dy)
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "focus":
        guard let appName = params.appName else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "focus requires 'app_name'"))
        }
        let (result, err) = FocusManager.focusApp(
            appName: appName,
            windowTitle: params.windowTitle,
            verify: params.verify ?? false
        )
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "frontmost":
        let (result, err) = FocusManager.frontmost()
        if let err = err { return Response(id: id, error: err) }
        return Response(id: id, result: AnyCodable(result!))

    case "list_windows":
        let pid = params.pid ?? frontmostPID()
        guard pid > 0 else {
            return Response(id: id, error: ErrorInfo(code: -1, message: "Cannot determine target app"))
        }
        let windows = FocusManager.listWindows(pid: pid)
        return Response(id: id, result: AnyCodable(windows))

    default:
        return Response(id: id, error: ErrorInfo(code: -1, message: "Unknown method: \(method)"))
    }
}

// MARK: - Helpers

func frontmostPID() -> Int {
    guard let app = NSWorkspace.shared.frontmostApplication else { return 0 }
    return Int(app.processIdentifier)
}

func writeResponse(_ resp: Response) {
    if let data = try? encoder.encode(resp),
       var str = String(data: data, encoding: .utf8) {
        str += "\n"
        FileHandle.standardOutput.write(str.data(using: .utf8)!)
    }
}

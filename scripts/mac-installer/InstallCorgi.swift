import AppKit

let installLine = "curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh"
let storeApp = "com.andriiklymiuk.corgi"

struct Step {
    var title: String
    var run: () -> (ok: Bool, note: String)
}

func shell(_ command: String) -> (ok: Bool, out: String) {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/bin/zsh")
    p.arguments = ["-lc", command]
    let pipe = Pipe()
    p.standardOutput = pipe
    p.standardError = pipe
    p.standardInput = FileHandle.nullDevice
    guard (try? p.run()) != nil else { return (false, "could not start a shell") }
    let out = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
    p.waitUntilExit()
    return (p.terminationStatus == 0, out)
}

func lastLine(_ text: String) -> String {
    text.split(separator: "\n").map(String.init).last { !$0.trimmingCharacters(in: .whitespaces).isEmpty } ?? ""
}

func findCorgi() -> String? {
    let home = NSHomeDirectory()
    let own = [home + "/.local/bin/corgi", home + "/.corgi/bin/corgi"]
    let fresh = ProcessInfo.processInfo.environment["CORGI_INSTALL_FRESH"] == "1"
    let known = (fresh ? [] : ["/opt/homebrew/bin/corgi", "/usr/local/bin/corgi"]) + own
    if let hit = known.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) { return hit }
    if fresh { return nil }
    let found = shell("command -v corgi").out.trimmingCharacters(in: .whitespacesAndNewlines)
    return found.hasPrefix("/") ? found : nil
}

func newestPairFile(since: Date) -> String? {
    let desktop = NSHomeDirectory() + "/Desktop"
    let files = (try? FileManager.default.contentsOfDirectory(atPath: desktop)) ?? []
    return files.filter { $0.hasSuffix(".corgipair") }.map { desktop + "/" + $0 }.filter {
        ((try? FileManager.default.attributesOfItem(atPath: $0)[.modificationDate]) as? Date ?? .distantPast) >= since
    }.first
}

func steps() -> [Step] {
    var corgi = ""
    let started = Date().addingTimeInterval(-2)
    let installOnly = ProcessInfo.processInfo.environment["CORGI_INSTALL_ONLY"] == "1"
    var list = [
        Step(title: "Get the corgi command line") {
            if let have = findCorgi() {
                corgi = have
                return (true, "already here: \(have)")
            }
            let r = shell(installLine)
            guard r.ok, let now = findCorgi() else { return (false, lastLine(r.out)) }
            corgi = now
            return (true, "installed: \(now)")
        },
    ]
    if installOnly { return list }
    list += [
        Step(title: "Start corgi, now and at login") {
            let r = shell("'\(corgi)' agent up --at-login")
            return (r.ok, r.ok ? "running" : lastLine(r.out))
        },
        Step(title: "Feed the board from Claude Code") {
            let r = shell("'\(corgi)' agent track enable")
            return (true, r.ok ? "hooks are in" : "skipped: " + lastLine(r.out))
        },
        Step(title: "Pair the Corgi app with it") {
            let r = shell("'\(corgi)' agent pair --file")
            guard r.ok, let file = newestPairFile(since: started) else { return (false, lastLine(r.out)) }
            let opened = shell("open -b \(storeApp) '\(file)' || open '\(file)'")
            return (opened.ok, opened.ok ? "the Corgi app asks you to confirm" : "open \(file) with the Corgi app")
        },
    ]
    return list
}

final class Installer: NSObject, NSApplicationDelegate {
    let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 460, height: 300), styleMask: [.titled, .closable], backing: .buffered, defer: false)
    let stack = NSStackView()
    let button = NSButton(title: "Close", target: nil, action: nil)

    func applicationDidFinishLaunching(_ notification: Notification) {
        window.title = "Install corgi"
        window.center()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.edgeInsets = NSEdgeInsets(top: 22, left: 24, bottom: 22, right: 24)
        window.contentView = stack
        let head = NSTextField(labelWithString: "Setting up corgi on this Mac")
        head.font = .systemFont(ofSize: 17, weight: .semibold)
        stack.addArrangedSubview(head)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        DispatchQueue.global(qos: .userInitiated).async { self.work() }
    }

    func line(_ text: String, color: NSColor = .labelColor, small: Bool = false) -> NSTextField {
        let label = NSTextField(wrappingLabelWithString: text)
        label.font = .systemFont(ofSize: small ? 12 : 14)
        label.textColor = color
        label.preferredMaxLayoutWidth = 410
        DispatchQueue.main.sync { stack.addArrangedSubview(label) }
        return label
    }

    func work() {
        var failed = false
        for step in steps() {
            let title = line("…  " + step.title)
            let result = step.run()
            DispatchQueue.main.sync { title.stringValue = (result.ok ? "✓  " : "✗  ") + step.title }
            _ = line(result.note, color: result.ok ? .secondaryLabelColor : .systemRed, small: true)
            if !result.ok {
                failed = true
                break
            }
        }
        _ = line(failed ? "Not finished. In Terminal: \(installLine) && corgi agent up" : "Done. You can close this window and put the installer in the Trash.", small: true)
        if ProcessInfo.processInfo.environment["CORGI_INSTALL_QUIT"] == "1" { exit(failed ? 1 : 0) }
        DispatchQueue.main.sync {
            button.target = NSApp
            button.action = #selector(NSApplication.terminate(_:))
            button.keyEquivalent = "\r"
            stack.addArrangedSubview(button)
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

let app = NSApplication.shared
let delegate = Installer()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()

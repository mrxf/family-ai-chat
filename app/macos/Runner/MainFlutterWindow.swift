import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow {
  override func awakeFromNib() {
    let flutterViewController = FlutterViewController()
    self.contentViewController = flutterViewController

    // Wide enough for the two-pane layout (sidebar + chat) on first launch;
    // narrower windows fall back to the phone layout, so keep a sane minimum.
    let initialSize = NSSize(width: 1180, height: 800)
    if let screen = NSScreen.main?.visibleFrame {
      let size = NSSize(width: min(initialSize.width, screen.width), height: min(initialSize.height, screen.height))
      let origin = NSPoint(x: screen.midX - size.width / 2, y: screen.midY - size.height / 2)
      self.setFrame(NSRect(origin: origin, size: size), display: true)
    }
    self.minSize = NSSize(width: 400, height: 560)
    self.title = "小暖"
    self.setFrameAutosaveName("XiaoNuanMainWindow")

    RegisterGeneratedPlugins(registry: flutterViewController)

    super.awakeFromNib()
  }
}

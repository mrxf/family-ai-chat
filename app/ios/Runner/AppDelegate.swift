import Flutter
import UIKit
import UserNotifications

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  private let reminderChannelName = "com.nuan.chat/reminders"
  private let reminderPrefix = "nuan_sched_"
  private var reminderChannel: FlutterMethodChannel?
  private var dartReady = false
  private var pendingOpenSchedules = false

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    UNUserNotificationCenter.current().delegate = self
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    let channel = FlutterMethodChannel(
      name: reminderChannelName,
      binaryMessenger: engineBridge.applicationRegistrar.messenger()
    )
    channel.setMethodCallHandler { [weak self] call, result in
      self?.handleReminderCall(call: call, result: result)
    }
    reminderChannel = channel
  }

  private func handleReminderCall(call: FlutterMethodCall, result: @escaping FlutterResult) {
    let center = UNUserNotificationCenter.current()
    switch call.method {
    case "takePendingOpenSchedules":
      dartReady = true
      let open = pendingOpenSchedules
      pendingOpenSchedules = false
      result(open)

    case "hasPermission":
      center.getNotificationSettings { settings in
        let ok = settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional
        DispatchQueue.main.async { result(ok) }
      }

    case "requestPermission":
      center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
        DispatchQueue.main.async { result(granted) }
      }

    case "openSettings":
      DispatchQueue.main.async {
        if let url = URL(string: UIApplication.openSettingsURLString) {
          UIApplication.shared.open(url, options: [:], completionHandler: nil)
        }
        result(nil)
      }

    case "syncReminders":
      let args = call.arguments as? [String: Any]
      let rawList = (args?["reminders"] as? [[String: Any]]) ?? []
      syncLocalNotifications(center: center, items: rawList) {
        DispatchQueue.main.async { result(nil) }
      }

    case "clearAll":
      clearLocalNotifications(center: center) {
        DispatchQueue.main.async { result(nil) }
      }

    default:
      result(FlutterMethodNotImplemented)
    }
  }

  private func clearLocalNotifications(center: UNUserNotificationCenter, completion: @escaping () -> Void) {
    center.getPendingNotificationRequests { [reminderPrefix] reqs in
      let ids = reqs.map(\.identifier).filter { $0.hasPrefix(reminderPrefix) }
      if !ids.isEmpty {
        center.removePendingNotificationRequests(withIdentifiers: ids)
      }
      completion()
    }
  }

  private func syncLocalNotifications(
    center: UNUserNotificationCenter,
    items: [[String: Any]],
    completion: @escaping () -> Void
  ) {
    clearLocalNotifications(center: center) { [reminderPrefix] in
      let nowMs = Date().timeIntervalSince1970 * 1000.0
      var count = 0
      for item in items {
        if count >= 48 { break }
        guard let fireNum = item["fire_at"] as? NSNumber else { continue }
        let fireMs = fireNum.doubleValue
        if fireMs <= nowMs + 1000 { continue }

        let rawId = (item["id"] as? String) ?? "\(count)"
        let title = ((item["title"] as? String) ?? "日程提醒").trimmingCharacters(in: .whitespacesAndNewlines)
        let body = ((item["body"] as? String) ?? "").trimmingCharacters(in: .whitespacesAndNewlines)

        let content = UNMutableNotificationContent()
        content.title = title.isEmpty ? "小暖日程提醒" : title
        content.body = body.isEmpty ? "您设定的日程时间到啦" : body
        content.sound = .default
        content.userInfo = ["open_schedules": true]

        let fireDate = Date(timeIntervalSince1970: fireMs / 1000.0)
        let comps = Calendar.current.dateComponents([.year, .month, .day, .hour, .minute, .second], from: fireDate)
        let trigger = UNCalendarNotificationTrigger(dateMatching: comps, repeats: false)
        let req = UNNotificationRequest(
          identifier: "\(reminderPrefix)\(rawId)",
          content: content,
          trigger: trigger
        )
        center.add(req, withCompletionHandler: nil)
        count += 1
      }
      completion()
    }
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    willPresent notification: UNNotification,
    withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
  ) {
    if #available(iOS 14.0, *) {
      completionHandler([.banner, .list, .sound])
    } else {
      completionHandler([.alert, .sound])
    }
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    didReceive response: UNNotificationResponse,
    withCompletionHandler completionHandler: @escaping () -> Void
  ) {
    if response.notification.request.content.userInfo["open_schedules"] as? Bool == true {
      if dartReady, let ch = reminderChannel {
        ch.invokeMethod("onOpenSchedules", arguments: nil)
      } else {
        pendingOpenSchedules = true
      }
    }
    completionHandler()
  }
}

import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models/models.dart';
import '../platform_capabilities.dart';

/// Rings schedule reminders as local OS notifications.
///
/// Phones implement it natively (Android AlarmManager / iOS
/// UNUserNotificationCenter). Elsewhere [UnsupportedReminderService] keeps
/// callers simple: schedules still sync with the server, nothing rings locally.
abstract interface class ReminderService {
  /// Called with [onOpenSchedules] when the user taps a reminder notification.
  void attach({required void Function() onOpenSchedules});

  /// True once if the app was cold-started by tapping a reminder.
  Future<bool> takePendingOpenSchedules();

  Future<bool> hasPermission();
  Future<bool> requestPermission();
  Future<void> openSettings();

  /// Replaces every scheduled local notification with [reminders].
  Future<void> syncReminders(List<ScheduleReminder> reminders);
  Future<void> clearAll();
}

final reminderServiceProvider = Provider<ReminderService>((ref) {
  final caps = ref.watch(platformCapabilitiesProvider);
  return caps.localReminders ? MethodChannelReminderService() : const UnsupportedReminderService();
});

class MethodChannelReminderService implements ReminderService {
  static const _channel = MethodChannel('com.nuan.chat/reminders');

  @override
  void attach({required void Function() onOpenSchedules}) {
    _channel.setMethodCallHandler((call) async {
      if (call.method == 'onOpenSchedules') onOpenSchedules();
    });
  }

  @override
  Future<bool> takePendingOpenSchedules() async {
    try {
      return await _channel.invokeMethod<bool>('takePendingOpenSchedules') == true;
    } catch (_) {
      return false;
    }
  }

  @override
  Future<bool> hasPermission() async {
    try {
      return await _channel.invokeMethod<bool>('hasPermission') ?? true;
    } catch (_) {
      // Missing plugin in widget tests: behave as if permission was granted.
      return true;
    }
  }

  @override
  Future<bool> requestPermission() async {
    try {
      return await _channel.invokeMethod<bool>('requestPermission') ?? false;
    } on MissingPluginException {
      return true;
    } catch (_) {
      return false;
    }
  }

  @override
  Future<void> openSettings() async {
    try {
      await _channel.invokeMethod<void>('openSettings');
    } catch (_) {}
  }

  @override
  Future<void> syncReminders(List<ScheduleReminder> reminders) async {
    try {
      await _channel.invokeMethod<void>('syncReminders', {
        'reminders': reminders.map((r) => r.toJson()).toList(growable: false),
      });
    } catch (_) {}
  }

  @override
  Future<void> clearAll() async {
    try {
      await _channel.invokeMethod<void>('clearAll');
    } catch (_) {}
  }
}

/// Devices without local notifications (desktop for now).
class UnsupportedReminderService implements ReminderService {
  const UnsupportedReminderService();

  @override
  void attach({required void Function() onOpenSchedules}) {}
  @override
  Future<bool> takePendingOpenSchedules() async => false;
  @override
  Future<bool> hasPermission() async => false;
  @override
  Future<bool> requestPermission() async => false;
  @override
  Future<void> openSettings() async {}
  @override
  Future<void> syncReminders(List<ScheduleReminder> reminders) async {}
  @override
  Future<void> clearAll() async {}
}

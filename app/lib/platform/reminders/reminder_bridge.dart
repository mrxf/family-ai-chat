import 'package:flutter/services.dart';

import '../../core/models/models.dart';

/// Native bridge for local phone schedule notifications (Android AlarmManager / iOS UNUserNotificationCenter).
class ReminderBridge {
  ReminderBridge._();
  static final ReminderBridge instance = ReminderBridge._();

  static const _channel = MethodChannel('com.nuan.chat/reminders');
  void Function()? _onOpenSchedules;

  void attach({required void Function() onOpenSchedules}) {
    _onOpenSchedules = onOpenSchedules;
    _channel.setMethodCallHandler((call) async {
      if (call.method == 'onOpenSchedules') {
        _onOpenSchedules?.call();
      }
    });
  }

  Future<bool> takePendingOpenSchedules() async {
    try {
      final v = await _channel.invokeMethod<bool>('takePendingOpenSchedules');
      return v == true;
    } on MissingPluginException {
      return false;
    } catch (_) {
      return false;
    }
  }

  Future<bool> hasPermission() async {
    try {
      final v = await _channel.invokeMethod<bool>('hasPermission');
      return v ?? true;
    } on MissingPluginException {
      return true;
    } catch (_) {
      return true;
    }
  }

  Future<bool> requestPermission() async {
    try {
      final v = await _channel.invokeMethod<bool>('requestPermission');
      return v ?? false;
    } on MissingPluginException {
      return true;
    } catch (_) {
      return false;
    }
  }

  Future<void> openSettings() async {
    try {
      await _channel.invokeMethod<void>('openSettings');
    } catch (_) {}
  }

  Future<void> syncReminders(List<ScheduleReminder> reminders) async {
    try {
      await _channel.invokeMethod<void>('syncReminders', {
        'reminders': reminders.map((r) => r.toJson()).toList(growable: false),
      });
    } on MissingPluginException {
      // Widget tests or desktop platforms without native alarm bridge.
    } catch (_) {}
  }

  Future<void> clearAll() async {
    try {
      await _channel.invokeMethod<void>('clearAll');
    } on MissingPluginException {
      // Ignore in test environments.
    } catch (_) {}
  }
}

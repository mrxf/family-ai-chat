import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models/models.dart';
import '../platform/reminders/reminder_bridge.dart';
import 'session.dart';

/// Loads the user's schedules and automatically keeps phone local notifications in sync.
final schedulesProvider = FutureProvider<SchedulesResult>((ref) async {
  final api = ref.watch(sessionProvider.select((s) => s.api));
  if (api == null) {
    unawaited(ReminderBridge.instance.clearAll());
    return const SchedulesResult();
  }
  final res = await api.schedules();
  unawaited(ReminderBridge.instance.syncReminders(res.upcomingReminders));
  return res;
});

final notificationPermissionProvider = FutureProvider.autoDispose<bool>((ref) {
  return ReminderBridge.instance.hasPermission();
});

bool _askedPermissionThisSession = false;

/// Called when an AI chat turn invokes schedule tools: refreshes schedules,
/// re-syncs local alarms, and prompts once for OS notification permission if needed.
Future<void> syncSchedulesAfterChat(Ref ref) async {
  try {
    ref.invalidate(schedulesProvider);
    final res = await ref.read(schedulesProvider.future);
    if (res.upcomingReminders.isNotEmpty && !_askedPermissionThisSession) {
      final hasPerm = await ReminderBridge.instance.hasPermission();
      if (!hasPerm) {
        _askedPermissionThisSession = true;
        final granted = await ReminderBridge.instance.requestPermission();
        if (granted) {
          await ReminderBridge.instance.syncReminders(res.upcomingReminders);
        }
        ref.invalidate(notificationPermissionProvider);
      }
    }
  } catch (_) {}
}

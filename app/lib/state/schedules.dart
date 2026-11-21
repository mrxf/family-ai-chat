import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models/models.dart';
import '../platform/platform_capabilities.dart';
import '../platform/reminders/reminder_service.dart';
import 'session.dart';

/// Loads the user's schedules and automatically keeps local notifications in sync.
final schedulesProvider = FutureProvider<SchedulesResult>((ref) async {
  final api = ref.watch(sessionProvider.select((s) => s.api));
  final reminders = ref.watch(reminderServiceProvider);
  if (api == null) {
    unawaited(reminders.clearAll());
    return const SchedulesResult();
  }
  final res = await api.schedules();
  unawaited(reminders.syncReminders(res.upcomingReminders));
  return res;
});

final notificationPermissionProvider = FutureProvider.autoDispose<bool>((ref) {
  return ref.watch(reminderServiceProvider).hasPermission();
});

bool _askedPermissionThisSession = false;

/// Called when an AI chat turn invokes schedule tools: refreshes schedules,
/// re-syncs local alarms, and prompts once for OS notification permission if needed.
Future<void> syncSchedulesAfterChat(Ref ref) async {
  try {
    ref.invalidate(schedulesProvider);
    final res = await ref.read(schedulesProvider.future);
    final reminders = ref.read(reminderServiceProvider);
    if (res.upcomingReminders.isNotEmpty && !_askedPermissionThisSession && ref.read(platformCapabilitiesProvider).localReminders) {
      final hasPerm = await reminders.hasPermission();
      if (!hasPerm) {
        _askedPermissionThisSession = true;
        final granted = await reminders.requestPermission();
        if (granted) {
          await reminders.syncReminders(res.upcomingReminders);
        }
        ref.invalidate(notificationPermissionProvider);
      }
    }
  } catch (_) {}
}

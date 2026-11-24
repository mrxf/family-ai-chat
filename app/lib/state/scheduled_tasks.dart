import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models/models.dart';
import 'session.dart';

/// Loads the user's background AI scheduled tasks.
final scheduledTasksProvider = FutureProvider<ScheduledTasksResult>((ref) async {
  final api = ref.watch(sessionProvider.select((s) => s.api));
  if (api == null) return const ScheduledTasksResult();
  return api.scheduledTasks();
});

/// Called when an AI chat turn creates/updates/runs automation tasks.
Future<void> syncScheduledTasksAfterChat(Ref ref) async {
  try {
    ref.invalidate(scheduledTasksProvider);
    await ref.read(scheduledTasksProvider.future);
  } catch (_) {}
}

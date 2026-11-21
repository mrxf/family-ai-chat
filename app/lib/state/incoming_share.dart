import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../platform/share_intake/share_intake.dart';

export '../platform/share_intake/share_intake.dart' show SharedPayload;

/// The latest share waiting to be put into the composer (null when none).
class IncomingShareNotifier extends Notifier<SharedPayload?> {
  @override
  SharedPayload? build() {
    final intake = ref.watch(shareIntakeProvider);
    intake.listen((payload) => state = payload);
    ref.onDispose(intake.cancel);
    unawaited(_pullInitial(intake));
    return null;
  }

  Future<void> _pullInitial(ShareIntake intake) async {
    final payload = await intake.takePending();
    if (payload != null && ref.mounted) state = payload;
  }

  /// Atomically takes and clears the pending share payload so it is consumed once.
  SharedPayload? take() {
    final current = state;
    if (current != null) state = null;
    return current;
  }

  @visibleForTesting
  void push(SharedPayload payload) {
    if (!payload.isEmpty) state = payload;
  }
}

final incomingShareProvider = NotifierProvider<IncomingShareNotifier, SharedPayload?>(IncomingShareNotifier.new);

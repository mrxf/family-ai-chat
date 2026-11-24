import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models/app_settings_steps.dart';
import '../core/models/models.dart';
import 'chat.dart';
import 'pinned_components.dart';
import 'session.dart';

/// Serializes device actions and keeps durable receipts so history replay cannot apply them twice.
class AppActionExecutor {
  AppActionExecutor({
    required this.deviceId,
    required this.readValues,
    required this.applyValues,
    required this.readReceipts,
    required this.saveReceipts,
    required this.writeState,
  });

  final String deviceId;
  final Map<String, dynamic> Function() readValues;
  final Future<void> Function(Map<String, dynamic>) applyValues;
  final List<Map<String, dynamic>> Function() readReceipts;
  final Future<void> Function(List<Map<String, dynamic>>) saveReceipts;
  final Future<void> Function(String, String, Map<String, dynamic>) writeState;
  Future<void> _queue = Future.value();

  Future<void> _serial(Future<void> Function() operation) {
    final next = _queue.then((_) => operation());
    _queue = next.catchError((Object _) {});
    return next;
  }

  Map<String, dynamic> _allowed(String action, Map<String, dynamic> patch, {bool restoring = false}) {
    if (action == 'toggle_suggestions') return {if (patch['visible'] is bool) 'visible': patch['visible']};
    if (action != 'set_settings') return {};
    final scale = patch['font_scale'];
    final theme = patch['theme'];
    return {
      if (scale is num && scale.isFinite && (fontScaleSteps.contains(scale.toDouble()) || (restoring && scale >= 0.5 && scale <= 3)))
        'font_scale': scale.toDouble(),
      if (const ['system', 'light', 'dark'].contains(theme)) 'theme': theme,
    };
  }

  Future<void> _save(List<Map<String, dynamic>> receipts, Map<String, dynamic> receipt) async {
    receipts.removeWhere((r) => r['id'] == receipt['id']);
    receipts.add(receipt);
    if (receipts.length > 200) receipts.removeRange(0, receipts.length - 200);
    await saveReceipts(receipts);
  }

  /// Executes only pending actions addressed to this installation; retries acknowledgements from durable receipts.
  Future<void> receive(String messageId, MessageComponent component) => _serial(() async {
    if (component.kind != 'app_action' || component.data['target_device_id'] != deviceId || component.state['status'] != 'pending') return;
    final action = '${component.data['action']}';
    final raw = component.data['patch'];
    final patch = _allowed(action, raw is Map ? raw.cast<String, dynamic>() : {});
    final receipts = readReceipts();
    Map<String, dynamic>? receipt;
    for (final r in receipts) {
      if (r['id'] == component.id) receipt = r;
    }
    if (receipt != null) {
      if (receipt['phase'] == 'prepared') {
        await applyValues((receipt['patch'] as Map).cast<String, dynamic>());
        receipt['phase'] = 'applied';
        await _save(receipts, receipt);
      }
      await writeState(messageId, component.id, (receipt['state'] as Map).cast<String, dynamic>());
      return;
    }
    if (patch.isEmpty) {
      await writeState(messageId, component.id, {'status': 'unsupported', 'applied_device_id': deviceId});
      return;
    }
    final current = readValues();
    final previous = {for (final key in patch.keys) key: current[key]};
    final state = <String, dynamic>{
      'status': 'applied',
      'applied_device_id': deviceId,
      'previous': previous,
      'applied_at': DateTime.now().millisecondsSinceEpoch,
    };
    receipt = {'id': component.id, 'phase': 'prepared', 'patch': patch, 'state': state};
    // Persist the before-image first: a crash can safely finish an absolute patch without losing undo.
    await _save(receipts, receipt);
    await applyValues(patch);
    receipt['phase'] = 'applied';
    await _save(receipts, receipt);
    await writeState(messageId, component.id, state);
  });

  /// Restores the saved before-image on the original device and makes repeated undo harmless.
  Future<void> undo(String messageId, MessageComponent component) => _serial(() async {
    if (component.data['target_device_id'] != deviceId) return;
    final receipts = readReceipts();
    Map<String, dynamic>? receipt;
    for (final r in receipts) {
      if (r['id'] == component.id) receipt = r;
    }
    final rawState = receipt?['state'];
    final state = rawState is Map ? rawState.cast<String, dynamic>() : component.state;
    if (state['applied_device_id'] != deviceId) return;
    if (state['status'] == 'undone') {
      await writeState(messageId, component.id, state);
      return;
    }
    if (state['status'] != 'applied') return;
    final previous = state['previous'];
    if (previous is! Map) return;
    final patch = _allowed('${component.data['action']}', previous.cast<String, dynamic>(), restoring: true);
    if (patch.isEmpty) return;
    final nextState = {...state, 'status': 'undone'};
    // Undo uses the same recovery journal, including when the PATCH acknowledgement fails.
    receipt = {'id': component.id, 'phase': 'prepared', 'patch': patch, 'state': nextState};
    await _save(receipts, receipt);
    await applyValues(patch);
    receipt['phase'] = 'applied';
    await _save(receipts, receipt);
    await writeState(messageId, component.id, nextState);
  });
}

/// Provides the executor with the existing preference and component-state APIs.
final appActionsProvider = Provider<AppActionExecutor>((ref) {
  final storage = ref.read(storageProvider);
  return AppActionExecutor(
    deviceId: ref.read(deviceIdProvider),
    readValues: () {
      final settings = ref.read(settingsProvider);
      return {'font_scale': settings.fontScale, 'theme': settings.themeMode.name, 'visible': ref.read(pinnedComponentsProvider).suggestionsPinned};
    },
    applyValues: (patch) async {
      final settings = ref.read(settingsProvider);
      if (patch.containsKey('font_scale') || patch.containsKey('theme')) {
        ref
            .read(settingsProvider.notifier)
            .update(
              settings.copyWith(
                fontScale: (patch['font_scale'] as num?)?.toDouble(),
                themeMode: patch['theme'] == null ? null : ThemeMode.values.byName(patch['theme'] as String),
              ),
            );
        // Wait for the preference writes before marking the journal complete.
        await storage.prefs.setDouble('fontScale', ref.read(settingsProvider).fontScale);
        await storage.prefs.setInt('themeMode', ref.read(settingsProvider).themeMode.index);
      }
      if (patch['visible'] is bool) {
        ref.read(pinnedComponentsProvider.notifier).setSuggestionsPinned(patch['visible'] as bool);
        await storage.writeCache('home_suggestions_pinned', patch['visible']);
      }
    },
    readReceipts: () {
      final raw = storage.prefs.getString('applied_app_actions');
      if (raw == null) return [];
      try {
        return (jsonDecode(raw) as List).map((r) => (r as Map).cast<String, dynamic>()).toList();
      } catch (_) {
        return [];
      }
    },
    saveReceipts: (receipts) async {
      if (!await storage.prefs.setString('applied_app_actions', jsonEncode(receipts))) throw StateError('无法保存操作记录');
    },
    writeState: (messageId, componentId, state) async {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      await api.updateMessageComponentState(messageId, componentId, state);
      if (ref.mounted) ref.read(chatProvider.notifier).syncComponentStateFromPin(messageId, componentId, state);
    },
  );
});

/// Forwards streamed snapshots and loaded history without adding business logic to chat.dart.
void bindAppActions(Ref ref, ValueListenable<LiveText> live, ChatState Function() current, void Function(void Function(ChatState?, ChatState)) subscribe) {
  void dispatch(String messageId, Iterable<MessageComponent> components) {
    if (messageId.isEmpty || messageId.startsWith('local-')) return;
    for (final component in components) {
      if (component.kind == 'app_action' && component.state['status'] == 'pending') {
        unawaited(ref.read(appActionsProvider).receive(messageId, component).catchError((Object _) {}));
      }
    }
  }

  void onLive() {
    final chat = current();
    if (!chat.isPrivate) dispatch(chat.activeId ?? '', live.value.components);
  }

  live.addListener(onLive);
  ref.onDispose(() => live.removeListener(onLive));
  subscribe((_, next) {
    if (!next.isPrivate) {
      for (final message in next.messages) {
        dispatch(message.id, message.components);
      }
    }
  });
}

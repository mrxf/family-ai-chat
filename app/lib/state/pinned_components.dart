import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import 'chat.dart';
import 'session.dart';

@immutable
class PinnedComponentsState {
  final List<PinnedComponent> items;
  final bool loaded;

  /// Built-in "猜你想问" recommendation widget card is pinned by default on the home board.
  final bool suggestionsPinned;

  const PinnedComponentsState({
    this.items = const [],
    this.loaded = false,
    this.suggestionsPinned = true,
  });

  bool isPinned(String messageId, String componentId) =>
      items.any((p) => p.messageId == messageId && p.componentId == componentId);

  PinnedComponentsState copyWith({
    List<PinnedComponent>? items,
    bool? loaded,
    bool? suggestionsPinned,
  }) =>
      PinnedComponentsState(
        items: items ?? this.items,
        loaded: loaded ?? this.loaded,
        suggestionsPinned: suggestionsPinned ?? this.suggestionsPinned,
      );
}

/// Manages components pinned to the home board and keeps their mutable state
/// synchronized with both the server and any open conversation.
class PinnedComponentsNotifier extends Notifier<PinnedComponentsState> {
  static const _cacheKey = 'pinned_components';
  static const _suggestionsPinKey = 'home_suggestions_pinned';

  @override
  PinnedComponentsState build() {
    ref.listen(sessionProvider.select((s) => s.api), (prev, next) {
      if (identical(prev, next)) return;
      state = PinnedComponentsState(suggestionsPinned: state.suggestionsPinned);
      if (next != null) unawaited(refresh());
    });
    Future.microtask(_init);
    return const PinnedComponentsState();
  }

  Api? get _api => ref.read(sessionProvider).api;

  Future<void> _init() async {
    final storage = ref.read(storageProvider);
    final cached = await storage.readCache(_cacheKey);
    final sugPref = await storage.readCache(_suggestionsPinKey);
    if (!ref.mounted) return;
    final sugPinned = sugPref is bool ? sugPref : true;
    if (cached is List && !state.loaded) {
      final list = cached.map((e) => PinnedComponent.fromJson((e as Map).cast())).toList();
      state = state.copyWith(items: list, suggestionsPinned: sugPinned);
    } else {
      state = state.copyWith(suggestionsPinned: sugPinned);
    }
    await refresh();
  }

  Future<void> refresh() async {
    if (!ref.mounted) return;
    final api = _api;
    if (api == null) return;
    try {
      final items = await api.pinnedComponents();
      if (!ref.mounted || !identical(_api, api)) return;
      state = state.copyWith(items: items, loaded: true);
      _persist();
    } catch (_) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(loaded: true);
      }
    }
  }

  void _persist() {
    ref.read(storageProvider).writeCache(_cacheKey, state.items.map((p) => p.toJson()).toList());
  }

  void setSuggestionsPinned(bool pinned) {
    state = state.copyWith(suggestionsPinned: pinned);
    ref.read(storageProvider).writeCache(_suggestionsPinKey, pinned);
  }

  /// Pins a component to the home board. Returns null on success or an error message.
  Future<String?> pin({
    required String messageId,
    required MessageComponent component,
    String conversationId = '',
    String conversationTitle = '',
  }) async {
    final api = _api;
    if (api == null) return '尚未登录';
    if (state.isPinned(messageId, component.id)) return null;

    final before = state.items;
    final optimistic = PinnedComponent(
      id: 'local-pin-$messageId-${component.id}',
      conversationId: conversationId,
      conversationTitle: conversationTitle,
      messageId: messageId,
      componentId: component.id,
      pinnedAt: DateTime.now().millisecondsSinceEpoch,
      component: component,
    );
    state = state.copyWith(items: [optimistic, ...before]);
    _persist();

    try {
      final saved = await api.pinComponent(messageId, component.id);
      if (!ref.mounted || !identical(_api, api)) return null;
      state = state.copyWith(items: [
        saved,
        for (final p in state.items)
          if (!(p.messageId == messageId && p.componentId == component.id)) p,
      ]);
      _persist();
      return null;
    } catch (e) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(items: before);
        _persist();
      }
      return ApiError.from(e).message;
    }
  }

  /// Unpins a component from the home board. Returns null on success or an error message.
  Future<String?> unpin(String messageId, String componentId) async {
    final api = _api;
    if (api == null) return '尚未登录';
    final before = state.items;
    state = state.copyWith(
      items: before.where((p) => !(p.messageId == messageId && p.componentId == componentId)).toList(),
    );
    _persist();
    try {
      await api.unpinComponent(messageId, componentId);
      return null;
    } catch (e) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(items: before);
        _persist();
      }
      return ApiError.from(e).message;
    }
  }

  /// Updates a pinned component's mutable state from the home board, propagating
  /// the update to the open conversation (if any) and persisting to the server.
  Future<void> updateComponentState(
    String messageId,
    String componentId,
    Map<String, dynamic> nextState,
  ) async {
    final before = state.items;
    syncComponentStateFromChat(messageId, componentId, nextState);
    ref.read(chatProvider.notifier).syncComponentStateFromPin(messageId, componentId, nextState);
    final api = _api;
    if (api == null) return;
    try {
      await api.updateMessageComponentState(messageId, componentId, nextState);
    } catch (_) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(items: before);
        _persist();
      }
    }
  }

  /// Updates a pinned component's mutable content data, syncing with the server
  /// and any open conversation. Returns null on success or an error message.
  Future<String?> updateComponentData(
    String messageId,
    String componentId,
    Map<String, dynamic> nextData,
  ) async {
    final api = _api;
    if (api == null) return '尚未登录';
    final before = state.items;
    syncComponentDataFromChat(messageId, componentId, nextData);
    ref.read(chatProvider.notifier).syncComponentDataFromPin(messageId, componentId, nextData);
    try {
      final saved = await api.updateMessageComponent(messageId, componentId, data: nextData);
      if (!ref.mounted || !identical(_api, api)) return null;
      syncComponentDataFromChat(messageId, componentId, saved.data);
      ref.read(chatProvider.notifier).syncComponentDataFromPin(messageId, componentId, saved.data);
      return null;
    } catch (e) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(items: before);
        _persist();
      }
      return ApiError.from(e).message;
    }
  }

  /// Permanently deletes a component and removes it from both Home and open chat.
  Future<String?> deleteComponent(String messageId, String componentId) async {
    final api = _api;
    if (api == null) return '尚未登录';
    final before = state.items;
    state = state.copyWith(
      items: before.where((p) => !(p.messageId == messageId && p.componentId == componentId)).toList(),
    );
    _persist();
    ref.read(chatProvider.notifier).removeComponentLocal(messageId, componentId);
    try {
      await api.deleteMessageComponent(messageId, componentId);
      return null;
    } catch (e) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(items: before);
        _persist();
      }
      return ApiError.from(e).message;
    }
  }

  /// Moves a pinned component left/up (-1) or right/down (+1) on the home board.
  Future<String?> movePin(String pinId, int delta) async {
    final api = _api;
    if (api == null) return '尚未登录';
    final idx = state.items.indexWhere((p) => p.id == pinId);
    final nextIdx = idx + delta;
    if (idx < 0 || nextIdx < 0 || nextIdx >= state.items.length) return null;
    final before = state.items;
    final reordered = [...before];
    final item = reordered.removeAt(idx);
    reordered.insert(nextIdx, item);
    state = state.copyWith(items: reordered);
    _persist();
    try {
      final saved = await api.reorderPinnedComponents([for (final p in reordered) p.id]);
      if (!ref.mounted || !identical(_api, api)) return null;
      state = state.copyWith(items: saved);
      _persist();
      return null;
    } catch (e) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(items: before);
        _persist();
      }
      return ApiError.from(e).message;
    }
  }

  /// Reflects an in-chat component data edit on the home board immediately.
  void syncComponentDataFromChat(
    String messageId,
    String componentId,
    Map<String, dynamic> nextData,
  ) {
    var changed = false;
    final next = [
      for (final p in state.items)
        if (p.messageId == messageId && p.componentId == componentId)
          () {
            changed = true;
            return p.copyWith(component: p.component.copyWith(data: nextData));
          }()
        else
          p,
    ];
    if (!changed) return;
    state = state.copyWith(items: next);
    _persist();
  }

  /// Reflects an in-chat component state edit on the home board immediately.
  void syncComponentStateFromChat(
    String messageId,
    String componentId,
    Map<String, dynamic> nextState,
  ) {
    var changed = false;
    final next = [
      for (final p in state.items)
        if (p.messageId == messageId && p.componentId == componentId)
          () {
            changed = true;
            return p.copyWith(component: p.component.copyWith(state: nextState));
          }()
        else
          p,
    ];
    if (!changed) return;
    state = state.copyWith(items: next);
    _persist();
  }

  /// Removes all pinned components belonging to a deleted conversation immediately.
  void removeByConversation(String conversationId) {
    final next = state.items.where((p) => p.conversationId != conversationId).toList();
    if (next.length == state.items.length) return;
    state = state.copyWith(items: next);
    _persist();
  }
}

final pinnedComponentsProvider =
    NotifierProvider<PinnedComponentsNotifier, PinnedComponentsState>(PinnedComponentsNotifier.new);

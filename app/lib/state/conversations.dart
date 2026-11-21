import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import 'session.dart';

class ConvListState {
  final List<Conversation> items;
  final bool loading, hasMore, loaded;
  const ConvListState({this.items = const [], this.loading = false, this.hasMore = true, this.loaded = false});
  ConvListState copyWith({List<Conversation>? items, bool? loading, bool? hasMore, bool? loaded}) =>
      ConvListState(items: items ?? this.items, loading: loading ?? this.loading, hasMore: hasMore ?? this.hasMore, loaded: loaded ?? this.loaded);
}

class ConversationsNotifier extends Notifier<ConvListState> {
  static const _page = 50;

  /// Server-side pagination cursor. Kept separately from the displayed list,
  /// whose updatedAt values change locally.
  int? _nextBefore;

  @override
  ConvListState build() {
    ref.listen(sessionProvider.select((s) => s.api), (prev, next) {
      if (identical(prev, next)) return;
      _nextBefore = null;
      state = const ConvListState();
      if (next != null) unawaited(refresh());
    });
    Future.microtask(_init);
    return const ConvListState();
  }

  Future<void> _init() async {
    final cached = await ref.read(storageProvider).readCache('conversations');
    if (!ref.mounted) return;
    if (cached is List && !state.loaded) {
      state = state.copyWith(items: cached.map((e) => Conversation.fromJson((e as Map).cast())).toList());
    }
    await refresh();
  }

  Api? get _api => ref.read(sessionProvider).api;

  Future<void> refresh() async {
    if (!ref.mounted) return;
    final api = _api;
    if (api == null) return;
    state = state.copyWith(loading: true);
    try {
      final items = await api.conversations(limit: _page);
      if (!ref.mounted || !identical(_api, api)) return;
      _nextBefore = items.isEmpty ? null : items.last.updatedAt;
      state = ConvListState(items: items, hasMore: items.length >= _page, loaded: true);
      _persist();
    } catch (_) {
      if (ref.mounted && identical(_api, api)) state = state.copyWith(loading: false);
    }
  }

  Future<void> loadMore() async {
    final api = _api;
    final before = _nextBefore;
    if (api == null || before == null || state.loading || !state.hasMore) return;
    state = state.copyWith(loading: true);
    try {
      final more = await api.conversations(before: before, limit: _page);
      if (!identical(_api, api)) return;
      if (more.isNotEmpty) _nextBefore = more.last.updatedAt;
      final seen = state.items.map((c) => c.id).toSet();
      state = state.copyWith(items: [...state.items, ...more.where((c) => seen.add(c.id))], hasMore: more.length >= _page, loading: false);
    } catch (_) {
      if (identical(_api, api)) state = state.copyWith(loading: false);
    }
  }

  void _persist() {
    ref.read(storageProvider).writeCache('conversations', state.items.take(80).map((c) => c.toJson()).toList());
  }

  /// Inserts or moves a conversation to the top (after a message is sent).
  void upsertTop(Conversation c) {
    final rest = state.items.where((x) => x.id != c.id);
    state = state.copyWith(items: [c, ...rest]);
    _persist();
  }

  /// Marks a conversation as just active and moves it to the top.
  void touch(String id) {
    final c = state.items.where((x) => x.id == id).firstOrNull;
    if (c != null) upsertTop(c.copyWith(updatedAt: DateTime.now().millisecondsSinceEpoch));
  }

  void patchLocal(String id, Conversation Function(Conversation) f) {
    state = state.copyWith(items: [for (final c in state.items) c.id == id ? f(c) : c]);
    _persist();
  }

  Future<void> rename(String id, String title) async {
    patchLocal(id, (c) => c.copyWith(title: title));
    await _api?.updateConversation(id, {'title': title});
  }

  Future<void> togglePin(Conversation c) async {
    patchLocal(c.id, (x) => x.copyWith(pinned: !c.pinned));
    await _api?.updateConversation(c.id, {'pinned': !c.pinned});
  }

  Future<void> delete(String id) async {
    final before = state.items;
    state = state.copyWith(items: before.where((c) => c.id != id).toList());
    _persist();
    ref.read(storageProvider).deleteCache('conv_$id');
    try {
      await _api?.deleteConversation(id);
    } catch (e) {
      state = state.copyWith(items: before);
      rethrow;
    }
  }
}

final conversationsProvider = NotifierProvider<ConversationsNotifier, ConvListState>(ConversationsNotifier.new);

/// id → title, for widgets that only care about titles.
final conversationsTitlesProvider = Provider<Map<String, String>>((ref) {
  final items = ref.watch(conversationsProvider.select((s) => s.items));
  return {for (final c in items) c.id: c.title};
});

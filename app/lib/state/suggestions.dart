import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import 'session.dart';

@immutable
class HomeSuggestionsState {
  final List<HomeSuggestion> items;
  final bool loaded;

  const HomeSuggestionsState({
    this.items = const [],
    this.loaded = false,
  });

  bool get isPersonalized => items.isNotEmpty;

  HomeSuggestionsState copyWith({
    List<HomeSuggestion>? items,
    bool? loaded,
  }) =>
      HomeSuggestionsState(
        items: items ?? this.items,
        loaded: loaded ?? this.loaded,
      );
}

/// Manages the personalized home suggestion pool returned by `GET /api/suggestions`.
///
/// The server applies a 6-hour stale-while-revalidate cooldown and refreshes in the
/// background when enough new non-private conversations exist, so calling [refresh]
/// on app launch or resume is cheap and non-blocking.
class HomeSuggestionsNotifier extends Notifier<HomeSuggestionsState> {
  static const _cacheKey = 'home_suggestions';

  @override
  HomeSuggestionsState build() {
    ref.listen(sessionProvider.select((s) => s.api), (prev, next) {
      if (identical(prev, next)) return;
      state = const HomeSuggestionsState();
      if (next != null) unawaited(refresh());
    });
    Future.microtask(_init);
    return const HomeSuggestionsState();
  }

  Api? get _api => ref.read(sessionProvider).api;

  Future<void> _init() async {
    final cached = await ref.read(storageProvider).readCache(_cacheKey);
    if (!ref.mounted) return;
    if (cached is List && !state.loaded) {
      final list = cached
          .whereType<Map>()
          .map((e) => HomeSuggestion.fromJson(e.cast()))
          .where((s) => s.text.isNotEmpty)
          .toList();
      if (list.isNotEmpty) {
        state = state.copyWith(items: list);
      }
    }
    await refresh();
  }

  Future<void> refresh() async {
    if (!ref.mounted) return;
    final api = _api;
    if (api == null) return;
    try {
      final items = await api.homeSuggestions();
      if (!ref.mounted || !identical(_api, api)) return;
      state = state.copyWith(items: items, loaded: true);
      ref.read(storageProvider).writeCache(_cacheKey, items.map((e) => e.toJson()).toList());
    } catch (_) {
      if (ref.mounted && identical(_api, api)) {
        state = state.copyWith(loaded: true);
      }
    }
  }
}

final homeSuggestionsProvider =
    NotifierProvider<HomeSuggestionsNotifier, HomeSuggestionsState>(HomeSuggestionsNotifier.new);

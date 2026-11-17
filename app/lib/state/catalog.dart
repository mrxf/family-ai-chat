import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/admin_models.dart';
import 'session.dart';

/// Everything the admin "models & providers" screens show, loaded together.
class Catalog {
  final List<AdminProvider> providers;
  final List<AdminModel> models;
  final ModelSettings settings;
  const Catalog({required this.providers, required this.models, required this.settings});

  AdminProvider? provider(String id) {
    for (final p in providers) {
      if (p.id == id) return p;
    }
    return null;
  }

  AdminModel? model(String id) {
    for (final m in models) {
      if (m.id == id) return m;
    }
    return null;
  }

  List<AdminModel> modelsOf(String providerId) => models.where((m) => m.providerId == providerId).toList();

  /// Enabled model whose provider is enabled too, i.e. visible to the family.
  bool usable(AdminModel m) => m.enabled && (provider(m.providerId)?.enabled ?? false);

  List<AdminModel> get usableModels => models.where(usable).toList();

  Catalog copyWith({List<AdminProvider>? providers, List<AdminModel>? models, ModelSettings? settings}) =>
      Catalog(providers: providers ?? this.providers, models: models ?? this.models, settings: settings ?? this.settings);
}

class CatalogNotifier extends AsyncNotifier<Catalog> {
  @override
  Future<Catalog> build() => _load();

  Future<Catalog> _load() async {
    final api = ref.read(apiProvider);
    final (providers, models, settings) = await (api.adminProviders(), api.adminModels(), api.adminModelSettings()).wait;
    return Catalog(providers: providers, models: models, settings: settings);
  }

  /// Reloads after a change and refreshes the session so the admin's own
  /// model list (composer, pickers) matches what the family now sees.
  Future<void> reload() async {
    final session = ref.read(sessionProvider.notifier);
    try {
      final c = await _load();
      if (ref.mounted) state = AsyncData(c);
    } catch (e, st) {
      if (ref.mounted && !state.hasValue) state = AsyncError(e, st);
    }
    unawaited(session.refresh());
  }

  /// Optimistic local reorder, then persist.
  Future<void> reorder(List<String> ids) async {
    final c = state.value;
    if (c == null) return;
    final byId = {for (final m in c.models) m.id: m};
    final rest = c.models.where((m) => !ids.contains(m.id));
    state = AsyncData(c.copyWith(models: [for (final id in ids) ?byId[id], ...rest]));
    try {
      await ref.read(apiProvider).adminReorderModels(ids);
    } finally {
      await reload();
    }
  }

  void setSettings(ModelSettings s) {
    final c = state.value;
    if (c != null) state = AsyncData(c.copyWith(settings: s));
  }
}

final catalogProvider = AsyncNotifierProvider.autoDispose<CatalogNotifier, Catalog>(CatalogNotifier.new);

final presetsProvider = FutureProvider.autoDispose((ref) => ref.watch(apiProvider).adminPresets());

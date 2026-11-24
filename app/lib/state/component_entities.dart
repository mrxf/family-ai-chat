import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import 'pinned_components.dart';
import 'session.dart';

/// Shared cache of live canonical [ComponentEntity] records keyed by `entityId`.
///
/// Used when a historical conversation card toggles to "View latest", and keeps
/// home pinned cards synchronized when an entity is updated from any surface.
class ComponentEntitiesNotifier extends Notifier<Map<String, ComponentEntity>> {
  @override
  Map<String, ComponentEntity> build() {
    ref.listen(sessionProvider.select((s) => s.api), (prev, next) {
      if (!identical(prev, next)) {
        state = const {};
      }
    });
    return const {};
  }

  Api? get _api => ref.read(sessionProvider).api;

  void put(ComponentEntity entity) {
    if (entity.id.isEmpty) return;
    state = {...state, entity.id: entity};
  }

  /// Fetches the latest canonical snapshot for [entityId] from the server and caches it.
  Future<ComponentEntity?> fetchLatest(String entityId) async {
    if (entityId.isEmpty) return null;
    final api = _api;
    if (api == null) return null;
    try {
      final ent = await api.getComponentEntity(entityId);
      if (!ref.mounted || !identical(_api, api)) return null;
      put(ent);
      return ent;
    } catch (_) {
      return state[entityId];
    }
  }

  /// Updates live interactive state on [entityId] without incrementing its data version.
  Future<void> updateState(String entityId, Map<String, dynamic> nextState) async {
    if (entityId.isEmpty) return;
    final cur = state[entityId];
    if (cur != null) {
      put(ComponentEntity(
        id: cur.id,
        kind: cur.kind,
        title: cur.title,
        currentVersion: cur.currentVersion,
        data: cur.data,
        state: nextState,
        originConversationId: cur.originConversationId,
        originMessageId: cur.originMessageId,
        originComponentId: cur.originComponentId,
        lastSourceType: cur.lastSourceType,
        lastSourceConversationId: cur.lastSourceConversationId,
        lastSourceMessageId: cur.lastSourceMessageId,
        lastSourceTaskId: cur.lastSourceTaskId,
        createdAt: cur.createdAt,
        updatedAt: DateTime.now().millisecondsSinceEpoch,
        deleted: cur.deleted,
      ));
    }
    ref.read(pinnedComponentsProvider.notifier).syncComponentStateByEntity(entityId, nextState);
    final api = _api;
    if (api == null) return;
    try {
      final saved = await api.updateComponentEntity(entityId, state: nextState);
      if (!ref.mounted || !identical(_api, api)) return;
      put(saved);
      ref.read(pinnedComponentsProvider.notifier).syncEntityFromLive(saved);
    } catch (_) {
      if (cur != null && ref.mounted) put(cur);
    }
  }

  /// Updates content data on [entityId] (creates a new version) and syncs Home pins.
  Future<String?> updateData(String entityId, Map<String, dynamic> nextData, {String? changeSummary}) async {
    if (entityId.isEmpty) return '组件标识缺失';
    final api = _api;
    if (api == null) return '尚未登录';
    try {
      final saved = await api.updateComponentEntity(entityId, data: nextData, changeSummary: changeSummary);
      if (!ref.mounted || !identical(_api, api)) return null;
      put(saved);
      ref.read(pinnedComponentsProvider.notifier).syncEntityFromLive(saved);
      return null;
    } catch (e) {
      return ApiError.from(e).message;
    }
  }
}

final componentEntitiesProvider =
    NotifierProvider<ComponentEntitiesNotifier, Map<String, ComponentEntity>>(ComponentEntitiesNotifier.new);

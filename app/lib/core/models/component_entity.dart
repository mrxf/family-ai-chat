import 'json_read.dart';
import 'message.dart';

/// Canonical live state of a globally unique, versioned UI component entity.
class ComponentEntity {
  final String id;
  final String kind;
  final String title;
  final int currentVersion;
  final Map<String, dynamic> data;
  final Map<String, dynamic> state;
  final String originConversationId;
  final String originMessageId;
  final String originComponentId;
  final String lastSourceType;
  final String lastSourceConversationId;
  final String lastSourceMessageId;
  final String lastSourceTaskId;
  final int createdAt;
  final int updatedAt;
  final bool deleted;

  const ComponentEntity({
    required this.id,
    required this.kind,
    this.title = '',
    this.currentVersion = 1,
    this.data = const {},
    this.state = const {},
    this.originConversationId = '',
    this.originMessageId = '',
    this.originComponentId = '',
    this.lastSourceType = '',
    this.lastSourceConversationId = '',
    this.lastSourceMessageId = '',
    this.lastSourceTaskId = '',
    this.createdAt = 0,
    this.updatedAt = 0,
    this.deleted = false,
  });

  factory ComponentEntity.fromJson(Map<String, dynamic> j) {
    final ver = asInt(j['current_version']);
    return ComponentEntity(
      id: asStr(j['id']),
      kind: asStr(j['kind']),
      title: asStr(j['title']),
      currentVersion: ver > 0 ? ver : 1,
      data: j['data'] is Map ? Map<String, dynamic>.from((j['data'] as Map).cast()) : const {},
      state: j['state'] is Map ? Map<String, dynamic>.from((j['state'] as Map).cast()) : const {},
      originConversationId: asStr(j['origin_conversation_id']),
      originMessageId: asStr(j['origin_message_id']),
      originComponentId: asStr(j['origin_component_id']),
      lastSourceType: asStr(j['last_source_type']),
      lastSourceConversationId: asStr(j['last_source_conversation_id']),
      lastSourceMessageId: asStr(j['last_source_message_id']),
      lastSourceTaskId: asStr(j['last_source_task_id']),
      createdAt: asInt(j['created_at']),
      updatedAt: asInt(j['updated_at']),
      deleted: j['deleted'] == true,
    );
  }

  /// Converts this live entity into a [MessageComponent] suitable for card rendering.
  MessageComponent toMessageComponent({String? idOverride, int pos = 0}) => MessageComponent(
        id: idOverride ?? (originComponentId.isNotEmpty ? originComponentId : id),
        entityId: id,
        kind: kind,
        pos: pos,
        version: currentVersion,
        latestVersion: currentVersion,
        deleted: deleted,
        data: data,
        state: state,
      );
}

/// One immutable historical version snapshot of a [ComponentEntity].
class ComponentVersionRecord {
  final String id;
  final String componentId;
  final int version;
  final Map<String, dynamic> data;
  final Map<String, dynamic> state;
  final String changeSummary;
  final String sourceType; // 'create' | 'chat_update' | 'task_update' | 'manual_edit'
  final String sourceConversationId;
  final String sourceConversationTitle;
  final String sourceMessageId;
  final String sourceTaskId;
  final String sourceTaskTitle;
  final int createdAt;

  const ComponentVersionRecord({
    required this.id,
    required this.componentId,
    required this.version,
    this.data = const {},
    this.state = const {},
    this.changeSummary = '',
    this.sourceType = 'create',
    this.sourceConversationId = '',
    this.sourceConversationTitle = '',
    this.sourceMessageId = '',
    this.sourceTaskId = '',
    this.sourceTaskTitle = '',
    this.createdAt = 0,
  });

  factory ComponentVersionRecord.fromJson(Map<String, dynamic> j) {
    final ver = asInt(j['version']);
    return ComponentVersionRecord(
      id: asStr(j['id']),
      componentId: asStr(j['component_id']),
      version: ver > 0 ? ver : 1,
      data: j['data'] is Map ? Map<String, dynamic>.from((j['data'] as Map).cast()) : const {},
      state: j['state'] is Map ? Map<String, dynamic>.from((j['state'] as Map).cast()) : const {},
      changeSummary: asStr(j['change_summary']),
      sourceType: asStr(j['source_type']),
      sourceConversationId: asStr(j['source_conversation_id']),
      sourceConversationTitle: asStr(j['source_conversation_title']),
      sourceMessageId: asStr(j['source_message_id']),
      sourceTaskId: asStr(j['source_task_id']),
      sourceTaskTitle: asStr(j['source_task_title']),
      createdAt: asInt(j['created_at']),
    );
  }

  /// Human-readable Chinese provenance label for display in the version timeline.
  String get sourceLabel => switch (sourceType) {
        'create' => '首次创建',
        'chat_update' => '对话更新',
        'task_update' => '定时任务',
        'manual_edit' => '手动编辑',
        _ => '内容更新',
      };
}

/// Response payload for `GET /api/components/{entityId}/versions`.
class ComponentVersionsResponse {
  final ComponentEntity entity;
  final List<ComponentVersionRecord> versions;

  const ComponentVersionsResponse({required this.entity, required this.versions});

  factory ComponentVersionsResponse.fromJson(Map<String, dynamic> j) => ComponentVersionsResponse(
        entity: ComponentEntity.fromJson((j['entity'] as Map).cast()),
        versions: ((j['versions'] as List?) ?? const [])
            .whereType<Map>()
            .map((e) => ComponentVersionRecord.fromJson(e.cast()))
            .toList(),
      );
}

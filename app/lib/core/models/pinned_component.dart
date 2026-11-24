import 'json_read.dart';
import 'message.dart';

/// A personalized prompt suggestion displayed in the home "猜你想问" card.
class HomeSuggestion {
  final String emoji;
  final String text;

  const HomeSuggestion({required this.emoji, required this.text});

  factory HomeSuggestion.fromJson(Map<String, dynamic> j) => HomeSuggestion(
        emoji: asStr(j['emoji']),
        text: asStr(j['text']),
      );

  Map<String, dynamic> toJson() => {'emoji': emoji, 'text': text};
}

/// A message component pinned to the user's home board.
class PinnedComponent {
  final String id;
  final String entityId;
  final String conversationId;
  final String conversationTitle;
  final String messageId;
  final String componentId;
  final int sortOrder;
  final int pinnedAt;
  final int updatedAt;
  final MessageComponent component;

  const PinnedComponent({
    required this.id,
    this.entityId = '',
    required this.conversationId,
    this.conversationTitle = '',
    required this.messageId,
    required this.componentId,
    this.sortOrder = 0,
    this.pinnedAt = 0,
    this.updatedAt = 0,
    required this.component,
  });

  String get effectiveEntityId => entityId.isNotEmpty ? entityId : component.entityId;

  factory PinnedComponent.fromJson(Map<String, dynamic> j) => PinnedComponent(
        id: asStr(j['id']),
        entityId: asStr(j['entity_id']),
        conversationId: asStr(j['conversation_id']),
        conversationTitle: asStr(j['conversation_title']),
        messageId: asStr(j['message_id']),
        componentId: asStr(j['component_id']),
        sortOrder: asInt(j['sort_order']),
        pinnedAt: asInt(j['pinned_at']),
        updatedAt: asInt(j['updated_at']),
        component: j['component'] is Map
            ? MessageComponent.fromJson((j['component'] as Map).cast())
            : MessageComponent(id: asStr(j['component_id']), kind: ''),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        if (entityId.isNotEmpty) 'entity_id': entityId,
        'conversation_id': conversationId,
        'conversation_title': conversationTitle,
        'message_id': messageId,
        'component_id': componentId,
        'sort_order': sortOrder,
        'pinned_at': pinnedAt,
        if (updatedAt > 0) 'updated_at': updatedAt,
        'component': component.toJson(),
      };

  PinnedComponent copyWith({
    String? entityId,
    String? conversationTitle,
    int? updatedAt,
    MessageComponent? component,
  }) =>
      PinnedComponent(
        id: id,
        entityId: entityId ?? this.entityId,
        conversationId: conversationId,
        conversationTitle: conversationTitle ?? this.conversationTitle,
        messageId: messageId,
        componentId: componentId,
        sortOrder: sortOrder,
        pinnedAt: pinnedAt,
        updatedAt: updatedAt ?? this.updatedAt,
        component: component ?? this.component,
      );
}

/// Snapshot of a component referenced when starting or continuing a conversation.
class QuotedComponent {
  final String entityId;
  final int version;
  final String conversationId;
  final String conversationTitle;
  final String messageId;
  final String componentId;
  final String kind;
  final String title;
  final String summary;
  final Map<String, dynamic> data;
  final Map<String, dynamic> state;

  const QuotedComponent({
    this.entityId = '',
    this.version = 0,
    this.conversationId = '',
    this.conversationTitle = '',
    this.messageId = '',
    this.componentId = '',
    required this.kind,
    required this.title,
    this.summary = '',
    this.data = const {},
    this.state = const {},
  });

  factory QuotedComponent.fromComponent({
    required String messageId,
    required MessageComponent component,
    String conversationId = '',
    String conversationTitle = '',
  }) {
    final meta = describeComponent(component);
    return QuotedComponent(
      entityId: component.entityId,
      version: component.version,
      conversationId: conversationId,
      conversationTitle: conversationTitle,
      messageId: messageId,
      componentId: component.id,
      kind: component.kind,
      title: meta.title,
      summary: meta.summary,
      data: component.data,
      state: component.state,
    );
  }

  factory QuotedComponent.fromJson(Map<String, dynamic> j) => QuotedComponent(
        entityId: asStr(j['entity_id']),
        version: asInt(j['version']),
        conversationId: asStr(j['conversation_id']),
        conversationTitle: asStr(j['conversation_title']),
        messageId: asStr(j['message_id']),
        componentId: asStr(j['component_id']),
        kind: asStr(j['kind']),
        title: asStr(j['title']),
        summary: asStr(j['summary']),
        data: j['data'] is Map ? Map<String, dynamic>.from((j['data'] as Map).cast()) : const {},
        state: j['state'] is Map ? Map<String, dynamic>.from((j['state'] as Map).cast()) : const {},
      );

  Map<String, dynamic> toJson() => {
        if (entityId.isNotEmpty) 'entity_id': entityId,
        if (version > 0) 'version': version,
        if (conversationId.isNotEmpty) 'conversation_id': conversationId,
        if (conversationTitle.isNotEmpty) 'conversation_title': conversationTitle,
        if (messageId.isNotEmpty) 'message_id': messageId,
        if (componentId.isNotEmpty) 'component_id': componentId,
        'kind': kind,
        'title': title,
        if (summary.isNotEmpty) 'summary': summary,
        if (data.isNotEmpty) 'data': data,
        if (state.isNotEmpty) 'state': state,
      };

  /// Minimal payload sent to POST /api/chat so the server snapshots live state.
  Map<String, dynamic> toRequestRef() => {
        if (entityId.isNotEmpty) 'entity_id': entityId,
        if (messageId.isNotEmpty) 'message_id': messageId,
        if (componentId.isNotEmpty) 'component_id': componentId,
      };
}

/// Returns whether a component kind can be pinned to the home board.
bool isComponentPinnable(MessageComponent c) =>
    c.id.isNotEmpty && const {'weather', 'todo', 'feed', 'chart', 'metrics', 'quiz'}.contains(c.kind);

/// Extracts a human-readable kind label, title and state summary for a component.
({String kindLabel, String title, String summary}) describeComponent(MessageComponent c) {
  final d = c.data;
  switch (c.kind) {
    case 'feed':
      final title = '${d['title'] ?? ''}'.trim();
      final rawItems = d['items'] is List ? (d['items'] as List) : const [];
      final firstTitle = rawItems.isNotEmpty && rawItems.first is Map ? '${(rawItems.first as Map)['title'] ?? ''}'.trim() : '';
      final summary = rawItems.isEmpty
          ? '${d['subtitle'] ?? ''}'.trim()
          : (firstTitle.isEmpty ? '共 ${rawItems.length} 条' : '共 ${rawItems.length} 条 · $firstTitle');
      return (kindLabel: '动态资讯', title: title.isEmpty ? '动态资讯' : title, summary: summary);
    case 'weather':
      final loc = '${d['location'] ?? ''}'.trim();
      final dateLabel = '${d['date_label'] ?? ''}'.trim();
      final title = loc.isEmpty ? '天气预报' : (dateLabel.isEmpty ? loc : '$loc · $dateLabel');
      final cond = '${d['condition'] ?? ''}'.trim();
      final temp = d['temp'] as num?;
      final low = d['low'] as num?;
      final high = d['high'] as num?;
      final parts = <String>[
        if (cond.isNotEmpty) cond,
        if (temp != null) '${temp.round()}°C' else if (low != null && high != null) '${low.round()}~${high.round()}°C',
      ];
      return (kindLabel: '天气卡片', title: title, summary: parts.join(' · '));
    case 'todo':
      final title = '${d['title'] ?? ''}'.trim();
      final rawItems = d['items'] is List ? (d['items'] as List) : const [];
      final hasSaved = c.state.containsKey('done_ids');
      final doneIds = <String>{
        if (hasSaved && c.state['done_ids'] is List)
          for (final id in c.state['done_ids'] as List) '$id',
      };
      var total = 0;
      var done = 0;
      for (var i = 0; i < rawItems.length; i++) {
        final m = rawItems[i];
        if (m is! Map) continue;
        total++;
        final id = '${m['id'] ?? 'i${i + 1}'}';
        final isDone = hasSaved ? doneIds.contains(id) : m['done'] == true;
        if (isDone) done++;
      }
      return (kindLabel: '待办清单', title: title.isEmpty ? '待办清单' : title, summary: '已完成 $done/$total 项');
    case 'chart':
      final title = '${d['title'] ?? ''}'.trim();
      final sub = '${d['subtitle'] ?? ''}'.trim();
      return (kindLabel: '数据图表', title: title.isEmpty ? '数据图表' : title, summary: sub);
    case 'metrics':
      final title = '${d['title'] ?? ''}'.trim();
      final raw = d['metrics'] is List ? (d['metrics'] as List) : const [];
      final kvs = <String>[];
      for (final m in raw.take(3)) {
        if (m is Map) kvs.add('${m['label'] ?? ''} ${m['value'] ?? ''}${m['unit'] ?? ''}'.trim());
      }
      return (kindLabel: '指标看板', title: title.isEmpty ? '核心指标' : title, summary: kvs.join(' · '));
    case 'quiz':
      final q = '${d['question'] ?? ''}'.trim();
      final sel = (c.state['selected_index'] as num?)?.toInt() ?? -1;
      final ans = (d['answer_index'] as num?)?.toInt() ?? -1;
      final summary = sel < 0 ? '尚未作答' : (sel == ans ? '已作答（答对）' : '已作答（待复习）');
      return (kindLabel: '互动小测', title: q.isEmpty ? '互动小测' : q, summary: summary);
    default:
      return (kindLabel: '卡片', title: '引用卡片', summary: '');
  }
}

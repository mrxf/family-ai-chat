import 'package:flutter/foundation.dart';

import '../core/models/models.dart';

/// Live text of the generation in progress. Only the streaming bubble listens
/// to this, so tokens never rebuild the whole message list.
enum LivePhase { waiting, reasoning, answering }

@immutable
class RunStep {
  final String kind, title, detail, status;
  final DateTime? startedAt;

  const RunStep({
    required this.kind,
    required this.title,
    this.detail = '',
    this.status = 'done',
    this.startedAt,
  });

  bool get running => status == 'running';
  bool get isError => status == 'error';

  factory RunStep.fromJson(Map<String, dynamic> j) {
    final ts = (j['started_at'] as num?)?.toInt() ?? 0;
    return RunStep(
      kind: '${j['kind'] ?? ''}',
      title: '${j['title'] ?? ''}',
      detail: '${j['detail'] ?? ''}',
      status: '${j['status'] ?? 'done'}',
      startedAt: ts > 0 ? DateTime.fromMillisecondsSinceEpoch(ts) : null,
    );
  }
}

@immutable
class LiveText {
  final String content, reasoning;

  /// What a tool is doing right now ("正在画图…"), empty when idle.
  final String activity;

  /// Chronological task trace (memory recall, web search, webpage fetch).
  final List<RunStep> steps;

  /// Pictures drawn so far in this reply.
  final List<Attachment> attachments;

  /// Web pages referenced so far in this reply.
  final List<WebSource> sources;

  /// Interactive rich cards interleaved in this reply.
  final List<MessageComponent> components;

  /// When the current activity started (for the elapsed-seconds counter).
  final DateTime? activitySince;

  const LiveText(
    this.content,
    this.reasoning, {
    this.activity = '',
    this.steps = const [],
    this.attachments = const [],
    this.sources = const [],
    this.components = const [],
    this.activitySince,
  });
  static const empty = LiveText('', '');

  LivePhase get phase => (content.isNotEmpty || components.isNotEmpty)
      ? LivePhase.answering
      : reasoning.isNotEmpty
          ? LivePhase.reasoning
          : LivePhase.waiting;

  bool get drawing => activity.contains('画') || activity.contains('改图');
  bool get working => activity.isNotEmpty && !drawing;

  /// [activityAt] is the server's start time (unix ms) when known, so the
  /// elapsed counter survives reconnects.
  LiveText copyWith({
    String? content,
    String? reasoning,
    String? activity,
    int? activityAt,
    List<RunStep>? steps,
    List<Attachment>? attachments,
    List<WebSource>? sources,
    List<MessageComponent>? components,
  }) {
    final act = activity ?? this.activity;
    DateTime? since;
    if (act.isNotEmpty) {
      since = activityAt != null && activityAt > 0
          ? DateTime.fromMillisecondsSinceEpoch(activityAt)
          : (act == this.activity ? activitySince : null) ?? DateTime.now();
    }
    return LiveText(
      content ?? this.content,
      reasoning ?? this.reasoning,
      activity: act,
      steps: steps ?? this.steps,
      attachments: attachments ?? this.attachments,
      sources: sources ?? this.sources,
      components: components ?? this.components,
      activitySince: since,
    );
  }
}

@immutable
class ChatState {
  final String? conversationId;
  final String? assistantId;
  final String? modelId;

  /// Concrete model ID currently chosen by Auto mode (or last answered turn) in this conversation.
  final String? resolvedModelId;
  final bool think;
  final bool isPrivate;
  final bool isShared;
  final List<Message> messages;

  /// Messages before this index are laid out upward from the scroll anchor,
  /// messages from it onward downward. Set to the latest user message when
  /// sending so the new turn starts at the top of the screen and grows down.
  final int anchor;
  final bool loading, hasMore, loadingOlder;
  final String? activeId;
  final String activeUserId;
  final String activeUserName;
  final int lastSyncUpdatedAt;

  /// Bumps whenever a different conversation is shown (resets scroll).
  final int epoch;

  /// Optional component quote staged in the composer for the next message.
  final QuotedComponent? pendingQuote;

  /// Message ID to briefly highlight after jumping from a pinned component.
  final String? highlightMessageId;

  const ChatState({
    this.conversationId,
    this.assistantId,
    this.modelId,
    this.resolvedModelId,
    this.think = false,
    this.isPrivate = false,
    this.isShared = false,
    this.messages = const [],
    this.anchor = 0,
    this.loading = false,
    this.hasMore = false,
    this.loadingOlder = false,
    this.activeId,
    this.activeUserId = '',
    this.activeUserName = '',
    this.lastSyncUpdatedAt = 0,
    this.epoch = 0,
    this.pendingQuote,
    this.highlightMessageId,
  });

  bool get isEmpty => messages.isEmpty && !loading;
  bool get busy => activeId != null;

  /// True when on the plain home welcome screen (not inside a conversation,
  /// assistant view, private chat, or quoted-card draft).
  bool get isAtHome =>
      conversationId == null &&
      messages.isEmpty &&
      !loading &&
      assistantId == null &&
      !isPrivate &&
      pendingQuote == null;

  ChatState copyWith({
    String? conversationId,
    String? assistantId,
    Object? modelId = _keep,
    Object? resolvedModelId = _keep,
    bool? think,
    bool? isPrivate,
    bool? isShared,
    List<Message>? messages,
    int? anchor,
    bool? loading,
    bool? hasMore,
    bool? loadingOlder,
    Object? activeId = _keep,
    String? activeUserId,
    String? activeUserName,
    int? lastSyncUpdatedAt,
    int? epoch,
    Object? pendingQuote = _keep,
    Object? highlightMessageId = _keep,
  }) =>
      ChatState(
        conversationId: conversationId ?? this.conversationId,
        assistantId: assistantId ?? this.assistantId,
        modelId: modelId == _keep ? this.modelId : modelId as String?,
        resolvedModelId: resolvedModelId == _keep ? this.resolvedModelId : resolvedModelId as String?,
        think: think ?? this.think,
        isPrivate: isPrivate ?? this.isPrivate,
        isShared: isShared ?? this.isShared,
        messages: messages ?? this.messages,
        anchor: anchor ?? this.anchor,
        loading: loading ?? this.loading,
        hasMore: hasMore ?? this.hasMore,
        loadingOlder: loadingOlder ?? this.loadingOlder,
        activeId: activeId == _keep ? this.activeId : activeId as String?,
        activeUserId: activeUserId ?? this.activeUserId,
        activeUserName: activeUserName ?? this.activeUserName,
        lastSyncUpdatedAt: lastSyncUpdatedAt ?? this.lastSyncUpdatedAt,
        epoch: epoch ?? this.epoch,
        pendingQuote: pendingQuote == _keep ? this.pendingQuote : pendingQuote as QuotedComponent?,
        highlightMessageId: highlightMessageId == _keep ? this.highlightMessageId : highlightMessageId as String?,
      );
}

const _keep = Object();

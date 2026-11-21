import 'json_read.dart';

class Conversation {
  final String id, title, snippet;
  final String? assistantId, modelId;
  final bool autoMode, pinned, isPrivate;
  final int createdAt, updatedAt;
  const Conversation({
    required this.id,
    required this.title,
    this.snippet = '',
    this.assistantId,
    this.modelId,
    this.autoMode = false,
    required this.pinned,
    this.isPrivate = false,
    required this.createdAt,
    required this.updatedAt,
  });
  factory Conversation.fromJson(Map<String, dynamic> j) => Conversation(
        id: asStr(j['id']),
        title: asStr(j['title']),
        snippet: asStr(j['snippet']),
        assistantId: asStrOrNull(j['assistant_id']),
        modelId: asStrOrNull(j['model_id']),
        autoMode: j['auto_mode'] == true,
        pinned: j['pinned'] == true,
        isPrivate: j['private'] == true,
        createdAt: asInt(j['created_at']),
        updatedAt: asInt(j['updated_at']),
      );
  Map<String, dynamic> toJson() => {
        'id': id,
        'title': title,
        'assistant_id': assistantId,
        'model_id': modelId,
        if (autoMode) 'auto_mode': true,
        'pinned': pinned,
        if (isPrivate) 'private': true,
        'created_at': createdAt,
        'updated_at': updatedAt,
      };

  Conversation copyWith({String? title, String? snippet, bool? pinned, int? updatedAt, String? modelId, bool? autoMode}) => Conversation(
        id: id,
        title: title ?? this.title,
        snippet: snippet ?? this.snippet,
        assistantId: assistantId,
        modelId: modelId ?? this.modelId,
        autoMode: autoMode ?? this.autoMode,
        pinned: pinned ?? this.pinned,
        isPrivate: isPrivate,
        createdAt: createdAt,
        updatedAt: updatedAt ?? this.updatedAt,
      );
}

class Attachment {
  final String fileId, mime;
  final int width, height;
  /// Pictures drawn by the assistant: the prompt it used.
  final String prompt;
  /// A modified picture: the file it was made from.
  final String source;
  /// Local file path for an image that is still uploading / just sent (optimistic UI).
  final String? localPath;
  const Attachment({required this.fileId, required this.mime, this.width = 0, this.height = 0, this.prompt = '', this.source = '', this.localPath});
  factory Attachment.fromJson(Map<String, dynamic> j) =>
      Attachment(fileId: asStr(j['file_id']), mime: asStr(j['mime']), width: asInt(j['width']), height: asInt(j['height']), prompt: asStr(j['prompt']), source: asStr(j['source']));
  Map<String, dynamic> toJson() => {'file_id': fileId, 'mime': mime, 'width': width, 'height': height, if (prompt.isNotEmpty) 'prompt': prompt, if (source.isNotEmpty) 'source': source};
  double get aspect => width > 0 && height > 0 ? width / height : 1;
}

class WebSource {
  final String title, url, siteName, date, snippet;
  const WebSource({required this.title, required this.url, this.siteName = '', this.date = '', this.snippet = ''});
  factory WebSource.fromJson(Map<String, dynamic> j) => WebSource(
        title: asStr(j['title']),
        url: asStr(j['url']),
        siteName: asStr(j['site_name']),
        date: asStr(j['date']),
        snippet: asStr(j['snippet']),
      );
  Map<String, dynamic> toJson() => {
        'title': title,
        'url': url,
        if (siteName.isNotEmpty) 'site_name': siteName,
        if (date.isNotEmpty) 'date': date,
        if (snippet.isNotEmpty) 'snippet': snippet,
      };
}

class MessageComponent {
  final String id;
  final String kind; // 'weather' | 'todo' | 'chart' | 'quiz' | 'metrics' | 'ask_user'
  final int pos; // rune offset in Message.content
  final Map<String, dynamic> data;
  final Map<String, dynamic> state;

  const MessageComponent({
    required this.id,
    required this.kind,
    this.pos = 0,
    this.data = const {},
    this.state = const {},
  });

  factory MessageComponent.fromJson(Map<String, dynamic> j) => MessageComponent(
        id: asStr(j['id']),
        kind: asStr(j['kind']),
        pos: asInt(j['pos']),
        data: j['data'] is Map ? Map<String, dynamic>.from((j['data'] as Map).cast()) : const {},
        state: j['state'] is Map ? Map<String, dynamic>.from((j['state'] as Map).cast()) : const {},
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'kind': kind,
        'pos': pos,
        'data': data,
        if (state.isNotEmpty) 'state': state,
      };

  MessageComponent copyWith({Map<String, dynamic>? state}) => MessageComponent(
        id: id,
        kind: kind,
        pos: pos,
        data: data,
        state: state ?? this.state,
      );
}

enum MsgStatus { done, streaming, error, cancelled, interrupted, sending }

MsgStatus _status(String s) => switch (s) {
      'streaming' => MsgStatus.streaming,
      'error' => MsgStatus.error,
      'cancelled' => MsgStatus.cancelled,
      'interrupted' => MsgStatus.interrupted,
      _ => MsgStatus.done,
    };

class Message {
  final String id, conversationId, role, content, reasoning, error;
  final List<Attachment> attachments;
  final List<WebSource> sources;
  final List<MessageComponent> components;
  final MsgStatus status;
  final String? modelId;
  final int durationMs, completionTokens, createdAt;

  const Message({
    required this.id,
    required this.conversationId,
    required this.role,
    this.content = '',
    this.reasoning = '',
    this.error = '',
    this.attachments = const [],
    this.sources = const [],
    this.components = const [],
    this.status = MsgStatus.done,
    this.modelId,
    this.durationMs = 0,
    this.completionTokens = 0,
    this.createdAt = 0,
  });

  bool get isUser => role == 'user';
  bool get isActive => status == MsgStatus.streaming || status == MsgStatus.sending;

  factory Message.fromJson(Map<String, dynamic> j) => Message(
        id: asStr(j['id']),
        conversationId: asStr(j['conversation_id']),
        role: asStr(j['role']),
        content: asStr(j['content']),
        reasoning: asStr(j['reasoning']),
        error: asStr(j['error']),
        attachments: ((j['attachments'] as List?) ?? const []).map((e) => Attachment.fromJson((e as Map).cast())).toList(),
        sources: ((j['sources'] as List?) ?? const []).map((e) => WebSource.fromJson((e as Map).cast())).toList(),
        components: ((j['components'] as List?) ?? const []).map((e) => MessageComponent.fromJson((e as Map).cast())).toList(),
        status: _status(asStr(j['status'])),
        modelId: asStrOrNull(j['model_id']),
        durationMs: asInt(j['duration_ms']),
        completionTokens: asInt(j['completion_tokens']),
        createdAt: asInt(j['created_at']),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'conversation_id': conversationId,
        'role': role,
        'content': content,
        'reasoning': reasoning,
        'error': error,
        'attachments': attachments.map((a) => a.toJson()).toList(),
        if (sources.isNotEmpty) 'sources': sources.map((s) => s.toJson()).toList(),
        if (components.isNotEmpty) 'components': components.map((c) => c.toJson()).toList(),
        'status': status.name == 'sending' ? 'done' : status.name,
        'model_id': modelId,
        'duration_ms': durationMs,
        'completion_tokens': completionTokens,
        'created_at': createdAt,
      };

  Message copyWith({
    String? id,
    String? content,
    String? reasoning,
    MsgStatus? status,
    String? error,
    List<Attachment>? attachments,
    List<WebSource>? sources,
    List<MessageComponent>? components,
    String? modelId,
  }) =>
      Message(
        id: id ?? this.id,
        conversationId: conversationId,
        role: role,
        content: content ?? this.content,
        reasoning: reasoning ?? this.reasoning,
        error: error ?? this.error,
        attachments: attachments ?? this.attachments,
        sources: sources ?? this.sources,
        components: components ?? this.components,
        status: status ?? this.status,
        modelId: modelId ?? this.modelId,
        durationMs: durationMs,
        completionTokens: completionTokens,
        createdAt: createdAt,
      );
}

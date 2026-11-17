import 'package:flutter/widgets.dart';

// Plain data classes mirroring the server JSON.

int _int(dynamic v) => v is int ? v : (v is num ? v.toInt() : 0);
String _str(dynamic v) => v is String ? v : '';
String? _strN(dynamic v) => v is String && v.isNotEmpty ? v : null;

class User {
  final String id, username, displayName, avatar, role;
  final Map<String, dynamic> settings;
  final bool disabled;
  const User({required this.id, required this.username, required this.displayName, required this.avatar, required this.role, this.settings = const {}, this.disabled = false});

  bool get isAdmin => role == 'admin';
  String get initial => displayName.isEmpty ? '我' : displayName.characters.first;

  factory User.fromJson(Map<String, dynamic> j) => User(
        id: _str(j['id']),
        username: _str(j['username']),
        displayName: _str(j['display_name']),
        avatar: _str(j['avatar']),
        role: _str(j['role']),
        settings: (j['settings'] as Map?)?.cast<String, dynamic>() ?? const {},
        disabled: j['disabled'] == true,
      );
  Map<String, dynamic> toJson() =>
      {'id': id, 'username': username, 'display_name': displayName, 'avatar': avatar, 'role': role, 'settings': settings, 'disabled': disabled};
}

class ChatModel {
  final String id, displayName, description;
  final bool vision, think;
  const ChatModel({required this.id, required this.displayName, required this.description, required this.vision, required this.think});
  factory ChatModel.fromJson(Map<String, dynamic> j) => ChatModel(
        id: _str(j['id']),
        displayName: _str(j['display_name']),
        description: _str(j['description']),
        vision: j['vision'] == true,
        think: j['think'] == true,
      );
  Map<String, dynamic> toJson() => {'id': id, 'display_name': displayName, 'description': description, 'vision': vision, 'think': think};
}

class Assistant {
  final String id, name, avatar, description, greeting;
  final List<String> suggestions;
  final String? ownerId, modelId;
  const Assistant({required this.id, required this.name, required this.avatar, required this.description, required this.greeting, required this.suggestions, this.ownerId, this.modelId});
  factory Assistant.fromJson(Map<String, dynamic> j) => Assistant(
        id: _str(j['id']),
        name: _str(j['name']),
        avatar: _str(j['avatar']),
        description: _str(j['description']),
        greeting: _str(j['greeting']),
        suggestions: ((j['suggestions'] as List?) ?? const []).map((e) => e.toString()).toList(),
        ownerId: _strN(j['owner_id']),
        modelId: _strN(j['model_id']),
      );
  Map<String, dynamic> toJson() =>
      {'id': id, 'name': name, 'avatar': avatar, 'description': description, 'greeting': greeting, 'suggestions': suggestions, 'owner_id': ownerId, 'model_id': modelId};
}

class Conversation {
  final String id, title;
  final String? assistantId, modelId;
  final bool pinned;
  final int createdAt, updatedAt;
  const Conversation({required this.id, required this.title, this.assistantId, this.modelId, required this.pinned, required this.createdAt, required this.updatedAt});
  factory Conversation.fromJson(Map<String, dynamic> j) => Conversation(
        id: _str(j['id']),
        title: _str(j['title']),
        assistantId: _strN(j['assistant_id']),
        modelId: _strN(j['model_id']),
        pinned: j['pinned'] == true,
        createdAt: _int(j['created_at']),
        updatedAt: _int(j['updated_at']),
      );
  Map<String, dynamic> toJson() =>
      {'id': id, 'title': title, 'assistant_id': assistantId, 'model_id': modelId, 'pinned': pinned, 'created_at': createdAt, 'updated_at': updatedAt};

  Conversation copyWith({String? title, bool? pinned, int? updatedAt, String? modelId}) => Conversation(
        id: id,
        title: title ?? this.title,
        assistantId: assistantId,
        modelId: modelId ?? this.modelId,
        pinned: pinned ?? this.pinned,
        createdAt: createdAt,
        updatedAt: updatedAt ?? this.updatedAt,
      );
}

class Attachment {
  final String fileId, mime;
  final int width, height;
  /// Local file path for an image that is still uploading / just sent (optimistic UI).
  final String? localPath;
  const Attachment({required this.fileId, required this.mime, this.width = 0, this.height = 0, this.localPath});
  factory Attachment.fromJson(Map<String, dynamic> j) =>
      Attachment(fileId: _str(j['file_id']), mime: _str(j['mime']), width: _int(j['width']), height: _int(j['height']));
  Map<String, dynamic> toJson() => {'file_id': fileId, 'mime': mime, 'width': width, 'height': height};
  double get aspect => width > 0 && height > 0 ? width / height : 1;
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
    this.status = MsgStatus.done,
    this.modelId,
    this.durationMs = 0,
    this.completionTokens = 0,
    this.createdAt = 0,
  });

  bool get isUser => role == 'user';
  bool get isActive => status == MsgStatus.streaming || status == MsgStatus.sending;

  factory Message.fromJson(Map<String, dynamic> j) => Message(
        id: _str(j['id']),
        conversationId: _str(j['conversation_id']),
        role: _str(j['role']),
        content: _str(j['content']),
        reasoning: _str(j['reasoning']),
        error: _str(j['error']),
        attachments: ((j['attachments'] as List?) ?? const []).map((e) => Attachment.fromJson((e as Map).cast())).toList(),
        status: _status(_str(j['status'])),
        modelId: _strN(j['model_id']),
        durationMs: _int(j['duration_ms']),
        completionTokens: _int(j['completion_tokens']),
        createdAt: _int(j['created_at']),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'conversation_id': conversationId,
        'role': role,
        'content': content,
        'reasoning': reasoning,
        'error': error,
        'attachments': attachments.map((a) => a.toJson()).toList(),
        'status': status.name == 'sending' ? 'done' : status.name,
        'model_id': modelId,
        'duration_ms': durationMs,
        'completion_tokens': completionTokens,
        'created_at': createdAt,
      };

  Message copyWith({String? id, String? content, String? reasoning, MsgStatus? status, String? error, List<Attachment>? attachments}) => Message(
        id: id ?? this.id,
        conversationId: conversationId,
        role: role,
        content: content ?? this.content,
        reasoning: reasoning ?? this.reasoning,
        error: error ?? this.error,
        attachments: attachments ?? this.attachments,
        status: status ?? this.status,
        modelId: modelId,
        durationMs: durationMs,
        completionTokens: completionTokens,
        createdAt: createdAt,
      );
}

class Bootstrap {
  final User user;
  final List<ChatModel> models;
  final List<Assistant> assistants;
  final String defaultModelId;
  final bool voiceInput;
  const Bootstrap({required this.user, required this.models, required this.assistants, required this.defaultModelId, required this.voiceInput});

  factory Bootstrap.fromJson(Map<String, dynamic> j) => Bootstrap(
        user: User.fromJson((j['user'] as Map).cast()),
        models: ((j['models'] as List?) ?? const []).map((e) => ChatModel.fromJson((e as Map).cast())).toList(),
        assistants: ((j['assistants'] as List?) ?? const []).map((e) => Assistant.fromJson((e as Map).cast())).toList(),
        defaultModelId: _str(j['default_model_id']),
        voiceInput: ((j['features'] as Map?)?['voice_input']) == true,
      );
  Map<String, dynamic> toJson() => {
        'user': user.toJson(),
        'models': models.map((m) => m.toJson()).toList(),
        'assistants': assistants.map((a) => a.toJson()).toList(),
        'default_model_id': defaultModelId,
        'features': {'voice_input': voiceInput},
      };

  Bootstrap copyWith({User? user}) =>
      Bootstrap(user: user ?? this.user, models: models, assistants: assistants, defaultModelId: defaultModelId, voiceInput: voiceInput);

  ChatModel? model(String? id) {
    for (final m in models) {
      if (m.id == id) return m;
    }
    return null;
  }

  ChatModel? get defaultModel => model(defaultModelId) ?? (models.isEmpty ? null : models.first);

  /// Same order the server uses: conversation choice → assistant's model → global default.
  ChatModel? effectiveModel(String? modelId, String? assistantId) =>
      model(modelId) ?? model(assistant(assistantId)?.modelId) ?? defaultModel;

  Assistant? assistant(String? id) {
    for (final a in assistants) {
      if (a.id == id) return a;
    }
    return null;
  }
}

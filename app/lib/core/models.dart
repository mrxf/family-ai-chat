import 'package:flutter/widgets.dart';

// Plain data classes mirroring the server JSON.

const kAutoModelId = 'auto';

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
  final bool useMemory;
  const Assistant({required this.id, required this.name, required this.avatar, required this.description, required this.greeting, required this.suggestions, this.ownerId, this.modelId, this.useMemory = true});
  factory Assistant.fromJson(Map<String, dynamic> j) => Assistant(
        id: _str(j['id']),
        name: _str(j['name']),
        avatar: _str(j['avatar']),
        description: _str(j['description']),
        greeting: _str(j['greeting']),
        suggestions: ((j['suggestions'] as List?) ?? const []).map((e) => e.toString()).toList(),
        ownerId: _strN(j['owner_id']),
        modelId: _strN(j['model_id']),
        useMemory: j['use_memory'] != false,
      );
  Map<String, dynamic> toJson() =>
      {'id': id, 'name': name, 'avatar': avatar, 'description': description, 'greeting': greeting, 'suggestions': suggestions, 'owner_id': ownerId, 'model_id': modelId, 'use_memory': useMemory};
}

enum MemoryCategory {
  health('健康与用药'),
  family('家人'),
  profile('关于我'),
  preference('喜好习惯'),
  ongoing('近期在做'),
  episode('聊过的事');

  const MemoryCategory(this.label);
  final String label;
  static MemoryCategory parse(dynamic value) => values.where((c) => c.name == value).firstOrNull ?? profile;
}

class Memory {
  final String id, content, source;
  final MemoryCategory category;
  final String? sourceConvId;
  final bool pinned, isNew;
  final int createdAt, updatedAt;
  const Memory({required this.id, required this.category, required this.content, required this.source, this.sourceConvId,
    required this.pinned, required this.isNew, required this.createdAt, required this.updatedAt});

  factory Memory.fromJson(Map<String, dynamic> j) => Memory(
    id: _str(j['id']), category: MemoryCategory.parse(j['category']), content: memoryToDisplay(_str(j['content'])), source: _str(j['source']),
    sourceConvId: _strN(j['source_conv_id']), pinned: j['pinned'] == true, isNew: j['is_new'] == true,
    createdAt: _int(j['created_at']), updatedAt: _int(j['updated_at']),
  );
}

/// Memories are stored in the third person ("用户…") so every model knows who
/// they are about; the family reads and edits them addressed to themselves ("你…").
/// Compounds like 用户体验 or 老年用户 are left alone.
final _subject = RegExp(
  r'(?<!老年|年轻|新|老|普通|目标|终端|企业|个人|付费|活跃|潜在|核心|海外|国内|广大|中国)用户'
  r'(?!体验|研究|画像|增长|群|数|量|端|界面|反馈|需求|调研|协议|中心|侧|运营|留存|访谈|行为|价值|隐私|数据)',
);
String memoryToDisplay(String s) => s.replaceAll(_subject, '你');

String memoryToStorage(String s) => s
    .trim()
    .replaceFirst(RegExp(r'^[我你](?!们)'), '用户')
    .replaceAll(RegExp(r'[我你]的'), '用户的')
    .replaceAllMapped(RegExp(r'([，,；;。])[我你](?!们)'), (m) => '${m[1]}用户');

/// A memory to create: typed by hand, or an import candidate. Import candidates
/// also carry retrieval keywords, an end date for ongoing items, and the existing
/// memory they would replace.
class MemoryDraft {
  final MemoryCategory category;
  final String content;
  final String keywords;
  final String? validUntil; // YYYY-MM-DD
  final String? replacesId;
  final String? replaces; // the replaced text, shown in the preview
  const MemoryDraft({required this.category, required this.content, this.keywords = '', this.validUntil, this.replacesId, this.replaces});
  factory MemoryDraft.fromJson(Map<String, dynamic> j) => MemoryDraft(
    category: MemoryCategory.parse(j['category']),
    content: memoryToDisplay(_str(j['content'])),
    keywords: _str(j['keywords']),
    validUntil: _strN(j['valid_until']),
    replacesId: _strN(j['replaces_id']),
    replaces: _strN(j['replaces']) == null ? null : memoryToDisplay(_str(j['replaces'])),
  );
  Map<String, dynamic> toJson() => {
    'category': category.name,
    'content': memoryToStorage(content),
    if (keywords.isNotEmpty) 'keywords': keywords,
    if (validUntil != null) 'valid_until': validUntil,
    if (replacesId != null) 'replaces_id': replacesId,
  };

  /// Applies a user's edit: rewritten text drops the model's keywords (the server
  /// fills retrieval in later), and an end date only applies to ongoing items.
  MemoryDraft edited(MemoryDraft e) => MemoryDraft(
    category: e.category,
    content: e.content,
    keywords: e.content == content ? keywords : '',
    validUntil: e.category == MemoryCategory.ongoing ? validUntil : null,
    replacesId: replacesId,
    replaces: replaces,
  );
}

/// Organizing pasted text runs on the server; the App polls this.
class MemoryImportJob {
  final String id, status;
  final int done, total, skipped, failed;
  final List<MemoryDraft> items;
  final bool truncated;
  final String? error;
  const MemoryImportJob({required this.id, required this.status, this.done = 0, this.total = 0, this.skipped = 0, this.failed = 0,
    this.items = const [], this.truncated = false, this.error});
  bool get running => status == 'running';
  bool get failedAll => status == 'failed';
  factory MemoryImportJob.fromJson(Map<String, dynamic> j) => MemoryImportJob(
    id: _str(j['id']), status: _str(j['status']), done: _int(j['done']), total: _int(j['total']),
    skipped: _int(j['skipped']), failed: _int(j['failed']), truncated: j['truncated'] == true, error: _strN(j['error']),
    items: ((j['items'] as List?) ?? const []).map((e) => MemoryDraft.fromJson((e as Map).cast())).toList(),
  );
}

class MemoriesResult {
  final List<Memory> memories;
  final int total, newCount;
  final bool enabled;
  const MemoriesResult({required this.memories, required this.total, required this.newCount, required this.enabled});
  factory MemoriesResult.fromJson(Map<String, dynamic> j) => MemoriesResult(
    memories: ((j['memories'] as List?) ?? []).map((e) => Memory.fromJson((e as Map).cast())).toList(),
    total: _int(j['total']), newCount: _int(j['new']), enabled: j['enabled'] != false,
  );
}

/// Fixed category order, newest first within each nonempty group. Does not mutate the input.
Map<MemoryCategory, List<Memory>> groupMemories(Iterable<Memory> memories) {
  final result = <MemoryCategory, List<Memory>>{};
  for (final category in MemoryCategory.values) {
    final items = memories.where((m) => m.category == category).toList()
      ..sort((a, b) {
        final time = b.createdAt.compareTo(a.createdAt);
        return time != 0 ? time : a.id.compareTo(b.id);
      });
    if (items.isNotEmpty) result[category] = items;
  }
  return result;
}

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
        id: _str(j['id']),
        title: _str(j['title']),
        snippet: _str(j['snippet']),
        assistantId: _strN(j['assistant_id']),
        modelId: _strN(j['model_id']),
        autoMode: j['auto_mode'] == true,
        pinned: j['pinned'] == true,
        isPrivate: j['private'] == true,
        createdAt: _int(j['created_at']),
        updatedAt: _int(j['updated_at']),
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
      Attachment(fileId: _str(j['file_id']), mime: _str(j['mime']), width: _int(j['width']), height: _int(j['height']), prompt: _str(j['prompt']), source: _str(j['source']));
  Map<String, dynamic> toJson() => {'file_id': fileId, 'mime': mime, 'width': width, 'height': height, if (prompt.isNotEmpty) 'prompt': prompt, if (source.isNotEmpty) 'source': source};
  double get aspect => width > 0 && height > 0 ? width / height : 1;
}

class WebSource {
  final String title, url, siteName, date, snippet;
  const WebSource({required this.title, required this.url, this.siteName = '', this.date = '', this.snippet = ''});
  factory WebSource.fromJson(Map<String, dynamic> j) => WebSource(
        title: _str(j['title']),
        url: _str(j['url']),
        siteName: _str(j['site_name']),
        date: _str(j['date']),
        snippet: _str(j['snippet']),
      );
  Map<String, dynamic> toJson() => {
        'title': title,
        'url': url,
        if (siteName.isNotEmpty) 'site_name': siteName,
        if (date.isNotEmpty) 'date': date,
        if (snippet.isNotEmpty) 'snippet': snippet,
      };
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
        sources: ((j['sources'] as List?) ?? const []).map((e) => WebSource.fromJson((e as Map).cast())).toList(),
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
        if (sources.isNotEmpty) 'sources': sources.map((s) => s.toJson()).toList(),
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
        status: status ?? this.status,
        modelId: modelId ?? this.modelId,
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
  final bool voiceInput, webSearch, imageGeneration, autoModel, autoDynamic;
  final String voiceMode; // 'stream' | 'upload' | ''
  final String searchProvider; // 'deepseek' | 'serp' | ''
  /// Pictures the user may still draw today; -1 = no limit.
  final int imagesLeft;
  const Bootstrap({
    required this.user,
    required this.models,
    required this.assistants,
    required this.defaultModelId,
    required this.voiceInput,
    this.voiceMode = '',
    this.webSearch = false,
    this.searchProvider = '',
    this.imageGeneration = false,
    this.imagesLeft = -1,
    this.autoModel = false,
    this.autoDynamic = false,
  });

  factory Bootstrap.fromJson(Map<String, dynamic> j) {
    final feat = j['features'] as Map?;
    final voiceOn = feat?['voice_input'] == true;
    final rawMode = _str(feat?['voice_mode']);
    return Bootstrap(
      user: User.fromJson((j['user'] as Map).cast()),
      models: ((j['models'] as List?) ?? const []).map((e) => ChatModel.fromJson((e as Map).cast())).toList(),
      assistants: ((j['assistants'] as List?) ?? const []).map((e) => Assistant.fromJson((e as Map).cast())).toList(),
      defaultModelId: _str(j['default_model_id']),
      voiceInput: voiceOn,
      voiceMode: rawMode.isNotEmpty ? rawMode : (voiceOn ? 'upload' : ''),
      webSearch: feat?['web_search'] == true,
      searchProvider: _str(feat?['search_provider']),
      imageGeneration: feat?['image_generation'] == true,
      imagesLeft: feat?['images_left'] is num ? (feat!['images_left'] as num).toInt() : -1,
      autoModel: feat?['auto_model'] == true,
      autoDynamic: feat?['auto_dynamic'] == true,
    );
  }
  Map<String, dynamic> toJson() => {
        'user': user.toJson(),
        'models': models.map((m) => m.toJson()).toList(),
        'assistants': assistants.map((a) => a.toJson()).toList(),
        'default_model_id': defaultModelId,
        'features': {
          'voice_input': voiceInput,
          'voice_mode': voiceMode,
          'web_search': webSearch,
          'search_provider': searchProvider,
          'image_generation': imageGeneration,
          'images_left': imagesLeft,
          'auto_model': autoModel,
          'auto_dynamic': autoDynamic,
        },
      };

  Bootstrap copyWith({User? user}) => Bootstrap(
        user: user ?? this.user,
        models: models,
        assistants: assistants,
        defaultModelId: defaultModelId,
        voiceInput: voiceInput,
        voiceMode: voiceMode,
        webSearch: webSearch,
        searchProvider: searchProvider,
        imageGeneration: imageGeneration,
        imagesLeft: imagesLeft,
        autoModel: autoModel,
        autoDynamic: autoDynamic,
      );

  ChatModel? model(String? id) {
    for (final m in models) {
      if (m.id == id) return m;
    }
    return null;
  }

  /// Virtual entry representing server-side Auto mode in the picker and composer.
  ChatModel get autoChatModel => ChatModel(
        id: kAutoModelId,
        displayName: '自动选模型',
        description: autoDynamic ? '不用纠结选哪个，根据问题自动挑模型，换话题也会自动调整' : '不用纠结选哪个，小暖根据你的问题自动挑最合适的模型',
        vision: models.any((m) => m.vision),
        think: models.any((m) => m.think),
      );

  bool isAutoSelection(String? modelId, String? assistantId) {
    if (!autoModel || models.isEmpty) return false;
    if (modelId == kAutoModelId) return true;
    if (modelId != null && modelId.isNotEmpty && model(modelId) != null) return false;
    final asstModel = assistant(assistantId)?.modelId;
    if (asstModel != null && model(asstModel) != null) return false;
    return defaultModelId == kAutoModelId;
  }

  ChatModel? get defaultModel {
    if (autoModel && defaultModelId == kAutoModelId && models.isNotEmpty) return autoChatModel;
    return model(defaultModelId) ?? (models.isEmpty ? null : models.first);
  }

  /// Same order the server uses: conversation choice → assistant's model → global default.
  ChatModel? effectiveModel(String? modelId, String? assistantId) {
    if (isAutoSelection(modelId, assistantId)) return autoChatModel;
    return model(modelId) ?? model(assistant(assistantId)?.modelId) ?? (models.isEmpty ? null : (model(defaultModelId) ?? models.first));
  }

  Assistant? assistant(String? id) {
    for (final a in assistants) {
      if (a.id == id) return a;
    }
    return null;
  }
}

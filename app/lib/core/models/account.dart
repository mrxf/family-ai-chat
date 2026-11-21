import 'package:flutter/widgets.dart' show StringCharacters;

import 'json_read.dart';

/// Model id meaning "let the server pick a model for each turn".
const kAutoModelId = 'auto';

class User {
  final String id, username, displayName, avatar, role;
  final Map<String, dynamic> settings;
  final bool disabled;
  const User({required this.id, required this.username, required this.displayName, required this.avatar, required this.role, this.settings = const {}, this.disabled = false});

  bool get isAdmin => role == 'admin';
  String get initial => displayName.isEmpty ? '我' : displayName.characters.first;

  factory User.fromJson(Map<String, dynamic> j) => User(
        id: asStr(j['id']),
        username: asStr(j['username']),
        displayName: asStr(j['display_name']),
        avatar: asStr(j['avatar']),
        role: asStr(j['role']),
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
        id: asStr(j['id']),
        displayName: asStr(j['display_name']),
        description: asStr(j['description']),
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
        id: asStr(j['id']),
        name: asStr(j['name']),
        avatar: asStr(j['avatar']),
        description: asStr(j['description']),
        greeting: asStr(j['greeting']),
        suggestions: ((j['suggestions'] as List?) ?? const []).map((e) => e.toString()).toList(),
        ownerId: asStrOrNull(j['owner_id']),
        modelId: asStrOrNull(j['model_id']),
        useMemory: j['use_memory'] != false,
      );
  Map<String, dynamic> toJson() =>
      {'id': id, 'name': name, 'avatar': avatar, 'description': description, 'greeting': greeting, 'suggestions': suggestions, 'owner_id': ownerId, 'model_id': modelId, 'use_memory': useMemory};
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
    final rawMode = asStr(feat?['voice_mode']);
    return Bootstrap(
      user: User.fromJson((j['user'] as Map).cast()),
      models: ((j['models'] as List?) ?? const []).map((e) => ChatModel.fromJson((e as Map).cast())).toList(),
      assistants: ((j['assistants'] as List?) ?? const []).map((e) => Assistant.fromJson((e as Map).cast())).toList(),
      defaultModelId: asStr(j['default_model_id']),
      voiceInput: voiceOn,
      voiceMode: rawMode.isNotEmpty ? rawMode : (voiceOn ? 'upload' : ''),
      webSearch: feat?['web_search'] == true,
      searchProvider: asStr(feat?['search_provider']),
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

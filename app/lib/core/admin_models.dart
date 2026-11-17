// Data classes for the admin "models & providers" screens.

int _int(dynamic v) => v is int ? v : (v is num ? v.toInt() : 0);
double _dbl(dynamic v) => v is num ? v.toDouble() : 0;
String _str(dynamic v) => v is String ? v : '';

class ProviderPreset {
  final String kind, name, baseUrl, description, keyUrl, keyPrefix;
  const ProviderPreset({required this.kind, required this.name, required this.baseUrl, required this.description, required this.keyUrl, required this.keyPrefix});
  bool get isCustom => kind == 'custom';
  factory ProviderPreset.fromJson(Map<String, dynamic> j) => ProviderPreset(
        kind: _str(j['kind']),
        name: _str(j['name']),
        baseUrl: _str(j['base_url']),
        description: _str(j['description']),
        keyUrl: _str(j['key_url']),
        keyPrefix: _str(j['key_prefix']),
      );
}

class AdminProvider {
  final String id, kind, name, baseUrl, apiKeyHint, endpoint, keyUrl;
  final bool enabled;
  final int sort, modelCount, enabledModelCount;
  const AdminProvider({
    required this.id,
    required this.kind,
    required this.name,
    required this.baseUrl,
    required this.apiKeyHint,
    required this.endpoint,
    required this.keyUrl,
    required this.enabled,
    required this.sort,
    required this.modelCount,
    required this.enabledModelCount,
  });
  factory AdminProvider.fromJson(Map<String, dynamic> j) => AdminProvider(
        id: _str(j['id']),
        kind: _str(j['kind']).isEmpty ? 'custom' : _str(j['kind']),
        name: _str(j['name']),
        baseUrl: _str(j['base_url']),
        apiKeyHint: _str(j['api_key_hint']),
        endpoint: _str(j['endpoint']),
        keyUrl: _str(j['key_url']),
        enabled: j['enabled'] == true,
        sort: _int(j['sort']),
        modelCount: _int(j['model_count']),
        enabledModelCount: _int(j['enabled_model_count']),
      );

  String get host => Uri.tryParse(baseUrl)?.host ?? baseUrl;
}

class AdminModel {
  final String id, providerId, kind, upstreamModel, displayName, description, defaultEffort, thinkEffort;
  final bool vision, tools, enabled;
  final int contextWindow, maxOutput, sort;
  final double inputPrice, outputPrice;
  const AdminModel({
    required this.id,
    required this.providerId,
    required this.kind,
    required this.upstreamModel,
    required this.displayName,
    required this.description,
    required this.defaultEffort,
    required this.thinkEffort,
    required this.vision,
    required this.tools,
    required this.enabled,
    required this.contextWindow,
    required this.maxOutput,
    required this.sort,
    required this.inputPrice,
    required this.outputPrice,
  });
  factory AdminModel.fromJson(Map<String, dynamic> j) => AdminModel(
        id: _str(j['id']),
        providerId: _str(j['provider_id']),
        kind: _str(j['kind']) == 'image' ? 'image' : 'chat',
        upstreamModel: _str(j['upstream_model']),
        displayName: _str(j['display_name']),
        description: _str(j['description']),
        defaultEffort: _str(j['default_effort']),
        thinkEffort: _str(j['think_effort']),
        vision: j['vision'] == true,
        tools: j['tools'] == true,
        enabled: j['enabled'] == true,
        contextWindow: _int(j['context_window']),
        maxOutput: _int(j['max_output']),
        sort: _int(j['sort']),
        inputPrice: _dbl(j['input_price']),
        outputPrice: _dbl(j['output_price']),
      );
  bool get think => thinkEffort.isNotEmpty;
  bool get hasPrice => inputPrice > 0 || outputPrice > 0;
  bool get isImage => kind == 'image';
}

/// A model offered by the provider's /models, with suggested settings.
class UpstreamItem {
  final String upstreamModel, displayName, description, defaultEffort, thinkEffort, existingModelId;
  final bool vision, chat, image, tools, recommended;
  final int contextWindow, created;
  final double inputPrice, outputPrice;
  const UpstreamItem({
    required this.upstreamModel,
    required this.displayName,
    required this.description,
    required this.defaultEffort,
    required this.thinkEffort,
    required this.existingModelId,
    required this.vision,
    required this.chat,
    required this.image,
    required this.tools,
    required this.recommended,
    required this.contextWindow,
    required this.created,
    required this.inputPrice,
    required this.outputPrice,
  });
  factory UpstreamItem.fromJson(Map<String, dynamic> j) => UpstreamItem(
        upstreamModel: _str(j['upstream_model']),
        displayName: _str(j['display_name']),
        description: _str(j['description']),
        defaultEffort: _str(j['default_effort']),
        thinkEffort: _str(j['think_effort']),
        existingModelId: _str(j['existing_model_id']),
        vision: j['vision'] == true,
        chat: j['chat'] == true,
        image: j['image'] == true,
        tools: j['tools'] == true,
        recommended: j['recommended'] == true,
        contextWindow: _int(j['context_window']),
        created: _int(j['created']),
        inputPrice: _dbl(j['input_price']),
        outputPrice: _dbl(j['output_price']),
      );
  bool get added => existingModelId.isNotEmpty;
  bool get think => thinkEffort.isNotEmpty;
  bool get hasPrice => inputPrice > 0 || outputPrice > 0;

  Map<String, dynamic> toModelJson() => {
        'kind': image ? 'image' : 'chat',
        'tools': tools,
        'upstream_model': upstreamModel,
        'display_name': displayName,
        'description': description,
        'vision': vision,
        'default_effort': defaultEffort,
        'think_effort': thinkEffort,
        'context_window': contextWindow,
        'input_price': inputPrice,
        'output_price': outputPrice,
      };
}

class ProviderTestResult {
  final bool ok;
  final String message, endpoint;
  final int modelCount, chatModelCount, latencyMs;
  final double? keyUsage, keyLimit;
  const ProviderTestResult({
    required this.ok,
    required this.message,
    required this.endpoint,
    required this.modelCount,
    required this.chatModelCount,
    required this.latencyMs,
    this.keyUsage,
    this.keyLimit,
  });
  factory ProviderTestResult.fromJson(Map<String, dynamic> j) => ProviderTestResult(
        ok: j['ok'] == true,
        message: _str(j['message']),
        endpoint: _str(j['endpoint']),
        modelCount: _int(j['model_count']),
        chatModelCount: _int(j['chat_model_count']),
        latencyMs: _int(j['latency_ms']),
        keyUsage: j['key_usage'] is num ? (j['key_usage'] as num).toDouble() : null,
        keyLimit: j['key_limit'] is num ? (j['key_limit'] as num).toDouble() : null,
      );
}

class ModelTestResult {
  final bool ok, reasoning;
  final String message, reply, effort;
  final int firstTokenMs, durationMs;
  final double cost;
  /// Image models: the test picture.
  final String fileId;
  final int width, height;
  const ModelTestResult({
    this.fileId = '',
    this.width = 0,
    this.height = 0,
    required this.ok,
    required this.reasoning,
    required this.message,
    required this.reply,
    required this.effort,
    required this.firstTokenMs,
    required this.durationMs,
    required this.cost,
  });
  factory ModelTestResult.fromJson(Map<String, dynamic> j) => ModelTestResult(
        ok: j['ok'] == true,
        reasoning: j['reasoning'] == true,
        message: _str(j['message']),
        reply: _str(j['reply']),
        effort: _str(j['effort']),
        firstTokenMs: _int(j['first_token_ms']),
        durationMs: _int(j['duration_ms']),
        cost: _dbl(j['cost']),
        fileId: _str(j['file_id']),
        width: _int(j['width']),
        height: _int(j['height']),
      );
}

/// Model choices the admin assigns for the whole family.
class ModelSettings {
  final String defaultModelId, fallbackModelId, titleModelId, imageModelId;
  /// Pictures per family member per day; 0 = no limit.
  final int imageDailyLimit;
  const ModelSettings({this.defaultModelId = '', this.fallbackModelId = '', this.titleModelId = '', this.imageModelId = '', this.imageDailyLimit = 0});
  factory ModelSettings.fromJson(Map<String, dynamic> j) => ModelSettings(
        defaultModelId: _str(j['default_model_id']),
        fallbackModelId: _str(j['fallback_model_id']),
        titleModelId: _str(j['title_model_id']),
        imageModelId: _str(j['image_model_id']),
        imageDailyLimit: int.tryParse(_str(j['image_daily_limit'])) ?? 0,
      );
}

class UsageRow {
  final String id, name; // user id or model id; name only for models
  final int messages, promptTokens, completionTokens;
  final double cost;
  const UsageRow({required this.id, required this.name, required this.messages, required this.promptTokens, required this.completionTokens, required this.cost});
  int get tokens => promptTokens + completionTokens;
  factory UsageRow.user(Map<String, dynamic> j) => UsageRow(
        id: _str(j['user_id']),
        name: '',
        messages: _int(j['messages']),
        promptTokens: _int(j['prompt_tokens']),
        completionTokens: _int(j['completion_tokens']),
        cost: _dbl(j['cost']),
      );
  factory UsageRow.model(Map<String, dynamic> j) => UsageRow(
        id: _str(j['model_id']),
        name: _str(j['display_name']),
        messages: _int(j['messages']),
        promptTokens: _int(j['prompt_tokens']),
        completionTokens: _int(j['completion_tokens']),
        cost: _dbl(j['cost']),
      );
}

class UsageReport {
  final int days;
  final List<UsageRow> users, models;
  final UsageRow total;
  const UsageReport({required this.days, required this.users, required this.models, required this.total});
  factory UsageReport.fromJson(Map<String, dynamic> j) => UsageReport(
        days: _int(j['days']),
        users: ((j['items'] as List?) ?? const []).map((e) => UsageRow.user((e as Map).cast())).toList(),
        models: ((j['models'] as List?) ?? const []).map((e) => UsageRow.model((e as Map).cast())).toList(),
        total: UsageRow.user(((j['total'] as Map?) ?? const {}).cast()),
      );
}

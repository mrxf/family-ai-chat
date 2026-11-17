import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';

import 'admin_models.dart';
import 'models.dart';

class ApiError implements Exception {
  final int status;
  final String code;
  final String message;
  const ApiError(this.status, this.code, this.message);

  bool get isUnauthorized => status == 401;
  bool get isNetwork => status == 0;

  @override
  String toString() => message;

  static ApiError from(Object e) {
    if (e is ApiError) return e;
    if (e is DioException) {
      final data = e.response?.data;
      if (data is Map && data['error'] is Map) {
        final err = data['error'] as Map;
        return ApiError(e.response?.statusCode ?? 0, '${err['code']}', '${err['message']}');
      }
      if (e.response != null) {
        return ApiError(e.response!.statusCode ?? 0, 'http', '服务器返回了错误（${e.response!.statusCode}）');
      }
      return switch (e.type) {
        DioExceptionType.connectionTimeout || DioExceptionType.receiveTimeout || DioExceptionType.sendTimeout =>
          const ApiError(0, 'timeout', '连接超时，请检查网络'),
        DioExceptionType.cancel => const ApiError(0, 'cancelled', '已取消'),
        _ => const ApiError(0, 'network', '网络连接失败，请检查网络或服务器地址'),
      };
    }
    return ApiError(0, 'unknown', e.toString());
  }
}

/// Normalises what a user types ("192.168.1.5:8080", "chat.example.com") into a base URL.
String normalizeServer(String input) {
  var s = input.trim();
  if (s.isEmpty) return s;
  if (!s.startsWith('http://') && !s.startsWith('https://')) {
    final isLocal = RegExp(r'^(localhost|127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)').hasMatch(s) || RegExp(r':\d+$').hasMatch(s);
    s = '${isLocal ? 'http' : 'https'}://$s';
  }
  while (s.endsWith('/')) {
    s = s.substring(0, s.length - 1);
  }
  return s;
}

class SseEvent {
  final String event;
  final Map<String, dynamic> data;
  const SseEvent(this.event, this.data);
}

class Api {
  Api(this.baseUrl, {this.token, this.onUnauthorized})
      : dio = Dio(BaseOptions(
          baseUrl: baseUrl,
          connectTimeout: const Duration(seconds: 12),
          receiveTimeout: const Duration(seconds: 60),
          sendTimeout: const Duration(seconds: 60),
          headers: {if (token != null) 'Authorization': 'Bearer $token'},
        )) {
    dio.interceptors.add(InterceptorsWrapper(onError: (e, h) {
      if (e.response?.statusCode == 401 && token != null) onUnauthorized?.call();
      h.next(e);
    }));
  }

  final String baseUrl;
  final String? token;
  final void Function()? onUnauthorized;
  final Dio dio;

  Map<String, String> get authHeaders => {if (token != null) 'Authorization': 'Bearer $token'};
  String fileUrl(String id) => '$baseUrl/api/files/$id';

  Future<dynamic> _req(String method, String path, {Object? body, Map<String, dynamic>? query, CancelToken? cancel}) async {
    try {
      final r = await dio.request(path, data: body, queryParameters: query, cancelToken: cancel, options: Options(method: method));
      return r.data;
    } catch (e) {
      throw ApiError.from(e);
    }
  }

  // ---- auth ----
  Future<bool> ping() async {
    try {
      final r = await dio.get('/healthz', options: Options(receiveTimeout: const Duration(seconds: 5)));
      return r.data is Map && r.data['ok'] == true;
    } catch (_) {
      return false;
    }
  }

  Future<(String, User)> login(String username, String password, String device) async {
    final d = await _req('POST', '/api/auth/login', body: {'username': username, 'password': password, 'device': device});
    return (d['token'] as String, User.fromJson((d['user'] as Map).cast()));
  }

  Future<void> logout() => _req('POST', '/api/auth/logout');

  /// First-run setup: whether this server still has no accounts.
  Future<bool> needsSetup() async {
    try {
      final d = await _req('GET', '/api/setup');
      return d is Map && d['needs_setup'] == true;
    } catch (_) {
      return false; // older servers have no setup endpoint
    }
  }

  Future<(String, User)> setup({required String code, required String username, required String password, required String displayName, required String device}) async {
    final d = await _req('POST', '/api/setup',
        body: {'code': code, 'username': username, 'password': password, 'display_name': displayName, 'device': device});
    return (d['token'] as String, User.fromJson((d['user'] as Map).cast()));
  }

  Future<Bootstrap> bootstrap() async => Bootstrap.fromJson((await _req('GET', '/api/bootstrap') as Map).cast());

  Future<User> updateMe(Map<String, dynamic> patch) async => User.fromJson((await _req('PATCH', '/api/me', body: patch) as Map).cast());

  // ---- conversations ----
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async {
    final d = await _req('GET', '/api/conversations', query: {
      'limit': limit,
      'before': ?before,
      if (q != null && q.isNotEmpty) 'q': q,
    });
    return ((d['items'] as List?) ?? []).map((e) => Conversation.fromJson((e as Map).cast())).toList();
  }

  Future<ConversationDetail> conversation(String id) async {
    final d = (await _req('GET', '/api/conversations/$id') as Map).cast<String, dynamic>();
    return ConversationDetail(
      conversation: Conversation.fromJson((d['conversation'] as Map).cast()),
      messages: ((d['messages'] as List?) ?? []).map((e) => Message.fromJson((e as Map).cast())).toList(),
      hasMore: d['has_more'] == true,
      activeMessageId: (d['active_message_id'] as String?) ?? '',
    );
  }

  Future<(List<Message>, bool)> olderMessages(String convId, String before) async {
    final d = await _req('GET', '/api/conversations/$convId/messages', query: {'before': before, 'limit': 60});
    return (((d['items'] as List?) ?? []).map((e) => Message.fromJson((e as Map).cast())).toList(), d['has_more'] == true);
  }

  Future<Conversation> updateConversation(String id, Map<String, dynamic> patch) async =>
      Conversation.fromJson((await _req('PATCH', '/api/conversations/$id', body: patch) as Map).cast());

  Future<void> deleteConversation(String id) => _req('DELETE', '/api/conversations/$id');

  // ---- chat ----
  Future<SendResult> send({
    String? conversationId,
    String? assistantId,
    String? modelId,
    required String content,
    List<Attachment> attachments = const [],
    String? editOf,
    bool think = false,
  }) async {
    final d = await _req('POST', '/api/chat', body: {
      'conversation_id': ?conversationId,
      'assistant_id': ?assistantId,
      'model_id': ?modelId,
      'content': content,
      'attachments': attachments.map((a) => {'file_id': a.fileId}).toList(),
      'edit_of': ?editOf,
      'think': think,
    });
    return SendResult.fromJson((d as Map).cast());
  }

  Future<SendResult> regenerate(String convId, {String? modelId, bool think = false}) async {
    final d = await _req('POST', '/api/conversations/$convId/regenerate', body: {'model_id': ?modelId, 'think': think});
    return SendResult.fromJson((d as Map).cast());
  }

  Future<void> cancel(String messageId) => _req('POST', '/api/messages/$messageId/cancel');

  /// Opens the SSE stream for a generation. The stream ends after `done`
  /// or throws on network failure (callers reconnect; the server replays a snapshot).
  Stream<SseEvent> stream(String messageId, CancelToken cancel) async* {
    Response<ResponseBody> resp;
    try {
      resp = await dio.get<ResponseBody>(
        '/api/messages/$messageId/stream',
        cancelToken: cancel,
        options: Options(responseType: ResponseType.stream, receiveTimeout: const Duration(seconds: 45), headers: {'Accept': 'text/event-stream'}),
      );
    } catch (e) {
      throw ApiError.from(e);
    }
    var event = 'message';
    final data = StringBuffer();
    try {
      await for (final line in resp.data!.stream.cast<List<int>>().transform(utf8.decoder).transform(const LineSplitter())) {
        if (line.isEmpty) {
          if (data.isNotEmpty) {
            final decoded = jsonDecode(data.toString());
            yield SseEvent(event, decoded is Map ? decoded.cast<String, dynamic>() : {});
          }
          event = 'message';
          data.clear();
        } else if (line.startsWith('event:')) {
          event = line.substring(6).trim();
        } else if (line.startsWith('data:')) {
          if (data.isNotEmpty) data.write('\n');
          data.write(line.substring(5).trimLeft());
        }
      }
    } catch (e) {
      throw ApiError.from(e);
    }
  }

  // ---- files ----
  Future<Attachment> upload(Uint8List bytes, String filename, {void Function(double)? onProgress}) async {
    final form = FormData.fromMap({'file': MultipartFile.fromBytes(bytes, filename: filename)});
    try {
      final r = await dio.post('/api/files', data: form, onSendProgress: (a, b) => onProgress?.call(b > 0 ? a / b : 0));
      final d = (r.data as Map).cast<String, dynamic>();
      return Attachment(fileId: d['id'], mime: d['mime'], width: d['width'] ?? 0, height: d['height'] ?? 0);
    } catch (e) {
      throw ApiError.from(e);
    }
  }

  Future<String> transcribe(String path) async {
    final form = FormData.fromMap({'file': await MultipartFile.fromFile(path, filename: 'voice.m4a')});
    final d = await _req('POST', '/api/audio/transcriptions', body: form);
    return (d['text'] as String?) ?? '';
  }

  // ---- app update ----
  Future<Map<String, dynamic>> appVersion() async => ((await _req('GET', '/api/app/version')) as Map).cast();

  // ---- admin ----
  Future<List<User>> adminUsers() async => ((await _req('GET', '/api/admin/users')) as List).map((e) => User.fromJson((e as Map).cast())).toList();
  Future<User> adminCreateUser(Map<String, dynamic> body) async => User.fromJson((await _req('POST', '/api/admin/users', body: body) as Map).cast());
  Future<User> adminUpdateUser(String id, Map<String, dynamic> body) async =>
      User.fromJson((await _req('PATCH', '/api/admin/users/$id', body: body) as Map).cast());
  Future<void> adminDeleteUser(String id) => _req('DELETE', '/api/admin/users/$id');
  Future<Map<String, dynamic>> adminSettings() async => ((await _req('GET', '/api/admin/settings')) as Map).cast();
  Future<Map<String, dynamic>> adminPutSettings(Map<String, dynamic> body) async => ((await _req('PUT', '/api/admin/settings', body: body)) as Map).cast();
  Future<UsageReport> adminUsage(int days) async => UsageReport.fromJson(((await _req('GET', '/api/admin/usage', query: {'days': days})) as Map).cast());

  // ---- admin: providers & models ----
  static List<T> _list<T>(dynamic d, T Function(Map<String, dynamic>) f) => ((d as List?) ?? const []).map((e) => f((e as Map).cast<String, dynamic>())).toList();

  Future<List<ProviderPreset>> adminPresets() async => _list(await _req('GET', '/api/admin/provider-presets'), ProviderPreset.fromJson);
  Future<List<AdminProvider>> adminProviders() async => _list(await _req('GET', '/api/admin/providers'), AdminProvider.fromJson);
  Future<AdminProvider> adminCreateProvider(Map<String, dynamic> body) async =>
      AdminProvider.fromJson((await _req('POST', '/api/admin/providers', body: body) as Map).cast());
  Future<AdminProvider> adminUpdateProvider(String id, Map<String, dynamic> body) async =>
      AdminProvider.fromJson((await _req('PATCH', '/api/admin/providers/$id', body: body) as Map).cast());
  Future<ModelSettings> adminDeleteProvider(String id) async => ModelSettings.fromJson((await _req('DELETE', '/api/admin/providers/$id') as Map).cast());
  Future<ProviderTestResult> adminTestProvider(Map<String, dynamic> body) async =>
      ProviderTestResult.fromJson((await _req('POST', '/api/admin/providers/test', body: body) as Map).cast());
  Future<List<UpstreamItem>> adminUpstreamModels(String providerId) async =>
      _list(((await _req('GET', '/api/admin/providers/$providerId/upstream-models')) as Map)['items'], UpstreamItem.fromJson);

  Future<List<AdminModel>> adminModels() async => _list(await _req('GET', '/api/admin/models'), AdminModel.fromJson);
  Future<AdminModel> adminCreateModel(Map<String, dynamic> body) async => AdminModel.fromJson((await _req('POST', '/api/admin/models', body: body) as Map).cast());
  Future<AdminModel> adminUpdateModel(String id, Map<String, dynamic> body) async =>
      AdminModel.fromJson((await _req('PATCH', '/api/admin/models/$id', body: body) as Map).cast());
  Future<ModelSettings> adminDeleteModel(String id) async => ModelSettings.fromJson((await _req('DELETE', '/api/admin/models/$id') as Map).cast());
  Future<List<AdminModel>> adminBatchModels(String providerId, List<Map<String, dynamic>> models) async =>
      _list(((await _req('POST', '/api/admin/models/batch', body: {'provider_id': providerId, 'models': models})) as Map)['items'], AdminModel.fromJson);
  Future<List<AdminModel>> adminReorderModels(List<String> ids) async => _list(await _req('PUT', '/api/admin/models/order', body: {'ids': ids}), AdminModel.fromJson);
  Future<ModelTestResult> adminTestModel(String id, {bool think = false}) async {
    try {
      final r = await dio.post('/api/admin/models/$id/test', data: {'think': think}, options: Options(receiveTimeout: const Duration(seconds: 100)));
      return ModelTestResult.fromJson((r.data as Map).cast());
    } catch (e) {
      throw ApiError.from(e);
    }
  }

  Future<ModelSettings> adminModelSettings() async => ModelSettings.fromJson((await _req('GET', '/api/admin/settings') as Map).cast());
  Future<ModelSettings> adminPutModelSettings(Map<String, String> body) async =>
      ModelSettings.fromJson((await _req('PUT', '/api/admin/settings', body: body) as Map).cast());
}

class ConversationDetail {
  final Conversation conversation;
  final List<Message> messages;
  final bool hasMore;
  final String activeMessageId;
  const ConversationDetail({required this.conversation, required this.messages, required this.hasMore, required this.activeMessageId});
}

class SendResult {
  final Conversation conversation;
  final Message? userMessage;
  final Message assistant;
  const SendResult(this.conversation, this.userMessage, this.assistant);
  factory SendResult.fromJson(Map<String, dynamic> j) => SendResult(
        Conversation.fromJson((j['conversation'] as Map).cast()),
        j['user_message'] == null ? null : Message.fromJson((j['user_message'] as Map).cast()),
        Message.fromJson((j['assistant_message'] as Map).cast()),
      );
}

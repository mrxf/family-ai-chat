import 'dart:async';

import 'package:dio/dio.dart';
import 'package:family_chat/core/models/admin_models.dart';
import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/admin/models_page.dart';
import 'package:family_chat/ui/features/home/home_page.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemStorage implements Storage {
  _MemStorage(this.prefs);

  @override
  final SharedPreferences prefs;

  final Map<String, Object?> caches = {};

  @override
  Future<void> clearCache() async => caches.clear();

  @override
  Future<void> deleteCache(String key) async => caches.remove(key);

  @override
  Future<dynamic> readCache(String key) async => caches[key];

  @override
  Future<String?> readToken() async => 'test-token';

  @override
  Future<void> writeCache(String key, Object? value) async => caches[key] = value;

  @override
  Future<void> writeToken(String? token) async {}
}

class _AutoFakeApi extends Api {
  _AutoFakeApi() : super('http://localhost:8080', token: 'test');

  final now = DateTime.now().millisecondsSinceEpoch;
  String? lastSentModelId;
  String nextResolvedModelId = 'm-carbon';
  ModelSettings settings = const ModelSettings(defaultModelId: 'm-deepseek');

  @override
  Future<Map<String, dynamic>> appVersion() async => {'available': false};

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async => const [];

  @override
  Future<List<PinnedComponent>> pinnedComponents() async => const [];

  @override
  Future<List<HomeSuggestion>> homeSuggestions() async => const [];

  @override
  Future<SendResult> send({
    String? conversationId,
    String? assistantId,
    String? modelId,
    required String content,
    List<Attachment> attachments = const [],
    QuotedComponent? quotedComponent,
    String? editOf,
    bool think = false,
    bool isPrivate = false,
  }) async {
    lastSentModelId = modelId;
    final cid = conversationId ?? 'conv-auto-1';
    final aid = 'asst-${DateTime.now().microsecondsSinceEpoch}';
    return SendResult(
      Conversation(id: cid, title: '测试对话', autoMode: true, pinned: false, createdAt: now, updatedAt: now),
      Message(id: 'u-$aid', conversationId: cid, role: 'user', content: content, createdAt: now),
      Message(id: aid, conversationId: cid, role: 'assistant', status: MsgStatus.streaming, createdAt: now),
    );
  }

  @override
  Stream<SseEvent> stream(String messageId, CancelToken cancel) async* {
    final chosen = nextResolvedModelId;
    yield const SseEvent('snapshot', {'content': '', 'reasoning': '', 'status': 'streaming', 'activity': '正在挑选合适的模型…', 'attachments': []});
    yield SseEvent('model', {'conversation_id': 'conv-auto-1', 'model_id': chosen});
    yield const SseEvent('delta', {'content': '这是自动选模后的回答'});
    yield SseEvent('done', {
      'message': {
        'id': messageId,
        'conversation_id': 'conv-auto-1',
        'role': 'assistant',
        'content': '这是自动选模后的回答',
        'status': 'done',
        'model_id': chosen,
        'created_at': now,
      },
    });
  }

  @override
  Future<List<AdminProvider>> adminProviders() async => const [
        AdminProvider(
          id: 'p1',
          kind: 'openrouter',
          name: 'OpenRouter',
          baseUrl: 'https://openrouter.ai/api/v1',
          apiKeyHint: 'sk-****1234',
          endpoint: 'https://openrouter.ai/api/v1/chat/completions',
          keyUrl: '',
          enabled: true,
          sort: 0,
          modelCount: 2,
          enabledModelCount: 2,
        ),
      ];

  @override
  Future<List<AdminModel>> adminModels() async => const [
        AdminModel(
          id: 'm-carbon',
          providerId: 'p1',
          kind: 'chat',
          upstreamModel: 'gemini-4-carbon',
          displayName: 'Carbon',
          description: '旗舰研究',
          routingNote: '医疗问题和研究',
          builtinRoutingNote: 'Gemini Carbon v4.0（顶级旗舰）',
          autoTier: 5,
          defaultEffort: 'low',
          thinkEffort: 'high',
          vision: true,
          tools: true,
          enabled: true,
          contextWindow: 128000,
          maxOutput: 0,
          sort: 0,
          inputPrice: 2,
          outputPrice: 10,
        ),
        AdminModel(
          id: 'm-deepseek',
          providerId: 'p1',
          kind: 'chat',
          upstreamModel: 'deepseek-flash',
          displayName: 'DeepSeek Flash',
          description: '快速省钱',
          routingNote: '日常聊天',
          builtinRoutingNote: 'DeepSeek Flash（快速日常）',
          autoTier: 2,
          defaultEffort: '',
          thinkEffort: '',
          vision: false,
          tools: true,
          enabled: true,
          contextWindow: 128000,
          maxOutput: 0,
          sort: 1,
          inputPrice: 0.1,
          outputPrice: 0.4,
        ),
      ];

  @override
  Future<ModelSettings> adminModelSettings() async => settings;

  @override
  Future<ModelSettings> adminPutModelSettings(Map<String, String> body) async {
    settings = ModelSettings(
      defaultModelId: body['default_model_id'] ?? settings.defaultModelId,
      autoEnabled: body.containsKey('auto_enabled') ? body['auto_enabled'] == '1' : settings.autoEnabled,
      autoDynamic: body.containsKey('auto_dynamic') ? body['auto_dynamic'] == '1' : settings.autoDynamic,
      autoModelId: body['auto_model_id'] ?? settings.autoModelId,
    );
    return settings;
  }

  @override
  Future<Bootstrap> bootstrap() async => _boot(autoOn: settings.autoEnabled, autoDyn: settings.autoDynamic);
}

Bootstrap _boot({required bool autoOn, bool autoDyn = false, String defaultId = 'm-deepseek'}) => Bootstrap(
      user: const User(id: 'u1', username: 'admin', displayName: '管理员', avatar: '🌸', role: 'admin'),
      models: const [
        ChatModel(id: 'm-carbon', displayName: 'Carbon', description: '医疗与研究', vision: true, think: true),
        ChatModel(id: 'm-hydrogen', displayName: 'Gemini 氢气', description: '日常与数学题', vision: true, think: false),
        ChatModel(id: 'm-deepseek', displayName: 'DeepSeek', description: '日常聊天', vision: false, think: false),
      ],
      assistants: const [],
      defaultModelId: defaultId,
      voiceInput: false,
      autoModel: autoOn,
      autoDynamic: autoDyn,
    );

class _StubSession extends SessionNotifier {
  _StubSession(this._api, this._initialBoot);
  final Api _api;
  final Bootstrap _initialBoot;

  @override
  Session build() => Session(
        status: SessionStatus.loggedIn,
        server: 'http://localhost:8080',
        api: _api,
        boot: _initialBoot,
      );
}

void main() {
  testWidgets('Auto option hidden when off, shown when enabled and updates resolved model live', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _MemStorage(prefs);
    final api = _AutoFakeApi();

    // 1. Auto mode disabled -> picker does NOT show 自动选模型.
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(storage),
          sessionProvider.overrideWith(() => _StubSession(api, _boot(autoOn: false))),
        ],
        child: MaterialApp(theme: buildTheme(Brightness.light), home: const HomePage()),
      ),
    );
    await tester.pump(const Duration(seconds: 3));
    await tester.tap(find.text('DeepSeek'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('自动选模型'), findsNothing);
    Navigator.of(tester.element(find.byType(HomePage))).pop();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));

    // 2. Auto mode enabled -> picker shows 自动选模型, sending resolves to Carbon, then dynamically switches to Gemini 氢气.
    await tester.pumpWidget(
      ProviderScope(
        key: const ValueKey('enabled'),
        overrides: [
          storageProvider.overrideWithValue(storage),
          sessionProvider.overrideWith(() => _StubSession(api, _boot(autoOn: true, autoDyn: true))),
        ],
        child: MaterialApp(theme: buildTheme(Brightness.light), home: const HomePage()),
      ),
    );
    await tester.pump(const Duration(seconds: 3));

    await tester.tap(find.text('DeepSeek'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('自动选模型'), findsOneWidget);
    await tester.tap(find.text('自动选模型'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('自动选模型'), findsOneWidget);

    // First turn routes to Carbon.
    api.nextResolvedModelId = 'm-carbon';
    await tester.enterText(find.byType(TextField), '帮我解读体检报告');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.byTooltip('发送'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(api.lastSentModelId, kAutoModelId);
    expect(find.text('自动 · Carbon'), findsOneWidget);

    // Second turn dynamically switches to Gemini 氢气.
    api.nextResolvedModelId = 'm-hydrogen';
    await tester.enterText(find.byType(TextField), '帮孩子讲一道三年级数学题');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.byTooltip('发送'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('自动 · Gemini 氢气'), findsOneWidget);
  });

  testWidgets('Admin ModelsPage prompts for judge model when enabling Auto mode', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _MemStorage(prefs);
    final api = _AutoFakeApi();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(storage),
          sessionProvider.overrideWith(() => _StubSession(api, _boot(autoOn: false))),
        ],
        child: MaterialApp(theme: buildTheme(Brightness.light), home: const ModelsPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('开启 Auto 模式'), findsOneWidget);

    // Toggling Auto on opens the judge model chooser.
    await tester.tap(find.text('开启 Auto 模式'));
    await tester.pumpAndSettle();
    expect(find.textContaining('选择 Auto 的判断模型'), findsOneWidget);

    await tester.tap(find.text('DeepSeek Flash').last);
    await tester.pumpAndSettle();

    expect(api.settings.autoEnabled, isTrue);
    expect(api.settings.autoModelId, 'm-deepseek');
    expect(find.text('对话中动态调整模型'), findsOneWidget);

    // Scroll down to verify model row displays admin's routing_note.
    await tester.scrollUntilVisible(find.text('适用场景：医疗问题和研究'), 200, scrollable: find.byType(Scrollable).first);
    expect(find.text('适用场景：医疗问题和研究'), findsOneWidget);
  });
}

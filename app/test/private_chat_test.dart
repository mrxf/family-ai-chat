import 'dart:async';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/home/home_page.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _RecordingStorage implements Storage {
  _RecordingStorage(this.prefs);

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

class _PrivateTestApi extends Api {
  _PrivateTestApi() : super('http://localhost:8080', token: 'test');

  final List<({String content, bool isPrivate, String? conversationId})> sent = [];
  final List<String> deletedConversations = [];
  Completer<SendResult>? pendingSend;
  int _seq = 0;

  @override
  Future<Map<String, dynamic>> appVersion() async => {'available': false};

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async => const [];

  @override
  Future<void> deleteConversation(String id) async {
    deletedConversations.add(id);
  }

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
    sent.add((content: content, isPrivate: isPrivate, conversationId: conversationId));
    if (pendingSend != null) {
      return pendingSend!.future;
    }
    final n = ++_seq;
    final cid = conversationId ?? (isPrivate ? 'priv-$n' : 'norm-$n');
    final now = DateTime.now().millisecondsSinceEpoch;
    return SendResult(
      Conversation(
        id: cid,
        title: '',
        assistantId: assistantId,
        modelId: modelId,
        pinned: false,
        isPrivate: isPrivate,
        createdAt: now,
        updatedAt: now,
      ),
      Message(id: 'u-$n', conversationId: cid, role: 'user', content: content, createdAt: now),
      Message(id: 'a-$n', conversationId: cid, role: 'assistant', status: MsgStatus.streaming, createdAt: now),
    );
  }

  @override
  Stream<SseEvent> stream(String messageId, CancelToken cancel) async* {
    yield const SseEvent('snapshot', {'content': '这是隐私回答。', 'reasoning': '', 'status': 'streaming', 'attachments': []});
    yield SseEvent('done', {
      'message': {
        'id': messageId,
        'conversation_id': 'priv-1',
        'role': 'assistant',
        'content': '这是隐私回答。',
        'reasoning': '',
        'attachments': const [],
        'status': 'done',
      },
    });
  }
}

class _StubSession extends SessionNotifier {
  _StubSession(this._api);
  final Api _api;

  @override
  Session build() => Session(
        status: SessionStatus.loggedIn,
        server: 'http://localhost:8080',
        api: _api,
        boot: const Bootstrap(
          user: User(id: 'u1', username: 'mama', displayName: '妈妈', avatar: '🌸', role: 'member'),
          models: [
            ChatModel(id: 'm1', displayName: 'GPT-6.1 Terra', description: '日常首选', vision: true, think: true),
          ],
          assistants: [
            Assistant(
              id: 'builtin-health',
              name: '健康顾问',
              avatar: '🩺',
              description: '看懂体检报告',
              greeting: '体检报告看不懂可以问我。',
              suggestions: ['帮我看看血常规'],
            ),
          ],
          defaultModelId: 'm1',
          voiceInput: false,
        ),
      );
}

void main() {
  testWidgets('private chat lifecycle: enter, pick assistant, send without caching, and delete on exit (1.3x font scale)', (tester) async {
    SharedPreferences.setMockInitialValues({'fontScale': 1.3});
    final prefs = await SharedPreferences.getInstance();
    final storage = _RecordingStorage(prefs);
    final api = _PrivateTestApi();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(storage),
          sessionProvider.overrideWith(() => _StubSession(api)),
        ],
        child: MaterialApp(
          theme: buildTheme(Brightness.light),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(textScaler: const TextScaler.linear(1.3)),
            child: child!,
          ),
          home: const HomePage(),
        ),
      ),
    );
    // Drain HomePage's 2-second silent update check timer (normal Welcome has a continuous Floating animation).
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    // Normal welcome screen initially: no private-chat button in top bar, dashed entry at bottom.
    expect(find.text('小暖'), findsOneWidget);
    expect(find.byTooltip('隐私对话'), findsNothing);
    expect(find.byTooltip('分享对话'), findsNothing);

    // Drawer only shows the primary 新对话 button (no private-mode button next to it).
    await tester.tap(find.byTooltip('聊天记录'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.text('新对话'), findsOneWidget);
    expect(find.byTooltip('新建隐私对话'), findsNothing);
    expect(find.text('隐私'), findsNothing);
    await tester.tap(find.byTooltip('关闭'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    // Scroll to the bottom of Welcome and tap the dashed-border 开启隐私对话 entry card.
    await tester.scrollUntilVisible(find.text('开启隐私对话'), 220, scrollable: find.byType(Scrollable).first);
    await tester.tap(find.text('开启隐私对话'));
    await tester.pumpAndSettle();

    expect(find.text('隐私对话'), findsAtLeastNWidgets(1));
    expect(find.text('退出'), findsOneWidget);
    expect(find.text('不进聊天记录'), findsOneWidget);
    expect(find.text('不读取也不写入记忆'), findsOneWidget);
    expect(find.text('关闭即清理'), findsOneWidget);
    expect(find.text('在隐私对话中提问…'), findsOneWidget);

    // Selecting an assistant inside private mode stays in private mode.
    await tester.scrollUntilVisible(find.text('健康顾问'), 200, scrollable: find.byType(Scrollable).first);
    await tester.tap(find.text('健康顾问'));
    await tester.pumpAndSettle();
    expect(find.text('隐私对话 · 健康顾问'), findsAtLeastNWidgets(1));

    // Send a question in private mode.
    await tester.enterText(find.byType(TextField), '记住我想给老爸买按摩椅');
    await tester.pump();
    await tester.tap(find.byTooltip('发送'));
    await tester.pumpAndSettle();

    expect(api.sent.length, 1);
    expect(api.sent.first.isPrivate, isTrue);
    expect(find.text('这是隐私回答。'), findsOneWidget);
    expect(find.text('隐私对话中 · 离开后记录与图片自动清除，小暖不会记住'), findsOneWidget);

    // Neither conversationsProvider nor local Storage cache received the private chat.
    final container = ProviderScope.containerOf(tester.element(find.byType(HomePage)));
    expect(container.read(conversationsProvider).items, isEmpty);
    expect(storage.caches.keys.where((k) => k.startsWith('conv_')), isEmpty);

    // Type an unsent draft, then tap 退出: server DELETE is called immediately and draft is cleared.
    await tester.enterText(find.byType(TextField), '半截没发出去的隐私草稿');
    await tester.pump();
    await tester.tap(find.text('退出'));
    await tester.pump(const Duration(milliseconds: 500));

    expect(api.deletedConversations, ['priv-1']);
    expect(find.text('小暖'), findsOneWidget);
    expect(find.text('半截没发出去的隐私草稿'), findsNothing);
  });

  test('in-flight private send deletes server conversation if user exits before send resolves', () async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _RecordingStorage(prefs);
    final api = _PrivateTestApi()..pendingSend = Completer<SendResult>();

    final container = ProviderContainer(
      overrides: [
        storageProvider.overrideWithValue(storage),
        sessionProvider.overrideWith(() => _StubSession(api)),
      ],
    );
    addTearDown(container.dispose);

    final chat = container.read(chatProvider.notifier);
    chat.newChat(isPrivate: true);
    final sendFuture = chat.send('正在发送中的隐私问题');

    // User immediately exits private chat before HTTP POST /api/chat returns.
    chat.exitPrivateChat();
    expect(container.read(chatProvider).isPrivate, isFalse);

    final now = DateTime.now().millisecondsSinceEpoch;
    api.pendingSend!.complete(SendResult(
      Conversation(id: 'priv-race', title: '', pinned: false, isPrivate: true, createdAt: now, updatedAt: now),
      Message(id: 'u-r', conversationId: 'priv-race', role: 'user', content: '正在发送中的隐私问题', createdAt: now),
      Message(id: 'a-r', conversationId: 'priv-race', role: 'assistant', status: MsgStatus.streaming, createdAt: now),
    ));
    await sendFuture;
    await Future<void>.delayed(Duration.zero);

    expect(api.deletedConversations, ['priv-race']);
    expect(container.read(conversationsProvider).items, isEmpty);
  });

  test('closing private chat unlinks local compressed temp images', () async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _RecordingStorage(prefs);
    final api = _PrivateTestApi();
    final tmp = await Directory.systemTemp.createTemp('nuan_priv_test_');
    final imgFile = File('${tmp.path}/nuan_temp.jpg')..writeAsBytesSync([1, 2, 3]);
    addTearDown(() => tmp.delete(recursive: true).catchError((_) => tmp));

    final container = ProviderContainer(
      overrides: [
        storageProvider.overrideWithValue(storage),
        sessionProvider.overrideWith(() => _StubSession(api)),
      ],
    );
    addTearDown(container.dispose);

    final chat = container.read(chatProvider.notifier);
    chat.newChat(isPrivate: true);
    // Simulate a private chat message holding a local temp file path.
    chat.state = chat.state.copyWith(
      conversationId: 'priv-img',
      messages: [
        Message(
          id: 'u1',
          conversationId: 'priv-img',
          role: 'user',
          content: '图',
          attachments: [Attachment(fileId: 'f1', mime: 'image/jpeg', localPath: imgFile.path)],
        ),
      ],
    );

    chat.exitPrivateChat();
    await Future<void>.delayed(const Duration(milliseconds: 30));

    expect(api.deletedConversations, ['priv-img']);
    expect(imgFile.existsSync(), isFalse);
  });
}

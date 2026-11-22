import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/home/home_page.dart';
import 'package:family_chat/ui/features/share/share_page.dart';
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

class _ShareTestApi extends Api {
  _ShareTestApi() : super('http://localhost:8080', token: 'test');

  @override
  Future<Map<String, dynamic>> appVersion() async => {'available': false};

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async => const [];

  @override
  Future<List<PinnedComponent>> pinnedComponents() async => const [];

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
    const cid = 'conv-share-1';
    final now = DateTime(2026, 11, 18, 10, 30).millisecondsSinceEpoch;
    return SendResult(
      Conversation(id: cid, title: '家常红烧排骨做法', pinned: false, createdAt: now, updatedAt: now),
      Message(id: 'u-1', conversationId: cid, role: 'user', content: content, createdAt: now),
      Message(id: 'a-1', conversationId: cid, role: 'assistant', status: MsgStatus.streaming, createdAt: now),
    );
  }

  @override
  Stream<SseEvent> stream(String messageId, CancelToken cancel) async* {
    const answer = '### 家常红烧排骨\n\n1. **冷水焯水**：排骨加姜片料酒去腥；\n2. **小火炒糖色**：冰糖炒至琥珀色下排骨翻炒；\n3. **慢炖收汁**：加热水小火焖 40 分钟，大火收浓汤汁即可。';
    yield const SseEvent('snapshot', {'content': answer, 'reasoning': '', 'status': 'streaming', 'attachments': []});
    yield SseEvent('done', {
      'message': {
        'id': messageId,
        'conversation_id': 'conv-share-1',
        'role': 'assistant',
        'content': answer,
        'reasoning': '先整理步骤让长辈一看就会。',
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
          assistants: [],
          defaultModelId: 'm1',
          voiceInput: false,
        ),
      );
}

void main() {
  testWidgets('share conversation flow: top-right entry, message selection, theme switch, and tap-to-save PNG', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _MemStorage(prefs);
    final api = _ShareTestApi();
    final savedImages = <Uint8List>[];
    debugShareSaveOverride = (bytes) async => savedImages.add(bytes);
    addTearDown(() => debugShareSaveOverride = null);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(storage),
          sessionProvider.overrideWith(() => _StubSession(api)),
        ],
        child: MaterialApp(
          theme: buildTheme(Brightness.light),
          home: const HomePage(),
        ),
      ),
    );
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 300));

    // Empty new-chat page: no share button in top bar, dashed private-chat card at bottom.
    expect(find.byTooltip('分享对话'), findsNothing);
    expect(find.text('开启隐私对话'), findsOneWidget);

    // Send a message to enter the conversation page.
    await tester.enterText(find.byType(TextField), '教我做一道简单的红烧排骨');
    await tester.pump();
    await tester.tap(find.byTooltip('发送'));
    await tester.pumpAndSettle();

    // Ensure conversation title is in conversationsProvider.
    final container = ProviderScope.containerOf(tester.element(find.byType(HomePage)));
    container.read(conversationsProvider.notifier).upsertTop(
          const Conversation(id: 'conv-share-1', title: '家常红烧排骨做法', pinned: false, createdAt: 1, updatedAt: 2),
        );
    await tester.pump();

    // Top-right now displays the share button!
    expect(find.byTooltip('分享对话'), findsOneWidget);
    await tester.tap(find.byTooltip('分享对话'));
    await tester.pumpAndSettle();

    // Step 1: Selection page shows both messages selected by default.
    expect(find.text('选择分享内容'), findsOneWidget);
    expect(find.text('已选 2 条对话'), findsOneWidget);

    // Toggle select-all off and back on.
    await tester.tap(find.text('取消全选'));
    await tester.pump();
    expect(find.text('请勾选要分享的对话'), findsOneWidget);

    await tester.tap(find.text('全选'));
    await tester.pump();
    expect(find.text('已选 2 条对话'), findsOneWidget);

    // Proceed to Step 2: Poster preview.
    await tester.tap(find.text('生成分享图片'));
    await tester.pumpAndSettle();

    expect(find.text('小暖 · AI 对话分享'), findsOneWidget);
    expect(find.text('家常红烧排骨做法'), findsOneWidget);
    expect(find.text('小暖 · 家人的贴心 AI 伙伴'), findsOneWidget);

    // Switch poster theme to 晴杏 and 夜阑.
    await tester.tap(find.text('晴杏'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('夜阑'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('暖阳'));
    await tester.pumpAndSettle();

    // Tap directly on the generated poster card to save locally (runAsync needed for boundary.toImage).
    await tester.runAsync(() async {
      final cardFinder = find.byType(SharePosterCard);
      await tester.tap(cardFinder);
      await Future<void>.delayed(const Duration(milliseconds: 250));
    });
    await tester.pumpAndSettle();

    expect(savedImages.length, 1);
    // Verify valid PNG header: 89 50 4E 47 0D 0A 1A 0A.
    expect(savedImages.first.sublist(0, 8), equals([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]));
    expect(find.textContaining('已保存到相册'), findsAtLeastNWidgets(1));
  });
}

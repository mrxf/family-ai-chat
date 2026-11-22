import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/app/navigation.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/incoming_share.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/home/home_page.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Valid 1x1 transparent PNG.
final _tinyPng = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
);

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

  final List<({String content, List<Attachment> attachments, bool isPrivate, String? assistantId, String? conversationId})> sent = [];
  int _seq = 0;

  @override
  Future<Map<String, dynamic>> appVersion() async => {'available': false};

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async => const [];

  @override
  Future<void> deleteConversation(String id) async {}

  @override
  Future<Attachment> upload(Uint8List bytes, String filename, {bool isPrivate = false, void Function(double)? onProgress}) async {
    final n = ++_seq;
    return Attachment(fileId: 'file-$n', mime: 'image/png', width: 1, height: 1);
  }

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
    sent.add((
      content: content,
      attachments: attachments,
      isPrivate: isPrivate,
      assistantId: assistantId,
      conversationId: conversationId,
    ));
    final n = ++_seq;
    final cid = conversationId ?? 'conv-$n';
    final now = DateTime.now().millisecondsSinceEpoch;
    return SendResult(
      Conversation(
        id: cid,
        title: '测试对话',
        assistantId: assistantId,
        modelId: modelId,
        pinned: false,
        isPrivate: isPrivate,
        createdAt: now,
        updatedAt: now,
      ),
      Message(id: 'u-$n', conversationId: cid, role: 'user', content: content, attachments: attachments, createdAt: now),
      Message(id: 'a-$n', conversationId: cid, role: 'assistant', status: MsgStatus.streaming, createdAt: now),
    );
  }

  @override
  Stream<SseEvent> stream(String messageId, CancelToken cancel) async* {
    yield const SseEvent('snapshot', {'content': '这是一朵绣球花。', 'reasoning': '', 'status': 'streaming', 'attachments': []});
    yield SseEvent('done', {
      'message': {
        'id': messageId,
        'conversation_id': 'conv-1',
        'role': 'assistant',
        'content': '这是一朵绣球花。',
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
            ChatModel(id: 'm2', displayName: '纯文本模型', description: '不支持看图', vision: false, think: false),
          ],
          assistants: [
            Assistant(
              id: 'builtin-antifraud',
              name: '防骗卫士',
              avatar: '🛡️',
              description: '识别可疑信息',
              greeting: '发给我看看靠不靠谱。',
              suggestions: ['帮我看看这条短信'],
            ),
          ],
          defaultModelId: 'm1',
          voiceInput: false,
        ),
      );
}

Future<Directory> _makeSharedDir() async {
  final root = await Directory.systemTemp.createTemp('nuan_share_test_');
  final dir = Directory('${root.path}/shared_incoming');
  await dir.create(recursive: true);
  return dir;
}

/// Pumps interleaved real IO (File / ImageCodec) and FakeAsync microtasks.
Future<void> _pumpShareIo(WidgetTester tester) async {
  for (var i = 0; i < 45; i++) {
    await tester.pump();
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
  }
  await tester.pump();
}

void main() {
  const shareChannel = MethodChannel('com.nuan.chat/share');

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(shareChannel, null);
  });

  testWidgets('cold-start shared image and text populate composer without auto-sending and clean up temp file', (tester) async {
    final sharedDir = await tester.runAsync(_makeSharedDir);
    addTearDown(() => sharedDir!.parent.delete(recursive: true).catchError((_) => sharedDir.parent));
    final imgFile = File('${sharedDir!.path}/share_1_0.png');
    await tester.runAsync(() => imgFile.writeAsBytes(_tinyPng));

    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(shareChannel, (call) async {
      if (call.method == 'takePendingShare') {
        return <String, Object>{
          'text': '帮我看下链接 https://example.com',
          'images': <String>[imgFile.path],
          'totalImages': 1,
          'failedImages': 0,
        };
      }
      return null;
    });

    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final api = _ShareTestApi();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(_MemStorage(prefs)),
          sessionProvider.overrideWith(() => _StubSession(api)),
        ],
        child: MaterialApp(
          navigatorKey: navigatorKey,
          theme: buildTheme(Brightness.light),
          home: const HomePage(),
        ),
      ),
    );

    // Allow initial MethodChannel call -> _consumeIncomingShare -> applyShared -> real file/codec IO to settle.
    await _pumpShareIo(tester);
    await tester.pump(const Duration(seconds: 3));

    // Not auto-sent: user can edit or append "这是啥" before sending.
    expect(api.sent, isEmpty);
    final tf = tester.widget<TextField>(find.byType(TextField));
    expect(tf.controller!.text, '帮我看下链接 https://example.com');
    expect(find.text('想问这张图片什么？'), findsOneWidget);
    expect(find.byType(Image), findsWidgets);

    // Raw shared_incoming temp file was cleaned up after import.
    final exists = await tester.runAsync(() => imgFile.exists());
    expect(exists, isFalse);

    // User adds question and taps Send.
    await tester.enterText(find.byType(TextField), '这是啥花？');
    await tester.pump();
    // Wait for the bottom SnackBar toast (2.2s) to dismiss before tapping Send underneath it.
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.byTooltip('发送'));
    await tester.pumpAndSettle();

    expect(api.sent.length, 1);
    expect(api.sent.first.content, '这是啥花？');
    expect(api.sent.first.attachments.length, 1);
  });

  testWidgets('sharing while in an active conversation pops stacked routes and opens a fresh new chat', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final api = _ShareTestApi();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(_MemStorage(prefs)),
          sessionProvider.overrideWith(() => _StubSession(api)),
        ],
        child: MaterialApp(
          navigatorKey: navigatorKey,
          theme: buildTheme(Brightness.light),
          home: const HomePage(),
        ),
      ),
    );
    await tester.pump(const Duration(seconds: 3));

    // Start an existing conversation first.
    await tester.enterText(find.byType(TextField), '今天晚上吃什么');
    await tester.pump();
    // Wait for the bottom SnackBar toast (2.2s) to dismiss before tapping Send underneath it.
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.byTooltip('发送'));
    await tester.pumpAndSettle();

    final container = ProviderScope.containerOf(tester.element(find.byType(HomePage)));
    expect(container.read(chatProvider).conversationId, isNotNull);
    expect(container.read(chatProvider).messages, isNotEmpty);

    // Push a secondary route on top of HomePage (simulating Settings / Memories screen).
    navigatorKey.currentState!.push(MaterialPageRoute<void>(builder: (_) => const Scaffold(body: Text('设置页'))));
    await tester.pumpAndSettle();
    expect(find.text('设置页'), findsOneWidget);

    // Simulate incoming "问小暖" / text share from another app.
    // Note: normal Welcome has a continuous floating mascot animation, so pump fixed durations instead of pumpAndSettle.
    container.read(incomingShareProvider.notifier).push(
          const SharedPayload(text: '阿莫西林胶囊一天吃几次'),
        );
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();

    // Stacked route popped, chat reset to a brand new conversation, text placed in composer without sending.
    expect(find.text('设置页'), findsNothing);
    expect(container.read(chatProvider).conversationId, isNull);
    expect(container.read(chatProvider).messages, isEmpty);
    expect(api.sent.length, 1); // Still only the earlier message.

    final tf = tester.widget<TextField>(find.byType(TextField));
    expect(tf.controller!.text, '阿莫西林胶囊一天吃几次');
  });

  testWidgets('sharing into an empty assistant or empty private chat keeps current mode and appends consecutive WeChat images', (tester) async {
    final sharedDir = await tester.runAsync(_makeSharedDir);
    addTearDown(() => sharedDir!.parent.delete(recursive: true).catchError((_) => sharedDir.parent));
    final img1 = File('${sharedDir!.path}/share_1.png');
    final img2 = File('${sharedDir.path}/share_2.png');
    await tester.runAsync(() async {
      await img1.writeAsBytes(_tinyPng);
      await img2.writeAsBytes(_tinyPng);
    });

    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final api = _ShareTestApi();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(_MemStorage(prefs)),
          sessionProvider.overrideWith(() => _StubSession(api)),
        ],
        child: MaterialApp(
          navigatorKey: navigatorKey,
          theme: buildTheme(Brightness.light),
          home: const HomePage(),
        ),
      ),
    );
    await tester.pump(const Duration(seconds: 3));

    // Enter empty private chat + pick 防骗卫士 assistant.
    await tester.scrollUntilVisible(find.text('开启隐私对话'), 220, scrollable: find.byType(Scrollable).first);
    await tester.tap(find.text('开启隐私对话'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('防骗卫士'), 200, scrollable: find.byType(Scrollable).first);
    await tester.tap(find.text('防骗卫士'));
    await tester.pumpAndSettle();

    final container = ProviderScope.containerOf(tester.element(find.byType(HomePage)));
    expect(container.read(chatProvider).isPrivate, isTrue);
    expect(container.read(chatProvider).assistantId, 'builtin-antifraud');

    // Share first image from WeChat ("用其他应用打开").
    container.read(incomingShareProvider.notifier).push(SharedPayload(imagePaths: [img1.path], totalImages: 1));
    await _pumpShareIo(tester);

    // Share second image from WeChat before sending.
    container.read(incomingShareProvider.notifier).push(SharedPayload(imagePaths: [img2.path], totalImages: 1));
    await _pumpShareIo(tester);

    // Still in empty private chat with 防骗卫士, and both images are attached!
    expect(container.read(chatProvider).isPrivate, isTrue);
    expect(container.read(chatProvider).assistantId, 'builtin-antifraud');
    expect(find.text('想问这张图片什么？（离开后清除）'), findsOneWidget);

    // Wait for the bottom SnackBar toast (2.2s) to dismiss before tapping Send underneath it.
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.byTooltip('发送'));
    await tester.pumpAndSettle();
    expect(api.sent.length, 1);
    expect(api.sent.first.isPrivate, isTrue);
    expect(api.sent.first.assistantId, 'builtin-antifraud');
    expect(api.sent.first.attachments.length, 2);
  });

  testWidgets('sharing more than 9 images caps at 9, cleans excess temp files, and shows friendly toast', (tester) async {
    final sharedDir = await tester.runAsync(_makeSharedDir);
    addTearDown(() => sharedDir!.parent.delete(recursive: true).catchError((_) => sharedDir.parent));
    final paths = <String>[];
    await tester.runAsync(() async {
      for (var i = 0; i < 11; i++) {
        final f = File('${sharedDir!.path}/share_cap_$i.png');
        await f.writeAsBytes(_tinyPng);
        paths.add(f.path);
      }
    });

    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final api = _ShareTestApi();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          storageProvider.overrideWithValue(_MemStorage(prefs)),
          sessionProvider.overrideWith(() => _StubSession(api)),
        ],
        child: MaterialApp(
          navigatorKey: navigatorKey,
          theme: buildTheme(Brightness.light),
          home: const HomePage(),
        ),
      ),
    );
    await tester.pump(const Duration(seconds: 3));

    final container = ProviderScope.containerOf(tester.element(find.byType(HomePage)));
    // First share 6 images, then share 5 more (6 + 5 = 11 > 9).
    container.read(incomingShareProvider.notifier).push(
          SharedPayload(imagePaths: paths.sublist(0, 6), totalImages: 6),
        );
    await _pumpShareIo(tester);

    container.read(incomingShareProvider.notifier).push(
          SharedPayload(imagePaths: paths.sublist(6), totalImages: 5),
        );
    await _pumpShareIo(tester);
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('最多同时发 9 张图片，已选取前 9 张'), findsOneWidget);

    // Excess 10th and 11th shared_incoming temp files were also deleted.
    final tenthExists = await tester.runAsync(() => File(paths[9]).exists());
    final eleventhExists = await tester.runAsync(() => File(paths[10]).exists());
    expect(tenthExists, isFalse);
    expect(eleventhExists, isFalse);

    // Wait for the bottom SnackBar toast (2.2s) to dismiss before tapping Send underneath it.
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.byTooltip('发送'));
    await tester.pumpAndSettle();
    expect(api.sent.length, 1);
    expect(api.sent.first.attachments.length, 9);
  });
}

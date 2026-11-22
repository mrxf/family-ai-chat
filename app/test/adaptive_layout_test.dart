import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/platform/platform_capabilities.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/desktop/desktop_home_layout.dart';
import 'package:family_chat/ui/features/chat/composer/composer.dart';
import 'package:family_chat/ui/features/conversations/conversation_panel.dart';
import 'package:family_chat/ui/features/conversations/search_page.dart';
import 'package:family_chat/ui/features/home/home_page.dart';
import 'package:family_chat/ui/mobile/mobile_home_layout.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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

class _FakeApi extends Api {
  _FakeApi() : super('http://localhost:8080', token: 'test');

  final now = DateTime.now().millisecondsSinceEpoch;
  final sent = <String>[];

  @override
  Future<Map<String, dynamic>> appVersion() async => {'available': false};

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async => before != null
      ? const []
      : [Conversation(id: 'c1', title: '周末去哪玩', pinned: false, isPrivate: false, createdAt: now, updatedAt: now)];

  @override
  Future<List<PinnedComponent>> pinnedComponents() async => const [];

  @override
  Future<ConversationDetail> conversation(String id, {String? focusMessageId}) async => ConversationDetail(
        conversation: Conversation(id: id, title: '周末去哪玩', pinned: false, isPrivate: false, createdAt: now, updatedAt: now),
        messages: [
          Message(id: 'u1', conversationId: id, role: 'user', content: '周末去哪', createdAt: now),
          Message(id: 'a1', conversationId: id, role: 'assistant', content: '可以去公园走走', createdAt: now),
        ],
        hasMore: false,
        activeMessageId: '',
      );

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
    sent.add(content);
    throw const ApiError(503, 'offline', '测试环境不发送');
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
          user: User(id: 'u1', username: 'user', displayName: '测试用户', avatar: '🌸', role: 'member'),
          models: [ChatModel(id: 'm1', displayName: '默认模型', description: '', vision: true, think: true)],
          assistants: [],
          defaultModelId: 'm1',
          voiceInput: false,
        ),
      );
}

Future<_FakeApi> _pumpHome(WidgetTester tester, {required Size size, TargetPlatform platform = TargetPlatform.android}) async {
  SharedPreferences.setMockInitialValues({});
  final prefs = await SharedPreferences.getInstance();
  final api = _FakeApi();
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        storageProvider.overrideWithValue(_MemStorage(prefs)),
        sessionProvider.overrideWith(() => _StubSession(api)),
        platformCapabilitiesProvider.overrideWithValue(PlatformCapabilities.of(platform)),
      ],
      child: MaterialApp(theme: buildTheme(Brightness.light), home: const HomePage()),
    ),
  );
  await tester.pump(const Duration(seconds: 3));
  await tester.pump(const Duration(milliseconds: 300));
  return api;
}

Finder get _composerField => find.descendant(of: find.byType(Composer), matching: find.byType(TextField));

void main() {
  testWidgets('compact window: chat fills the screen, history lives in a drawer', (tester) async {
    await _pumpHome(tester, size: const Size(400, 800));
    expect(find.byType(MobileHomeLayout), findsOneWidget);
    expect(find.byType(ConversationPanel), findsNothing);
    expect(find.byTooltip('聊天记录'), findsOneWidget);
  });

  testWidgets('expanded window: permanent sidebar, no drawer button', (tester) async {
    await _pumpHome(tester, size: const Size(1280, 800), platform: TargetPlatform.macOS);
    expect(find.byType(DesktopHomeLayout), findsOneWidget);
    expect(find.byType(ConversationPanel), findsOneWidget);
    expect(find.byType(HistoryDrawer), findsNothing);
    expect(find.byTooltip('聊天记录'), findsNothing);
    expect(find.byTooltip('关闭'), findsNothing, reason: 'the sidebar cannot be closed');
    expect(find.text('周末去哪玩'), findsOneWidget);
  });

  testWidgets('resizing across the breakpoint keeps the unsent draft', (tester) async {
    await _pumpHome(tester, size: const Size(1280, 800), platform: TargetPlatform.macOS);
    await tester.enterText(_composerField, '还没发出去的草稿');
    await tester.pump();

    tester.view.physicalSize = const Size(420, 800);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.byType(MobileHomeLayout), findsOneWidget);
    expect(tester.widget<TextField>(_composerField).controller!.text, '还没发出去的草稿');
  });

  testWidgets('desktop: Enter sends, Shift+Enter does not', (tester) async {
    final api = await _pumpHome(tester, size: const Size(1280, 800), platform: TargetPlatform.macOS);
    await tester.tap(_composerField);
    await tester.enterText(_composerField, '第一行');
    await tester.pump();

    await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
    await tester.pump();
    expect(api.sent, isEmpty);

    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pump(const Duration(milliseconds: 300));
    expect(api.sent, ['第一行']);
  });

  testWidgets('desktop: Cmd+K opens history search on macOS', (tester) async {
    await _pumpHome(tester, size: const Size(1280, 800), platform: TargetPlatform.macOS);
    await tester.sendKeyDownEvent(LogicalKeyboardKey.metaLeft);
    await tester.sendKeyEvent(LogicalKeyboardKey.keyK);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.metaLeft);
    await tester.pumpAndSettle();
    expect(find.byType(SearchPage), findsOneWidget);
  });

  testWidgets('phones keep Enter for the keyboard; only the send action sends', (tester) async {
    final api = await _pumpHome(tester, size: const Size(400, 800));
    await tester.tap(_composerField);
    await tester.enterText(_composerField, '你好');
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pump(const Duration(milliseconds: 300));
    expect(api.sent, isEmpty);
  });

  for (final (label, size, minRight) in [('phone', const Size(400, 800), 340.0), ('desktop', const Size(1280, 800), 1150.0)]) {
    testWidgets('$label: user bubbles hug the right edge of the chat column', (tester) async {
      await _pumpHome(tester, size: size, platform: size.width > 840 ? TargetPlatform.macOS : TargetPlatform.android);
      final container = ProviderScope.containerOf(tester.element(find.byType(HomePage)));
      await container.read(chatProvider.notifier).open('c1');
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pump(const Duration(milliseconds: 500));
      final user = tester.getRect(find.text('周末去哪'));
      final reply = tester.getRect(find.text('可以去公园走走'));
      expect(user.right, greaterThan(minRight), reason: 'right-aligned, not centered');
      expect(reply.left, lessThan(user.left), reason: 'assistant text starts on the left');
    });
  }

  test('history is bucketed by pin state and recency', () {
    final now = DateTime(2026, 11, 21, 15);
    int at(DateTime d) => d.millisecondsSinceEpoch;
    Conversation c(String id, DateTime d, {bool pinned = false}) =>
        Conversation(id: id, title: id, pinned: pinned, isPrivate: false, createdAt: at(d), updatedAt: at(d));
    final groups = groupByRecency([
      c('today', DateTime(2026, 11, 21, 9)),
      c('pinned-old', DateTime(2025, 1, 1), pinned: true),
      c('yesterday', DateTime(2026, 11, 20, 23)),
      c('week', DateTime(2026, 11, 16)),
      c('month', DateTime(2026, 11, 1)),
      c('older', DateTime(2026, 6, 1)),
    ], now);
    expect([for (final g in groups) g.label], ['置顶', '今天', '昨天', '最近 7 天', '最近 30 天', '更早']);
    expect([for (final g in groups) g.items.single.id], ['pinned-old', 'today', 'yesterday', 'week', 'month', 'older']);
    expect(groupByRecency(const [], now), isEmpty);
  });
}

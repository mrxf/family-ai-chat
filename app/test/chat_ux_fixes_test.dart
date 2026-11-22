import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/chat/composer/composer.dart';
import 'package:family_chat/ui/features/conversations/conversation_panel.dart';
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

class _FakeApi extends Api {
  _FakeApi() : super('http://localhost:8080', token: 'test');

  final now = DateTime.now().millisecondsSinceEpoch;

  late final List<Conversation> convList = [
    Conversation(id: 'c1', title: '第一个对话', pinned: false, isPrivate: false, createdAt: now, updatedAt: now),
    Conversation(id: 'c2', title: '第二个对话', modelId: 'm3', pinned: false, isPrivate: false, createdAt: now, updatedAt: now - 1000),
  ];

  @override
  Future<Map<String, dynamic>> appVersion() async => {'available': false};

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async {
    if (before != null) return const [];
    return convList;
  }

  @override
  Future<List<PinnedComponent>> pinnedComponents() async => const [];

  @override
  Future<List<HomeSuggestion>> homeSuggestions() async => const [];

  @override
  Future<ConversationDetail> conversation(String id, {String? focusMessageId, int limit = 30}) async {
    final c = convList.firstWhere((e) => e.id == id);
    return ConversationDetail(
      conversation: c,
      messages: [
        Message(id: 'u-$id', conversationId: id, role: 'user', content: '你好 $id', createdAt: now),
        Message(id: 'a-$id', conversationId: id, role: 'assistant', content: '回复 $id', createdAt: now),
      ],
      hasMore: false,
      activeMessageId: '',
    );
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
          models: [
            ChatModel(id: 'm1', displayName: '默认模型', description: '', vision: true, think: true),
            ChatModel(id: 'm2', displayName: '备选模型 A', description: '', vision: true, think: true),
            ChatModel(id: 'm3', displayName: '备选模型 B', description: '', vision: false, think: false),
          ],
          assistants: [],
          defaultModelId: 'm1',
          voiceInput: false,
        ),
      );
}

void main() {
  testWidgets('clicking history drawer conversation does not focus composer input', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _MemStorage(prefs);
    final api = _FakeApi();

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

    final composerField = find.descendant(of: find.byType(Composer), matching: find.byType(TextField));
    expect(composerField, findsOneWidget);

    // Focus the composer input first (simulates user having typed / focused earlier).
    await tester.tap(composerField);
    await tester.pump();
    final textField = tester.widget<TextField>(composerField);
    expect(textField.focusNode!.hasFocus, isTrue);

    // Swiping from the left edge must NOT open the history drawer (drag gesture disabled).
    await tester.dragFrom(const Offset(0, 300), const Offset(300, 0));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byType(HistoryDrawer), findsNothing);

    // Open the history drawer via the AppBar menu button.
    await tester.tap(find.byTooltip('聊天记录'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byType(HistoryDrawer), findsOneWidget);
    expect(textField.focusNode!.hasFocus, isFalse);

    // Tap a conversation in the history list.
    await tester.tap(find.text('第一个对话'));
    await tester.pump(const Duration(milliseconds: 400));
    await tester.pump(const Duration(milliseconds: 400));

    // Drawer is closed, conversation loaded, and composer must remain unfocused.
    expect(find.byType(HistoryDrawer), findsNothing);
    expect(find.text('你好 c1'), findsOneWidget);
    expect(textField.focusNode!.hasFocus, isFalse);
  });

  test('selected non-default model persists across conversation switches and resets on restart', () async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    final storage = _MemStorage(prefs);
    final api = _FakeApi();

    final container = ProviderContainer(
      overrides: [
        storageProvider.overrideWithValue(storage),
        sessionProvider.overrideWith(() => _StubSession(api)),
      ],
    );
    addTearDown(container.dispose);

    // Load conversations list.
    await container.read(conversationsProvider.notifier).refresh();

    // Initially no override -> system default (null in ChatState, resolved to m1 by Bootstrap).
    expect(container.read(chatProvider).modelId, isNull);

    // User picks a non-default model m2.
    container.read(chatProvider.notifier).setModel('m2');
    expect(container.read(chatProvider).modelId, 'm2');

    // Switch to c1 (server modelId is null) -> keeps m2.
    await container.read(chatProvider.notifier).open('c1');
    expect(container.read(chatProvider).modelId, 'm2');

    // Switch to c2 (server modelId is m3) -> keeps user's chosen m2.
    await container.read(chatProvider.notifier).open('c2');
    expect(container.read(chatProvider).modelId, 'm2');

    // Start a new chat -> keeps m2.
    container.read(chatProvider.notifier).newChat();
    expect(container.read(chatProvider).modelId, 'm2');

    // Simulate app exit & restart (fresh ProviderContainer with same storage) -> resets to system default (null).
    final restarted = ProviderContainer(
      overrides: [
        storageProvider.overrideWithValue(storage),
        sessionProvider.overrideWith(() => _StubSession(api)),
      ],
    );
    addTearDown(restarted.dispose);
    expect(restarted.read(chatProvider).modelId, isNull);
  });
}

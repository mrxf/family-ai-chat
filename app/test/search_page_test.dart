import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/drawer.dart';
import 'package:family_chat/ui/search_page.dart';
import 'package:family_chat/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

class _FakeApi extends Api {
  _FakeApi(this.onSearch) : super('http://localhost:8080', token: 'test');
  final Future<List<Conversation>> Function(String q) onSearch;

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) {
    if (q != null && q.isNotEmpty) return onSearch(q);
    return Future.value(const []);
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
          user: User(id: 'u1', username: 'zed', displayName: '小远', avatar: '', role: 'admin'),
          models: [],
          assistants: [],
          defaultModelId: '',
          voiceInput: false,
        ),
      );
}

class _StubConvs extends ConversationsNotifier {
  _StubConvs(this._initial);
  final List<Conversation> _initial;

  @override
  ConvListState build() => ConvListState(items: _initial, hasMore: false, loaded: true);
}

void main() {
  testWidgets('drawer opens dedicated search page with recent items and snippet highlights', (tester) async {
    final now = DateTime.now().millisecondsSinceEpoch;
    final recentItems = [
      Conversation(
        id: 'c1',
        title: '免Root安卓短剧广告减半方案',
        pinned: false,
        createdAt: now - 4 * 86400 * 1000,
        updatedAt: now - 4 * 86400 * 1000,
      ),
      Conversation(
        id: 'c2',
        title: '周末饮食建议',
        pinned: false,
        createdAt: now - 7 * 86400 * 1000,
        updatedAt: now - 7 * 86400 * 1000,
      ),
    ];

    final api = _FakeApi((q) async => [
          Conversation(
            id: 'c2',
            title: '周末饮食建议',
            snippet: '…平时多补充铁元素，比如瘦肉和动物肝脏，一个月后复查…',
            pinned: false,
            createdAt: now - 7 * 86400 * 1000,
            updatedAt: now - 7 * 86400 * 1000,
          ),
        ]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionProvider.overrideWith(() => _StubSession(api)),
          conversationsProvider.overrideWith(() => _StubConvs(recentItems)),
        ],
        child: MaterialApp(
          theme: buildTheme(Brightness.light),
          home: Scaffold(
            drawer: const HistoryDrawer(),
            body: Builder(
              builder: (ctx) => Center(
                child: ElevatedButton(
                  onPressed: () => Scaffold.of(ctx).openDrawer(),
                  child: const Text('打开抽屉'),
                ),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开抽屉'));
    await tester.pumpAndSettle();

    // Drawer no longer has an inline search TextField, only the search icon button.
    expect(find.descendant(of: find.byType(HistoryDrawer), matching: find.byType(TextField)), findsNothing);
    expect(find.byTooltip('搜索聊天记录'), findsOneWidget);

    // Tap search icon to open dedicated SearchPage.
    await tester.tap(find.byTooltip('搜索聊天记录'));
    await tester.pumpAndSettle();

    expect(find.byType(SearchPage), findsOneWidget);
    expect(find.text('近期对话'), findsOneWidget);
    expect(find.text('免Root安卓短剧广告减半方案'), findsOneWidget);
    expect(find.text('4天'), findsOneWidget);

    // Enter a query matching message body and verify snippet rendering.
    await tester.enterText(find.byType(TextField), '动物肝脏');
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();

    expect(find.text('搜索结果'), findsOneWidget);
    expect(find.textContaining('动物肝脏', findRichText: true), findsWidgets);

    // Tap floating close button to return to the open drawer.
    await tester.tap(find.byTooltip('关闭搜索'));
    await tester.pumpAndSettle();

    expect(find.byType(SearchPage), findsNothing);
    expect(find.byType(HistoryDrawer), findsOneWidget);
  });
}

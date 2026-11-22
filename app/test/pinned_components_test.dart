import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/state/pinned_components.dart';
import 'package:family_chat/ui/features/chat/components/suggestions_card.dart';
import 'package:family_chat/ui/features/chat/components/todo_card.dart';
import 'package:family_chat/ui/features/chat/composer/quote_bar.dart';
import 'package:family_chat/ui/features/chat/pinned_components_board.dart';
import 'package:family_chat/ui/theme/theme.dart';

class _FakePinnedNotifier extends PinnedComponentsNotifier {
  _FakePinnedNotifier(this._initial);
  final PinnedComponentsState _initial;

  @override
  PinnedComponentsState build() => _initial;

  @override
  Future<void> refresh() async {}

  @override
  void setSuggestionsPinned(bool pinned) {
    state = state.copyWith(suggestionsPinned: pinned);
  }
}

Widget _wrap(Widget child, {PinnedComponentsState? pinned}) => ProviderScope(
      overrides: [
        if (pinned != null)
          pinnedComponentsProvider.overrideWith(() => _FakePinnedNotifier(pinned)),
      ],
      child: MaterialApp(
        theme: buildTheme(Brightness.light),
        home: Scaffold(body: child),
      ),
    );

void main() {
  test('columnCountForWidth scales smoothly up to 4 columns for 4K screens', () {
    expect(PinnedComponentsBoard.columnCountForWidth(390, 5), 1);
    expect(PinnedComponentsBoard.columnCountForWidth(800, 5), 2);
    expect(PinnedComponentsBoard.columnCountForWidth(1200, 5), 3);
    expect(PinnedComponentsBoard.columnCountForWidth(1600, 5), 4);
    // Clamps to actual item count so 1 or 2 cards don't leave empty columns.
    expect(PinnedComponentsBoard.columnCountForWidth(1920, 1), 1);
    expect(PinnedComponentsBoard.columnCountForWidth(1920, 2), 2);
  });

  test('describeComponent summarizes todo progress accurately', () {
    const comp = MessageComponent(
      id: 'c1',
      kind: 'todo',
      data: {
        'title': '周末采购',
        'items': [
          {'id': 'i1', 'text': '牛奶'},
          {'id': 'i2', 'text': '鸡蛋'},
          {'id': 'i3', 'text': '苹果'},
        ],
      },
      state: {
        'done_ids': ['i1', 'i3'],
      },
    );
    final meta = describeComponent(comp);
    expect(meta.title, '周末采购');
    expect(meta.summary, '已完成 2/3 项');
  });

  testWidgets('TodoCard invokes custom onStateChanged callback when provided', (tester) async {
    Map<String, dynamic>? updated;
    const comp = MessageComponent(
      id: 'c_todo',
      kind: 'todo',
      data: {
        'title': '测试清单',
        'items': [
          {'id': 'i1', 'text': '买牛奶'},
          {'id': 'i2', 'text': '买鸡蛋'},
        ],
      },
      state: {'done_ids': []},
    );

    await tester.pumpWidget(_wrap(
      TodoCard(
        messageId: 'm1',
        component: comp,
        onStateChanged: (next) => updated = next,
      ),
    ));

    await tester.tap(find.text('买牛奶'));
    await tester.pump();
    expect(updated, {'done_ids': ['i1']});
  });

  testWidgets('PinnedComponentsBoard shows default SuggestionsCard and allows unpin/restore', (tester) async {
    String? picked;
    await tester.pumpWidget(_wrap(
      SingleChildScrollView(
        child: PinnedComponentsBoard(onSuggestion: (t) => picked = t),
      ),
      pinned: const PinnedComponentsState(loaded: true, suggestionsPinned: true),
    ));

    expect(find.byType(SuggestionsCard), findsOneWidget);
    expect(find.text('猜你想问'), findsOneWidget);

    await tester.tap(find.byTooltip('取消固定'));
    await tester.pumpAndSettle();
    expect(find.byType(SuggestionsCard), findsNothing);
    expect(find.text('恢复「猜你想问」卡片'), findsOneWidget);

    await tester.tap(find.text('恢复「猜你想问」卡片'));
    await tester.pumpAndSettle();
    expect(find.byType(SuggestionsCard), findsOneWidget);
    expect(picked, isNull);
  });

  testWidgets('ComposerQuoteBar renders quote metadata and dismisses cleanly', (tester) async {
    var dismissed = false;
    await tester.pumpWidget(_wrap(
      ComposerQuoteBar(
        quote: const QuotedComponent(
          kind: 'weather',
          title: '北京 · 明天',
          summary: '晴 · 22°C',
        ),
        onDismiss: () => dismissed = true,
      ),
    ));

    expect(find.text('引用 · 北京 · 明天'), findsOneWidget);
    expect(find.text('晴 · 22°C'), findsOneWidget);
    await tester.tap(find.byTooltip('移除引用'));
    expect(dismissed, isTrue);
  });
}

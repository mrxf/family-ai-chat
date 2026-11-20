import 'package:family_chat/core/admin_models.dart';
import 'package:family_chat/core/models.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/ui/icons.dart';
import 'package:family_chat/ui/messages.dart';
import 'package:family_chat/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:phosphoricons_flutter/phosphoricons_flutter.dart';

void main() {
  test('Bootstrap and Message parse web_search feature and persisted web sources', () {
    final boot = Bootstrap.fromJson({
      'user': {'id': 'u1', 'username': 'mama', 'display_name': '妈妈', 'avatar': '', 'role': 'member'},
      'models': [
        {'id': 'm1', 'display_name': 'DeepSeek', 'description': '', 'vision': false, 'think': false},
      ],
      'assistants': const [],
      'default_model_id': 'm1',
      'features': {
        'voice_input': false,
        'web_search': true,
        'search_provider': 'deepseek',
      },
    });
    expect(boot.webSearch, isTrue);
    expect(boot.searchProvider, 'deepseek');

    final msg = Message.fromJson({
      'id': 'msg-1',
      'conversation_id': 'c1',
      'role': 'assistant',
      'content': '今天国内金价约 796 元/克。',
      'status': 'done',
      'sources': [
        {
          'title': '国内金价最新行情',
          'url': 'https://m.thepaper.cn/newsDetail_forward_1',
          'site_name': '澎湃新闻',
          'date': '2026-11-19',
        },
      ],
    });
    expect(msg.sources.length, 1);
    expect(msg.sources.first.siteName, '澎湃新闻');
    expect(Message.fromJson(msg.toJson()).sources.first.url, 'https://m.thepaper.cn/newsDetail_forward_1');
  });

  test('AdminSearchState parses DeepSeek and reserved SerpAPI providers', () {
    final state = AdminSearchState.fromJson({
      'active': 'deepseek',
      'enabled': true,
      'supports_fetch': true,
      'providers': [
        {
          'kind': 'deepseek',
          'name': 'DeepSeek 联网搜索',
          'description': '实时搜索与网页阅读',
          'available': true,
          'supports_fetch': true,
          'key_url': 'https://platform.deepseek.com/api_keys',
          'default_url': 'https://api.deepseek.com',
          'has_key': true,
          'api_key_hint': 'sk-****e5c0',
          'ready': true,
          'reusable_provider_key_hint': 'sk-****9999',
        },
        {
          'kind': 'serp',
          'name': 'SerpAPI (Google 搜索)',
          'description': '预留',
          'available': false,
          'supports_fetch': false,
          'key_url': 'https://serpapi.com',
          'default_url': 'https://serpapi.com',
          'has_key': false,
          'ready': false,
        },
      ],
    });
    expect(state.enabled, isTrue);
    expect(state.byKind('deepseek')?.ready, isTrue);
    expect(state.byKind('serp')?.available, isFalse);
  });

  test('AppIcons.globe and AppIcons.article scale stroke weights dynamically', () {
    expect(AppIcons.resolveForScale(AppIcons.globe, 0.85), PhosphorIconsThin.globe);
    expect(AppIcons.resolveForScale(AppIcons.globe, 1.0), PhosphorIconsLight.globe);
    expect(AppIcons.resolveForScale(AppIcons.globe, 1.15), PhosphorIconsRegular.globe);
    expect(AppIcons.resolveForScale(AppIcons.globe, 1.3), PhosphorIconsBold.globe);

    expect(AppIcons.resolveForScale(AppIcons.article, 0.85), PhosphorIconsThin.article);
    expect(AppIcons.resolveForScale(AppIcons.article, 1.3), PhosphorIconsBold.article);
  });

  testWidgets('TaskAndSourcesPanel shows live steps while searching, folds when settled, and expands on tap', (tester) async {
    Widget wrap(Widget child) => MaterialApp(
          theme: buildTheme(Brightness.light),
          home: Scaffold(body: Padding(padding: const EdgeInsets.all(16), child: child)),
        );

    // 1. Live running search step is expanded automatically so the user sees progress.
    await tester.pumpWidget(wrap(
      const TaskAndSourcesPanel(
        steps: [
          RunStep(kind: 'memory', title: '已翻看记忆：黄金', status: 'done'),
          RunStep(kind: 'search', title: '联网搜索：今日国内金价', status: 'running'),
        ],
        sources: [],
        isLive: true,
        waitingForReply: true,
        activity: '正在联网搜索「今日国内金价」…',
      ),
    ));
    expect(find.textContaining('正在联网搜索：今日国内金价'), findsOneWidget);
    expect(find.text('已翻看记忆：黄金'), findsOneWidget);

    // 2. Once reply finishes (or streams), collapses into a summary pill with source count & site name;
    // tapping the pill expands the clickable web sources.
    await tester.pumpWidget(wrap(
      const TaskAndSourcesPanel(
        steps: [],
        sources: [
          WebSource(
            title: '国内金价跌破800元/克',
            url: 'https://m.thepaper.cn/newsDetail_forward_1',
            siteName: '澎湃新闻',
            date: '2026-11-18',
          ),
          WebSource(
            title: '零售金商报价',
            url: 'https://quote.stockstar.com/gold/salegold.shtml',
            siteName: '证券之星',
            date: '2026-11-18',
          ),
        ],
        isLive: false,
        waitingForReply: false,
      ),
    ));
    await tester.pumpAndSettle();
    expect(find.text('已参考 2 篇网页 · 澎湃新闻、证券之星'), findsOneWidget);
    expect(find.text('国内金价跌破800元/克'), findsNothing);

    await tester.tap(find.text('已参考 2 篇网页 · 澎湃新闻、证券之星'));
    await tester.pumpAndSettle();
    expect(find.text('国内金价跌破800元/克'), findsOneWidget);
    expect(find.text('零售金商报价'), findsOneWidget);
  });

  testWidgets('TaskAndSourcesPanel renders history step live progress and settles to 已翻看前面的聊天记录', (tester) async {
    Widget wrap(Widget child) => MaterialApp(
          theme: buildTheme(Brightness.light),
          home: Scaffold(body: Padding(padding: const EdgeInsets.all(16), child: child)),
        );

    await tester.pumpWidget(wrap(
      const TaskAndSourcesPanel(
        steps: [
          RunStep(kind: 'history', title: '翻看前面的聊天记录', status: 'running'),
        ],
        sources: [],
        isLive: true,
        waitingForReply: true,
        activity: '正在翻看前面的聊天记录…',
      ),
    ));
    expect(find.textContaining('正在翻看前面的聊天记录'), findsOneWidget);

    await tester.pumpWidget(wrap(
      const TaskAndSourcesPanel(
        steps: [
          RunStep(kind: 'history', title: '已翻看前面的聊天记录', status: 'done'),
        ],
        sources: [],
        isLive: false,
        waitingForReply: false,
      ),
    ));
    await tester.pumpAndSettle();
    // Settled panel collapses to the single header pill by default.
    expect(find.text('已翻看前面的聊天记录'), findsOneWidget);
  });
}

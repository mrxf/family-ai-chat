import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/state/pinned_components.dart';
import 'package:family_chat/state/scheduled_tasks.dart';
import 'package:family_chat/ui/features/automations/scheduled_tasks_page.dart';
import 'package:family_chat/ui/features/chat/components/feed_card.dart';
import 'package:family_chat/ui/theme/theme.dart';

class _FakePinnedNotifier extends PinnedComponentsNotifier {
  _FakePinnedNotifier(this._initial);
  final PinnedComponentsState _initial;

  @override
  PinnedComponentsState build() => _initial;

  @override
  Future<void> refresh() async {}
}

void main() {
  test('ScheduledTask formats schedule rules cleanly', () {
    final daily = ScheduledTask.fromJson({
      'id': 't1',
      'title': '每日大模型新闻',
      'prompt': '查询最新大模型新闻，更新前三条',
      'schedule_kind': 'daily',
      'time_of_day': '07:00',
      'enabled': true,
      'target_message_id': 'm1',
      'target_component_id': 'c1',
    });
    expect(daily.scheduleLabel, '每天 07:00');
    expect(daily.hasTargetComponent, isTrue);

    const weekly = ScheduledTask(
      id: 't2',
      title: '周报复盘',
      prompt: '复盘本周待办',
      scheduleKind: ScheduledTaskKind.weekly,
      timeOfDay: '21:00',
      weekdays: '1,5',
    );
    expect(weekly.scheduleLabel, '每周一、周五 21:00');

    const interval = ScheduledTask(
      id: 't3',
      title: '每2小时检查',
      prompt: '检查状态',
      scheduleKind: ScheduledTaskKind.interval,
      intervalMinutes: 120,
    );
    expect(interval.scheduleLabel, '每 2 小时');
  });

  test('feed component is pinnable and summarized accurately', () {
    const comp = MessageComponent(
      id: 'comp_feed_1',
      kind: 'feed',
      data: {
        'title': '每日大模型新闻',
        'items': [
          {'id': 'f1', 'title': '模型A发布'},
          {'id': 'f2', 'title': '模型B开源'},
        ],
      },
    );
    expect(isComponentPinnable(comp), isTrue);
    final meta = describeComponent(comp);
    expect(meta.kindLabel, '动态资讯');
    expect(meta.title, '每日大模型新闻');
    expect(meta.summary, '共 2 条 · 模型A发布');
  });

  testWidgets('FeedCard renders rolling 6 news items with tags and source', (tester) async {
    const comp = MessageComponent(
      id: 'comp_feed_1',
      kind: 'feed',
      data: {
        'title': '每日大模型新闻',
        'subtitle': '每天 07:00 滚动更新',
        'items': [
          {'id': 'n1', 'title': '今日头条1', 'summary': '详细摘要1', 'tag': '今日', 'source': '机器之心'},
          {'id': 'n2', 'title': '今日头条2', 'tag': '今日'},
          {'id': 'n3', 'title': '今日头条3', 'tag': '今日'},
          {'id': 'o1', 'title': '昨日旧闻1', 'tag': '往期'},
          {'id': 'o2', 'title': '昨日旧闻2', 'tag': '往期'},
          {'id': 'o3', 'title': '昨日旧闻3', 'tag': '往期'},
        ],
      },
    );

    await tester.pumpWidget(MaterialApp(
      theme: buildTheme(Brightness.light),
      home: const Scaffold(
        body: SingleChildScrollView(child: FeedCard(component: comp)),
      ),
    ));

    expect(find.text('每日大模型新闻'), findsOneWidget);
    expect(find.text('今日头条1'), findsOneWidget);
    expect(find.text('昨日旧闻3'), findsOneWidget);
    expect(find.text('机器之心'), findsOneWidget);
    expect(find.text('今日'), findsNWidgets(3));
    expect(find.text('往期'), findsNWidgets(3));
  });

  testWidgets('ScheduledTasksPage renders on mobile and desktop layouts', (tester) async {
    const task = ScheduledTask(
      id: 't1',
      title: '早7点大模型新闻',
      prompt: '查询最新大模型新闻，更新前三条，原前三条后移',
      scheduleKind: ScheduledTaskKind.daily,
      timeOfDay: '07:00',
      enabled: true,
      targetMessageId: 'm1',
      targetComponentId: 'c1',
      lastStatus: 'success',
      lastSummary: '已更新最新 3 条新闻',
      runCount: 3,
    );
    const pin = PinnedComponent(
      id: 'pin1',
      conversationId: 'conv1',
      conversationTitle: '大模型新闻',
      messageId: 'm1',
      componentId: 'c1',
      component: MessageComponent(
        id: 'c1',
        kind: 'feed',
        data: {'title': '每日大模型新闻', 'items': [{'id': 'f1', 'title': '新闻1'}]},
      ),
    );

    Widget buildApp({required bool embedded}) => ProviderScope(
          overrides: [
            scheduledTasksProvider.overrideWith((_) async => const ScheduledTasksResult(items: [task], activeCount: 1, totalCount: 1)),
            pinnedComponentsProvider.overrideWith(() => _FakePinnedNotifier(const PinnedComponentsState(items: [pin], loaded: true))),
          ],
          child: MaterialApp(
            theme: buildTheme(Brightness.light),
            home: ScheduledTasksPage(embedded: embedded),
          ),
        );

    // Mobile narrow layout
    await tester.pumpWidget(buildApp(embedded: false));
    await tester.pumpAndSettle();
    expect(find.text('AI 定时任务'), findsOneWidget);
    expect(find.text('早7点大模型新闻'), findsOneWidget);
    expect(find.text('每天 07:00'), findsOneWidget);
    expect(find.text('更新卡片：每日大模型新闻'), findsOneWidget);
    expect(find.text('新建 AI 定时任务'), findsOneWidget);

    // Desktop expanded layout
    tester.view.physicalSize = const Size(1280, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(buildApp(embedded: true));
    await tester.pumpAndSettle();
    expect(find.text('新建任务'), findsOneWidget);
    expect(find.text('早7点大模型新闻'), findsOneWidget);
  });
}

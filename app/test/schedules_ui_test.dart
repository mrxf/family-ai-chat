import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/schedules.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/chat/messages/messages.dart';
import 'package:family_chat/ui/features/conversations/conversation_panel.dart';
import 'package:family_chat/ui/features/schedules/schedules_page.dart';
import 'package:family_chat/ui/theme/icons.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:phosphoricons_flutter/phosphoricons_flutter.dart';

void main() {
  test('Schedule and SchedulesResult parse recurrence, reminders, and labels', () {
    final res = SchedulesResult.fromJson({
      'today_pending': 1,
      'active_total': 2,
      'schedules': [
        {
          'id': 's1',
          'title': '吃降压药',
          'start_at': 1795132800000,
          'remind_before_min': 0,
          'recurrence': 'weekly',
          'recurrence_days': '1,3,5',
          'status': 'active',
        },
        {
          'id': 's2',
          'title': '去协和医院复查',
          'location': '门诊三楼',
          'note': '空腹抽血',
          'start_at': 1795224600000,
          'remind_before_min': 30,
          'recurrence': 'none',
          'status': 'active',
        },
      ],
      'today_occurrences': [
        {
          'schedule': {
            'id': 's1',
            'title': '吃降压药',
            'start_at': 1795132800000,
            'remind_before_min': 0,
            'recurrence': 'weekly',
            'recurrence_days': '1,3,5',
            'status': 'active',
          },
          'occurrence_date': '2026-11-20',
          'start_at': 1795132800000,
          'completed': false,
        },
      ],
      'upcoming_reminders': [
        {
          'id': 'sched_s2_2026-11-21',
          'schedule_id': 's2',
          'occurrence_date': '2026-11-21',
          'title': '去协和医院复查',
          'body': '09:30 · 提前 30 分钟提醒',
          'start_at': 1795224600000,
          'fire_at': 1795222800000,
          'all_day': false,
        },
      ],
    });

    expect(res.todayPending, 1);
    expect(res.activeTotal, 2);
    expect(res.schedules[0].recurrenceSummary, '每周一、周三、周五');
    expect(res.schedules[0].remindLabel, '准点提醒');
    expect(res.schedules[1].remindLabel, '提前 30 分钟提醒');
    expect(res.upcomingReminders.single.id, 'sched_s2_2026-11-21');
    expect(AppIcons.resolveForScale(AppIcons.calendar, 1.0), PhosphorIconsLight.calendarCheck);
  });

  testWidgets('TaskAndSourcesPanel renders skill and schedule RunSteps', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.light),
        home: const Scaffold(
          body: TaskAndSourcesPanel(
            steps: [
              RunStep(kind: 'skill', title: '已加载技能：日程与提醒管理', status: 'done'),
              RunStep(kind: 'schedule', title: '已添加日程：协和医院空腹体检', detail: '2026-11-21 08:30', status: 'done'),
            ],
            sources: [],
            isLive: false,
            waitingForReply: false,
          ),
        ),
      ),
    );

    expect(find.text('已添加日程：协和医院空腹体检'), findsOneWidget);
    await tester.tap(find.text('已添加日程：协和医院空腹体检'));
    await tester.pumpAndSettle();
    expect(find.text('已加载技能：日程与提醒管理'), findsOneWidget);
  });

  testWidgets('SchedulesPage displays today items and permission banner', (tester) async {
    final sample = SchedulesResult.fromJson({
      'today_pending': 1,
      'active_total': 1,
      'schedules': [
        {
          'id': 's1',
          'title': '下午三点接孙子放学',
          'location': '实验小学南门',
          'start_at': DateTime(2026, 11, 20, 15, 0).millisecondsSinceEpoch,
          'remind_before_min': 30,
          'recurrence': 'weekdays',
          'status': 'active',
        },
      ],
      'today_occurrences': [
        {
          'schedule': {
            'id': 's1',
            'title': '下午三点接孙子放学',
            'location': '实验小学南门',
            'start_at': DateTime(2026, 11, 20, 15, 0).millisecondsSinceEpoch,
            'remind_before_min': 30,
            'recurrence': 'weekdays',
            'status': 'active',
          },
          'occurrence_date': '2026-11-20',
          'start_at': DateTime(2026, 11, 20, 15, 0).millisecondsSinceEpoch,
          'completed': false,
        },
      ],
    });

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          schedulesProvider.overrideWith((_) async => sample),
          notificationPermissionProvider.overrideWith((_) async => false),
        ],
        child: MaterialApp(
          theme: buildTheme(Brightness.light),
          home: const SchedulesPage(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('我的日程'), findsOneWidget);
    expect(find.text('下午三点接孙子放学'), findsOneWidget);
    expect(find.text('每个工作日'), findsOneWidget);
    expect(find.textContaining('尚未开启手机通知权限'), findsOneWidget);
  });

  testWidgets('ConversationPanel hides schedule entry when empty and shows compact icon+count when active', (tester) async {
    final user = User(id: 'u1', username: 'mom', displayName: '妈妈', avatar: '🌸', role: 'user');
    final boot = Bootstrap(user: user, models: const [], assistants: const [], defaultModelId: '', voiceInput: false);

    Widget buildPanel(SchedulesResult schedules) => ProviderScope(
          key: ValueKey('panel-${schedules.activeTotal}'),
          overrides: [
            sessionProvider.overrideWith(() => _StubSession(Session(status: SessionStatus.loggedIn, boot: boot))),
            conversationsProvider.overrideWith(_StubConversations.new),
            schedulesProvider.overrideWith((_) async => schedules),
          ],
          child: MaterialApp(
            theme: buildTheme(Brightness.light),
            home: const Scaffold(body: SizedBox(width: 300, child: ConversationPanel())),
          ),
        );

    // 1. No pending schedules: sidebar hides schedule badge completely.
    await tester.pumpWidget(buildPanel(const SchedulesResult(activeTotal: 0, todayPending: 0)));
    await tester.pumpAndSettle();
    expect(find.text('我的日程'), findsNothing);
    expect(find.byIcon(AppIcons.calendar), findsNothing);

    // 2. Active pending schedules exist: shows compact calendar icon + count pill (no full-row text).
    await tester.pumpWidget(buildPanel(const SchedulesResult(activeTotal: 3, todayPending: 1)));
    await tester.pumpAndSettle();
    expect(find.text('我的日程'), findsNothing);
    expect(find.byIcon(AppIcons.calendar), findsOneWidget);
    expect(find.text('3'), findsOneWidget);
  });
}

class _StubSession extends SessionNotifier {
  _StubSession(this._state);
  final Session _state;
  @override
  Session build() => _state;
}

class _StubConversations extends ConversationsNotifier {
  @override
  ConvListState build() => const ConvListState(loaded: true);
}

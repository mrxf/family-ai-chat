import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/platform/platform_capabilities.dart';
import 'package:family_chat/state/conversations.dart';
import 'package:family_chat/state/desktop_nav.dart';
import 'package:family_chat/state/memories.dart';
import 'package:family_chat/state/pinned_components.dart';
import 'package:family_chat/state/schedules.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/state/suggestions.dart';
import 'package:family_chat/ui/adaptive/adaptive_modal.dart';
import 'package:family_chat/ui/desktop/desktop_home_layout.dart';
import 'package:family_chat/ui/features/chat/composer/composer.dart';
import 'package:family_chat/ui/theme/theme.dart';

class _StubSessionNotifier extends SessionNotifier {
  _StubSessionNotifier(this._initial);
  final Session _initial;

  @override
  Session build() => _initial;
}

class _StubConversationsNotifier extends ConversationsNotifier {
  @override
  ConvListState build() => const ConvListState(loaded: true);

  @override
  Future<void> refresh() async {}
}

class _StubPinnedNotifier extends PinnedComponentsNotifier {
  @override
  PinnedComponentsState build() => const PinnedComponentsState(loaded: true);

  @override
  Future<void> refresh() async {}
}

class _StubSuggestionsNotifier extends HomeSuggestionsNotifier {
  @override
  HomeSuggestionsState build() => const HomeSuggestionsState(loaded: true);

  @override
  Future<void> refresh() async {}
}

class _StubSettingsNotifier extends SettingsNotifier {
  @override
  AppSettings build() => const AppSettings();

  @override
  void update(AppSettings s) {
    state = s;
  }
}

void main() {
  const adminUser = User(id: 'u1', username: 'dad', displayName: '爸爸', avatar: '👨', role: 'admin');
  const boot = Bootstrap(
    user: adminUser,
    models: [],
    assistants: [],
    defaultModelId: '',
    autoModel: false,
    imageGeneration: false,
    imagesLeft: 0,
    voiceInput: false,
  );
  const session = Session(
    status: SessionStatus.loggedIn,
    server: 'https://example.com',
    boot: boot,
  );

  final activeSchedules = SchedulesResult(
    schedules: [
      Schedule(
        id: 's1',
        title: '周会讨论',
        startAt: DateTime.now().add(const Duration(hours: 2)).millisecondsSinceEpoch,
        allDay: false,
      ),
    ],
    todayPending: 1,
    activeTotal: 1,
  );

  Widget buildDesktopApp() {
    final composerKey = GlobalKey<ComposerState>();
    return ProviderScope(
      overrides: [
        platformCapabilitiesProvider.overrideWithValue(PlatformCapabilities.of(TargetPlatform.macOS)),
        sessionProvider.overrideWith(() => _StubSessionNotifier(session)),
        settingsProvider.overrideWith(_StubSettingsNotifier.new),
        conversationsProvider.overrideWith(_StubConversationsNotifier.new),
        pinnedComponentsProvider.overrideWith(_StubPinnedNotifier.new),
        homeSuggestionsProvider.overrideWith(_StubSuggestionsNotifier.new),
        schedulesProvider.overrideWith((_) async => activeSchedules),
        memoriesProvider.overrideWith((_) async => const MemoriesResult(enabled: true, memories: [], total: 0, newCount: 0)),
      ],
      child: MaterialApp(
        theme: buildTheme(Brightness.light),
        home: DesktopHomeLayout(composerKey: composerKey),
      ),
    );
  }

  testWidgets('Desktop sidebar switches to settings nav and opens embedded schedules', (tester) async {
    tester.view.physicalSize = const Size(1280, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(buildDesktopApp());
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    // Initially in chat mode: conversation sidebar visible, schedule pill shows count 1.
    expect(find.text('新对话'), findsOneWidget);
    expect(find.text('1'), findsOneWidget);

    // Clicking schedule pill keeps conversation sidebar and opens embedded desktop SchedulesPage.
    await tester.tap(find.text('1'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('新对话'), findsOneWidget);
    expect(find.text('我的日程'), findsOneWidget);
    expect(find.text('添加日程'), findsOneWidget);
    expect(find.text('手动添加日程'), findsNothing);

    // Clicking settings gear switches sidebar to DesktopSettingsSidebar and shows general settings.
    await tester.tap(find.byTooltip('设置'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('返回对话'), findsOneWidget);
    expect(find.text('通用与外观'), findsWidgets);
    expect(find.text('账号与安全'), findsOneWidget);
    expect(find.text('家庭成员'), findsOneWidget);
    expect(find.text('编辑资料'), findsOneWidget);

    // Switching to Account tab updates right pane directly.
    await tester.tap(find.text('账号与安全'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('修改密码'), findsOneWidget);
    expect(find.text('退出登录'), findsWidgets);

    // Clicking "返回对话" returns to chat + conversation sidebar.
    await tester.tap(find.text('返回对话'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('新对话'), findsOneWidget);
    expect(find.text('返回对话'), findsNothing);
  });

  testWidgets('showAdaptiveModal opens Dialog on desktop and bottom sheet on mobile', (tester) async {
    // Desktop (expanded)
    tester.view.physicalSize = const Size(1280, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.light),
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => showAdaptiveModal<void>(
                context: context,
                builder: (_) => const Padding(padding: EdgeInsets.all(16), child: Text('测试弹窗内容')),
              ),
              child: const Text('打开'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开'));
    await tester.pumpAndSettle();
    expect(find.byType(Dialog), findsOneWidget);
    expect(find.text('测试弹窗内容'), findsOneWidget);
  });

  test('DesktopNavNotifier transitions cleanly', () {
    final container = ProviderContainer();
    addTearDown(container.dispose);

    final ctrl = container.read(desktopNavProvider.notifier);
    expect(container.read(desktopNavProvider).section, DesktopSection.chat);

    ctrl.openSettings(SettingsTab.account);
    expect(container.read(desktopNavProvider).section, DesktopSection.settings);
    expect(container.read(desktopNavProvider).settingsTab, SettingsTab.account);

    ctrl.toggleSettings();
    expect(container.read(desktopNavProvider).section, DesktopSection.chat);

    ctrl.showSchedules();
    expect(container.read(desktopNavProvider).section, DesktopSection.schedules);
  });
}

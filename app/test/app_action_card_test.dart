import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/platform/platform_capabilities.dart';
import 'package:family_chat/state/app_actions.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/chat/app_action_card.dart';

void main() {
  for (final wide in [false, true]) {
    testWidgets('action card ${wide ? 'wide desktop' : 'narrow mobile'} supports undo and status', (tester) async {
      tester.view.physicalSize = wide ? const Size(1400, 900) : const Size(360, 720);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      var undoWrites = 0;
      final executor = AppActionExecutor(
        deviceId: 'this',
        readValues: () => {},
        applyValues: (_) async {},
        readReceipts: () => [],
        saveReceipts: (_) async {},
        writeState: (_, _, state) async {
          if (state['status'] == 'undone') undoWrites++;
        },
      );
      Future<void> show(String status, {String target = 'this'}) => tester.pumpWidget(
        ProviderScope(
          overrides: [
            deviceIdProvider.overrideWithValue('this'),
            appActionsProvider.overrideWithValue(executor),
            platformCapabilitiesProvider.overrideWithValue(PlatformCapabilities.of(wide ? TargetPlatform.macOS : TargetPlatform.android)),
          ],
          child: MaterialApp(
            home: Scaffold(
              body: AppActionCard(
                messageId: 'm',
                component: MessageComponent(
                  id: 'a',
                  kind: 'app_action',
                  data: {'action': 'set_settings', 'summary': '字体调到「大」', 'target_device_id': target},
                  state: {
                    'status': status,
                    'applied_device_id': 'this',
                    'previous': {'font_scale': 1.0},
                  },
                ),
              ),
            ),
          ),
        ),
      );
      await show('applied');
      expect(find.text('字体调到「大」'), findsOneWidget);
      expect(find.byTooltip('恢复调整前的设置'), findsOneWidget);
      await tester.tap(find.text('撤销'));
      await tester.pumpAndSettle();
      expect(undoWrites, 1);
      expect(tester.takeException(), isNull);
      await show('undone');
      expect(find.text('已撤销'), findsOneWidget);
      expect(find.text('撤销'), findsNothing);
      await show('unsupported');
      expect(find.text(wide ? '这台电脑不支持' : '这台手机不支持'), findsOneWidget);
      await show('applied', target: 'other');
      expect(find.text('撤销'), findsNothing);
      expect(find.text('字体调到「大」'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }
}

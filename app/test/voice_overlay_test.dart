import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/ui/features/chat/composer/voice_overlay.dart';
import 'package:family_chat/ui/theme/theme.dart';

void main() {
  testWidgets('VoiceOverlay transitions cleanly across recording, cancelling, and recognizing', (tester) async {
    final phase = ValueNotifier(VoicePhase.recording);
    final level = ValueNotifier(0.4);
    final seconds = ValueNotifier(3);
    final liveText = ValueNotifier('');
    final cancelProgress = ValueNotifier(0.0);

    addTearDown(() {
      phase.dispose();
      level.dispose();
      seconds.dispose();
      liveText.dispose();
      cancelProgress.dispose();
    });

    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.light),
        home: Scaffold(
          body: VoiceOverlay(
            phase: phase,
            level: level,
            seconds: seconds,
            liveText: liveText,
            cancelProgress: cancelProgress,
            streamMode: true,
            maxSeconds: 60,
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 250));

    expect(find.text('正在聆听 · 00:03'), findsOneWidget);
    expect(find.text('请直接说出你想问的…'), findsOneWidget);
    expect(find.text('松开即发送'), findsOneWidget);
    expect(find.text('手指上滑取消'), findsOneWidget);

    // Live transcript arrives.
    liveText.value = '明天北京天气怎么样';
    await tester.pump();
    expect(find.text('明天北京天气怎么样'), findsOneWidget);

    // Slide up into cancelling state.
    cancelProgress.value = 1.0;
    phase.value = VoicePhase.cancelling;
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text('准备取消'), findsOneWidget);
    expect(find.text('松开手指，取消发送'), findsOneWidget);
    expect(find.text('下滑可恢复发送'), findsOneWidget);

    // Recognizing state.
    phase.value = VoicePhase.recognizing;
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text('语音识别中'), findsOneWidget);
    expect(find.text('正在整理语音内容…'), findsOneWidget);
  });
}

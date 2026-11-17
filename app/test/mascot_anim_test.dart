import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/ui/mascot_anim.dart';
import 'package:family_chat/ui/mascot_sheets.dart';
import 'package:family_chat/ui/widgets.dart';

Widget _host(Widget child, {bool reduceMotion = false}) => MediaQuery(
      data: MediaQueryData(disableAnimations: reduceMotion),
      child: Directionality(textDirection: TextDirection.ltr, child: Center(child: child)),
    );

/// Let the real asset decode (needs a real async zone), then pump a frame.
Future<void> _settleLoad(WidgetTester tester) async {
  for (var i = 0; i < 20 && find.byType(Mascot).evaluate().isNotEmpty; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
    await tester.pump();
  }
}

bool _animating() => find.descendant(of: find.byType(AnimatedMascot), matching: find.byType(CustomPaint)).evaluate().isNotEmpty;

void main() {
  setUp(debugResetMascot);

  testWidgets('every motion sheet decodes and animates', (tester) async {
    for (final m in MascotMotion.values.where((m) => m != MascotMotion.welcome)) {
      await tester.pumpWidget(_host(AnimatedMascot(key: ValueKey(m), size: 120, motion: m)));
      expect(find.byType(Mascot), findsOneWidget, reason: 'static image while loading');
      await _settleLoad(tester);
      expect(_animating(), isTrue, reason: '$m should animate once loaded');
    }
  });

  testWidgets('greets once per launch, then idles', (tester) async {
    await tester.pumpWidget(_host(const AnimatedMascot(size: 120, greet: true)));
    await _settleLoad(tester);
    expect(_animating(), isTrue);
    await tester.pump(mascotSheets[MascotMotion.welcome]!.duration + const Duration(milliseconds: 100));
    await _settleLoad(tester);
    await tester.pump(const Duration(milliseconds: 100));
    expect(_animating(), isTrue, reason: 'idle loop after the greeting');

    // A second greeter in the same launch just idles.
    await tester.pumpWidget(_host(const AnimatedMascot(key: ValueKey('again'), size: 120, greet: true)));
    final state = tester.state(find.byType(AnimatedMascot));
    expect((state as dynamic).debugPlaying, MascotMotion.idle);
  });

  testWidgets('reduced motion shows the static image', (tester) async {
    await tester.pumpWidget(_host(const AnimatedMascot(size: 120), reduceMotion: true));
    await _settleLoad(tester);
    await tester.pump(const Duration(seconds: 1));
    expect(find.byType(Mascot), findsOneWidget);
  });
}

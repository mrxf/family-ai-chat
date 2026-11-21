import 'package:family_chat/ui/theme/icons.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:phosphoricons_flutter/phosphoricons_flutter.dart';

void main() {
  test('AppIcons.resolveForScale maps Light-base icons across Thin/Light/Regular/Bold', () {
    expect(AppIcons.resolveForScale(AppIcons.menu, 0.85), PhosphorIconsThin.list);
    expect(AppIcons.resolveForScale(AppIcons.menu, 0.92), PhosphorIconsLight.list);
    expect(AppIcons.resolveForScale(AppIcons.menu, 1.0), PhosphorIconsLight.list);
    expect(AppIcons.resolveForScale(AppIcons.menu, 1.15), PhosphorIconsRegular.list);
    expect(AppIcons.resolveForScale(AppIcons.menu, 1.3), PhosphorIconsBold.list);
  });

  test('AppIcons.resolveForScale preserves +1 emphasis for Regular-base icons and clamps at Bold', () {
    expect(AppIcons.resolveForScale(AppIcons.arrowUp, 0.85), PhosphorIconsLight.arrowUp);
    expect(AppIcons.resolveForScale(AppIcons.arrowUp, 1.0), PhosphorIconsRegular.arrowUp);
    expect(AppIcons.resolveForScale(AppIcons.arrowUp, 1.15), PhosphorIconsBold.arrowUp);
    expect(AppIcons.resolveForScale(AppIcons.arrowUp, 1.3), PhosphorIconsBold.arrowUp);
  });

  test('AppIcons.resolveForScale keeps Fill icons unchanged', () {
    for (final scale in [0.85, 0.92, 1.0, 1.15, 1.3]) {
      expect(AppIcons.resolveForScale(AppIcons.checkCircleFill, scale), PhosphorIconsFill.checkCircle);
    }
  });

  testWidgets('AppIcon updates rendered stroke weight when MediaQuery textScaler changes', (tester) async {
    Widget buildWithScale(double scale) => MediaQuery(
          data: MediaQueryData(textScaler: TextScaler.linear(scale)),
          child: const Directionality(
            textDirection: TextDirection.ltr,
            child: AppIcon(AppIcons.settings, size: 24),
          ),
        );

    await tester.pumpWidget(buildWithScale(0.85));
    expect(tester.widget<Icon>(find.byType(Icon)).icon, PhosphorIconsThin.gearSix);

    await tester.pumpWidget(buildWithScale(1.0));
    expect(tester.widget<Icon>(find.byType(Icon)).icon, PhosphorIconsLight.gearSix);

    await tester.pumpWidget(buildWithScale(1.15));
    expect(tester.widget<Icon>(find.byType(Icon)).icon, PhosphorIconsRegular.gearSix);

    await tester.pumpWidget(buildWithScale(1.3));
    expect(tester.widget<Icon>(find.byType(Icon)).icon, PhosphorIconsBold.gearSix);
  });
}

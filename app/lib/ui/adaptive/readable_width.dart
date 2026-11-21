import 'package:flutter/widgets.dart';

/// Centers [child] and caps its width so lines stay readable on wide windows.
/// A no-op on narrow screens.
class ReadableWidth extends StatelessWidget {
  const ReadableWidth({super.key, required this.maxWidth, required this.child});

  final double maxWidth;
  final Widget child;

  @override
  Widget build(BuildContext context) => Align(
    alignment: Alignment.topCenter,
    // Takes the child's height, so it also works inside Columns and slivers.
    heightFactor: 1,
    child: ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth),
      child: child,
    ),
  );
}

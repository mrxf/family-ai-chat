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
      // Fill the column (up to maxWidth) so children keep their own
      // alignment: user bubbles stay right-aligned instead of the whole
      // message shrinking to its content and being centered.
      child: SizedBox(width: double.infinity, child: child),
    ),
  );
}

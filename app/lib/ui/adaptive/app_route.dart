import 'package:flutter/material.dart';

import '../desktop/desktop_page_header.dart';
import '../theme/theme.dart';
import 'breakpoints.dart';

/// The route for full-screen pages (settings, admin, memories...).
///
/// Use this instead of `MaterialPageRoute` so every page adapts the same way:
/// on wide windows outside a desktop workspace pane, the page is shown as a
/// centered column of readable width rather than stretched edge to edge.
MaterialPageRoute<T> appRoute<T>(WidgetBuilder builder, {RouteSettings? settings, bool fullscreenDialog = false}) => MaterialPageRoute<T>(
  settings: settings,
  fullscreenDialog: fullscreenDialog,
  builder: (context) => AdaptivePageFrame(child: builder(context)),
);

/// Frames a page for the current window size. See [appRoute].
class AdaptivePageFrame extends StatelessWidget {
  const AdaptivePageFrame({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    if (DesktopPaneScope.of(context)) return child;
    final width = MediaQuery.sizeOf(context).width;
    if (!WindowSize.of(width).isExpanded) return child;
    final p = context.pal;
    return ColoredBox(
      color: p.surfaceAlt,
      child: Center(
        child: Container(
          constraints: const BoxConstraints(maxWidth: ContentWidth.page),
          decoration: BoxDecoration(
            border: Border.symmetric(vertical: BorderSide(color: p.line)),
          ),
          child: child,
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';

import '../theme/theme.dart';

/// Marks a subtree as hosted inside the desktop workspace right pane.
///
/// Nested routes pushed via `appRoute(...)` check this scope so they fill the
/// pane naturally instead of drawing the centered vertical-border frame meant
/// for root-level routes.
class DesktopPaneScope extends InheritedWidget {
  const DesktopPaneScope({super.key, required super.child});

  static bool of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<DesktopPaneScope>() != null;

  @override
  bool updateShouldNotify(DesktopPaneScope oldWidget) => false;
}

/// Unified header for desktop workspace panes (Settings, Schedules, Memories, Admin).
class DesktopPageHeader extends StatelessWidget {
  const DesktopPageHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.actions = const [],
    this.maxWidth = 860,
  });

  final String title;
  final String? subtitle;
  final List<Widget> actions;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      decoration: BoxDecoration(
        color: p.bg,
        border: Border(bottom: BorderSide(color: p.line)),
      ),
      child: Center(
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: maxWidth),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(32, 22, 32, 18),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        title,
                        style: TextStyle(fontSize: 23, fontWeight: FontWeight.w700, color: p.text, letterSpacing: -0.2),
                      ),
                      if (subtitle != null && subtitle!.isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(subtitle!, style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.4)),
                      ],
                    ],
                  ),
                ),
                if (actions.isNotEmpty) ...[
                  const SizedBox(width: 16),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      for (var i = 0; i < actions.length; i++) ...[
                        if (i > 0) const SizedBox(width: 8),
                        actions[i],
                      ],
                    ],
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Centers desktop page content to a comfortable reading width without adding
/// side borders.
class DesktopPageBody extends StatelessWidget {
  const DesktopPageBody({super.key, required this.child, this.maxWidth = 860});

  final Widget child;
  final double maxWidth;

  @override
  Widget build(BuildContext context) => Align(
        alignment: Alignment.topCenter,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: maxWidth),
          child: child,
        ),
      );
}

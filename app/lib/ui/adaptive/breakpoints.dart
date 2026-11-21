import 'package:flutter/widgets.dart';

/// Window size classes (Material 3). Layout follows the window, not the
/// device: a narrow desktop window gets the phone layout, a landscape tablet
/// gets the two-pane layout.
enum WindowSize {
  /// Phones in portrait, narrow desktop windows.
  compact,

  /// Large phones in landscape, tablets in portrait.
  medium,

  /// Tablets in landscape, desktop windows: sidebar + content side by side.
  expanded;

  static const mediumMin = 600.0;
  static const expandedMin = 840.0;

  static WindowSize of(double width) => width >= expandedMin
      ? WindowSize.expanded
      : width >= mediumMin
      ? WindowSize.medium
      : WindowSize.compact;

  bool get isExpanded => this == WindowSize.expanded;
}

/// Widths that keep text comfortable to read on wide windows.
abstract final class ContentWidth {
  /// The conversation column (messages + composer).
  static const chat = 820.0;

  /// Settings, admin and other secondary pages.
  static const page = 760.0;

  /// Conversation sidebar in the two-pane layout.
  static const sidebar = 300.0;
}

extension WindowSizeContext on BuildContext {
  WindowSize get windowSize => WindowSize.of(MediaQuery.sizeOf(this).width);
}

import 'package:flutter/material.dart';

import '../theme/theme.dart';
import 'breakpoints.dart';

/// Presents modal content as a centered desktop dialog on expanded windows and
/// as a bottom sheet on compact/medium windows.
Future<T?> showAdaptiveModal<T>({
  required BuildContext context,
  required WidgetBuilder builder,
  double maxWidth = 500,
  bool isDismissible = true,
  bool enableDrag = true,
}) {
  if (!context.windowSize.isExpanded) {
    return showModalBottomSheet<T>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      isDismissible: isDismissible,
      enableDrag: enableDrag,
      builder: builder,
    );
  }
  return showDialog<T>(
    context: context,
    barrierDismissible: isDismissible,
    builder: (ctx) {
      final p = ctx.pal;
      final maxHeight = MediaQuery.sizeOf(ctx).height * 0.86;
      return Dialog(
        backgroundColor: p.surface,
        surfaceTintColor: Colors.transparent,
        insetPadding: const EdgeInsets.symmetric(horizontal: 32, vertical: 28),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(22),
          side: BorderSide(color: p.line),
        ),
        clipBehavior: Clip.antiAlias,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: maxWidth, maxHeight: maxHeight),
          child: Padding(
            padding: const EdgeInsets.only(top: 16),
            child: builder(ctx),
          ),
        ),
      );
    },
  );
}

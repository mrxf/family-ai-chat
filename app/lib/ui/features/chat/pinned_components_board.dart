import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../state/pinned_components.dart';
import '../../adaptive/breakpoints.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import 'components/component_card.dart';
import 'components/component_card_shell.dart';
import 'components/suggestions_card.dart';

/// Responsive multi-column board for the home screen, hosting both conversation
/// components pinned by the user and built-in home cards such as [SuggestionsCard].
///
/// Column count scales smoothly from 1 column on phones up to 4 columns on 4K
/// desktop windows based on parent constraints (never MediaQuery screen width).
class PinnedComponentsBoard extends ConsumerWidget {
  const PinnedComponentsBoard({
    super.key,
    required this.onSuggestion,
    this.includeDraw = false,
  });

  final void Function(String text) onSuggestion;
  final bool includeDraw;

  /// Computes the column count for the available width, supporting up to 4 columns on 4K displays.
  static int columnCountForWidth(double width, int itemCount) {
    if (itemCount <= 1) return 1;
    final maxCols = width >= 1520
        ? 4
        : width >= 1120
            ? 3
            : width >= 720
                ? 2
                : 1;
    return math.min(maxCols, itemCount);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final board = ref.watch(pinnedComponentsProvider);
    final notifier = ref.read(pinnedComponentsProvider.notifier);

    final cards = <Widget>[
      for (final pin in board.items)
        HomePinnedComponentShell(
          key: ValueKey('pin-${pin.messageId}-${pin.componentId}'),
          pin: pin,
          child: ChatComponentCard(
            messageId: pin.messageId,
            component: pin.component,
            isLastMessage: false,
            onStateChanged: (nextState) =>
                notifier.updateComponentState(pin.messageId, pin.componentId, nextState),
          ),
        ),
      if (board.suggestionsPinned)
        SuggestionsCard(
          key: const ValueKey('builtin-suggestions'),
          onSuggestion: onSuggestion,
          includeDraw: includeDraw,
          onUnpin: () {
            notifier.setSuggestionsPinned(false);
            showToast(context, '已取消固定「猜你想问」，可在下方随时恢复');
          },
        ),
    ];

    if (cards.isEmpty) {
      return Center(
        child: _RestoreSuggestionsPill(
          onTap: () => notifier.setSuggestionsPinned(true),
        ),
      );
    }

    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 16.0;
        final availW = constraints.maxWidth.isFinite ? constraints.maxWidth : ContentWidth.chat;
        final cols = columnCountForWidth(availW, cards.length);
        // Keep a single card at the comfortable chat width; let multi-column boards breathe up to 4K width.
        final boardMaxW = cols == 1 ? ContentWidth.chat : math.min(availW, 1680.0);

        final buckets = List.generate(cols, (_) => <Widget>[]);
        for (var i = 0; i < cards.length; i++) {
          buckets[i % cols].add(cards[i]);
        }

        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Center(
              child: ConstrainedBox(
                constraints: BoxConstraints(maxWidth: boardMaxW),
                child: cols == 1
                    ? Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          for (var i = 0; i < cards.length; i++) ...[
                            if (i > 0) const SizedBox(height: gap),
                            cards[i],
                          ],
                        ],
                      )
                    : Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          for (var c = 0; c < cols; c++) ...[
                            if (c > 0) const SizedBox(width: gap),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  for (var i = 0; i < buckets[c].length; i++) ...[
                                    if (i > 0) const SizedBox(height: gap),
                                    buckets[c][i],
                                  ],
                                ],
                              ),
                            ),
                          ],
                        ],
                      ),
              ),
            ),
            if (!board.suggestionsPinned) ...[
              const SizedBox(height: 12),
              Center(
                child: _RestoreSuggestionsPill(
                  onTap: () => notifier.setSuggestionsPinned(true),
                ),
              ),
            ],
            if (board.items.isEmpty) ...[
              const SizedBox(height: 10),
              Center(
                child: Text(
                  '小贴士：对话里的天气、待办清单等卡片都可以固定到这里',
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 12.5, color: p.faint),
                ),
              ),
            ],
          ],
        );
      },
    );
  }
}

class _RestoreSuggestionsPill extends StatelessWidget {
  const _RestoreSuggestionsPill({required this.onTap});
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.96,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        decoration: BoxDecoration(
          color: p.surfaceAlt,
          borderRadius: BorderRadius.circular(999),
          border: Border.all(color: p.line),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            AppIcon(AppIcons.pin, size: 13.5, color: p.muted),
            const SizedBox(width: 5),
            Text('恢复「猜你想问」卡片', style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w500)),
          ],
        ),
      ),
    );
  }
}

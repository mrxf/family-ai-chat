import 'package:flutter/material.dart';

import '../../../../core/models/models.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../components/component_card_shell.dart';

/// Preview strip mounted above the composer input when quoting a component into a chat.
class ComposerQuoteBar extends StatelessWidget {
  const ComposerQuoteBar({
    super.key,
    required this.quote,
    required this.onDismiss,
  });

  final QuotedComponent quote;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      margin: const EdgeInsets.fromLTRB(12, 10, 12, 2),
      padding: const EdgeInsets.fromLTRB(12, 9, 8, 9),
      decoration: BoxDecoration(
        color: p.surfaceAlt,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: p.accent.withValues(alpha: 0.28)),
      ),
      child: Row(
        children: [
          Container(
            width: 30,
            height: 30,
            decoration: BoxDecoration(
              color: p.accentSoft,
              borderRadius: BorderRadius.circular(9),
            ),
            child: Center(child: AppIcon(componentKindIcon(quote.kind), size: 16.5, color: p.accent)),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '引用 · ${quote.title}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.text),
                ),
                if (quote.summary.isNotEmpty)
                  Text(
                    quote.summary,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12, color: p.muted),
                  ),
              ],
            ),
          ),
          Tooltip(
            message: '移除引用',
            child: Pressable(
              scale: 0.9,
              onTap: onDismiss,
              child: Padding(
                padding: const EdgeInsets.all(6),
                child: AppIcon(AppIcons.closeSmall, size: 16, color: p.muted),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Compact quoted-component badge displayed above a user message bubble.
class QuotedComponentChip extends StatelessWidget {
  const QuotedComponentChip({
    super.key,
    required this.quote,
    this.maxWidth = 320,
    this.onTap,
  });

  final QuotedComponent quote;
  final double maxWidth;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final content = Container(
      constraints: BoxConstraints(maxWidth: maxWidth),
      padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 7),
      decoration: BoxDecoration(
        color: p.surfaceAlt,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: p.line),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          AppIcon(componentKindIcon(quote.kind), size: 15, color: p.accent),
          const SizedBox(width: 7),
          Flexible(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  quote.title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.text),
                ),
                if (quote.summary.isNotEmpty)
                  Text(
                    quote.summary,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 11.5, color: p.muted),
                  ),
              ],
            ),
          ),
          if (onTap != null) ...[
            const SizedBox(width: 5),
            AppIcon(AppIcons.arrowUpRight, size: 13.5, color: p.faint),
          ],
        ],
      ),
    );
    if (onTap == null) return content;
    return Tooltip(
      message: quote.conversationTitle.isEmpty ? '查看原对话' : '查看原对话：${quote.conversationTitle}',
      child: Pressable(scale: 0.98, onTap: onTap, child: content),
    );
  }
}

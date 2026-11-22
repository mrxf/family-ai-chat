import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/sheets.dart';

const _defaultSuggestions = [
  ('🎂', '帮我写一段给家人的生日祝福'),
  ('🥗', '高血压饮食上要注意什么？'),
  ('🍳', '教我做一道简单的家常菜'),
  ('🛡️', '怎么分辨诈骗电话和短信？'),
  ('😴', '晚上睡不着有什么好办法？'),
  ('📖', '给我讲个有意思的历史小故事'),
  ('🧳', '帮我列一份出门旅行的行李清单'),
  ('💡', '用大白话解释一下什么是通货膨胀'),
  ('📱', '手机内存满了怎么清理？'),
  ('🌱', '阳台适合种哪些好养的花？'),
  ('🧮', '帮孩子出 10 道口算练习题'),
  ('✈️', '周末带孩子去哪里玩比较好？'),
];

const _drawSuggestions = [
  ('🎨', '帮我画一只可爱的小猫'),
  ('🖼️', '画一张春天公园的水彩画'),
  ('🧧', '帮我画一张新年贺卡，写上「阖家幸福」'),
  ('🌄', '画一张适合做手机壁纸的风景画'),
];

/// Built-in component-level recommendation card pinned by default on the home board.
///
/// Styled identically to chat components ([TodoCard], [MetricsCard], etc.) so it
/// participates natively in the multi-column home board and can be unpinned anytime.
class SuggestionsCard extends StatefulWidget {
  const SuggestionsCard({
    super.key,
    required this.onSuggestion,
    this.includeDraw = false,
    this.onUnpin,
  });

  final void Function(String text) onSuggestion;
  final bool includeDraw;
  final VoidCallback? onUnpin;

  @override
  State<SuggestionsCard> createState() => _SuggestionsCardState();
}

class _SuggestionsCardState extends State<SuggestionsCard> {
  late List<(String, String)> _picks = _shuffle();
  late final _drawPick = _drawSuggestions[math.Random().nextInt(_drawSuggestions.length)];

  List<(String, String)> _shuffle() => (List.of(_defaultSuggestions)..shuffle(math.Random())).take(4).toList();

  void _openMenu() {
    if (widget.onUnpin == null) return;
    showActionSheet(context, actions: [
      SheetAction(AppIcons.refresh, '换一批问题', () => setState(() => _picks = _shuffle())),
      SheetAction(AppIcons.unpin, '从首页取消固定', widget.onUnpin!, danger: true),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final items = widget.includeDraw ? [_drawPick, ..._picks.take(3)] : _picks;

    return GestureDetector(
      behavior: HitTestBehavior.translucent,
      onSecondaryTap: widget.onUnpin != null ? _openMenu : null,
      onLongPress: widget.onUnpin != null ? _openMenu : null,
      child: Container(
        decoration: BoxDecoration(
          color: p.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: p.line),
          boxShadow: [BoxShadow(color: p.shadow, blurRadius: 12, offset: const Offset(0, 4))],
        ),
        padding: const EdgeInsets.fromLTRB(16, 15, 16, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 34,
                  height: 34,
                  decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(11)),
                  child: Center(child: AppIcon(AppIcons.sparkles, size: 19, color: p.accent)),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('猜你想问', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: p.text)),
                      Text('随便点一句就能开聊', style: TextStyle(fontSize: 12.5, color: p.muted)),
                    ],
                  ),
                ),
                Tooltip(
                  message: '换一批',
                  child: Pressable(
                    scale: 0.92,
                    onTap: () => setState(() => _picks = _shuffle()),
                    child: Padding(
                      padding: const EdgeInsets.all(6),
                      child: AppIcon(AppIcons.refresh, size: 17, color: p.muted),
                    ),
                  ),
                ),
                if (widget.onUnpin != null)
                  Tooltip(
                    message: '取消固定',
                    child: Pressable(
                      scale: 0.92,
                      onTap: widget.onUnpin,
                      child: Padding(
                        padding: const EdgeInsets.all(6),
                        child: AppIcon(AppIcons.unpin, size: 16.5, color: p.muted),
                      ),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 10),
            for (var i = 0; i < items.length; i++) ...[
              if (i > 0) Divider(height: 1, color: p.line.withValues(alpha: 0.6)),
              Pressable(
                scale: 0.99,
                onTap: () => widget.onSuggestion(items[i].$2),
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 10),
                  child: Row(
                    children: [
                      if (items[i].$1.isNotEmpty) ...[
                        Text(items[i].$1, style: const TextStyle(fontSize: 18)),
                        const SizedBox(width: 10),
                      ],
                      Expanded(
                        child: Text(
                          items[i].$2,
                          style: TextStyle(fontSize: 15, height: 1.38, color: p.text),
                        ),
                      ),
                      const SizedBox(width: 6),
                      AppIcon(AppIcons.arrowUpRight, size: 15.5, color: p.faint),
                    ],
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

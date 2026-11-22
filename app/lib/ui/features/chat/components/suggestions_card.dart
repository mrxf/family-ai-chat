import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/suggestions.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/sheets.dart';

const _defaultSuggestions = [
  HomeSuggestion(emoji: '🎂', text: '帮我写一段给家人的生日祝福'),
  HomeSuggestion(emoji: '🥗', text: '高血压饮食上要注意什么？'),
  HomeSuggestion(emoji: '🍳', text: '教我做一道简单的家常菜'),
  HomeSuggestion(emoji: '🛡️', text: '怎么分辨诈骗电话和短信？'),
  HomeSuggestion(emoji: '😴', text: '晚上睡不着有什么好办法？'),
  HomeSuggestion(emoji: '📖', text: '给我讲个有意思的历史小故事'),
  HomeSuggestion(emoji: '🧳', text: '帮我列一份出门旅行的行李清单'),
  HomeSuggestion(emoji: '💡', text: '用大白话解释一下什么是通货膨胀'),
  HomeSuggestion(emoji: '📱', text: '手机内存满了怎么清理？'),
  HomeSuggestion(emoji: '🌱', text: '阳台适合种哪些好养的花？'),
  HomeSuggestion(emoji: '🧮', text: '帮孩子出 10 道口算练习题'),
  HomeSuggestion(emoji: '✈️', text: '周末带孩子去哪里玩比较好？'),
];

const _drawSuggestions = [
  HomeSuggestion(emoji: '🎨', text: '帮我画一只可爱的小猫'),
  HomeSuggestion(emoji: '🖼️', text: '画一张春天公园的水彩画'),
  HomeSuggestion(emoji: '🧧', text: '帮我画一张新年贺卡，写上「阖家幸福」'),
  HomeSuggestion(emoji: '🌄', text: '画一张适合做手机壁纸的风景画'),
];

/// Built-in component-level recommendation card pinned by default on the home board.
///
/// Uses the server-generated personalized pool from [homeSuggestionsProvider] when
/// available, gracefully falling back to [_defaultSuggestions] for cold-start users.
/// "换一批" rotates locally across the full 10–12 item pool without extra LLM calls.
class SuggestionsCard extends ConsumerStatefulWidget {
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
  ConsumerState<SuggestionsCard> createState() => _SuggestionsCardState();
}

class _SuggestionsCardState extends ConsumerState<SuggestionsCard> {
  final _rng = math.Random();
  String _poolSignature = '';
  List<HomeSuggestion> _deck = const [];
  int _cursor = 0;
  List<HomeSuggestion> _picks = const [];
  late HomeSuggestion _drawPick = _drawSuggestions[_rng.nextInt(_drawSuggestions.length)];

  List<HomeSuggestion> _effectivePool(List<HomeSuggestion> personalized) {
    if (personalized.length >= 4) return personalized;
    if (personalized.isEmpty) return _defaultSuggestions;
    final seen = {for (final s in personalized) s.text};
    return [
      ...personalized,
      for (final d in _defaultSuggestions)
        if (!seen.contains(d.text)) d,
    ];
  }

  void _syncPool(List<HomeSuggestion> personalized) {
    final pool = _effectivePool(personalized);
    final sig = pool.map((e) => e.text).join('\u0000');
    if (sig == _poolSignature && _picks.isNotEmpty) return;
    _poolSignature = sig;
    _deck = List.of(pool)..shuffle(_rng);
    _cursor = 0;
    _picks = _takeNext(4);
  }

  List<HomeSuggestion> _takeNext(int count) {
    if (_deck.isEmpty) return const [];
    final out = <HomeSuggestion>[];
    final seen = <String>{};
    var guard = 0;
    while (out.length < count && guard < _deck.length * 2) {
      guard++;
      if (_cursor >= _deck.length) {
        _deck = List.of(_deck)..shuffle(_rng);
        _cursor = 0;
      }
      final candidate = _deck[_cursor++];
      if (seen.add(candidate.text)) {
        out.add(candidate);
      }
    }
    return out;
  }

  void _rotate() {
    setState(() {
      _picks = _takeNext(4);
      if (widget.includeDraw) {
        _drawPick = _drawSuggestions[_rng.nextInt(_drawSuggestions.length)];
      }
    });
  }

  void _openMenu() {
    if (widget.onUnpin == null) return;
    showActionSheet(context, actions: [
      SheetAction(AppIcons.refresh, '换一批问题', _rotate),
      SheetAction(AppIcons.unpin, '从首页取消固定', widget.onUnpin!, danger: true),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final sugState = ref.watch(homeSuggestionsProvider);
    _syncPool(sugState.items);

    final items = widget.includeDraw ? [_drawPick, ..._picks.take(3)] : _picks;
    final subtitle = sugState.isPersonalized ? '根据你的近期对话定制' : '随便点一句就能开聊';

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
                      Text(subtitle, style: TextStyle(fontSize: 12.5, color: p.muted)),
                    ],
                  ),
                ),
                Tooltip(
                  message: '换一批',
                  child: Pressable(
                    scale: 0.92,
                    onTap: _rotate,
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
                onTap: () => widget.onSuggestion(items[i].text),
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 10),
                  child: Row(
                    children: [
                      if (items[i].emoji.isNotEmpty) ...[
                        Text(items[i].emoji, style: const TextStyle(fontSize: 18)),
                        const SizedBox(width: 10),
                      ],
                      Expanded(
                        child: Text(
                          items[i].text,
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

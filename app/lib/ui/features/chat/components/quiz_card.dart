import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

class QuizCard extends ConsumerWidget {
  const QuizCard({super.key, required this.messageId, required this.component});
  final String messageId;
  final MessageComponent component;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final d = component.data;
    final question = '${d['question'] ?? ''}'.trim();
    final tag = '${d['tag'] ?? '互动小测'}'.trim();
    final options = [
      if (d['options'] is List)
        for (final o in d['options'] as List) '$o',
    ];
    final answerIdx = ((d['answer_index'] as num?)?.toInt() ?? 0).clamp(0, math.max<int>(0, options.length - 1));
    final explanation = '${d['explanation'] ?? ''}'.trim();

    if (question.isEmpty || options.length < 2) return const SizedBox.shrink();

    final selectedIdx = (component.state['selected_index'] as num?)?.toInt() ?? -1;
    final answered = selectedIdx >= 0 && selectedIdx < options.length;
    final isRight = answered && selectedIdx == answerIdx;

    void choose(int idx) {
      HapticFeedback.lightImpact();
      ref.read(chatProvider.notifier).updateComponentState(
        messageId,
        component.id,
        {'selected_index': idx},
      );
    }

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(
          color: !answered
              ? p.line
              : (isRight ? p.good.withValues(alpha: 0.42) : p.accent.withValues(alpha: 0.42)),
        ),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 12, offset: const Offset(0, 4))],
      ),
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(10)),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(AppIcons.question, size: 15, color: p.accent),
                    const SizedBox(width: 5),
                    Text(
                      tag.isEmpty ? '互动小测' : tag,
                      style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.accent),
                    ),
                  ],
                ),
              ),
              const Spacer(),
              if (answered)
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(
                      isRight ? AppIcons.checkCircleFill : AppIcons.thinkOn,
                      size: 15,
                      color: isRight ? p.good : p.accent,
                    ),
                    const SizedBox(width: 4),
                    Text(
                      isRight ? '答对啦' : '看解析涨知识',
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                        color: isRight ? p.good : p.accent,
                      ),
                    ),
                  ],
                )
              else
                Text('点选项揭晓答案', style: TextStyle(fontSize: 12.5, color: p.faint)),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            question,
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, height: 1.45, color: p.text),
          ),
          const SizedBox(height: 12),
          for (var i = 0; i < options.length; i++) ...[
            if (i > 0) const SizedBox(height: 8),
            _QuizOptionTile(
              letter: String.fromCharCode(65 + i),
              text: options[i],
              answered: answered,
              isSelected: selectedIdx == i,
              isCorrect: answerIdx == i,
              onTap: () => choose(i),
            ),
          ],
          AnimatedSize(
            duration: const Duration(milliseconds: 240),
            curve: Curves.easeOutCubic,
            child: answered
                ? Container(
                    margin: const EdgeInsets.only(top: 12),
                    padding: const EdgeInsets.all(12),
                    decoration: BoxDecoration(
                      color: isRight ? p.good.withValues(alpha: 0.08) : p.surfaceAlt,
                      borderRadius: BorderRadius.circular(14),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Text(
                              '正确答案：${String.fromCharCode(65 + answerIdx)}',
                              style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w700, color: p.good),
                            ),
                            const Spacer(),
                            Pressable(
                              onTap: () => choose(-1),
                              child: Text('重做', style: TextStyle(fontSize: 12.5, color: p.muted)),
                            ),
                          ],
                        ),
                        if (explanation.isNotEmpty) ...[
                          const SizedBox(height: 5),
                          Text(
                            explanation,
                            style: TextStyle(fontSize: 14, height: 1.48, color: p.text),
                          ),
                        ],
                      ],
                    ),
                  )
                : const SizedBox(width: double.infinity),
          ),
        ],
      ),
    );
  }
}

class _QuizOptionTile extends StatelessWidget {
  const _QuizOptionTile({
    required this.letter,
    required this.text,
    required this.answered,
    required this.isSelected,
    required this.isCorrect,
    required this.onTap,
  });

  final String letter, text;
  final bool answered, isSelected, isCorrect;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    Color bg = p.surfaceAlt.withValues(alpha: 0.55);
    Color border = p.line;
    Color badgeBg = p.surface;
    Color badgeFg = p.muted;

    if (answered) {
      if (isCorrect) {
        bg = p.good.withValues(alpha: 0.11);
        border = p.good.withValues(alpha: 0.55);
        badgeBg = p.good;
        badgeFg = Colors.white;
      } else if (isSelected) {
        bg = p.bad.withValues(alpha: 0.09);
        border = p.bad.withValues(alpha: 0.5);
        badgeBg = p.bad;
        badgeFg = Colors.white;
      }
    }

    return Pressable(
      scale: 0.99,
      onTap: answered ? null : onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 11),
        decoration: BoxDecoration(
          color: bg,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: border),
        ),
        child: Row(
          children: [
            Container(
              width: 26,
              height: 26,
              decoration: BoxDecoration(
                color: badgeBg,
                shape: BoxShape.circle,
                border: answered && (isCorrect || isSelected) ? null : Border.all(color: p.line),
              ),
              child: Center(
                child: Text(
                  letter,
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w700, color: badgeFg),
                ),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                text,
                style: TextStyle(
                  fontSize: 14.5,
                  height: 1.38,
                  fontWeight: (answered && isCorrect) ? FontWeight.w600 : FontWeight.w400,
                  color: p.text,
                ),
              ),
            ),
            if (answered && isCorrect)
              AppIcon(AppIcons.checkCircleFill, size: 19, color: p.good)
            else if (answered && isSelected && !isCorrect)
              AppIcon(AppIcons.closeCircle, size: 19, color: p.bad),
          ],
        ),
      ),
    );
  }
}

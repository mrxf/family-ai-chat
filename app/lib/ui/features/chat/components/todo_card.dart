import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

class TodoCard extends ConsumerWidget {
  const TodoCard({super.key, required this.messageId, required this.component});
  final String messageId;
  final MessageComponent component;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final d = component.data;
    final title = '${d['title'] ?? '待办清单'}'.trim();
    final subtitle = '${d['subtitle'] ?? ''}'.trim();
    final rawItems = d['items'] is List ? (d['items'] as List) : const [];

    final hasSavedState = component.state.containsKey('done_ids');
    final doneSet = <String>{
      if (hasSavedState && component.state['done_ids'] is List)
        for (final id in component.state['done_ids'] as List) '$id',
    };

    final items = <({String id, String text, String note, String tag, bool done})>[];
    for (var i = 0; i < rawItems.length; i++) {
      final m = rawItems[i];
      if (m is! Map) continue;
      final id = '${m['id'] ?? 'i${i + 1}'}';
      final initialDone = m['done'] == true;
      final isDone = hasSavedState ? doneSet.contains(id) : initialDone;
      if (!hasSavedState && initialDone) doneSet.add(id);
      items.add((
        id: id,
        text: '${m['text'] ?? ''}',
        note: '${m['note'] ?? ''}',
        tag: '${m['tag'] ?? ''}',
        done: isDone,
      ));
    }
    if (items.isEmpty) return const SizedBox.shrink();

    final doneCount = items.where((e) => e.done).length;
    final progress = doneCount / items.length;
    final allDone = doneCount == items.length;

    void toggle(String id) {
      HapticFeedback.selectionClick();
      final next = {...doneSet};
      if (!next.remove(id)) next.add(id);
      final ordered = [for (final it in items) if (next.contains(it.id)) it.id];
      ref.read(chatProvider.notifier).updateComponentState(
        messageId,
        component.id,
        {'done_ids': ordered},
      );
    }

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: allDone ? p.good.withValues(alpha: 0.38) : p.line),
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
                decoration: BoxDecoration(
                  color: allDone ? p.good.withValues(alpha: 0.14) : p.accentSoft,
                  borderRadius: BorderRadius.circular(11),
                ),
                child: Center(
                  child: AppIcon(
                    allDone ? AppIcons.checkCircle : AppIcons.listChecks,
                    size: 19,
                    color: allDone ? p.good : p.accent,
                  ),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: p.text)),
                    if (subtitle.isNotEmpty)
                      Text(subtitle, style: TextStyle(fontSize: 12.5, color: p.muted)),
                  ],
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                decoration: BoxDecoration(
                  color: allDone ? p.good.withValues(alpha: 0.14) : p.surfaceAlt,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  allDone ? '全部完成 ✓' : '$doneCount / ${items.length}',
                  style: TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                    color: allDone ? p.good : p.accent,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 11),
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: TweenAnimationBuilder<double>(
              tween: Tween(begin: 0, end: progress),
              duration: const Duration(milliseconds: 260),
              curve: Curves.easeOutCubic,
              builder: (_, val, _) => LinearProgressIndicator(
                value: val,
                minHeight: 5,
                backgroundColor: p.surfaceAlt,
                valueColor: AlwaysStoppedAnimation(allDone ? p.good : p.accent),
              ),
            ),
          ),
          const SizedBox(height: 8),
          for (var i = 0; i < items.length; i++) ...[
            if (i > 0) Divider(height: 1, color: p.line.withValues(alpha: 0.6)),
            Pressable(
              scale: 0.99,
              onTap: () => toggle(items[i].id),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 9),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 1),
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 180),
                        width: 22,
                        height: 22,
                        decoration: BoxDecoration(
                          color: items[i].done ? p.good : Colors.transparent,
                          borderRadius: BorderRadius.circular(7),
                          border: Border.all(
                            color: items[i].done ? p.good : p.faint,
                            width: 1.6,
                          ),
                        ),
                        child: items[i].done
                            ? const Center(child: Icon(Icons.check_rounded, size: 15, color: Colors.white))
                            : null,
                      ),
                    ),
                    const SizedBox(width: 11),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  items[i].text,
                                  style: TextStyle(
                                    fontSize: 15,
                                    height: 1.38,
                                    color: items[i].done ? p.muted : p.text,
                                    decoration: items[i].done ? TextDecoration.lineThrough : null,
                                    decorationColor: p.muted,
                                  ),
                                ),
                              ),
                              if (items[i].tag.isNotEmpty) ...[
                                const SizedBox(width: 6),
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: p.surfaceAlt,
                                    borderRadius: BorderRadius.circular(8),
                                  ),
                                  child: Text(
                                    items[i].tag,
                                    style: TextStyle(fontSize: 11.5, color: p.muted, fontWeight: FontWeight.w500),
                                  ),
                                ),
                              ],
                            ],
                          ),
                          if (items[i].note.isNotEmpty) ...[
                            const SizedBox(height: 2),
                            Text(items[i].note, style: TextStyle(fontSize: 12.5, color: p.faint)),
                          ],
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

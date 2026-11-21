import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

class AskUserCard extends ConsumerStatefulWidget {
  const AskUserCard({
    super.key,
    required this.messageId,
    required this.component,
    required this.isLastMessage,
  });

  final String messageId;
  final MessageComponent component;
  final bool isLastMessage;

  @override
  ConsumerState<AskUserCard> createState() => _AskUserCardState();
}

class _AskUserCardState extends ConsumerState<AskUserCard> {
  int _step = 0;
  final Map<String, Set<String>> _selected = {};
  final Map<String, bool> _useCustom = {};
  final Map<String, TextEditingController> _customCtrls = {};
  bool _hydrated = false;
  bool _forceExpanded = false;

  @override
  void dispose() {
    for (final c in _customCtrls.values) {
      c.dispose();
    }
    super.dispose();
  }

  TextEditingController _ctrlFor(String qid) =>
      _customCtrls.putIfAbsent(qid, () => TextEditingController()..addListener(() => setState(() {})));

  void _hydrateFromStateIfNeeded() {
    if (_hydrated) return;
    _hydrated = true;
    final ans = widget.component.state['answers'];
    if (ans is Map) {
      for (final entry in ans.entries) {
        final qid = '${entry.key}';
        final v = entry.value;
        if (v is Map) {
          if (v['selected'] is List) {
            _selected[qid] = {for (final s in v['selected'] as List) '$s'};
          }
          final custom = '${v['custom'] ?? ''}'.trim();
          if (custom.isNotEmpty) {
            _useCustom[qid] = true;
            _ctrlFor(qid).text = custom;
          }
        }
      }
    }
  }

  List<({
    String id,
    String question,
    bool multi,
    List<({String label, String desc, bool recommended})> options,
  })> _parseQuestions() {
    final raw = widget.component.data['questions'];
    if (raw is! List) return const [];
    final out = <({
      String id,
      String question,
      bool multi,
      List<({String label, String desc, bool recommended})> options,
    })>[];
    for (var i = 0; i < raw.length; i++) {
      final m = raw[i];
      if (m is! Map) continue;
      final opts = <({String label, String desc, bool recommended})>[];
      if (m['options'] is List) {
        for (final o in m['options'] as List) {
          if (o is Map) {
            final lbl = '${o['label'] ?? ''}'.trim();
            if (lbl.isNotEmpty) {
              opts.add((
                label: lbl,
                desc: '${o['description'] ?? ''}'.trim(),
                recommended: o['recommended'] == true,
              ));
            }
          } else {
            final lbl = '$o'.trim();
            if (lbl.isNotEmpty) opts.add((label: lbl, desc: '', recommended: false));
          }
        }
      }
      if (opts.isNotEmpty) {
        out.add((
          id: '${m['id'] ?? 'q${i + 1}'}',
          question: '${m['question'] ?? ''}'.trim(),
          multi: m['multi_select'] == true,
          options: opts,
        ));
      }
    }
    return out;
  }

  bool _questionHasAnswer(String qid) {
    final sel = _selected[qid];
    if (sel != null && sel.isNotEmpty) return true;
    if (_useCustom[qid] == true && _ctrlFor(qid).text.trim().isNotEmpty) return true;
    return false;
  }

  List<String> _answerLabelsFor(String qid) {
    final parts = <String>[...?_selected[qid]];
    if (_useCustom[qid] == true) {
      final c = _ctrlFor(qid).text.trim();
      if (c.isNotEmpty) parts.add(c);
    }
    return parts;
  }

  void _toggleOption(String qid, String label, bool multi, int totalQuestions) {
    HapticFeedback.selectionClick();
    setState(() {
      final set = _selected.putIfAbsent(qid, () => <String>{});
      if (multi) {
        if (!set.remove(label)) set.add(label);
      } else {
        set
          ..clear()
          ..add(label);
        _useCustom[qid] = false;
        // Smoothly step forward when answering single-choice multi-question prompts.
        if (_step < totalQuestions - 1) {
          Future.delayed(const Duration(milliseconds: 180), () {
            if (mounted && _step < totalQuestions - 1) {
              setState(() => _step++);
            }
          });
        }
      }
    });
  }

  void _toggleCustom(String qid, bool multi) {
    HapticFeedback.selectionClick();
    setState(() {
      final next = !(_useCustom[qid] ?? false);
      _useCustom[qid] = next;
      if (next && !multi) {
        _selected[qid]?.clear();
      }
    });
  }

  Future<void> _submit(
    List<({
      String id,
      String question,
      bool multi,
      List<({String label, String desc, bool recommended})> options,
    })> questions,
  ) async {
    HapticFeedback.mediumImpact();
    final answersJson = <String, dynamic>{};
    final lines = <String>[];

    for (var i = 0; i < questions.length; i++) {
      final q = questions[i];
      final sel = _selected[q.id]?.toList() ?? const <String>[];
      final custom = (_useCustom[q.id] == true) ? _ctrlFor(q.id).text.trim() : '';
      answersJson[q.id] = {
        if (sel.isNotEmpty) 'selected': sel,
        if (custom.isNotEmpty) 'custom': custom,
      };
      final combined = [...sel, if (custom.isNotEmpty) custom];
      if (combined.isNotEmpty) {
        if (questions.length == 1) {
          lines.add(combined.join('、'));
        } else {
          lines.add('${i + 1}. ${q.question}：${combined.join('、')}');
        }
      }
    }
    if (lines.isEmpty) return;

    final replyText = questions.length == 1 ? lines.first : '我的选择：\n${lines.join('\n')}';
    setState(() => _forceExpanded = false);
    await ref.read(chatProvider.notifier).submitAskUserAnswer(
          widget.messageId,
          widget.component.id,
          {'submitted': true, 'dismissed': false, 'answers': answersJson},
          replyText,
        );
  }

  Future<void> _dismiss(
    List<({
      String id,
      String question,
      bool multi,
      List<({String label, String desc, bool recommended})> options,
    })> questions,
  ) async {
    HapticFeedback.lightImpact();
    final answersJson = <String, dynamic>{};
    for (final q in questions) {
      final sel = _selected[q.id]?.toList() ?? const <String>[];
      final custom = (_useCustom[q.id] == true) ? _ctrlFor(q.id).text.trim() : '';
      if (sel.isNotEmpty || custom.isNotEmpty) {
        answersJson[q.id] = {
          if (sel.isNotEmpty) 'selected': sel,
          if (custom.isNotEmpty) 'custom': custom,
        };
      }
    }
    setState(() => _forceExpanded = false);
    await ref.read(chatProvider.notifier).updateComponentState(
          widget.messageId,
          widget.component.id,
          {'submitted': false, 'dismissed': true, 'answers': answersJson},
        );
  }

  Future<void> _reopen() async {
    HapticFeedback.selectionClick();
    setState(() => _forceExpanded = true);
    if (widget.component.state['dismissed'] == true) {
      final next = Map<String, dynamic>.from(widget.component.state)..['dismissed'] = false;
      await ref.read(chatProvider.notifier).updateComponentState(
            widget.messageId,
            widget.component.id,
            next,
          );
    }
  }

  @override
  Widget build(BuildContext context) {
    _hydrateFromStateIfNeeded();
    final p = context.pal;
    final questions = _parseQuestions();
    if (questions.isEmpty) return const SizedBox.shrink();

    final title = '${widget.component.data['title'] ?? ''}'.trim();
    final submitted = widget.component.state['submitted'] == true;
    final dismissed = widget.component.state['dismissed'] == true;
    final chatBusy = ref.watch(chatProvider.select((s) => s.busy));

    // ---- Confirmed / Submitted Summary View ----
    if (submitted) {
      return Container(
        decoration: BoxDecoration(
          color: p.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: p.good.withValues(alpha: 0.35)),
        ),
        padding: const EdgeInsets.fromLTRB(16, 13, 16, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                AppIcon(AppIcons.checkCircleFill, size: 18, color: p.good),
                const SizedBox(width: 7),
                Text(
                  title.isEmpty ? '已确认偏好' : '$title · 已确认',
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w700, color: p.good),
                ),
              ],
            ),
            const SizedBox(height: 10),
            for (var i = 0; i < questions.length; i++) ...[
              if (i > 0) const SizedBox(height: 8),
              Builder(
                builder: (_) {
                  final vals = _answerLabelsFor(questions[i].id);
                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Text(
                          questions[i].question,
                          style: TextStyle(fontSize: 13.5, color: p.muted),
                        ),
                      ),
                      const SizedBox(width: 10),
                      Flexible(
                        child: Text(
                          vals.isEmpty ? '未指定' : vals.join('、'),
                          textAlign: TextAlign.right,
                          style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.text),
                        ),
                      ),
                    ],
                  );
                },
              ),
            ],
          ],
        ),
      );
    }

    // ---- Dismissed or User Continued Chatting Without Answering ----
    final autoCollapsed = !widget.isLastMessage && !_forceExpanded;
    if ((dismissed && !_forceExpanded) || autoCollapsed) {
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        decoration: BoxDecoration(
          color: p.surfaceAlt.withValues(alpha: 0.72),
          borderRadius: BorderRadius.circular(16),
          border: Border.all(color: p.line),
        ),
        child: Row(
          children: [
            AppIcon(AppIcons.sparkles, size: 15, color: p.muted),
            const SizedBox(width: 7),
            Expanded(
              child: Text(
                dismissed ? '已跳过偏好选择，直接自由对话' : '未点选偏好 · 已继续对话',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 13.5, color: p.muted),
              ),
            ),
            const SizedBox(width: 8),
            Pressable(
              onTap: _reopen,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
                child: Text(
                  '重新展开',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: p.accent),
                ),
              ),
            ),
          ],
        ),
      );
    }

    // ---- Interactive Selection View ----
    final curIdx = _step.clamp(0, questions.length - 1);
    final q = questions[curIdx];
    final answeredCount = questions.where((item) => _questionHasAnswer(item.id)).length;
    final canSubmit = answeredCount > 0 && !chatBusy;
    final curSel = _selected[q.id] ?? const <String>{};
    final customOn = _useCustom[q.id] == true;

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: p.accent.withValues(alpha: 0.34), width: 1.2),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 14, offset: const Offset(0, 5))],
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
                    AppIcon(AppIcons.sparkles, size: 14, color: p.accent),
                    const SizedBox(width: 5),
                    Text(
                      title.isEmpty ? '请帮小暖确认一下' : title,
                      style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.accent),
                    ),
                  ],
                ),
              ),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(8)),
                child: Text(
                  q.multi ? '可多选' : '单选',
                  style: TextStyle(fontSize: 11.5, color: p.muted, fontWeight: FontWeight.w500),
                ),
              ),
              const SizedBox(width: 6),
              Tooltip(
                message: '跳过不选',
                child: Pressable(
                  onTap: () => _dismiss(questions),
                  child: Container(
                    width: 26,
                    height: 26,
                    decoration: BoxDecoration(
                      color: p.surfaceAlt,
                      shape: BoxShape.circle,
                    ),
                    child: Center(child: AppIcon(AppIcons.close, size: 15, color: p.muted)),
                  ),
                ),
              ),
            ],
          ),
          if (questions.length > 1) ...[
            const SizedBox(height: 11),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (var i = 0; i < questions.length; i++) ...[
                    if (i > 0) const SizedBox(width: 6),
                    Pressable(
                      onTap: () => setState(() => _step = i),
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 180),
                        padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 5),
                        decoration: BoxDecoration(
                          color: curIdx == i
                              ? p.accent
                              : (_questionHasAnswer(questions[i].id) ? p.accentSoft : p.surfaceAlt),
                          borderRadius: BorderRadius.circular(14),
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(
                              '问题 ${i + 1}',
                              style: TextStyle(
                                fontSize: 12.5,
                                fontWeight: FontWeight.w600,
                                color: curIdx == i
                                    ? Colors.white
                                    : (_questionHasAnswer(questions[i].id) ? p.accent : p.muted),
                              ),
                            ),
                            if (_questionHasAnswer(questions[i].id) && curIdx != i) ...[
                              const SizedBox(width: 4),
                              Icon(Icons.check_rounded, size: 13, color: p.accent),
                            ],
                          ],
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
          const SizedBox(height: 12),
          Text(
            q.question,
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, height: 1.4, color: p.text),
          ),
          const SizedBox(height: 11),
          for (var i = 0; i < q.options.length; i++) ...[
            if (i > 0) const SizedBox(height: 8),
            Builder(
              builder: (_) {
                final opt = q.options[i];
                final active = curSel.contains(opt.label);
                return Pressable(
                  scale: 0.99,
                  onTap: () => _toggleOption(q.id, opt.label, q.multi, questions.length),
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 180),
                    padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 11),
                    decoration: BoxDecoration(
                      color: active ? p.accentSoft.withValues(alpha: 0.72) : p.surfaceAlt.withValues(alpha: 0.5),
                      borderRadius: BorderRadius.circular(14),
                      border: Border.all(
                        color: active ? p.accent : p.line,
                        width: active ? 1.5 : 1.0,
                      ),
                    ),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Padding(
                          padding: const EdgeInsets.only(top: 2),
                          child: AppIcon(
                            q.multi
                                ? (active ? AppIcons.checkSquare : AppIcons.square)
                                : (active ? AppIcons.radioOn : AppIcons.radioOff),
                            size: 19,
                            color: active ? p.accent : p.faint,
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  Flexible(
                                    child: Text(
                                      opt.label,
                                      style: TextStyle(
                                        fontSize: 15,
                                        fontWeight: active ? FontWeight.w600 : FontWeight.w500,
                                        color: p.text,
                                      ),
                                    ),
                                  ),
                                  if (opt.recommended) ...[
                                    const SizedBox(width: 6),
                                    Container(
                                      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1.5),
                                      decoration: BoxDecoration(
                                        color: p.accent.withValues(alpha: 0.15),
                                        borderRadius: BorderRadius.circular(6),
                                      ),
                                      child: Text(
                                        '推荐',
                                        style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: p.accent),
                                      ),
                                    ),
                                  ],
                                ],
                              ),
                              if (opt.desc.isNotEmpty) ...[
                                const SizedBox(height: 2),
                                Text(opt.desc, style: TextStyle(fontSize: 12.5, color: p.muted)),
                              ],
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                );
              },
            ),
          ],
          const SizedBox(height: 8),
          Pressable(
            scale: 0.99,
            onTap: () => _toggleCustom(q.id, q.multi),
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 180),
              padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 10),
              decoration: BoxDecoration(
                color: customOn ? p.accentSoft.withValues(alpha: 0.6) : Colors.transparent,
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: customOn ? p.accent : p.line),
              ),
              child: Row(
                children: [
                  AppIcon(
                    customOn ? AppIcons.edit : AppIcons.plus,
                    size: 17,
                    color: customOn ? p.accent : p.muted,
                  ),
                  const SizedBox(width: 9),
                  Text(
                    '其他（自己补充说明…）',
                    style: TextStyle(
                      fontSize: 14,
                      color: customOn ? p.text : p.muted,
                      fontWeight: customOn ? FontWeight.w600 : FontWeight.w400,
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (customOn) ...[
            const SizedBox(height: 8),
            TextField(
              controller: _ctrlFor(q.id),
              style: TextStyle(fontSize: 14.5, color: p.text),
              minLines: 1,
              maxLines: 3,
              decoration: InputDecoration(
                hintText: '写下你的想法…',
                contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
                fillColor: p.bg,
              ),
            ),
          ],
          const SizedBox(height: 13),
          Row(
            children: [
              Pressable(
                onTap: () => _dismiss(questions),
                child: Container(
                  height: 46,
                  padding: const EdgeInsets.symmetric(horizontal: 14),
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: p.surfaceAlt,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Text(
                    '暂不选择',
                    style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: p.muted),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              if (questions.length > 1 && curIdx > 0) ...[
                OutlinedButton(
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size(68, 44),
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                  ),
                  onPressed: () => setState(() => _step = curIdx - 1),
                  child: const Text('上一题'),
                ),
                const SizedBox(width: 6),
              ],
              if (questions.length > 1 && curIdx < questions.length - 1 && answeredCount < questions.length)
                Expanded(
                  child: OutlinedButton(
                    style: OutlinedButton.styleFrom(minimumSize: const Size(88, 44)),
                    onPressed: () => setState(() => _step = curIdx + 1),
                    child: const Text('下一题'),
                  ),
                )
              else
                Expanded(
                  child: FilledButton(
                    style: FilledButton.styleFrom(
                      minimumSize: const Size(112, 46),
                      backgroundColor: p.accent,
                      foregroundColor: Colors.white,
                    ),
                    onPressed: canSubmit ? () => _submit(questions) : null,
                    child: Text(
                      questions.length > 1
                          ? '确认发送（$answeredCount/${questions.length}）'
                          : '确认并发送',
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

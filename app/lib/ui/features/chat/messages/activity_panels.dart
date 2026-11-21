import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../theme/icons.dart';
import '../../../widgets/mascot/mascot_anim.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

/// Shown between sending and the first token (or during fast read-only tool calls).
class ThinkingIndicator extends StatefulWidget {
  const ThinkingIndicator({super.key, this.label = '', this.since, this.compact = false});
  final String label;
  final DateTime? since;
  final bool compact;
  @override
  State<ThinkingIndicator> createState() => _ThinkingIndicatorState();
}

class _ThinkingIndicatorState extends State<ThinkingIndicator> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1400))..repeat();
  final DateTime _created = DateTime.now();
  Timer? _tick;

  @override
  void initState() {
    super.initState();
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _tick?.cancel();
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final secs = DateTime.now().difference(widget.since ?? _created).inSeconds.clamp(0, 9999);
    final baseLabel = widget.label;
    final fullLabel = baseLabel.isNotEmpty && secs >= 5 ? '$baseLabel（${secs}s）' : baseLabel;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (!widget.compact) ...[
            const AnimatedMascot(size: 34, motion: MascotMotion.thinking),
            const SizedBox(width: 8),
          ],
          AnimatedBuilder(
            animation: _c,
            builder: (_, _) => Row(
              children: [
                for (var i = 0; i < 3; i++)
                  Container(
                    margin: const EdgeInsets.symmetric(horizontal: 3),
                    width: widget.compact ? 6 : 8,
                    height: widget.compact ? 6 : 8,
                    decoration: BoxDecoration(
                      color: p.accent.withValues(alpha: 0.25 + 0.75 * _pulse((_c.value - i * 0.18) % 1.0)),
                      shape: BoxShape.circle,
                    ),
                  ),
              ],
            ),
          ),
          if (fullLabel.isNotEmpty) ...[
            const SizedBox(width: 8),
            Flexible(child: Text(fullLabel, style: TextStyle(fontSize: 13.5, color: p.muted))),
          ],
        ],
      ),
    );
  }

  double _pulse(double t) => t < 0.5 ? Curves.easeOut.transform(t * 2) : Curves.easeIn.transform((1 - t) * 2);
}

/// Live task progress (memory lookup, web search, webpage reading) and collapsible
/// web reference list. Expands automatically while tasks are running so the user
/// sees every step in real time, then folds into a compact pill once the reply flows.
class TaskAndSourcesPanel extends StatefulWidget {
  const TaskAndSourcesPanel({
    super.key,
    required this.steps,
    required this.sources,
    required this.isLive,
    required this.waitingForReply,
    this.activity = '',
    this.activitySince,
  });

  final List<RunStep> steps;
  final List<WebSource> sources;
  final bool isLive;
  final bool waitingForReply;
  final String activity;
  final DateTime? activitySince;

  @override
  State<TaskAndSourcesPanel> createState() => _TaskAndSourcesPanelState();
}

class _TaskAndSourcesPanelState extends State<TaskAndSourcesPanel> {
  bool? _userOpen;
  Timer? _tick;

  bool get _activelyWorking =>
      widget.isLive && (widget.steps.any((s) => s.running) || widget.waitingForReply);

  @override
  void initState() {
    super.initState();
    _syncTimer();
  }

  @override
  void didUpdateWidget(covariant TaskAndSourcesPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    _syncTimer();
  }

  void _syncTimer() {
    if (_activelyWorking && _tick == null) {
      _tick = Timer.periodic(const Duration(seconds: 1), (_) {
        if (mounted) setState(() {});
      });
    } else if (!_activelyWorking && _tick != null) {
      _tick?.cancel();
      _tick = null;
    }
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  static IconData _iconForStep(String kind) => switch (kind) {
        'search' => AppIcons.globe,
        'fetch' => AppIcons.article,
        'history' => AppIcons.history,
        'memory' => AppIcons.manageSearch,
        'schedule' => AppIcons.calendar,
        'component' => AppIcons.cards,
        'ask_user' => AppIcons.question,
        'skill' => AppIcons.sparkles,
        _ => AppIcons.sparkles,
      };

  static String _sitePreview(List<WebSource> sources) {
    final names = <String>[];
    for (final s in sources) {
      final n = s.siteName.trim().isNotEmpty ? s.siteName.trim() : (Uri.tryParse(s.url)?.host ?? '');
      if (n.isNotEmpty && !names.contains(n)) {
        names.add(n);
        if (names.length >= 2) break;
      }
    }
    if (names.isEmpty) return '';
    return ' · ${names.join('、')}${sources.length > names.length ? '等' : ''}';
  }

  String _headerLabel(RunStep? running) {
    if (running != null) {
      final secs = running.startedAt != null
          ? DateTime.now().difference(running.startedAt!).inSeconds.clamp(0, 999)
          : 0;
      return secs >= 2 ? '正在${running.title}（${secs}s）' : '正在${running.title}…';
    }
    if (widget.waitingForReply) {
      final act = widget.activity.isNotEmpty ? widget.activity : '正在整理检索结果…';
      final secs = widget.activitySince != null
          ? DateTime.now().difference(widget.activitySince!).inSeconds.clamp(0, 999)
          : 0;
      return secs >= 3 ? '$act（${secs}s）' : act;
    }
    final hadMemory = widget.steps.any((s) => s.kind == 'memory');
    final hadHistory = widget.steps.any((s) => s.kind == 'history');
    final lastSchedule = widget.steps.where((s) => s.kind == 'schedule').lastOrNull;
    final lastAskUser = widget.steps.where((s) => s.kind == 'ask_user').lastOrNull;
    final lastComp = widget.steps.where((s) => s.kind == 'component').lastOrNull;
    final lastSkill = widget.steps.where((s) => s.kind == 'skill').lastOrNull;
    if (widget.sources.isNotEmpty) {
      final prefix = hadHistory && hadMemory
          ? '已翻看记录与记忆 · '
          : (hadHistory ? '已翻看前面的聊天记录 · ' : (hadMemory ? '已翻看记忆 · ' : ''));
      return '$prefix已参考 ${widget.sources.length} 篇网页${_sitePreview(widget.sources)}';
    }
    if (lastSchedule != null) {
      return lastSchedule.title.isNotEmpty ? lastSchedule.title : '已处理日程安排';
    }
    if (lastAskUser != null) {
      return lastAskUser.title.isNotEmpty ? lastAskUser.title : '等待确认偏好';
    }
    if (lastComp != null) {
      return lastComp.title.isNotEmpty ? lastComp.title : '已生成交互卡片';
    }
    final hasSearch = widget.steps.any((s) => s.kind == 'search' || s.kind == 'fetch');
    if (hasSearch) return '已完成联网检索';
    if (lastSkill != null) {
      return lastSkill.title.isNotEmpty ? lastSkill.title : '已加载扩展技能';
    }
    if (hadHistory && hadMemory) return '已翻看前面的聊天与记忆';
    if (hadHistory) return '已翻看前面的聊天记录';
    return '已翻看相关记忆';
  }

  Future<void> _openSource(String rawUrl) async {
    final uri = Uri.tryParse(rawUrl.trim());
    if (uri == null) return;
    final ok = await launchUrl(uri, mode: LaunchMode.externalApplication);
    if (!ok && mounted) showToast(context, '打不开网页链接');
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final running = widget.steps.where((s) => s.running).lastOrNull;
    final busy = running != null || widget.waitingForReply;
    final open = _userOpen ?? busy;
    final hasWeb = widget.sources.isNotEmpty || widget.steps.any((s) => s.kind == 'search' || s.kind == 'fetch');
    final hasSchedule = widget.steps.any((s) => s.kind == 'schedule');
    final hasAskUser = widget.steps.any((s) => s.kind == 'ask_user');
    final hasComponent = widget.steps.any((s) => s.kind == 'component');
    final hasSkill = widget.steps.any((s) => s.kind == 'skill');
    final onlyHistory = !hasWeb && !hasSchedule && !hasAskUser && !hasComponent && !hasSkill && widget.steps.isNotEmpty && widget.steps.every((s) => s.kind == 'history');
    final headerIcon = hasWeb
        ? AppIcons.globe
        : hasSchedule
            ? AppIcons.calendar
            : hasAskUser
                ? AppIcons.question
                : hasComponent
                    ? AppIcons.cards
                    : hasSkill
                        ? AppIcons.sparkles
                        : (onlyHistory ? AppIcons.history : AppIcons.manageSearch);

    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Pressable(
            onTap: () => setState(() => _userOpen = !open),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
              decoration: BoxDecoration(
                color: busy ? Color.lerp(p.surfaceAlt, p.accentSoft, 0.38) : p.surfaceAlt,
                borderRadius: BorderRadius.circular(20),
                border: busy ? Border.all(color: p.accent.withValues(alpha: 0.25)) : null,
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (busy)
                    SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(strokeWidth: 1.9, color: p.accent),
                    )
                  else
                    AppIcon(
                      headerIcon,
                      size: 16,
                      color: p.accent,
                    ),
                  const SizedBox(width: 7),
                  Flexible(
                    child: Text(
                      _headerLabel(running),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13.5,
                        color: busy ? p.text : p.muted,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ),
                  const SizedBox(width: 3),
                  AnimatedRotation(
                    turns: open ? 0.5 : 0,
                    duration: const Duration(milliseconds: 200),
                    child: AppIcon(AppIcons.chevronDown, size: 15, color: p.muted),
                  ),
                ],
              ),
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 240),
            curve: Curves.easeOutCubic,
            alignment: Alignment.topCenter,
            child: open
                ? Container(
                    margin: const EdgeInsets.only(top: 8),
                    padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
                    decoration: BoxDecoration(
                      color: p.surface,
                      borderRadius: BorderRadius.circular(16),
                      border: Border.all(color: p.line),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        for (var i = 0; i < widget.steps.length; i++) ...[
                          if (i > 0) const SizedBox(height: 7),
                          _StepRow(step: widget.steps[i], icon: _iconForStep(widget.steps[i].kind)),
                        ],
                        if (widget.waitingForReply && running == null) ...[
                          if (widget.steps.isNotEmpty) const SizedBox(height: 7),
                          Row(children: [
                            SizedBox(
                              width: 14,
                              height: 14,
                              child: CircularProgressIndicator(strokeWidth: 1.8, color: p.accent),
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                widget.activity.isNotEmpty ? widget.activity : '正在结合检索内容整理回答…',
                                style: TextStyle(fontSize: 13.5, color: p.text, fontWeight: FontWeight.w500),
                              ),
                            ),
                          ]),
                        ],
                        if (widget.sources.isNotEmpty) ...[
                          if (widget.steps.isNotEmpty) ...[
                            const SizedBox(height: 9),
                            Divider(height: 1, thickness: 0.7, color: p.line.withValues(alpha: 0.8)),
                            const SizedBox(height: 8),
                          ],
                          for (var i = 0; i < widget.sources.length; i++) ...[
                            if (i > 0) const SizedBox(height: 6),
                            _SourceTile(index: i + 1, source: widget.sources[i], onTap: () => _openSource(widget.sources[i].url)),
                          ],
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

class _StepRow extends StatelessWidget {
  const _StepRow({required this.step, required this.icon});
  final RunStep step;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: step.running
              ? SizedBox(width: 15, height: 15, child: CircularProgressIndicator(strokeWidth: 1.8, color: p.accent))
              : AppIcon(
                  step.isError ? AppIcons.info : icon,
                  size: 16,
                  color: step.isError ? p.bad : p.accent,
                ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Wrap(
            spacing: 6,
            runSpacing: 2,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text(
                step.title,
                style: TextStyle(
                  fontSize: 13.5,
                  color: step.running ? p.text : p.muted,
                  fontWeight: step.running ? FontWeight.w500 : FontWeight.w400,
                ),
              ),
              if (step.detail.isNotEmpty)
                Text(
                  '· ${step.detail}',
                  style: TextStyle(fontSize: 12.5, color: step.isError ? p.bad : p.faint),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _SourceTile extends StatelessWidget {
  const _SourceTile({required this.index, required this.source, required this.onTap});
  final int index;
  final WebSource source;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final site = source.siteName.trim().isNotEmpty ? source.siteName.trim() : (Uri.tryParse(source.url)?.host ?? '网页');
    final meta = source.date.trim().isNotEmpty ? '$site · ${source.date.trim()}' : site;
    return Pressable(
      scale: 0.985,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: p.surfaceAlt.withValues(alpha: 0.7),
          borderRadius: BorderRadius.circular(11),
        ),
        child: Row(
          children: [
            Container(
              width: 20,
              height: 20,
              alignment: Alignment.center,
              decoration: BoxDecoration(
                color: p.surface,
                borderRadius: BorderRadius.circular(6),
                border: Border.all(color: p.line),
              ),
              child: Text(
                '$index',
                style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: p.muted),
              ),
            ),
            const SizedBox(width: 9),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    source.title.isNotEmpty ? source.title : site,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: p.text),
                  ),
                  const SizedBox(height: 1.5),
                  Text(
                    meta,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12, color: p.muted),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 6),
            AppIcon(AppIcons.externalLink, size: 15, color: p.faint),
          ],
        ),
      ),
    );
  }
}

/// Collapsible "deep thinking" process. Expanded while reasoning streams,
/// collapses automatically once the answer begins.
class ReasoningPanel extends StatefulWidget {
  const ReasoningPanel({super.key, required this.text, required this.thinking, this.durationMs = 0});
  final String text;
  final bool thinking;
  final int durationMs;

  @override
  State<ReasoningPanel> createState() => _ReasoningPanelState();
}

class _ReasoningPanelState extends State<ReasoningPanel> {
  bool? _userOpen;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final open = _userOpen ?? widget.thinking;
    final label = widget.thinking ? '正在深度思考…' : '已深度思考';
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Pressable(
            onTap: () => setState(() => _userOpen = !open),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
              decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(20)),
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                AppIcon(AppIcons.thinkOff, size: 16, color: p.accent),
                const SizedBox(width: 6),
                Text(label, style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w500)),
                const SizedBox(width: 3),
                AnimatedRotation(
                  turns: open ? 0.5 : 0,
                  duration: const Duration(milliseconds: 200),
                  child: AppIcon(AppIcons.chevronDown, size: 15, color: p.muted),
                ),
              ]),
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 260),
            curve: Curves.easeOutCubic,
            alignment: Alignment.topCenter,
            child: open
                ? Container(
                    margin: const EdgeInsets.only(top: 8),
                    padding: const EdgeInsets.only(left: 12),
                    decoration: BoxDecoration(border: Border(left: BorderSide(color: p.line, width: 2))),
                    child: Text(widget.text.trim(), style: TextStyle(fontSize: 14.5, height: 1.6, color: p.muted)),
                  )
                : const SizedBox(width: double.infinity),
          ),
        ],
      ),
    );
  }
}

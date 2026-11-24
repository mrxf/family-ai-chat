import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/desktop_nav.dart';
import '../../../state/pinned_components.dart';
import '../../../state/scheduled_tasks.dart';
import '../../../state/session.dart';
import '../../adaptive/breakpoints.dart';
import '../../desktop/desktop_page_header.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import 'scheduled_task_editor.dart';
import 'scheduled_task_runs_sheet.dart';

/// Cross-platform management page for background AI scheduled automation tasks.
class ScheduledTasksPage extends ConsumerStatefulWidget {
  const ScheduledTasksPage({super.key, this.embedded = false});

  /// True when hosted inside the desktop workspace pane.
  final bool embedded;

  @override
  ConsumerState<ScheduledTasksPage> createState() => _ScheduledTasksPageState();
}

class _ScheduledTasksPageState extends ConsumerState<ScheduledTasksPage> {
  bool _busy = false;
  Timer? _pollTimer;

  @override
  void initState() {
    super.initState();
    Future.microtask(() => ref.invalidate(scheduledTasksProvider));
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    super.dispose();
  }

  void _syncPolling(bool hasRunning) {
    if (hasRunning && _pollTimer == null) {
      _pollTimer = Timer.periodic(const Duration(seconds: 4), (_) {
        if (!mounted) return;
        ref.invalidate(scheduledTasksProvider);
        unawaited(ref.read(pinnedComponentsProvider.notifier).refresh());
      });
    } else if (!hasRunning && _pollTimer != null) {
      _pollTimer?.cancel();
      _pollTimer = null;
    }
  }

  Future<void> _refresh() async {
    try {
      ref.invalidate(scheduledTasksProvider);
      await ref.read(scheduledTasksProvider.future);
      unawaited(ref.read(pinnedComponentsProvider.notifier).refresh());
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    }
  }

  Future<void> _mutate(Future<void> Function() fn, {String? toast}) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await fn();
      if (!mounted) return;
      if (toast != null) showToast(context, toast);
      await _refresh();
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _openEditor([ScheduledTask? existing]) async {
    await showScheduledTaskEditor(
      context,
      title: existing == null ? '新建 AI 定时任务' : '编辑 AI 定时任务',
      existing: existing,
      onSave: (draft) async {
        final api = ref.read(apiProvider);
        if (existing == null) {
          await api.createScheduledTask(draft);
        } else {
          await api.updateScheduledTask(existing.id, draft.toJson());
        }
        if (mounted) await _refresh();
      },
    );
  }

  Future<void> _runNow(ScheduledTask task) async {
    await _mutate(
      () => ref.read(apiProvider).runScheduledTaskNow(task.id),
      toast: '已开始在后台执行「${task.title}」',
    );
  }

  Future<void> _toggleEnabled(ScheduledTask task, bool enabled) async {
    await _mutate(
      () => ref.read(apiProvider).updateScheduledTask(task.id, {'enabled': enabled}),
      toast: enabled ? '已启用定时任务' : '已暂停定时任务',
    );
  }

  Future<void> _deleteTask(ScheduledTask task) async {
    final ok = await confirm(
      context,
      title: '删除这个定时任务？',
      body: '删除后，「${task.title}」将不再自动运行（已更新到首页的卡片内容会保留）。',
      ok: '删除',
      danger: true,
    );
    if (!ok || !mounted) return;
    await _mutate(() => ref.read(apiProvider).deleteScheduledTask(task.id), toast: '已删除定时任务');
  }

  void _openArchiveConv(ScheduledTask task) {
    final cid = task.conversationId;
    if (cid == null || cid.isEmpty) return;
    if (!widget.embedded) Navigator.of(context).popUntil((r) => r.isFirst);
    if (context.windowSize.isExpanded) {
      ref.read(desktopNavProvider.notifier).showChat();
    }
    ref.read(chatProvider.notifier).open(cid);
  }

  void _showTaskMenu(ScheduledTask task) {
    showActionSheet(
      context,
      title: task.title,
      actions: [
        SheetAction(AppIcons.play, '立即运行一次', () => _runNow(task)),
        SheetAction(AppIcons.history, '查看执行记录', () => showScheduledTaskRunsSheet(context, task)),
        if (task.conversationId != null && task.conversationId!.isNotEmpty)
          SheetAction(AppIcons.arrowUpRight, '打开任务执行对话', () => _openArchiveConv(task)),
        SheetAction(AppIcons.edit, '编辑任务', () => _openEditor(task)),
        SheetAction(
          AppIcons.delete,
          '删除任务',
          () => _deleteTask(task),
          danger: true,
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final asyncData = ref.watch(scheduledTasksProvider);
    final data = asyncData.value ?? const ScheduledTasksResult();
    final pins = ref.watch(pinnedComponentsProvider).items;
    final hasRunning = data.items.any((t) => t.isRunning);
    _syncPolling(hasRunning);

    final pinTitleMap = <String, String>{
      for (final pin in pins) ...{
        if (pin.effectiveEntityId.isNotEmpty) 'ent:${pin.effectiveEntityId}': describeComponent(pin.component).title,
        '${pin.messageId}|${pin.componentId}': describeComponent(pin.component).title,
      },
    };

    final embedded = widget.embedded;
    final subtitle = data.totalCount == 0
        ? '让小暖按时联网查新闻、分析任务并原地更新首页卡片'
        : '已启用 ${data.activeCount} 个 · 共 ${data.totalCount} 个定时任务';

    final content = RefreshIndicator(
      onRefresh: _refresh,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: embedded ? const EdgeInsets.fromLTRB(32, 20, 32, 36) : const EdgeInsets.fromLTRB(16, 8, 16, 28),
        children: [
          DesktopPageBody(
            maxWidth: embedded ? 860 : double.infinity,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: p.surface,
                    borderRadius: BorderRadius.circular(20),
                    border: Border.all(color: p.line),
                  ),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Container(
                        width: 40,
                        height: 40,
                        decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(12)),
                        child: Center(child: AppIcon(AppIcons.repeat, color: p.accent, size: 22)),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Text(
                          '在聊天里直接说「每天早上 7 点帮我查最新大模型新闻，更新前三条，把原来的前三条放后面三条」，或点按钮手动配置，小暖就会在后台按时自动完成并刷新首页卡片。',
                          style: TextStyle(fontSize: 14.5, height: 1.48, color: p.muted),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 14),
                if (asyncData.isLoading && data.items.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 60),
                    child: Center(child: CircularProgressIndicator()),
                  )
                else if (data.items.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 56, horizontal: 20),
                    child: Column(
                      children: [
                        AppIcon(AppIcons.repeat, size: 46, color: p.faint),
                        const SizedBox(height: 14),
                        Text('还没有配置 AI 定时任务', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: p.text)),
                        const SizedBox(height: 6),
                        Text(
                          embedded ? '点右上角「新建任务」，或在聊天里直接告诉小暖你的定时需求' : '点下方按钮新建，或在聊天里直接告诉小暖你的定时需求',
                          textAlign: TextAlign.center,
                          style: TextStyle(fontSize: 14.5, color: p.muted, height: 1.5),
                        ),
                      ],
                    ),
                  )
                else
                  for (final task in data.items)
                    _TaskCard(
                      task: task,
                      boundCardTitle: (task.targetEntityId.isNotEmpty ? pinTitleMap['ent:${task.targetEntityId}'] : null) ??
                          pinTitleMap['${task.targetMessageId}|${task.targetComponentId}'],
                      showQuickActions: embedded,
                      busy: _busy,
                      onToggle: (v) => _toggleEnabled(task, v),
                      onRunNow: () => _runNow(task),
                      onRuns: () => showScheduledTaskRunsSheet(context, task),
                      onEdit: () => _openEditor(task),
                      onDelete: () => _deleteTask(task),
                      onMenu: () => _showTaskMenu(task),
                    ),
              ],
            ),
          ),
        ],
      ),
    );

    if (embedded) {
      return Scaffold(
        body: Column(
          children: [
            DesktopPageHeader(
              title: 'AI 定时任务',
              subtitle: subtitle,
              actions: [
                IconButton(
                  tooltip: '刷新',
                  onPressed: _busy ? null : _refresh,
                  icon: const AppIcon(AppIcons.refresh),
                ),
                FilledButton.icon(
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                  ),
                  onPressed: _busy ? null : () => _openEditor(),
                  icon: const AppIcon(AppIcons.plus, size: 17),
                  label: const Text('新建任务', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
                ),
              ],
            ),
            Expanded(child: content),
          ],
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('AI 定时任务'),
        actions: [
          IconButton(
            tooltip: '刷新',
            onPressed: _busy ? null : _refresh,
            icon: const AppIcon(AppIcons.refresh),
          ),
        ],
      ),
      bottomNavigationBar: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
          child: FilledButton.icon(
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(52)),
            onPressed: _busy ? null : () => _openEditor(),
            icon: const AppIcon(AppIcons.plus, size: 20),
            label: const Text('新建 AI 定时任务'),
          ),
        ),
      ),
      body: content,
    );
  }
}

class _TaskCard extends StatelessWidget {
  const _TaskCard({
    required this.task,
    required this.boundCardTitle,
    required this.showQuickActions,
    required this.busy,
    required this.onToggle,
    required this.onRunNow,
    required this.onRuns,
    required this.onEdit,
    required this.onDelete,
    required this.onMenu,
  });

  final ScheduledTask task;
  final String? boundCardTitle;
  final bool showQuickActions;
  final bool busy;
  final ValueChanged<bool> onToggle;
  final VoidCallback onRunNow;
  final VoidCallback onRuns;
  final VoidCallback onEdit;
  final VoidCallback onDelete;
  final VoidCallback onMenu;

  static String _fmtNext(int ms) {
    if (ms <= 0) return '';
    final d = DateTime.fromMillisecondsSinceEpoch(ms);
    return '${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')} '
        '${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final nextStr = _fmtNext(task.nextRunAt);
    final statusColor = switch (task.lastStatus) {
      'success' => p.good,
      'running' => p.accent,
      'error' => p.bad,
      _ => p.muted,
    };
    final statusText = switch (task.lastStatus) {
      'success' => '上次成功',
      'running' => '执行中…',
      'error' => '上次失败',
      _ => '尚未执行',
    };

    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: GestureDetector(
        onSecondaryTap: onMenu,
        onLongPress: onMenu,
        child: Material(
          color: p.surface,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(20),
            side: BorderSide(color: task.isRunning ? p.accent.withValues(alpha: 0.45) : p.line),
          ),
          clipBehavior: Clip.antiAlias,
          child: InkWell(
            onTap: onRuns,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 14, 14, 14),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        decoration: BoxDecoration(
                          color: task.enabled ? p.accentSoft : p.surfaceAlt,
                          borderRadius: BorderRadius.circular(9),
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            AppIcon(AppIcons.clock, size: 13, color: task.enabled ? p.accent : p.muted),
                            const SizedBox(width: 4),
                            Text(
                              task.scheduleLabel,
                              style: TextStyle(
                                fontSize: 12.5,
                                fontWeight: FontWeight.w600,
                                color: task.enabled ? p.accent : p.muted,
                              ),
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(width: 8),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2.5),
                        decoration: BoxDecoration(
                          color: statusColor.withValues(alpha: 0.12),
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Text(
                          statusText,
                          style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: statusColor),
                        ),
                      ),
                      const Spacer(),
                      Switch.adaptive(
                        value: task.enabled,
                        onChanged: busy ? null : onToggle,
                      ),
                    ],
                  ),
                  const SizedBox(height: 6),
                  Text(
                    task.title,
                    style: TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                      color: task.enabled ? p.text : p.muted,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    task.prompt,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 14, height: 1.42, color: p.muted),
                  ),
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 10,
                    runSpacing: 6,
                    children: [
                      if (nextStr.isNotEmpty && task.enabled)
                        _InfoTag(icon: AppIcons.calendar, text: '下次：$nextStr'),
                      if (task.hasTargetComponent)
                        _InfoTag(
                          icon: AppIcons.cards,
                          text: boundCardTitle != null ? '更新卡片：$boundCardTitle' : '已绑定首页卡片',
                          accent: true,
                        ),
                      if (task.runCount > 0)
                        _InfoTag(icon: AppIcons.history, text: '已执行 ${task.runCount} 次'),
                    ],
                  ),
                  if (task.lastSummary.isNotEmpty || task.lastError.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 7),
                      decoration: BoxDecoration(
                        color: task.lastStatus == 'error' ? p.bad.withValues(alpha: 0.08) : p.surfaceAlt.withValues(alpha: 0.75),
                        borderRadius: BorderRadius.circular(11),
                      ),
                      child: Text(
                        task.lastStatus == 'error' ? task.lastError : task.lastSummary,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 12.5,
                          height: 1.38,
                          color: task.lastStatus == 'error' ? p.bad : p.muted,
                        ),
                      ),
                    ),
                  ],
                  const SizedBox(height: 10),
                  Row(
                    children: [
                      OutlinedButton.icon(
                        style: OutlinedButton.styleFrom(
                          visualDensity: VisualDensity.compact,
                          padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 6),
                        ),
                        onPressed: busy || task.isRunning ? null : onRunNow,
                        icon: task.isRunning
                            ? const SizedBox(width: 13, height: 13, child: CircularProgressIndicator(strokeWidth: 2))
                            : const AppIcon(AppIcons.play, size: 14),
                        label: Text(task.isRunning ? '执行中…' : '立即运行', style: const TextStyle(fontSize: 13)),
                      ),
                      const SizedBox(width: 8),
                      TextButton.icon(
                        style: TextButton.styleFrom(visualDensity: VisualDensity.compact),
                        onPressed: onRuns,
                        icon: const AppIcon(AppIcons.history, size: 14),
                        label: const Text('执行记录', style: TextStyle(fontSize: 13)),
                      ),
                      const Spacer(),
                      if (showQuickActions) ...[
                        IconButton(
                          tooltip: '编辑任务',
                          visualDensity: VisualDensity.compact,
                          onPressed: onEdit,
                          icon: AppIcon(AppIcons.edit, size: 18, color: p.muted),
                        ),
                        IconButton(
                          tooltip: '删除任务',
                          visualDensity: VisualDensity.compact,
                          onPressed: onDelete,
                          icon: AppIcon(AppIcons.delete, size: 18, color: p.muted),
                        ),
                      ] else
                        IconButton(
                          tooltip: '更多操作',
                          visualDensity: VisualDensity.compact,
                          onPressed: onMenu,
                          icon: AppIcon(AppIcons.more, size: 19, color: p.muted),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _InfoTag extends StatelessWidget {
  const _InfoTag({required this.icon, required this.text, this.accent = false});
  final IconData icon;
  final String text;
  final bool accent;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final color = accent ? p.accent : p.muted;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        AppIcon(icon, size: 13.5, color: color),
        const SizedBox(width: 4),
        Text(text, style: TextStyle(fontSize: 12.5, color: color, fontWeight: accent ? FontWeight.w600 : FontWeight.w400)),
      ],
    );
  }
}

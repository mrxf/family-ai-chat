import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/desktop_nav.dart';
import '../../../state/session.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../adaptive/breakpoints.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';

/// Displays the recent execution history of a [ScheduledTask] in an adaptive modal.
Future<void> showScheduledTaskRunsSheet(BuildContext context, ScheduledTask task) =>
    showAdaptiveModal<void>(
      context: context,
      maxWidth: 560,
      builder: (_) => _ScheduledTaskRunsBody(task: task),
    );

class _ScheduledTaskRunsBody extends ConsumerStatefulWidget {
  const _ScheduledTaskRunsBody({required this.task});
  final ScheduledTask task;

  @override
  ConsumerState<_ScheduledTaskRunsBody> createState() => _ScheduledTaskRunsBodyState();
}

class _ScheduledTaskRunsBodyState extends ConsumerState<_ScheduledTaskRunsBody> {
  List<ScheduledTaskRun>? _runs;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final list = await ref.read(apiProvider).scheduledTaskRuns(widget.task.id);
      if (mounted) setState(() { _runs = list; _error = null; });
    } catch (e) {
      if (mounted) setState(() => _error = ApiError.from(e).message);
    }
  }

  void _openConversation(String convId, String? msgId) {
    if (convId.isEmpty) return;
    Navigator.of(context).popUntil((r) => r.isFirst);
    if (context.windowSize.isExpanded) {
      ref.read(desktopNavProvider.notifier).showChat();
    }
    ref.read(chatProvider.notifier).open(convId, focusMessageId: (msgId != null && msgId.isNotEmpty) ? msgId : null);
  }

  static String _fmtDateTime(int ms) {
    if (ms <= 0) return '-';
    final d = DateTime.fromMillisecondsSinceEpoch(ms);
    return '${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')} '
        '${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
  }

  static String _fmtDuration(int startMs, int endMs) {
    if (startMs <= 0 || endMs <= startMs) return '';
    final sec = ((endMs - startMs) / 1000).round();
    if (sec < 60) return '${sec}s';
    return '${sec ~/ 60}m ${sec % 60}s';
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final runs = _runs;

    return SafeArea(
      top: false,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxHeight: 540),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 18),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('执行记录 · ${widget.task.title}', maxLines: 1, overflow: TextOverflow.ellipsis, style: Theme.of(context).textTheme.titleMedium),
                        Text(widget.task.scheduleLabel, style: TextStyle(fontSize: 12.5, color: p.muted)),
                      ],
                    ),
                  ),
                  IconButton(
                    tooltip: '刷新',
                    onPressed: _load,
                    icon: const AppIcon(AppIcons.refresh, size: 19),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 28),
                  child: Center(child: Text(_error!, style: TextStyle(color: p.bad))),
                )
              else if (runs == null)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 36),
                  child: Center(child: CircularProgressIndicator()),
                )
              else if (runs.isEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 36),
                  child: Center(
                    child: Text('还没有执行记录，可点击「立即运行」试跑一次', style: TextStyle(color: p.muted)),
                  ),
                )
              else
                Flexible(
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: runs.length,
                    separatorBuilder: (_, _) => const SizedBox(height: 8),
                    itemBuilder: (_, i) {
                      final r = runs[i];
                      final isOk = r.status == 'success';
                      final isRun = r.status == 'running';
                      final statusColor = isOk ? p.good : (isRun ? p.accent : p.bad);
                      final statusLabel = isOk ? '成功' : (isRun ? '执行中' : '失败');
                      final dur = _fmtDuration(r.startedAt, r.finishedAt);

                      return Container(
                        padding: const EdgeInsets.all(13),
                        decoration: BoxDecoration(
                          color: p.surfaceAlt.withValues(alpha: 0.7),
                          borderRadius: BorderRadius.circular(14),
                          border: Border.all(color: p.line),
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: statusColor.withValues(alpha: 0.14),
                                    borderRadius: BorderRadius.circular(8),
                                  ),
                                  child: Text(statusLabel, style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w700, color: statusColor)),
                                ),
                                const SizedBox(width: 8),
                                Text(_fmtDateTime(r.startedAt), style: TextStyle(fontSize: 12.5, color: p.muted)),
                                if (r.triggerSource == 'manual') ...[
                                  const SizedBox(width: 6),
                                  Text('· 手动', style: TextStyle(fontSize: 12, color: p.faint)),
                                ],
                                if (dur.isNotEmpty) ...[
                                  const SizedBox(width: 6),
                                  Text('· $dur', style: TextStyle(fontSize: 12, color: p.faint)),
                                ],
                                const Spacer(),
                                if (r.updatedComponents > 0)
                                  Text('更新 ${r.updatedComponents} 卡片', style: TextStyle(fontSize: 11.5, color: p.accent, fontWeight: FontWeight.w600)),
                              ],
                            ),
                            if (r.summary.isNotEmpty) ...[
                              const SizedBox(height: 7),
                              Text(r.summary, style: TextStyle(fontSize: 13.5, height: 1.42, color: p.text)),
                            ],
                            if (r.error.isNotEmpty) ...[
                              const SizedBox(height: 6),
                              Text(r.error, style: TextStyle(fontSize: 13, height: 1.4, color: p.bad)),
                            ],
                            if (r.conversationId.isNotEmpty) ...[
                              const SizedBox(height: 6),
                              Align(
                                alignment: Alignment.centerRight,
                                child: TextButton.icon(
                                  style: TextButton.styleFrom(visualDensity: VisualDensity.compact),
                                  onPressed: () => _openConversation(r.conversationId, r.messageId),
                                  icon: const AppIcon(AppIcons.arrowUpRight, size: 14),
                                  label: Text(isRun ? '查看实时执行过程' : '查看完整执行对话', style: const TextStyle(fontSize: 12.5)),
                                ),
                              ),
                            ],
                          ],
                        ),
                      );
                    },
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

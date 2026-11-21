import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../platform/platform_capabilities.dart';
import '../../../platform/reminders/reminder_service.dart';
import '../../../state/schedules.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import 'schedule_editor.dart';
import '../../widgets/sheets.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

class SchedulesPage extends ConsumerStatefulWidget {
  const SchedulesPage({super.key});

  @override
  ConsumerState<SchedulesPage> createState() => _SchedulesPageState();
}

class _SchedulesPageState extends ConsumerState<SchedulesPage> with WidgetsBindingObserver {
  bool _busy = false;
  bool _showCompleted = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    Future.microtask(() {
      ref.invalidate(schedulesProvider);
      ref.invalidate(notificationPermissionProvider);
    });
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && mounted) {
      ref.invalidate(notificationPermissionProvider);
      ref.invalidate(schedulesProvider);
    }
  }

  Future<void> _refresh() async {
    try {
      ref.invalidate(schedulesProvider);
      ref.invalidate(notificationPermissionProvider);
      await ref.read(schedulesProvider.future);
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

  Future<void> _enableNotifications() async {
    final reminders = ref.read(reminderServiceProvider);
    final granted = await reminders.requestPermission();
    if (!granted && mounted) {
      await reminders.openSettings();
    }
    if (mounted) {
      ref.invalidate(notificationPermissionProvider);
      await _refresh();
    }
  }

  Future<void> _openEditor([Schedule? existing]) async {
    await showScheduleEditor(
      context,
      title: existing == null ? '添加日程提醒' : '修改日程提醒',
      existing: existing,
      onSave: (draft) async {
        final api = ref.read(apiProvider);
        if (existing == null) {
          await api.createSchedule(draft);
        } else {
          await api.updateSchedule(existing.id, draft.toJson());
        }
        if (mounted && ref.read(platformCapabilitiesProvider).localReminders) {
          final reminders = ref.read(reminderServiceProvider);
          if (!await reminders.hasPermission() && draft.remindBeforeMin >= 0) {
            await reminders.requestPermission();
          }
        }
        if (mounted) {
          await _refresh();
        }
      },
    );
  }

  void _showItemMenu(Schedule item, {String? occurrenceDate, bool? occurrenceCompleted}) {
    final isDone = occurrenceCompleted ?? item.isCompleted;
    showActionSheet(
      context,
      title: item.title,
      actions: [
        SheetAction(
          isDone ? AppIcons.circle : AppIcons.checkCircle,
          isDone ? '重新标为待办' : (item.isRecurring ? '标记此次已完成' : '标记已完成'),
          () => _mutate(
            () => ref.read(apiProvider).toggleScheduleComplete(
                  item.id,
                  completed: !isDone,
                  occurrenceDate: occurrenceDate,
                ),
            toast: !isDone ? '已标记完成' : '已恢复为待办',
          ),
        ),
        SheetAction(AppIcons.edit, '修改日程', () => _openEditor(item)),
        SheetAction(
          AppIcons.delete,
          item.isRecurring ? '删除整个周期提醒' : '删除这条日程',
          () async {
            final ok = await confirm(
              context,
              title: item.isRecurring ? '删除周期提醒？' : '删除这条日程？',
              body: item.isRecurring
                  ? '删除后，「${item.title}」以后的周期提醒也会全部停止。'
                  : '删除后无法恢复，手机上的对应提醒也会取消。',
              ok: '删除',
              danger: true,
            );
            if (!ok || !mounted) return;
            await _mutate(() => ref.read(apiProvider).deleteSchedule(item.id), toast: '已删除日程');
          },
          danger: true,
        ),
      ],
    );
  }

  void _showPageMenu(int completedCount) {
    showActionSheet(
      context,
      actions: [
        if (completedCount > 0)
          SheetAction(
            AppIcons.clearAll,
            '清理已完成的单次日程（$completedCount）',
            () async {
              final ok = await confirm(
                context,
                title: '清理已完成日程？',
                body: '已打勾完成的单次日程会被清理，未完成与周期提醒不受影响。',
                ok: '清理',
                danger: true,
              );
              if (!ok || !mounted) return;
              await _mutate(() => ref.read(apiProvider).clearCompletedSchedules(), toast: '已清理完成的日程');
            },
            danger: true,
          ),
        if (ref.read(platformCapabilitiesProvider).localReminders)
          SheetAction(AppIcons.bell, '检查手机通知设置', () => ref.read(reminderServiceProvider).openSettings()),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final asyncData = ref.watch(schedulesProvider);
    final caps = ref.watch(platformCapabilitiesProvider);
    // Devices without local notifications have nothing to grant.
    final hasPerm = !caps.localReminders || (ref.watch(notificationPermissionProvider).value ?? true);
    final data = asyncData.value ?? const SchedulesResult();

    final completedItems = data.schedules.where((s) => s.isCompleted).toList();
    // Far-future or recurring schedules that don't appear in today/tomorrow/upcoming windows
    final shownIds = <String>{
      for (final o in data.todayOccurrences) o.schedule.id,
      for (final o in data.tomorrowOccurrences) o.schedule.id,
      for (final o in data.upcomingOccurrences) o.schedule.id,
    };
    final laterSchedules = data.schedules
        .where((s) => !s.isCompleted && !shownIds.contains(s.id))
        .toList();

    final isEmpty = data.todayOccurrences.isEmpty &&
        data.tomorrowOccurrences.isEmpty &&
        data.upcomingOccurrences.isEmpty &&
        laterSchedules.isEmpty &&
        completedItems.isEmpty;

    return Scaffold(
      appBar: AppBar(
        title: const Text('我的日程'),
        actions: [
          IconButton(
            tooltip: '更多',
            onPressed: _busy ? null : () => _showPageMenu(completedItems.length),
            icon: const AppIcon(AppIcons.more),
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
            label: const Text('手动添加日程'),
          ),
        ),
      ),
      body: RefreshIndicator(
        onRefresh: _refresh,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 28),
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
                    child: Center(child: AppIcon(AppIcons.calendar, color: p.accent, size: 22)),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      caps.localReminders
                          ? '聊天时直接跟小暖说「明天早上八点提醒我吃药」或说出你的安排，小暖就会自动记在这里并在手机上准点提醒你。'
                          : '聊天时直接跟小暖说「明天早上八点提醒我吃药」或说出你的安排，小暖就会自动记在这里，到点由手机上的小暖响铃提醒（电脑端暂不响铃）。',
                      style: TextStyle(fontSize: 15, height: 1.45, color: p.muted),
                    ),
                  ),
                ],
              ),
            ),
            if (!hasPerm) ...[
              const SizedBox(height: 12),
              Container(
                padding: const EdgeInsets.fromLTRB(16, 14, 12, 14),
                decoration: BoxDecoration(
                  color: p.accentSoft.withValues(alpha: 0.65),
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: p.accent.withValues(alpha: 0.3)),
                ),
                child: Row(
                  children: [
                    AppIcon(AppIcons.bellOff, color: p.accent, size: 22),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text(
                        '尚未开启手机通知权限，开启后到点才能响铃提醒',
                        style: TextStyle(fontSize: 15, fontWeight: FontWeight.w500, color: p.text),
                      ),
                    ),
                    TextButton(
                      onPressed: _enableNotifications,
                      child: const Text('去开启', style: TextStyle(fontWeight: FontWeight.w700)),
                    ),
                  ],
                ),
              ),
            ],
            if (asyncData.isLoading && data.schedules.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 60),
                child: Center(child: CircularProgressIndicator()),
              )
            else if (isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 56, horizontal: 20),
                child: Column(
                  children: [
                    AppIcon(AppIcons.calendar, size: 46, color: p.faint),
                    const SizedBox(height: 14),
                    Text(
                      '目前还没有待办日程',
                      style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: p.text),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      '可以点下方按钮手动添加，或者回聊天里直接告诉小暖你的安排',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 15, color: p.muted, height: 1.5),
                    ),
                  ],
                ),
              )
            else ...[
              if (data.todayOccurrences.isNotEmpty) ...[
                _SectionHeader('今天', count: data.todayPending > 0 ? '待办 ${data.todayPending}' : '全部完成'),
                for (final occ in data.todayOccurrences)
                  _OccurrenceCard(
                    occ: occ,
                    showDate: false,
                    onToggle: () => _mutate(
                      () => ref.read(apiProvider).toggleScheduleComplete(
                            occ.schedule.id,
                            completed: !occ.completed,
                            occurrenceDate: occ.occurrenceDate,
                          ),
                    ),
                    onTap: () => _showItemMenu(
                      occ.schedule,
                      occurrenceDate: occ.occurrenceDate,
                      occurrenceCompleted: occ.completed,
                    ),
                  ),
              ],
              if (data.tomorrowOccurrences.isNotEmpty) ...[
                _SectionHeader('明天', count: '${data.tomorrowOccurrences.length} 项'),
                for (final occ in data.tomorrowOccurrences)
                  _OccurrenceCard(
                    occ: occ,
                    showDate: false,
                    onToggle: () => _mutate(
                      () => ref.read(apiProvider).toggleScheduleComplete(
                            occ.schedule.id,
                            completed: !occ.completed,
                            occurrenceDate: occ.occurrenceDate,
                          ),
                    ),
                    onTap: () => _showItemMenu(
                      occ.schedule,
                      occurrenceDate: occ.occurrenceDate,
                      occurrenceCompleted: occ.completed,
                    ),
                  ),
              ],
              if (data.upcomingOccurrences.isNotEmpty) ...[
                _SectionHeader('未来两周', count: '${data.upcomingOccurrences.length} 项'),
                for (final occ in data.upcomingOccurrences)
                  _OccurrenceCard(
                    occ: occ,
                    showDate: true,
                    onToggle: () => _mutate(
                      () => ref.read(apiProvider).toggleScheduleComplete(
                            occ.schedule.id,
                            completed: !occ.completed,
                            occurrenceDate: occ.occurrenceDate,
                          ),
                    ),
                    onTap: () => _showItemMenu(
                      occ.schedule,
                      occurrenceDate: occ.occurrenceDate,
                      occurrenceCompleted: occ.completed,
                    ),
                  ),
              ],
              if (laterSchedules.isNotEmpty) ...[
                _SectionHeader('更远日期的安排', count: '${laterSchedules.length} 项'),
                for (final sc in laterSchedules)
                  _OccurrenceCard(
                    occ: ScheduleOccurrence(
                      schedule: sc,
                      occurrenceDate: _isoDate(sc.startAt),
                      startAt: sc.startAt,
                      endAt: sc.endAt,
                      completed: sc.isCompleted,
                    ),
                    showDate: true,
                    showYear: true,
                    onToggle: () => _mutate(
                      () => ref.read(apiProvider).toggleScheduleComplete(sc.id, completed: !sc.isCompleted),
                    ),
                    onTap: () => _showItemMenu(sc),
                  ),
              ],
              if (completedItems.isNotEmpty) ...[
                const SizedBox(height: 14),
                InkWell(
                  borderRadius: BorderRadius.circular(12),
                  onTap: () => setState(() => _showCompleted = !_showCompleted),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 4),
                    child: Row(
                      children: [
                        Text(
                          '已完成（${completedItems.length}）',
                          style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: p.muted),
                        ),
                        const SizedBox(width: 4),
                        AppIcon(_showCompleted ? AppIcons.chevronUp : AppIcons.chevronDown, size: 16, color: p.muted),
                      ],
                    ),
                  ),
                ),
                if (_showCompleted)
                  for (final sc in completedItems)
                    _OccurrenceCard(
                      occ: ScheduleOccurrence(
                        schedule: sc,
                        occurrenceDate: _isoDate(sc.startAt),
                        startAt: sc.startAt,
                        endAt: sc.endAt,
                        completed: true,
                      ),
                      showDate: true,
                      onToggle: () => _mutate(
                        () => ref.read(apiProvider).toggleScheduleComplete(sc.id, completed: false),
                      ),
                      onTap: () => _showItemMenu(sc),
                    ),
              ],
            ],
          ],
        ),
      ),
    );
  }

  static String _isoDate(int ms) {
    final d = DateTime.fromMillisecondsSinceEpoch(ms);
    return '${d.year}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
  }
}

class _SectionHeader extends StatelessWidget {
  const _SectionHeader(this.title, {this.count});
  final String title;
  final String? count;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 18, 4, 8),
      child: Row(
        children: [
          Text(title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
          if (count != null && count!.isNotEmpty) ...[
            const SizedBox(width: 8),
            Text(count!, style: TextStyle(fontSize: 13.5, color: p.muted, fontWeight: FontWeight.w500)),
          ],
        ],
      ),
    );
  }
}

class _OccurrenceCard extends StatelessWidget {
  const _OccurrenceCard({
    required this.occ,
    required this.showDate,
    this.showYear = false,
    required this.onToggle,
    required this.onTap,
  });

  final ScheduleOccurrence occ;
  final bool showDate;
  final bool showYear;
  final VoidCallback onToggle;
  final VoidCallback onTap;

  static const _wd = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日'];

  String _timeText() {
    final sc = occ.schedule;
    final dt = DateTime.fromMillisecondsSinceEpoch(occ.startAt);
    final hhmm = '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
    final timePart = sc.allDay ? '全天' : hhmm;
    if (!showDate) return timePart;
    final datePart = showYear
        ? '${dt.year}年${dt.month}月${dt.day}日 ${_wd[dt.weekday]}'
        : '${dt.month}月${dt.day}日 ${_wd[dt.weekday]}';
    return '$datePart · $timePart';
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final sc = occ.schedule;
    final done = occ.completed;

    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Material(
        color: p.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(18),
          side: BorderSide(color: p.line),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(6, 12, 14, 12),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                IconButton(
                  constraints: const BoxConstraints(minWidth: 46, minHeight: 46),
                  tooltip: done ? '取消完成' : '标记完成',
                  onPressed: onToggle,
                  icon: AppIcon(
                    done ? AppIcons.checkCircleFill : AppIcons.circle,
                    color: done ? p.accent : p.muted,
                    size: 25,
                  ),
                ),
                const SizedBox(width: 4),
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.only(top: 2),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Flexible(
                              child: Text(
                                _timeText(),
                                style: TextStyle(
                                  fontSize: 14,
                                  fontWeight: FontWeight.w600,
                                  color: done ? p.faint : p.accent,
                                ),
                              ),
                            ),
                            if (sc.isRecurring) ...[
                              const SizedBox(width: 8),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                decoration: BoxDecoration(
                                  color: p.surfaceAlt,
                                  borderRadius: BorderRadius.circular(8),
                                ),
                                child: Text(
                                  sc.recurrenceSummary,
                                  style: TextStyle(fontSize: 12, color: p.muted, fontWeight: FontWeight.w500),
                                ),
                              ),
                            ],
                          ],
                        ),
                        const SizedBox(height: 4),
                        Text(
                          sc.title,
                          style: TextStyle(
                            fontSize: 17.5,
                            fontWeight: FontWeight.w600,
                            color: done ? p.faint : p.text,
                            decoration: done ? TextDecoration.lineThrough : null,
                          ),
                        ),
                        if (sc.location.isNotEmpty || sc.note.isNotEmpty || sc.remindBeforeMin >= 0) ...[
                          const SizedBox(height: 6),
                          Wrap(
                            spacing: 10,
                            runSpacing: 4,
                            children: [
                              if (sc.remindBeforeMin >= 0)
                                _MetaChip(icon: AppIcons.bell, text: sc.remindLabel, faint: done),
                              if (sc.location.isNotEmpty)
                                _MetaChip(icon: AppIcons.mapPin, text: sc.location, faint: done),
                            ],
                          ),
                        ],
                        if (sc.note.isNotEmpty) ...[
                          const SizedBox(height: 5),
                          Text(
                            sc.note,
                            style: TextStyle(fontSize: 14.5, color: done ? p.faint : p.muted, height: 1.4),
                          ),
                        ],
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _MetaChip extends StatelessWidget {
  const _MetaChip({required this.icon, required this.text, this.faint = false});
  final IconData icon;
  final String text;
  final bool faint;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final color = faint ? p.faint : p.muted;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        AppIcon(icon, size: 14, color: color),
        const SizedBox(width: 4),
        Flexible(
          child: Text(
            text,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontSize: 13, color: color),
          ),
        ),
      ],
    );
  }
}

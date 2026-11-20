import 'package:flutter/material.dart';

import '../core/api.dart';
import '../core/models.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

Future<ScheduleDraft?> showScheduleEditor(
  BuildContext context, {
  String title = '添加日程',
  Schedule? existing,
  Future<void> Function(ScheduleDraft)? onSave,
}) =>
    showModalBottomSheet<ScheduleDraft>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      isDismissible: false,
      enableDrag: false,
      builder: (_) => _ScheduleEditorSheet(title: title, existing: existing, onSave: onSave),
    );

class _ScheduleEditorSheet extends StatefulWidget {
  const _ScheduleEditorSheet({required this.title, this.existing, this.onSave});
  final String title;
  final Schedule? existing;
  final Future<void> Function(ScheduleDraft)? onSave;

  @override
  State<_ScheduleEditorSheet> createState() => _ScheduleEditorSheetState();
}

class _ScheduleEditorSheetState extends State<_ScheduleEditorSheet> {
  final _form = GlobalKey<FormState>();
  late final TextEditingController _titleCtrl;
  late final TextEditingController _locationCtrl;
  late final TextEditingController _noteCtrl;

  late DateTime _date;
  late TimeOfDay _time;
  late bool _allDay;
  late int _remindMin;
  late ScheduleRecurrence _recurrence;
  late Set<int> _weekDays;
  bool _saving = false;

  static const _remindOptions = <(int, String)>[
    (0, '准点提醒'),
    (10, '提前 10 分钟'),
    (30, '提前 30 分钟'),
    (60, '提前 1 小时'),
    (1440, '提前 1 天'),
    (-1, '仅记录不响铃'),
  ];

  @override
  void initState() {
    super.initState();
    final ex = widget.existing;
    _titleCtrl = TextEditingController(text: ex?.title ?? '');
    _locationCtrl = TextEditingController(text: ex?.location ?? '');
    _noteCtrl = TextEditingController(text: ex?.note ?? '');

    final initialDt = ex != null
        ? DateTime.fromMillisecondsSinceEpoch(ex.startAt)
        : DateTime.now().add(const Duration(hours: 1));
    _date = DateTime(initialDt.year, initialDt.month, initialDt.day);
    _time = ex != null ? TimeOfDay.fromDateTime(initialDt) : TimeOfDay(hour: initialDt.hour, minute: 0);
    _allDay = ex?.allDay ?? false;
    _remindMin = ex?.remindBeforeMin ?? 0;
    _recurrence = ex?.recurrence ?? ScheduleRecurrence.none;
    _weekDays = {};
    if (ex != null && ex.recurrenceDays.isNotEmpty) {
      for (final p in ex.recurrenceDays.split(',')) {
        final n = int.tryParse(p.trim());
        if (n != null && n >= 1 && n <= 7) _weekDays.add(n);
      }
    }
    if (_weekDays.isEmpty) {
      _weekDays.add(_date.weekday);
    }
  }

  @override
  void dispose() {
    _titleCtrl.dispose();
    _locationCtrl.dispose();
    _noteCtrl.dispose();
    super.dispose();
  }

  Future<void> _pickDate() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: _date,
      firstDate: DateTime(now.year - 1, 1, 1),
      lastDate: DateTime(now.year + 10, 12, 31),
    );
    if (picked != null && mounted) {
      setState(() {
        _date = DateTime(picked.year, picked.month, picked.day);
        if (_recurrence == ScheduleRecurrence.weekly && _weekDays.length <= 1) {
          _weekDays = {_date.weekday};
        }
      });
    }
  }

  Future<void> _pickTime() async {
    final picked = await showTimePicker(
      context: context,
      initialTime: _time,
      builder: (ctx, child) => MediaQuery(
        data: MediaQuery.of(ctx).copyWith(alwaysUse24HourFormat: true),
        child: child!,
      ),
    );
    if (picked != null && mounted) {
      setState(() => _time = picked);
    }
  }

  Future<void> _save() async {
    if (_saving) return;
    if (!_form.currentState!.validate()) return;
    final startDt = _allDay
        ? DateTime(_date.year, _date.month, _date.day)
        : DateTime(_date.year, _date.month, _date.day, _time.hour, _time.minute);
    final sortedDays = (_weekDays.toList()..sort()).join(',');
    final draft = ScheduleDraft(
      title: _titleCtrl.text.trim(),
      startAt: startDt.millisecondsSinceEpoch,
      allDay: _allDay,
      remindBeforeMin: _remindMin,
      recurrence: _recurrence,
      recurrenceDays: _recurrence == ScheduleRecurrence.weekly ? sortedDays : '',
      location: _locationCtrl.text.trim(),
      note: _noteCtrl.text.trim(),
    );
    setState(() => _saving = true);
    try {
      await widget.onSave?.call(draft);
      if (mounted) Navigator.pop(context, draft);
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  static const _weekdays = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日'];

  String _formatDateLabel(DateTime d) {
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);
    final diff = d.difference(today).inDays;
    final prefix = switch (diff) {
      0 => '今天 · ',
      1 => '明天 · ',
      2 => '后天 · ',
      _ => '',
    };
    return '$prefix${d.year}年${d.month}月${d.day}日（${_weekdays[d.weekday]}）';
  }

  String _formatTimeLabel(TimeOfDay t) =>
      '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final remindChoices = [
      ..._remindOptions,
      if (!_remindOptions.any((o) => o.$1 == _remindMin)) (_remindMin, '提前 $_remindMin 分钟'),
    ];

    return PopScope(
      canPop: !_saving,
      child: SafeArea(
        top: false,
        child: SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(20, 8, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
          child: Form(
            key: _form,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(widget.title, style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 16),
                TextFormField(
                  controller: _titleCtrl,
                  enabled: !_saving,
                  maxLength: 120,
                  style: Theme.of(context).textTheme.bodyLarge,
                  decoration: const InputDecoration(
                    labelText: '要记什么事？',
                    hintText: '例如：吃降压药、去医院复查、接孩子',
                  ),
                  validator: (v) {
                    final n = (v ?? '').trim().runes.length;
                    if (n == 0) return '请先填写要记的事项';
                    if (n > 120) return '标题请控制在 120 字以内';
                    return null;
                  },
                ),
                const SizedBox(height: 8),
                SwitchListTile.adaptive(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('全天事项', style: TextStyle(fontSize: 16.5, fontWeight: FontWeight.w500)),
                  subtitle: Text('不指定几点钟（开启提醒时默认当天 09:00 通知）', style: TextStyle(fontSize: 13.5, color: p.muted)),
                  value: _allDay,
                  onChanged: _saving ? null : (v) => setState(() => _allDay = v),
                ),
                const SizedBox(height: 6),
                Row(
                  children: [
                    Expanded(
                      flex: 3,
                      child: OutlinedButton.icon(
                        style: OutlinedButton.styleFrom(
                          minimumSize: const Size.fromHeight(48),
                          alignment: Alignment.centerLeft,
                          padding: const EdgeInsets.symmetric(horizontal: 14),
                        ),
                        onPressed: _saving ? null : _pickDate,
                        icon: const AppIcon(AppIcons.calendar, size: 19),
                        label: Text(_formatDateLabel(_date), overflow: TextOverflow.ellipsis),
                      ),
                    ),
                    if (!_allDay) ...[
                      const SizedBox(width: 10),
                      Expanded(
                        flex: 2,
                        child: OutlinedButton.icon(
                          style: OutlinedButton.styleFrom(
                            minimumSize: const Size.fromHeight(48),
                            padding: const EdgeInsets.symmetric(horizontal: 14),
                          ),
                          onPressed: _saving ? null : _pickTime,
                          icon: const AppIcon(AppIcons.clock, size: 19),
                          label: Text(_formatTimeLabel(_time), style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
                        ),
                      ),
                    ],
                  ],
                ),
                const SizedBox(height: 18),
                Text('重复规则', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: p.muted)),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final r in ScheduleRecurrence.values)
                      ChoiceChip(
                        label: Text(r.label),
                        selected: _recurrence == r,
                        onSelected: _saving ? null : (_) => setState(() => _recurrence = r),
                      ),
                  ],
                ),
                if (_recurrence == ScheduleRecurrence.weekly) ...[
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: [
                      for (var d = 1; d <= 7; d++)
                        FilterChip(
                          label: Text(_weekdays[d]),
                          selected: _weekDays.contains(d),
                          onSelected: _saving
                              ? null
                              : (on) => setState(() {
                                    if (on) {
                                      _weekDays.add(d);
                                    } else if (_weekDays.length > 1) {
                                      _weekDays.remove(d);
                                    }
                                  }),
                        ),
                    ],
                  ),
                ],
                const SizedBox(height: 18),
                Text('手机提醒时间', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: p.muted)),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final opt in remindChoices)
                      ChoiceChip(
                        label: Text(opt.$2),
                        selected: _remindMin == opt.$1,
                        onSelected: _saving ? null : (_) => setState(() => _remindMin = opt.$1),
                      ),
                  ],
                ),
                const SizedBox(height: 16),
                TextFormField(
                  controller: _locationCtrl,
                  enabled: !_saving,
                  decoration: const InputDecoration(
                    labelText: '地点（选填）',
                    hintText: '例如：社区医院门诊二楼',
                  ),
                ),
                const SizedBox(height: 12),
                TextFormField(
                  controller: _noteCtrl,
                  enabled: !_saving,
                  minLines: 1,
                  maxLines: 3,
                  decoration: const InputDecoration(
                    labelText: '备注（选填）',
                    hintText: '例如：空腹抽血、记得带医保卡',
                  ),
                ),
                const SizedBox(height: 22),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                        onPressed: _saving ? null : () => Navigator.pop(context),
                        child: const Text('取消'),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: FilledButton(
                        style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                        onPressed: _saving ? null : _save,
                        child: Text(_saving ? '保存中…' : '保存日程'),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

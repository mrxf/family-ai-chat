import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/pinned_components.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

/// Opens a cross-platform adaptive sheet/dialog to create or edit an AI scheduled task.
Future<ScheduledTaskDraft?> showScheduledTaskEditor(
  BuildContext context, {
  String title = '新建 AI 定时任务',
  ScheduledTask? existing,
  String? initialTitle,
  String? initialPrompt,
  String? initialTargetEntityId,
  String? initialTargetMessageId,
  String? initialTargetComponentId,
  Future<void> Function(ScheduledTaskDraft)? onSave,
}) =>
    showAdaptiveModal<ScheduledTaskDraft>(
      context: context,
      maxWidth: 560,
      isDismissible: false,
      enableDrag: false,
      builder: (_) => _ScheduledTaskEditorSheet(
        title: title,
        existing: existing,
        initialTitle: initialTitle,
        initialPrompt: initialPrompt,
        initialTargetEntityId: initialTargetEntityId,
        initialTargetMessageId: initialTargetMessageId,
        initialTargetComponentId: initialTargetComponentId,
        onSave: onSave,
      ),
    );

class _ScheduledTaskEditorSheet extends ConsumerStatefulWidget {
  const _ScheduledTaskEditorSheet({
    required this.title,
    this.existing,
    this.initialTitle,
    this.initialPrompt,
    this.initialTargetEntityId,
    this.initialTargetMessageId,
    this.initialTargetComponentId,
    this.onSave,
  });

  final String title;
  final ScheduledTask? existing;
  final String? initialTitle;
  final String? initialPrompt;
  final String? initialTargetEntityId;
  final String? initialTargetMessageId;
  final String? initialTargetComponentId;
  final Future<void> Function(ScheduledTaskDraft)? onSave;

  @override
  ConsumerState<_ScheduledTaskEditorSheet> createState() => _ScheduledTaskEditorSheetState();
}

class _ScheduledTaskEditorSheetState extends ConsumerState<_ScheduledTaskEditorSheet> {
  final _form = GlobalKey<FormState>();
  late final TextEditingController _titleCtrl;
  late final TextEditingController _promptCtrl;
  late final TextEditingController _intervalCtrl;

  late ScheduledTaskKind _kind;
  late TimeOfDay _time;
  late Set<int> _weekDays;
  late int _dayOfMonth;
  late DateTime _onceDate;
  late bool _enabled;
  bool _runImmediately = false;
  late String _targetEntityId;
  late String _targetMessageId;
  late String _targetComponentId;
  bool _saving = false;

  static const _weekdays = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日'];

  static const _templates = [
    (
      label: '每日 AI 新闻',
      title: '每日 AI 前沿新闻',
      time: TimeOfDay(hour: 9, minute: 0),
      prompt: '使用 web_search 查询近 24 小时最新的 3 条 AI 领域重磅新闻。如果已绑定卡片，请调用 update_component 用 prepend_items 插入最新 3 条（tag 设为「今日」），设置 previous_tag="往期"、max_items=6；如果尚未绑定卡片，请先调用 show_feed 生成「每日 AI 新闻」资讯卡片并调用 pin_component 固定到首页。',
    ),
    (
      label: '每晚待办复盘',
      title: '每晚日程与待办复盘',
      time: TimeOfDay(hour: 21, minute: 0),
      prompt: '调用 list_schedules 查看今明两天日程，并调用 list_components 查看首页未完成待办清单，总结今天进展并整理明天最重要的 3 件事。',
    ),
    (
      label: '每日天气简报',
      title: '每日天气更新',
      time: TimeOfDay(hour: 7, minute: 30),
      prompt: '使用 web_search 查询本地今日及未来 7 天天气预报。如果已绑定天气卡片，请调用 update_component 原地更新为最新一周天气；如果尚未绑定，请先调用 show_weather 生成天气卡片并调用 pin_component 固定到首页。',
    ),
  ];

  @override
  void initState() {
    super.initState();
    final ex = widget.existing;
    _titleCtrl = TextEditingController(text: ex?.title ?? widget.initialTitle ?? '');
    _promptCtrl = TextEditingController(text: ex?.prompt ?? widget.initialPrompt ?? '');
    _kind = ex?.scheduleKind ?? ScheduledTaskKind.daily;

    var hh = 7;
    var mm = 0;
    final rawTime = ex?.timeOfDay ?? '07:00';
    final parts = rawTime.split(':');
    if (parts.length >= 2) {
      hh = int.tryParse(parts[0]) ?? 7;
      mm = int.tryParse(parts[1]) ?? 0;
    }
    _time = TimeOfDay(hour: hh.clamp(0, 23), minute: mm.clamp(0, 59));

    _weekDays = {};
    if (ex != null && ex.weekdays.isNotEmpty) {
      for (final p in ex.weekdays.split(',')) {
        final n = int.tryParse(p.trim());
        if (n != null && n >= 1 && n <= 7) _weekDays.add(n);
      }
    }
    if (_weekDays.isEmpty) _weekDays.add(DateTime.now().weekday);

    _dayOfMonth = (ex?.dayOfMonth ?? 1).clamp(1, 31);
    _intervalCtrl = TextEditingController(text: '${(ex?.intervalMinutes ?? 60).clamp(15, 10080)}');
    final onceDt = ex != null && ex.runAt > 0
        ? DateTime.fromMillisecondsSinceEpoch(ex.runAt)
        : DateTime.now().add(const Duration(hours: 1));
    _onceDate = DateTime(onceDt.year, onceDt.month, onceDt.day);
    if (ex != null && ex.scheduleKind == ScheduledTaskKind.once && ex.runAt > 0) {
      _time = TimeOfDay.fromDateTime(onceDt);
    }

    _enabled = ex?.enabled ?? true;
    _targetEntityId = ex?.targetEntityId ?? widget.initialTargetEntityId ?? '';
    _targetMessageId = ex?.targetMessageId ?? widget.initialTargetMessageId ?? '';
    _targetComponentId = ex?.targetComponentId ?? widget.initialTargetComponentId ?? '';
  }

  @override
  void dispose() {
    _titleCtrl.dispose();
    _promptCtrl.dispose();
    _intervalCtrl.dispose();
    super.dispose();
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
    if (picked != null && mounted) setState(() => _time = picked);
  }

  Future<void> _pickOnceDate() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: _onceDate,
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: DateTime(now.year + 5, 12, 31),
    );
    if (picked != null && mounted) {
      setState(() => _onceDate = DateTime(picked.year, picked.month, picked.day));
    }
  }

  String _fmtTime(TimeOfDay t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

  Future<void> _save() async {
    if (_saving) return;
    if (!_form.currentState!.validate()) return;
    final intervalMins = int.tryParse(_intervalCtrl.text.trim()) ?? 60;
    if (_kind == ScheduledTaskKind.interval && intervalMins < 15) {
      showToast(context, '间隔执行最短为 15 分钟');
      return;
    }
    final onceDt = DateTime(_onceDate.year, _onceDate.month, _onceDate.day, _time.hour, _time.minute);
    final sortedDays = (_weekDays.toList()..sort()).join(',');
    final draft = ScheduledTaskDraft(
      title: _titleCtrl.text.trim(),
      prompt: _promptCtrl.text.trim(),
      scheduleKind: _kind,
      timeOfDay: _fmtTime(_time),
      weekdays: _kind == ScheduledTaskKind.weekly ? sortedDays : '',
      dayOfMonth: _dayOfMonth,
      intervalMinutes: _kind == ScheduledTaskKind.interval ? intervalMins : 0,
      runAt: _kind == ScheduledTaskKind.once ? onceDt.millisecondsSinceEpoch : 0,
      enabled: _enabled,
      targetEntityId: _targetEntityId,
      targetMessageId: _targetMessageId,
      targetComponentId: _targetComponentId,
      runImmediately: widget.existing == null && _runImmediately,
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

  String _pinKey(PinnedComponent pin) =>
      pin.effectiveEntityId.isNotEmpty ? 'ent:${pin.effectiveEntityId}' : '${pin.messageId}|${pin.componentId}';

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final pins = ref.watch(pinnedComponentsProvider).items;
    final matchedPin = pins.where((pin) {
      if (_targetEntityId.isNotEmpty && pin.effectiveEntityId == _targetEntityId) return true;
      return _targetMessageId.isNotEmpty &&
          _targetComponentId.isNotEmpty &&
          pin.messageId == _targetMessageId &&
          pin.componentId == _targetComponentId;
    }).firstOrNull;
    final currentTargetKey = matchedPin != null ? _pinKey(matchedPin) : '';
    final hasAnyTarget = _targetEntityId.isNotEmpty || (_targetMessageId.isNotEmpty && _targetComponentId.isNotEmpty);

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
                if (widget.existing == null && !hasAnyTarget) ...[
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 8,
                    runSpacing: 6,
                    children: [
                      for (final tpl in _templates)
                        ActionChip(
                          avatar: AppIcon(AppIcons.sparkles, size: 14, color: p.accent),
                          label: Text(tpl.label, style: const TextStyle(fontSize: 12.5)),
                          onPressed: _saving
                              ? null
                              : () => setState(() {
                                    _titleCtrl.text = tpl.title;
                                    _promptCtrl.text = tpl.prompt;
                                    _kind = ScheduledTaskKind.daily;
                                    _time = tpl.time;
                                    _runImmediately = true;
                                  }),
                        ),
                    ],
                  ),
                ],
                const SizedBox(height: 14),
                TextFormField(
                  controller: _titleCtrl,
                  enabled: !_saving,
                  maxLength: 80,
                  decoration: const InputDecoration(
                    labelText: '任务名称',
                    hintText: '例如：每日大模型新闻更新、每晚日程复盘',
                  ),
                  validator: (v) => (v ?? '').trim().isEmpty ? '请填写任务名称' : null,
                ),
                const SizedBox(height: 8),
                TextFormField(
                  controller: _promptCtrl,
                  enabled: !_saving,
                  minLines: 3,
                  maxLines: 6,
                  maxLength: 2000,
                  decoration: const InputDecoration(
                    labelText: 'AI 执行指令',
                    hintText: '例如：查询最新 3 条大模型新闻，更新卡片前三条，把原来的前三条放到第 4-6 条',
                    alignLabelWithHint: true,
                  ),
                  validator: (v) => (v ?? '').trim().isEmpty ? '请填写 AI 定时要执行的指令' : null,
                ),
                const SizedBox(height: 12),
                Text('触发频率', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.muted)),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final k in ScheduledTaskKind.values)
                      ChoiceChip(
                        label: Text(k.label),
                        selected: _kind == k,
                        onSelected: _saving ? null : (_) => setState(() => _kind = k),
                      ),
                  ],
                ),
                const SizedBox(height: 12),
                if (_kind == ScheduledTaskKind.daily ||
                    _kind == ScheduledTaskKind.weekdays ||
                    _kind == ScheduledTaskKind.weekly ||
                    _kind == ScheduledTaskKind.monthly) ...[
                  Row(
                    children: [
                      Expanded(
                        child: OutlinedButton.icon(
                          style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(46)),
                          onPressed: _saving ? null : _pickTime,
                          icon: const AppIcon(AppIcons.clock, size: 18),
                          label: Text('执行时间 ${_fmtTime(_time)}', style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600)),
                        ),
                      ),
                      if (_kind == ScheduledTaskKind.monthly) ...[
                        const SizedBox(width: 10),
                        DropdownButton<int>(
                          value: _dayOfMonth,
                          items: [
                            for (var d = 1; d <= 31; d++) DropdownMenuItem(value: d, child: Text('每月 $d 日')),
                          ],
                          onChanged: _saving ? null : (v) => setState(() => _dayOfMonth = v ?? 1),
                        ),
                      ],
                    ],
                  ),
                  if (_kind == ScheduledTaskKind.weekly) ...[
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
                ] else if (_kind == ScheduledTaskKind.interval) ...[
                  TextFormField(
                    controller: _intervalCtrl,
                    enabled: !_saving,
                    keyboardType: TextInputType.number,
                    decoration: const InputDecoration(
                      labelText: '间隔分钟数（最短 15 分钟）',
                      hintText: '例如：60 表示每小时执行一次',
                    ),
                  ),
                ] else if (_kind == ScheduledTaskKind.once) ...[
                  Row(
                    children: [
                      Expanded(
                        flex: 3,
                        child: OutlinedButton.icon(
                          style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(46)),
                          onPressed: _saving ? null : _pickOnceDate,
                          icon: const AppIcon(AppIcons.calendar, size: 18),
                          label: Text('${_onceDate.year}-${_onceDate.month.toString().padLeft(2, '0')}-${_onceDate.day.toString().padLeft(2, '0')}'),
                        ),
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        flex: 2,
                        child: OutlinedButton.icon(
                          style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(46)),
                          onPressed: _saving ? null : _pickTime,
                          icon: const AppIcon(AppIcons.clock, size: 18),
                          label: Text(_fmtTime(_time)),
                        ),
                      ),
                    ],
                  ),
                ],
                if (pins.isNotEmpty || hasAnyTarget) ...[
                  const SizedBox(height: 16),
                  Text('绑定要原地更新的首页卡片（选填）', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.muted)),
                  const SizedBox(height: 8),
                  DropdownButtonFormField<String>(
                    initialValue: currentTargetKey,
                    isExpanded: true,
                    decoration: const InputDecoration(isDense: true),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('不绑定（自动分析或首次运行时创建）')),
                      for (final pin in pins)
                        DropdownMenuItem(
                          value: _pinKey(pin),
                          child: Text(
                            '${describeComponent(pin.component).kindLabel} · ${describeComponent(pin.component).title}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                    ],
                    onChanged: _saving
                        ? null
                        : (v) => setState(() {
                              if (v == null || v.isEmpty) {
                                _targetEntityId = '';
                                _targetMessageId = '';
                                _targetComponentId = '';
                              } else {
                                final picked = pins.where((pin) => _pinKey(pin) == v).firstOrNull;
                                if (picked != null) {
                                  _targetEntityId = picked.effectiveEntityId;
                                  _targetMessageId = picked.messageId;
                                  _targetComponentId = picked.componentId;
                                }
                              }
                            }),
                  ),
                ],
                if (widget.existing == null) ...[
                  const SizedBox(height: 8),
                  SwitchListTile.adaptive(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('保存后立即试跑一次', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w500)),
                    subtitle: Text('立即在后台执行一遍验证效果', style: TextStyle(fontSize: 12.5, color: p.muted)),
                    value: _runImmediately,
                    onChanged: _saving ? null : (v) => setState(() => _runImmediately = v),
                  ),
                ],
                const SizedBox(height: 18),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                        onPressed: _saving ? null : () => Navigator.pop(context),
                        child: const Text('取消'),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: FilledButton(
                        style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                        onPressed: _saving ? null : _save,
                        child: Text(_saving ? '保存中…' : '保存任务'),
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

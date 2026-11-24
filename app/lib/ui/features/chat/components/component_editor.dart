import 'package:flutter/material.dart';

import '../../../../core/models/models.dart';
import '../../../adaptive/adaptive_modal.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

/// Opens a cross-platform adaptive modal allowing the user to directly edit
/// a component's title, subtitle, and entries (supports feed, todo, weather, metrics).
Future<Map<String, dynamic>?> showComponentEditor(
  BuildContext context, {
  required MessageComponent component,
  required Future<String?> Function(Map<String, dynamic> nextData) onSave,
}) =>
    showAdaptiveModal<Map<String, dynamic>>(
      context: context,
      maxWidth: 560,
      isDismissible: false,
      enableDrag: false,
      builder: (_) => _ComponentEditorSheet(component: component, onSave: onSave),
    );

bool isComponentDirectlyEditable(MessageComponent c) =>
    const {'feed', 'todo', 'weather', 'metrics'}.contains(c.kind);

class _EditableRow {
  _EditableRow({
    required this.id,
    required String primary,
    String secondary = '',
    String tag = '',
    String extra = '',
    this.done = false,
  })  : primaryCtrl = TextEditingController(text: primary),
        secondaryCtrl = TextEditingController(text: secondary),
        tagCtrl = TextEditingController(text: tag),
        extraCtrl = TextEditingController(text: extra);

  final String id;
  final TextEditingController primaryCtrl;
  final TextEditingController secondaryCtrl;
  final TextEditingController tagCtrl;
  final TextEditingController extraCtrl;
  final bool done;

  void dispose() {
    primaryCtrl.dispose();
    secondaryCtrl.dispose();
    tagCtrl.dispose();
    extraCtrl.dispose();
  }
}

class _ComponentEditorSheet extends StatefulWidget {
  const _ComponentEditorSheet({required this.component, required this.onSave});
  final MessageComponent component;
  final Future<String?> Function(Map<String, dynamic> nextData) onSave;

  @override
  State<_ComponentEditorSheet> createState() => _ComponentEditorSheetState();
}

class _ComponentEditorSheetState extends State<_ComponentEditorSheet> {
  late final TextEditingController _titleCtrl;
  late final TextEditingController _subtitleCtrl;
  late final TextEditingController _weatherCondCtrl;
  late final TextEditingController _weatherTempCtrl;
  late final TextEditingController _weatherTipCtrl;
  final List<_EditableRow> _rows = [];
  bool _saving = false;

  String get _kind => widget.component.kind;

  @override
  void initState() {
    super.initState();
    final d = widget.component.data;
    _titleCtrl = TextEditingController(text: '${d['title'] ?? d['location'] ?? ''}');
    _subtitleCtrl = TextEditingController(text: '${d['subtitle'] ?? d['date_label'] ?? ''}');
    _weatherCondCtrl = TextEditingController(text: '${d['condition'] ?? ''}');
    _weatherTempCtrl = TextEditingController(text: d['temp'] != null ? '${d['temp']}' : '');
    _weatherTipCtrl = TextEditingController(text: '${d['tip'] ?? ''}');

    if (_kind == 'feed') {
      final raw = d['items'] is List ? (d['items'] as List) : const [];
      for (var i = 0; i < raw.length; i++) {
        final m = raw[i];
        if (m is! Map) continue;
        _rows.add(_EditableRow(
          id: '${m['id'] ?? 'f${i + 1}'}',
          primary: '${m['title'] ?? ''}',
          secondary: '${m['summary'] ?? ''}',
          tag: '${m['tag'] ?? ''}',
          extra: '${m['source'] ?? ''}',
        ));
      }
    } else if (_kind == 'todo') {
      final raw = d['items'] is List ? (d['items'] as List) : const [];
      for (var i = 0; i < raw.length; i++) {
        final m = raw[i];
        if (m is! Map) continue;
        _rows.add(_EditableRow(
          id: '${m['id'] ?? 'i${i + 1}'}',
          primary: '${m['text'] ?? ''}',
          secondary: '${m['note'] ?? ''}',
          tag: '${m['tag'] ?? ''}',
          done: m['done'] == true,
        ));
      }
    } else if (_kind == 'metrics') {
      final raw = d['metrics'] is List ? (d['metrics'] as List) : const [];
      for (var i = 0; i < raw.length; i++) {
        final m = raw[i];
        if (m is! Map) continue;
        _rows.add(_EditableRow(
          id: 'm${i + 1}',
          primary: '${m['label'] ?? ''}',
          secondary: '${m['value'] ?? ''}',
          tag: '${m['unit'] ?? ''}',
          extra: '${m['delta'] ?? ''}',
        ));
      }
    }
  }

  @override
  void dispose() {
    _titleCtrl.dispose();
    _subtitleCtrl.dispose();
    _weatherCondCtrl.dispose();
    _weatherTempCtrl.dispose();
    _weatherTipCtrl.dispose();
    for (final r in _rows) {
      r.dispose();
    }
    super.dispose();
  }

  void _addRow() {
    final prefix = _kind == 'feed' ? 'f' : (_kind == 'todo' ? 'i' : 'm');
    setState(() {
      _rows.insert(0, _EditableRow(id: '${prefix}_${DateTime.now().millisecondsSinceEpoch}', primary: ''));
    });
  }

  void _moveRow(int idx, int delta) {
    final target = idx + delta;
    if (target < 0 || target >= _rows.length) return;
    setState(() {
      final item = _rows.removeAt(idx);
      _rows.insert(target, item);
    });
  }

  void _removeRow(int idx) {
    if (_rows.length <= 1) {
      showToast(context, '至少保留 1 条内容');
      return;
    }
    setState(() {
      final removed = _rows.removeAt(idx);
      removed.dispose();
    });
  }

  Map<String, dynamic>? _buildPayload() {
    final base = Map<String, dynamic>.from(widget.component.data);
    if (_kind == 'weather') {
      final loc = _titleCtrl.text.trim();
      final cond = _weatherCondCtrl.text.trim();
      if (loc.isEmpty || cond.isEmpty) {
        showToast(context, '请填写城市和天气现象');
        return null;
      }
      base['location'] = loc;
      base['date_label'] = _subtitleCtrl.text.trim();
      base['condition'] = cond;
      final parsedTemp = double.tryParse(_weatherTempCtrl.text.trim());
      if (parsedTemp != null) base['temp'] = parsedTemp;
      base['tip'] = _weatherTipCtrl.text.trim();
      return base;
    }

    final title = _titleCtrl.text.trim();
    if (title.isEmpty) {
      showToast(context, '请填写卡片标题');
      return null;
    }
    base['title'] = title;
    base['subtitle'] = _subtitleCtrl.text.trim();

    if (_kind == 'feed') {
      final origById = <String, Map<String, dynamic>>{};
      if (base['items'] is List) {
        for (final e in base['items'] as List) {
          if (e is Map && e['id'] != null) origById['${e['id']}'] = Map<String, dynamic>.from(e.cast());
        }
      }
      final items = <Map<String, dynamic>>[];
      for (final r in _rows) {
        final t = r.primaryCtrl.text.trim();
        if (t.isEmpty) continue;
        final orig = origById[r.id];
        items.add({
          'id': r.id,
          'title': t,
          if (r.secondaryCtrl.text.trim().isNotEmpty) 'summary': r.secondaryCtrl.text.trim(),
          if (r.tagCtrl.text.trim().isNotEmpty) 'tag': r.tagCtrl.text.trim(),
          if (r.extraCtrl.text.trim().isNotEmpty) 'source': r.extraCtrl.text.trim(),
          if (orig != null && '${orig['url'] ?? ''}'.isNotEmpty) 'url': orig['url'],
          if (orig != null && '${orig['published_at'] ?? ''}'.isNotEmpty) 'published_at': orig['published_at'],
        });
      }
      if (items.isEmpty) {
        showToast(context, '请至少保留 1 条有效条目');
        return null;
      }
      base['items'] = items;
      return base;
    }

    if (_kind == 'todo') {
      final items = <Map<String, dynamic>>[];
      for (final r in _rows) {
        final t = r.primaryCtrl.text.trim();
        if (t.isEmpty) continue;
        items.add({
          'id': r.id,
          'text': t,
          if (r.secondaryCtrl.text.trim().isNotEmpty) 'note': r.secondaryCtrl.text.trim(),
          if (r.tagCtrl.text.trim().isNotEmpty) 'tag': r.tagCtrl.text.trim(),
          if (r.done) 'done': true,
        });
      }
      if (items.isEmpty) {
        showToast(context, '请至少保留 1 条待办事项');
        return null;
      }
      base['items'] = items;
      return base;
    }

    if (_kind == 'metrics') {
      final metrics = <Map<String, dynamic>>[];
      for (final r in _rows) {
        final lbl = r.primaryCtrl.text.trim();
        final val = r.secondaryCtrl.text.trim();
        if (lbl.isEmpty || val.isEmpty) continue;
        metrics.add({
          'label': lbl,
          'value': val,
          if (r.tagCtrl.text.trim().isNotEmpty) 'unit': r.tagCtrl.text.trim(),
          if (r.extraCtrl.text.trim().isNotEmpty) 'delta': r.extraCtrl.text.trim(),
        });
      }
      if (metrics.isEmpty) {
        showToast(context, '请至少保留 1 项指标');
        return null;
      }
      base['metrics'] = metrics;
      return base;
    }
    return base;
  }

  Future<void> _save() async {
    if (_saving) return;
    final payload = _buildPayload();
    if (payload == null) return;
    setState(() => _saving = true);
    try {
      final err = await widget.onSave(payload);
      if (!mounted) return;
      if (err != null) {
        showToast(context, err);
        return;
      }
      Navigator.pop(context, payload);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isListKind = _kind == 'feed' || _kind == 'todo' || _kind == 'metrics';

    return PopScope(
      canPop: !_saving,
      child: SafeArea(
        top: false,
        child: SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(20, 8, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('编辑卡片内容', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 14),
              TextField(
                controller: _titleCtrl,
                enabled: !_saving,
                decoration: InputDecoration(labelText: _kind == 'weather' ? '城市' : '标题'),
              ),
              const SizedBox(height: 10),
              TextField(
                controller: _subtitleCtrl,
                enabled: !_saving,
                decoration: InputDecoration(labelText: _kind == 'weather' ? '日期标签（选填）' : '副标题（选填）'),
              ),
              if (_kind == 'weather') ...[
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _weatherCondCtrl,
                        enabled: !_saving,
                        decoration: const InputDecoration(labelText: '天气现象', hintText: '晴 / 多云 / 小雨'),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: TextField(
                        controller: _weatherTempCtrl,
                        enabled: !_saving,
                        keyboardType: const TextInputType.numberWithOptions(decimal: true, signed: true),
                        decoration: const InputDecoration(labelText: '气温 (°C)'),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 10),
                TextField(
                  controller: _weatherTipCtrl,
                  enabled: !_saving,
                  decoration: const InputDecoration(labelText: '贴心提示（选填）'),
                ),
              ],
              if (isListKind) ...[
                const SizedBox(height: 16),
                Row(
                  children: [
                    Text(
                      _kind == 'metrics' ? '指标项（${_rows.length}）' : '条目列表（${_rows.length}）',
                      style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: p.muted),
                    ),
                    const Spacer(),
                    TextButton.icon(
                      onPressed: _saving ? null : _addRow,
                      icon: const AppIcon(AppIcons.plus, size: 16),
                      label: const Text('新增一条'),
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                for (var i = 0; i < _rows.length; i++)
                  Container(
                    key: ValueKey(_rows[i].id),
                    margin: const EdgeInsets.only(bottom: 10),
                    padding: const EdgeInsets.fromLTRB(12, 10, 8, 10),
                    decoration: BoxDecoration(
                      color: p.surfaceAlt.withValues(alpha: 0.6),
                      borderRadius: BorderRadius.circular(14),
                      border: Border.all(color: p.line),
                    ),
                    child: Column(
                      children: [
                        Row(
                          children: [
                            Text('#${i + 1}', style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w700, color: p.accent)),
                            const Spacer(),
                            IconButton(
                              tooltip: '上移',
                              visualDensity: VisualDensity.compact,
                              onPressed: _saving || i == 0 ? null : () => _moveRow(i, -1),
                              icon: const AppIcon(AppIcons.chevronUp, size: 16),
                            ),
                            IconButton(
                              tooltip: '下移',
                              visualDensity: VisualDensity.compact,
                              onPressed: _saving || i == _rows.length - 1 ? null : () => _moveRow(i, 1),
                              icon: const AppIcon(AppIcons.chevronDown, size: 16),
                            ),
                            IconButton(
                              tooltip: '删除这条',
                              visualDensity: VisualDensity.compact,
                              onPressed: _saving ? null : () => _removeRow(i),
                              icon: AppIcon(AppIcons.delete, size: 16, color: p.bad),
                            ),
                          ],
                        ),
                        TextField(
                          controller: _rows[i].primaryCtrl,
                          enabled: !_saving,
                          decoration: InputDecoration(
                            isDense: true,
                            labelText: _kind == 'feed' ? '标题' : (_kind == 'todo' ? '待办内容' : '指标名'),
                          ),
                        ),
                        const SizedBox(height: 8),
                        Row(
                          children: [
                            Expanded(
                              flex: 3,
                              child: TextField(
                                controller: _rows[i].secondaryCtrl,
                                enabled: !_saving,
                                decoration: InputDecoration(
                                  isDense: true,
                                  labelText: _kind == 'feed' ? '摘要（选填）' : (_kind == 'todo' ? '备注（选填）' : '数值'),
                                ),
                              ),
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              flex: 2,
                              child: TextField(
                                controller: _rows[i].tagCtrl,
                                enabled: !_saving,
                                decoration: InputDecoration(
                                  isDense: true,
                                  labelText: _kind == 'metrics' ? '单位' : '标签',
                                ),
                              ),
                            ),
                          ],
                        ),
                      ],
                    ),
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
                      child: Text(_saving ? '保存中…' : '保存修改'),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../state/catalog.dart';
import '../../state/session.dart';
import '../sheets.dart';
import '../theme.dart';
import '../widgets.dart';
import 'common.dart';

/// Edits a model (modelId) or creates one manually under providerId.
class ModelEditPage extends ConsumerStatefulWidget {
  const ModelEditPage({super.key, this.modelId, this.providerId}) : assert(modelId != null || providerId != null);
  final String? modelId;
  final String? providerId;

  @override
  ConsumerState<ModelEditPage> createState() => _ModelEditPageState();
}

// Everyday replies: what reasoning_effort to send.
// GPT-5 (5.0) has no 'none'; 'minimal' is its fastest setting.
const _dailyOptions = [('none', '快速'), ('minimal', '少想'), ('low', '稍想一下'), ('', '不发送')];
// Deep thinking button: '' = the model has no deep-thinking mode.
const _thinkOptions = [('', '不支持'), ('low', '低'), ('medium', '中'), ('high', '高')];
// Mainstream models start at 128K now; 1M for the long-context ones.
const _contextOptions = [128000, 200000, 256000, 400000, 1000000];

class _ModelEditPageState extends ConsumerState<ModelEditPage> {
  final _name = TextEditingController();
  final _desc = TextEditingController();
  final _upstream = TextEditingController();
  final _daily = TextEditingController();
  final _think = TextEditingController();
  final _context = TextEditingController();
  final _maxOut = TextEditingController();
  final _inPrice = TextEditingController();
  final _outPrice = TextEditingController();
  bool _vision = false, _enabled = true;
  bool _loaded = false, _dirty = false, _saving = false, _advanced = false;
  ModelTestResult? _test;
  bool? _testingThink; // null = not testing

  bool get _isNew => widget.modelId == null;

  @override
  void initState() {
    super.initState();
    for (final c in [_name, _desc, _upstream, _daily, _think, _context, _maxOut, _inPrice, _outPrice]) {
      c.addListener(_markDirty);
    }
  }

  void _markDirty() {
    if (_loaded && !_dirty) setState(() => _dirty = true);
  }

  void _load(AdminModel? m) {
    _name.text = m?.displayName ?? '';
    _desc.text = m?.description ?? '';
    _upstream.text = m?.upstreamModel ?? '';
    _daily.text = m?.defaultEffort ?? '';
    _think.text = m?.thinkEffort ?? '';
    _context.text = '${m?.contextWindow ?? 128000}';
    _maxOut.text = (m?.maxOutput ?? 0) == 0 ? '' : '${m!.maxOutput}';
    _inPrice.text = _priceText(m?.inputPrice ?? 0);
    _outPrice.text = _priceText(m?.outputPrice ?? 0);
    _vision = m?.vision ?? false;
    _enabled = m?.enabled ?? true;
    _advanced = m != null && (!_dailyOptions.any((o) => o.$1 == m.defaultEffort) || !_thinkOptions.any((o) => o.$1 == m.thinkEffort));
    _loaded = true;
  }

  static String _priceText(double v) => v == 0 ? '' : v.toString();

  @override
  void dispose() {
    for (final c in [_name, _desc, _upstream, _daily, _think, _context, _maxOut, _inPrice, _outPrice]) {
      c.dispose();
    }
    super.dispose();
  }

  Map<String, dynamic> _body() => {
        'display_name': _name.text.trim(),
        'description': _desc.text.trim(),
        'upstream_model': _upstream.text.trim(),
        'default_effort': _daily.text.trim(),
        'think_effort': _think.text.trim(),
        'context_window': int.tryParse(_context.text.trim()) ?? 128000,
        'max_output': int.tryParse(_maxOut.text.trim()) ?? 0,
        'input_price': double.tryParse(_inPrice.text.trim()) ?? 0,
        'output_price': double.tryParse(_outPrice.text.trim()) ?? 0,
        'vision': _vision,
        'enabled': _enabled,
      };

  /// Saves; returns the saved model id, or null on failure.
  Future<String?> _save({bool quiet = false}) async {
    if (_upstream.text.trim().isEmpty) {
      showToast(context, '请填写上游模型 ID');
      return null;
    }
    setState(() => _saving = true);
    try {
      final api = ref.read(apiProvider);
      final m = _isNew
          ? await api.adminCreateModel({..._body(), 'provider_id': widget.providerId})
          : await api.adminUpdateModel(widget.modelId!, _body());
      await ref.read(catalogProvider.notifier).reload();
      if (!mounted) return m.id;
      setState(() => _dirty = false);
      if (_isNew) {
        Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => ModelEditPage(modelId: m.id)));
        showToast(context, '已添加');
      } else if (!quiet) {
        showToast(context, '已保存');
      }
      return m.id;
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
      return null;
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _runTest(bool think) async {
    if (_dirty && await _save(quiet: true) == null) return;
    if (!mounted) return;
    setState(() {
      _testingThink = think;
      _test = null;
    });
    try {
      final r = await ref.read(apiProvider).adminTestModel(widget.modelId!, think: think);
      if (mounted) setState(() => _test = r);
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _testingThink = null);
    }
  }

  Future<void> _delete(Catalog c, AdminModel m) async {
    final isDefault = c.settings.defaultModelId == m.id;
    final ok = await confirm(context,
        title: '删除「${m.displayName}」？',
        body: isDefault ? '它是全家默认模型，删除后会自动换成其他可用的模型。聊天记录不受影响。' : '家人将不能再选择它，聊天记录不受影响。',
        ok: '删除',
        danger: true);
    if (!ok) return;
    try {
      await ref.read(apiProvider).adminDeleteModel(m.id);
      await ref.read(catalogProvider.notifier).reload();
      if (mounted) {
        Navigator.of(context).pop();
        showToast(context, '已删除');
      }
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    }
  }

  Future<void> _makeDefault(AdminModel m) async {
    try {
      final s = await ref.read(apiProvider).adminPutModelSettings({'default_model_id': m.id});
      ref.read(catalogProvider.notifier).setSettings(s);
      await ref.read(catalogProvider.notifier).reload();
      if (mounted) showToast(context, '已设为全家默认模型');
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    }
  }

  Future<bool> _confirmLeave() async {
    if (!_dirty) return true;
    return confirm(context, title: '放弃修改？', body: '改动还没有保存。', ok: '放弃', danger: true);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final catalog = ref.watch(catalogProvider);
    final c = catalog.value;
    final m = _isNew ? null : c?.model(widget.modelId!);
    if (c == null || (!_isNew && m == null)) {
      return Scaffold(appBar: AppBar(), body: catalog.hasError ? ErrorView(message: ApiError.from(catalog.error!).message) : const LoadingView());
    }
    if (!_loaded) _load(m);
    final provider = c.provider(m?.providerId ?? widget.providerId!);
    final isDefault = m != null && c.settings.defaultModelId == m.id;

    return PopScope(
      canPop: !_dirty,
      onPopInvokedWithResult: (didPop, _) async {
        if (!didPop && await _confirmLeave() && context.mounted) {
          setState(() => _dirty = false);
          Navigator.of(context).pop();
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: Text(_isNew ? '手动添加模型' : '模型设置'),
          actions: [
            if (m != null) IconButton(tooltip: '删除', icon: Icon(Icons.delete_outline_rounded, color: p.bad), onPressed: () => _delete(c, m)),
          ],
        ),
        bottomNavigationBar: AnimatedSize(
          duration: const Duration(milliseconds: 200),
          child: _dirty || _isNew
              ? BottomBar(child: PrimaryButton(label: _isNew ? '添加' : '保存修改', busy: _saving, onTap: _save))
              : const SizedBox(width: double.infinity),
        ),
        body: ListView(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 40),
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          children: [
            if (provider != null)
              Row(children: [
                ProviderIcon(kind: provider.kind, size: 22),
                const SizedBox(width: 8),
                Expanded(child: Text('来自「${provider.name}」', style: TextStyle(color: p.muted, fontSize: 14))),
                if (m != null && !isDefault && c.usable(m)) TextButton.icon(onPressed: () => _makeDefault(m), icon: const Icon(Icons.star_outline_rounded, size: 18), label: const Text('设为全家默认')),
                if (isDefault) Tag('全家默认', icon: Icons.star_rounded, color: p.accent),
              ]),

            // ---- what the family sees ----
            const SectionHeader('家人看到的'),
            _field(_name, '显示名称', hint: '例如：GPT-5 Mini', maxLength: 40),
            const SizedBox(height: 12),
            _field(_desc, '一句话介绍', hint: '例如：反应快，适合日常问答', maxLength: 60),
            const SizedBox(height: 12),
            GroupCard(children: [
              SwitchRow(
                value: _enabled,
                onChanged: (v) => setState(() {
                  _enabled = v;
                  _dirty = true;
                }),
                title: '给家人使用',
                desc: '关掉后家人在模型列表里看不到它',
              ),
              SwitchRow(
                value: _vision,
                onChanged: (v) => setState(() {
                  _vision = v;
                  _dirty = true;
                }),
                title: '能看图片',
                desc: '打开后才会把照片发给它；不支持看图的模型打开会报错',
              ),
            ]),

            // ---- thinking ----
            const SectionHeader('思考设置', subtitle: '模型先想一想再回答，更准但更慢、更贵'),
            GroupCard(padding: const EdgeInsets.fromLTRB(18, 18, 18, 8), children: [
              Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                const TitleDesc('平时回答', desc: '「快速」首字最快；不能关掉思考的模型（如 GPT-5）选「少想」或「稍想一下」；老模型选「不发送」'),
                const SizedBox(height: 12),
                Choice<String>(
                  value: _dailyOptions.any((o) => o.$1 == _daily.text) ? _daily.text : '?',
                  options: _dailyOptions,
                  onChanged: (v) => setState(() => _daily.text = v),
                ),
                Padding(padding: const EdgeInsets.symmetric(vertical: 18), child: Divider(height: 1, color: p.line)),
                const TitleDesc('「深度思考」按钮', desc: '家人打开「深度思考」时用的强度；选「不支持」就不显示这个按钮'),
                const SizedBox(height: 12),
                Choice<String>(
                  value: _thinkOptions.any((o) => o.$1 == _think.text) ? _think.text : '?',
                  options: _thinkOptions,
                  onChanged: (v) => setState(() => _think.text = v),
                ),
                const SizedBox(height: 6),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    style: TextButton.styleFrom(foregroundColor: p.muted, padding: const EdgeInsets.symmetric(horizontal: 4), textStyle: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500)),
                    onPressed: () => setState(() => _advanced = !_advanced),
                    icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.tune_rounded, size: 18),
                    label: Text(_advanced ? '收起高级设置' : '高级：直接填写参数值'),
                  ),
                ),
                AnimatedSize(
                  duration: const Duration(milliseconds: 200),
                  child: _advanced
                      ? Column(children: [
                          Text('会作为 reasoning_effort 发给上游，例如 minimal、low、medium、high、none；留空表示不发送。', style: AdminText.desc(p)),
                          const SizedBox(height: 12),
                          Row(children: [
                            Expanded(child: _field(_daily, '平时', mono: true)),
                            const SizedBox(width: 10),
                            Expanded(child: _field(_think, '深度思考', mono: true)),
                          ]),
                          const SizedBox(height: 10),
                        ])
                      : const SizedBox(width: double.infinity),
                ),
              ]),
            ]),

            // ---- test ----
            if (!_isNew) ...[
              const SectionHeader('试一下', subtitle: '用现在的设置真的发一句话，确认能正常回答'),
              GroupCard(padding: const EdgeInsets.all(14), children: [
                Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  Row(children: [
                    Expanded(child: _testButton(false, '测试回答')),
                    if (_think.text.trim().isNotEmpty) ...[const SizedBox(width: 10), Expanded(child: _testButton(true, '测试深度思考'))],
                  ]),
                  AnimatedSize(
                    duration: const Duration(milliseconds: 220),
                    child: _test == null
                        ? const SizedBox(width: double.infinity)
                        : Padding(
                            padding: const EdgeInsets.only(top: 12),
                            child: ResultBanner(
                              ok: _test!.ok,
                              title: _test!.ok ? '回答正常' : '出错了',
                              lines: _test!.ok
                                  ? [
                                      '「${_test!.reply.isEmpty ? '（没有文字）' : _test!.reply}」',
                                      '首字 ${(_test!.firstTokenMs / 1000).toStringAsFixed(1)} 秒，总共 ${(_test!.durationMs / 1000).toStringAsFixed(1)} 秒'
                                          '${_test!.reasoning ? '，有思考过程' : ''}${_test!.cost > 0 ? '，花费 ${fmtUsd(_test!.cost)}' : ''}',
                                    ]
                                  : [
                                      _test!.message,
                                      if (_test!.effort.isNotEmpty) '如果提示参数不支持，试试把思考档位改成「不发送」或「稍想一下」。',
                                    ],
                            ),
                          ),
                  ),
                ]),
              ]),
            ],

            // ---- technical ----
            const SectionHeader('技术参数'),
            _field(_upstream, '上游模型 ID', hint: '例如 openai/gpt-5-mini', mono: true, helper: '必须和供应商那边的名字完全一致'),
            const SizedBox(height: 16),
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 4),
              child: TitleDesc('上下文长度', desc: '每次最多带上多少聊天记录。越大越能记住长对话，但每次回答都更贵。'),
            ),
            const SizedBox(height: 12),
            Wrap(spacing: 8, runSpacing: 8, children: [
              for (final n in _contextOptions)
                ChoiceChip(
                  label: Text(fmtContext(n)),
                  selected: _context.text == '$n',
                  showCheckmark: false,
                  selectedColor: p.accentSoft,
                  side: BorderSide(color: _context.text == '$n' ? p.accent : p.line),
                  labelStyle: TextStyle(color: _context.text == '$n' ? p.accent : p.text, fontWeight: FontWeight.w600),
                  onSelected: (_) => setState(() => _context.text = '$n'),
                ),
            ]),
            const SizedBox(height: 10),
            _field(_context, '自定义（token 数）', number: true),
            const SizedBox(height: 12),
            _field(_maxOut, '单次回答最长（token 数）', hint: '留空表示不限制', number: true),

            const SectionHeader('价格', subtitle: '美元 / 每百万 token。填了才能估算花费；OpenRouter 会直接返回实际花费'),
            Row(children: [
              Expanded(child: _field(_inPrice, '输入', hint: '0', decimal: true, prefix: '\$')),
              const SizedBox(width: 10),
              Expanded(child: _field(_outPrice, '输出', hint: '0', decimal: true, prefix: '\$')),
            ]),
          ],
        ),
      ),
    );
  }

  Widget _testButton(bool think, String label) {
    final running = _testingThink == think;
    return OutlinedButton.icon(
      style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48), shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14))),
      onPressed: _testingThink != null ? null : () => _runTest(think),
      icon: running ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)) : Icon(think ? Icons.lightbulb_outline_rounded : Icons.play_arrow_rounded),
      label: Text(running ? '等待回答…' : label),
    );
  }

  Widget _field(TextEditingController c, String label,
      {String? hint, String? helper, int? maxLength, bool mono = false, bool number = false, bool decimal = false, String? prefix}) {
    return TextField(
      controller: c,
      maxLength: maxLength,
      autocorrect: !mono,
      keyboardType: number || decimal ? TextInputType.numberWithOptions(decimal: decimal) : null,
      inputFormatters: number ? [FilteringTextInputFormatter.digitsOnly] : (decimal ? [FilteringTextInputFormatter.allow(RegExp(r'[0-9.]'))] : null),
      style: TextStyle(fontSize: 16, fontFamily: mono ? 'monospace' : null),
      decoration: InputDecoration(labelText: label, hintText: hint, helperText: helper, counterText: '', prefixText: prefix),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../state/catalog.dart';
import '../../state/session.dart';
import '../icons.dart';
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

const _kindOptions = [('chat', '聊天'), ('image', '画图'), ('embedding', '向量检索')];
// Everyday replies: what reasoning_effort to send.
const _dailyOptions = [('none', '快速'), ('minimal', '少想'), ('low', '稍想一下'), ('', '不发送')];
// Deep thinking button: '' = the model has no deep-thinking mode.
const _thinkOptions = [('', '不支持'), ('low', '低'), ('medium', '中'), ('high', '高')];
// Mainstream models start at 128K now; 1M for the long-context ones.
const _contextOptions = [128000, 200000, 256000, 400000, 1000000];

class _ModelEditPageState extends ConsumerState<ModelEditPage> {
  final _name = TextEditingController();
  final _desc = TextEditingController();
  final _routingNote = TextEditingController();
  final _upstream = TextEditingController();
  final _daily = TextEditingController();
  final _think = TextEditingController();
  final _context = TextEditingController();
  final _maxOut = TextEditingController();
  final _inPrice = TextEditingController();
  final _outPrice = TextEditingController();
  bool _vision = false, _enabled = true, _tools = true;
  String _kind = 'chat';
  bool get _image => _kind == 'image';
  bool get _chat => _kind == 'chat';
  bool get _embedding => _kind == 'embedding';
  bool _loaded = false, _dirty = false, _saving = false, _advanced = false;
  ModelTestResult? _test;
  bool? _testingThink; // null = not testing

  bool get _isNew => widget.modelId == null;

  @override
  void initState() {
    super.initState();
    for (final c in [_name, _desc, _routingNote, _upstream, _daily, _think, _context, _maxOut, _inPrice, _outPrice]) {
      c.addListener(_markDirty);
    }
  }

  void _markDirty() {
    if (_loaded && !_dirty) setState(() => _dirty = true);
  }

  void _load(AdminModel? m) {
    _name.text = m?.displayName ?? '';
    _desc.text = m?.description ?? '';
    _routingNote.text = m?.routingNote ?? '';
    _upstream.text = m?.upstreamModel ?? '';
    _daily.text = m?.defaultEffort ?? '';
    _think.text = m?.thinkEffort ?? '';
    _context.text = '${m?.contextWindow ?? 128000}';
    _maxOut.text = (m?.maxOutput ?? 0) == 0 ? '' : '${m!.maxOutput}';
    _inPrice.text = _priceText(m?.inputPrice ?? 0);
    _outPrice.text = _priceText(m?.outputPrice ?? 0);
    _vision = m?.vision ?? false;
    _enabled = m?.enabled ?? true;
    _tools = m?.tools ?? true;
    _kind = m?.kind ?? 'chat';
    _advanced = m != null && (!_dailyOptions.any((o) => o.$1 == m.defaultEffort) || !_thinkOptions.any((o) => o.$1 == m.thinkEffort));
    _loaded = true;
  }

  static String _priceText(double v) => v == 0 ? '' : v.toString();

  @override
  void dispose() {
    for (final c in [_name, _desc, _routingNote, _upstream, _daily, _think, _context, _maxOut, _inPrice, _outPrice]) {
      c.dispose();
    }
    super.dispose();
  }

  Map<String, dynamic> _body() => {
        'display_name': _name.text.trim(),
        'description': _desc.text.trim(),
        'routing_note': _chat ? _routingNote.text.trim() : '',
        'upstream_model': _upstream.text.trim(),
        'default_effort': _chat ? _daily.text.trim() : '',
        'think_effort': _chat ? _think.text.trim() : '',
        'context_window': _embedding ? 0 : (int.tryParse(_context.text.trim()) ?? 128000),
        'max_output': _chat ? (int.tryParse(_maxOut.text.trim()) ?? 0) : 0,
        'input_price': double.tryParse(_inPrice.text.trim()) ?? 0,
        'output_price': double.tryParse(_outPrice.text.trim()) ?? 0,
        'vision': _chat && _vision,
        'tools': _chat && _tools,
        'enabled': _enabled,
        'kind': _kind,
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
      final m = _isNew ? await api.adminCreateModel({..._body(), 'provider_id': widget.providerId}) : await api.adminUpdateModel(widget.modelId!, _body());
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
      final r = await ref.read(apiProvider).adminTestModel(widget.modelId!, think: think, image: _image);
      if (mounted) setState(() => _test = r);
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _testingThink = null);
    }
  }

  Future<void> _delete(Catalog c, AdminModel m) async {
    final isDefault = c.settings.defaultModelId == m.id;
    final ok = await confirm(
      context,
      title: '删除「${m.displayName}」？',
      body: isDefault ? '它是全家默认模型，删除后会自动换成其他可用的模型。聊天记录不受影响。' : '家人将不能再选择它，聊天记录不受影响。',
      ok: '删除',
      danger: true,
    );
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
      return Scaffold(
        appBar: AppBar(),
        body: catalog.hasError ? ErrorView(message: ApiError.from(catalog.error!).message) : const LoadingView(),
      );
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
            if (m != null)
              IconButton(
                tooltip: '删除',
                icon: Icon(AppIcons.delete, color: p.bad),
                onPressed: () => _delete(c, m),
              ),
          ],
        ),
        bottomNavigationBar: AnimatedSize(
          duration: const Duration(milliseconds: 200),
          child: _dirty || _isNew
              ? BottomBar(
                  child: PrimaryButton(label: _isNew ? '添加模型' : '保存修改', busy: _saving, onTap: _save),
                )
              : const SizedBox(width: double.infinity),
        ),
        body: ListView(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 44),
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          children: [
            if (provider != null)
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
                decoration: BoxDecoration(
                  color: p.surface,
                  borderRadius: BorderRadius.circular(16),
                  border: Border.all(color: p.line),
                ),
                child: Row(
                  children: [
                    ProviderIcon(kind: provider.kind, size: 26),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text('来自「${provider.name}」', style: TextStyle(color: p.muted, fontSize: 14)),
                    ),
                    if (m != null && !isDefault && c.usable(m))
                      SectionAction(
                        label: '设为全家默认',
                        icon: AppIcons.star,
                        filled: true,
                        onTap: () => _makeDefault(m),
                      ),
                    if (isDefault) Tag('全家默认', icon: AppIcons.starActive, color: p.accent),
                  ],
                ),
              ),

            const SectionHeader('模型类型'),
            GroupCard(
              padding: const EdgeInsets.all(14),
              children: [
                Choice<String>(
                  value: _kind,
                  options: _kindOptions,
                  enabled: !_saving && _testingThink == null,
                  onChanged: (v) => setState(() {
                    _kind = v;
                    if (!_embedding && (int.tryParse(_context.text) ?? 0) < 1024) _context.text = '128000';
                    _dirty = true;
                    _test = null;
                  }),
                ),
              ],
            ),

            // ---- what the family sees ----
            SectionHeader(_chat ? '家人看到的' : '基本信息'),
            GroupCard(
              children: [
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                    AdminField(
                      controller: _name,
                      label: '显示名称',
                      hint: _embedding ? '例如：BGE M3' : (_image ? '例如：GPT Image' : '例如：GPT-5 Mini'),
                      maxLength: 40,
                    ),
                    if (_chat) ...[
                      const SizedBox(height: 14),
                      AdminField(
                        controller: _desc,
                        label: '一句话介绍',
                        statusText: '选填',
                        hint: '例如：反应快，适合日常问答',
                        maxLength: 60,
                      ),
                    ],
                  ]),
                ),
                SwitchRow(
                  icon: AppIcons.eye,
                  value: _enabled,
                  onChanged: (v) => setState(() {
                    _enabled = v;
                    _dirty = true;
                  }),
                  title: _chat ? '给家人使用' : '启用',
                  desc: _embedding ? '供记忆检索使用，不出现在家人的模型列表里' : (_image ? '关掉后不会再用它画图' : '关掉后家人在模型列表里看不到它'),
                ),
                if (_chat) ...[
                  SwitchRow(
                    icon: AppIcons.image,
                    value: _vision,
                    onChanged: (v) => setState(() {
                      _vision = v;
                      _dirty = true;
                    }),
                    title: '能看图片',
                    desc: '打开后才会把照片发给它；不支持看图的模型打开会报错',
                  ),
                  SwitchRow(
                    icon: AppIcons.magicWand,
                    value: _tools,
                    onChanged: (v) => setState(() {
                      _tools = v;
                      _dirty = true;
                    }),
                    title: '会用工具',
                    desc: '家人说「帮我画…」时由它自己调用画图模型。若回答常报错可关掉试试',
                  ),
                ],
              ],
            ),

            // ---- Auto routing scenario guide ----
            if (_chat) ...[
              const SectionHeader(
                'Auto 选模型参考',
                subtitle: '介绍它最适合解决什么场景的问题；不填时由服务端内置的御三家与开源模型知识库兜底',
              ),
              GroupCard(
                padding: const EdgeInsets.all(16),
                children: [
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      AdminField(
                        controller: _routingNote,
                        label: '适合什么场景（给 Auto 选模型看）',
                        statusText: '选填 · 最高优先级',
                        statusColor: p.accent,
                        hint: '例如：日常闲聊、孩子数学题；或：医疗问题和深度研究',
                        maxLength: 300,
                        minLines: 2,
                        maxLines: 3,
                      ),
                      const SizedBox(height: 12),
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: p.surfaceAlt,
                          borderRadius: BorderRadius.circular(12),
                        ),
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Padding(
                              padding: const EdgeInsets.only(top: 1.5),
                              child: Icon(AppIcons.sparkles, size: 15, color: p.accent),
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                (m?.builtinRoutingNote ?? '').isNotEmpty
                                    ? '服务端内置兜底：${m!.builtinRoutingNote}'
                                    : '保存后服务端会自动根据模型家族与版本号（如 Opus > Sonnet、高版本优于低版本）生成兜底认知。',
                                style: TextStyle(fontSize: 13, color: p.muted, height: 1.45),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ],

            // ---- thinking ----
            if (_chat) ...[
              const SectionHeader('思考设置', subtitle: '模型先想一想再回答，更准但更慢、更贵'),
              GroupCard(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 10),
                children: [
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const TitleDesc('平时回答', desc: '「快速」首字最快；不能关掉思考的模型（如 GPT-5）选「少想」或「稍想一下」；老模型选「不发送」'),
                      const SizedBox(height: 12),
                      Choice<String>(
                        value: _dailyOptions.any((o) => o.$1 == _daily.text) ? _daily.text : '?',
                        options: _dailyOptions,
                        onChanged: (v) => setState(() => _daily.text = v),
                      ),
                      Padding(
                        padding: const EdgeInsets.symmetric(vertical: 16),
                        child: Divider(height: 1, thickness: 0.7, color: p.line.withValues(alpha: 0.75)),
                      ),
                      const TitleDesc('「深度思考」按钮', desc: '家人打开「深度思考」时用的强度；选「不支持」就不显示这个按钮'),
                      const SizedBox(height: 12),
                      Choice<String>(
                        value: _thinkOptions.any((o) => o.$1 == _think.text) ? _think.text : '?',
                        options: _thinkOptions,
                        onChanged: (v) => setState(() => _think.text = v),
                      ),
                      const SizedBox(height: 10),
                      Padding(
                        padding: const EdgeInsets.only(bottom: 4),
                        child: SectionAction(
                          label: _advanced ? '收起高级设置' : '高级：直接填写参数值',
                          icon: _advanced ? AppIcons.chevronUp : AppIcons.tune,
                          onTap: () => setState(() => _advanced = !_advanced),
                        ),
                      ),
                      AnimatedSize(
                        duration: const Duration(milliseconds: 200),
                        child: _advanced
                            ? Padding(
                                padding: const EdgeInsets.only(top: 8, bottom: 6),
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text('会作为 reasoning_effort 发给上游，例如 minimal、low、medium、high、none；留空表示不发送。', style: AdminText.desc(p)),
                                    const SizedBox(height: 12),
                                    Row(
                                      children: [
                                        Expanded(child: AdminField(controller: _daily, label: '平时', mono: true)),
                                        const SizedBox(width: 10),
                                        Expanded(child: AdminField(controller: _think, label: '深度思考', mono: true)),
                                      ],
                                    ),
                                  ],
                                ),
                              )
                            : const SizedBox(width: double.infinity),
                      ),
                    ],
                  ),
                ],
              ),
            ],

            // ---- test ----
            if (!_isNew) ...[
              SectionHeader(
                '试一下',
                subtitle: _embedding
                    ? '发送一小段文字，确认能用于记忆检索'
                    : (_image ? '真的画一张小图，确认能正常出图（约半分钟，会产生少量费用）' : '用现在的设置发一句话，确认能正常回答'),
              ),
              GroupCard(
                padding: const EdgeInsets.all(14),
                children: [
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Row(
                        children: [
                          Expanded(child: _testButton(false, _embedding ? '测试记忆检索' : (_image ? '画一张试试' : '测试回答'))),
                          if (_chat && _think.text.trim().isNotEmpty) ...[
                            const SizedBox(width: 10),
                            Expanded(child: _testButton(true, '测试深度思考')),
                          ],
                        ],
                      ),
                      AnimatedSize(
                        duration: const Duration(milliseconds: 220),
                        child: _test == null
                            ? const SizedBox(width: double.infinity)
                            : Padding(
                                padding: const EdgeInsets.only(top: 12),
                                child: _image
                                    ? _imageResult(_test!)
                                    : ResultBanner(
                                        ok: _test!.ok,
                                        title: _test!.ok ? (_embedding ? '记忆检索正常' : '回答正常') : '出错了',
                                        lines: _test!.ok
                                            ? [
                                                '「${_test!.reply.isEmpty ? '（没有文字）' : _test!.reply}」',
                                                if (_chat)
                                                  '首字 ${(_test!.firstTokenMs / 1000).toStringAsFixed(1)} 秒，总共 ${(_test!.durationMs / 1000).toStringAsFixed(1)} 秒'
                                                      '${_test!.reasoning ? '，有思考过程' : ''}${_test!.cost > 0 ? '，花费 ${fmtUsd(_test!.cost)}' : ''}',
                                              ]
                                            : [_test!.message, if (_chat && _test!.effort.isNotEmpty) '如果提示参数不支持，试试把思考档位改成「不发送」或「稍想一下」。'],
                                      ),
                              ),
                      ),
                    ],
                  ),
                ],
              ),
            ],

            // ---- technical ----
            const SectionHeader('技术参数'),
            GroupCard(
              padding: const EdgeInsets.all(16),
              children: [
                Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  AdminField(
                    controller: _upstream,
                    label: '上游模型 ID',
                    hint: '例如 openai/gpt-5-mini',
                    mono: true,
                    helper: '必须和供应商那边的名字完全一致',
                  ),
                  if (_chat) ...[
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                      child: Divider(height: 1, thickness: 0.7, color: p.line.withValues(alpha: 0.75)),
                    ),
                    const TitleDesc('上下文长度', desc: '每次最多带上多少聊天记录。越大越能记住长对话，但每次回答更贵。'),
                    const SizedBox(height: 12),
                    ListenableBuilder(
                      listenable: _context,
                      builder: (_, _) => Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          for (final n in _contextOptions)
                            Pressable(
                              scale: 0.96,
                              onTap: () => setState(() => _context.text = '$n'),
                              child: AnimatedContainer(
                                duration: const Duration(milliseconds: 160),
                                padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 7),
                                decoration: BoxDecoration(
                                  color: _context.text == '$n' ? p.accentSoft : p.surfaceAlt,
                                  borderRadius: BorderRadius.circular(11),
                                  border: Border.all(
                                    color: _context.text == '$n' ? p.accent.withValues(alpha: 0.55) : Colors.transparent,
                                  ),
                                ),
                                child: Text(
                                  fmtContext(n),
                                  style: TextStyle(
                                    fontSize: 13.5,
                                    fontWeight: _context.text == '$n' ? FontWeight.w600 : FontWeight.w500,
                                    color: _context.text == '$n' ? p.accent : p.muted,
                                  ),
                                ),
                              ),
                            ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 12),
                    AdminField(controller: _context, label: '自定义上下文（token 数）', number: true, mono: true),
                    const SizedBox(height: 14),
                    AdminField(controller: _maxOut, label: '单次回答最长（token 数）', statusText: '选填', hint: '留空表示不限制', number: true, mono: true),
                  ],
                ]),
              ],
            ),

            SectionHeader(
              '价格',
              subtitle: _image ? '美元 / 每百万 token（画图接口会返回 token 数），用于估算花费' : '美元 / 每百万 token。填了才能估算花费；OpenRouter 会直接返回实际花费',
            ),
            GroupCard(
              padding: const EdgeInsets.all(16),
              children: [
                Row(
                  children: [
                    Expanded(
                      child: AdminField(controller: _inPrice, label: '输入价格', hint: '0', decimal: true, mono: true, prefixText: '\$ '),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: AdminField(controller: _outPrice, label: '输出价格', hint: '0', decimal: true, mono: true, prefixText: '\$ '),
                    ),
                  ],
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _imageResult(ModelTestResult t) {
    if (!t.ok) return ResultBanner(ok: false, title: '没画出来', lines: [t.message]);
    final api = ref.read(apiProvider);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ResultBanner(
          ok: true,
          title: '画图正常',
          lines: [
            '用时 ${(t.durationMs / 1000).toStringAsFixed(0)} 秒${t.width > 0 ? '，${t.width}×${t.height}' : ''}${t.cost > 0 ? '，花费 ${fmtUsd(t.cost)}' : ''}',
          ],
        ),
        if (t.fileId.isNotEmpty) ...[
          const SizedBox(height: 10),
          ClipRRect(
            borderRadius: BorderRadius.circular(16),
            child: AspectRatio(
              aspectRatio: t.width > 0 && t.height > 0 ? t.width / t.height : 1,
              child: Image.network(api.fileUrl(t.fileId), headers: api.authHeaders, fit: BoxFit.cover, cacheWidth: 800),
            ),
          ),
        ],
      ],
    );
  }

  Widget _testButton(bool think, String label) {
    final running = _testingThink == think;
    return SecondaryButton(
      label: running ? (_embedding ? '正在测试…' : (_image ? '正在画…' : '等待回答…')) : label,
      icon: think ? AppIcons.thinkOff : AppIcons.play,
      busy: running,
      onTap: _testingThink != null ? null : () => _runTest(think),
    );
  }
}

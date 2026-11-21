import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/admin_models.dart';
import '../../../state/catalog.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import 'admin_common.dart';
import 'model_edit_page.dart';
import 'models_page.dart' show ModelRow;
import 'upstream_picker_page.dart';

const _addRoute = '/add-provider';

/// Starts the add-provider flow. When it finishes the user lands on the new
/// provider's page with the model picker on top.
void openAddProvider(BuildContext context) => Navigator.of(context).push(addProviderRoute());

Route<void> addProviderRoute() => appRoute((_) => const PresetPickerPage(), settings: const RouteSettings(name: _addRoute));

// ---------------------------------------------------------------------------
// Step 1: choose what kind of provider.

class PresetPickerPage extends ConsumerWidget {
  const PresetPickerPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final presets = ref.watch(presetsProvider);
    final p = context.pal;
    return Scaffold(
      appBar: AppBar(title: const Text('添加供应商')),
      body: presets.when(
        loading: () => const LoadingView(),
        error: (e, _) => ErrorView(message: ApiError.from(e).message, onRetry: () => ref.invalidate(presetsProvider)),
        data: (list) => ListView(
          padding: const EdgeInsets.fromLTRB(16, 6, 16, 36),
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(8, 0, 8, 14),
              child: Text(
                '选择你手上有 API Key 的服务。不知道选哪个？OpenRouter 一个 Key 就能用主流模型，按用量付费。',
                style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.55),
              ),
            ),
            for (final (i, pr) in list.indexed)
              Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: Pressable(
                  scale: 0.985,
                  onTap: () => Navigator.of(context).push(appRoute((_) => ProviderFormPage(preset: pr))),
                  child: Container(
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(
                      color: p.surface,
                      borderRadius: BorderRadius.circular(20),
                      border: Border.all(color: i == 0 ? p.accent.withValues(alpha: 0.38) : p.line),
                    ),
                    child: Row(children: [
                      ProviderIcon(kind: pr.kind, size: 46),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                          Row(children: [
                            Flexible(
                              child: Text(
                                pr.name,
                                style: TextStyle(fontSize: 16.5, fontWeight: i == 0 ? FontWeight.w600 : FontWeight.w500, color: p.text),
                              ),
                            ),
                            if (i == 0) ...[const SizedBox(width: 8), Tag('推荐', color: p.accent)],
                          ]),
                          const SizedBox(height: 4),
                          Text(pr.description, style: AdminText.desc(p)),
                        ]),
                      ),
                      const SizedBox(width: 6),
                      AppIcon(AppIcons.chevronRight, color: p.faint, size: 18),
                    ]),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Step 2: key (and address for custom), test, save, then pick models.

class ProviderFormPage extends ConsumerStatefulWidget {
  const ProviderFormPage({super.key, required this.preset});
  final ProviderPreset preset;

  @override
  ConsumerState<ProviderFormPage> createState() => _ProviderFormPageState();
}

class _ProviderFormPageState extends ConsumerState<ProviderFormPage> {
  late final _name = TextEditingController(text: widget.preset.isCustom ? '' : widget.preset.name);
  late final _url = TextEditingController(text: widget.preset.baseUrl);
  final _key = TextEditingController();
  bool _hideKey = true, _busy = false, _showUrl = false;
  ProviderTestResult? _result;
  String? _error;

  ProviderPreset get pr => widget.preset;

  @override
  void initState() {
    super.initState();
    _showUrl = pr.isCustom;
    for (final c in [_name, _url, _key]) {
      c.addListener(() {
        if (_result != null || _error != null) {
          setState(() {
            _result = null;
            _error = null;
          });
        } else if (c == _url) {
          setState(() {});
        }
      });
    }
  }

  @override
  void dispose() {
    _name.dispose();
    _url.dispose();
    _key.dispose();
    super.dispose();
  }

  Map<String, dynamic> get _body => {'kind': pr.kind, 'name': _name.text.trim(), 'base_url': _url.text.trim(), 'api_key': _key.text.trim()};

  String? _validate() {
    if (_name.text.trim().isEmpty) return '请填写名称';
    if (endpointPreview(_url.text).isEmpty) return '请填写正确的接口地址（以 http:// 或 https:// 开头）';
    if (_key.text.trim().isEmpty) return '请粘贴 API Key';
    return null;
  }

  Future<void> _submit({bool skipTest = false}) async {
    FocusScope.of(context).unfocus();
    final err = _validate();
    setState(() => _error = err);
    if (err != null) return;
    final api = ref.read(apiProvider);
    setState(() => _busy = true);
    try {
      if (!skipTest) {
        final r = await api.adminTestProvider(_body);
        if (!mounted) return;
        setState(() => _result = r);
        if (!r.ok) return;
      }
      final created = await api.adminCreateProvider(_body);
      await ref.read(catalogProvider.notifier).reload();
      if (!mounted) return;
      HapticFeedback.mediumImpact();
      final nav = Navigator.of(context);
      nav.popUntil((r) => r.settings.name == _addRoute || r.isFirst);
      if (nav.canPop()) nav.pop();
      nav.push(appRoute((_) => ProviderDetailPage(providerId: created.id)));
      nav.push(appRoute((_) => UpstreamPickerPage(providerId: created.id, firstTime: true)));
    } on ApiError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _paste() async {
    final d = await Clipboard.getData(Clipboard.kTextPlain);
    final t = d?.text?.trim() ?? '';
    if (t.isEmpty) {
      if (mounted) showToast(context, '剪贴板是空的');
      return;
    }
    _key.text = t;
  }

  @override
  Widget build(BuildContext context) {
    ref.watch(catalogProvider);
    final p = context.pal;
    final preview = endpointPreview(_url.text);
    final failed = _result != null && !_result!.ok;
    return Scaffold(
      appBar: AppBar(title: Text(pr.isCustom ? '自定义供应商' : pr.name)),
      bottomNavigationBar: BottomBar(
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          PrimaryButton(label: failed ? '重新测试' : '测试并保存', busy: _busy, onTap: _submit),
          if (failed)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: TextButton(
                onPressed: _busy ? null : () => _submit(skipTest: true),
                child: Text('先保存，稍后再处理', style: TextStyle(color: p.muted, fontSize: 14)),
              ),
            ),
        ]),
      ),
      body: ListView(
        keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: [
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: p.surface,
              borderRadius: BorderRadius.circular(20),
              border: Border.all(color: p.line),
            ),
            child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
              ProviderIcon(kind: pr.kind, size: 44),
              const SizedBox(width: 14),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(pr.description, style: TextStyle(fontSize: 14, height: 1.5, color: p.muted)),
                  if (pr.keyUrl.isNotEmpty) ...[
                    const SizedBox(height: 10),
                    SectionAction(
                      label: '去官网创建 API Key',
                      icon: AppIcons.externalLink,
                      filled: true,
                      onTap: () => openLink(context, pr.keyUrl),
                    ),
                  ],
                ]),
              ),
            ]),
          ),
          const SectionHeader('连接信息'),
          GroupCard(
            padding: const EdgeInsets.all(16),
            children: [
              Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                AdminField(
                  controller: _key,
                  label: 'API Key',
                  statusText: '只保存在你的服务器上',
                  hint: pr.keyPrefix.isEmpty ? '粘贴 API Key' : '${pr.keyPrefix}…',
                  obscureText: _hideKey,
                  mono: true,
                  suffix: Row(mainAxisSize: MainAxisSize.min, children: [
                    Pressable(
                      onTap: () => setState(() => _hideKey = !_hideKey),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
                        child: AppIcon(_hideKey ? AppIcons.eye : AppIcons.eyeOff, size: 19, color: p.muted),
                      ),
                    ),
                    const SizedBox(width: 4),
                    FieldActionPill(label: '粘贴', onTap: _paste),
                  ]),
                ),
                const SizedBox(height: 14),
                AdminField(
                  controller: _name,
                  label: '显示名称',
                  hint: '例如：我的中转站',
                  maxLength: 30,
                ),
                if (!pr.isCustom && !_showUrl) ...[
                  const SizedBox(height: 12),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: SectionAction(
                      label: '修改接口地址（一般不需要）',
                      icon: AppIcons.tune,
                      onTap: () => setState(() => _showUrl = true),
                    ),
                  ),
                ],
                if (_showUrl) ...[
                  const SizedBox(height: 14),
                  AdminField(
                    controller: _url,
                    label: '接口地址',
                    hint: 'https://example.com/v1',
                    mono: true,
                    keyboardType: TextInputType.url,
                    helper: preview.isEmpty ? '填写服务商文档里的 Base URL。只填域名时会自动加上 /v1。' : '实际请求：$preview',
                  ),
                ],
              ]),
            ],
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 220),
            alignment: Alignment.topCenter,
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              if (_error != null) Padding(padding: const EdgeInsets.only(top: 16), child: ResultBanner(ok: false, title: _error!)),
              if (_result != null)
                Padding(
                  padding: const EdgeInsets.only(top: 16),
                  child: ResultBanner(ok: _result!.ok, title: _result!.ok ? '连接成功' : '连接失败', lines: providerTestLines(_result!)),
                ),
            ]),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Provider detail: connection, models, enable / delete.

class ProviderDetailPage extends ConsumerStatefulWidget {
  const ProviderDetailPage({super.key, required this.providerId});
  final String providerId;

  @override
  ConsumerState<ProviderDetailPage> createState() => _ProviderDetailPageState();
}

class _ProviderDetailPageState extends ConsumerState<ProviderDetailPage> {
  ProviderTestResult? _test;
  bool _testing = false;

  Future<void> _update(Map<String, dynamic> body, {String done = '已保存'}) async {
    try {
      await ref.read(apiProvider).adminUpdateProvider(widget.providerId, body);
      await ref.read(catalogProvider.notifier).reload();
      if (mounted) {
        setState(() => _test = null);
        showToast(context, done);
      }
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    }
  }

  Future<void> _runTest() async {
    setState(() {
      _testing = true;
      _test = null;
    });
    try {
      final r = await ref.read(apiProvider).adminTestProvider({'id': widget.providerId});
      if (mounted) setState(() => _test = r);
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _editName(AdminProvider pr) async {
    final v = await showTextEditor(context, title: '供应商名称', initial: pr.name, ok: '保存', maxLines: 1);
    if (v != null && v.isNotEmpty && v != pr.name) await _update({'name': v});
  }

  Future<void> _editKey(AdminProvider pr) async {
    final v = await _showSecretEditor(context, title: '更换 API Key', hint: '粘贴新的 API Key');
    if (v != null && v.isNotEmpty) await _update({'api_key': v}, done: 'Key 已更换');
  }

  Future<void> _editUrl(AdminProvider pr) async {
    final r = await showModalBottomSheet<(String, String)>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => _UrlEditor(initial: pr.baseUrl),
    );
    if (r == null) return;
    final (url, key) = r;
    if (url == pr.baseUrl && key.isEmpty) return;
    await _update({'base_url': url, if (key.isNotEmpty) 'api_key': key});
  }

  Future<void> _delete(Catalog c, AdminProvider pr) async {
    final n = c.modelsOf(pr.id).length;
    final hasDefault = c.modelsOf(pr.id).any((m) => m.id == c.settings.defaultModelId);
    final ok = await confirm(
      context,
      title: '删除「${pr.name}」？',
      body: [
        if (n > 0) '它下面的 $n 个模型会一起删除。',
        if (hasDefault) '全家默认模型在这里，删除后会自动换成其他可用的模型。',
        '聊天记录不受影响。',
      ].join('\n'),
      ok: '删除',
      danger: true,
    );
    if (!ok) return;
    try {
      final s = await ref.read(apiProvider).adminDeleteProvider(pr.id);
      final c2 = ref.read(catalogProvider).value;
      await ref.read(catalogProvider.notifier).reload();
      if (!mounted) return;
      Navigator.of(context).pop();
      final def = c2?.model(s.defaultModelId)?.displayName;
      showToast(context, hasDefault && def != null ? '已删除，全家默认模型换成了「$def」' : '已删除');
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final catalog = ref.watch(catalogProvider);
    final c = catalog.value;
    final pr = c?.provider(widget.providerId);
    if (c == null || pr == null) {
      return Scaffold(
        appBar: AppBar(),
        body: catalog.hasError ? ErrorView(message: ApiError.from(catalog.error!).message, onRetry: () => ref.invalidate(catalogProvider)) : const LoadingView(),
      );
    }
    final models = c.modelsOf(pr.id);
    return Scaffold(
      appBar: AppBar(title: Text(pr.name)),
      body: RefreshIndicator(
        onRefresh: () => ref.read(catalogProvider.notifier).reload(),
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 44),
          children: [
            // ---- header ----
            Container(
              padding: const EdgeInsets.fromLTRB(18, 16, 14, 16),
              decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(22), border: Border.all(color: p.line)),
              child: Row(children: [
                ProviderIcon(kind: pr.kind, size: 52, muted: !pr.enabled),
                const SizedBox(width: 15),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(pr.name, style: TextStyle(fontSize: 19, fontWeight: FontWeight.w600, color: p.text)),
                    const SizedBox(height: 4),
                    Text(
                      pr.enabled ? '已启用 · ${models.where((m) => m.enabled && m.isChat).length} 个对话模型给家人用' : '已停用 · 家人看不到它的模型',
                      style: TextStyle(fontSize: 13.5, color: pr.enabled ? p.muted : p.bad),
                    ),
                  ]),
                ),
                Transform.scale(
                  scale: 0.9,
                  child: Switch(value: pr.enabled, onChanged: (v) => _update({'enabled': v}, done: v ? '已启用' : '已停用')),
                ),
              ]),
            ),

            // ---- connection ----
            const SectionHeader('连接'),
            GroupCard(children: [
              InfoRow(icon: AppIcons.badge, title: '名称', value: pr.name, onTap: () => _editName(pr)),
              InfoRow(icon: AppIcons.link, title: '接口地址', subtitle: pr.endpoint, onTap: () => _editUrl(pr)),
              InfoRow(icon: AppIcons.key, title: 'API Key', value: pr.apiKeyHint, onTap: () => _editKey(pr)),
              if (pr.keyUrl.isNotEmpty)
                InfoRow(
                  icon: AppIcons.wallet,
                  title: '去官网查看余额、管理 Key',
                  trailing: AppIcon(AppIcons.externalLink, size: 18, color: p.faint),
                  onTap: () => openLink(context, pr.keyUrl),
                ),
              Padding(
                padding: const EdgeInsets.all(14),
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  SecondaryButton(
                    label: _testing ? '正在测试…' : '测试连接',
                    icon: AppIcons.connectionTest,
                    busy: _testing,
                    onTap: _runTest,
                  ),
                  if (_test != null) ...[
                    const SizedBox(height: 12),
                    ResultBanner(ok: _test!.ok, title: _test!.ok ? '连接正常' : '连接失败', lines: providerTestLines(_test!)),
                  ],
                ]),
              ),
            ]),

            // ---- models ----
            SectionHeader(
              '模型（${models.length}）',
              subtitle: models.isEmpty ? '从列表里挑几个给家人用' : '点开可以改名字、思考设置和价格',
            ),
            for (final m in models)
              Padding(padding: const EdgeInsets.only(bottom: 10), child: ModelRow(model: m, catalog: c, showProvider: false)),
            const SizedBox(height: 4),
            Row(children: [
              Expanded(
                child: PrimaryButton(
                  label: '从列表添加',
                  icon: AppIcons.listPlus,
                  onTap: () => Navigator.of(context).push(appRoute((_) => UpstreamPickerPage(providerId: pr.id))),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: SecondaryButton(
                  label: '手动添加',
                  icon: AppIcons.editNote,
                  onTap: () => Navigator.of(context).push(appRoute((_) => ModelEditPage(providerId: pr.id))),
                ),
              ),
            ]),

            // ---- danger (same calm style as SettingsPage logout) ----
            const SizedBox(height: 28),
            Pressable(
              scale: 0.98,
              onTap: () => _delete(c, pr),
              child: Container(
                height: 52,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: p.surface,
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: p.line),
                ),
                child: Text('删除这个供应商', style: TextStyle(color: p.bad, fontSize: 16, fontWeight: FontWeight.w500)),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Address editor. Moving to another host requires the key again (server enforces it too).
class _UrlEditor extends StatefulWidget {
  const _UrlEditor({required this.initial});
  final String initial;

  @override
  State<_UrlEditor> createState() => _UrlEditorState();
}

class _UrlEditorState extends State<_UrlEditor> {
  late final _url = TextEditingController(text: widget.initial);
  final _key = TextEditingController();

  @override
  void dispose() {
    _url.dispose();
    _key.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return ListenableBuilder(
      listenable: Listenable.merge([_url, _key]),
      builder: (_, _) {
        final preview = endpointPreview(_url.text);
        final hostChanged = hostOf(_url.text) != hostOf(widget.initial);
        final valid = preview.isNotEmpty && (!hostChanged || _key.text.trim().isNotEmpty);
        return Padding(
          padding: EdgeInsets.fromLTRB(20, 0, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Text('接口地址', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 14),
            AdminField(
              controller: _url,
              label: 'Base URL',
              mono: true,
              keyboardType: TextInputType.url,
              helper: preview.isEmpty ? '地址需要以 http:// 或 https:// 开头' : '实际请求：$preview',
            ),
            AnimatedSize(
              duration: const Duration(milliseconds: 200),
              child: hostChanged
                  ? Padding(
                      padding: const EdgeInsets.only(top: 14),
                      child: AdminField(
                        controller: _key,
                        label: 'API Key',
                        statusText: '换域名需重新填写',
                        statusColor: p.accent,
                        hint: '粘贴 API Key',
                        obscureText: true,
                        mono: true,
                      ),
                    )
                  : const SizedBox(width: double.infinity),
            ),
            const SizedBox(height: 18),
            PrimaryButton(
              label: '保存',
              onTap: valid ? () => Navigator.pop(context, (_url.text.trim(), _key.text.trim())) : null,
            ),
          ]),
        );
      },
    );
  }
}

Future<String?> _showSecretEditor(BuildContext context, {required String title, String? hint}) {
  final ctrl = TextEditingController();
  return showModalBottomSheet<String>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) => Padding(
      padding: EdgeInsets.fromLTRB(20, 0, 20, MediaQuery.viewInsetsOf(ctx).bottom + 20),
      child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text(title, style: Theme.of(ctx).textTheme.titleMedium),
        const SizedBox(height: 14),
        AdminField(
          controller: ctrl,
          label: '新的密钥',
          hint: hint,
          obscureText: true,
          mono: true,
          suffix: FieldActionPill(
            label: '粘贴',
            onTap: () async => ctrl.text = (await Clipboard.getData(Clipboard.kTextPlain))?.text?.trim() ?? '',
          ),
        ),
        const SizedBox(height: 18),
        PrimaryButton(label: '保存', onTap: () => Navigator.pop(ctx, ctrl.text.trim())),
      ]),
    ),
  );
}

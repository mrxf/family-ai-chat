import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/admin_models.dart';
import '../../../core/api.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import 'admin_common.dart';

final _searchStateProvider = FutureProvider.autoDispose<AdminSearchState>((ref) => ref.watch(apiProvider).adminSearch());

class WebSearchAdminPage extends ConsumerStatefulWidget {
  const WebSearchAdminPage({super.key});

  @override
  ConsumerState<WebSearchAdminPage> createState() => _WebSearchAdminPageState();
}

class _WebSearchAdminPageState extends ConsumerState<WebSearchAdminPage> {
  String _selectedKind = 'deepseek';
  bool _initialized = false;

  final _apiKey = TextEditingController();
  final _baseUrl = TextEditingController();

  bool _hideKey = true;
  bool _testing = false;
  bool _saving = false;
  SearchTestResult? _testResult;
  String? _error;

  @override
  void initState() {
    super.initState();
    for (final c in [_apiKey, _baseUrl]) {
      c.addListener(() {
        if (_testResult != null || _error != null) {
          setState(() {
            _testResult = null;
            _error = null;
          });
        }
      });
    }
  }

  @override
  void dispose() {
    _apiKey.dispose();
    _baseUrl.dispose();
    super.dispose();
  }

  void _loadProviderFields(AdminSearchProvider p) {
    _apiKey.clear();
    _baseUrl.text = p.baseUrl.isNotEmpty ? p.baseUrl : p.defaultUrl;
    _testResult = null;
    _error = null;
  }

  void _selectProvider(AdminSearchState state, String kind) {
    final pr = state.byKind(kind);
    if (pr == null) return;
    if (!pr.available) {
      showToast(context, '该搜索接口已预留，将在后续版本开放');
      return;
    }
    setState(() {
      _selectedKind = kind;
      _loadProviderFields(pr);
    });
  }

  Map<String, dynamic> _configBody({bool reuseProviderKey = false}) => {
        'kind': _selectedKind,
        'api_key': _apiKey.text.trim(),
        'base_url': _baseUrl.text.trim(),
        if (reuseProviderKey) 'reuse_provider_key': true,
      };

  String? _validate(AdminSearchProvider pr, {bool reuseProviderKey = false}) {
    final u = _baseUrl.text.trim();
    if (u.isNotEmpty && !u.startsWith('http://') && !u.startsWith('https://')) {
      return '接口地址需要以 http:// 或 https:// 开头';
    }
    if (!reuseProviderKey && _apiKey.text.trim().isEmpty && !pr.hasKey) {
      return '请填写 API Key';
    }
    return null;
  }

  Future<void> _pasteInto(TextEditingController ctrl) async {
    final d = await Clipboard.getData(Clipboard.kTextPlain);
    final t = d?.text?.trim() ?? '';
    if (t.isEmpty) {
      if (mounted) showToast(context, '剪贴板是空的');
      return;
    }
    ctrl.text = t;
  }

  Future<void> _runTest(AdminSearchProvider pr, {bool reuseProviderKey = false}) async {
    FocusScope.of(context).unfocus();
    final err = _validate(pr, reuseProviderKey: reuseProviderKey);
    if (err != null) {
      setState(() => _error = err);
      return;
    }
    setState(() {
      _testing = true;
      _error = null;
      _testResult = null;
    });
    try {
      final r = await ref.read(apiProvider).adminTestSearch(_configBody(reuseProviderKey: reuseProviderKey));
      if (mounted) setState(() => _testResult = r);
    } on ApiError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _saveAndEnable(AdminSearchProvider pr, {bool reuseProviderKey = false}) async {
    FocusScope.of(context).unfocus();
    final err = _validate(pr, reuseProviderKey: reuseProviderKey);
    if (err != null) {
      setState(() => _error = err);
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    final api = ref.read(apiProvider);
    try {
      final testRes = await api.adminTestSearch(_configBody(reuseProviderKey: reuseProviderKey));
      if (!mounted) return;
      setState(() => _testResult = testRes);
      if (!testRes.ok) return;

      await api.adminPutSearch({
        'active': _selectedKind,
        'config': _configBody(reuseProviderKey: reuseProviderKey),
      });
      await ref.read(sessionProvider.notifier).refresh();
      ref.invalidate(_searchStateProvider);
      if (!mounted) return;
      _apiKey.clear();
      HapticFeedback.mediumImpact();
      showToast(context, '联网搜索已开启，全家人都可以查询实时信息了');
    } on ApiError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _toggleEnabled(AdminSearchState state, bool enable) async {
    if (!enable) {
      setState(() => _saving = true);
      try {
        await ref.read(apiProvider).adminPutSearch({'active': ''});
        await ref.read(sessionProvider.notifier).refresh();
        ref.invalidate(_searchStateProvider);
        if (mounted) showToast(context, '联网搜索已关闭');
      } on ApiError catch (e) {
        if (mounted) showToast(context, e.message);
      } finally {
        if (mounted) setState(() => _saving = false);
      }
      return;
    }

    final current = state.byKind(_selectedKind);
    if (current != null && current.ready && _apiKey.text.trim().isEmpty) {
      setState(() => _saving = true);
      try {
        await ref.read(apiProvider).adminPutSearch({'active': _selectedKind});
        await ref.read(sessionProvider.notifier).refresh();
        ref.invalidate(_searchStateProvider);
        if (mounted) showToast(context, '已开启${current.name}');
      } on ApiError catch (e) {
        if (mounted) showToast(context, e.message);
      } finally {
        if (mounted) setState(() => _saving = false);
      }
    } else {
      showToast(context, '请先在下方填写 API Key，点击底部「测试并启用」');
    }
  }

  Widget _secretSuffix(Palette p, TextEditingController ctrl) => Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Pressable(
            onTap: () => setState(() => _hideKey = !_hideKey),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
              child: AppIcon(
                _hideKey ? AppIcons.eye : AppIcons.eyeOff,
                size: 19,
                color: p.muted,
              ),
            ),
          ),
          const SizedBox(width: 4),
          FieldActionPill(label: '粘贴', onTap: () => _pasteInto(ctrl)),
        ],
      );

  @override
  Widget build(BuildContext context) {
    final asyncState = ref.watch(_searchStateProvider);
    final p = context.pal;

    return asyncState.when(
      loading: () => Scaffold(appBar: AppBar(title: const Text('联网搜索')), body: const LoadingView()),
      error: (e, _) => Scaffold(
        appBar: AppBar(title: const Text('联网搜索')),
        body: ErrorView(message: ApiError.from(e).message, onRetry: () => ref.invalidate(_searchStateProvider)),
      ),
      data: (state) {
        if (!_initialized) {
          _initialized = true;
          _selectedKind = state.active.isNotEmpty ? state.active : 'deepseek';
          final initial = state.byKind(_selectedKind) ?? state.providers.first;
          _selectedKind = initial.kind;
          _loadProviderFields(initial);
        }
        final current = state.byKind(_selectedKind) ?? state.providers.first;
        final isActiveThis = state.enabled && state.active == current.kind;
        final canReuse = !current.hasKey && current.reusableProviderKeyHint.isNotEmpty && _apiKey.text.trim().isEmpty;

        return Scaffold(
          appBar: AppBar(title: const Text('联网搜索')),
          bottomNavigationBar: BottomBar(
            child: Row(children: [
              SecondaryButton(
                label: '测试连接',
                icon: AppIcons.connectionTest,
                busy: _testing,
                onTap: _saving ? null : () => _runTest(current),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: PrimaryButton(
                  label: isActiveThis ? '保存并更新' : '测试并启用',
                  icon: AppIcons.globe,
                  busy: _saving,
                  onTap: _testing ? null : () => _saveAndEnable(current),
                ),
              ),
            ]),
          ),
          body: ListView(
            keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 44),
            children: [
              // ---- Master toggle hero card ----
              Pressable(
                scale: 0.985,
                onTap: _saving ? null : () => _toggleEnabled(state, !state.enabled),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 220),
                  padding: const EdgeInsets.fromLTRB(16, 16, 14, 16),
                  decoration: BoxDecoration(
                    color: state.enabled ? Color.lerp(p.surface, p.accentSoft, 0.25) : p.surface,
                    borderRadius: BorderRadius.circular(22),
                    border: Border.all(color: state.enabled ? p.accent.withValues(alpha: 0.35) : p.line),
                  ),
                  child: Row(children: [
                    Container(
                      width: 44,
                      height: 44,
                      decoration: BoxDecoration(
                        color: state.enabled ? p.accentSoft : p.surfaceAlt,
                        borderRadius: BorderRadius.circular(14),
                      ),
                      child: AppIcon(
                        state.enabled ? AppIcons.globe : AppIcons.cloudOff,
                        color: state.enabled ? p.accent : p.muted,
                        size: 22,
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                        Row(children: [
                          Text('启用联网搜索', style: AdminText.title(p)),
                          if (state.enabled) ...[
                            const SizedBox(width: 8),
                            Tag('已开启', color: p.good),
                          ],
                        ]),
                        const SizedBox(height: 4),
                        Text(
                          state.enabled
                              ? '当前使用「${state.byKind(state.active)?.name ?? state.active}」，家人询问实时新闻、价格行情或政策时会自动联网查证'
                              : '未启用时，小暖不会上网搜索；配置启用后全家人的对话都能自动查实时信息',
                          style: AdminText.desc(p),
                        ),
                      ]),
                    ),
                    const SizedBox(width: 8),
                    Transform.scale(
                      scale: 0.9,
                      child: Switch(
                        value: state.enabled,
                        onChanged: _saving ? null : (v) => _toggleEnabled(state, v),
                      ),
                    ),
                  ]),
                ),
              ),

              const SectionHeader('选择联网搜索服务', subtitle: '智能按需触发：仅在需要最新资讯或核实事实时联网，日常闲聊不乱搜'),
              for (final pr in state.providers) ...[
                Builder(builder: (_) {
                  final selected = pr.available && _selectedKind == pr.kind;
                  final isLive = state.enabled && state.active == pr.kind;
                  return Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: Pressable(
                      scale: 0.985,
                      onTap: () => _selectProvider(state, pr.kind),
                      child: Opacity(
                        opacity: pr.available ? 1 : 0.58,
                        child: AnimatedContainer(
                          duration: const Duration(milliseconds: 180),
                          padding: const EdgeInsets.all(15),
                          decoration: BoxDecoration(
                            color: selected ? Color.lerp(p.surface, p.accentSoft, 0.30) : p.surface,
                            borderRadius: BorderRadius.circular(20),
                            border: Border.all(
                              color: selected ? p.accent.withValues(alpha: 0.55) : p.line,
                              width: selected ? 1.3 : 1,
                            ),
                          ),
                          child: Row(children: [
                            ProviderIcon(
                              kind: pr.kind,
                              size: 44,
                              accentOverride: selected ? p.accent : null,
                              muted: !selected,
                            ),
                            const SizedBox(width: 14),
                            Expanded(
                              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                Wrap(spacing: 6, runSpacing: 4, crossAxisAlignment: WrapCrossAlignment.center, children: [
                                  Text(
                                    pr.name,
                                    style: TextStyle(
                                      fontSize: 16.5,
                                      fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                                      color: p.text,
                                    ),
                                  ),
                                  if (!pr.available)
                                    const Tag('即将支持')
                                  else if (pr.supportsFetch)
                                    Tag('搜索 + 读网页', color: selected ? p.accent : null)
                                  else
                                    const Tag('网页搜索'),
                                  if (isLive) Tag('当前使用', color: p.good),
                                ]),
                                const SizedBox(height: 5),
                                Text(pr.description, style: AdminText.desc(p)),
                              ]),
                            ),
                            if (pr.available) ...[
                              const SizedBox(width: 10),
                              AppIcon(
                                selected ? AppIcons.checkCircle : AppIcons.circle,
                                size: 21,
                                color: selected ? p.accent : p.line,
                              ),
                            ],
                          ]),
                        ),
                      ),
                    ),
                  );
                }),
              ],

              SectionHeader(
                '${current.name}密钥配置',
                trailing: current.keyUrl.isNotEmpty
                    ? SectionAction(
                        label: '获取 Key',
                        icon: AppIcons.externalLink,
                        filled: true,
                        onTap: () => openLink(context, current.keyUrl),
                      )
                    : null,
              ),

              if (canReuse)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Pressable(
                    scale: 0.985,
                    onTap: (_saving || _testing) ? null : () => _saveAndEnable(current, reuseProviderKey: true),
                    child: Container(
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(
                        color: p.accentSoft.withValues(alpha: 0.65),
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(color: p.accent.withValues(alpha: 0.3)),
                      ),
                      child: Row(children: [
                        AppIcon(AppIcons.sparkles, color: p.accent, size: 20),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Text(
                            '检测到已在「模型与供应商」配置 DeepSeek 密钥（${current.reusableProviderKeyHint}），点此一键复用并开启',
                            style: TextStyle(fontSize: 13.5, color: p.text, height: 1.4),
                          ),
                        ),
                        const SizedBox(width: 6),
                        AppIcon(AppIcons.chevronRight, color: p.accent, size: 17),
                      ]),
                    ),
                  ),
                ),

              GroupCard(
                padding: const EdgeInsets.all(16),
                children: [
                  Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                    AdminField(
                      controller: _apiKey,
                      label: 'API Key',
                      statusText: current.hasKey ? '已保存 ${current.apiKeyHint}' : '必填',
                      statusColor: current.hasKey ? p.good : p.faint,
                      hint: current.hasKey ? '留空则继续使用已保存的 Key' : '${current.keyPrefix.isNotEmpty ? current.keyPrefix : 'sk-'}...',
                      obscureText: _hideKey,
                      mono: true,
                      suffix: _secretSuffix(p, _apiKey),
                      helper: current.priceNote.isNotEmpty ? current.priceNote : null,
                    ),
                    const SizedBox(height: 14),
                    AdminField(
                      controller: _baseUrl,
                      label: '接口地址',
                      statusText: '默认官方直连',
                      hint: current.defaultUrl,
                      mono: true,
                      keyboardType: TextInputType.url,
                      helper: '与对话模型解耦：无论家人当前使用 GPT、Claude、Gemini 还是自动选模型，都能调用这套联网搜索。',
                    ),
                  ]),
                ],
              ),

              if (_error != null) ...[
                const SizedBox(height: 16),
                ResultBanner(ok: false, title: '配置有误', lines: [_error!]),
              ] else if (_testResult != null) ...[
                const SizedBox(height: 16),
                ResultBanner(
                  ok: _testResult!.ok,
                  title: _testResult!.ok ? '搜索测试通过' : '搜索测试未通过',
                  lines: [
                    _testResult!.message,
                    if (_testResult!.ok) '响应耗时 ${_testResult!.latencyMs} 毫秒',
                  ],
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}

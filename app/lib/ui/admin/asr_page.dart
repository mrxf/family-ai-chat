import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../state/session.dart';
import '../sheets.dart';
import '../theme.dart';
import '../widgets.dart';
import 'common.dart';

final _asrStateProvider = FutureProvider.autoDispose<AdminAsrState>((ref) => ref.watch(apiProvider).adminAsr());

class AsrAdminPage extends ConsumerStatefulWidget {
  const AsrAdminPage({super.key});

  @override
  ConsumerState<AsrAdminPage> createState() => _AsrAdminPageState();
}

class _AsrAdminPageState extends ConsumerState<AsrAdminPage> {
  String _selectedKind = 'tencent';
  bool _initialized = false;

  final _appId = TextEditingController();
  final _accessKey = TextEditingController();
  final _secretKey = TextEditingController();
  final _baseUrl = TextEditingController();
  final _model = TextEditingController();

  bool _hideSecret = true;
  bool _testing = false;
  bool _saving = false;
  AsrTestResult? _testResult;
  String? _error;

  @override
  void initState() {
    super.initState();
    for (final c in [_appId, _accessKey, _secretKey, _baseUrl, _model]) {
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
    _appId.dispose();
    _accessKey.dispose();
    _secretKey.dispose();
    _baseUrl.dispose();
    _model.dispose();
    super.dispose();
  }

  void _loadProviderFields(AdminAsrProvider p) {
    _appId.text = p.appId;
    _accessKey.text = p.accessKey;
    _secretKey.clear();
    _baseUrl.text = p.baseUrl.isNotEmpty ? p.baseUrl : p.defaultUrl;
    _model.text = p.model.isNotEmpty ? p.model : p.defaultModel;
    _testResult = null;
    _error = null;
  }

  void _selectProvider(AdminAsrState state, String kind) {
    final pr = state.byKind(kind);
    if (pr == null) return;
    if (!pr.available) {
      showToast(context, '科大讯飞接口已预留，将在后续版本开放');
      return;
    }
    setState(() {
      _selectedKind = kind;
      _loadProviderFields(pr);
    });
  }

  Map<String, dynamic> _configBody() => {
        'kind': _selectedKind,
        'app_id': _appId.text.trim(),
        'access_key': _accessKey.text.trim(),
        'secret_key': _secretKey.text.trim(),
        'base_url': _baseUrl.text.trim(),
        'model': _model.text.trim(),
      };

  String? _validate(AdminAsrProvider pr) {
    if (pr.kind == 'tencent') {
      if (_accessKey.text.trim().isEmpty) return '请填写腾讯云 SecretId';
      if (_secretKey.text.trim().isEmpty && !pr.hasSecret) return '请填写腾讯云 SecretKey';
    } else if (pr.kind == 'openai') {
      final u = _baseUrl.text.trim();
      if (!u.startsWith('http://') && !u.startsWith('https://')) {
        return '接口地址需要以 http:// 或 https:// 开头';
      }
      if (_secretKey.text.trim().isEmpty && !pr.hasSecret) return '请填写 API Key';
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

  Future<void> _runTest(AdminAsrProvider pr) async {
    FocusScope.of(context).unfocus();
    final err = _validate(pr);
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
      final r = await ref.read(apiProvider).adminTestAsr(_configBody());
      if (!mounted) return;
      if (r.appId.isNotEmpty && _appId.text.trim().isEmpty) {
        _appId.text = r.appId;
      }
      setState(() => _testResult = r);
    } on ApiError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _saveAndEnable(AdminAsrProvider pr) async {
    FocusScope.of(context).unfocus();
    final err = _validate(pr);
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
      final testRes = await api.adminTestAsr(_configBody());
      if (!mounted) return;
      if (testRes.appId.isNotEmpty && _appId.text.trim().isEmpty) {
        _appId.text = testRes.appId;
      }
      setState(() => _testResult = testRes);
      if (!testRes.ok) return;

      final updated = await api.adminPutAsr({
        'active': _selectedKind,
        'config': _configBody(),
      });
      await ref.read(sessionProvider.notifier).refresh();
      ref.invalidate(_asrStateProvider);
      if (!mounted) return;
      final savedPr = updated.byKind(_selectedKind);
      if (savedPr != null) {
        _secretKey.clear();
        if (savedPr.appId.isNotEmpty) _appId.text = savedPr.appId;
      }
      HapticFeedback.mediumImpact();
      showToast(context, '语音输入已启用，聊天框已显示麦克风');
    } on ApiError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _toggleEnabled(AdminAsrState state, bool enable) async {
    if (!enable) {
      setState(() => _saving = true);
      try {
        await ref.read(apiProvider).adminPutAsr({'active': ''});
        await ref.read(sessionProvider.notifier).refresh();
        ref.invalidate(_asrStateProvider);
        if (mounted) showToast(context, '语音输入已关闭，麦克风按钮已隐藏');
      } on ApiError catch (e) {
        if (mounted) showToast(context, e.message);
      } finally {
        if (mounted) setState(() => _saving = false);
      }
      return;
    }

    final current = state.byKind(_selectedKind);
    if (current != null && current.ready && _secretKey.text.trim().isEmpty) {
      setState(() => _saving = true);
      try {
        await ref.read(apiProvider).adminPutAsr({'active': _selectedKind});
        await ref.read(sessionProvider.notifier).refresh();
        ref.invalidate(_asrStateProvider);
        if (mounted) showToast(context, '已开启${current.name}');
      } on ApiError catch (e) {
        if (mounted) showToast(context, e.message);
      } finally {
        if (mounted) setState(() => _saving = false);
      }
    } else {
      showToast(context, '请先在下方填写密钥，点击底部「测试并启用」');
    }
  }

  void _pickTencentModel(AdminAsrProvider pr) {
    FocusScope.of(context).unfocus();
    showActionSheet(
      context,
      title: '选择识别引擎',
      actions: [
        for (final m in pr.models)
          SheetAction(
            _model.text.trim() == m.id ? Icons.radio_button_checked_rounded : Icons.radio_button_off_rounded,
            m.name,
            () => setState(() => _model.text = m.id),
            subtitle: m.description,
          ),
      ],
    );
  }

  Widget _secretSuffix(Palette p, TextEditingController ctrl) => Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Pressable(
            onTap: () => setState(() => _hideSecret = !_hideSecret),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
              child: Icon(
                _hideSecret ? Icons.visibility_outlined : Icons.visibility_off_outlined,
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
    final asyncState = ref.watch(_asrStateProvider);
    final p = context.pal;

    return asyncState.when(
      loading: () => Scaffold(appBar: AppBar(title: const Text('语音输入')), body: const LoadingView()),
      error: (e, _) => Scaffold(
        appBar: AppBar(title: const Text('语音输入')),
        body: ErrorView(message: ApiError.from(e).message, onRetry: () => ref.invalidate(_asrStateProvider)),
      ),
      data: (state) {
        if (!_initialized) {
          _initialized = true;
          _selectedKind = state.active.isNotEmpty ? state.active : 'tencent';
          final initial = state.byKind(_selectedKind) ?? state.providers.first;
          _selectedKind = initial.kind;
          _loadProviderFields(initial);
        }
        final current = state.byKind(_selectedKind) ?? state.providers.first;
        final isActiveThis = state.enabled && state.active == current.kind;

        return Scaffold(
          appBar: AppBar(title: const Text('语音输入')),
          bottomNavigationBar: BottomBar(
            child: Row(children: [
              SecondaryButton(
                label: '测试连接',
                icon: Icons.wifi_tethering_rounded,
                busy: _testing,
                onTap: _saving ? null : () => _runTest(current),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: PrimaryButton(
                  label: isActiveThis ? '保存并更新' : '测试并启用',
                  icon: Icons.mic_rounded,
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
                      child: Icon(
                        state.enabled ? Icons.mic_rounded : Icons.mic_none_rounded,
                        color: state.enabled ? p.accent : p.muted,
                        size: 23,
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                        Row(children: [
                          Text('启用语音输入', style: AdminText.title(p)),
                          if (state.enabled) ...[
                            const SizedBox(width: 8),
                            Tag('已开启', color: p.good),
                          ],
                        ]),
                        const SizedBox(height: 4),
                        Text(
                          state.enabled
                              ? '当前使用「${state.byKind(state.active)?.name ?? state.active}」，聊天框已显示麦克风'
                              : '未启用时，所有人的聊天框都不会显示麦克风按钮',
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

              const SectionHeader('选择语音识别服务', subtitle: '支持流式实时转文字与整段录音转写'),
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
                                  else if (pr.isStream)
                                    Tag('边说边出字', color: selected ? p.accent : null)
                                  else
                                    const Tag('录完转写'),
                                  if (isLive) Tag('当前使用', color: p.good),
                                ]),
                                const SizedBox(height: 5),
                                Text(pr.description, style: AdminText.desc(p)),
                              ]),
                            ),
                            if (pr.available) ...[
                              const SizedBox(width: 10),
                              Icon(
                                selected ? Icons.check_circle_rounded : Icons.circle_outlined,
                                size: 22,
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

              if (current.kind == 'tencent') ...[
                SectionHeader(
                  '腾讯云密钥配置',
                  trailing: current.keyUrl.isNotEmpty
                      ? SectionAction(
                          label: '获取密钥',
                          icon: Icons.open_in_new_rounded,
                          filled: true,
                          onTap: () => openLink(context, current.keyUrl),
                        )
                      : null,
                ),
                GroupCard(
                  children: [
                    Padding(
                      padding: const EdgeInsets.all(16),
                      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                        AdminField(
                          controller: _accessKey,
                          label: 'SecretId',
                          statusText: '必填',
                          hint: 'AKID...',
                          mono: true,
                          suffix: FieldActionPill(label: '粘贴', onTap: () => _pasteInto(_accessKey)),
                        ),
                        const SizedBox(height: 14),
                        AdminField(
                          controller: _secretKey,
                          label: 'SecretKey',
                          statusText: current.hasSecret ? '已保存 ${current.secretKeyHint}' : '必填',
                          statusColor: current.hasSecret ? p.good : p.faint,
                          hint: current.hasSecret ? '留空则继续使用已保存的密钥' : '粘贴 SecretKey',
                          obscureText: _hideSecret,
                          mono: true,
                          suffix: _secretSuffix(p, _secretKey),
                        ),
                        const SizedBox(height: 14),
                        AdminField(
                          controller: _appId,
                          label: 'AppID',
                          statusText: '选填 · 自动获取',
                          hint: '例如 1305171418',
                          mono: true,
                          keyboardType: TextInputType.number,
                          helper: '主账号密钥留空即可自动查询；若使用无 CAM 权限的子账号，请在腾讯云控制台「账号信息」查看并填写。',
                        ),
                      ]),
                    ),
                    InfoRow(
                      icon: Icons.tune_rounded,
                      title: '识别引擎',
                      subtitle: '通用版适配免费额度；方言版需大模型资源包',
                      value: current.models.where((m) => m.id == _model.text.trim()).map((m) => m.name).firstOrNull ?? _model.text.trim(),
                      onTap: () => _pickTencentModel(current),
                    ),
                  ],
                ),
              ] else if (current.kind == 'openai') ...[
                SectionHeader(
                  'OpenAI 兼容转写配置',
                  trailing: current.keyUrl.isNotEmpty
                      ? SectionAction(
                          label: '获取 Key',
                          icon: Icons.open_in_new_rounded,
                          filled: true,
                          onTap: () => openLink(context, current.keyUrl),
                        )
                      : null,
                ),
                GroupCard(
                  padding: const EdgeInsets.all(16),
                  children: [
                    Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                      AdminField(
                        controller: _baseUrl,
                        label: '接口地址',
                        hint: 'https://api.siliconflow.cn/v1',
                        mono: true,
                        keyboardType: TextInputType.url,
                      ),
                      const SizedBox(height: 14),
                      AdminField(
                        controller: _secretKey,
                        label: 'API Key',
                        statusText: current.hasSecret ? '已保存 ${current.secretKeyHint}' : '必填',
                        statusColor: current.hasSecret ? p.good : p.faint,
                        hint: current.hasSecret ? '留空则继续使用已保存的 Key' : 'sk-...',
                        obscureText: _hideSecret,
                        mono: true,
                        suffix: _secretSuffix(p, _secretKey),
                      ),
                      const SizedBox(height: 14),
                      AdminField(
                        controller: _model,
                        label: '转写模型名称',
                        hint: 'FunAudioLLM/SenseVoiceSmall',
                        mono: true,
                      ),
                    ]),
                  ],
                ),
              ],

              if (_error != null) ...[
                const SizedBox(height: 16),
                ResultBanner(ok: false, title: '配置有误', lines: [_error!]),
              ] else if (_testResult != null) ...[
                const SizedBox(height: 16),
                ResultBanner(
                  ok: _testResult!.ok,
                  title: _testResult!.ok ? '连接测试通过' : '连接测试未通过',
                  lines: [
                    _testResult!.message,
                    if (_testResult!.ok)
                      '握手耗时 ${_testResult!.latencyMs} 毫秒${_testResult!.appId.isNotEmpty ? ' · AppID: ${_testResult!.appId}' : ''}',
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

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/invite_code.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../admin/admin_common.dart';
import 'invite_generator_sheet.dart';
import 'setup_page.dart';

/// Dedicated configuration screen for switching between the official default server
/// (`https://xiaonuan.miaojiai.com`) and a private self-hosted `nuan-server`.
class SelfHostConfigPage extends ConsumerStatefulWidget {
  const SelfHostConfigPage({super.key, required this.initialServer});
  final String initialServer;

  @override
  ConsumerState<SelfHostConfigPage> createState() => _SelfHostConfigPageState();
}

class _SelfHostConfigPageState extends ConsumerState<SelfHostConfigPage> {
  late bool _selfHosted = isSelfHostedServer(widget.initialServer);
  late final TextEditingController _urlCtrl = TextEditingController(
    text: isSelfHostedServer(widget.initialServer) ? displayHost(widget.initialServer) : '',
  );

  bool _testing = false;
  bool? _testOk;
  bool _needsSetup = false;
  int _latencyMs = 0;
  String? _testMessage;

  @override
  void initState() {
    super.initState();
    _urlCtrl.addListener(_onUrlEdited);
    if (_selfHosted && _urlCtrl.text.trim().isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _testConnection());
    }
  }

  @override
  void dispose() {
    _urlCtrl.removeListener(_onUrlEdited);
    _urlCtrl.dispose();
    super.dispose();
  }

  void _onUrlEdited() {
    if (_testOk != null || _testMessage != null || _needsSetup) {
      setState(() {
        _testOk = null;
        _testMessage = null;
        _needsSetup = false;
      });
    } else {
      setState(() {});
    }
  }

  String get _normalizedCustom => normalizeServer(_urlCtrl.text);

  Future<void> _pasteUrl() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final raw = data?.text?.trim() ?? '';
    if (raw.isEmpty) {
      if (mounted) showToast(context, '剪贴板里还没有内容');
      return;
    }
    // Also support pasting a #xn...xn# token directly into the self-host page!
    final inspected = XnInviteCodec.inspectText(raw);
    if (inspected.hasInvite) {
      final inv = inspected.payload!;
      setState(() {
        _selfHosted = inv.isSelfHosted;
        if (inv.isSelfHosted) _urlCtrl.text = inv.serverHost;
      });
      if (mounted) showToast(context, '已从口令识别服务器地址');
      if (inv.isSelfHosted) await _testConnection();
      return;
    }
    setState(() => _urlCtrl.text = raw);
    await _testConnection();
  }

  Future<void> _testConnection() async {
    FocusScope.of(context).unfocus();
    final raw = _urlCtrl.text.trim();
    if (raw.isEmpty) {
      setState(() {
        _testOk = false;
        _testMessage = '请先输入自托管服务器地址';
      });
      return;
    }
    if (!isValidServerUrl(raw)) {
      setState(() {
        _testOk = false;
        _testMessage = '地址格式不正确，例如 chat.example.com 或 192.168.1.5:8080';
      });
      return;
    }
    final target = normalizeServer(raw);
    if (!isSelfHostedServer(target)) {
      setState(() {
        _testOk = true;
        _needsSetup = false;
        _latencyMs = 0;
        _testMessage = '这是官方默认服务器，无需开启自托管模式即可直接使用';
      });
      return;
    }

    setState(() {
      _testing = true;
      _testOk = null;
      _testMessage = null;
      _needsSetup = false;
    });
    final sw = Stopwatch()..start();
    final api = ref.read(apiFactoryProvider)(target);
    final ok = await api.ping();
    final setup = ok && await api.needsSetup();
    sw.stop();
    if (!mounted || normalizeServer(_urlCtrl.text) != target) return;
    setState(() {
      _testing = false;
      _testOk = ok;
      _needsSetup = setup;
      _latencyMs = sw.elapsedMilliseconds;
      _testMessage = ok
          ? (setup ? '连接成功（${_latencyMs}ms）· 这台新服务器还没有管理员，可立即初始化' : '连接成功（${_latencyMs}ms）· 自托管服务运行正常')
          : '连不上这个地址，请检查域名、端口或网络是否可达';
    });
  }

  Future<void> _save() async {
    FocusScope.of(context).unfocus();
    if (!_selfHosted) {
      await ref.read(sessionProvider.notifier).setServer(kDefaultServer);
      if (!mounted) return;
      showToast(context, '已切换为官方默认服务器');
      Navigator.of(context).pop(true);
      return;
    }

    final raw = _urlCtrl.text.trim();
    if (raw.isEmpty) {
      setState(() {
        _testOk = false;
        _testMessage = '请输入自托管服务器地址，或切回官方默认服务';
      });
      return;
    }
    if (!isValidServerUrl(raw)) {
      setState(() {
        _testOk = false;
        _testMessage = '服务器地址格式不正确，请检查是否有空格或特殊符号';
      });
      return;
    }
    final target = normalizeServer(raw);
    await ref.read(sessionProvider.notifier).setServer(target);
    if (!mounted) return;
    showToast(
      context,
      isSelfHostedServer(target) ? '已启用自托管服务器：${displayHost(target)}' : '已切换为官方默认服务器',
    );
    Navigator.of(context).pop(true);
  }

  Future<void> _openInviteSheet() async {
    FocusScope.of(context).unfocus();
    String target = kDefaultServer;
    if (_selfHosted) {
      final raw = _urlCtrl.text.trim();
      if (raw.isEmpty || !isValidServerUrl(raw)) {
        setState(() {
          _testOk = false;
          _testMessage = '请先填好有效的自托管服务器地址，再生成家人口令';
        });
        return;
      }
      target = normalizeServer(raw);
      await ref.read(sessionProvider.notifier).setServer(target);
    }
    if (!mounted) return;
    await showInviteGeneratorSheet(context, ref, initialServer: target);
  }

  Future<void> _goSetup() async {
    final target = _normalizedCustom;
    if (!isValidServerUrl(target)) return;
    await ref.read(sessionProvider.notifier).setServer(target);
    if (!mounted) return;
    await Navigator.of(context).push(appRoute((_) => SetupPage(server: target)));
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final preview = _normalizedCustom;

    return Scaffold(
      appBar: AppBar(title: const Text('服务器与自托管')),
      bottomNavigationBar: BottomBar(
        child: PrimaryButton(
          label: _selfHosted ? '保存自托管配置' : '使用官方默认服务',
          icon: AppIcons.checkBold,
          onTap: _save,
        ),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(18, 8, 18, 28),
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(4, 2, 4, 16),
            child: Text(
              '普通家庭使用官方默认服务即可，免填网址；如果你在自家 NAS 或云服务器部署了小暖后端，可切换为自托管。',
              style: TextStyle(fontSize: 14.5, color: p.muted, height: 1.5),
            ),
          ),
          _ModeOptionCard(
            selected: !_selfHosted,
            icon: AppIcons.sparkles,
            title: '官方默认服务',
            badge: '推荐',
            subtitle: '${displayHost(kDefaultServer)} · 打开即用，免填网址',
            onTap: () => setState(() => _selfHosted = false),
          ),
          const SizedBox(height: 12),
          _ModeOptionCard(
            selected: _selfHosted,
            icon: AppIcons.server,
            title: '自托管服务器',
            badge: '私有部署',
            subtitle: '连接自己搭建的 nuan-server，对话与记忆保存在你自己的服务器',
            onTap: () => setState(() => _selfHosted = true),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 240),
            curve: Curves.easeOutCubic,
            child: !_selfHosted
                ? const SizedBox(width: double.infinity)
                : Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      const SectionHeader('自托管地址配置'),
                      GroupCard(
                        padding: const EdgeInsets.all(16),
                        children: [
                          AdminField(
                            controller: _urlCtrl,
                            label: '服务器域名或 IP:端口',
                            hint: '例如 chat.example.com 或 192.168.1.5:8080',
                            mono: true,
                            keyboardType: TextInputType.url,
                            prefixIcon: AppIcons.server,
                            helper: preview.isNotEmpty && isValidServerUrl(preview)
                                ? '完整连接地址：$preview'
                                : '公网域名默认走 https://，局域网 IP 或带端口地址自动识别 http://',
                            suffix: _urlCtrl.text.isEmpty
                                ? FieldActionPill(label: '粘贴', onTap: _pasteUrl)
                                : IconButton(
                                    tooltip: '清空',
                                    icon: AppIcon(AppIcons.closeCircle, size: 19, color: p.faint),
                                    onPressed: () => _urlCtrl.clear(),
                                  ),
                          ),
                          const SizedBox(height: 14),
                          SecondaryButton(
                            label: '测试连接',
                            icon: AppIcons.connectionTest,
                            busy: _testing,
                            onTap: _testConnection,
                          ),
                          if (_testOk != null && _testMessage != null) ...[
                            const SizedBox(height: 14),
                            ResultBanner(ok: _testOk!, title: _testMessage!),
                          ],
                          if (_needsSetup) ...[
                            const SizedBox(height: 12),
                            Pressable(
                              scale: 0.98,
                              onTap: _goSetup,
                              child: Container(
                                padding: const EdgeInsets.all(14),
                                decoration: BoxDecoration(
                                  color: p.accentSoft,
                                  borderRadius: BorderRadius.circular(16),
                                  border: Border.all(color: p.accent.withValues(alpha: 0.38)),
                                ),
                                child: Row(children: [
                                  AppIcon(AppIcons.rocket, color: p.accent, size: 21),
                                  const SizedBox(width: 10),
                                  Expanded(
                                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                      const Text('初始化这台新服务器', style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w700)),
                                      const SizedBox(height: 2),
                                      Text('输入日志里的 8 位设置码，创建首个管理员账号', style: TextStyle(fontSize: 13, color: p.muted)),
                                    ]),
                                  ),
                                  AppIcon(AppIcons.chevronRight, color: p.accent, size: 18),
                                ]),
                              ),
                            ),
                          ],
                        ],
                      ),
                    ],
                  ),
          ),
          const SectionHeader('异地家人免输接入'),
          Container(
            padding: const EdgeInsets.all(18),
            decoration: BoxDecoration(
              color: p.surface,
              borderRadius: BorderRadius.circular(20),
              border: Border.all(color: p.line),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(children: [
                  Container(
                    width: 42,
                    height: 42,
                    decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(13)),
                    child: AppIcon(AppIcons.magicWand, color: p.accent, size: 22),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      const Text('生成 #xn...xn# 一键登录口令', style: TextStyle(fontSize: 16.5, fontWeight: FontWeight.w700)),
                      const SizedBox(height: 3),
                      Text('加密打包服务器与账号，父母复制后打开即自动登录', style: TextStyle(fontSize: 13.5, color: p.muted)),
                    ]),
                  ),
                ]),
                const SizedBox(height: 14),
                Text(
                  '适合给异地父母配置：生成一段小暖专属对称加密口令发到微信，父母复制整条消息后进入登录页，会自动识别并弹出「立即登录」，无需理解服务器地址或手敲密码。',
                  style: TextStyle(fontSize: 14, color: p.muted, height: 1.5),
                ),
                const SizedBox(height: 14),
                SecondaryButton(
                  label: '生成家人加密口令',
                  icon: AppIcons.copy,
                  onTap: _openInviteSheet,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ModeOptionCard extends StatelessWidget {
  const _ModeOptionCard({
    required this.selected,
    required this.icon,
    required this.title,
    required this.badge,
    required this.subtitle,
    required this.onTap,
  });

  final bool selected;
  final IconData icon;
  final String title, badge, subtitle;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.985,
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: selected ? p.accentSoft.withValues(alpha: 0.55) : p.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: selected ? p.accent : p.line, width: selected ? 1.6 : 1),
        ),
        child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: AppIcon(
              selected ? AppIcons.checkCircleFill : AppIcons.circle,
              color: selected ? p.accent : p.faint,
              size: 22,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                AppIcon(icon, size: 18, color: selected ? p.accent : p.muted),
                const SizedBox(width: 6),
                Flexible(
                  child: Text(title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700)),
                ),
                const SizedBox(width: 8),
                Tag(badge, color: selected ? p.accent : null),
              ]),
              const SizedBox(height: 5),
              Text(subtitle, style: TextStyle(fontSize: 14, color: p.muted, height: 1.45)),
            ]),
          ),
        ]),
      ),
    );
  }
}

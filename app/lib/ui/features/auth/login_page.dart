import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/invite_code.dart';
import '../../../state/incoming_share.dart';
import '../../../state/session.dart';
import '../admin/admin_common.dart' show PrimaryButton, ResultBanner, Tag;
import '../../theme/icons.dart';
import '../../widgets/mascot/mascot_anim.dart';
import 'self_host_page.dart';
import 'setup_page.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

class LoginPage extends ConsumerStatefulWidget {
  const LoginPage({super.key});

  @override
  ConsumerState<LoginPage> createState() => _LoginPageState();
}

enum _Reach { unknown, checking, ok, fail }

class _LoginPageState extends ConsumerState<LoginPage> with WidgetsBindingObserver {
  late final _user = TextEditingController(text: ref.read(sessionProvider.notifier).lastUsername);
  final _pass = TextEditingController();
  final _passFocus = FocusNode();

  bool _busy = false;
  bool _hide = true;
  bool _inviteDialogOpen = false;
  String? _error;
  _Reach _reach = _Reach.unknown;
  bool _needsSetup = false;
  Timer? _checkTimer;
  Timer? _focusRetryTimer;
  ProviderSubscription<SharedPayload?>? _shareSub;
  final Set<String> _handledFingerprints = <String>{};

  String get _activeServer => effectiveServer(ref.read(sessionProvider).server);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _checkServer(_activeServer, immediate: true);

    // Check clipboard once the first frame renders (and once more shortly after on Android 10+
    // where Clipboard.getData returns null until the window acquires input focus).
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _inspectClipboard(manual: false, scheduleFocusRetry: true);
    });

    // Also support sharing a WeChat text message containing #xn...xn# straight into 小暖.
    _shareSub = ref.listenManual<SharedPayload?>(
      incomingShareProvider,
      (_, next) {
        if (next != null && next.text.contains(XnInviteCodec.prefix)) {
          final inspected = XnInviteCodec.inspectText(next.text);
          if (inspected.hasInvite || inspected.hasError) {
            ref.read(incomingShareProvider.notifier).take();
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (mounted) _presentInspection(inspected, manual: true);
            });
          }
        }
      },
      fireImmediately: true,
    );
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _checkTimer?.cancel();
    _focusRetryTimer?.cancel();
    _shareSub?.close();
    _user.dispose();
    _pass.dispose();
    _passFocus.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _inspectClipboard(manual: false);
    }
  }

  void _checkServer(String serverUrl, {bool immediate = false}) {
    _checkTimer?.cancel();
    final s = effectiveServer(serverUrl);
    setState(() {
      _reach = _Reach.checking;
      _needsSetup = false;
    });
    _checkTimer = Timer(Duration(milliseconds: immediate ? 20 : 350), () async {
      final api = ref.read(apiFactoryProvider)(s);
      final ok = await api.ping();
      final setup = ok && await api.needsSetup();
      if (mounted && _activeServer == s) {
        setState(() {
          _reach = ok ? _Reach.ok : _Reach.fail;
          _needsSetup = setup;
        });
      }
    });
  }

  Future<void> _inspectClipboard({required bool manual, bool scheduleFocusRetry = false}) async {
    if (!mounted || _busy || _inviteDialogOpen) return;
    if (ref.read(sessionProvider).status != SessionStatus.loggedOut) return;

    String text = '';
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      text = data?.text ?? '';
    } catch (_) {}

    if (!mounted || _busy || _inviteDialogOpen) return;

    if (text.trim().isEmpty) {
      if (scheduleFocusRetry) {
        _focusRetryTimer?.cancel();
        _focusRetryTimer = Timer(const Duration(milliseconds: 350), () {
          if (mounted) _inspectClipboard(manual: false);
        });
      }
      if (manual) _openManualPasteSheet();
      return;
    }

    final inspected = XnInviteCodec.inspectText(text);
    if (!inspected.hasInvite && !inspected.hasError) {
      if (manual) _openManualPasteSheet();
      return;
    }

    await _presentInspection(inspected, manual: manual);
  }

  Future<void> _presentInspection(InviteInspection inspected, {required bool manual}) async {
    if (!mounted || _inviteDialogOpen) return;
    final prefs = ref.read(storageProvider).prefs;
    final fp = inspected.fingerprint;

    if (!manual) {
      if (InviteCodeGuard.wasGeneratedLocally(fp)) return;
      if (InviteCodeGuard.wasConsumed(prefs, fp)) return;
      if (fp != null && _handledFingerprints.contains(fp)) return;
    }
    if (fp != null) _handledFingerprints.add(fp);

    if (inspected.hasError) {
      showToast(context, inspected.error!.message);
      if (manual) _openManualPasteSheet(initialError: inspected.error!.message);
      return;
    }

    final invite = inspected.payload!;
    _inviteDialogOpen = true;
    try {
      await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (_) => _AutoLoginDialog(
          invite: invite,
          fingerprint: fp,
          onFallbackPrefill: () {
            _user.text = invite.username;
            _pass.text = invite.password;
          },
        ),
      );
    } finally {
      _inviteDialogOpen = false;
    }
  }

  Future<void> _openManualPasteSheet({String? initialError}) async {
    final pasted = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _ManualPasteSheet(
        initialError: initialError,
        onOpenGenerator: () {
          Navigator.of(context).pop();
          showInviteGeneratorSheet(context, ref, initialServer: _activeServer);
        },
      ),
    );
    if (pasted == null || pasted.trim().isEmpty || !mounted) return;
    final inspected = XnInviteCodec.inspectText(pasted);
    if (!inspected.hasInvite && !inspected.hasError) {
      showToast(context, '没有找到 #xn...xn# 格式的小暖口令，请检查是否复制完整');
      return;
    }
    await _presentInspection(inspected, manual: true);
  }

  Future<void> _openSelfHostConfig() async {
    FocusScope.of(context).unfocus();
    await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => SelfHostConfigPage(initialServer: _activeServer)),
    );
  }

  Future<void> _login() async {
    FocusScope.of(context).unfocus();
    // Smart convenience: if a parent pasted the #xn...xn# code into the username box by mistake,
    // recognize it gracefully instead of failing login!
    for (final candidate in [_user.text, _pass.text]) {
      if (candidate.contains(XnInviteCodec.prefix)) {
        final ins = XnInviteCodec.inspectText(candidate);
        if (ins.hasInvite) {
          _user.clear();
          _pass.clear();
          await _presentInspection(ins, manual: true);
          return;
        }
      }
    }

    if (_user.text.trim().isEmpty || _pass.text.isEmpty) {
      setState(() => _error = '请填写用户名和密码');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(sessionProvider.notifier).login(_activeServer, _user.text, _pass.text);
    } on ApiError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (e) {
      if (mounted) setState(() => _error = '登录失败：$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final server = ref.watch(sessionProvider.select((s) => effectiveServer(s.server)));
    final isSelfHost = isSelfHostedServer(server);

    ref.listen<String>(
      sessionProvider.select((s) => effectiveServer(s.server)),
      (prev, next) {
        if (prev != next) {
          setState(() => _error = null);
          _checkServer(next, immediate: true);
        }
      },
    );

    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(28, 20, 28, 24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 440),
              child: AutofillGroup(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const Center(child: Floating(child: AnimatedMascot(size: 136, greet: true))),
                    const SizedBox(height: 16),
                    const Text(
                      '你好，我是小暖',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 28, fontWeight: FontWeight.w800, letterSpacing: 0.5),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '家里人专属的 AI 助手',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 16, color: p.muted),
                    ),
                    const SizedBox(height: 28),
                    AnimatedSize(
                      duration: const Duration(milliseconds: 240),
                      curve: Curves.easeOutCubic,
                      child: !isSelfHost
                          ? const SizedBox(width: double.infinity)
                          : Padding(
                              padding: const EdgeInsets.only(bottom: 18),
                              child: _SelfHostCard(
                                host: displayHost(server),
                                reach: _reach,
                                onConfigure: _openSelfHostConfig,
                                onInvite: () => showInviteGeneratorSheet(context, ref, initialServer: server),
                                onResetDefault: () async {
                                  await ref.read(sessionProvider.notifier).setServer(kDefaultServer);
                                  if (context.mounted) showToast(context, '已切回官方默认服务');
                                },
                              ),
                            ),
                    ),
                    AnimatedSize(
                      duration: const Duration(milliseconds: 240),
                      child: !_needsSetup
                          ? const SizedBox(width: double.infinity)
                          : Padding(
                              padding: const EdgeInsets.only(bottom: 18),
                              child: Pressable(
                                scale: 0.98,
                                onTap: () => Navigator.of(context).push(
                                  MaterialPageRoute(builder: (_) => SetupPage(server: server)),
                                ),
                                child: Container(
                                  padding: const EdgeInsets.all(16),
                                  decoration: BoxDecoration(
                                    color: p.accentSoft,
                                    borderRadius: BorderRadius.circular(18),
                                    border: Border.all(color: p.accent.withValues(alpha: 0.4)),
                                  ),
                                  child: Row(children: [
                                    AppIcon(AppIcons.rocket, color: p.accent),
                                    const SizedBox(width: 12),
                                    Expanded(
                                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                        const Text('这台服务器还没有管理员', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
                                        const SizedBox(height: 2),
                                        Text('你是部署它的人？点这里完成初始化', style: TextStyle(fontSize: 13.5, color: p.muted)),
                                      ]),
                                    ),
                                    AppIcon(AppIcons.chevronRight, color: p.accent, size: 18),
                                  ]),
                                ),
                              ),
                            ),
                    ),
                    _label('用户名'),
                    TextField(
                      controller: _user,
                      autocorrect: false,
                      autofillHints: const [AutofillHints.username],
                      textInputAction: TextInputAction.next,
                      onSubmitted: (_) => _passFocus.requestFocus(),
                      style: const TextStyle(fontSize: 17),
                      decoration: InputDecoration(
                        hintText: '管理员给你的用户名',
                        prefixIcon: AppIcon(AppIcons.user, color: p.faint),
                      ),
                    ),
                    const SizedBox(height: 16),
                    _label('密码'),
                    TextField(
                      controller: _pass,
                      focusNode: _passFocus,
                      obscureText: _hide,
                      autofillHints: const [AutofillHints.password],
                      textInputAction: TextInputAction.done,
                      onSubmitted: (_) => _login(),
                      style: const TextStyle(fontSize: 17),
                      decoration: InputDecoration(
                        hintText: '请输入密码',
                        prefixIcon: AppIcon(AppIcons.lock, color: p.faint),
                        suffixIcon: IconButton(
                          icon: AppIcon(_hide ? AppIcons.eye : AppIcons.eyeOff, color: p.faint),
                          onPressed: () => setState(() => _hide = !_hide),
                        ),
                      ),
                    ),
                    AnimatedSize(
                      duration: const Duration(milliseconds: 200),
                      child: _error == null
                          ? const SizedBox(height: 28)
                          : Padding(
                              padding: const EdgeInsets.symmetric(vertical: 14),
                              child: Row(children: [
                                AppIcon(AppIcons.info, color: p.bad, size: 20),
                                const SizedBox(width: 8),
                                Expanded(child: Text(_error!, style: TextStyle(color: p.bad, fontSize: 15))),
                              ]),
                            ),
                    ),
                    Pressable(
                      scale: 0.98,
                      onTap: _busy ? null : _login,
                      child: Container(
                        height: 56,
                        decoration: BoxDecoration(
                          gradient: Palette.brandGradient,
                          borderRadius: BorderRadius.circular(18),
                          boxShadow: [
                            BoxShadow(color: Palette.brand.withValues(alpha: 0.32), blurRadius: 18, offset: const Offset(0, 6)),
                          ],
                        ),
                        alignment: Alignment.center,
                        child: _busy
                            ? const SizedBox(width: 24, height: 24, child: CircularProgressIndicator(strokeWidth: 2.6, color: Colors.white))
                            : const Text('登 录', style: TextStyle(color: Colors.white, fontSize: 18, fontWeight: FontWeight.w700, letterSpacing: 2)),
                      ),
                    ),
                    const SizedBox(height: 18),
                    Wrap(
                      alignment: WrapAlignment.center,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      spacing: 6,
                      runSpacing: 4,
                      children: [
                        _FooterActionButton(
                          icon: AppIcons.paste,
                          label: '粘贴登录口令',
                          onTap: () => _inspectClipboard(manual: true),
                        ),
                        Text('·', style: TextStyle(color: p.faint, fontSize: 16)),
                        _FooterActionButton(
                          icon: AppIcons.server,
                          label: isSelfHost ? '切换服务器' : '自托管服务器',
                          onTap: _openSelfHostConfig,
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '家人发来了 #xn...xn# 口令？复制后打开本页即可自动登录',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: p.faint, fontSize: 13.5, height: 1.4),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _label(String t) => Padding(
        padding: const EdgeInsets.only(left: 4, bottom: 8),
        child: Text(t, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: context.pal.muted)),
      );
}

class _FooterActionButton extends StatelessWidget {
  const _FooterActionButton({required this.icon, required this.label, required this.onTap});
  final IconData icon;
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.96,
      onTap: onTap,
      child: ConstrainedBox(
        constraints: const BoxConstraints(minHeight: 44),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
          child: Row(mainAxisSize: MainAxisSize.min, children: [
            AppIcon(icon, size: 17, color: p.accent),
            const SizedBox(width: 5),
            Text(label, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: p.muted)),
          ]),
        ),
      ),
    );
  }
}

/// Displayed on the login screen whenever the user has configured a self-hosted server,
/// surfacing live reachability, server configuration, and one-tap family invite generation.
class _SelfHostCard extends StatelessWidget {
  const _SelfHostCard({
    required this.host,
    required this.reach,
    required this.onConfigure,
    required this.onInvite,
    required this.onResetDefault,
  });

  final String host;
  final _Reach reach;
  final VoidCallback onConfigure;
  final VoidCallback onInvite;
  final VoidCallback onResetDefault;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final (statusLabel, statusColor) = switch (reach) {
      _Reach.ok => ('已连通', p.good),
      _Reach.fail => ('连不上', p.bad),
      _Reach.checking => ('检测中', p.muted),
      _Reach.unknown => ('待检测', p.muted),
    };

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: reach == _Reach.fail ? p.bad.withValues(alpha: 0.45) : p.line),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 13, 12, 12),
            child: Row(children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(12)),
                child: AppIcon(AppIcons.server, color: p.accent, size: 20),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Row(children: [
                    const Flexible(
                      child: Text('自托管服务器', maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w700)),
                    ),
                    const SizedBox(width: 7),
                    Tag(
                      statusLabel,
                      color: statusColor,
                      icon: reach == _Reach.ok ? AppIcons.checkBold : (reach == _Reach.fail ? AppIcons.error : null),
                    ),
                  ]),
                  const SizedBox(height: 3),
                  Text(
                    host,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13.5, color: p.muted, fontFamily: 'monospace'),
                  ),
                ]),
              ),
              const SizedBox(width: 8),
              Pressable(
                scale: 0.95,
                onTap: onConfigure,
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 7),
                  decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(11)),
                  child: Text('配置', style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.accent)),
                ),
              ),
            ]),
          ),
          Divider(height: 1, color: p.line),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            child: Row(children: [
              Expanded(
                child: Pressable(
                  scale: 0.97,
                  onTap: onInvite,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 6),
                    child: Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                      AppIcon(AppIcons.magicWand, size: 16, color: p.accent),
                      const SizedBox(width: 5),
                      Flexible(
                        child: Text(
                          '生成家人口令',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.text),
                        ),
                      ),
                    ]),
                  ),
                ),
              ),
              Container(width: 1, height: 18, color: p.line),
              Expanded(
                child: Pressable(
                  scale: 0.97,
                  onTap: onResetDefault,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 6),
                    child: Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                      AppIcon(AppIcons.repeat, size: 15, color: p.muted),
                      const SizedBox(width: 5),
                      Flexible(
                        child: Text(
                          '切回默认服务',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: p.muted),
                        ),
                      ),
                    ]),
                  ),
                ),
              ),
            ]),
          ),
        ],
      ),
    );
  }
}

/// Elder-friendly confirmation dialog popped automatically when `#xn...xn#` is detected.
class _AutoLoginDialog extends ConsumerStatefulWidget {
  const _AutoLoginDialog({
    required this.invite,
    required this.fingerprint,
    required this.onFallbackPrefill,
  });

  final InvitePayload invite;
  final String? fingerprint;
  final VoidCallback onFallbackPrefill;

  @override
  ConsumerState<_AutoLoginDialog> createState() => _AutoLoginDialogState();
}

class _AutoLoginDialogState extends ConsumerState<_AutoLoginDialog> {
  bool _submitting = false;
  String? _error;

  Future<void> _confirm() async {
    final inv = widget.invite;
    final prefs = ref.read(storageProvider).prefs;
    setState(() {
      _submitting = true;
      _error = null;
    });

    try {
      if (inv.hasCredentials) {
        await ref.read(sessionProvider.notifier).login(inv.server, inv.username, inv.password);
        await InviteCodeGuard.markConsumed(prefs, widget.fingerprint);
        // Wipe the encrypted credential from the system clipboard after a successful login.
        try {
          await Clipboard.setData(const ClipboardData(text: ''));
        } catch (_) {}
        HapticFeedback.mediumImpact();
        if (mounted) Navigator.of(context).pop();
      } else {
        await ref.read(sessionProvider.notifier).setServer(inv.server);
        await InviteCodeGuard.markConsumed(prefs, widget.fingerprint);
        try {
          await Clipboard.setData(const ClipboardData(text: ''));
        } catch (_) {}
        HapticFeedback.selectionClick();
        if (mounted) {
          showToast(context, '已切换到自托管服务器：${inv.serverHost}');
          Navigator.of(context).pop();
        }
      }
    } on ApiError catch (e) {
      await ref.read(sessionProvider.notifier).setServer(inv.server);
      widget.onFallbackPrefill();
      if (mounted) {
        setState(() {
          _submitting = false;
          _error = e.message;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _submitting = false;
          _error = '连接失败：$e';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final inv = widget.invite;
    final fullLogin = inv.hasCredentials;
    final title = fullLogin
        ? (inv.displayName.isNotEmpty ? '你好，${inv.displayName}！' : '识别到登录口令')
        : '识别到自托管配置';
    final subtitle = fullLogin
        ? '家人已为你配好小暖账号，点下面按钮就能直接进入'
        : '点下面按钮即可切换到家人配置好的自托管服务器';

    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 24, vertical: 24),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 400),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(22, 24, 22, 18),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Center(child: Mascot(size: 72)),
              const SizedBox(height: 14),
              Text(
                title,
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 23, fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 6),
              Text(
                subtitle,
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 15.5, color: p.muted, height: 1.45),
              ),
              const SizedBox(height: 18),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 13),
                decoration: BoxDecoration(
                  color: p.surfaceAlt,
                  borderRadius: BorderRadius.circular(16),
                  border: Border.all(color: p.line),
                ),
                child: Column(
                  children: [
                    if (fullLogin) ...[
                      _InfoLine(label: '登录账号', value: inv.username),
                      const SizedBox(height: 8),
                    ],
                    _InfoLine(
                      label: '连接服务',
                      value: inv.isSelfHosted ? '自托管 · ${inv.serverHost}' : '官方默认服务',
                    ),
                  ],
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 14),
                ResultBanner(ok: false, title: _error!),
              ],
              const SizedBox(height: 20),
              PrimaryButton(
                label: fullLogin ? '立即自动登录' : '使用这台服务器',
                icon: fullLogin ? AppIcons.checkBold : AppIcons.server,
                busy: _submitting,
                onTap: _confirm,
              ),
              const SizedBox(height: 8),
              Center(
                child: TextButton(
                  onPressed: _submitting ? null : () => Navigator.of(context).pop(),
                  child: Text('暂不使用', style: TextStyle(fontSize: 15, color: p.muted)),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _InfoLine extends StatelessWidget {
  const _InfoLine({required this.label, required this.value});
  final String label, value;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Row(children: [
      Text(label, style: TextStyle(fontSize: 14.5, color: p.muted)),
      const SizedBox(width: 12),
      Expanded(
        child: Text(
          value,
          textAlign: TextAlign.right,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600, color: p.text),
        ),
      ),
    ]);
  }
}

/// Fallback paste sheet when the phone OS blocks silent clipboard reading or clipboard is empty.
class _ManualPasteSheet extends StatefulWidget {
  const _ManualPasteSheet({this.initialError, required this.onOpenGenerator});
  final String? initialError;
  final VoidCallback onOpenGenerator;

  @override
  State<_ManualPasteSheet> createState() => _ManualPasteSheetState();
}

class _ManualPasteSheetState extends State<_ManualPasteSheet> {
  final _ctrl = TextEditingController();

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(20, 4, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('粘贴登录口令', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 6),
            Text(
              '把家人发在微信里的整段消息粘贴到下方框内（包含 #xn...xn#），点「识别并登录」即可。',
              style: TextStyle(fontSize: 14.5, color: p.muted, height: 1.45),
            ),
            if (widget.initialError != null) ...[
              const SizedBox(height: 12),
              ResultBanner(ok: false, title: widget.initialError!),
            ],
            const SizedBox(height: 14),
            TextField(
              controller: _ctrl,
              autofocus: true,
              minLines: 3,
              maxLines: 5,
              style: const TextStyle(fontSize: 16, height: 1.45),
              decoration: InputDecoration(
                hintText: '长按此处点「粘贴」…',
                fillColor: p.surfaceAlt,
                enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(16), borderSide: BorderSide.none),
              ),
            ),
            const SizedBox(height: 16),
            PrimaryButton(
              label: '识别并登录',
              icon: AppIcons.checkBold,
              onTap: () => Navigator.of(context).pop(_ctrl.text),
            ),
            const SizedBox(height: 8),
            Center(
              child: TextButton.icon(
                onPressed: widget.onOpenGenerator,
                icon: AppIcon(AppIcons.magicWand, size: 16, color: p.accent),
                label: Text('我是家人管理员，制作登录口令', style: TextStyle(fontSize: 14, color: p.muted)),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

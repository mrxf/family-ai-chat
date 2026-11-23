import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/invite_code.dart';
import '../../../state/session.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../admin/admin_common.dart';

/// Modal to generate and copy an encrypted `#xn...xn#` invite message for family members.
Future<void> showInviteGeneratorSheet(
  BuildContext context,
  WidgetRef ref, {
  String? initialServer,
  String initialUsername = '',
  String initialPassword = '',
  String initialDisplayName = '',
  bool lockAccount = false,
}) {
  final server = effectiveServer(initialServer ?? ref.read(sessionProvider).server);
  return showAdaptiveModal<void>(
    context: context,
    maxWidth: 500,
    builder: (_) => _InviteGeneratorSheet(
      server: server,
      initialUsername: initialUsername,
      initialPassword: initialPassword,
      initialDisplayName: initialDisplayName,
      lockAccount: lockAccount,
    ),
  );
}

class _InviteGeneratorSheet extends ConsumerStatefulWidget {
  const _InviteGeneratorSheet({
    required this.server,
    required this.initialUsername,
    required this.initialPassword,
    required this.initialDisplayName,
    required this.lockAccount,
  });

  final String server;
  final String initialUsername;
  final String initialPassword;
  final String initialDisplayName;
  final bool lockAccount;

  @override
  ConsumerState<_InviteGeneratorSheet> createState() => _InviteGeneratorSheetState();
}

class _InviteGeneratorSheetState extends ConsumerState<_InviteGeneratorSheet> {
  late final TextEditingController _nameCtrl = TextEditingController(text: widget.initialDisplayName);
  late final TextEditingController _userCtrl = TextEditingController(text: widget.initialUsername);
  late final TextEditingController _passCtrl = TextEditingController(text: widget.initialPassword);

  bool _hidePass = false;
  bool _showPlain = false;
  bool _verifying = false;
  bool? _verifyOk;
  String? _verifyMsg;
  late final String? _lockedMessage = widget.lockAccount ? _buildShareMessage() : null;

  @override
  void dispose() {
    _nameCtrl.dispose();
    _userCtrl.dispose();
    _passCtrl.dispose();
    super.dispose();
  }

  InvitePayload _currentPayload() => InvitePayload(
        server: widget.server,
        username: _userCtrl.text.trim(),
        password: _passCtrl.text,
        displayName: _nameCtrl.text.trim(),
      );

  String? _buildShareMessage() {
    try {
      final p = _currentPayload();
      final code = XnInviteCodec.encode(p);
      return XnInviteCodec.formatShareMessage(p, prebuiltCode: code);
    } on InviteCodeException {
      return null;
    }
  }

  Future<void> _verifyAccount() async {
    FocusScope.of(context).unfocus();
    final u = _userCtrl.text.trim();
    final pw = _passCtrl.text;
    if (u.isEmpty || pw.isEmpty) {
      setState(() {
        _verifyOk = false;
        _verifyMsg = '请先填写家人的用户名和密码再测试';
      });
      return;
    }
    setState(() {
      _verifying = true;
      _verifyOk = null;
      _verifyMsg = null;
    });
    final factory = ref.read(apiFactoryProvider);
    try {
      final (tok, user) = await factory(widget.server).login(u, pw, 'InviteCheck');
      // Immediately revoke the temporary check token so no extra session remains.
      try {
        await factory(widget.server, token: tok).logout();
      } catch (_) {}
      if (!mounted) return;
      if (_nameCtrl.text.trim().isEmpty && user.displayName.isNotEmpty) {
        _nameCtrl.text = user.displayName;
      }
      setState(() {
        _verifyOk = true;
        _verifyMsg = '验证通过！账号「${user.displayName}」可正常登录，放心复制发给家人吧';
      });
    } on ApiError catch (e) {
      if (!mounted) return;
      setState(() {
        _verifyOk = false;
        _verifyMsg = '验证未通过：${e.message}';
      });
    } finally {
      if (mounted) setState(() => _verifying = false);
    }
  }

  void _copyEncrypted() {
    FocusScope.of(context).unfocus();
    try {
      final payload = _currentPayload();
      final msg = _lockedMessage ?? XnInviteCodec.formatShareMessage(payload);
      InviteCodeGuard.markGeneratedLocally(msg);
      Clipboard.setData(ClipboardData(text: msg));
      HapticFeedback.mediumImpact();
      showToast(context, '已复制一键登录口令，去微信粘贴发给家人吧');
      Navigator.of(context).pop();
    } on InviteCodeException catch (e) {
      setState(() {
        _verifyOk = false;
        _verifyMsg = e.message;
      });
    }
  }

  void _copyPlain() {
    final host = displayHost(widget.server);
    final lines = <String>[
      '小暖登录信息',
      if (isSelfHostedServer(widget.server)) '自托管服务器：$host',
      if (_userCtrl.text.trim().isNotEmpty) '用户名：${_userCtrl.text.trim()}',
      if (_passCtrl.text.isNotEmpty) '密码：${_passCtrl.text}',
    ];
    Clipboard.setData(ClipboardData(text: lines.join('\n')));
    HapticFeedback.selectionClick();
    showToast(context, '已复制明文登录信息');
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isSelfHost = isSelfHostedServer(widget.server);
    final host = displayHost(widget.server);

    if (widget.lockAccount && _lockedMessage != null) {
      final who = widget.initialDisplayName.trim().isEmpty ? widget.initialUsername : widget.initialDisplayName.trim();
      return SafeArea(
        child: Padding(
          padding: EdgeInsets.fromLTRB(20, 4, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(children: [
                  const Mascot(size: 46),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Text('已为「$who」生成登录口令', style: const TextStyle(fontSize: 19, fontWeight: FontWeight.w700)),
                      const SizedBox(height: 3),
                      Text('发给家人复制后打开小暖，点一下即可自动登录', style: TextStyle(fontSize: 13.5, color: p.muted)),
                    ]),
                  ),
                ]),
                const SizedBox(height: 16),
                Container(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: p.surfaceAlt,
                    borderRadius: BorderRadius.circular(16),
                    border: Border.all(color: p.line),
                  ),
                  child: SelectableText(
                    _lockedMessage,
                    style: TextStyle(fontSize: 14.5, color: p.text, height: 1.55),
                  ),
                ),
                const SizedBox(height: 16),
                PrimaryButton(
                  label: '复制一键登录口令（推荐发微信）',
                  icon: AppIcons.copy,
                  onTap: _copyEncrypted,
                ),
                const SizedBox(height: 8),
                Center(
                  child: TextButton.icon(
                    onPressed: () => setState(() => _showPlain = !_showPlain),
                    icon: AppIcon(_showPlain ? AppIcons.chevronUp : AppIcons.chevronDown, size: 16, color: p.muted),
                    label: Text(_showPlain ? '收起明文账号密码' : '查看普通明文账号密码', style: TextStyle(fontSize: 14, color: p.muted)),
                  ),
                ),
                if (_showPlain)
                  Container(
                    padding: const EdgeInsets.fromLTRB(14, 12, 10, 12),
                    decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(14), border: Border.all(color: p.line)),
                    child: Row(children: [
                      Expanded(
                        child: SelectableText(
                          '${isSelfHost ? '服务器：$host\n' : '服务器：官方默认服务（$host）\n'}用户名：${widget.initialUsername}\n密码：${widget.initialPassword}',
                          style: TextStyle(fontSize: 14, color: p.muted, height: 1.6),
                        ),
                      ),
                      IconButton(tooltip: '复制明文', icon: AppIcon(AppIcons.copy, size: 19, color: p.accent), onPressed: _copyPlain),
                    ]),
                  ),
              ],
            ),
          ),
        ),
      );
    }

    final hasCreds = _userCtrl.text.trim().isNotEmpty && _passCtrl.text.isNotEmpty;
    final canGenerate = hasCreds || isSelfHost;

    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(20, 2, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('生成家人免输登录口令', style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 6),
              Text(
                '口令经小暖专属对称加密（#xn...xn#），不会在微信聊天记录中暴露明文密码；父母复制后打开小暖会自动弹出登录。',
                style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.45),
              ),
              const SizedBox(height: 14),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
                child: Row(children: [
                  AppIcon(isSelfHost ? AppIcons.server : AppIcons.sparkles, size: 18, color: p.accent),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      isSelfHost ? '目标服务：自托管（$host）' : '目标服务：官方默认（$host）',
                      style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.text),
                    ),
                  ),
                ]),
              ),
              const SizedBox(height: 14),
              TextField(
                controller: _nameCtrl,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  labelText: '家人称呼（选填，如：妈妈、爸爸）',
                  prefixIcon: AppIcon(AppIcons.heart),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _userCtrl,
                autocorrect: false,
                onChanged: (_) => setState(() {
                  _verifyOk = null;
                  _verifyMsg = null;
                }),
                decoration: const InputDecoration(
                  labelText: '家人的登录用户名',
                  prefixIcon: AppIcon(AppIcons.user),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _passCtrl,
                obscureText: _hidePass,
                autocorrect: false,
                onChanged: (_) => setState(() {
                  _verifyOk = null;
                  _verifyMsg = null;
                }),
                decoration: InputDecoration(
                  labelText: '家人的登录密码',
                  prefixIcon: const AppIcon(AppIcons.lock),
                  suffixIcon: IconButton(
                    icon: AppIcon(_hidePass ? AppIcons.eye : AppIcons.eyeOff, color: p.faint),
                    onPressed: () => setState(() => _hidePass = !_hidePass),
                  ),
                ),
              ),
              if (isSelfHost && !hasCreds) ...[
                const SizedBox(height: 10),
                Text(
                  '提示：不填用户名和密码时，将生成「纯自托管服务器配置口令」；填好用户名和密码则父母可一键自动登录。',
                  style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.45),
                ),
              ],
              if (_verifyMsg != null && _verifyOk != null) ...[
                const SizedBox(height: 12),
                ResultBanner(ok: _verifyOk!, title: _verifyMsg!),
              ],
              const SizedBox(height: 16),
              if (hasCreds) ...[
                SecondaryButton(
                  label: '先测试一下账号能否登录',
                  icon: AppIcons.connectionTest,
                  busy: _verifying,
                  onTap: _verifyAccount,
                ),
                const SizedBox(height: 10),
              ],
              PrimaryButton(
                label: hasCreds ? '复制一键自动登录口令' : '复制自托管服务器口令',
                icon: AppIcons.copy,
                onTap: canGenerate ? _copyEncrypted : null,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

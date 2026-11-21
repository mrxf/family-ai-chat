import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/invite_code.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../admin/admin_common.dart' show PrimaryButton, ResultBanner;

/// Elder-friendly confirmation dialog popped automatically when `#xn...xn#` is detected.
class InviteLoginDialog extends ConsumerStatefulWidget {
  const InviteLoginDialog({
    super.key,
    required this.invite,
    required this.fingerprint,
    required this.onFallbackPrefill,
  });

  final InvitePayload invite;
  final String? fingerprint;
  final VoidCallback onFallbackPrefill;

  @override
  ConsumerState<InviteLoginDialog> createState() => _InviteLoginDialogState();
}

class _InviteLoginDialogState extends ConsumerState<InviteLoginDialog> {
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
class ManualInvitePasteSheet extends StatefulWidget {
  const ManualInvitePasteSheet({super.key, this.initialError, required this.onOpenGenerator});
  final String? initialError;
  final VoidCallback onOpenGenerator;

  @override
  State<ManualInvitePasteSheet> createState() => _ManualInvitePasteSheetState();
}

class _ManualInvitePasteSheetState extends State<ManualInvitePasteSheet> {
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

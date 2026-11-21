import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../admin/admin_common.dart';
import '../admin/provider_pages.dart';

/// First-run setup of a fresh server: setup code from the server log + the admin account.
/// Afterwards the user lands in the app and is taken straight to "add provider".
class SetupPage extends ConsumerStatefulWidget {
  const SetupPage({super.key, required this.server});
  final String server;

  @override
  ConsumerState<SetupPage> createState() => _SetupPageState();
}

class _SetupPageState extends ConsumerState<SetupPage> {
  final _code = TextEditingController();
  final _name = TextEditingController(text: '管理员');
  final _user = TextEditingController(text: 'admin');
  final _pass = TextEditingController();
  final _scroll = ScrollController();
  bool _hide = true, _busy = false;
  String? _error;

  @override
  void dispose() {
    for (final c in [_code, _name, _user, _pass]) {
      c.dispose();
    }
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    FocusScope.of(context).unfocus();
    String? err;
    if (_code.text.replaceAll(RegExp(r'[\s-]'), '').length != 8) {
      err = '设置码是 8 位，例如 ABCD-EFGH';
    } else if (_user.text.trim().length < 2) {
      err = '用户名至少 2 位';
    } else if (_pass.text.length < 8) {
      err = '管理员密码至少 8 位';
    }
    _showError(err);
    if (err != null) return;
    setState(() => _busy = true);
    try {
      final (token, _) = await Api(widget.server).setup(
        code: _code.text,
        username: _user.text.trim(),
        password: _pass.text,
        displayName: _name.text.trim(),
        device: 'Android',
      );
      final nav = Navigator.of(context);
      await ref.read(sessionProvider.notifier).loginWithToken(widget.server, _user.text.trim(), token);
      HapticFeedback.mediumImpact();
      // The gate swaps the login page for home; take the admin straight to adding a provider.
      nav.popUntil((r) => r.isFirst);
      nav.push(addProviderRoute());
    } on ApiError catch (e) {
      if (mounted) _showError(e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Shows (or clears) the error and scrolls down so it and the button stay in view.
  void _showError(String? err) {
    setState(() => _error = err);
    if (err == null) return;
    // Wait for the banner's grow animation before measuring the scroll extent.
    Future.delayed(const Duration(milliseconds: 220), () {
      if (mounted && _scroll.hasClients) {
        _scroll.animateTo(_scroll.position.maxScrollExtent, duration: const Duration(milliseconds: 250), curve: Curves.easeOutCubic);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final host = widget.server.replaceFirst(RegExp(r'^https?://'), '');
    return Scaffold(
      appBar: AppBar(title: const Text('初始化服务器')),
      body: ListView(
        controller: _scroll,
        // Explicit padding drops the automatic safe-area inset; add it back so the
        // button can scroll clear of the gesture bar.
        padding: EdgeInsets.fromLTRB(20, 4, 20, 32 + MediaQuery.paddingOf(context).bottom),
        children: [
          Row(children: [
            const Mascot(size: 64),
            const SizedBox(width: 14),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                const Text('欢迎！这是一台新服务器', style: TextStyle(fontSize: 19, fontWeight: FontWeight.w700)),
                const SizedBox(height: 4),
                Text(host, style: TextStyle(fontSize: 14, color: p.muted)),
              ]),
            ),
          ]),
          const SizedBox(height: 20),
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(18)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              const Text('第 1 步：找到设置码', style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w700)),
              const SizedBox(height: 6),
              Text('服务器第一次启动时会在日志里打印 8 位设置码（setup_code），每次重启都会换一个。用 Docker 部署的可以这样查看：',
                  style: TextStyle(fontSize: 14, color: p.text, height: 1.5)),
              const SizedBox(height: 10),
              _Command('docker logs nuan 2>&1 | grep setup_code'),
            ]),
          ),
          const SizedBox(height: 18),
          TextField(
            controller: _code,
            textCapitalization: TextCapitalization.characters,
            autocorrect: false,
            style: const TextStyle(fontSize: 20, letterSpacing: 3, fontFamily: 'monospace', fontWeight: FontWeight.w700),
            decoration: InputDecoration(labelText: '设置码', hintText: 'ABCD-EFGH', prefixIcon: AppIcon(AppIcons.pinCode, color: p.faint)),
          ),
          const SizedBox(height: 26),
          const Text('第 2 步：创建管理员账号', style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w700)),
          const SizedBox(height: 4),
          Text('管理员可以给家人建账号、配置模型。之后可以在设置里改密码。', style: TextStyle(fontSize: 13.5, color: p.muted)),
          const SizedBox(height: 14),
          TextField(controller: _name, maxLength: 20, decoration: const InputDecoration(labelText: '你的称呼', counterText: '')),
          const SizedBox(height: 12),
          TextField(controller: _user, autocorrect: false, decoration: const InputDecoration(labelText: '登录用户名')),
          const SizedBox(height: 12),
          TextField(
            controller: _pass,
            obscureText: _hide,
            decoration: InputDecoration(
              labelText: '密码（至少 8 位）',
              suffixIcon: IconButton(icon: AppIcon(_hide ? AppIcons.eye : AppIcons.eyeOff), onPressed: () => setState(() => _hide = !_hide)),
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 200),
            child: _error == null ? const SizedBox(height: 24, width: double.infinity) : Padding(padding: const EdgeInsets.symmetric(vertical: 16), child: ResultBanner(ok: false, title: _error!)),
          ),
          PrimaryButton(label: '创建管理员，开始使用', busy: _busy, onTap: _submit),
          const SizedBox(height: 12),
          Text('下一步会引导你添加模型服务（API Key）。', textAlign: TextAlign.center, style: TextStyle(fontSize: 13.5, color: p.faint)),
        ],
      ),
    );
  }
}

class _Command extends StatelessWidget {
  const _Command(this.cmd);
  final String cmd;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.98,
      onTap: () {
        Clipboard.setData(ClipboardData(text: cmd));
        showToast(context, '已复制命令');
      },
      child: Container(
        padding: const EdgeInsets.fromLTRB(12, 10, 10, 10),
        decoration: BoxDecoration(color: p.preBg, borderRadius: BorderRadius.circular(12)),
        child: Row(children: [
          Expanded(child: Text(cmd, style: TextStyle(color: p.preText, fontFamily: 'monospace', fontSize: 13))),
          AppIcon(AppIcons.copy, size: 17, color: p.preText.withValues(alpha: 0.7)),
        ]),
      ),
    );
  }
}

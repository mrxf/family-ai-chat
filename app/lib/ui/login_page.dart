import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../state/session.dart';
import 'icons.dart';
import 'setup_page.dart';
import 'mascot_anim.dart';
import 'theme.dart';
import 'widgets.dart';

class LoginPage extends ConsumerStatefulWidget {
  const LoginPage({super.key});

  @override
  ConsumerState<LoginPage> createState() => _LoginPageState();
}

enum _Reach { unknown, checking, ok, fail }

class _LoginPageState extends ConsumerState<LoginPage> {
  late final _server = TextEditingController(text: ref.read(sessionProvider.notifier).lastServer);
  late final _user = TextEditingController(text: ref.read(sessionProvider.notifier).lastUsername);
  final _pass = TextEditingController();
  final _passFocus = FocusNode();
  bool _busy = false, _hide = true;
  String? _error;
  _Reach _reach = _Reach.unknown;
  bool _needsSetup = false;
  Timer? _debounce;

  @override
  void initState() {
    super.initState();
    if (_server.text.isNotEmpty) _check();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _server.dispose();
    _user.dispose();
    _pass.dispose();
    _passFocus.dispose();
    super.dispose();
  }

  void _check() {
    _debounce?.cancel();
    final s = normalizeServer(_server.text);
    if (s.isEmpty) {
      setState(() => _reach = _Reach.unknown);
      return;
    }
    setState(() {
      _reach = _Reach.checking;
      _needsSetup = false;
    });
    _debounce = Timer(const Duration(milliseconds: 500), () async {
      final api = Api(s);
      final ok = await api.ping();
      final setup = ok && await api.needsSetup();
      if (mounted && normalizeServer(_server.text) == s) {
        setState(() {
          _reach = ok ? _Reach.ok : _Reach.fail;
          _needsSetup = setup;
        });
      }
    });
  }

  Future<void> _login() async {
    FocusScope.of(context).unfocus();
    if (_server.text.trim().isEmpty || _user.text.trim().isEmpty || _pass.text.isEmpty) {
      setState(() => _error = '请把服务器地址、用户名和密码都填上');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(sessionProvider.notifier).login(_server.text, _user.text, _pass.text);
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
    Widget? reachIcon = switch (_reach) {
      _Reach.checking => Padding(padding: const EdgeInsets.all(14), child: SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: p.faint))),
      _Reach.ok => Icon(AppIcons.checkCircle, color: p.good),
      _Reach.fail => Icon(AppIcons.error, color: p.bad),
      _Reach.unknown => null,
    };

    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 440),
              child: AutofillGroup(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const Center(child: Floating(child: AnimatedMascot(size: 140, greet: true))),
                    const SizedBox(height: 16),
                    const Text('你好，我是小暖', textAlign: TextAlign.center, style: TextStyle(fontSize: 28, fontWeight: FontWeight.w800, letterSpacing: 0.5)),
                    const SizedBox(height: 8),
                    Text('家里人专属的 AI 助手', textAlign: TextAlign.center, style: TextStyle(fontSize: 16, color: p.muted)),
                    const SizedBox(height: 36),
                    _label('服务器地址'),
                    TextField(
                      controller: _server,
                      keyboardType: TextInputType.url,
                      autocorrect: false,
                      textInputAction: TextInputAction.next,
                      onChanged: (_) => _check(),
                      style: const TextStyle(fontSize: 17),
                      decoration: InputDecoration(
                        hintText: '例如 chat.example.com',
                        prefixIcon: Icon(AppIcons.server, color: p.faint),
                        suffixIcon: reachIcon,
                        helperText: _reach == _Reach.fail ? '连不上这个地址，请检查是否填写正确' : null,
                        helperStyle: TextStyle(color: p.bad),
                      ),
                    ),
                    AnimatedSize(
                      duration: const Duration(milliseconds: 250),
                      child: !_needsSetup
                          ? const SizedBox(width: double.infinity)
                          : Padding(
                              padding: const EdgeInsets.only(top: 14),
                              child: Pressable(
                                scale: 0.98,
                                onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => SetupPage(server: normalizeServer(_server.text)))),
                                child: Container(
                                  padding: const EdgeInsets.all(16),
                                  decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.accent.withValues(alpha: 0.4))),
                                  child: Row(children: [
                                    Icon(AppIcons.rocket, color: p.accent),
                                    const SizedBox(width: 12),
                                    Expanded(
                                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                        const Text('这台服务器还没有管理员', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
                                        const SizedBox(height: 2),
                                        Text('你是部署它的人？点这里完成初始化', style: TextStyle(fontSize: 13.5, color: p.muted)),
                                      ]),
                                    ),
                                    Icon(AppIcons.chevronRight, color: p.accent, size: 18),
                                  ]),
                                ),
                              ),
                            ),
                    ),
                    const SizedBox(height: 16),
                    _label('用户名'),
                    TextField(
                      controller: _user,
                      autocorrect: false,
                      autofillHints: const [AutofillHints.username],
                      textInputAction: TextInputAction.next,
                      onSubmitted: (_) => _passFocus.requestFocus(),
                      style: const TextStyle(fontSize: 17),
                      decoration: InputDecoration(hintText: '管理员给你的用户名', prefixIcon: Icon(AppIcons.user, color: p.faint)),
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
                        prefixIcon: Icon(AppIcons.lock, color: p.faint),
                        suffixIcon: IconButton(
                          icon: Icon(_hide ? AppIcons.eye : AppIcons.eyeOff, color: p.faint),
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
                                Icon(AppIcons.info, color: p.bad, size: 20),
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
                          boxShadow: [BoxShadow(color: Palette.brand.withValues(alpha: 0.32), blurRadius: 18, offset: const Offset(0, 6))],
                        ),
                        alignment: Alignment.center,
                        child: _busy
                            ? const SizedBox(width: 24, height: 24, child: CircularProgressIndicator(strokeWidth: 2.6, color: Colors.white))
                            : const Text('登 录', style: TextStyle(color: Colors.white, fontSize: 18, fontWeight: FontWeight.w700, letterSpacing: 2)),
                      ),
                    ),
                    const SizedBox(height: 20),
                    Text('没有账号？请找家里的管理员帮你创建', textAlign: TextAlign.center, style: TextStyle(color: p.faint, fontSize: 14)),
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

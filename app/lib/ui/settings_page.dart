import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../core/api.dart';
import '../state/session.dart';
import 'admin/models_page.dart';
import 'admin/usage_page.dart';
import 'admin_page.dart';
import 'sheets.dart';
import 'theme.dart';
import 'update.dart';
import 'widgets.dart';

const avatarChoices = ['', '👵', '👴', '👩', '👨', '👧', '👦', '🧑', '🐱', '🐶', '🐼', '🐰', '🦊', '🐻', '🌻', '🌈', '⭐', '🍀'];

class SettingsPage extends ConsumerWidget {
  const SettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final session = ref.watch(sessionProvider);
    final user = session.user;
    if (user == null) return const Scaffold();
    final s = ref.watch(settingsProvider);
    final settings = ref.read(settingsProvider.notifier);

    return Scaffold(
      appBar: AppBar(title: const Text('设置')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 40),
        children: [
          // ---- profile ----
          Pressable(
            scale: 0.98,
            onTap: () => _editProfile(context, ref),
            child: Container(
              padding: const EdgeInsets.all(18),
              decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(22), border: Border.all(color: p.line)),
              child: Row(children: [
                Avatar(label: user.displayName, emoji: user.avatar, size: 60),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(user.displayName, style: const TextStyle(fontSize: 21, fontWeight: FontWeight.w700)),
                    const SizedBox(height: 4),
                    Text('用户名：${user.username}${user.isAdmin ? ' · 管理员' : ''}', style: TextStyle(fontSize: 14, color: p.muted)),
                  ]),
                ),
                Icon(Icons.chevron_right_rounded, color: p.faint),
              ]),
            ),
          ),

          _Section('字体大小', children: [
            _FontScaleSlider(value: s.fontScale, onChanged: (v) => settings.update(s.copyWith(fontScale: v))),
          ]),

          _Section('外观', children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: _Segmented<ThemeMode>(
                value: s.themeMode,
                options: const {ThemeMode.system: '跟随系统', ThemeMode.light: '浅色', ThemeMode.dark: '深色'},
                onChanged: (v) => settings.update(s.copyWith(themeMode: v)),
              ),
            ),
          ]),

          _Section('朗读', children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 12, 18, 4),
              child: Row(children: [
                const Text('语速', style: TextStyle(fontSize: 16)),
                Expanded(
                  child: Slider(
                    value: s.ttsRate,
                    min: 0.3,
                    max: 0.7,
                    divisions: 4,
                    label: _rateLabel(s.ttsRate),
                    onChanged: (v) => settings.update(s.copyWith(ttsRate: v)),
                  ),
                ),
                SizedBox(width: 36, child: Text(_rateLabel(s.ttsRate), style: TextStyle(color: p.muted))),
              ]),
            ),
          ]),

          _Section('账号', children: [
            _Tile(icon: Icons.lock_reset_rounded, title: '修改密码', onTap: () => _changePassword(context, ref)),
          ]),

          if (user.isAdmin)
            _Section('家庭管理', children: [
              _Tile(
                icon: Icons.family_restroom_rounded,
                title: '家庭成员',
                subtitle: '给家人创建账号、重置密码',
                onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const AdminUsersPage())),
              ),
              _Tile(
                icon: Icons.hub_rounded,
                title: '模型与供应商',
                subtitle: session.boot?.defaultModel == null ? '还没有可用的模型，点这里添加' : '默认：${session.boot!.defaultModel!.displayName} · 共 ${session.boot!.models.length} 个模型',
                onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const ModelsPage())),
              ),
              _Tile(
                icon: Icons.bar_chart_rounded,
                title: '用量与花费',
                subtitle: '每个人、每个模型用了多少钱',
                onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const UsagePage())),
              ),
            ]),

          _Section('关于', children: [
            _Tile(icon: Icons.system_update_rounded, title: '检查更新', onTap: () => checkForUpdate(context, ref)),
            FutureBuilder<PackageInfo>(
              future: PackageInfo.fromPlatform(),
              builder: (_, snap) => _Tile(icon: Icons.info_outline_rounded, title: '版本', trailing: Text(snap.data?.version ?? '', style: TextStyle(color: p.muted))),
            ),
            _Tile(icon: Icons.dns_outlined, title: '服务器', trailing: Text(session.server.replaceFirst(RegExp(r'^https?://'), ''), style: TextStyle(color: p.muted, fontSize: 14))),
          ]),

          const SizedBox(height: 24),
          Pressable(
            scale: 0.98,
            onTap: () async {
              if (await confirm(context, title: '退出登录？', body: '退出后需要重新输入密码。聊天记录保存在服务器上，不会丢失。', ok: '退出', danger: true)) {
                if (context.mounted) Navigator.of(context).popUntil((r) => r.isFirst);
                await ref.read(sessionProvider.notifier).logout();
              }
            },
            child: Container(
              height: 54,
              alignment: Alignment.center,
              decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.line)),
              child: Text('退出登录', style: TextStyle(color: p.bad, fontSize: 17, fontWeight: FontWeight.w600)),
            ),
          ),
        ],
      ),
    );
  }

  static String _rateLabel(double r) => switch (r) {
        <= 0.35 => '很慢',
        <= 0.45 => '较慢',
        <= 0.55 => '正常',
        <= 0.65 => '较快',
        _ => '很快',
      };

  Future<void> _editProfile(BuildContext context, WidgetRef ref) async {
    final user = ref.read(sessionProvider).user!;
    final name = TextEditingController(text: user.displayName);
    var avatar = user.avatar;
    final ok = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => StatefulBuilder(builder: (ctx, setS) {
        final p = ctx.pal;
        return Padding(
          padding: EdgeInsets.fromLTRB(20, 0, 20, MediaQuery.viewInsetsOf(ctx).bottom + 20),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Text('个人资料', style: Theme.of(ctx).textTheme.titleMedium),
            const SizedBox(height: 16),
            Center(child: Avatar(label: name.text, emoji: avatar, size: 72)),
            const SizedBox(height: 14),
            Wrap(spacing: 8, runSpacing: 8, alignment: WrapAlignment.center, children: [
              for (final e in avatarChoices)
                Pressable(
                  onTap: () => setS(() => avatar = e),
                  child: Container(
                    decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: avatar == e ? p.accent : Colors.transparent, width: 2)),
                    padding: const EdgeInsets.all(2),
                    child: Avatar(label: name.text, emoji: e, size: 40),
                  ),
                ),
            ]),
            const SizedBox(height: 16),
            TextField(
              controller: name,
              maxLength: 20,
              onChanged: (_) => setS(() {}),
              style: const TextStyle(fontSize: 17),
              decoration: const InputDecoration(labelText: '称呼（小暖会这样叫你）'),
            ),
            const SizedBox(height: 8),
            FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('保存')),
          ]),
        );
      }),
    );
    if (ok != true) return;
    try {
      final u = await ref.read(apiProvider).updateMe({'display_name': name.text.trim(), 'avatar': avatar});
      ref.read(sessionProvider.notifier).setUser(u);
      if (context.mounted) showToast(context, '已保存');
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }

  Future<void> _changePassword(BuildContext context, WidgetRef ref) async {
    final old = TextEditingController(), nw = TextEditingController();
    final ok = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => Padding(
        padding: EdgeInsets.fromLTRB(20, 0, 20, MediaQuery.viewInsetsOf(ctx).bottom + 20),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text('修改密码', style: Theme.of(ctx).textTheme.titleMedium),
          const SizedBox(height: 16),
          TextField(controller: old, obscureText: true, decoration: const InputDecoration(labelText: '原密码')),
          const SizedBox(height: 12),
          TextField(controller: nw, obscureText: true, decoration: const InputDecoration(labelText: '新密码（至少 6 位）')),
          const SizedBox(height: 16),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('确认修改')),
        ]),
      ),
    );
    if (ok != true) return;
    try {
      await ref.read(apiProvider).updateMe({'old_password': old.text, 'new_password': nw.text});
      if (context.mounted) showToast(context, '密码已修改');
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }
}

/// Five font sizes on a stepped slider, with a live preview.
class _FontScaleSlider extends StatelessWidget {
  const _FontScaleSlider({required this.value, required this.onChanged});
  final double value;
  final ValueChanged<double> onChanged;

  static const _steps = [(0.85, '小'), (0.92, '较小'), (1.0, '标准'), (1.15, '大'), (1.3, '特大')];

  int get _index {
    var best = 0;
    for (var i = 1; i < _steps.length; i++) {
      if ((_steps[i].$1 - value).abs() < (_steps[best].$1 - value).abs()) best = i;
    }
    return best;
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final i = _index;
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 14, 12, 16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(crossAxisAlignment: CrossAxisAlignment.center, children: [
          Padding(padding: const EdgeInsets.only(left: 8), child: Text('A', textScaler: TextScaler.noScaling, style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w600))),
          Expanded(
            child: Slider(
              value: i.toDouble(),
              min: 0,
              max: (_steps.length - 1).toDouble(),
              divisions: _steps.length - 1,
              label: _steps[i].$2,
              onChanged: (v) {
                final n = v.round();
                if (n != i) {
                  HapticFeedback.selectionClick();
                  onChanged(_steps[n].$1);
                }
              },
            ),
          ),
          Padding(padding: const EdgeInsets.only(right: 8), child: Text('A', textScaler: TextScaler.noScaling, style: TextStyle(fontSize: 24, color: p.muted, fontWeight: FontWeight.w600))),
        ]),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24),
          child: Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
            for (final (j, (_, label)) in _steps.indexed)
              SizedBox(
                width: 40,
                child: Text(label,
                    textAlign: TextAlign.center,
                    textScaler: TextScaler.noScaling,
                    style: TextStyle(fontSize: 13, color: j == i ? p.accent : p.faint, fontWeight: j == i ? FontWeight.w700 : FontWeight.w500)),
              ),
          ]),
        ),
        const SizedBox(height: 14),
        Container(
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
          child: Text('小暖会用这个大小显示文字。拖动上面的滑块，找一个看着最舒服的大小。', style: Theme.of(context).textTheme.bodyLarge),
        ),
      ]),
    );
  }
}

class _Section extends StatelessWidget {
  const _Section(this.title, {required this.children});
  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(8, 24, 8, 8),
        child: Text(title, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.muted)),
      ),
      Container(
        clipBehavior: Clip.antiAlias,
        decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(20), border: Border.all(color: p.line)),
        child: Column(children: children),
      ),
    ]);
  }
}

class _Tile extends StatelessWidget {
  const _Tile({required this.icon, required this.title, this.subtitle, this.onTap, this.trailing});
  final IconData icon;
  final String title;
  final String? subtitle;
  final VoidCallback? onTap;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 2),
      leading: Icon(icon, color: p.accent),
      title: Text(title, style: const TextStyle(fontSize: 17)),
      subtitle: subtitle == null || subtitle!.isEmpty ? null : Text(subtitle!, style: TextStyle(fontSize: 13.5, color: p.muted)),
      trailing: trailing ?? (onTap != null ? Icon(Icons.chevron_right_rounded, color: p.faint) : null),
      onTap: onTap,
    );
  }
}

class _Segmented<T> extends StatelessWidget {
  const _Segmented({required this.value, required this.options, required this.onChanged});
  final T value;
  final Map<T, String> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(16)),
      child: Row(children: [
        for (final e in options.entries)
          Expanded(
            child: Pressable(
              onTap: () => onChanged(e.key),
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 200),
                height: 44,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: e.key == value ? p.surface : Colors.transparent,
                  borderRadius: BorderRadius.circular(12),
                  boxShadow: e.key == value ? [BoxShadow(color: p.shadow, blurRadius: 8, offset: const Offset(0, 2))] : null,
                ),
                child: Text(e.value,
                    style: TextStyle(fontSize: 16, fontWeight: e.key == value ? FontWeight.w700 : FontWeight.w500, color: e.key == value ? p.accent : p.muted)),
              ),
            ),
          ),
      ]),
    );
  }
}

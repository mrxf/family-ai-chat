import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/session.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../desktop/desktop_page_header.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import '../auth/invite_generator_sheet.dart';
import '../settings/settings_page.dart' show avatarChoices;

final _usersProvider = FutureProvider.autoDispose<List<User>>((ref) => ref.watch(apiProvider).adminUsers());

class AdminUsersPage extends ConsumerWidget {
  const AdminUsersPage({super.key, this.embedded = false});

  final bool embedded;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final users = ref.watch(_usersProvider);
    final me = ref.watch(sessionProvider).user;
    if (me == null) return const Scaffold();

    final body = users.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (e, _) => Center(child: Text(ApiError.from(e).message)),
      data: (list) => RefreshIndicator(
        onRefresh: () => ref.refresh(_usersProvider.future),
        child: ListView(
          padding: embedded ? const EdgeInsets.fromLTRB(32, 20, 32, 40) : const EdgeInsets.fromLTRB(16, 8, 16, 100),
          children: [
            DesktopPageBody(
              maxWidth: embedded ? 860 : double.infinity,
              child: Column(
                children: [
                  for (var i = 0; i < list.length; i++) ...[
                    if (i > 0) const SizedBox(height: 10),
                    Builder(
                      builder: (_) {
                        final u = list[i];
                        final disabled = u.disabled;
                        return Pressable(
                          scale: 0.98,
                          onTap: () => _actions(context, ref, u, isMe: u.id == me.id),
                          child: Container(
                            padding: const EdgeInsets.all(14),
                            decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.line)),
                            child: Row(children: [
                              Opacity(opacity: disabled ? 0.4 : 1, child: Avatar(label: u.displayName, emoji: u.avatar, size: 48)),
                              const SizedBox(width: 14),
                              Expanded(
                                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                  Row(children: [
                                    Flexible(child: Text(u.displayName, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600))),
                                    if (u.isAdmin) _badge(p, '管理员', p.accent),
                                    if (u.id == me.id) _badge(p, '我', p.muted),
                                  ]),
                                  const SizedBox(height: 3),
                                  Text(disabled ? '已停用 · 不能登录' : '用户名：${u.username}', style: TextStyle(fontSize: 14, color: disabled ? p.bad : p.muted)),
                                ]),
                              ),
                              AppIcon(AppIcons.more, color: p.faint),
                            ]),
                          ),
                        );
                      },
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );

    if (embedded) {
      return Scaffold(
        body: Column(
          children: [
            DesktopPageHeader(
              title: '家庭成员',
              subtitle: '给家人创建账号、重置密码或管理停用状态',
              actions: [
                FilledButton.icon(
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                  ),
                  onPressed: () => _create(context, ref),
                  icon: const AppIcon(AppIcons.userPlus, size: 17),
                  label: const Text('添加家人', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
                ),
              ],
            ),
            Expanded(child: body),
          ],
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(title: const Text('家庭成员')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _create(context, ref),
        backgroundColor: p.accent,
        foregroundColor: Colors.white,
        icon: const AppIcon(AppIcons.userPlus, size: 21),
        label: const Text('添加家人', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
      ),
      body: body,
    );
  }

  Widget _badge(Palette p, String t, Color c) => Container(
        margin: const EdgeInsets.only(left: 8),
        padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
        decoration: BoxDecoration(color: c.withValues(alpha: 0.12), borderRadius: BorderRadius.circular(8)),
        child: Text(t, style: TextStyle(fontSize: 12, color: c, fontWeight: FontWeight.w600)),
      );

  Future<void> _create(BuildContext context, WidgetRef ref) async {
    final name = TextEditingController(), username = TextEditingController(), pass = TextEditingController(text: _randomPassword());
    var avatar = '';
    var admin = false;
    final ok = await showAdaptiveModal<bool>(
      context: context,
      maxWidth: 480,
      builder: (ctx) => StatefulBuilder(builder: (ctx, setS) {
        final p = ctx.pal;
        return Padding(
          padding: EdgeInsets.fromLTRB(20, 0, 20, MediaQuery.viewInsetsOf(ctx).bottom + 20),
          child: SingleChildScrollView(
            child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              Text('添加家人', style: Theme.of(ctx).textTheme.titleMedium),
              const SizedBox(height: 14),
              SizedBox(
                height: 48,
                child: ListView(scrollDirection: Axis.horizontal, children: [
                  for (final e in avatarChoices)
                    Pressable(
                      onTap: () => setS(() => avatar = e),
                      child: Container(
                        margin: const EdgeInsets.only(right: 6),
                        padding: const EdgeInsets.all(2),
                        decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: avatar == e ? p.accent : Colors.transparent, width: 2)),
                        child: Avatar(label: name.text, emoji: e, size: 40),
                      ),
                    ),
                ]),
              ),
              const SizedBox(height: 14),
              TextField(controller: name, onChanged: (_) => setS(() {}), decoration: const InputDecoration(labelText: '称呼（如：妈妈、老公）')),
              const SizedBox(height: 12),
              TextField(controller: username, autocorrect: false, decoration: const InputDecoration(labelText: '登录用户名（如：mama）')),
              const SizedBox(height: 12),
              TextField(controller: pass, decoration: const InputDecoration(labelText: '初始密码（至少 6 位）')),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                value: admin,
                onChanged: (v) => setS(() => admin = v),
                title: const Text('设为管理员'),
                subtitle: Text('管理员可以添加家人、修改默认模型', style: TextStyle(color: p.muted, fontSize: 13)),
              ),
              const SizedBox(height: 8),
              FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('创建账号')),
            ]),
          ),
        );
      }),
    );
    if (ok != true) return;
    try {
      final u = await ref.read(apiProvider).adminCreateUser({
        'display_name': name.text.trim(),
        'username': username.text.trim(),
        'password': pass.text,
        'avatar': avatar,
        'role': admin ? 'admin' : 'member',
      });
      if (!context.mounted) return;
      ref.invalidate(_usersProvider);
      _showCredentials(context, ref, u, pass.text);
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }

  void _showCredentials(BuildContext context, WidgetRef ref, User u, String password) {
    showInviteGeneratorSheet(
      context,
      ref,
      initialUsername: u.username,
      initialPassword: password,
      initialDisplayName: u.displayName,
      lockAccount: true,
    );
  }

  void _actions(BuildContext context, WidgetRef ref, User u, {required bool isMe}) {
    final api = ref.read(apiProvider);
    Future<void> run(Future<void> Function() f, String done) async {
      try {
        await f();
        if (!context.mounted) return;
        ref.invalidate(_usersProvider);
        showToast(context, done);
      } on ApiError catch (e) {
        if (context.mounted) showToast(context, e.message);
      }
    }

    showActionSheet(context, title: '${u.displayName}（${u.username}）', actions: [
      if (!u.disabled)
        SheetAction(
          AppIcons.magicWand,
          '生成免输登录口令',
          () => showInviteGeneratorSheet(
            context,
            ref,
            initialUsername: u.username,
            initialDisplayName: u.displayName,
          ),
          subtitle: '生成 #xn...xn# 加密口令，父母复制打开即自动登录',
        ),
      SheetAction(AppIcons.key, '重置密码', () async {
        final pw = await showTextEditor(context, title: '给 ${u.displayName} 设置新密码', initial: _randomPassword(), ok: '确定', maxLines: 1);
        if (pw == null || pw.isEmpty) return;
        await run(() => api.adminUpdateUser(u.id, {'password': pw}), '密码已重置');
        if (context.mounted) _showCredentials(context, ref, u, pw);
      }),
      if (!isMe)
        SheetAction(u.isAdmin ? AppIcons.shieldOff : AppIcons.shield, u.isAdmin ? '取消管理员' : '设为管理员',
            () => run(() => api.adminUpdateUser(u.id, {'role': u.isAdmin ? 'member' : 'admin'}), '已更新')),
      if (!isMe)
        SheetAction(u.disabled ? AppIcons.lockOpen : AppIcons.ban, u.disabled ? '恢复使用' : '停用账号（暂时不让登录）', () async {
          if (!u.disabled && !await confirm(context, title: '停用 ${u.displayName} 的账号？', body: 'TA 会立刻退出登录，聊天记录会保留。之后可以随时恢复。', ok: '停用', danger: true)) return;
          await run(() => api.adminUpdateUser(u.id, {'disabled': !u.disabled}), u.disabled ? '已恢复' : '已停用');
        }),
      if (!isMe)
        SheetAction(AppIcons.delete, '删除账号', () async {
          if (!await confirm(context, title: '删除 ${u.displayName} 的账号？', body: 'TA 的所有聊天记录也会一起删除，无法恢复。', ok: '删除', danger: true)) return;
          await run(() => api.adminDeleteUser(u.id), '已删除');
        }, danger: true),
    ]);
  }

  static String _randomPassword() {
    const chars = 'abcdefghjkmnpqrstuvwxyz23456789';
    final r = math.Random.secure();
    return List.generate(8, (_) => chars[r.nextInt(chars.length)]).join();
  }
}

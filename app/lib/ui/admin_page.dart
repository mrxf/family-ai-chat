import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models.dart';
import '../state/session.dart';
import 'settings_page.dart' show avatarChoices;
import 'sheets.dart';
import 'theme.dart';
import 'widgets.dart';

final _usersProvider = FutureProvider.autoDispose<List<User>>((ref) => ref.watch(apiProvider).adminUsers());

class AdminUsersPage extends ConsumerWidget {
  const AdminUsersPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final users = ref.watch(_usersProvider);
    final me = ref.watch(sessionProvider).user;
    if (me == null) return const Scaffold();

    return Scaffold(
      appBar: AppBar(title: const Text('家庭成员')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _create(context, ref),
        backgroundColor: p.accent,
        foregroundColor: Colors.white,
        icon: const Icon(Icons.person_add_alt_1_rounded),
        label: const Text('添加家人', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
      ),
      body: users.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text(ApiError.from(e).message)),
        data: (list) => RefreshIndicator(
          onRefresh: () => ref.refresh(_usersProvider.future),
          child: ListView.separated(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 100),
            itemCount: list.length,
            separatorBuilder: (_, _) => const SizedBox(height: 10),
            itemBuilder: (_, i) {
              final u = list[i];
              final disabled = u.settings['__disabled'] == true;
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
                        Text('用户名：${u.username}', style: TextStyle(fontSize: 14, color: p.muted)),
                      ]),
                    ),
                    Icon(Icons.more_horiz_rounded, color: p.faint),
                  ]),
                ),
              );
            },
          ),
        ),
      ),
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
    final ok = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
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
    final server = ref.read(sessionProvider).server.replaceFirst(RegExp(r'^https?://'), '');
    final text = '小暖登录信息\n服务器：$server\n用户名：${u.username}\n密码：$password';
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('已为「${u.displayName}」创建账号'),
        content: SelectableText(text, style: const TextStyle(fontSize: 16, height: 1.7)),
        actions: [
          TextButton(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: text));
              showToast(context, '已复制，可以发给家人');
            },
            child: const Text('复制'),
          ),
          FilledButton(onPressed: () => Navigator.pop(ctx), child: const Text('好的')),
        ],
      ),
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
      SheetAction(Icons.password_rounded, '重置密码', () async {
        final pw = await showTextEditor(context, title: '给 ${u.displayName} 设置新密码', initial: _randomPassword(), ok: '确定', maxLines: 1);
        if (pw == null || pw.isEmpty) return;
        await run(() => api.adminUpdateUser(u.id, {'password': pw}), '密码已重置');
        if (context.mounted) _showCredentials(context, ref, u, pw);
      }),
      if (!isMe)
        SheetAction(u.isAdmin ? Icons.remove_moderator_outlined : Icons.admin_panel_settings_outlined, u.isAdmin ? '取消管理员' : '设为管理员',
            () => run(() => api.adminUpdateUser(u.id, {'role': u.isAdmin ? 'member' : 'admin'}), '已更新')),
      if (!isMe)
        SheetAction(Icons.delete_outline_rounded, '删除账号', () async {
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

final _usageProvider = FutureProvider.autoDispose<(List<Map<String, dynamic>>, List<User>)>((ref) async {
  final api = ref.watch(apiProvider);
  return (await api.adminUsage(30), await api.adminUsers());
});

class AdminUsagePage extends ConsumerWidget {
  const AdminUsagePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final data = ref.watch(_usageProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('最近 30 天用量')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text(ApiError.from(e).message)),
        data: (d) {
          final (rows, users) = d;
          final byId = {for (final u in users) u.id: u};
          rows.sort((a, b) => ((b['completion_tokens'] as num) + (b['prompt_tokens'] as num)).compareTo((a['completion_tokens'] as num) + (a['prompt_tokens'] as num)));
          final maxTokens = rows.isEmpty ? 1 : rows.map((r) => (r['prompt_tokens'] as num) + (r['completion_tokens'] as num)).reduce((a, b) => a > b ? a : b);
          if (rows.isEmpty) return Center(child: Text('还没有使用记录', style: TextStyle(color: p.muted)));
          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              for (final r in rows)
                Builder(builder: (_) {
                  final u = byId[r['user_id']];
                  final total = (r['prompt_tokens'] as num) + (r['completion_tokens'] as num);
                  return Container(
                    margin: const EdgeInsets.only(bottom: 10),
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.line)),
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Row(children: [
                        Avatar(label: u?.displayName ?? '?', emoji: u?.avatar ?? '', size: 36),
                        const SizedBox(width: 10),
                        Expanded(child: Text(u?.displayName ?? '已删除用户', style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600))),
                        Text('${r['messages']} 次回答', style: TextStyle(color: p.muted)),
                      ]),
                      const SizedBox(height: 12),
                      ClipRRect(
                        borderRadius: BorderRadius.circular(6),
                        child: LinearProgressIndicator(value: total / maxTokens, minHeight: 10, backgroundColor: p.surfaceAlt, color: p.accent),
                      ),
                      const SizedBox(height: 8),
                      Text('${_fmt(total)} tokens（输入 ${_fmt(r['prompt_tokens'] as num)}，输出 ${_fmt(r['completion_tokens'] as num)}）',
                          style: TextStyle(fontSize: 13, color: p.muted)),
                    ]),
                  );
                }),
            ],
          );
        },
      ),
    );
  }

  static String _fmt(num n) => n >= 10000 ? '${(n / 10000).toStringAsFixed(1)} 万' : '$n';
}

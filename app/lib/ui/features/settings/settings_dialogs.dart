import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../state/session.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

const avatarChoices = ['', '👵', '👴', '👩', '👨', '👧', '👦', '🧑', '🐱', '🐶', '🐼', '🐰', '🦊', '🐻', '🌻', '🌈', '⭐', '🍀'];

/// Opens the profile editor (avatar + display name) as a bottom sheet on mobile
/// or a centered dialog on desktop.
Future<void> showEditProfileDialog(BuildContext context, WidgetRef ref) async {
  final user = ref.read(sessionProvider).user;
  if (user == null) return;
  final name = TextEditingController(text: user.displayName);
  var avatar = user.avatar;
  final ok = await showAdaptiveModal<bool>(
    context: context,
    maxWidth: 460,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, setS) {
        final p = ctx.pal;
        return SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(22, 4, 22, MediaQuery.viewInsetsOf(ctx).bottom + 22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('个人资料', style: Theme.of(ctx).textTheme.titleMedium),
              const SizedBox(height: 16),
              Center(child: Avatar(label: name.text, emoji: avatar, size: 72)),
              const SizedBox(height: 14),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                alignment: WrapAlignment.center,
                children: [
                  for (final e in avatarChoices)
                    Pressable(
                      onTap: () => setS(() => avatar = e),
                      child: Container(
                        decoration: BoxDecoration(
                          shape: BoxShape.circle,
                          border: Border.all(color: avatar == e ? p.accent : Colors.transparent, width: 2),
                        ),
                        padding: const EdgeInsets.all(2),
                        child: Avatar(label: name.text, emoji: e, size: 40),
                      ),
                    ),
                ],
              ),
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
            ],
          ),
        );
      },
    ),
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

/// Opens the password change dialog/sheet.
Future<void> showChangePasswordDialog(BuildContext context, WidgetRef ref) async {
  final old = TextEditingController();
  final nw = TextEditingController();
  final ok = await showAdaptiveModal<bool>(
    context: context,
    maxWidth: 440,
    builder: (ctx) => SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(22, 4, 22, MediaQuery.viewInsetsOf(ctx).bottom + 22),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('修改密码', style: Theme.of(ctx).textTheme.titleMedium),
          const SizedBox(height: 16),
          TextField(controller: old, obscureText: true, decoration: const InputDecoration(labelText: '原密码')),
          const SizedBox(height: 12),
          TextField(controller: nw, obscureText: true, decoration: const InputDecoration(labelText: '新密码（至少 6 位）')),
          const SizedBox(height: 18),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('确认修改')),
        ],
      ),
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

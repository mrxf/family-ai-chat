import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/session.dart';
import '../../../state/sharing.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';

/// Multi-select picker to share a conversation directly with household members.
Future<Conversation?> showFamilySharePickerSheet(
  BuildContext context,
  WidgetRef ref,
  Conversation conv,
) {
  return showAdaptiveModal<Conversation>(
    context: context,
    maxWidth: 460,
    builder: (_) => _FamilySharePickerBody(conv: conv),
  );
}

class _FamilySharePickerBody extends ConsumerStatefulWidget {
  const _FamilySharePickerBody({required this.conv});
  final Conversation conv;

  @override
  ConsumerState<_FamilySharePickerBody> createState() => _FamilySharePickerBodyState();
}

class _FamilySharePickerBodyState extends ConsumerState<_FamilySharePickerBody> {
  final Set<String> _selected = <String>{};
  bool _submitting = false;

  late final Set<String> _existingIds = {
    for (final m in widget.conv.members) m.userId,
  };

  Future<void> _submit() async {
    if (_selected.isEmpty || _submitting) return;
    setState(() => _submitting = true);
    try {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      final updated = await api.addConversationMembers(widget.conv.id, _selected.toList());
      ref.read(conversationsProvider.notifier).replaceConversation(updated);
      ref.invalidate(ownedSharedConversationsProvider);
      if (mounted) {
        Navigator.pop(context, updated);
        showToast(context, '已共享给 ${_selected.length} 位家人');
      }
    } on ApiError catch (e) {
      if (mounted) {
        setState(() => _submitting = false);
        showToast(context, e.message);
      }
    } catch (_) {
      if (mounted) {
        setState(() => _submitting = false);
        showToast(context, '共享失败，请稍后重试');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final membersAsync = ref.watch(familyMembersProvider);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('共享给家人', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700)),
            const SizedBox(height: 4),
            Text(
              '可多选家人一起加入对话，双方聊天与卡片都会同步更新（个人记忆不共享，记忆召回过程自动隐藏）',
              style: TextStyle(fontSize: 13, height: 1.45, color: p.muted),
            ),
            const SizedBox(height: 14),
            membersAsync.when(
              loading: () => const Padding(
                padding: EdgeInsets.symmetric(vertical: 28),
                child: Center(child: CircularProgressIndicator(strokeWidth: 2.2)),
              ),
              error: (e, _) => Padding(
                padding: const EdgeInsets.symmetric(vertical: 20),
                child: Text('加载家庭成员失败：$e', style: TextStyle(color: p.bad)),
              ),
              data: (members) {
                if (members.isEmpty) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(vertical: 24),
                    child: Text(
                      '家里暂时还没有其他成员账号，管理员可在「设置 → 家庭成员」中添加家人。',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 14, color: p.muted, height: 1.5),
                    ),
                  );
                }
                return ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 320),
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: members.length,
                    separatorBuilder: (_, _) => Divider(height: 1, color: p.line),
                    itemBuilder: (_, i) {
                      final m = members[i];
                      final alreadyIn = _existingIds.contains(m.id);
                      final checked = alreadyIn || _selected.contains(m.id);
                      return ListTile(
                        contentPadding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
                        leading: Avatar(label: m.displayName, emoji: m.avatar, size: 38),
                        title: Text(m.displayName, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                        subtitle: Text(
                          alreadyIn ? '已在共享中' : '@${m.username}',
                          style: TextStyle(fontSize: 13, color: alreadyIn ? p.accent : p.muted),
                        ),
                        trailing: AppIcon(
                          checked ? AppIcons.checkCircleFill : AppIcons.circle,
                          size: 22,
                          color: alreadyIn ? p.faint : (checked ? p.accent : p.faint),
                        ),
                        onTap: alreadyIn
                            ? null
                            : () => setState(() {
                                  if (!_selected.remove(m.id)) _selected.add(m.id);
                                }),
                      );
                    },
                  ),
                );
              },
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: _selected.isEmpty || _submitting ? null : _submit,
              child: _submitting
                  ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                  : Text(_selected.isEmpty ? '请选择家人' : '确认共享（${_selected.length} 人）'),
            ),
          ],
        ),
      ),
    );
  }
}

/// Manages participants of a shared conversation (add/remove/revoke for owner; view/leave for members).
Future<void> showSharedMembersSheet(
  BuildContext context,
  WidgetRef ref,
  Conversation initialConv,
) async {
  await showAdaptiveModal<void>(
    context: context,
    maxWidth: 460,
    builder: (_) => _SharedMembersSheetBody(convId: initialConv.id, initialConv: initialConv),
  );
}

class _SharedMembersSheetBody extends ConsumerStatefulWidget {
  const _SharedMembersSheetBody({required this.convId, required this.initialConv});
  final String convId;
  final Conversation initialConv;

  @override
  ConsumerState<_SharedMembersSheetBody> createState() => _SharedMembersSheetBodyState();
}

class _SharedMembersSheetBodyState extends ConsumerState<_SharedMembersSheetBody> {
  bool _busy = false;

  Future<void> _removeMember(Conversation conv, ConversationMember m) async {
    final ok = await confirm(
      context,
      title: '移出 ${m.displayName}？',
      body: '移出后对方将无法继续查看或参与这个共享对话。',
      ok: '移出',
      danger: true,
    );
    if (!ok || !mounted) return;
    setState(() => _busy = true);
    try {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      final updated = await api.removeConversationMember(conv.id, m.userId);
      if (updated != null) ref.read(conversationsProvider.notifier).replaceConversation(updated);
      ref.invalidate(ownedSharedConversationsProvider);
      if (mounted) {
        showToast(context, '已移出 ${m.displayName}');
        if (updated != null && !updated.isShared) Navigator.pop(context);
      }
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _revokeAll(Conversation conv) async {
    final ok = await confirm(
      context,
      title: '收回全部共享？',
      body: '收回后所有家人都将不再看到此对话，对话会恢复为你自己的普通个人对话。',
      ok: '收回共享',
      danger: true,
    );
    if (!ok || !mounted) return;
    setState(() => _busy = true);
    try {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      final updated = await api.revokeAllConversationMembers(conv.id);
      if (updated != null) ref.read(conversationsProvider.notifier).replaceConversation(updated);
      ref.invalidate(ownedSharedConversationsProvider);
      if (mounted) {
        Navigator.pop(context);
        showToast(context, '已收回共享，恢复为个人对话');
      }
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _leave(Conversation conv) async {
    final ok = await confirm(
      context,
      title: '退出这个共享对话？',
      body: '退出后仅从你的列表移除，不影响发起人和其他家人继续聊。',
      ok: '退出共享',
      danger: true,
    );
    if (!ok || !mounted) return;
    setState(() => _busy = true);
    try {
      await ref.read(conversationsProvider.notifier).delete(conv.id);
      if (ref.read(chatProvider).conversationId == conv.id) {
        ref.read(chatProvider.notifier).newChat();
      }
      if (mounted) {
        Navigator.pop(context);
        showToast(context, '已退出共享对话');
      }
    } catch (_) {
      if (mounted) showToast(context, '退出失败，请检查网络');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final conv = ref.watch(conversationsProvider).items.where((c) => c.id == widget.convId).firstOrNull ?? widget.initialConv;
    final members = conv.members;

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(children: [
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  const Text('共享成员管理', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700)),
                  const SizedBox(height: 2),
                  Text('共 ${conv.memberCount > 0 ? conv.memberCount : members.length} 人参与 · 对话与卡片实时同步',
                      style: TextStyle(fontSize: 13, color: p.muted)),
                ]),
              ),
              if (conv.isOwner)
                TextButton.icon(
                  icon: AppIcon(AppIcons.userPlus, size: 17, color: p.accent),
                  label: const Text('添加家人'),
                  onPressed: _busy ? null : () => showFamilySharePickerSheet(context, ref, conv),
                ),
            ]),
            const SizedBox(height: 12),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 280),
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: members.length,
                separatorBuilder: (_, _) => Divider(height: 1, color: p.line),
                itemBuilder: (_, i) {
                  final m = members[i];
                  return ListTile(
                    contentPadding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
                    leading: Avatar(label: m.displayName, emoji: m.avatar, size: 36),
                    title: Row(children: [
                      Flexible(
                        child: Text(m.displayName, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600)),
                      ),
                      if (m.isOwner) ...[
                        const SizedBox(width: 6),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                          decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(6)),
                          child: Text('发起人', style: TextStyle(fontSize: 11.5, color: p.accent, fontWeight: FontWeight.w600)),
                        ),
                      ],
                    ]),
                    trailing: conv.isOwner && !m.isOwner
                        ? TextButton(
                            onPressed: _busy ? null : () => _removeMember(conv, m),
                            child: Text('移出', style: TextStyle(color: p.bad)),
                          )
                        : null,
                  );
                },
              ),
            ),
            const SizedBox(height: 16),
            if (conv.isOwner)
              OutlinedButton.icon(
                style: OutlinedButton.styleFrom(foregroundColor: p.bad, side: BorderSide(color: p.bad.withValues(alpha: 0.4))),
                icon: AppIcon(AppIcons.ban, size: 17, color: p.bad),
                label: const Text('收回全部共享'),
                onPressed: _busy ? null : () => _revokeAll(conv),
              )
            else
              OutlinedButton.icon(
                style: OutlinedButton.styleFrom(foregroundColor: p.bad, side: BorderSide(color: p.bad.withValues(alpha: 0.4))),
                icon: AppIcon(AppIcons.close, size: 17, color: p.bad),
                label: const Text('退出共享'),
                onPressed: _busy ? null : () => _leave(conv),
              ),
          ],
        ),
      ),
    );
  }
}

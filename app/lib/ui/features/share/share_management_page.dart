import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../core/share_code.dart';
import '../../../state/conversations.dart';
import '../../../state/session.dart';
import '../../../state/sharing.dart';
import '../../adaptive/breakpoints.dart';
import '../../adaptive/readable_width.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import 'family_share_sheet.dart';

/// Dedicated module for managing active family shared conversations (revoke/manage members)
/// and issued short-code share links (re-copy/revoke).
class ShareManagementPage extends ConsumerStatefulWidget {
  const ShareManagementPage({super.key});

  @override
  ConsumerState<ShareManagementPage> createState() => _ShareManagementPageState();
}

class _ShareManagementPageState extends ConsumerState<ShareManagementPage> with SingleTickerProviderStateMixin {
  late final TabController _tabs = TabController(length: 2, vsync: this);

  @override
  void dispose() {
    _tabs.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Scaffold(
      appBar: AppBar(
        title: const Text('共享与分享管理'),
        bottom: TabBar(
          controller: _tabs,
          labelColor: p.accent,
          unselectedLabelColor: p.muted,
          indicatorColor: p.accent,
          tabs: const [
            Tab(text: '共享中的对话'),
            Tab(text: '发出的短口令'),
          ],
        ),
      ),
      body: ReadableWidth(
        maxWidth: ContentWidth.page,
        child: TabBarView(
          controller: _tabs,
          children: const [
            _OwnedSharedTab(),
            _UserSharesTab(),
          ],
        ),
      ),
    );
  }
}

class _OwnedSharedTab extends ConsumerWidget {
  const _OwnedSharedTab();

  Future<void> _revokeAll(BuildContext context, WidgetRef ref, Conversation c) async {
    final ok = await confirm(
      context,
      title: '收回《${c.title.isEmpty ? '新对话' : c.title}》的共享？',
      body: '收回后所有家人都将不再看到此对话，对话会恢复为你自己的个人对话。',
      ok: '收回共享',
      danger: true,
    );
    if (!ok || !context.mounted) return;
    try {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      final updated = await api.revokeAllConversationMembers(c.id);
      if (updated != null) ref.read(conversationsProvider.notifier).replaceConversation(updated);
      ref.invalidate(ownedSharedConversationsProvider);
      if (context.mounted) showToast(context, '已收回共享');
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final async = ref.watch(ownedSharedConversationsProvider);
    return RefreshIndicator(
      onRefresh: () async => ref.invalidate(ownedSharedConversationsProvider),
      child: async.when(
        loading: () => const Center(child: CircularProgressIndicator(strokeWidth: 2.2)),
        error: (e, _) => Center(child: Text('加载失败：$e', style: TextStyle(color: p.bad))),
        data: (items) {
          if (items.isEmpty) {
            return ListView(children: [
              const SizedBox(height: 100),
              Center(
                child: Text(
                  '你还没有发起共享对话\n在对话右上角点「分享 → 共享给家人」即可开始',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: p.faint, fontSize: 15, height: 1.6),
                ),
              ),
            ]);
          }
          return ListView.separated(
            padding: const EdgeInsets.all(16),
            itemCount: items.length,
            separatorBuilder: (_, _) => const SizedBox(height: 10),
            itemBuilder: (_, i) {
              final c = items[i];
              final others = c.members.where((m) => !m.isOwner).map((m) => m.displayName).join('、');
              return Container(
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: p.surface,
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: p.line),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(children: [
                      Expanded(
                        child: Text(
                          c.title.isEmpty ? '未命名对话' : c.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                        ),
                      ),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(8)),
                        child: Text('${c.memberCount} 人', style: TextStyle(fontSize: 12, color: p.accent, fontWeight: FontWeight.w600)),
                      ),
                    ]),
                    const SizedBox(height: 6),
                    Text('已共享给：${others.isEmpty ? '家人' : others}', style: TextStyle(fontSize: 13.5, color: p.muted)),
                    const SizedBox(height: 12),
                    Row(children: [
                      OutlinedButton.icon(
                        icon: AppIcon(AppIcons.family, size: 16, color: p.text),
                        label: const Text('管理成员'),
                        onPressed: () async {
                          await showSharedMembersSheet(context, ref, c);
                          ref.invalidate(ownedSharedConversationsProvider);
                        },
                      ),
                      const SizedBox(width: 8),
                      TextButton.icon(
                        icon: AppIcon(AppIcons.ban, size: 16, color: p.bad),
                        label: Text('收回共享', style: TextStyle(color: p.bad)),
                        onPressed: () => _revokeAll(context, ref, c),
                      ),
                    ]),
                  ],
                ),
              );
            },
          );
        },
      ),
    );
  }
}

class _UserSharesTab extends ConsumerWidget {
  const _UserSharesTab();

  String _formatDate(int ms) {
    if (ms <= 0) return '长期有效';
    final d = DateTime.fromMillisecondsSinceEpoch(ms);
    return '${d.month}月${d.day}日到期';
  }

  Future<void> _revoke(BuildContext context, WidgetRef ref, ConversationShare s) async {
    final ok = await confirm(
      context,
      title: '撤销这条分享口令？',
      body: '撤销后别人将无法再通过该口令导入；已经导入的副本不受影响。',
      ok: '撤销',
      danger: true,
    );
    if (!ok || !context.mounted) return;
    try {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      await api.revokeShare(s.id);
      ref.invalidate(userSharesProvider);
      if (context.mounted) showToast(context, '口令已撤销');
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final async = ref.watch(userSharesProvider);
    final myName = ref.watch(sessionProvider).user?.displayName;
    return RefreshIndicator(
      onRefresh: () async => ref.invalidate(userSharesProvider),
      child: async.when(
        loading: () => const Center(child: CircularProgressIndicator(strokeWidth: 2.2)),
        error: (e, _) => Center(child: Text('加载失败：$e', style: TextStyle(color: p.bad))),
        data: (items) {
          if (items.isEmpty) {
            return ListView(children: [
              const SizedBox(height: 100),
              Center(
                child: Text(
                  '还没有发出过短口令\n在对话中点「分享 → 分享短口令」即可生成',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: p.faint, fontSize: 15, height: 1.6),
                ),
              ),
            ]);
          }
          return ListView.separated(
            padding: const EdgeInsets.all(16),
            itemCount: items.length,
            separatorBuilder: (_, _) => const SizedBox(height: 10),
            itemBuilder: (_, i) {
              final s = items[i];
              return Container(
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: p.surface,
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: p.line),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      s.title.isEmpty ? '分享的对话' : s.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '${s.messageCount} 条消息 · ${s.isExpired ? '已过期' : _formatDate(s.expiresAt)} · ${XnShareCodec.encode(s.code)}',
                      style: TextStyle(fontSize: 12.5, color: s.isExpired ? p.bad : p.muted),
                    ),
                    const SizedBox(height: 10),
                    Row(children: [
                      if (!s.isExpired)
                        OutlinedButton.icon(
                          icon: AppIcon(AppIcons.copy, size: 15, color: p.text),
                          label: const Text('复制口令'),
                          onPressed: () async {
                            final msg = XnShareCodec.formatShareMessage(title: s.title, code: s.code, creatorName: myName);
                            ShareCodeGuard.markGeneratedLocally(msg);
                            await Clipboard.setData(ClipboardData(text: msg));
                            if (context.mounted) showToast(context, '已复制分享口令');
                          },
                        ),
                      if (!s.isExpired) const SizedBox(width: 8),
                      TextButton.icon(
                        icon: AppIcon(AppIcons.delete, size: 15, color: p.bad),
                        label: Text('撤销', style: TextStyle(color: p.bad)),
                        onPressed: () => _revoke(context, ref, s),
                      ),
                    ]),
                  ],
                ),
              );
            },
          );
        },
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../state/chat.dart';
import '../state/conversations.dart';
import '../state/session.dart';
import 'admin/common.dart' show Tag;
import 'icons.dart';
import 'search_page.dart';
import 'self_host_page.dart';
import 'settings_page.dart';
import 'theme.dart';
import 'widgets.dart';

class HistoryDrawer extends ConsumerWidget {
  const HistoryDrawer({super.key});

  void _open(BuildContext context, WidgetRef ref, Conversation c) {
    FocusManager.instance.primaryFocus?.unfocus();
    Navigator.pop(context);
    ref.read(chatProvider.notifier).open(c.id);
  }

  Future<void> _openSearch(BuildContext context, WidgetRef ref) async {
    final convId = await openSearchPage(context);
    if (convId != null && context.mounted) {
      FocusManager.instance.primaryFocus?.unfocus();
      Navigator.pop(context);
      ref.read(chatProvider.notifier).open(convId);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final list = ref.watch(conversationsProvider);
    final currentId = ref.watch(chatProvider.select((s) => s.conversationId));
    final isPrivate = ref.watch(chatProvider.select((s) => s.isPrivate));
    final session = ref.watch(sessionProvider);
    final user = session.user;
    final items = list.items;

    final groups = <String, List<Conversation>>{};
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);
    for (final c in items) {
      final d = DateTime.fromMillisecondsSinceEpoch(c.updatedAt);
      final key = c.pinned
          ? '置顶'
          : !d.isBefore(today)
              ? '今天'
              : !d.isBefore(today.subtract(const Duration(days: 1)))
                  ? '昨天'
                  : !d.isBefore(today.subtract(const Duration(days: 7)))
                      ? '最近 7 天'
                      : !d.isBefore(today.subtract(const Duration(days: 30)))
                          ? '最近 30 天'
                          : '更早';
      (groups[key] ??= []).add(c);
    }
    const order = ['置顶', '今天', '昨天', '最近 7 天', '最近 30 天', '更早'];

    return Drawer(
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 10, 10, 8),
              child: Row(children: [
                const Mascot(size: 36),
                const SizedBox(width: 10),
                const Text('小暖', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700)),
                if (session.isSelfHosted) ...[
                  const SizedBox(width: 8),
                  Pressable(
                    scale: 0.95,
                    onTap: () {
                      Navigator.pop(context);
                      showSelfHostInfoSheet(context, ref);
                    },
                    child: Tag('自托管', color: p.accent, icon: AppIcons.server),
                  ),
                ],
                const Spacer(),
                SoftIconButton(
                  icon: AppIcons.search,
                  tooltip: '搜索聊天记录',
                  size: 42,
                  onTap: () => _openSearch(context, ref),
                ),
                SoftIconButton(
                  icon: AppIcons.close,
                  tooltip: '关闭',
                  size: 42,
                  onTap: () => Navigator.pop(context),
                ),
              ]),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
              child: Pressable(
                scale: 0.98,
                onTap: () {
                  Navigator.pop(context);
                  ref.read(chatProvider.notifier).newChat();
                },
                child: Container(
                  height: 50,
                  decoration: BoxDecoration(gradient: Palette.brandGradient, borderRadius: BorderRadius.circular(16), boxShadow: [
                    BoxShadow(color: Palette.brand.withValues(alpha: 0.28), blurRadius: 14, offset: const Offset(0, 5)),
                  ]),
                  child: const Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                    AppIcon(AppIcons.plus, color: Colors.white, size: 21),
                    SizedBox(width: 6),
                    Text('新对话', style: TextStyle(color: Colors.white, fontSize: 17, fontWeight: FontWeight.w600)),
                  ]),
                ),
              ),
            ),
            Expanded(
              child: RefreshIndicator(
                onRefresh: () => ref.read(conversationsProvider.notifier).refresh(),
                child: items.isEmpty
                    ? ListView(children: [
                        const SizedBox(height: 80),
                        Center(
                          child: Text(
                            list.loaded ? '还没有聊天记录\n点上面的「新对话」开始吧' : '加载中…',
                            textAlign: TextAlign.center,
                            style: TextStyle(color: p.faint, fontSize: 15, height: 1.6),
                          ),
                        ),
                      ])
                    : NotificationListener<ScrollNotification>(
                        onNotification: (n) {
                          if (n.metrics.extentAfter < 400) ref.read(conversationsProvider.notifier).loadMore();
                          return false;
                        },
                        child: ListView(
                          padding: const EdgeInsets.fromLTRB(10, 4, 10, 16),
                          children: [
                            for (final g in order)
                              if (groups[g] != null) ...[
                                Padding(
                                  padding: const EdgeInsets.fromLTRB(10, 14, 10, 6),
                                  child: Text(g, style: TextStyle(fontSize: 13.5, color: p.faint, fontWeight: FontWeight.w600)),
                                ),
                                for (final c in groups[g]!)
                                  _ConvTile(
                                    conv: c,
                                    selected: !isPrivate && c.id == currentId,
                                    onTap: () => _open(context, ref, c),
                                    onLongPress: () => showConversationMenu(context, ref, c),
                                  ),
                              ],
                          ],
                        ),
                      ),
              ),
            ),
            Divider(color: p.line, height: 1),
            if (user != null)
              InkWell(
                onTap: () {
                  Navigator.pop(context);
                  Navigator.of(context).push(MaterialPageRoute(builder: (_) => const SettingsPage()));
                },
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(18, 12, 12, 12),
                  child: Row(children: [
                    Avatar(label: user.displayName, emoji: user.avatar, size: 42),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text(user.displayName, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
                    ),
                    AppIcon(AppIcons.settings, color: p.muted, size: 22),
                    const SizedBox(width: 6),
                  ]),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _ConvTile extends StatelessWidget {
  const _ConvTile({required this.conv, required this.selected, required this.onTap, required this.onLongPress});
  final Conversation conv;
  final bool selected;
  final VoidCallback onTap, onLongPress;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.98,
      onTap: onTap,
      onLongPress: onLongPress,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        margin: const EdgeInsets.symmetric(vertical: 1),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
        decoration: BoxDecoration(color: selected ? p.surface : Colors.transparent, borderRadius: BorderRadius.circular(14)),
        child: Row(children: [
          if (conv.pinned) Padding(padding: const EdgeInsets.only(right: 6), child: AppIcon(AppIcons.pin, size: 15, color: p.accent)),
          Expanded(
            child: Text(
              conv.title.isEmpty ? '新对话' : conv.title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 16, fontWeight: selected ? FontWeight.w600 : FontWeight.w400, color: p.text),
            ),
          ),
        ]),
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/schedules.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../admin/admin_common.dart' show Tag;
import '../auth/self_host_info_sheet.dart';
import '../schedules/schedules_page.dart';
import '../settings/settings_page.dart';
import 'search_page.dart';

/// Compact windows: the conversation list slides in as a drawer.
class HistoryDrawer extends StatelessWidget {
  const HistoryDrawer({super.key});

  @override
  Widget build(BuildContext context) => const Drawer(child: SafeArea(child: ConversationPanel(inDrawer: true)));
}

/// Conversation history with search, new chat, schedules and the account row.
///
/// Shown inside [HistoryDrawer] on compact windows and as a permanent
/// sidebar on expanded ones; [inDrawer] decides whether actions close it.
class ConversationPanel extends ConsumerWidget {
  const ConversationPanel({super.key, this.inDrawer = false});

  final bool inDrawer;

  void _dismiss(BuildContext context) {
    if (inDrawer) Navigator.pop(context);
  }

  void _open(BuildContext context, WidgetRef ref, Conversation c) {
    FocusManager.instance.primaryFocus?.unfocus();
    _dismiss(context);
    ref.read(chatProvider.notifier).open(c.id);
  }

  Future<void> _openSearch(BuildContext context, WidgetRef ref) async {
    final convId = await openSearchPage(context);
    if (convId != null && context.mounted) {
      FocusManager.instance.primaryFocus?.unfocus();
      _dismiss(context);
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
    final todayPending = ref.watch(schedulesProvider).value?.todayPending ?? 0;

    final groups = groupByRecency(items, DateTime.now());

    return Column(
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
                  _dismiss(context);
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
            if (inDrawer)
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
              _dismiss(context);
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
                        for (final g in groups) ...[
                            Padding(
                              padding: const EdgeInsets.fromLTRB(10, 14, 10, 6),
                              child: Text(g.label, style: TextStyle(fontSize: 13.5, color: p.faint, fontWeight: FontWeight.w600)),
                            ),
                            for (final c in g.items)
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
        if (user != null) ...[
          InkWell(
            onTap: () {
              _dismiss(context);
              Navigator.of(context).push(appRoute((_) => const SchedulesPage()));
            },
            child: Padding(
              padding: const EdgeInsets.fromLTRB(18, 11, 14, 11),
              child: Row(children: [
                AppIcon(AppIcons.calendar, color: p.accent, size: 21),
                const SizedBox(width: 12),
                const Expanded(
                  child: Text('我的日程', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w500)),
                ),
                if (todayPending > 0)
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                    decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(10)),
                    child: Text(
                      '今天 $todayPending',
                      style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.accent),
                    ),
                  ),
                const SizedBox(width: 4),
                AppIcon(AppIcons.chevronRight, color: p.faint, size: 17),
              ]),
            ),
          ),
          Divider(color: p.line, height: 1),
          InkWell(
            onTap: () {
              _dismiss(context);
              Navigator.of(context).push(appRoute((_) => const SettingsPage()));
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
      ],
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

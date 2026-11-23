import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/desktop_nav.dart';
import '../../../state/schedules.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../adaptive/breakpoints.dart';
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
    if (!inDrawer && context.windowSize.isExpanded) {
      ref.read(desktopNavProvider.notifier).showChat();
    }
    ref.read(chatProvider.notifier).open(c.id);
  }

  Future<void> _openSearch(BuildContext context, WidgetRef ref) async {
    final convId = await openSearchPage(context);
    if (convId != null && context.mounted) {
      FocusManager.instance.primaryFocus?.unfocus();
      _dismiss(context);
      if (!inDrawer && context.windowSize.isExpanded) {
        ref.read(desktopNavProvider.notifier).showChat();
      }
      ref.read(chatProvider.notifier).open(convId);
    }
  }

  void _openSchedules(BuildContext context, WidgetRef ref) {
    _dismiss(context);
    if (!inDrawer && context.windowSize.isExpanded) {
      final nav = ref.read(desktopNavProvider);
      if (nav.section == DesktopSection.schedules) {
        ref.read(desktopNavProvider.notifier).showChat();
      } else {
        ref.read(desktopNavProvider.notifier).showSchedules();
      }
      return;
    }
    Navigator.of(context).push(appRoute((_) => const SchedulesPage()));
  }

  void _openSettings(BuildContext context, WidgetRef ref) {
    _dismiss(context);
    if (!inDrawer && context.windowSize.isExpanded) {
      ref.read(desktopNavProvider.notifier).openSettings();
      return;
    }
    Navigator.of(context).push(appRoute((_) => const SettingsPage()));
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
    final schedules = ref.watch(schedulesProvider).value;
    final pendingSchedules = schedules?.pendingTotal ?? 0;
    final todayPending = schedules?.todayPending ?? 0;
    final desktopSection = !inDrawer && context.windowSize.isExpanded
        ? ref.watch(desktopNavProvider.select((s) => s.section))
        : DesktopSection.chat;
    final schedulesSelected = desktopSection == DesktopSection.schedules;

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
              if (!inDrawer && context.windowSize.isExpanded) {
                ref.read(desktopNavProvider.notifier).showChat();
              }
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
                                selected: desktopSection == DesktopSection.chat && !isPrivate && c.id == currentId,
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
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 10, 8),
            child: Row(children: [
              Expanded(
                child: InkWell(
                  borderRadius: BorderRadius.circular(14),
                  onTap: () => _openSettings(context, ref),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
                    child: Row(children: [
                      Avatar(label: user.displayName, emoji: user.avatar, size: 38),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Text(
                          user.displayName,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                        ),
                      ),
                    ]),
                  ),
                ),
              ),
              if (pendingSchedules > 0) ...[
                const SizedBox(width: 4),
                Tooltip(
                  message: todayPending > 0
                      ? '我的日程 · 今天 $todayPending 项待办，共 $pendingSchedules 项未完成'
                      : '我的日程 · $pendingSchedules 项未完成',
                  child: Semantics(
                    button: true,
                    label: '我的日程，$pendingSchedules 项未完成',
                    child: Pressable(
                      scale: 0.94,
                      onTap: () => _openSchedules(context, ref),
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 160),
                        constraints: const BoxConstraints(minHeight: 34),
                        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 6),
                        decoration: BoxDecoration(
                          color: schedulesSelected ? p.accent : p.accentSoft,
                          borderRadius: BorderRadius.circular(999),
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            AppIcon(AppIcons.calendar, color: schedulesSelected ? Colors.white : p.accent, size: 15.5),
                            const SizedBox(width: 4),
                            Text(
                              '$pendingSchedules',
                              style: TextStyle(
                                fontSize: 12.5,
                                fontWeight: FontWeight.w700,
                                color: schedulesSelected ? Colors.white : p.accent,
                                height: 1,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ],
              const SizedBox(width: 2),
              Tooltip(
                message: '设置',
                child: Pressable(
                  scale: 0.92,
                  onTap: () => _openSettings(context, ref),
                  child: Padding(
                    padding: const EdgeInsets.all(8),
                    child: AppIcon(AppIcons.settings, color: p.muted, size: 21),
                  ),
                ),
              ),
            ]),
          ),
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

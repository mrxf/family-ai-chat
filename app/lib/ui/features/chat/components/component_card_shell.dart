import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../platform/platform_capabilities.dart';
import '../../../../state/chat.dart';
import '../../../../state/conversations.dart';
import '../../../../state/pinned_components.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/sheets.dart';

/// Returns the representative icon for a component [kind].
IconData componentKindIcon(String kind) => switch (kind) {
      'weather' => AppIcons.cloudSun,
      'todo' => AppIcons.listChecks,
      'chart' => AppIcons.chart,
      'metrics' => AppIcons.gauge,
      'quiz' => AppIcons.question,
      _ => AppIcons.cards,
    };

/// Interactive wrapper for a pinnable component inside a conversation message.
///
/// Adds non-intrusive pin / quote actions below the card (revealed on hover on
/// desktop, subtly available on touch, and always accessible via long-press /
/// right-click) without obscuring any card interior.
class ChatComponentActionShell extends ConsumerStatefulWidget {
  const ChatComponentActionShell({
    super.key,
    required this.messageId,
    required this.component,
    required this.child,
    this.enabled = true,
  });

  final String messageId;
  final MessageComponent component;
  final Widget child;
  final bool enabled;

  @override
  ConsumerState<ChatComponentActionShell> createState() => _ChatComponentActionShellState();
}

class _ChatComponentActionShellState extends ConsumerState<ChatComponentActionShell> {
  bool _hovered = false;

  Future<void> _togglePin(bool isPinned) async {
    final notifier = ref.read(pinnedComponentsProvider.notifier);
    if (isPinned) {
      final err = await notifier.unpin(widget.messageId, widget.component.id);
      if (mounted) showToast(context, err ?? '已取消固定');
      return;
    }
    final chat = ref.read(chatProvider);
    final convId = chat.conversationId ?? '';
    final title = convId.isEmpty ? '' : (ref.read(conversationsTitlesProvider)[convId] ?? '');
    final err = await notifier.pin(
      messageId: widget.messageId,
      component: widget.component,
      conversationId: convId,
      conversationTitle: title,
    );
    if (mounted) showToast(context, err ?? '已固定到首页');
  }

  void _quoteInNewChat() {
    final chat = ref.read(chatProvider);
    final convId = chat.conversationId ?? '';
    final title = convId.isEmpty ? '' : (ref.read(conversationsTitlesProvider)[convId] ?? '');
    final quote = QuotedComponent.fromComponent(
      messageId: widget.messageId,
      component: widget.component,
      conversationId: convId,
      conversationTitle: title,
    );
    ref.read(chatProvider.notifier).quoteComponentToNewChat(quote);
  }

  void _openMenu(bool isPinned) {
    showActionSheet(context, actions: [
      SheetAction(
        isPinned ? AppIcons.unpin : AppIcons.pin,
        isPinned ? '从首页取消固定' : '固定到首页',
        () => _togglePin(isPinned),
      ),
      SheetAction(AppIcons.chatBubble, '基于此卡片开新对话', _quoteInNewChat),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.enabled || !isComponentPinnable(widget.component)) {
      return widget.child;
    }
    final isDesktop = ref.watch(platformCapabilitiesProvider).isDesktop;
    final isPinned = ref.watch(
      pinnedComponentsProvider.select((s) => s.isPinned(widget.messageId, widget.component.id)),
    );
    final showBar = !isDesktop || _hovered || isPinned;

    return MouseRegion(
      onEnter: isDesktop ? (_) => setState(() => _hovered = true) : null,
      onExit: isDesktop ? (_) => setState(() => _hovered = false) : null,
      child: GestureDetector(
        behavior: HitTestBehavior.translucent,
        onSecondaryTap: () => _openMenu(isPinned),
        onLongPress: () => _openMenu(isPinned),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            widget.child,
            AnimatedOpacity(
              opacity: showBar ? 1.0 : 0.0,
              duration: const Duration(milliseconds: 160),
              child: IgnorePointer(
                ignoring: !showBar,
                child: Padding(
                  padding: const EdgeInsets.only(top: 5, left: 4, right: 4),
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      _InlineActionPill(
                        icon: isPinned ? AppIcons.pin : AppIcons.pin,
                        label: isPinned ? '已固定到首页' : '固定到首页',
                        accent: isPinned,
                        tooltip: isPinned ? '点击取消固定' : '固定到首页看板',
                        onTap: () => _togglePin(isPinned),
                      ),
                      const SizedBox(width: 8),
                      _InlineActionPill(
                        icon: AppIcons.chatBubble,
                        label: '引用提问',
                        tooltip: '基于这张卡片开启新对话',
                        onTap: _quoteInNewChat,
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Wrapper for a pinned component rendered on the home board.
///
/// Displays source conversation metadata and quick actions (jump to original
/// conversation, quote into a new chat, unpin) with both hover/click and
/// right-click/long-press support.
class HomePinnedComponentShell extends ConsumerWidget {
  const HomePinnedComponentShell({
    super.key,
    required this.pin,
    required this.child,
  });

  final PinnedComponent pin;
  final Widget child;

  void _openSource(WidgetRef ref) {
    if (pin.conversationId.isEmpty) return;
    ref.read(chatProvider.notifier).open(pin.conversationId, focusMessageId: pin.messageId);
  }

  void _quoteInNewChat(WidgetRef ref) {
    final quote = QuotedComponent.fromComponent(
      messageId: pin.messageId,
      component: pin.component,
      conversationId: pin.conversationId,
      conversationTitle: pin.conversationTitle,
    );
    ref.read(chatProvider.notifier).quoteComponentToNewChat(quote);
  }

  Future<void> _unpin(BuildContext context, WidgetRef ref) async {
    final err = await ref.read(pinnedComponentsProvider.notifier).unpin(pin.messageId, pin.componentId);
    if (context.mounted) showToast(context, err ?? '已从首页取消固定');
  }

  void _openMenu(BuildContext context, WidgetRef ref) {
    showActionSheet(context, actions: [
      if (pin.conversationId.isNotEmpty)
        SheetAction(AppIcons.arrowUpRight, '跳转到原对话', () => _openSource(ref)),
      SheetAction(AppIcons.chatBubble, '基于此卡片开新对话', () => _quoteInNewChat(ref)),
      SheetAction(AppIcons.unpin, '取消固定', () => _unpin(context, ref), danger: true),
    ]);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final sourceTitle = pin.conversationTitle.trim().isEmpty ? '原对话' : pin.conversationTitle.trim();

    return GestureDetector(
      behavior: HitTestBehavior.translucent,
      onSecondaryTap: () => _openMenu(context, ref),
      onLongPress: () => _openMenu(context, ref),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          child,
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 6, 4, 2),
            child: Row(
              children: [
                Expanded(
                  child: Tooltip(
                    message: '跳转到原对话：$sourceTitle',
                    child: Pressable(
                      scale: 0.98,
                      onTap: pin.conversationId.isEmpty ? null : () => _openSource(ref),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          AppIcon(AppIcons.arrowUpRight, size: 13.5, color: p.faint),
                          const SizedBox(width: 4),
                          Flexible(
                            child: Text(
                              sourceTitle,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(fontSize: 12.5, color: p.muted),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
                const SizedBox(width: 6),
                _HomeIconAction(
                  icon: AppIcons.chatBubble,
                  tooltip: '基于此卡片聊一聊',
                  onTap: () => _quoteInNewChat(ref),
                ),
                const SizedBox(width: 2),
                _HomeIconAction(
                  icon: AppIcons.unpin,
                  tooltip: '取消固定',
                  onTap: () => _unpin(context, ref),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineActionPill extends StatelessWidget {
  const _InlineActionPill({
    required this.icon,
    required this.label,
    required this.tooltip,
    required this.onTap,
    this.accent = false,
  });

  final IconData icon;
  final String label;
  final String tooltip;
  final VoidCallback onTap;
  final bool accent;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: tooltip,
      child: Pressable(
        scale: 0.96,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
          decoration: BoxDecoration(
            color: accent ? p.accentSoft : p.surfaceAlt.withValues(alpha: 0.72),
            borderRadius: BorderRadius.circular(10),
            border: Border.all(
              color: accent ? p.accent.withValues(alpha: 0.28) : p.line.withValues(alpha: 0.65),
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              AppIcon(icon, size: 13.5, color: accent ? p.accent : p.muted),
              const SizedBox(width: 4),
              Text(
                label,
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: accent ? FontWeight.w600 : FontWeight.w500,
                  color: accent ? p.accent : p.muted,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HomeIconAction extends StatelessWidget {
  const _HomeIconAction({required this.icon, required this.tooltip, required this.onTap});
  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: tooltip,
      child: Pressable(
        scale: 0.92,
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(6),
          child: AppIcon(icon, size: 16, color: p.muted),
        ),
      ),
    );
  }
}

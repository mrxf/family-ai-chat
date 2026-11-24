import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/session.dart';
import '../../../state/sharing.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import '../share/share_page.dart';

/// Top bar of the conversation: title + model picker, share, new chat, and
/// the exit button of a private chat. Shared by the phone and desktop layouts.
class ChatAppBar extends ConsumerWidget implements PreferredSizeWidget {
  const ChatAppBar({super.key, this.leading, required this.onExitPrivate});

  /// The drawer button on compact layouts; null when the sidebar is visible.
  final Widget? leading;

  /// Clears what the user typed before a private chat is wiped.
  final VoidCallback onExitPrivate;

  @override
  Size get preferredSize => const Size.fromHeight(kToolbarHeight);

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final chat = ref.watch(chatProvider);
    final conv = ref.watch(currentConversationProvider);
    final p = context.pal;
    return AppBar(
      leading: leading,
      automaticallyImplyLeading: false,
      title: const _ChatTitle(),
      actions: [
        if (chat.isPrivate)
          _ExitPrivateButton(
            onTap: () {
              FocusScope.of(context).unfocus();
              onExitPrivate();
              ref.read(chatProvider.notifier).exitPrivateChat();
            },
          )
        else ...[
          if (conv != null && conv.isShared)
            Padding(
              padding: const EdgeInsets.only(right: 2),
              child: Tooltip(
                message: conv.isOwner ? '管理共享成员或收回共享' : '查看共享成员',
                child: Pressable(
                  scale: 0.95,
                  onTap: () => showSharedMembersSheet(context, ref, conv),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                    decoration: BoxDecoration(
                      color: p.accentSoft,
                      borderRadius: BorderRadius.circular(18),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        AppIcon(AppIcons.family, size: 15, color: p.accent),
                        const SizedBox(width: 4),
                        Text(
                          '共享${conv.memberCount > 1 ? ' · ${conv.memberCount}' : ''}',
                          style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: p.accent),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          if (!chat.isEmpty && !chat.loading)
            IconButton(tooltip: '分享对话', icon: const AppIcon(AppIcons.share, size: 22), onPressed: () => openShareChat(context, ref)),
          IconButton(
            tooltip: '新对话',
            icon: const AppIcon(AppIcons.newChat, size: 22),
            onPressed: chat.conversationId == null && chat.assistantId == null ? null : () => ref.read(chatProvider.notifier).newChat(),
          ),
          const SizedBox(width: 4),
        ],
      ],
    );
  }
}

/// Conversation title with the model underneath; tap to switch models.
class _ChatTitle extends ConsumerWidget {
  const _ChatTitle();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final chat = ref.watch(chatProvider);
    final boot = ref.watch(sessionProvider).boot;
    if (boot == null) return const SizedBox.shrink();
    final model = boot.effectiveModel(chat.modelId, chat.assistantId);
    final isAuto = model?.id == kAutoModelId;
    final resolvedModel = isAuto ? boot.model(chat.resolvedModelId ?? _lastAssistantModel(chat.messages)) : null;
    final assistant = boot.assistant(chat.assistantId);
    final title = chat.isPrivate
        ? (assistant == null ? '隐私对话' : '隐私对话 · ${assistant.name}')
        : chat.conversationId == null
        ? (assistant?.name ?? '小暖')
        : ref.watch(_titleProvider(chat.conversationId!)) ?? (assistant?.name ?? '小暖');

    return Pressable(
      scale: 0.97,
      onTap: () async {
        final id = await showModelPicker(context, ref);
        if (id != null) ref.read(chatProvider.notifier).setModel(id);
      },
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          AnimatedSwitcher(
            duration: const Duration(milliseconds: 250),
            child: Row(
              key: ValueKey('${chat.isPrivate}-$title'),
              mainAxisSize: MainAxisSize.min,
              children: [
                if (chat.isPrivate) ...[AppIcon(AppIcons.privateChat, size: 17, color: p.privateAccent), const SizedBox(width: 5)],
                Flexible(
                  child: Text(
                    title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 17.5, fontWeight: FontWeight.w600, color: chat.isPrivate ? p.privateAccent : p.text),
                  ),
                ),
              ],
            ),
          ),
          if (model != null)
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (isAuto) ...[AppIcon(AppIcons.sparkles, size: 12.5, color: p.accent), const SizedBox(width: 3.5)],
                Flexible(
                  child: Text(
                    isAuto ? (resolvedModel != null ? '自动 · ${resolvedModel.displayName}' : '自动选模型') : model.displayName,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w400),
                  ),
                ),
                const SizedBox(width: 3),
                AppIcon(AppIcons.chevronDown, size: 13.5, color: p.muted),
              ],
            ),
        ],
      ),
    );
  }
}

class _ExitPrivateButton extends StatelessWidget {
  const _ExitPrivateButton({required this.onTap});
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.only(right: 8),
      child: Tooltip(
        message: '退出隐私对话',
        child: Pressable(
          onTap: onTap,
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 44, minWidth: 68),
            child: Center(
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
                decoration: BoxDecoration(
                  color: p.privateSoft,
                  borderRadius: BorderRadius.circular(20),
                  border: Border.all(color: p.privateAccent.withValues(alpha: 0.3)),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(AppIcons.close, size: 15, color: p.privateAccent),
                    const SizedBox(width: 4),
                    Text(
                      '退出',
                      style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: p.privateAccent),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

String? _lastAssistantModel(List<Message> msgs) {
  for (var i = msgs.length - 1; i >= 0; i--) {
    if (!msgs[i].isUser && msgs[i].modelId != null) return msgs[i].modelId;
  }
  return null;
}

/// Title of a conversation from the history list (updates live when the auto title arrives).
final _titleProvider = Provider.family<String?, String>((ref, id) {
  final t = ref.watch(conversationsTitlesProvider)[id];
  return t == null || t.isEmpty ? null : t;
});

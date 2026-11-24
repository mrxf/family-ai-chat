import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../state/chat.dart';
import '../../../state/session.dart';
import '../../../state/sharing.dart';
import '../../adaptive/breakpoints.dart';
import '../../adaptive/readable_width.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import 'chat_view.dart';
import 'composer/composer.dart';
import 'welcome.dart';

/// The conversation area below the app bar: private-chat banner, the welcome
/// screen or message list, and the composer. Shared by every layout.
class ChatPane extends ConsumerWidget {
  const ChatPane({super.key, required this.composerKey});

  final GlobalKey<ComposerState> composerKey;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final chat = ref.watch(chatProvider);
    // Keep shared conversations synced while open.
    ref.watch(sharedSyncProvider);
    final conv = ref.watch(currentConversationProvider);
    final myId = ref.watch(sessionProvider.select((s) => s.user?.id ?? ''));
    final otherBusy = conv != null && conv.isShared && chat.busy && chat.activeUserId.isNotEmpty && chat.activeUserId != myId;
    return Column(
      children: [
        if (chat.isPrivate && !chat.isEmpty && !chat.loading) const _PrivateBanner(),
        if (otherBusy) _SharedBusyBanner(userName: chat.activeUserName.isEmpty ? '家人' : chat.activeUserName),
        Expanded(
          child: GestureDetector(
            behavior: HitTestBehavior.translucent,
            onTap: () => FocusScope.of(context).unfocus(),
            child: AnimatedSwitcher(
              duration: const Duration(milliseconds: 280),
              switchInCurve: Curves.easeOut,
              child: chat.loading
                  ? const Center(key: ValueKey('loading'), child: CircularProgressIndicator(strokeWidth: 2.4))
                  : chat.isEmpty
                  ? Welcome(
                      key: ValueKey('welcome-${chat.epoch}'),
                      onSuggestion: (t) => composerKey.currentState?.sendText(t),
                    )
                  : const ChatView(key: ValueKey('chat')),
            ),
          ),
        ),
        ReadableWidth(
          maxWidth: ContentWidth.chat,
          child: Composer(key: composerKey),
        ),
      ],
    );
  }
}

class _PrivateBanner extends StatelessWidget {
  const _PrivateBanner();

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 7),
      decoration: BoxDecoration(
        color: p.privateSoft,
        border: Border(
          top: BorderSide(color: p.privateAccent.withValues(alpha: 0.14)),
          bottom: BorderSide(color: p.privateAccent.withValues(alpha: 0.14)),
        ),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          AppIcon(AppIcons.privateChat, size: 15, color: p.privateAccent),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              '隐私对话中 · 离开后记录与图片自动清除，小暖不会记住',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 13, color: p.privateAccent, fontWeight: FontWeight.w500),
            ),
          ),
        ],
      ),
    );
  }
}

class _SharedBusyBanner extends StatelessWidget {
  const _SharedBusyBanner({required this.userName});
  final String userName;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 7),
      decoration: BoxDecoration(
        color: p.accentSoft,
        border: Border(bottom: BorderSide(color: p.accent.withValues(alpha: 0.16))),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          AppIcon(AppIcons.family, size: 15, color: p.accent),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              '$userName 正在和小暖对话中，回答完成后即可继续发送…',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 13, color: p.accent, fontWeight: FontWeight.w500),
            ),
          ),
        ],
      ),
    );
  }
}

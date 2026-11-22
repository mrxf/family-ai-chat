import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../state/chat.dart';
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
    return Column(
      children: [
        if (chat.isPrivate && !chat.isEmpty && !chat.loading) const _PrivateBanner(),
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

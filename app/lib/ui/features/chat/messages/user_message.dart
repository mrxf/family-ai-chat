import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../../state/session.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/sheets.dart';
import '../composer/quote_bar.dart';
import 'message_images.dart';

// ======================= user =======================

class UserMessage extends ConsumerWidget {
  const UserMessage({super.key, required this.message});
  final Message message;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final m = message;
    final isShared = ref.watch(chatProvider.select((s) => s.isShared));
    final myId = isShared ? ref.watch(sessionProvider.select((s) => s.user?.id ?? '')) : '';
    final isOtherMember = isShared && m.senderUserId.isNotEmpty && m.senderUserId != myId;
    final alignEnd = !isOtherMember;
    // 80% of the conversation column (the window on phones, the centered
    // chat column on wide windows).
    return LayoutBuilder(builder: (context, constraints) {
      final maxW = constraints.maxWidth * 0.8;
      return Padding(
        padding: EdgeInsets.fromLTRB(alignEnd ? 48 : 16, 10, alignEnd ? 16 : 48, 6),
        child: Column(
          crossAxisAlignment: alignEnd ? CrossAxisAlignment.end : CrossAxisAlignment.start,
          children: [
            if (isShared && m.senderName.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Avatar(label: m.senderName, emoji: m.senderAvatar, size: 18),
                    const SizedBox(width: 5),
                    Text(
                      isOtherMember ? m.senderName : '${m.senderName}（我）',
                      style: TextStyle(fontSize: 12, color: p.muted, fontWeight: FontWeight.w500),
                    ),
                  ],
                ),
              ),
            if (m.quotedComponent != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 6),
                child: QuotedComponentChip(
                  quote: m.quotedComponent!,
                  maxWidth: maxW,
                  onTap: m.quotedComponent!.conversationId.isNotEmpty
                      ? () => ref.read(chatProvider.notifier).open(
                            m.quotedComponent!.conversationId,
                            focusMessageId: m.quotedComponent!.messageId.isNotEmpty ? m.quotedComponent!.messageId : null,
                          )
                      : null,
                ),
              ),
            if (m.attachments.isNotEmpty)
              Padding(
                padding: EdgeInsets.only(bottom: m.content.isEmpty ? 0 : 6),
                child: ImageGrid(attachments: m.attachments, alignEnd: alignEnd),
              ),
            if (m.content.isNotEmpty)
              Row(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.center,
                children: [
                  if (m.status == MsgStatus.sending)
                    Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: p.faint)),
                    ),
                  if (m.status == MsgStatus.error)
                    Pressable(
                      onTap: () => ref.read(chatProvider.notifier).retryFailed(m),
                      child: Padding(padding: const EdgeInsets.only(right: 8), child: AppIcon(AppIcons.error, color: p.bad, size: 22)),
                    ),
                  Flexible(
                    child: Pressable(
                      scale: 0.98,
                      onLongPress: () => showUserMessageActions(context, ref, m),
                      child: Container(
                        constraints: BoxConstraints(maxWidth: maxW),
                        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 11),
                        decoration: BoxDecoration(
                          color: isOtherMember ? p.surfaceAlt : p.userBubble,
                          border: isOtherMember ? Border.all(color: p.line) : null,
                          borderRadius: BorderRadius.only(
                            topLeft: const Radius.circular(22),
                            topRight: const Radius.circular(22),
                            bottomLeft: Radius.circular(alignEnd ? 22 : 6),
                            bottomRight: Radius.circular(alignEnd ? 6 : 22),
                          ),
                        ),
                        child: Text(
                          m.content,
                          style: Theme.of(context).textTheme.bodyLarge!.copyWith(
                                color: isOtherMember ? p.text : p.userText,
                                height: 1.5,
                              ),
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            if (m.status == MsgStatus.error)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Pressable(
                  onTap: () => ref.read(chatProvider.notifier).retryFailed(m),
                  child: Text('发送失败，点这里重试', style: TextStyle(color: p.bad, fontSize: 14)),
                ),
              ),
          ],
        ),
      );
    });
  }
}

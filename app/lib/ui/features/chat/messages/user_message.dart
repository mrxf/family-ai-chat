import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../theme/icons.dart';
import '../../../widgets/sheets.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import 'message_images.dart';

// ======================= user =======================

class UserMessage extends ConsumerWidget {
  const UserMessage({super.key, required this.message});
  final Message message;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final m = message;
    final maxW = MediaQuery.sizeOf(context).width * 0.8;
    return Padding(
      padding: const EdgeInsets.fromLTRB(48, 10, 16, 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          if (m.attachments.isNotEmpty)
            Padding(
              padding: EdgeInsets.only(bottom: m.content.isEmpty ? 0 : 6),
              child: ImageGrid(attachments: m.attachments, alignEnd: true),
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
                        color: p.userBubble,
                        borderRadius: const BorderRadius.only(
                          topLeft: Radius.circular(22),
                          topRight: Radius.circular(22),
                          bottomLeft: Radius.circular(22),
                          bottomRight: Radius.circular(6),
                        ),
                      ),
                      child: Text(m.content, style: Theme.of(context).textTheme.bodyLarge!.copyWith(color: p.userText, height: 1.5)),
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
  }
}

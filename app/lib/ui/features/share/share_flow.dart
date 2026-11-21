import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../widgets/base_widgets.dart';
import 'share_select_page.dart';

/// Collects messages in [chat] that have actual text or image content to share.
List<Message> shareableMessages(ChatState chat, LiveText live) {
  final out = <Message>[];
  for (final m in chat.messages) {
    if (m.id == chat.activeId) {
      if (live.content.trim().isNotEmpty || live.attachments.isNotEmpty || live.components.isNotEmpty) {
        out.add(m.copyWith(
          content: live.content,
          reasoning: live.reasoning,
          attachments: live.attachments,
          components: live.components,
          status: MsgStatus.done,
        ));
      }
      continue;
    }
    if (m.content.trim().isEmpty && m.attachments.isEmpty && m.components.isEmpty) continue;
    out.add(m);
  }
  return out;
}

/// Opens the two-step share flow: pick messages → preview & save XiaoNuan poster.
Future<void> openShareChat(BuildContext context, WidgetRef ref, {Message? focusMessage}) async {
  FocusManager.instance.primaryFocus?.unfocus();
  final chat = ref.read(chatProvider);
  final live = ref.read(chatProvider.notifier).live.value;
  final msgs = shareableMessages(chat, live);
  if (msgs.isEmpty) {
    showToast(context, chat.busy ? '等小暖回答完就可以分享啦' : '还没有可分享的对话内容');
    return;
  }
  final boot = ref.read(sessionProvider).boot;
  final assistant = boot?.assistant(chat.assistantId);
  final cid = chat.conversationId;
  final rawTitle = cid == null ? null : ref.read(conversationsTitlesProvider)[cid];
  final title = rawTitle != null && rawTitle.trim().isNotEmpty ? rawTitle.trim() : null;

  await Navigator.of(context).push(
    appRoute(
      (_) => ShareSelectPage(
        messages: msgs,
        initialFocusId: focusMessage?.id,
        conversationTitle: title,
        assistant: assistant,
      ),
    ),
  );
}

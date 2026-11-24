import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/session.dart';
import '../../../state/sharing.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import 'family_share_sheet.dart';
import 'share_code_sheet.dart';
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

/// Opens the unified share entry sheet: family shared chat / short-code link / poster image.
Future<void> openShareChat(BuildContext context, WidgetRef ref, {Message? focusMessage}) async {
  FocusManager.instance.primaryFocus?.unfocus();
  final chat = ref.read(chatProvider);
  if (chat.isPrivate) {
    showToast(context, '隐私对话不支持分享或共享');
    return;
  }
  final live = ref.read(chatProvider.notifier).live.value;
  final msgs = shareableMessages(chat, live);
  if (msgs.isEmpty) {
    showToast(context, chat.busy ? '等小暖回答完就可以分享啦' : '还没有可分享的对话内容');
    return;
  }
  final cid = chat.conversationId;
  final conv = cid == null ? null : ref.read(conversationsProvider).items.where((c) => c.id == cid).firstOrNull;
  final boot = ref.read(sessionProvider).boot;
  final assistant = boot?.assistant(chat.assistantId);
  final rawTitle = cid == null ? null : ref.read(conversationsTitlesProvider)[cid];
  final title = rawTitle != null && rawTitle.trim().isNotEmpty ? rawTitle.trim() : null;

  await showActionSheet(
    context,
    title: '选择分享方式',
    actions: [
      if (conv != null && conv.isOwner)
        SheetAction(
          AppIcons.family,
          conv.isShared ? '管理共享家人（已共享 ${conv.memberCount} 人）' : '共享给家人',
          () {
            if (conv.isShared) {
              showSharedMembersSheet(context, ref, conv);
            } else {
              showFamilySharePickerSheet(context, ref, conv);
            }
          },
          subtitle: '多人实时同步对话与卡片，不共享个人记忆',
        ),
      if (cid != null)
        SheetAction(
          AppIcons.link,
          '分享短口令（可继续对话）',
          () => _openShortCodeSelect(context, ref, cid: cid, msgs: msgs, title: title, assistant: assistant),
          subtitle: '生成类抖音口令，对方导入后独立继续聊',
        ),
      SheetAction(
        AppIcons.image,
        '生成长图海报',
        () {
          Navigator.of(context).push(
            appRoute(
              (_) => ShareSelectPage(
                messages: msgs,
                initialFocusId: focusMessage?.id,
                conversationTitle: title,
                assistant: assistant,
              ),
            ),
          );
        },
        subtitle: '挑选消息生成精美对话截图',
      ),
    ],
  );
}

Future<void> _openShortCodeSelect(
  BuildContext context,
  WidgetRef ref, {
  required String cid,
  required List<Message> msgs,
  String? title,
  Assistant? assistant,
}) async {
  final pickedIds = await Navigator.of(context).push<List<String>>(
    appRoute(
      (_) => ShareSelectPage(
        messages: msgs,
        conversationTitle: title,
        assistant: assistant,
        selectAllByDefault: true,
        confirmLabel: '生成分享口令',
        pickIdsOnly: true,
      ),
    ),
  );
  if (pickedIds == null || pickedIds.isEmpty || !context.mounted) return;
  final api = ref.read(sessionProvider).api;
  if (api == null) return;
  try {
    final share = await api.createConversationShare(
      cid,
      messageIds: pickedIds.length == msgs.length ? null : pickedIds,
    );
    ref.invalidate(userSharesProvider);
    if (context.mounted) {
      await showShareCodeResultSheet(context, ref, share);
    }
  } on ApiError catch (e) {
    if (context.mounted) showToast(context, e.message);
  } catch (_) {
    if (context.mounted) showToast(context, '生成分享口令失败，请稍后重试');
  }
}

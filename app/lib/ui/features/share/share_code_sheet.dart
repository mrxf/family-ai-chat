import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../core/share_code.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/desktop_nav.dart';
import '../../../state/session.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../adaptive/breakpoints.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

bool _sharePreviewOpen = false;

/// Shows the generated Douyin-style share text and copies it to the clipboard.
Future<void> showShareCodeResultSheet(BuildContext context, WidgetRef ref, ConversationShare share) async {
  final creatorName = ref.read(sessionProvider).user?.displayName ?? share.creatorName;
  final text = XnShareCodec.formatShareMessage(title: share.title, code: share.code, creatorName: creatorName);
  ShareCodeGuard.markGeneratedLocally(text);
  await Clipboard.setData(ClipboardData(text: text));
  if (!context.mounted) return;

  await showAdaptiveModal<void>(
    context: context,
    maxWidth: 460,
    builder: (ctx) {
      final p = ctx.pal;
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(children: [
                Container(
                  width: 38,
                  height: 38,
                  decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(12)),
                  child: Center(child: AppIcon(AppIcons.link, color: p.accent, size: 20)),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    const Text('分享口令已复制', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700)),
                    const SizedBox(height: 2),
                    Text('发给家人后，对方复制打开小暖即可导入并继续对话', style: TextStyle(fontSize: 13, color: p.muted)),
                  ]),
                ),
              ]),
              const SizedBox(height: 14),
              Container(
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: p.surfaceAlt,
                  borderRadius: BorderRadius.circular(16),
                  border: Border.all(color: p.line),
                ),
                child: SelectableText(
                  text,
                  style: TextStyle(fontSize: 14.5, height: 1.55, color: p.text),
                ),
              ),
              const SizedBox(height: 10),
              Text(
                '· 导入后成为对方的独立对话，双方后续聊天互不同步\n· 记忆和知识库不共享，卡片固化为当前最新版本\n· 有效期 30 天，可随时在「设置 → 共享与分享管理」中撤销',
                style: TextStyle(fontSize: 12.5, height: 1.55, color: p.muted),
              ),
              const SizedBox(height: 16),
              Row(children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: () => Navigator.pop(ctx),
                    child: const Text('完成'),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: FilledButton.icon(
                    icon: const AppIcon(AppIcons.copy, size: 17, color: Colors.white),
                    label: const Text('再次复制'),
                    onPressed: () async {
                      ShareCodeGuard.markGeneratedLocally(text);
                      await Clipboard.setData(ClipboardData(text: text));
                      if (ctx.mounted) showToast(ctx, '已复制到剪贴板');
                    },
                  ),
                ),
              ]),
            ],
          ),
        ),
      );
    },
  );
}

/// Manual entry modal for pasting a `#xns...xns#` share snippet (handy on desktop or iOS).
Future<void> showPasteShareCodeSheet(BuildContext context, WidgetRef ref) async {
  final ctrl = TextEditingController();
  try {
    final clip = await Clipboard.getData(Clipboard.kTextPlain);
    if (XnShareCodec.inspectText(clip?.text, allowBareCode: true).hasShare) {
      ctrl.text = clip!.text!.trim();
    }
  } catch (_) {}
  if (!context.mounted) return;

  final submitted = await showAdaptiveModal<String>(
    context: context,
    maxWidth: 460,
    builder: (ctx) {
      final p = ctx.pal;
      return Padding(
        padding: EdgeInsets.fromLTRB(20, 4, 20, MediaQuery.viewInsetsOf(ctx).bottom + 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('打开分享口令', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700)),
            const SizedBox(height: 6),
            Text('粘贴包含 #xns...xns# 的分享文字，即可预览并导入对话继续聊', style: TextStyle(fontSize: 13.5, color: p.muted)),
            const SizedBox(height: 14),
            TextField(
              controller: ctrl,
              autofocus: true,
              minLines: 2,
              maxLines: 5,
              style: const TextStyle(fontSize: 15, height: 1.5),
              decoration: InputDecoration(
                hintText: '粘贴分享文字或口令…',
                fillColor: p.surfaceAlt,
                enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(16), borderSide: BorderSide.none),
              ),
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => Navigator.pop(ctx, ctrl.text.trim()),
              child: const Text('解析并预览'),
            ),
          ],
        ),
      );
    },
  );
  ctrl.dispose();
  if (submitted == null || submitted.isEmpty || !context.mounted) return;
  final ins = XnShareCodec.inspectText(submitted, allowBareCode: true);
  if (!ins.hasShare) {
    showToast(context, '没有识别到有效的分享口令（#xns...xns#）');
    return;
  }
  await openShareCodePreview(context, ref, ins.code!, silent: false);
}

/// Silently inspects the clipboard when the signed-in app resumes.
Future<void> inspectClipboardForShareCode(BuildContext context, WidgetRef ref) async {
  if (_sharePreviewOpen) return;
  if (ref.read(sessionProvider).api == null) return;
  String? raw;
  try {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    raw = data?.text;
  } catch (_) {
    return;
  }
  final ins = XnShareCodec.inspectText(raw);
  if (!ins.hasShare) return;
  final code = ins.code!;
  if (ShareCodeGuard.wasGeneratedLocally(code)) return;
  final prefs = await SharedPreferences.getInstance();
  if (ShareCodeGuard.wasConsumed(prefs, code)) return;
  if (!context.mounted) return;
  await openShareCodePreview(context, ref, code, silent: true, prefs: prefs);
}

/// Fetches preview metadata for [code] and prompts the user to import and continue chatting.
Future<void> openShareCodePreview(
  BuildContext context,
  WidgetRef ref,
  String code, {
  required bool silent,
  SharedPreferences? prefs,
}) async {
  if (_sharePreviewOpen) return;
  final api = ref.read(sessionProvider).api;
  if (api == null) return;

  _sharePreviewOpen = true;
  try {
    final preview = await api.previewShare(code);
    if (!context.mounted) return;
    final sp = prefs ?? await SharedPreferences.getInstance();
    if (!context.mounted) return;
    final imported = await showAdaptiveModal<Conversation>(
      context: context,
      maxWidth: 440,
      builder: (ctx) => _ShareImportPreviewDialog(preview: preview, code: code),
    );
    await ShareCodeGuard.markConsumed(sp, code);
    if (imported != null && context.mounted) {
      ref.read(conversationsProvider.notifier).upsertTop(imported);
      if (context.windowSize.isExpanded) {
        ref.read(desktopNavProvider.notifier).showChat();
      }
      await ref.read(chatProvider.notifier).open(imported.id);
      if (context.mounted) showToast(context, '已导入对话，可以继续和小暖聊啦');
    }
  } on ApiError catch (e) {
    if (!silent && context.mounted) showToast(context, e.message);
  } catch (_) {
    if (!silent && context.mounted) showToast(context, '口令解析失败，请稍后重试');
  } finally {
    _sharePreviewOpen = false;
  }
}

class _ShareImportPreviewDialog extends ConsumerStatefulWidget {
  const _ShareImportPreviewDialog({required this.preview, required this.code});
  final ConversationShare preview;
  final String code;

  @override
  ConsumerState<_ShareImportPreviewDialog> createState() => _ShareImportPreviewDialogState();
}

class _ShareImportPreviewDialogState extends ConsumerState<_ShareImportPreviewDialog> {
  bool _importing = false;

  Future<void> _import() async {
    if (_importing) return;
    setState(() => _importing = true);
    try {
      final api = ref.read(sessionProvider).api;
      if (api == null) return;
      final conv = await api.importShare(widget.code);
      if (mounted) Navigator.pop(context, conv);
    } on ApiError catch (e) {
      if (mounted) {
        setState(() => _importing = false);
        showToast(context, e.message);
      }
    } catch (_) {
      if (mounted) {
        setState(() => _importing = false);
        showToast(context, '导入失败，请稍后重试');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final s = widget.preview;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(children: [
              Avatar(label: s.creatorName.isEmpty ? '家人' : s.creatorName, emoji: s.creatorAvatar, size: 40),
              const SizedBox(width: 12),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(
                    '${s.creatorName.isEmpty ? '家人' : s.creatorName} 分享了一段对话',
                    style: TextStyle(fontSize: 13.5, color: p.muted),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    s.title.isEmpty ? '分享的对话' : s.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
                  ),
                ]),
              ),
            ]),
            const SizedBox(height: 14),
            Container(
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(
                  '共 ${s.messageCount} 条消息${s.hasComponents ? ' · 含静态卡片快照' : ''}',
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.text),
                ),
                const SizedBox(height: 6),
                Text(
                  '导入后会复制为你的独立对话，你可以直接接着聊；不会与分享人同步，也不共享对方的记忆。',
                  style: TextStyle(fontSize: 13, height: 1.5, color: p.muted),
                ),
              ]),
            ),
            const SizedBox(height: 18),
            Row(children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: _importing ? null : () => Navigator.pop(context),
                  child: const Text('暂不导入'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: FilledButton(
                  onPressed: _importing ? null : _import,
                  child: _importing
                      ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                      : const Text('导入并继续对话'),
                ),
              ),
            ]),
          ],
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../state/chat.dart';
import '../state/session.dart';
import 'icons.dart';
import 'share_page.dart';
import 'theme.dart';
import 'widgets.dart';

class SheetAction {
  final IconData icon;
  final String label;
  final String? subtitle;
  final VoidCallback onTap;
  final bool danger;
  const SheetAction(this.icon, this.label, this.onTap, {this.subtitle, this.danger = false});
}

/// A simple bottom sheet of large, easy-to-hit actions.
Future<void> showActionSheet(BuildContext context, {String? title, required List<SheetAction> actions}) {
  return showModalBottomSheet(
    context: context,
    builder: (ctx) {
      final p = ctx.pal;
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (title != null)
                Padding(
                  padding: const EdgeInsets.fromLTRB(12, 0, 12, 10),
                  child: Text(title, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 14, color: p.muted)),
                ),
              for (final a in actions)
                ListTile(
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
                  leading: Icon(a.icon, color: a.danger ? p.bad : p.text),
                  title: Text(a.label, style: TextStyle(fontSize: 17, color: a.danger ? p.bad : p.text)),
                  subtitle: a.subtitle == null ? null : Text(a.subtitle!, style: TextStyle(fontSize: 13, color: p.muted)),
                  onTap: () {
                    Navigator.pop(ctx);
                    a.onTap();
                  },
                ),
            ],
          ),
        ),
      );
    },
  );
}

Future<bool> confirm(BuildContext context, {required String title, String? body, String ok = '确定', bool danger = false}) async {
  final p = context.pal;
  final r = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(title),
      content: body == null ? null : Text(body, style: const TextStyle(fontSize: 16, height: 1.5)),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: danger ? p.bad : p.accent, minimumSize: const Size(88, 46)),
          onPressed: () => Navigator.pop(ctx, true),
          child: Text(ok),
        ),
      ],
    ),
  );
  return r == true;
}

/// Multi-line text editor in a bottom sheet (edit message / rename).
Future<String?> showTextEditor(BuildContext context, {required String title, required String initial, String ok = '完成', int maxLines = 8, String? hint}) {
  final ctrl = TextEditingController(text: initial);
  return showModalBottomSheet<String>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) {
      final p = ctx.pal;
      return Padding(
        padding: EdgeInsets.fromLTRB(20, 0, 20, MediaQuery.viewInsetsOf(ctx).bottom + 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(title, style: Theme.of(ctx).textTheme.titleMedium),
            const SizedBox(height: 14),
            TextField(
              controller: ctrl,
              autofocus: true,
              minLines: 1,
              maxLines: maxLines,
              style: const TextStyle(fontSize: 17, height: 1.5),
              decoration: InputDecoration(hintText: hint, fillColor: p.surfaceAlt, enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(16), borderSide: BorderSide.none)),
            ),
            const SizedBox(height: 16),
            FilledButton(onPressed: () => Navigator.pop(ctx, ctrl.text.trim()), child: Text(ok)),
          ],
        ),
      );
    },
  );
}

void showUserMessageActions(BuildContext context, WidgetRef ref, Message m) {
  final chat = ref.read(chatProvider.notifier);
  final busy = ref.read(chatProvider).busy;
  showActionSheet(context, title: m.content, actions: [
    if (m.content.isNotEmpty)
      SheetAction(AppIcons.copy, '复制', () {
        Clipboard.setData(ClipboardData(text: m.content));
        showToast(context, '已复制');
      }),
    if (!busy)
      SheetAction(AppIcons.edit, '修改后重新发送', () async {
        final t = await showTextEditor(context, title: '修改问题', initial: m.content, ok: '重新发送');
        if (t != null && t.isNotEmpty) chat.editAndResend(m, t);
      }),
  ]);
}

void showAssistantMessageActions(BuildContext context, WidgetRef ref, Message m, bool isLast) {
  final chat = ref.read(chatProvider.notifier);
  final isPrivate = ref.read(chatProvider).isPrivate;
  showActionSheet(context, actions: [
    if (!isPrivate)
      SheetAction(AppIcons.share, '分享这段对话', () {
        openShareChat(context, ref, focusMessage: m);
      }),
    SheetAction(AppIcons.copy, '复制全部', () {
      Clipboard.setData(ClipboardData(text: m.content));
      showToast(context, '已复制');
    }),
    SheetAction(AppIcons.selectText, '选择部分文字', () {
      Navigator.of(context).push(MaterialPageRoute(builder: (_) => SelectTextPage(text: m.content)));
    }),
    if (isLast)
      SheetAction(AppIcons.swap, '换个模型重新回答', () async {
        final id = await showModelPicker(context, ref, title: '用哪个模型重新回答？');
        if (id != null) chat.regenerate(modelId: id);
      }),
  ]);
}

class SelectTextPage extends StatelessWidget {
  const SelectTextPage({super.key, required this.text});
  final String text;

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: const Text('选择文字')),
        body: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 40),
          child: SelectableText(text, style: Theme.of(context).textTheme.bodyLarge),
        ),
      );
}

/// Lets the user pick a model. Returns the chosen id.
Future<String?> showModelPicker(BuildContext context, WidgetRef ref, {String title = '选择模型'}) {
  final boot = ref.read(sessionProvider).boot;
  if (boot == null) return Future.value(null);
  final chat = ref.read(chatProvider);
  final current = boot.effectiveModel(chat.modelId, chat.assistantId)?.id;
  final items = [
    if (boot.autoModel && boot.models.isNotEmpty) boot.autoChatModel,
    ...boot.models,
  ];
  return showModalBottomSheet<String>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) {
      final p = ctx.pal;
      return SafeArea(
        child: ConstrainedBox(
          // Many models: scroll instead of overflowing the sheet.
          constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(ctx).height * 0.8),
          child: ListView(
            shrinkWrap: true,
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            children: [
              Padding(padding: const EdgeInsets.fromLTRB(8, 0, 8, 12), child: Text(title, style: Theme.of(ctx).textTheme.titleMedium)),
              for (final m in items)
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: Pressable(
                    scale: 0.98,
                    onTap: () => Navigator.pop(ctx, m.id),
                    child: AnimatedContainer(
                      duration: const Duration(milliseconds: 200),
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: m.id == current ? p.accentSoft : p.surfaceAlt,
                        borderRadius: BorderRadius.circular(18),
                        border: Border.all(color: m.id == current ? p.accent : Colors.transparent, width: 1.5),
                      ),
                      child: Row(children: [
                        Expanded(
                          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                            Row(children: [
                              if (m.id == kAutoModelId) ...[
                                Icon(AppIcons.sparkles, size: 18, color: p.accent),
                                const SizedBox(width: 6),
                              ],
                              Flexible(
                                child: Text(m.displayName, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
                              ),
                              if (m.id == boot.defaultModelId || m.id == kAutoModelId) ...[
                                const SizedBox(width: 8),
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                  decoration: BoxDecoration(color: p.accent.withValues(alpha: 0.15), borderRadius: BorderRadius.circular(8)),
                                  child: Text(m.id == boot.defaultModelId ? '推荐' : '省心', style: TextStyle(fontSize: 12, color: p.accent, fontWeight: FontWeight.w600)),
                                ),
                              ],
                            ]),
                            if (m.description.isNotEmpty) ...[
                              const SizedBox(height: 4),
                              Text(m.description, style: TextStyle(fontSize: 14, color: p.muted)),
                            ],
                            const SizedBox(height: 6),
                            Wrap(spacing: 10, children: [
                              if (m.vision) _cap(p, AppIcons.image, '能看图'),
                              if (m.think) _cap(p, AppIcons.thinkOff, '可深度思考'),
                            ]),
                          ]),
                        ),
                        if (m.id == current) Icon(AppIcons.checkCircle, color: p.accent),
                      ]),
                    ),
                  ),
                ),
            ],
          ),
        ),
      );
    },
  );
}

Widget _cap(Palette p, IconData icon, String label) => Row(mainAxisSize: MainAxisSize.min, children: [
      Icon(icon, size: 14, color: p.faint),
      const SizedBox(width: 3),
      Text(label, style: TextStyle(fontSize: 12.5, color: p.faint)),
    ]);

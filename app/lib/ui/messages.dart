import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show Clipboard, ClipboardData;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../core/tts.dart';
import '../state/chat.dart';
import '../state/session.dart';
import 'markdown.dart';
import 'sheets.dart';
import 'mascot_anim.dart';
import 'theme.dart';
import 'widgets.dart';

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
                    child: Padding(padding: const EdgeInsets.only(right: 8), child: Icon(Icons.error_rounded, color: p.bad, size: 24)),
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

// ======================= images =======================

class ImageGrid extends ConsumerWidget {
  const ImageGrid({super.key, required this.attachments, this.alignEnd = false});
  final List<Attachment> attachments;
  final bool alignEnd;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.watch(sessionProvider).api;
    Widget image(Attachment a, {BoxFit fit = BoxFit.cover}) {
      final local = a.localPath;
      if (local != null && File(local).existsSync()) {
        return Image.file(File(local), fit: fit, gaplessPlayback: true, cacheWidth: 900);
      }
      if (api == null || a.fileId.isEmpty) return const SizedBox();
      return Image.network(api.fileUrl(a.fileId), headers: api.authHeaders, fit: fit, gaplessPlayback: true, cacheWidth: 900,
          errorBuilder: (_, _, _) => Container(color: context.pal.surfaceAlt, child: Icon(Icons.broken_image_outlined, color: context.pal.faint)));
    }

    void open(int i) => Navigator.of(context).push(PageRouteBuilder(
          opaque: false,
          barrierColor: Colors.black,
          pageBuilder: (_, _, _) => ImageViewer(images: [for (final a in attachments) image(a, fit: BoxFit.contain)], initial: i),
          transitionsBuilder: (_, anim, _, child) => FadeTransition(opacity: anim, child: child),
        ));

    if (attachments.length == 1) {
      final a = attachments.first;
      final w = a.aspect >= 1 ? 240.0 : 180.0;
      return Pressable(
        scale: 0.98,
        onTap: () => open(0),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(18),
          child: SizedBox(width: w, height: (w / a.aspect).clamp(100.0, 320.0), child: Hero(tag: 'img-${a.fileId}-${a.localPath}', child: image(a))),
        ),
      );
    }
    return Wrap(
      alignment: alignEnd ? WrapAlignment.end : WrapAlignment.start,
      spacing: 6,
      runSpacing: 6,
      children: [
        for (var i = 0; i < attachments.length; i++)
          Pressable(
            scale: 0.97,
            onTap: () => open(i),
            child: ClipRRect(borderRadius: BorderRadius.circular(14), child: SizedBox(width: 92, height: 92, child: image(attachments[i]))),
          ),
      ],
    );
  }
}

class ImageViewer extends StatelessWidget {
  const ImageViewer({super.key, required this.images, this.initial = 0});
  final List<Widget> images;
  final int initial;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: GestureDetector(
        onTap: () => Navigator.pop(context),
        child: PageView(
          controller: PageController(initialPage: initial),
          children: [for (final img in images) InteractiveViewer(maxScale: 5, child: Center(child: img))],
        ),
      ),
    );
  }
}

// ======================= assistant =======================

class AssistantMessage extends ConsumerStatefulWidget {
  const AssistantMessage({super.key, required this.message, required this.isLast});
  final Message message;
  final bool isLast;

  @override
  ConsumerState<AssistantMessage> createState() => _AssistantMessageState();
}

class _AssistantMessageState extends ConsumerState<AssistantMessage> {
  static final _idle = ValueNotifier<LiveText>(LiveText.empty);

  /// Once a message has streamed in this widget we keep the reveal pipeline,
  /// so finishing doesn't swap the widget tree (no flash at the end).
  bool _wasLive = false;

  @override
  Widget build(BuildContext context) {
    final activeId = ref.watch(chatProvider.select((s) => s.activeId));
    final controller = ref.read(chatProvider.notifier);
    final isLive = activeId == widget.message.id;
    if (isLive) _wasLive = true;

    return ValueListenableBuilder<LiveText>(
      valueListenable: isLive ? controller.live : _idle,
      builder: (context, live, _) {
        final m = widget.message;
        final content = isLive ? live.content : m.content;
        final reasoning = isLive ? live.reasoning : m.reasoning;
        final phase = isLive ? live.phase : null;
        return Padding(
          padding: const EdgeInsets.fromLTRB(18, 8, 18, 4),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (reasoning.isNotEmpty) ReasoningPanel(text: reasoning, thinking: phase == LivePhase.reasoning, durationMs: m.durationMs),
              AnimatedSwitcher(
                duration: const Duration(milliseconds: 250),
                child: phase == LivePhase.waiting ? const ThinkingIndicator() : const SizedBox(width: double.infinity),
              ),
              if (content.isNotEmpty) Md(content, streaming: isLive, animate: _wasLive),
              if (!isLive) _Footer(message: m, isLast: widget.isLast),
            ],
          ),
        );
      },
    );
  }
}

class _Footer extends ConsumerWidget {
  const _Footer({required this.message, required this.isLast});
  final Message message;
  final bool isLast;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final m = message;
    final chat = ref.read(chatProvider.notifier);
    Widget note(IconData icon, String text, Color color) => Padding(
          padding: const EdgeInsets.only(top: 6, bottom: 2),
          child: Row(mainAxisSize: MainAxisSize.min, children: [
            Icon(icon, size: 16, color: color),
            const SizedBox(width: 6),
            Flexible(child: Text(text, style: TextStyle(fontSize: 14, color: color))),
          ]),
        );

    final children = <Widget>[];
    switch (m.status) {
      case MsgStatus.error:
        children.add(Container(
          margin: const EdgeInsets.only(top: 6),
          padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
          decoration: BoxDecoration(color: p.bad.withValues(alpha: 0.08), borderRadius: BorderRadius.circular(14)),
          child: Row(children: [
            Icon(Icons.cloud_off_rounded, size: 20, color: p.bad),
            const SizedBox(width: 10),
            Expanded(child: Text(m.error.isEmpty ? '回答出错了' : m.error, style: TextStyle(fontSize: 15, color: p.text))),
            if (isLast) TextButton(onPressed: () => chat.regenerate(), child: const Text('重试')),
          ]),
        ));
      case MsgStatus.cancelled:
        children.add(note(Icons.stop_circle_outlined, '已停止回答', p.muted));
      case MsgStatus.interrupted:
        children.add(note(Icons.info_outline_rounded, '回答被中断了，可以点下方「重新回答」', p.muted));
      default:
    }
    if (m.content.isNotEmpty || isLast) {
      children.add(_ActionRow(message: m, isLast: isLast));
    }
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: children);
  }
}

class _ActionRow extends ConsumerWidget {
  const _ActionRow({required this.message, required this.isLast});
  final Message message;
  final bool isLast;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final m = message;
    Widget btn(IconData icon, String tip, VoidCallback onTap, {Color? color}) => Tooltip(
          message: tip,
          child: Pressable(
            onTap: onTap,
            child: Padding(padding: const EdgeInsets.all(9), child: Icon(icon, size: 21, color: color ?? p.muted)),
          ),
        );
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: Transform.translate(
        offset: const Offset(-9, 0),
        child: Row(children: [
          if (m.content.isNotEmpty)
            btn(Icons.copy_rounded, '复制', () {
              Clipboard.setData(ClipboardData(text: m.content));
              showToast(context, '已复制');
            }),
          if (m.content.isNotEmpty)
            ValueListenableBuilder<String?>(
              valueListenable: Speaker.instance.speaking,
              builder: (_, speakingId, _) {
                final on = speakingId == m.id;
                return btn(on ? Icons.stop_circle_rounded : Icons.volume_up_rounded, on ? '停止朗读' : '朗读', () async {
                  final ok = await Speaker.instance.toggle(m.id, m.content, rate: ref.read(settingsProvider).ttsRate);
                  if (!ok && context.mounted) showToast(context, '这台手机暂时不支持朗读，可以在系统设置里安装中文语音');
                }, color: on ? p.accent : null);
              },
            ),
          if (isLast) btn(Icons.refresh_rounded, '重新回答', () => ref.read(chatProvider.notifier).regenerate()),
          if (m.content.isNotEmpty) btn(Icons.more_horiz_rounded, '更多', () => showAssistantMessageActions(context, ref, m, isLast)),
        ]),
      ),
    );
  }
}

/// Shown between sending and the first token.
class ThinkingIndicator extends StatefulWidget {
  const ThinkingIndicator({super.key});
  @override
  State<ThinkingIndicator> createState() => _ThinkingIndicatorState();
}

class _ThinkingIndicatorState extends State<ThinkingIndicator> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1400))..repeat();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const AnimatedMascot(size: 34, motion: MascotMotion.thinking),
          const SizedBox(width: 8),
          AnimatedBuilder(
            animation: _c,
            builder: (_, _) => Row(
              children: [
                for (var i = 0; i < 3; i++)
                  Container(
                    margin: const EdgeInsets.symmetric(horizontal: 3),
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: p.accent.withValues(alpha: 0.25 + 0.75 * _pulse((_c.value - i * 0.18) % 1.0)),
                      shape: BoxShape.circle,
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  double _pulse(double t) => t < 0.5 ? Curves.easeOut.transform(t * 2) : Curves.easeIn.transform((1 - t) * 2);
}

/// Collapsible "deep thinking" process. Expanded while reasoning streams,
/// collapses automatically once the answer begins.
class ReasoningPanel extends StatefulWidget {
  const ReasoningPanel({super.key, required this.text, required this.thinking, this.durationMs = 0});
  final String text;
  final bool thinking;
  final int durationMs;

  @override
  State<ReasoningPanel> createState() => _ReasoningPanelState();
}

class _ReasoningPanelState extends State<ReasoningPanel> {
  bool? _userOpen;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final open = _userOpen ?? widget.thinking;
    final label = widget.thinking ? '正在深度思考…' : '已深度思考';
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Pressable(
            onTap: () => setState(() => _userOpen = !open),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
              decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(20)),
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                Icon(Icons.lightbulb_outline_rounded, size: 17, color: p.accent),
                const SizedBox(width: 6),
                Text(label, style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w500)),
                AnimatedRotation(
                  turns: open ? 0.5 : 0,
                  duration: const Duration(milliseconds: 200),
                  child: Icon(Icons.expand_more_rounded, size: 18, color: p.muted),
                ),
              ]),
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 260),
            curve: Curves.easeOutCubic,
            alignment: Alignment.topCenter,
            child: open
                ? Container(
                    margin: const EdgeInsets.only(top: 8),
                    padding: const EdgeInsets.only(left: 12),
                    decoration: BoxDecoration(border: Border(left: BorderSide(color: p.line, width: 2))),
                    child: Text(widget.text.trim(), style: TextStyle(fontSize: 14.5, height: 1.6, color: p.muted)),
                  )
                : const SizedBox(width: double.infinity),
          ),
        ],
      ),
    );
  }
}

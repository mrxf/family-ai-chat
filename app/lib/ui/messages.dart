import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show Clipboard, ClipboardData, HapticFeedback, Uint8List;
import 'package:gal/gal.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../core/tts.dart';
import '../state/chat.dart';
import '../state/session.dart';
import 'icons.dart';
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
                    child: Padding(padding: const EdgeInsets.only(right: 8), child: Icon(AppIcons.error, color: p.bad, size: 22)),
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
  const ImageGrid({super.key, required this.attachments, this.alignEnd = false, this.large = false});
  final List<Attachment> attachments;
  final bool alignEnd;

  /// Pictures the assistant drew: shown big, they are the answer.
  final bool large;

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
          errorBuilder: (_, _, _) => Container(color: context.pal.surfaceAlt, child: Icon(AppIcons.brokenImage, color: context.pal.faint)));
    }

    void open(int i) => Navigator.of(context).push(PageRouteBuilder(
          opaque: false,
          barrierColor: Colors.black,
          pageBuilder: (_, _, _) => ImageViewer(
            images: [for (final a in attachments) image(a, fit: BoxFit.contain)],
            initial: i,
            saveable: large ? attachments : null,
          ),
          transitionsBuilder: (_, anim, _, child) => FadeTransition(opacity: anim, child: child),
        ));

    if (large) {
      final w = drawnImageWidth(context);
      return Wrap(spacing: 8, runSpacing: 8, children: [
        for (var i = 0; i < attachments.length; i++)
          Pressable(
            scale: 0.98,
            onTap: () => open(i),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(20),
              child: SizedBox(
                width: w,
                height: w / attachments[i].aspect.clamp(0.5, 2.0),
                child: _FadeIn(child: image(attachments[i])),
              ),
            ),
          ),
      ]);
    }
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

double drawnImageWidth(BuildContext context) => (MediaQuery.sizeOf(context).width * 0.72).clamp(200.0, 340.0);

class _FadeIn extends StatefulWidget {
  const _FadeIn({required this.child});
  final Widget child;
  @override
  State<_FadeIn> createState() => _FadeInState();
}

class _FadeInState extends State<_FadeIn> {
  bool _shown = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) setState(() => _shown = true);
    });
  }

  @override
  Widget build(BuildContext context) =>
      AnimatedOpacity(opacity: _shown ? 1 : 0, duration: const Duration(milliseconds: 450), curve: Curves.easeOut, child: widget.child);
}

/// Placeholder while a picture is being drawn: a soft shimmering card with
/// the mascot and how long it has been, so a 30-second wait feels alive.
class DrawingCard extends StatefulWidget {
  const DrawingCard({super.key, required this.label, this.since, this.isPrivate = false});
  final String label;
  final DateTime? since;
  final bool isPrivate;

  @override
  State<DrawingCard> createState() => _DrawingCardState();
}

class _DrawingCardState extends State<DrawingCard> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1800))..repeat();
  final DateTime _created = DateTime.now();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final w = drawnImageWidth(context);
    return ClipRRect(
      borderRadius: BorderRadius.circular(20),
      child: SizedBox(
        width: w,
        height: w,
        child: AnimatedBuilder(
          animation: _c,
          builder: (_, child) {
            final t = _c.value;
            final secs = DateTime.now().difference(widget.since ?? _created).inSeconds.clamp(0, 9999);
            return DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment(-1.6 + 3.2 * t, -1),
                  end: Alignment(-0.6 + 3.2 * t, 1),
                  colors: [p.surfaceAlt, p.accentSoft, p.surfaceAlt],
                ),
              ),
              child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
                child!,
                const SizedBox(height: 14),
                Text(widget.label, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: p.text)),
                const SizedBox(height: 6),
                Text(secs < 3 ? '一般要半分钟左右' : '已经 $secs 秒，一般要半分钟左右', style: TextStyle(fontSize: 13, color: p.muted)),
                const SizedBox(height: 2),
                Text(
                  widget.isPrivate ? '隐私对话关闭后会清除，画好后可保存到相册' : '离开这个页面也没关系，画好会留在这里',
                  style: TextStyle(fontSize: 12, color: p.faint),
                ),
              ]),
            );
          },
          child: const AnimatedMascot(size: 64, motion: MascotMotion.thinking),
        ),
      ),
    );
  }
}

class ImageViewer extends ConsumerStatefulWidget {
  const ImageViewer({super.key, required this.images, this.initial = 0, this.saveable});
  final List<Widget> images;
  final int initial;

  /// Pictures that can be saved to the gallery (same order as [images]).
  final List<Attachment>? saveable;

  @override
  ConsumerState<ImageViewer> createState() => _ImageViewerState();
}

class _ImageViewerState extends ConsumerState<ImageViewer> {
  late final _pages = PageController(initialPage: widget.initial);
  late int _page = widget.initial;
  bool _saving = false;

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final list = widget.saveable;
    if (list == null || _saving) return;
    setState(() => _saving = true);
    await saveToGallery(context, ref, [list[_page]]);
    if (mounted) setState(() => _saving = false);
  }

  @override
  Widget build(BuildContext context) {
    final canSave = widget.saveable != null;
    return Scaffold(
      backgroundColor: Colors.black,
      body: Stack(children: [
        GestureDetector(
          onTap: () => Navigator.pop(context),
          onLongPress: canSave ? _save : null,
          child: PageView(
            controller: _pages,
            onPageChanged: (i) => setState(() => _page = i),
            children: [for (final img in widget.images) InteractiveViewer(maxScale: 5, child: Center(child: img))],
          ),
        ),
        if (canSave)
          Positioned(
            left: 0,
            right: 0,
            bottom: 24 + MediaQuery.paddingOf(context).bottom,
            child: Center(
              child: FilledButton.icon(
                style: FilledButton.styleFrom(
                  backgroundColor: Colors.white.withValues(alpha: 0.16),
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(horizontal: 26, vertical: 14),
                  textStyle: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
                  shape: const StadiumBorder(),
                ),
                onPressed: _saving ? null : _save,
                icon: _saving
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const Icon(AppIcons.download),
                label: const Text('保存到相册'),
              ),
            ),
          ),
      ]),
    );
  }
}

/// Saves pictures to the phone's gallery and tells the user where they went.
Future<void> saveToGallery(BuildContext context, WidgetRef ref, List<Attachment> list) async {
  HapticFeedback.lightImpact();
  try {
    if (!await Gal.hasPermission() && !await Gal.requestPermission()) {
      if (context.mounted) showToast(context, '没有相册权限，请在手机设置里允许小暖保存照片');
      return;
    }
    final api = ref.read(sessionProvider).api;
    var saved = 0;
    for (final a in list) {
      final local = a.localPath;
      final Uint8List bytes;
      if (local != null && File(local).existsSync()) {
        bytes = await File(local).readAsBytes();
      } else if (api != null && a.fileId.isNotEmpty) {
        bytes = await api.fileBytes(a.fileId);
      } else {
        continue;
      }
      await Gal.putImageBytes(bytes, name: 'xiaonuan_${DateTime.now().millisecondsSinceEpoch}_$saved');
      saved++;
    }
    if (saved > 0 && context.mounted) showToast(context, '${saved > 1 ? '$saved 张图' : '已'}保存到相册，可以去微信发给家人了');
  } on GalException catch (e) {
    if (context.mounted) {
      showToast(context, e.type == GalExceptionType.permissionDenied ? '没有相册权限，请在手机设置里允许小暖保存照片' : '保存失败，请再试一次');
    }
  } catch (_) {
    if (context.mounted) showToast(context, '图片下载失败，请检查网络后再试');
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
    final isPrivate = ref.watch(chatProvider.select((s) => s.isPrivate));
    final controller = ref.read(chatProvider.notifier);
    final isLive = activeId == widget.message.id;
    if (isLive) _wasLive = true;

    return ValueListenableBuilder<LiveText>(
      valueListenable: isLive ? controller.live : _idle,
      builder: (context, live, _) {
        final m = widget.message;
        final content = isLive ? live.content : m.content;
        final reasoning = isLive ? live.reasoning : m.reasoning;
        final drawn = isLive ? live.attachments : m.attachments;
        final drawing = isLive && live.drawing;
        final working = isLive && live.working;
        final phase = isLive ? live.phase : null;
        return Padding(
          padding: const EdgeInsets.fromLTRB(18, 8, 18, 4),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (reasoning.isNotEmpty) ReasoningPanel(text: reasoning, thinking: phase == LivePhase.reasoning, durationMs: m.durationMs),
              AnimatedSwitcher(
                duration: const Duration(milliseconds: 250),
                child: phase == LivePhase.waiting && !drawing && drawn.isEmpty
                    ? ThinkingIndicator(label: working ? live.activity : '')
                    : const SizedBox(width: double.infinity),
              ),
              if (content.isNotEmpty) Md(content, streaming: isLive, animate: _wasLive),
              if (working && phase != LivePhase.waiting)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: ThinkingIndicator(label: live.activity, compact: true),
                ),
              if (drawn.isNotEmpty || drawing)
                Padding(
                  padding: EdgeInsets.only(top: content.isEmpty ? 2 : 10, bottom: 4),
                  child: Wrap(spacing: 8, runSpacing: 8, children: [
                    if (drawn.isNotEmpty) ImageGrid(attachments: drawn, large: true),
                    if (drawing) DrawingCard(label: live.activity, since: live.activitySince, isPrivate: isPrivate),
                  ]),
                ),
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
            Icon(AppIcons.cloudOff, size: 20, color: p.bad),
            const SizedBox(width: 10),
            Expanded(child: Text(m.error.isEmpty ? '回答出错了' : m.error, style: TextStyle(fontSize: 15, color: p.text))),
            if (isLast) TextButton(onPressed: () => chat.regenerate(), child: const Text('重试')),
          ]),
        ));
      case MsgStatus.cancelled:
        children.add(note(AppIcons.stopCircle, '已停止回答', p.muted));
      case MsgStatus.interrupted:
        children.add(note(AppIcons.info, '回答被中断了，可以点下方「重新回答」', p.muted));
      default:
    }
    if (m.content.isNotEmpty || m.attachments.isNotEmpty || isLast) {
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
    final boot = ref.watch(sessionProvider).boot;
    final sel = ref.watch(chatProvider.select((s) => (s.modelId, s.assistantId)));
    final showModelBadge = m.modelId != null && boot != null && boot.isAutoSelection(sel.$1, sel.$2);
    final usedModel = showModelBadge ? boot.model(m.modelId) : null;
    Widget btn(IconData icon, String tip, VoidCallback onTap, {Color? color}) => Tooltip(
          message: tip,
          child: Pressable(
            onTap: onTap,
            child: Padding(padding: const EdgeInsets.all(9), child: Icon(icon, size: 20, color: color ?? p.muted)),
          ),
        );
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: Transform.translate(
        offset: const Offset(-9, 0),
        child: Row(children: [
          if (m.attachments.isNotEmpty) btn(AppIcons.download, '保存图片', () => saveToGallery(context, ref, m.attachments)),
          if (m.content.isNotEmpty)
            btn(AppIcons.copy, '复制', () {
              Clipboard.setData(ClipboardData(text: m.content));
              showToast(context, '已复制');
            }),
          if (m.content.isNotEmpty)
            ValueListenableBuilder<String?>(
              valueListenable: Speaker.instance.speaking,
              builder: (_, speakingId, _) {
                final on = speakingId == m.id;
                return btn(on ? AppIcons.stopCircle : AppIcons.speaker, on ? '停止朗读' : '朗读', () async {
                  final ok = await Speaker.instance.toggle(m.id, m.content, rate: ref.read(settingsProvider).ttsRate);
                  if (!ok && context.mounted) showToast(context, '这台手机暂时不支持朗读，可以在系统设置里安装中文语音');
                }, color: on ? p.accent : null);
              },
            ),
          if (isLast) btn(AppIcons.refresh, '重新回答', () => ref.read(chatProvider.notifier).regenerate()),
          if (m.content.isNotEmpty) btn(AppIcons.more, '更多', () => showAssistantMessageActions(context, ref, m, isLast)),
          if (usedModel != null)
            Flexible(
              child: Padding(
                padding: const EdgeInsets.only(left: 4),
                child: Text(usedModel.displayName, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.faint)),
              ),
            ),
        ]),
      ),
    );
  }
}

/// Shown between sending and the first token (or during fast read-only tool calls).
class ThinkingIndicator extends StatefulWidget {
  const ThinkingIndicator({super.key, this.label = '', this.compact = false});
  final String label;
  final bool compact;
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
          if (!widget.compact) ...[
            const AnimatedMascot(size: 34, motion: MascotMotion.thinking),
            const SizedBox(width: 8),
          ],
          AnimatedBuilder(
            animation: _c,
            builder: (_, _) => Row(
              children: [
                for (var i = 0; i < 3; i++)
                  Container(
                    margin: const EdgeInsets.symmetric(horizontal: 3),
                    width: widget.compact ? 6 : 8,
                    height: widget.compact ? 6 : 8,
                    decoration: BoxDecoration(
                      color: p.accent.withValues(alpha: 0.25 + 0.75 * _pulse((_c.value - i * 0.18) % 1.0)),
                      shape: BoxShape.circle,
                    ),
                  ),
              ],
            ),
          ),
          if (widget.label.isNotEmpty) ...[
            const SizedBox(width: 8),
            Flexible(child: Text(widget.label, style: TextStyle(fontSize: 13.5, color: p.muted))),
          ],
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
                Icon(AppIcons.thinkOff, size: 16, color: p.accent),
                const SizedBox(width: 6),
                Text(label, style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w500)),
                const SizedBox(width: 3),
                AnimatedRotation(
                  turns: open ? 0.5 : 0,
                  duration: const Duration(milliseconds: 200),
                  child: Icon(AppIcons.chevronDown, size: 15, color: p.muted),
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

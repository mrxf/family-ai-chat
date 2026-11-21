import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show Clipboard, ClipboardData, HapticFeedback, Uint8List;
import 'package:gal/gal.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../../core/models/models.dart';
import '../../../../platform/speech/speaker.dart';
import '../../../../state/chat.dart';
import '../../../../state/session.dart';
import '../components/chat_components.dart';
import '../../../theme/icons.dart';
import '../../../widgets/sheets.dart';
import '../../../widgets/mascot/mascot_anim.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

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
          errorBuilder: (_, _, _) => Container(color: context.pal.surfaceAlt, child: AppIcon(AppIcons.brokenImage, color: context.pal.faint)));
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
                    : const AppIcon(AppIcons.download),
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
  List<RunStep> _settledSteps = const [];

  @override
  void didUpdateWidget(covariant AssistantMessage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.message.id != widget.message.id) {
      _wasLive = false;
      _settledSteps = const [];
    }
  }

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
        final sources = isLive ? live.sources : m.sources;
        final components = isLive ? live.components : m.components;
        if (isLive && live.steps.isNotEmpty) {
          _settledSteps = [
            for (final s in live.steps)
              s.running ? RunStep(kind: s.kind, title: s.title, detail: s.detail, status: 'done') : s,
          ];
        }
        final steps = isLive ? live.steps : _settledSteps;
        final hasStepPanel = steps.isNotEmpty || sources.isNotEmpty;
        final hasRunningStep = steps.any((s) => s.running);
        final drawing = isLive && live.drawing;
        final working = isLive && live.working;
        final phase = isLive ? live.phase : null;
        return Padding(
          padding: const EdgeInsets.fromLTRB(18, 8, 18, 4),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (hasStepPanel)
                TaskAndSourcesPanel(
                  steps: steps,
                  sources: sources,
                  isLive: isLive,
                  waitingForReply: isLive && phase == LivePhase.waiting && !drawing,
                  activity: live.activity,
                  activitySince: live.activitySince,
                ),
              if (reasoning.isNotEmpty) ReasoningPanel(text: reasoning, thinking: phase == LivePhase.reasoning, durationMs: m.durationMs),
              AnimatedSwitcher(
                duration: const Duration(milliseconds: 250),
                child: phase == LivePhase.waiting && !drawing && drawn.isEmpty && components.isEmpty && !hasStepPanel
                    ? ThinkingIndicator(
                        label: working ? live.activity : '正在思考…',
                        since: live.activitySince,
                      )
                    : const SizedBox(width: double.infinity),
              ),
              if (content.isNotEmpty || components.isNotEmpty)
                InterleavedAssistantBody(
                  messageId: m.id,
                  content: content,
                  components: components,
                  streaming: isLive,
                  animate: _wasLive,
                  isLastMessage: widget.isLast,
                ),
              if (working && phase != LivePhase.waiting && !hasRunningStep)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: ThinkingIndicator(label: live.activity, since: live.activitySince, compact: true),
                ),
              if (drawn.isNotEmpty || drawing)
                Padding(
                  padding: EdgeInsets.only(top: (content.isEmpty && components.isEmpty) ? 2 : 10, bottom: 4),
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
            AppIcon(icon, size: 16, color: color),
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
            AppIcon(AppIcons.cloudOff, size: 20, color: p.bad),
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
    if (m.content.isNotEmpty || m.attachments.isNotEmpty || m.components.isNotEmpty || isLast) {
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
            child: Padding(padding: const EdgeInsets.all(9), child: AppIcon(icon, size: 20, color: color ?? p.muted)),
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
  const ThinkingIndicator({super.key, this.label = '', this.since, this.compact = false});
  final String label;
  final DateTime? since;
  final bool compact;
  @override
  State<ThinkingIndicator> createState() => _ThinkingIndicatorState();
}

class _ThinkingIndicatorState extends State<ThinkingIndicator> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1400))..repeat();
  final DateTime _created = DateTime.now();
  Timer? _tick;

  @override
  void initState() {
    super.initState();
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _tick?.cancel();
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final secs = DateTime.now().difference(widget.since ?? _created).inSeconds.clamp(0, 9999);
    final baseLabel = widget.label;
    final fullLabel = baseLabel.isNotEmpty && secs >= 5 ? '$baseLabel（${secs}s）' : baseLabel;
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
          if (fullLabel.isNotEmpty) ...[
            const SizedBox(width: 8),
            Flexible(child: Text(fullLabel, style: TextStyle(fontSize: 13.5, color: p.muted))),
          ],
        ],
      ),
    );
  }

  double _pulse(double t) => t < 0.5 ? Curves.easeOut.transform(t * 2) : Curves.easeIn.transform((1 - t) * 2);
}

/// Live task progress (memory lookup, web search, webpage reading) and collapsible
/// web reference list. Expands automatically while tasks are running so the user
/// sees every step in real time, then folds into a compact pill once the reply flows.
class TaskAndSourcesPanel extends StatefulWidget {
  const TaskAndSourcesPanel({
    super.key,
    required this.steps,
    required this.sources,
    required this.isLive,
    required this.waitingForReply,
    this.activity = '',
    this.activitySince,
  });

  final List<RunStep> steps;
  final List<WebSource> sources;
  final bool isLive;
  final bool waitingForReply;
  final String activity;
  final DateTime? activitySince;

  @override
  State<TaskAndSourcesPanel> createState() => _TaskAndSourcesPanelState();
}

class _TaskAndSourcesPanelState extends State<TaskAndSourcesPanel> {
  bool? _userOpen;
  Timer? _tick;

  bool get _activelyWorking =>
      widget.isLive && (widget.steps.any((s) => s.running) || widget.waitingForReply);

  @override
  void initState() {
    super.initState();
    _syncTimer();
  }

  @override
  void didUpdateWidget(covariant TaskAndSourcesPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    _syncTimer();
  }

  void _syncTimer() {
    if (_activelyWorking && _tick == null) {
      _tick = Timer.periodic(const Duration(seconds: 1), (_) {
        if (mounted) setState(() {});
      });
    } else if (!_activelyWorking && _tick != null) {
      _tick?.cancel();
      _tick = null;
    }
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  static IconData _iconForStep(String kind) => switch (kind) {
        'search' => AppIcons.globe,
        'fetch' => AppIcons.article,
        'history' => AppIcons.history,
        'memory' => AppIcons.manageSearch,
        'schedule' => AppIcons.calendar,
        'component' => AppIcons.cards,
        'ask_user' => AppIcons.question,
        'skill' => AppIcons.sparkles,
        _ => AppIcons.sparkles,
      };

  static String _sitePreview(List<WebSource> sources) {
    final names = <String>[];
    for (final s in sources) {
      final n = s.siteName.trim().isNotEmpty ? s.siteName.trim() : (Uri.tryParse(s.url)?.host ?? '');
      if (n.isNotEmpty && !names.contains(n)) {
        names.add(n);
        if (names.length >= 2) break;
      }
    }
    if (names.isEmpty) return '';
    return ' · ${names.join('、')}${sources.length > names.length ? '等' : ''}';
  }

  String _headerLabel(RunStep? running) {
    if (running != null) {
      final secs = running.startedAt != null
          ? DateTime.now().difference(running.startedAt!).inSeconds.clamp(0, 999)
          : 0;
      return secs >= 2 ? '正在${running.title}（${secs}s）' : '正在${running.title}…';
    }
    if (widget.waitingForReply) {
      final act = widget.activity.isNotEmpty ? widget.activity : '正在整理检索结果…';
      final secs = widget.activitySince != null
          ? DateTime.now().difference(widget.activitySince!).inSeconds.clamp(0, 999)
          : 0;
      return secs >= 3 ? '$act（${secs}s）' : act;
    }
    final hadMemory = widget.steps.any((s) => s.kind == 'memory');
    final hadHistory = widget.steps.any((s) => s.kind == 'history');
    final lastSchedule = widget.steps.where((s) => s.kind == 'schedule').lastOrNull;
    final lastAskUser = widget.steps.where((s) => s.kind == 'ask_user').lastOrNull;
    final lastComp = widget.steps.where((s) => s.kind == 'component').lastOrNull;
    final lastSkill = widget.steps.where((s) => s.kind == 'skill').lastOrNull;
    if (widget.sources.isNotEmpty) {
      final prefix = hadHistory && hadMemory
          ? '已翻看记录与记忆 · '
          : (hadHistory ? '已翻看前面的聊天记录 · ' : (hadMemory ? '已翻看记忆 · ' : ''));
      return '$prefix已参考 ${widget.sources.length} 篇网页${_sitePreview(widget.sources)}';
    }
    if (lastSchedule != null) {
      return lastSchedule.title.isNotEmpty ? lastSchedule.title : '已处理日程安排';
    }
    if (lastAskUser != null) {
      return lastAskUser.title.isNotEmpty ? lastAskUser.title : '等待确认偏好';
    }
    if (lastComp != null) {
      return lastComp.title.isNotEmpty ? lastComp.title : '已生成交互卡片';
    }
    final hasSearch = widget.steps.any((s) => s.kind == 'search' || s.kind == 'fetch');
    if (hasSearch) return '已完成联网检索';
    if (lastSkill != null) {
      return lastSkill.title.isNotEmpty ? lastSkill.title : '已加载扩展技能';
    }
    if (hadHistory && hadMemory) return '已翻看前面的聊天与记忆';
    if (hadHistory) return '已翻看前面的聊天记录';
    return '已翻看相关记忆';
  }

  Future<void> _openSource(String rawUrl) async {
    final uri = Uri.tryParse(rawUrl.trim());
    if (uri == null) return;
    final ok = await launchUrl(uri, mode: LaunchMode.externalApplication);
    if (!ok && mounted) showToast(context, '打不开网页链接');
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final running = widget.steps.where((s) => s.running).lastOrNull;
    final busy = running != null || widget.waitingForReply;
    final open = _userOpen ?? busy;
    final hasWeb = widget.sources.isNotEmpty || widget.steps.any((s) => s.kind == 'search' || s.kind == 'fetch');
    final hasSchedule = widget.steps.any((s) => s.kind == 'schedule');
    final hasAskUser = widget.steps.any((s) => s.kind == 'ask_user');
    final hasComponent = widget.steps.any((s) => s.kind == 'component');
    final hasSkill = widget.steps.any((s) => s.kind == 'skill');
    final onlyHistory = !hasWeb && !hasSchedule && !hasAskUser && !hasComponent && !hasSkill && widget.steps.isNotEmpty && widget.steps.every((s) => s.kind == 'history');
    final headerIcon = hasWeb
        ? AppIcons.globe
        : hasSchedule
            ? AppIcons.calendar
            : hasAskUser
                ? AppIcons.question
                : hasComponent
                    ? AppIcons.cards
                    : hasSkill
                        ? AppIcons.sparkles
                        : (onlyHistory ? AppIcons.history : AppIcons.manageSearch);

    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Pressable(
            onTap: () => setState(() => _userOpen = !open),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
              decoration: BoxDecoration(
                color: busy ? Color.lerp(p.surfaceAlt, p.accentSoft, 0.38) : p.surfaceAlt,
                borderRadius: BorderRadius.circular(20),
                border: busy ? Border.all(color: p.accent.withValues(alpha: 0.25)) : null,
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (busy)
                    SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(strokeWidth: 1.9, color: p.accent),
                    )
                  else
                    AppIcon(
                      headerIcon,
                      size: 16,
                      color: p.accent,
                    ),
                  const SizedBox(width: 7),
                  Flexible(
                    child: Text(
                      _headerLabel(running),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13.5,
                        color: busy ? p.text : p.muted,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ),
                  const SizedBox(width: 3),
                  AnimatedRotation(
                    turns: open ? 0.5 : 0,
                    duration: const Duration(milliseconds: 200),
                    child: AppIcon(AppIcons.chevronDown, size: 15, color: p.muted),
                  ),
                ],
              ),
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 240),
            curve: Curves.easeOutCubic,
            alignment: Alignment.topCenter,
            child: open
                ? Container(
                    margin: const EdgeInsets.only(top: 8),
                    padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
                    decoration: BoxDecoration(
                      color: p.surface,
                      borderRadius: BorderRadius.circular(16),
                      border: Border.all(color: p.line),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        for (var i = 0; i < widget.steps.length; i++) ...[
                          if (i > 0) const SizedBox(height: 7),
                          _StepRow(step: widget.steps[i], icon: _iconForStep(widget.steps[i].kind)),
                        ],
                        if (widget.waitingForReply && running == null) ...[
                          if (widget.steps.isNotEmpty) const SizedBox(height: 7),
                          Row(children: [
                            SizedBox(
                              width: 14,
                              height: 14,
                              child: CircularProgressIndicator(strokeWidth: 1.8, color: p.accent),
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                widget.activity.isNotEmpty ? widget.activity : '正在结合检索内容整理回答…',
                                style: TextStyle(fontSize: 13.5, color: p.text, fontWeight: FontWeight.w500),
                              ),
                            ),
                          ]),
                        ],
                        if (widget.sources.isNotEmpty) ...[
                          if (widget.steps.isNotEmpty) ...[
                            const SizedBox(height: 9),
                            Divider(height: 1, thickness: 0.7, color: p.line.withValues(alpha: 0.8)),
                            const SizedBox(height: 8),
                          ],
                          for (var i = 0; i < widget.sources.length; i++) ...[
                            if (i > 0) const SizedBox(height: 6),
                            _SourceTile(index: i + 1, source: widget.sources[i], onTap: () => _openSource(widget.sources[i].url)),
                          ],
                        ],
                      ],
                    ),
                  )
                : const SizedBox(width: double.infinity),
          ),
        ],
      ),
    );
  }
}

class _StepRow extends StatelessWidget {
  const _StepRow({required this.step, required this.icon});
  final RunStep step;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: step.running
              ? SizedBox(width: 15, height: 15, child: CircularProgressIndicator(strokeWidth: 1.8, color: p.accent))
              : AppIcon(
                  step.isError ? AppIcons.info : icon,
                  size: 16,
                  color: step.isError ? p.bad : p.accent,
                ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Wrap(
            spacing: 6,
            runSpacing: 2,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text(
                step.title,
                style: TextStyle(
                  fontSize: 13.5,
                  color: step.running ? p.text : p.muted,
                  fontWeight: step.running ? FontWeight.w500 : FontWeight.w400,
                ),
              ),
              if (step.detail.isNotEmpty)
                Text(
                  '· ${step.detail}',
                  style: TextStyle(fontSize: 12.5, color: step.isError ? p.bad : p.faint),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _SourceTile extends StatelessWidget {
  const _SourceTile({required this.index, required this.source, required this.onTap});
  final int index;
  final WebSource source;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final site = source.siteName.trim().isNotEmpty ? source.siteName.trim() : (Uri.tryParse(source.url)?.host ?? '网页');
    final meta = source.date.trim().isNotEmpty ? '$site · ${source.date.trim()}' : site;
    return Pressable(
      scale: 0.985,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: p.surfaceAlt.withValues(alpha: 0.7),
          borderRadius: BorderRadius.circular(11),
        ),
        child: Row(
          children: [
            Container(
              width: 20,
              height: 20,
              alignment: Alignment.center,
              decoration: BoxDecoration(
                color: p.surface,
                borderRadius: BorderRadius.circular(6),
                border: Border.all(color: p.line),
              ),
              child: Text(
                '$index',
                style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: p.muted),
              ),
            ),
            const SizedBox(width: 9),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    source.title.isNotEmpty ? source.title : site,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: p.text),
                  ),
                  const SizedBox(height: 1.5),
                  Text(
                    meta,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12, color: p.muted),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 6),
            AppIcon(AppIcons.externalLink, size: 15, color: p.faint),
          ],
        ),
      ),
    );
  }
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
                AppIcon(AppIcons.thinkOff, size: 16, color: p.accent),
                const SizedBox(width: 6),
                Text(label, style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w500)),
                const SizedBox(width: 3),
                AnimatedRotation(
                  turns: open ? 0.5 : 0,
                  duration: const Duration(milliseconds: 200),
                  child: AppIcon(AppIcons.chevronDown, size: 15, color: p.muted),
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

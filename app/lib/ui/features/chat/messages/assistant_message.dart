import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show Clipboard, ClipboardData;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../platform/platform_capabilities.dart';
import '../../../../platform/speech/speaker.dart';
import '../../../../state/chat.dart';
import '../../../../state/session.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/sheets.dart';
import '../components/chat_components.dart';
import 'activity_panels.dart';
import 'message_images.dart';

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
                  canPin: !isPrivate,
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
                  if (!ok && context.mounted) showToast(context, '这台${ref.read(platformCapabilitiesProvider).deviceNoun}暂时不支持朗读，可以在系统设置里安装中文语音');
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

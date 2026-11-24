import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/api.dart';
import '../../../../core/models/models.dart';
import '../../../../platform/platform_capabilities.dart';
import '../../../../state/chat.dart';
import '../../../../state/component_entities.dart';
import '../../../../state/conversations.dart';
import '../../../../state/pinned_components.dart';
import '../../../../state/scheduled_tasks.dart';
import '../../../../state/session.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/sheets.dart';
import '../../automations/scheduled_task_editor.dart';
import '../../automations/scheduled_task_runs_sheet.dart';
import 'component_card.dart';
import 'component_editor.dart';
import 'component_version_sheet.dart';

/// Returns the representative icon for a component [kind].
IconData componentKindIcon(String kind) => switch (kind) {
      'weather' => AppIcons.cloudSun,
      'todo' => AppIcons.listChecks,
      'feed' => AppIcons.article,
      'chart' => AppIcons.chart,
      'metrics' => AppIcons.gauge,
      'quiz' => AppIcons.question,
      _ => AppIcons.cards,
    };

/// Interactive wrapper for a pinnable component inside a conversation message.
///
/// Renders historical version banners (with one-tap toggle to preview the latest
/// version) and adds pin / edit / history / schedule / quote actions below the card.
class ChatComponentActionShell extends ConsumerStatefulWidget {
  const ChatComponentActionShell({
    super.key,
    required this.messageId,
    required this.component,
    required this.child,
    this.isLastMessage = false,
    this.enabled = true,
  });

  final String messageId;
  final MessageComponent component;
  final Widget child;
  final bool isLastMessage;
  final bool enabled;

  @override
  ConsumerState<ChatComponentActionShell> createState() => _ChatComponentActionShellState();
}

class _ChatComponentActionShellState extends ConsumerState<ChatComponentActionShell> {
  bool _hovered = false;
  bool _viewingLatest = false;
  bool _loadingLatest = false;

  MessageComponent _activeComponent(ComponentEntity? liveEntity) {
    if (_viewingLatest && liveEntity != null) {
      return liveEntity.toMessageComponent(idOverride: widget.component.id, pos: widget.component.pos);
    }
    return widget.component;
  }

  Future<void> _toggleLatest() async {
    if (_viewingLatest) {
      setState(() => _viewingLatest = false);
      return;
    }
    final entityId = widget.component.entityId;
    if (entityId.isEmpty) return;
    setState(() => _loadingLatest = true);
    final ent = await ref.read(componentEntitiesProvider.notifier).fetchLatest(entityId);
    if (!mounted) return;
    setState(() {
      _loadingLatest = false;
      if (ent != null) {
        _viewingLatest = true;
      } else {
        showToast(context, '暂时无法加载最新版本');
      }
    });
  }

  void _openVersionHistory(MessageComponent comp) {
    if (!comp.hasEntity) return;
    final meta = describeComponent(comp);
    showComponentVersionHistory(context, entityId: comp.entityId, fallbackTitle: meta.title);
  }

  Future<void> _togglePin(bool isPinned, MessageComponent comp) async {
    final notifier = ref.read(pinnedComponentsProvider.notifier);
    if (isPinned) {
      final err = await notifier.unpin(widget.messageId, widget.component.id, entityId: comp.entityId);
      if (mounted) showToast(context, err ?? '已取消固定');
      return;
    }
    final chat = ref.read(chatProvider);
    final convId = chat.conversationId ?? '';
    final title = convId.isEmpty ? '' : (ref.read(conversationsTitlesProvider)[convId] ?? '');
    final err = await notifier.pin(
      messageId: widget.messageId,
      component: comp,
      conversationId: convId,
      conversationTitle: title,
    );
    if (mounted) showToast(context, err ?? '已固定到首页');
  }

  void _quoteInNewChat(MessageComponent comp) {
    final chat = ref.read(chatProvider);
    final convId = chat.conversationId ?? '';
    final title = convId.isEmpty ? '' : (ref.read(conversationsTitlesProvider)[convId] ?? '');
    final quote = QuotedComponent.fromComponent(
      messageId: widget.messageId,
      component: comp,
      conversationId: convId,
      conversationTitle: title,
    );
    ref.read(chatProvider.notifier).quoteComponentToNewChat(quote);
  }

  Future<void> _editComponent(MessageComponent comp) async {
    final saved = await showComponentEditor(
      context,
      component: comp,
      onSave: (nextData) async {
        if (comp.hasEntity) {
          final err = await ref.read(componentEntitiesProvider.notifier).updateData(comp.entityId, nextData);
          if (err == null) {
            setState(() => _viewingLatest = true);
          }
          return err;
        }
        return ref.read(chatProvider.notifier).updateComponentData(widget.messageId, widget.component.id, nextData);
      },
    );
    if (saved != null && mounted) showToast(context, '卡片内容已更新');
  }

  Future<void> _scheduleUpdate(MessageComponent comp) async {
    final meta = describeComponent(comp);
    final isFeed = comp.kind == 'feed';
    await showScheduledTaskEditor(
      context,
      title: '为此卡片设定时更新',
      initialTitle: '定时更新：${meta.title}',
      initialPrompt: isFeed
          ? '查询最新内容，更新《${meta.title}》卡片的前 3 条，把原来的前 3 条放到第 4-6 条（tag 标为往期）。'
          : '根据最新情况分析和更新《${meta.title}》卡片内容。',
      initialTargetEntityId: comp.entityId.isNotEmpty ? comp.entityId : widget.component.entityId,
      initialTargetMessageId: widget.messageId,
      initialTargetComponentId: widget.component.id,
      onSave: (draft) async {
        await ref.read(apiProvider).createScheduledTask(draft);
        ref.invalidate(scheduledTasksProvider);
        if (mounted) showToast(context, '已为这张卡片创建定时任务');
      },
    );
  }

  Future<void> _deleteComponent() async {
    final ok = await confirm(
      context,
      title: '删除这张卡片？',
      body: '删除后无法恢复，若已固定在首页也会同步移除。',
      ok: '删除',
      danger: true,
    );
    if (!ok || !mounted) return;
    final err = await ref.read(pinnedComponentsProvider.notifier).deleteComponent(widget.messageId, widget.component.id);
    if (mounted) showToast(context, err ?? '已删除卡片');
  }

  void _openMenu(bool isPinned, MessageComponent comp) {
    showActionSheet(context, actions: [
      if (widget.component.isOutdated)
        SheetAction(
          AppIcons.history,
          _viewingLatest ? '切回历史版本 v${widget.component.effectiveVersion}' : '查看最新版本 v${widget.component.effectiveLatestVersion}',
          _toggleLatest,
        ),
      if (comp.hasEntity)
        SheetAction(
          AppIcons.history,
          '查看版本历史（v${comp.effectiveLatestVersion}）',
          () => _openVersionHistory(comp),
        ),
      SheetAction(
        isPinned ? AppIcons.unpin : AppIcons.pin,
        isPinned ? '从首页取消固定' : '固定到首页',
        () => _togglePin(isPinned, comp),
      ),
      if (isComponentDirectlyEditable(comp))
        SheetAction(AppIcons.edit, '编辑卡片内容', () => _editComponent(comp)),
      SheetAction(AppIcons.timer, '设为定时更新', () => _scheduleUpdate(comp)),
      SheetAction(AppIcons.chatBubble, '基于此卡片开新对话', () => _quoteInNewChat(comp)),
      SheetAction(AppIcons.delete, '删除卡片', _deleteComponent, danger: true),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.enabled || !isComponentPinnable(widget.component)) {
      return widget.child;
    }
    final isDesktop = ref.watch(platformCapabilitiesProvider).isDesktop;
    final liveEntity = widget.component.hasEntity
        ? ref.watch(componentEntitiesProvider.select((m) => m[widget.component.entityId]))
        : null;
    final activeComp = _activeComponent(liveEntity);
    final isPinned = ref.watch(
      pinnedComponentsProvider.select(
        (s) => s.isPinned(widget.messageId, widget.component.id, entityId: widget.component.entityId),
      ),
    );
    final latestVer = liveEntity != null && liveEntity.currentVersion > widget.component.effectiveLatestVersion
        ? liveEntity.currentVersion
        : widget.component.effectiveLatestVersion;
    final isOutdated = widget.component.hasEntity && latestVer > widget.component.effectiveVersion;
    final isUpdatedBadge = widget.component.hasEntity && !isOutdated && widget.component.effectiveVersion > 1;
    final showBar = !isDesktop || _hovered || isPinned;

    Widget cardBody;
    if (_viewingLatest && liveEntity != null) {
      cardBody = ChatComponentCard(
        messageId: widget.messageId,
        component: activeComp,
        isLastMessage: widget.isLastMessage,
        onStateChanged: (nextState) =>
            ref.read(componentEntitiesProvider.notifier).updateState(liveEntity.id, nextState),
      );
    } else if (isOutdated) {
      // Historical snapshot is read-only to prevent accidentally mutating superseded versions.
      cardBody = GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => showToast(context, '当前为历史版本 v${widget.component.effectiveVersion}，点击上方「查看最新」可交互'),
        child: IgnorePointer(child: Opacity(opacity: 0.92, child: widget.child)),
      );
    } else {
      cardBody = widget.child;
    }

    return MouseRegion(
      onEnter: isDesktop ? (_) => setState(() => _hovered = true) : null,
      onExit: isDesktop ? (_) => setState(() => _hovered = false) : null,
      child: GestureDetector(
        behavior: HitTestBehavior.translucent,
        onSecondaryTap: () => _openMenu(isPinned, activeComp),
        onLongPress: () => _openMenu(isPinned, activeComp),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (isOutdated)
              ComponentVersionBannerBar(
                anchoredVersion: widget.component.effectiveVersion,
                latestVersion: latestVer,
                viewingLatest: _viewingLatest,
                loading: _loadingLatest,
                deleted: widget.component.deleted || (liveEntity?.deleted ?? false),
                onToggleLatest: _toggleLatest,
                onOpenHistory: () => _openVersionHistory(activeComp),
              )
            else if (isUpdatedBadge)
              ComponentUpdatedBadgeBar(
                version: widget.component.effectiveVersion,
                onOpenHistory: () => _openVersionHistory(activeComp),
              ),
            cardBody,
            AnimatedOpacity(
              opacity: showBar ? 1.0 : 0.0,
              duration: const Duration(milliseconds: 160),
              child: IgnorePointer(
                ignoring: !showBar,
                child: Padding(
                  padding: const EdgeInsets.only(top: 5, left: 4, right: 4),
                  child: Wrap(
                    alignment: WrapAlignment.end,
                    spacing: 6,
                    runSpacing: 4,
                    children: [
                      _InlineActionPill(
                        icon: AppIcons.pin,
                        label: isPinned ? '已固定' : '固定到首页',
                        accent: isPinned,
                        tooltip: isPinned ? '点击取消固定' : '固定到首页看板',
                        onTap: () => _togglePin(isPinned, activeComp),
                      ),
                      if (widget.component.hasEntity)
                        _InlineActionPill(
                          icon: AppIcons.history,
                          label: 'v$latestVer',
                          tooltip: '查看卡片版本历史（最多保留最近 10 个版本）',
                          onTap: () => _openVersionHistory(activeComp),
                        ),
                      if (isComponentDirectlyEditable(activeComp))
                        _InlineActionPill(
                          icon: AppIcons.edit,
                          label: '编辑',
                          tooltip: '手动修改卡片内容',
                          onTap: () => _editComponent(activeComp),
                        ),
                      _InlineActionPill(
                        icon: AppIcons.timer,
                        label: '定时更新',
                        tooltip: '为此卡片设置 AI 定时自动更新',
                        onTap: () => _scheduleUpdate(activeComp),
                      ),
                      _InlineActionPill(
                        icon: AppIcons.chatBubble,
                        label: '引用提问',
                        tooltip: '基于这张卡片开启新对话',
                        onTap: () => _quoteInNewChat(activeComp),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Wrapper for a pinned component rendered on the home board.
///
/// Displays source conversation metadata, live version pill, bound scheduled-task badge,
/// and quick actions (jump to source, version history, edit, schedule, reorder, quote, unpin).
class HomePinnedComponentShell extends ConsumerWidget {
  const HomePinnedComponentShell({
    super.key,
    required this.pin,
    required this.child,
    this.canMovePrev = false,
    this.canMoveNext = false,
  });

  final PinnedComponent pin;
  final Widget child;
  final bool canMovePrev;
  final bool canMoveNext;

  void _openSource(WidgetRef ref) {
    if (pin.conversationId.isEmpty) return;
    ref.read(chatProvider.notifier).open(pin.conversationId, focusMessageId: pin.messageId);
  }

  void _openVersionHistory(BuildContext context) {
    final entityId = pin.effectiveEntityId;
    if (entityId.isEmpty) return;
    final meta = describeComponent(pin.component);
    showComponentVersionHistory(context, entityId: entityId, fallbackTitle: meta.title);
  }

  void _quoteInNewChat(WidgetRef ref) {
    final quote = QuotedComponent.fromComponent(
      messageId: pin.messageId,
      component: pin.component,
      conversationId: pin.conversationId,
      conversationTitle: pin.conversationTitle,
    );
    ref.read(chatProvider.notifier).quoteComponentToNewChat(quote);
  }

  Future<void> _editComponent(BuildContext context, WidgetRef ref) async {
    final saved = await showComponentEditor(
      context,
      component: pin.component,
      onSave: (nextData) {
        final entId = pin.effectiveEntityId;
        if (entId.isNotEmpty) {
          return ref.read(componentEntitiesProvider.notifier).updateData(entId, nextData);
        }
        return ref.read(pinnedComponentsProvider.notifier).updateComponentData(pin.messageId, pin.componentId, nextData);
      },
    );
    if (saved != null && context.mounted) showToast(context, '卡片内容已更新');
  }

  Future<void> _openOrCreateSchedule(BuildContext context, WidgetRef ref, ScheduledTask? bound) async {
    if (bound != null) {
      await showScheduledTaskEditor(
        context,
        title: '编辑卡片定时更新',
        existing: bound,
        onSave: (draft) async {
          await ref.read(apiProvider).updateScheduledTask(bound.id, draft.toJson());
          ref.invalidate(scheduledTasksProvider);
          if (context.mounted) showToast(context, '定时任务已更新');
        },
      );
      return;
    }
    final meta = describeComponent(pin.component);
    final isFeed = pin.component.kind == 'feed';
    await showScheduledTaskEditor(
      context,
      title: '为此卡片设定时更新',
      initialTitle: '定时更新：${meta.title}',
      initialPrompt: isFeed
          ? '查询最新内容，更新《${meta.title}》卡片的前 3 条，把原来的前 3 条放到第 4-6 条（tag 标为往期）。'
          : '根据最新情况分析和更新《${meta.title}》卡片内容。',
      initialTargetEntityId: pin.effectiveEntityId,
      initialTargetMessageId: pin.messageId,
      initialTargetComponentId: pin.componentId,
      onSave: (draft) async {
        await ref.read(apiProvider).createScheduledTask(draft);
        ref.invalidate(scheduledTasksProvider);
        if (context.mounted) showToast(context, '已为这张卡片创建定时任务');
      },
    );
  }

  Future<void> _move(BuildContext context, WidgetRef ref, int delta) async {
    final err = await ref.read(pinnedComponentsProvider.notifier).movePin(pin.id, delta);
    if (err != null && context.mounted) showToast(context, err);
  }

  Future<void> _unpin(BuildContext context, WidgetRef ref) async {
    final err = await ref
        .read(pinnedComponentsProvider.notifier)
        .unpin(pin.messageId, pin.componentId, entityId: pin.effectiveEntityId);
    if (context.mounted) showToast(context, err ?? '已从首页取消固定');
  }

  Future<void> _deleteComponent(BuildContext context, WidgetRef ref) async {
    final ok = await confirm(
      context,
      title: '永久删除这张卡片？',
      body: '卡片将从首页看板中移除，且无法恢复。',
      ok: '删除',
      danger: true,
    );
    if (!ok || !context.mounted) return;
    final err = await ref.read(pinnedComponentsProvider.notifier).deleteComponent(pin.messageId, pin.componentId);
    if (context.mounted) showToast(context, err ?? '已删除卡片');
  }

  Future<void> _runBoundTaskNow(BuildContext context, WidgetRef ref, ScheduledTask bound) async {
    if (bound.isRunning) return;
    try {
      await ref.read(apiProvider).runScheduledTaskNow(bound.id);
      ref.invalidate(scheduledTasksProvider);
      if (context.mounted) showToast(context, '已开始在后台更新，完成后会自动刷新卡片');
    } catch (e) {
      if (context.mounted) showToast(context, ApiError.from(e).message);
    }
  }

  void _openMenu(BuildContext context, WidgetRef ref, ScheduledTask? bound) {
    final ver = pin.component.effectiveVersion;
    showActionSheet(context, actions: [
      if (bound != null)
        SheetAction(
          AppIcons.refresh,
          bound.isRunning ? '后台更新中…' : '立即更新一次',
          () => _runBoundTaskNow(context, ref, bound),
        ),
      if (pin.effectiveEntityId.isNotEmpty)
        SheetAction(AppIcons.history, '查看版本历史（v$ver）', () => _openVersionHistory(context)),
      if (pin.conversationId.isNotEmpty)
        SheetAction(AppIcons.arrowUpRight, '跳转到原对话', () => _openSource(ref)),
      if (isComponentDirectlyEditable(pin.component))
        SheetAction(AppIcons.edit, '编辑卡片内容', () => _editComponent(context, ref)),
      SheetAction(
        AppIcons.timer,
        bound != null ? '编辑定时更新（${bound.scheduleLabel}）' : '设为定时更新',
        () => _openOrCreateSchedule(context, ref, bound),
      ),
      if (bound != null)
        SheetAction(AppIcons.history, '查看定时执行记录', () => showScheduledTaskRunsSheet(context, bound)),
      if (canMovePrev)
        SheetAction(AppIcons.chevronUp, '向前移动', () => _move(context, ref, -1)),
      if (canMoveNext)
        SheetAction(AppIcons.chevronDown, '向后移动', () => _move(context, ref, 1)),
      SheetAction(AppIcons.chatBubble, '基于此卡片开新对话', () => _quoteInNewChat(ref)),
      SheetAction(AppIcons.unpin, '取消固定', () => _unpin(context, ref)),
      SheetAction(AppIcons.delete, '永久删除卡片', () => _deleteComponent(context, ref), danger: true),
    ]);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final sourceTitle = pin.conversationTitle.trim().isEmpty ? '原对话' : pin.conversationTitle.trim();
    final boundTask = ref.watch(
      scheduledTasksProvider.select(
        (async) => async.value?.findByTarget(pin.messageId, pin.componentId, entityId: pin.effectiveEntityId),
      ),
    );
    final hasEntity = pin.effectiveEntityId.isNotEmpty;
    final ver = pin.component.effectiveVersion;

    return GestureDetector(
      behavior: HitTestBehavior.translucent,
      onSecondaryTap: () => _openMenu(context, ref, boundTask),
      onLongPress: () => _openMenu(context, ref, boundTask),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          child,
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 6, 4, 2),
            child: Row(
              children: [
                Expanded(
                  child: Row(
                    children: [
                      Flexible(
                        child: Tooltip(
                          message: '跳转到原对话：$sourceTitle',
                          child: Pressable(
                            scale: 0.98,
                            onTap: pin.conversationId.isEmpty ? null : () => _openSource(ref),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                AppIcon(AppIcons.arrowUpRight, size: 13.5, color: p.faint),
                                const SizedBox(width: 4),
                                Flexible(
                                  child: Text(
                                    sourceTitle,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(fontSize: 12.5, color: p.muted),
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                      if (hasEntity) ...[
                        const SizedBox(width: 8),
                        Tooltip(
                          message: '当前为最新版本 v$ver，点击查看版本历史',
                          child: Pressable(
                            scale: 0.96,
                            onTap: () => _openVersionHistory(context),
                            child: Container(
                              padding: const EdgeInsets.symmetric(horizontal: 6.5, vertical: 2),
                              decoration: BoxDecoration(
                                color: p.surfaceAlt,
                                borderRadius: BorderRadius.circular(8),
                                border: Border.all(color: p.line.withValues(alpha: 0.7)),
                              ),
                              child: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  AppIcon(AppIcons.history, size: 11, color: p.muted),
                                  const SizedBox(width: 3),
                                  Text(
                                    'v$ver',
                                    style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: p.muted),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ),
                      ],
                      if (boundTask != null) ...[
                        const SizedBox(width: 6),
                        Tooltip(
                          message: boundTask.isRunning && (boundTask.conversationId?.isNotEmpty ?? false)
                              ? '点击查看实时执行过程'
                              : '点击查看或管理定时更新（${boundTask.scheduleLabel}）',
                          child: Pressable(
                            scale: 0.96,
                            onTap: () {
                              final cid = boundTask.conversationId ?? '';
                              if (boundTask.isRunning && cid.isNotEmpty) {
                                ref.read(chatProvider.notifier).open(cid);
                                return;
                              }
                              showScheduledTaskRunsSheet(context, boundTask);
                            },
                            child: Container(
                              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                              decoration: BoxDecoration(
                                color: boundTask.enabled ? p.accentSoft : p.surfaceAlt,
                                borderRadius: BorderRadius.circular(8),
                              ),
                              child: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  AppIcon(AppIcons.timer, size: 11.5, color: boundTask.enabled ? p.accent : p.muted),
                                  const SizedBox(width: 3),
                                  Text(
                                    boundTask.isRunning ? '更新中…' : boundTask.scheduleLabel,
                                    style: TextStyle(
                                      fontSize: 11,
                                      fontWeight: FontWeight.w600,
                                      color: boundTask.enabled ? p.accent : p.muted,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
                const SizedBox(width: 4),
                if (boundTask != null)
                  Tooltip(
                    message: boundTask.isRunning ? '后台更新中…' : '立即运行定时更新',
                    child: Pressable(
                      scale: 0.92,
                      onTap: boundTask.isRunning ? null : () => _runBoundTaskNow(context, ref, boundTask),
                      child: Padding(
                        padding: const EdgeInsets.all(6),
                        child: boundTask.isRunning
                            ? SizedBox(
                                width: 14,
                                height: 14,
                                child: CircularProgressIndicator(strokeWidth: 1.8, color: p.accent),
                              )
                            : AppIcon(AppIcons.refresh, size: 16, color: p.accent),
                      ),
                    ),
                  ),
                if (isComponentDirectlyEditable(pin.component))
                  _HomeIconAction(
                    icon: AppIcons.edit,
                    tooltip: '编辑卡片内容',
                    onTap: () => _editComponent(context, ref),
                  ),
                _HomeIconAction(
                  icon: AppIcons.chatBubble,
                  tooltip: '基于此卡片聊一聊',
                  onTap: () => _quoteInNewChat(ref),
                ),
                _HomeIconAction(
                  icon: AppIcons.more,
                  tooltip: '更多操作（版本历史 / 定时更新 / 排序 / 取消固定）',
                  onTap: () => _openMenu(context, ref, boundTask),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineActionPill extends StatelessWidget {
  const _InlineActionPill({
    required this.icon,
    required this.label,
    required this.tooltip,
    required this.onTap,
    this.accent = false,
  });

  final IconData icon;
  final String label;
  final String tooltip;
  final VoidCallback onTap;
  final bool accent;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: tooltip,
      child: Pressable(
        scale: 0.96,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
          decoration: BoxDecoration(
            color: accent ? p.accentSoft : p.surfaceAlt.withValues(alpha: 0.72),
            borderRadius: BorderRadius.circular(10),
            border: Border.all(
              color: accent ? p.accent.withValues(alpha: 0.28) : p.line.withValues(alpha: 0.65),
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              AppIcon(icon, size: 13.5, color: accent ? p.accent : p.muted),
              const SizedBox(width: 4),
              Text(
                label,
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: accent ? FontWeight.w600 : FontWeight.w500,
                  color: accent ? p.accent : p.muted,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HomeIconAction extends StatelessWidget {
  const _HomeIconAction({required this.icon, required this.tooltip, required this.onTap});
  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: tooltip,
      child: Pressable(
        scale: 0.92,
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(6),
          child: AppIcon(icon, size: 16, color: p.muted),
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../state/chat.dart';
import '../../../../state/session.dart';
import '../../../adaptive/adaptive_modal.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import 'component_card.dart';

/// Banner shown above a historical conversation card when a newer version exists.
class ComponentVersionBannerBar extends StatelessWidget {
  const ComponentVersionBannerBar({
    super.key,
    required this.anchoredVersion,
    required this.latestVersion,
    required this.viewingLatest,
    required this.loading,
    required this.deleted,
    required this.onToggleLatest,
    required this.onOpenHistory,
  });

  final int anchoredVersion;
  final int latestVersion;
  final bool viewingLatest;
  final bool loading;
  final bool deleted;
  final VoidCallback onToggleLatest;
  final VoidCallback onOpenHistory;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final label = deleted
        ? '历史快照 v$anchoredVersion · 该卡片已删除'
        : (viewingLatest ? '正在预览最新 v$latestVersion' : '历史版本 v$anchoredVersion · 已有更新 v$latestVersion');

    return Padding(
      padding: const EdgeInsets.only(bottom: 5),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
        decoration: BoxDecoration(
          color: viewingLatest ? p.accentSoft.withValues(alpha: 0.85) : p.surfaceAlt.withValues(alpha: 0.85),
          borderRadius: BorderRadius.circular(10),
          border: Border.all(
            color: viewingLatest ? p.accent.withValues(alpha: 0.35) : p.line.withValues(alpha: 0.8),
          ),
        ),
        child: Row(
          children: [
            AppIcon(AppIcons.history, size: 13.5, color: viewingLatest ? p.accent : p.muted),
            const SizedBox(width: 6),
            Expanded(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                  color: viewingLatest ? p.accent : p.muted,
                ),
              ),
            ),
            if (!deleted) ...[
              const SizedBox(width: 8),
              Tooltip(
                message: viewingLatest ? '切回本条对话当时的历史快照' : '原地切换到最新版本查看和交互',
                child: Pressable(
                  scale: 0.96,
                  onTap: loading ? null : onToggleLatest,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2.5),
                    decoration: BoxDecoration(
                      color: viewingLatest ? p.surface : p.accent,
                      borderRadius: BorderRadius.circular(8),
                      border: viewingLatest ? Border.all(color: p.accent.withValues(alpha: 0.4)) : null,
                    ),
                    child: Text(
                      loading ? '加载中…' : (viewingLatest ? '切回 v$anchoredVersion' : '查看最新'),
                      style: TextStyle(
                        fontSize: 11.5,
                        fontWeight: FontWeight.w600,
                        color: viewingLatest ? p.accent : Colors.white,
                      ),
                    ),
                  ),
                ),
              ),
            ],
            const SizedBox(width: 6),
            Tooltip(
              message: '查看完整版本历史（只读）',
              child: Pressable(
                scale: 0.95,
                onTap: onOpenHistory,
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
                  child: Text(
                    '历史',
                    style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: p.muted),
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

/// Compact badge shown above an updated card rendered in the reply that updated it.
class ComponentUpdatedBadgeBar extends StatelessWidget {
  const ComponentUpdatedBadgeBar({super.key, required this.version, required this.onOpenHistory});
  final int version;
  final VoidCallback onOpenHistory;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.only(bottom: 5),
      child: Row(
        children: [
          Tooltip(
            message: '已原地更新至 v$version，点击查看历史版本',
            child: Pressable(
              scale: 0.96,
              onTap: onOpenHistory,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 8.5, vertical: 3),
                decoration: BoxDecoration(
                  color: p.accentSoft.withValues(alpha: 0.8),
                  borderRadius: BorderRadius.circular(9),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(AppIcons.history, size: 12.5, color: p.accent),
                    const SizedBox(width: 4),
                    Text(
                      '已更新 · v$version',
                      style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: p.accent),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Opens a read-only version history sheet/dialog for a canonical component [entityId].
///
/// Displays up to the latest 10 retained versions with provenance metadata,
/// allows jumping to the originating conversation, and lets users expand any
/// historical snapshot in place as a read-only preview card.
Future<void> showComponentVersionHistory(
  BuildContext context, {
  required String entityId,
  required String fallbackTitle,
}) {
  return showAdaptiveModal<void>(
    context: context,
    maxWidth: 560,
    builder: (ctx) => _ComponentVersionHistoryBody(
      entityId: entityId,
      fallbackTitle: fallbackTitle,
    ),
  );
}

class _ComponentVersionHistoryBody extends ConsumerStatefulWidget {
  const _ComponentVersionHistoryBody({
    required this.entityId,
    required this.fallbackTitle,
  });

  final String entityId;
  final String fallbackTitle;

  @override
  ConsumerState<_ComponentVersionHistoryBody> createState() => _ComponentVersionHistoryBodyState();
}

class _ComponentVersionHistoryBodyState extends ConsumerState<_ComponentVersionHistoryBody> {
  Future<ComponentVersionsResponse>? _future;
  int? _expandedVersion;

  @override
  void initState() {
    super.initState();
    _load();
  }

  void _load() {
    final api = ref.read(sessionProvider).api;
    if (api == null) return;
    _future = api.listComponentVersions(widget.entityId);
  }

  String _formatTime(int ms) {
    if (ms <= 0) return '';
    final dt = DateTime.fromMillisecondsSinceEpoch(ms).toLocal();
    final now = DateTime.now();
    final diff = now.difference(dt);
    if (diff.inMinutes < 1) return '刚刚';
    if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
    if (diff.inHours < 24 && dt.day == now.day) {
      return '今天 ${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
    }
    return '${dt.month}月${dt.day}日 ${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
  }

  void _jumpToConversation(String convId, String msgId) {
    if (convId.isEmpty) return;
    Navigator.of(context).pop();
    ref.read(chatProvider.notifier).open(convId, focusMessageId: msgId.isEmpty ? null : msgId);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final future = _future;

    return Padding(
      padding: const EdgeInsets.fromLTRB(18, 14, 18, 18),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              AppIcon(AppIcons.history, size: 19, color: p.accent),
              const SizedBox(width: 8),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '版本历史',
                      style: TextStyle(fontSize: 16.5, fontWeight: FontWeight.w700, color: p.text),
                    ),
                    Text(
                      '${widget.fallbackTitle} · 最多保留最近 10 个版本（只读）',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 12, color: p.muted),
                    ),
                  ],
                ),
              ),
              IconButton(
                icon: AppIcon(AppIcons.close, size: 18, color: p.muted),
                tooltip: '关闭',
                onPressed: () => Navigator.of(context).pop(),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Flexible(
            child: future == null
                ? Center(child: Text('请先登录后查看版本历史', style: TextStyle(color: p.muted)))
                : FutureBuilder<ComponentVersionsResponse>(
                    future: future,
                    builder: (context, snap) {
                      if (snap.connectionState == ConnectionState.waiting) {
                        return const Padding(
                          padding: EdgeInsets.symmetric(vertical: 36),
                          child: Center(child: CircularProgressIndicator(strokeWidth: 2.2)),
                        );
                      }
                      if (snap.hasError) {
                        return Padding(
                          padding: const EdgeInsets.symmetric(vertical: 28),
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Text('加载版本历史失败', style: TextStyle(color: p.text, fontWeight: FontWeight.w600)),
                              const SizedBox(height: 8),
                              TextButton(
                                onPressed: () => setState(_load),
                                child: const Text('重试'),
                              ),
                            ],
                          ),
                        );
                      }
                      final resp = snap.data!;
                      final versions = resp.versions;
                      if (versions.isEmpty) {
                        return Padding(
                          padding: const EdgeInsets.symmetric(vertical: 32),
                          child: Center(child: Text('暂无历史版本记录', style: TextStyle(color: p.muted))),
                        );
                      }
                      return ListView.separated(
                        shrinkWrap: true,
                        itemCount: versions.length,
                        separatorBuilder: (_, _) => const SizedBox(height: 10),
                        itemBuilder: (context, i) {
                          final v = versions[i];
                          final isCurrent = v.version == resp.entity.currentVersion;
                          final expanded = _expandedVersion == v.version;
                          final sourceDetail = v.sourceTaskTitle.isNotEmpty
                              ? '定时任务：${v.sourceTaskTitle}'
                              : (v.sourceConversationTitle.isNotEmpty ? '对话：${v.sourceConversationTitle}' : '');

                          return Container(
                            decoration: BoxDecoration(
                              color: p.surfaceAlt.withValues(alpha: 0.55),
                              borderRadius: BorderRadius.circular(14),
                              border: Border.all(
                                color: isCurrent ? p.accent.withValues(alpha: 0.35) : p.line.withValues(alpha: 0.7),
                              ),
                            ),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: [
                                InkWell(
                                  borderRadius: BorderRadius.circular(14),
                                  onTap: () => setState(() => _expandedVersion = expanded ? null : v.version),
                                  child: Padding(
                                    padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Row(
                                          children: [
                                            Container(
                                              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                              decoration: BoxDecoration(
                                                color: isCurrent ? p.accent : p.surface,
                                                borderRadius: BorderRadius.circular(7),
                                                border: isCurrent ? null : Border.all(color: p.line),
                                              ),
                                              child: Text(
                                                isCurrent ? 'v${v.version} · 最新' : 'v${v.version}',
                                                style: TextStyle(
                                                  fontSize: 11.5,
                                                  fontWeight: FontWeight.w700,
                                                  color: isCurrent ? Colors.white : p.text,
                                                ),
                                              ),
                                            ),
                                            const SizedBox(width: 8),
                                            Container(
                                              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                              decoration: BoxDecoration(
                                                color: p.accentSoft,
                                                borderRadius: BorderRadius.circular(7),
                                              ),
                                              child: Text(
                                                v.sourceLabel,
                                                style: TextStyle(
                                                  fontSize: 11,
                                                  fontWeight: FontWeight.w600,
                                                  color: p.accent,
                                                ),
                                              ),
                                            ),
                                            const Spacer(),
                                            Text(
                                              _formatTime(v.createdAt),
                                              style: TextStyle(fontSize: 11.5, color: p.faint),
                                            ),
                                            const SizedBox(width: 4),
                                            AppIcon(
                                              expanded ? AppIcons.chevronUp : AppIcons.chevronDown,
                                              size: 14,
                                              color: p.muted,
                                            ),
                                          ],
                                        ),
                                        if (v.changeSummary.isNotEmpty) ...[
                                          const SizedBox(height: 6),
                                          Text(
                                            v.changeSummary,
                                            style: TextStyle(fontSize: 13.5, color: p.text, height: 1.35),
                                          ),
                                        ],
                                        if (sourceDetail.isNotEmpty || v.sourceConversationId.isNotEmpty) ...[
                                          const SizedBox(height: 6),
                                          Row(
                                            children: [
                                              if (sourceDetail.isNotEmpty)
                                                Expanded(
                                                  child: Text(
                                                    sourceDetail,
                                                    maxLines: 1,
                                                    overflow: TextOverflow.ellipsis,
                                                    style: TextStyle(fontSize: 12, color: p.muted),
                                                  ),
                                                ),
                                              if (v.sourceConversationId.isNotEmpty)
                                                Pressable(
                                                  scale: 0.96,
                                                  onTap: () => _jumpToConversation(v.sourceConversationId, v.sourceMessageId),
                                                  child: Row(
                                                    mainAxisSize: MainAxisSize.min,
                                                    children: [
                                                      Text(
                                                        '跳转来源',
                                                        style: TextStyle(
                                                          fontSize: 12,
                                                          fontWeight: FontWeight.w600,
                                                          color: p.accent,
                                                        ),
                                                      ),
                                                      const SizedBox(width: 2),
                                                      AppIcon(AppIcons.arrowUpRight, size: 12.5, color: p.accent),
                                                    ],
                                                  ),
                                                ),
                                            ],
                                          ),
                                        ],
                                      ],
                                    ),
                                  ),
                                ),
                                if (expanded)
                                  Padding(
                                    padding: const EdgeInsets.fromLTRB(10, 0, 10, 10),
                                    child: IgnorePointer(
                                      child: Opacity(
                                        opacity: 0.95,
                                        child: ChatComponentCard(
                                          messageId: v.sourceMessageId,
                                          component: MessageComponent(
                                            id: 'ver-${v.version}',
                                            entityId: resp.entity.id,
                                            kind: resp.entity.kind,
                                            version: v.version,
                                            latestVersion: resp.entity.currentVersion,
                                            data: v.data,
                                            state: v.state,
                                          ),
                                          isLastMessage: false,
                                        ),
                                      ),
                                    ),
                                  ),
                              ],
                            ),
                          );
                        },
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}

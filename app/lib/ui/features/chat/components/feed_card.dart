import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../../core/models/models.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

/// Formats a component's unix-ms `updated_at` into a compact Chinese label.
String formatComponentUpdatedAt(dynamic rawMs) {
  final ms = rawMs is num ? rawMs.toInt() : 0;
  if (ms <= 0) return '';
  final dt = DateTime.fromMillisecondsSinceEpoch(ms);
  final now = DateTime.now();
  final diff = now.difference(dt);
  if (diff.inMinutes < 1) return '刚刚更新';
  if (diff.inMinutes < 60 && diff.inMinutes >= 0) return '${diff.inMinutes} 分钟前更新';
  final hhmm = '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
  if (dt.year == now.year && dt.month == now.month && dt.day == now.day) {
    return '今天 $hhmm 更新';
  }
  final yesterday = now.subtract(const Duration(days: 1));
  if (dt.year == yesterday.year && dt.month == yesterday.month && dt.day == yesterday.day) {
    return '昨天 $hhmm 更新';
  }
  return '${dt.month}月${dt.day}日 $hhmm 更新';
}

/// Interactive dynamic feed / news briefing / ordered list card.
///
/// Highlights the top 3 items with accent rank pills, supports tapping an item
/// to expand its summary or open its external URL, and displays last-updated time.
class FeedCard extends StatefulWidget {
  const FeedCard({super.key, required this.component, this.compact = false});
  final MessageComponent component;

  /// When true (e.g. on the home pinned board), collapses items beyond the top 3 behind a toggle.
  final bool compact;

  @override
  State<FeedCard> createState() => _FeedCardState();
}

class _FeedCardState extends State<FeedCard> {
  final Set<String> _expanded = {};
  bool _showAll = false;

  Future<void> _openUrl(String rawUrl) async {
    final uri = Uri.tryParse(rawUrl);
    if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) return;
    await launchUrl(uri, mode: LaunchMode.externalApplication);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final d = widget.component.data;
    final title = '${d['title'] ?? '动态资讯'}'.trim();
    final subtitle = '${d['subtitle'] ?? ''}'.trim();
    final updatedLabel = formatComponentUpdatedAt(d['updated_at']);
    final rawItems = d['items'] is List ? (d['items'] as List) : const [];

    final items = <({String id, String title, String summary, String tag, String source, String url, String pub})>[];
    for (var i = 0; i < rawItems.length; i++) {
      final m = rawItems[i];
      if (m is! Map) continue;
      final itemTitle = '${m['title'] ?? ''}'.trim();
      if (itemTitle.isEmpty) continue;
      items.add((
        id: '${m['id'] ?? 'f${i + 1}'}',
        title: itemTitle,
        summary: '${m['summary'] ?? ''}'.trim(),
        tag: '${m['tag'] ?? ''}'.trim(),
        source: '${m['source'] ?? ''}'.trim(),
        url: '${m['url'] ?? ''}'.trim(),
        pub: '${m['published_at'] ?? ''}'.trim(),
      ));
    }
    if (items.isEmpty) return const SizedBox.shrink();

    final collapsible = widget.compact && items.length > 3;
    final visibleItems = collapsible && !_showAll ? items.take(3).toList() : items;
    final hiddenCount = items.length - visibleItems.length;

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: p.line),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 12, offset: const Offset(0, 4))],
      ),
      padding: const EdgeInsets.fromLTRB(16, 15, 16, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 34,
                height: 34,
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(11)),
                child: Center(child: AppIcon(AppIcons.article, size: 19, color: p.accent)),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title.isEmpty ? '动态资讯' : title, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: p.text)),
                    if (subtitle.isNotEmpty)
                      Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.muted)),
                  ],
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(12)),
                child: Text(
                  updatedLabel.isNotEmpty ? updatedLabel : '共 ${items.length} 条',
                  style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: p.muted),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          for (var i = 0; i < visibleItems.length; i++) ...[
            if (i > 0) Divider(height: 1, color: p.line.withValues(alpha: 0.6)),
            _FeedRow(
              rank: i + 1,
              title: visibleItems[i].title,
              summary: visibleItems[i].summary,
              tag: visibleItems[i].tag,
              source: visibleItems[i].source,
              url: visibleItems[i].url,
              pub: visibleItems[i].pub,
              expanded: _expanded.contains(visibleItems[i].id),
              onToggleExpand: visibleItems[i].summary.isEmpty
                  ? null
                  : () => setState(() {
                        if (!_expanded.remove(visibleItems[i].id)) _expanded.add(visibleItems[i].id);
                      }),
              onOpenUrl: visibleItems[i].url.isEmpty ? null : () => _openUrl(visibleItems[i].url),
            ),
          ],
          if (collapsible) ...[
            Divider(height: 1, color: p.line.withValues(alpha: 0.6)),
            Pressable(
              scale: 0.99,
              onTap: () => setState(() => _showAll = !_showAll),
              child: Padding(
                padding: const EdgeInsets.only(top: 9, bottom: 2),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text(
                      _showAll ? '收起往期内容' : '展开其余 $hiddenCount 条往期内容',
                      style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.muted),
                    ),
                    const SizedBox(width: 4),
                    AppIcon(_showAll ? AppIcons.chevronUp : AppIcons.chevronDown, size: 15, color: p.muted),
                  ],
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _FeedRow extends StatelessWidget {
  const _FeedRow({
    required this.rank,
    required this.title,
    required this.summary,
    required this.tag,
    required this.source,
    required this.url,
    required this.pub,
    required this.expanded,
    this.onToggleExpand,
    this.onOpenUrl,
  });

  final int rank;
  final String title, summary, tag, source, url, pub;
  final bool expanded;
  final VoidCallback? onToggleExpand;
  final VoidCallback? onOpenUrl;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isTop = rank <= 3;
    final hasMeta = source.isNotEmpty || pub.isNotEmpty || url.isNotEmpty;

    return Pressable(
      scale: 0.992,
      onTap: onToggleExpand ?? onOpenUrl,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              width: 22,
              height: 22,
              margin: const EdgeInsets.only(top: 1),
              decoration: BoxDecoration(
                color: isTop ? p.accentSoft : p.surfaceAlt,
                borderRadius: BorderRadius.circular(7),
              ),
              alignment: Alignment.center,
              child: Text(
                '$rank',
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                  color: isTop ? p.accent : p.muted,
                ),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Text(
                          title,
                          style: TextStyle(
                            fontSize: 14.8,
                            fontWeight: isTop ? FontWeight.w600 : FontWeight.w500,
                            height: 1.38,
                            color: p.text,
                          ),
                        ),
                      ),
                      if (tag.isNotEmpty) ...[
                        const SizedBox(width: 6),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                          decoration: BoxDecoration(
                            color: isTop ? p.accentSoft : p.surfaceAlt,
                            borderRadius: BorderRadius.circular(8),
                          ),
                          child: Text(
                            tag,
                            style: TextStyle(
                              fontSize: 11,
                              fontWeight: FontWeight.w600,
                              color: isTop ? p.accent : p.muted,
                            ),
                          ),
                        ),
                      ],
                    ],
                  ),
                  if (summary.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      summary,
                      maxLines: expanded ? 12 : 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 13, height: 1.42, color: p.muted),
                    ),
                  ],
                  if (hasMeta) ...[
                    const SizedBox(height: 5),
                    Row(
                      children: [
                        if (source.isNotEmpty)
                          Flexible(
                            child: Text(
                              source,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(fontSize: 11.5, color: p.faint),
                            ),
                          ),
                        if (source.isNotEmpty && pub.isNotEmpty)
                          Text(' · ', style: TextStyle(fontSize: 11.5, color: p.faint)),
                        if (pub.isNotEmpty)
                          Text(pub, style: TextStyle(fontSize: 11.5, color: p.faint)),
                        const Spacer(),
                        if (onOpenUrl != null)
                          Tooltip(
                            message: '打开原链接',
                            child: Pressable(
                              scale: 0.92,
                              onTap: onOpenUrl,
                              child: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  AppIcon(AppIcons.externalLink, size: 13, color: p.accent),
                                  const SizedBox(width: 3),
                                  Text('原文', style: TextStyle(fontSize: 11.5, color: p.accent, fontWeight: FontWeight.w600)),
                                ],
                              ),
                            ),
                          ),
                      ],
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

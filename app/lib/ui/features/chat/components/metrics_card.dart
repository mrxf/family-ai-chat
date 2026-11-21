import 'package:flutter/material.dart';

import '../../../../core/models/models.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';

class MetricsCard extends StatelessWidget {
  const MetricsCard({super.key, required this.component});
  final MessageComponent component;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final d = component.data;
    final title = '${d['title'] ?? '核心指标'}'.trim();
    final subtitle = '${d['subtitle'] ?? ''}'.trim();
    final summary = '${d['summary'] ?? ''}'.trim();
    final rawMetrics = d['metrics'] is List ? (d['metrics'] as List) : const [];

    final tiles = <({String label, String value, String unit, String delta, String status})>[];
    for (final m in rawMetrics) {
      if (m is! Map) continue;
      tiles.add((
        label: '${m['label'] ?? ''}',
        value: '${m['value'] ?? ''}',
        unit: '${m['unit'] ?? ''}',
        delta: '${m['delta'] ?? ''}',
        status: '${m['status'] ?? 'normal'}',
      ));
    }
    if (tiles.isEmpty) return const SizedBox.shrink();

    Color statusColor(String st) => switch (st) {
          'good' => p.good,
          'warn' => const Color(0xFFD97706),
          'bad' => p.bad,
          _ => p.accent,
        };

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: p.line),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 12, offset: const Offset(0, 4))],
      ),
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 34,
                height: 34,
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(11)),
                child: Center(child: AppIcon(AppIcons.gauge, size: 19, color: p.accent)),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: p.text)),
                    if (subtitle.isNotEmpty)
                      Text(subtitle, style: TextStyle(fontSize: 12.5, color: p.muted)),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 13),
          LayoutBuilder(
            builder: (context, constraints) {
              final twoCols = tiles.length >= 2 && constraints.maxWidth >= 250;
              final itemW = twoCols ? (constraints.maxWidth - 10) / 2 : constraints.maxWidth;
              return Wrap(
                spacing: 10,
                runSpacing: 10,
                children: [
                  for (var i = 0; i < tiles.length; i++)
                    SizedBox(
                      width: (twoCols && tiles.length.isOdd && i == 0) ? constraints.maxWidth : itemW,
                      child: Container(
                        padding: const EdgeInsets.fromLTRB(13, 11, 13, 11),
                        decoration: BoxDecoration(
                          color: p.surfaceAlt.withValues(alpha: 0.72),
                          borderRadius: BorderRadius.circular(15),
                          border: Border.all(
                            color: tiles[i].status == 'normal'
                                ? p.line.withValues(alpha: 0.6)
                                : statusColor(tiles[i].status).withValues(alpha: 0.32),
                          ),
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Container(
                                  width: 7,
                                  height: 7,
                                  decoration: BoxDecoration(
                                    color: statusColor(tiles[i].status),
                                    shape: BoxShape.circle,
                                  ),
                                ),
                                const SizedBox(width: 6),
                                Expanded(
                                  child: Text(
                                    tiles[i].label,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w500),
                                  ),
                                ),
                              ],
                            ),
                            const SizedBox(height: 6),
                            Row(
                              crossAxisAlignment: CrossAxisAlignment.baseline,
                              textBaseline: TextBaseline.alphabetic,
                              children: [
                                Flexible(
                                  child: Text(
                                    tiles[i].value,
                                    style: TextStyle(
                                      fontSize: 22,
                                      fontWeight: FontWeight.w700,
                                      color: p.text,
                                      height: 1.15,
                                    ),
                                  ),
                                ),
                                if (tiles[i].unit.isNotEmpty) ...[
                                  const SizedBox(width: 4),
                                  Text(
                                    tiles[i].unit,
                                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: p.muted),
                                  ),
                                ],
                              ],
                            ),
                            if (tiles[i].delta.isNotEmpty) ...[
                              const SizedBox(height: 4),
                              Text(
                                tiles[i].delta,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                  fontSize: 12,
                                  color: tiles[i].status == 'normal' ? p.faint : statusColor(tiles[i].status),
                                  fontWeight: tiles[i].status == 'normal' ? FontWeight.w400 : FontWeight.w500,
                                ),
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                ],
              );
            },
          ),
          if (summary.isNotEmpty) ...[
            const SizedBox(height: 12),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              decoration: BoxDecoration(
                color: p.accentSoft.withValues(alpha: 0.55),
                borderRadius: BorderRadius.circular(12),
              ),
              child: Text(summary, style: TextStyle(fontSize: 13.5, height: 1.4, color: p.text)),
            ),
          ],
        ],
      ),
    );
  }
}

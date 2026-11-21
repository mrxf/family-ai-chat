import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback;

import '../../../../core/models/models.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';

class ChartCard extends StatefulWidget {
  const ChartCard({super.key, required this.component});
  final MessageComponent component;

  static String fmtNum(double v, [String unit = '']) {
    final s = v == v.roundToDouble()
        ? '${v.round()}'
        : (v.abs() >= 100 ? v.toStringAsFixed(1) : v.toStringAsFixed(2).replaceFirst(RegExp(r'\.?0+$'), ''));
    return '$s$unit';
  }

  @override
  State<ChartCard> createState() => _ChartCardState();
}

class _ChartCardState extends State<ChartCard> {
  int? _selectedIdx;

  static const _seriesColorsLight = [
    Color(0xFFFF7A45),
    Color(0xFF3B82F6),
    Color(0xFF10B981),
    Color(0xFFF59E0B),
    Color(0xFF8B5CF6),
    Color(0xFFEC4899),
  ];

  static const _seriesColorsDark = [
    Color(0xFFFF8F5E),
    Color(0xFF60A5FA),
    Color(0xFF34D399),
    Color(0xFFFBBF24),
    Color(0xFFA78BFA),
    Color(0xFFF472B6),
  ];

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final dark = Theme.of(context).brightness == Brightness.dark;
    final palette = dark ? _seriesColorsDark : _seriesColorsLight;
    final d = widget.component.data;

    final chartType = '${d['chart_type'] ?? 'bar'}';
    final title = '${d['title'] ?? '数据图表'}'.trim();
    final subtitle = '${d['subtitle'] ?? ''}'.trim();
    final unit = '${d['unit'] ?? ''}'.trim();
    final labels = [
      if (d['labels'] is List)
        for (final x in d['labels'] as List) '$x',
    ];
    final series = <({String name, List<double> values})>[];
    if (d['series'] is List) {
      for (final s in d['series'] as List) {
        if (s is! Map) continue;
        final vals = [
          if (s['values'] is List)
            for (final v in s['values'] as List) (v as num?)?.toDouble() ?? 0.0,
        ];
        if (vals.isNotEmpty) {
          series.add((name: '${s['name'] ?? ''}', values: vals));
        }
      }
    }
    if (labels.isEmpty || series.isEmpty) return const SizedBox.shrink();

    final headerIcon = switch (chartType) {
      'line' => AppIcons.chartLine,
      'pie' => AppIcons.chartPie,
      _ => AppIcons.chart,
    };

    final sel = (_selectedIdx != null && _selectedIdx! >= 0 && _selectedIdx! < labels.length) ? _selectedIdx : null;

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
                child: Center(child: AppIcon(headerIcon, size: 19, color: p.accent)),
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
              if (unit.isNotEmpty && chartType != 'pie')
                Text('单位：$unit', style: TextStyle(fontSize: 12, color: p.faint)),
            ],
          ),
          if (series.length > 1 && chartType != 'pie') ...[
            const SizedBox(height: 10),
            Wrap(
              spacing: 12,
              runSpacing: 4,
              children: [
                for (var i = 0; i < series.length; i++)
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Container(
                        width: 9,
                        height: 9,
                        decoration: BoxDecoration(color: palette[i % palette.length], shape: BoxShape.circle),
                      ),
                      const SizedBox(width: 5),
                      Text(series[i].name, style: TextStyle(fontSize: 12.5, color: p.muted)),
                    ],
                  ),
              ],
            ),
          ],
          if (sel != null && chartType != 'pie') ...[
            const SizedBox(height: 10),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 6),
              decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(10)),
              child: Wrap(
                spacing: 10,
                children: [
                  Text(labels[sel], style: TextStyle(fontSize: 13, fontWeight: FontWeight.w700, color: p.text)),
                  for (var s = 0; s < series.length; s++)
                    if (sel < series[s].values.length)
                      Text(
                        '${series.length > 1 ? '${series[s].name}: ' : ''}${ChartCard.fmtNum(series[s].values[sel], unit)}',
                        style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: palette[s % palette.length]),
                      ),
                ],
              ),
            ),
          ],
          const SizedBox(height: 14),
          if (chartType == 'pie')
            _PieBody(
              labels: labels,
              values: series.first.values,
              unit: unit,
              palette: palette,
              selectedIdx: sel,
              onSelect: (i) {
                HapticFeedback.selectionClick();
                setState(() => _selectedIdx = _selectedIdx == i ? null : i);
              },
            )
          else
            LayoutBuilder(
              builder: (context, constraints) {
                return GestureDetector(
                  behavior: HitTestBehavior.opaque,
                  onTapDown: (details) {
                    final chartLeft = 38.0;
                    final w = math.max(1.0, constraints.maxWidth - chartLeft - 8);
                    final dx = (details.localPosition.dx - chartLeft).clamp(0.0, w);
                    final idx = ((dx / w) * labels.length).floor().clamp(0, labels.length - 1);
                    HapticFeedback.selectionClick();
                    setState(() => _selectedIdx = _selectedIdx == idx ? null : idx);
                  },
                  child: SizedBox(
                    height: 186,
                    width: double.infinity,
                    child: CustomPaint(
                      painter: _CartesianChartPainter(
                        isLine: chartType == 'line',
                        labels: labels,
                        series: series,
                        palette: palette,
                        selectedIdx: sel,
                        gridColor: p.line,
                        labelColor: p.muted,
                        highlightBg: p.accentSoft.withValues(alpha: 0.55),
                        fontFamily: DefaultTextStyle.of(context).style.fontFamily,
                      ),
                    ),
                  ),
                );
              },
            ),
        ],
      ),
    );
  }
}

class _CartesianChartPainter extends CustomPainter {
  _CartesianChartPainter({
    required this.isLine,
    required this.labels,
    required this.series,
    required this.palette,
    required this.selectedIdx,
    required this.gridColor,
    required this.labelColor,
    required this.highlightBg,
    this.fontFamily,
  });

  final bool isLine;
  final List<String> labels;
  final List<({String name, List<double> values})> series;
  final List<Color> palette;
  final int? selectedIdx;
  final Color gridColor, labelColor, highlightBg;
  final String? fontFamily;

  @override
  void paint(Canvas canvas, Size size) {
    const leftPad = 38.0;
    const rightPad = 8.0;
    const topPad = 12.0;
    const bottomPad = 28.0;
    final plotW = math.max(1.0, size.width - leftPad - rightPad);
    final plotH = math.max(1.0, size.height - topPad - bottomPad);

    var minV = double.infinity;
    var maxV = double.negativeInfinity;
    for (final s in series) {
      for (final v in s.values) {
        if (v < minV) minV = v;
        if (v > maxV) maxV = v;
      }
    }
    if (!minV.isFinite || !maxV.isFinite) return;
    if (!isLine && minV > 0) minV = 0;
    if ((maxV - minV).abs() < 1e-6) {
      maxV += 1;
      if (isLine || minV < 0) minV -= 1;
    } else if (isLine) {
      final pad = (maxV - minV) * 0.12;
      maxV += pad;
      minV -= pad;
      if (maxV - minV >= 4) {
        minV = (minV / 2).floor() * 2.0;
        maxV = (maxV / 2).ceil() * 2.0;
      }
    } else {
      maxV += (maxV - minV) * 0.1;
      if (maxV >= 10) {
        maxV = (maxV / 10).ceil() * 10.0;
      }
    }

    double yOf(double v) => topPad + plotH * (1 - (v - minV) / (maxV - minV));

    // Grid lines & Y ticks
    final gridPaint = Paint()
      ..color = gridColor
      ..strokeWidth = 1;
    for (var i = 0; i <= 2; i++) {
      final frac = i / 2.0;
      final y = topPad + plotH * frac;
      canvas.drawLine(Offset(leftPad, y), Offset(size.width - rightPad, y), gridPaint);
      final val = maxV - (maxV - minV) * frac;
      final tp = TextPainter(
        text: TextSpan(
          text: ChartCard.fmtNum(val),
          style: TextStyle(fontSize: 10.5, color: labelColor, fontFamily: fontFamily),
        ),
        textDirection: TextDirection.ltr,
      )..layout(maxWidth: leftPad - 4);
      tp.paint(canvas, Offset(leftPad - tp.width - 5, y - tp.height / 2));
    }

    final n = labels.length;
    final colW = plotW / n;

    if (selectedIdx != null) {
      final x = leftPad + selectedIdx! * colW;
      canvas.drawRRect(
        RRect.fromRectAndRadius(Rect.fromLTWH(x + 2, topPad, colW - 4, plotH), const Radius.circular(8)),
        Paint()..color = highlightBg,
      );
    }

    // X-axis labels (thin out automatically when crowded)
    final step = n <= 7 ? 1 : (n <= 12 ? 2 : 3);
    for (var i = 0; i < n; i += step) {
      final cx = leftPad + (i + 0.5) * colW;
      final tp = TextPainter(
        text: TextSpan(
          text: labels[i],
          style: TextStyle(
            fontSize: 11,
            fontWeight: selectedIdx == i ? FontWeight.w700 : FontWeight.w400,
            color: labelColor,
            fontFamily: fontFamily,
          ),
        ),
        maxLines: 1,
        ellipsis: '…',
        textDirection: TextDirection.ltr,
      )..layout(maxWidth: math.max(colW * step - 2, 24));
      tp.paint(canvas, Offset((cx - tp.width / 2).clamp(leftPad, size.width - tp.width), topPad + plotH + 7));
    }

    if (isLine) {
      for (var sIdx = 0; sIdx < series.length; sIdx++) {
        final vals = series[sIdx].values;
        final color = palette[sIdx % palette.length];
        final pts = <Offset>[
          for (var i = 0; i < math.min(n, vals.length); i++)
            Offset(leftPad + (i + 0.5) * colW, yOf(vals[i])),
        ];
        if (pts.isEmpty) continue;
        final path = Path()..moveTo(pts.first.dx, pts.first.dy);
        for (var i = 0; i < pts.length - 1; i++) {
          final midX = (pts[i].dx + pts[i + 1].dx) / 2;
          path.cubicTo(midX, pts[i].dy, midX, pts[i + 1].dy, pts[i + 1].dx, pts[i + 1].dy);
        }
        if (sIdx == 0 && pts.length >= 2) {
          final fillPath = Path.from(path)
            ..lineTo(pts.last.dx, topPad + plotH)
            ..lineTo(pts.first.dx, topPad + plotH)
            ..close();
          canvas.drawPath(
            fillPath,
            Paint()
              ..shader = LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                colors: [color.withValues(alpha: 0.25), color.withValues(alpha: 0.0)],
              ).createShader(Rect.fromLTWH(leftPad, topPad, plotW, plotH)),
          );
        }
        canvas.drawPath(
          path,
          Paint()
            ..color = color
            ..style = PaintingStyle.stroke
            ..strokeWidth = 2.6
            ..strokeCap = StrokeCap.round,
        );
        for (var i = 0; i < pts.length; i++) {
          final active = selectedIdx == i;
          canvas.drawCircle(pts[i], active ? 5.5 : 3.5, Paint()..color = color);
          canvas.drawCircle(pts[i], active ? 2.5 : 1.5, Paint()..color = Colors.white);
        }
      }
    } else {
      final zeroY = yOf(0.0.clamp(minV, maxV));
      final groupW = colW * 0.68;
      final barW = (groupW / series.length).clamp(4.0, 28.0);
      for (var i = 0; i < n; i++) {
        final groupStart = leftPad + (i + 0.5) * colW - (barW * series.length) / 2;
        for (var sIdx = 0; sIdx < series.length; sIdx++) {
          if (i >= series[sIdx].values.length) continue;
          final v = series[sIdx].values[i];
          final y = yOf(v);
          final top = math.min(y, zeroY);
          final bottom = math.max(y, zeroY);
          final h = math.max(3.0, bottom - top);
          final color = palette[sIdx % palette.length];
          final rect = RRect.fromRectAndCorners(
            Rect.fromLTWH(groupStart + sIdx * barW + 1, top, math.max(2, barW - 2), h),
            topLeft: const Radius.circular(6),
            topRight: const Radius.circular(6),
          );
          canvas.drawRRect(
            rect,
            Paint()..color = (selectedIdx == null || selectedIdx == i) ? color : color.withValues(alpha: 0.45),
          );
        }
      }
    }
  }

  @override
  bool shouldRepaint(covariant _CartesianChartPainter old) =>
      old.selectedIdx != selectedIdx || old.isLine != isLine || old.labels != labels || old.series != series;
}

class _PieBody extends StatelessWidget {
  const _PieBody({
    required this.labels,
    required this.values,
    required this.unit,
    required this.palette,
    required this.selectedIdx,
    required this.onSelect,
  });

  final List<String> labels;
  final List<double> values;
  final String unit;
  final List<Color> palette;
  final int? selectedIdx;
  final ValueChanged<int> onSelect;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final cleanVals = [for (final v in values) math.max(0.0, v)];
    final total = cleanVals.fold<double>(0, (a, b) => a + b);
    if (total <= 0) return const SizedBox.shrink();

    final sel = (selectedIdx != null && selectedIdx! < cleanVals.length) ? selectedIdx : null;
    final centerTitle = sel != null ? labels[sel] : '合计';
    final centerVal = sel != null ? ChartCard.fmtNum(cleanVals[sel], unit) : ChartCard.fmtNum(total, unit);
    final centerPct = sel != null ? '${((cleanVals[sel] / total) * 100).toStringAsFixed(1)}%' : '';

    return Row(
      children: [
        SizedBox(
          width: 136,
          height: 136,
          child: Stack(
            alignment: Alignment.center,
            children: [
              CustomPaint(
                size: const Size(136, 136),
                painter: _DonutPainter(
                  values: cleanVals,
                  total: total,
                  palette: palette,
                  selectedIdx: sel,
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 18),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      centerTitle,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 11.5, color: p.muted),
                    ),
                    Text(
                      centerVal,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w700, color: p.text),
                    ),
                    if (centerPct.isNotEmpty)
                      Text(centerPct, style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: p.accent)),
                  ],
                ),
              ),
            ],
          ),
        ),
        const SizedBox(width: 14),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (var i = 0; i < math.min(labels.length, cleanVals.length); i++) ...[
                if (i > 0) const SizedBox(height: 6),
                Pressable(
                  onTap: () => onSelect(i),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
                    decoration: BoxDecoration(
                      color: sel == i ? p.surfaceAlt : Colors.transparent,
                      borderRadius: BorderRadius.circular(9),
                    ),
                    child: Row(
                      children: [
                        Container(
                          width: 10,
                          height: 10,
                          decoration: BoxDecoration(
                            color: palette[i % palette.length],
                            borderRadius: BorderRadius.circular(3),
                          ),
                        ),
                        const SizedBox(width: 7),
                        Expanded(
                          child: Text(
                            labels[i],
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: 13,
                              fontWeight: sel == i ? FontWeight.w600 : FontWeight.w400,
                              color: p.text,
                            ),
                          ),
                        ),
                        Text(
                          '${((cleanVals[i] / total) * 100).round()}%',
                          style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.muted),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

class _DonutPainter extends CustomPainter {
  _DonutPainter({
    required this.values,
    required this.total,
    required this.palette,
    required this.selectedIdx,
  });

  final List<double> values;
  final double total;
  final List<Color> palette;
  final int? selectedIdx;

  @override
  void paint(Canvas canvas, Size size) {
    final center = size.center(Offset.zero);
    final radius = size.shortestSide / 2 - 12;
    var start = -math.pi / 2;
    const gap = 0.03;

    for (var i = 0; i < values.length; i++) {
      final sweep = (values[i] / total) * math.pi * 2;
      if (sweep <= 0) continue;
      final active = selectedIdx == i;
      final paint = Paint()
        ..color = (selectedIdx == null || active)
            ? palette[i % palette.length]
            : palette[i % palette.length].withValues(alpha: 0.38)
        ..style = PaintingStyle.stroke
        ..strokeWidth = active ? 20 : 16;
      canvas.drawArc(
        Rect.fromCircle(center: center, radius: active ? radius + 2 : radius),
        start + (values.length > 1 ? gap / 2 : 0),
        math.max(0.02, sweep - (values.length > 1 ? gap : 0)),
        false,
        paint,
      );
      start += sweep;
    }
  }

  @override
  bool shouldRepaint(covariant _DonutPainter old) =>
      old.selectedIdx != selectedIdx || old.values != values || old.total != total;
}

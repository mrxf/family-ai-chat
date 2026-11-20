import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../state/chat.dart';
import 'icons.dart';
import 'markdown.dart';
import 'theme.dart';
import 'widgets.dart';

// ======================= Inline Interleaving Host =======================

/// Splits assistant [content] by Unicode rune offsets ([MessageComponent.pos])
/// so rich interactive cards appear at the exact position the model invoked them.
class InterleavedAssistantBody extends StatelessWidget {
  const InterleavedAssistantBody({
    super.key,
    required this.messageId,
    required this.content,
    required this.components,
    required this.streaming,
    required this.animate,
    required this.isLastMessage,
  });

  final String messageId;
  final String content;
  final List<MessageComponent> components;
  final bool streaming;
  final bool animate;
  final bool isLastMessage;

  @override
  Widget build(BuildContext context) {
    if (components.isEmpty) {
      if (content.isEmpty) return const SizedBox.shrink();
      return Md(content, streaming: streaming, animate: animate);
    }

    final runes = content.runes.toList(growable: false);
    final sorted = [...components]..sort((a, b) => a.pos.compareTo(b.pos));

    final children = <Widget>[];
    var cursor = 0;

    void addTextSegment(String raw, {required bool isTail}) {
      final trimmed = raw.trim();
      if (trimmed.isEmpty) return;
      if (children.isNotEmpty) children.add(const SizedBox(height: 10));
      children.add(Md(trimmed, streaming: streaming && isTail, animate: animate));
    }

    for (final comp in sorted) {
      final pos = comp.pos.clamp(0, runes.length);
      if (pos > cursor) {
        addTextSegment(String.fromCharCodes(runes.sublist(cursor, pos)), isTail: false);
        cursor = pos;
      }
      if (children.isNotEmpty) children.add(const SizedBox(height: 10));
      children.add(
        _ComponentEntrance(
          key: ValueKey('comp-${comp.id}'),
          animate: animate,
          child: ChatComponentCard(
            messageId: messageId,
            component: comp,
            isLastMessage: isLastMessage,
          ),
        ),
      );
    }

    if (cursor < runes.length) {
      addTextSegment(String.fromCharCodes(runes.sublist(cursor)), isTail: true);
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: children,
    );
  }
}

class _ComponentEntrance extends StatefulWidget {
  const _ComponentEntrance({super.key, required this.animate, required this.child});
  final bool animate;
  final Widget child;

  @override
  State<_ComponentEntrance> createState() => _ComponentEntranceState();
}

class _ComponentEntranceState extends State<_ComponentEntrance> {
  late bool _visible = !widget.animate;

  @override
  void initState() {
    super.initState();
    if (!_visible) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() => _visible = true);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.animate) return widget.child;
    return AnimatedOpacity(
      opacity: _visible ? 1 : 0,
      duration: const Duration(milliseconds: 320),
      curve: Curves.easeOutCubic,
      child: AnimatedSlide(
        offset: _visible ? Offset.zero : const Offset(0, 0.03),
        duration: const Duration(milliseconds: 320),
        curve: Curves.easeOutCubic,
        child: widget.child,
      ),
    );
  }
}

/// Dispatcher for all supported assistant component kinds.
class ChatComponentCard extends StatelessWidget {
  const ChatComponentCard({
    super.key,
    required this.messageId,
    required this.component,
    required this.isLastMessage,
  });

  final String messageId;
  final MessageComponent component;
  final bool isLastMessage;

  @override
  Widget build(BuildContext context) {
    return switch (component.kind) {
      'weather' => WeatherCard(component: component),
      'todo' => TodoCard(messageId: messageId, component: component),
      'chart' => ChartCard(component: component),
      'quiz' => QuizCard(messageId: messageId, component: component),
      'metrics' => MetricsCard(component: component),
      'ask_user' => AskUserCard(
          messageId: messageId,
          component: component,
          isLastMessage: isLastMessage,
        ),
      _ => const SizedBox.shrink(),
    };
  }
}

// ======================= 1. Weather Card =======================

class WeatherCard extends StatelessWidget {
  const WeatherCard({super.key, required this.component});
  final MessageComponent component;

  static ({List<Color> gradient, IconData icon, Color accent}) _skyStyle(String cond, bool dark) {
    final c = cond.trim();
    if (c.contains('雷')) {
      return (
        gradient: dark
            ? const [Color(0xFF2B2244), Color(0xFF1A1B2E)]
            : const [Color(0xFF53488D), Color(0xFF343B68)],
        icon: AppIcons.cloudLightning,
        accent: const Color(0xFFFFD166),
      );
    }
    if (c.contains('雪')) {
      return (
        gradient: dark
            ? const [Color(0xFF243447), Color(0xFF19222D)]
            : const [Color(0xFF658EB8), Color(0xFF476C96)],
        icon: AppIcons.cloudSnow,
        accent: const Color(0xFFE0F2FE),
      );
    }
    if (c.contains('雨')) {
      return (
        gradient: dark
            ? const [Color(0xFF1E3246), Color(0xFF152230)]
            : const [Color(0xFF3E6B89), Color(0xFF2B4C65)],
        icon: AppIcons.cloudRain,
        accent: const Color(0xFF90E0EF),
      );
    }
    if (c.contains('阴') || c.contains('雾') || c.contains('霾') || c.contains('沙')) {
      return (
        gradient: dark
            ? const [Color(0xFF2D333B), Color(0xFF1E2228)]
            : const [Color(0xFF607385), Color(0xFF475766)],
        icon: AppIcons.cloud,
        accent: const Color(0xFFE2E8F0),
      );
    }
    if (c.contains('云')) {
      return (
        gradient: dark
            ? const [Color(0xFF27384C), Color(0xFF1D2836)]
            : const [Color(0xFF4A89DC), Color(0xFF356BB3)],
        icon: AppIcons.cloudSun,
        accent: const Color(0xFFFFE082),
      );
    }
    // Sunny / Clear
    return (
      gradient: dark
          ? const [Color(0xFF3B2B1E), Color(0xFF241B15)]
          : const [Color(0xFFF5853F), Color(0xFFE05D34)],
      icon: AppIcons.sun,
      accent: const Color(0xFFFFF3B0),
    );
  }

  static String _fmtDeg(num? v) {
    if (v == null) return '--';
    final d = v.toDouble();
    return d == d.roundToDouble() ? '${d.round()}°' : '${d.toStringAsFixed(1)}°';
  }

  @override
  Widget build(BuildContext context) {
    final d = component.data;
    final dark = Theme.of(context).brightness == Brightness.dark;
    final location = '${d['location'] ?? ''}'.trim();
    final dateLabel = '${d['date_label'] ?? ''}'.trim();
    final condition = '${d['condition'] ?? '晴'}'.trim();
    final temp = d['temp'] as num?;
    final high = d['high'] as num?;
    final low = d['low'] as num?;
    final feelsLike = d['feels_like'] as num?;
    final humidity = '${d['humidity'] ?? ''}'.trim();
    final wind = '${d['wind'] ?? ''}'.trim();
    final aqi = '${d['aqi'] ?? ''}'.trim();
    final tip = '${d['tip'] ?? ''}'.trim();
    final forecastRaw = d['forecast'] is List ? (d['forecast'] as List) : const [];

    final sky = _skyStyle(condition, dark);
    final metaPills = <(IconData, String)>[
      if (feelsLike != null) (AppIcons.thermometer, '体感 ${_fmtDeg(feelsLike)}'),
      if (humidity.isNotEmpty) (AppIcons.drop, '湿度 $humidity'),
      if (wind.isNotEmpty) (AppIcons.wind, wind),
      if (aqi.isNotEmpty) (AppIcons.sparkles, '空气 $aqi'),
    ];

    final fallbackBase = (low ?? high ?? temp ?? 0).toDouble();
    var weekMin = (low ?? high ?? temp ?? 0).toDouble();
    var weekMax = (high ?? low ?? temp ?? 0).toDouble();
    final forecast = <({String day, String cond, double low, double high})>[];
    for (final e in forecastRaw) {
      if (e is! Map) continue;
      final fl = ((e['low'] as num?) ?? fallbackBase).toDouble();
      final fh = ((e['high'] as num?) ?? fallbackBase).toDouble();
      if (fl < weekMin) weekMin = fl;
      if (fh > weekMax) weekMax = fh;
      forecast.add((
        day: '${e['day'] ?? ''}',
        cond: '${e['condition'] ?? ''}',
        low: fl,
        high: fh,
      ));
    }

    final explicitCompact = d['compact'];
    final isCompact = explicitCompact is bool
        ? (explicitCompact && forecast.isEmpty)
        : (forecast.isEmpty && metaPills.length <= 1);

    if (isCompact) {
      final String mainTemp;
      String subRange = '';
      if (temp != null) {
        mainTemp = _fmtDeg(temp);
        if (low != null && high != null) {
          subRange = '${_fmtDeg(low)} ~ ${_fmtDeg(high)}';
        }
      } else if (low != null && high != null) {
        mainTemp = low == high ? _fmtDeg(high) : '${_fmtDeg(low)}~${_fmtDeg(high)}';
      } else if (high != null || low != null) {
        mainTemp = _fmtDeg(high ?? low);
      } else {
        mainTemp = '';
      }
      final subMeta = [
        condition,
        if (wind.isNotEmpty) wind else if (humidity.isNotEmpty) '湿度 $humidity',
      ].join('  ·  ');

      return Container(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: sky.gradient,
          ),
          borderRadius: BorderRadius.circular(18),
          boxShadow: [
            BoxShadow(
              color: sky.gradient.last.withValues(alpha: dark ? 0.30 : 0.18),
              blurRadius: 12,
              offset: const Offset(0, 4),
            ),
          ],
        ),
        padding: const EdgeInsets.fromLTRB(14, 12, 15, 12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: Colors.white.withValues(alpha: 0.15),
                    shape: BoxShape.circle,
                  ),
                  child: Center(child: AppIcon(sky.icon, size: 23, color: sky.accent)),
                ),
                const SizedBox(width: 11),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Flexible(
                            child: Text(
                              location.isEmpty ? '天气' : location,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(
                                fontSize: 16,
                                fontWeight: FontWeight.w700,
                                color: Colors.white,
                              ),
                            ),
                          ),
                          if (dateLabel.isNotEmpty) ...[
                            const SizedBox(width: 6),
                            Flexible(
                              child: Container(
                                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 1.5),
                                decoration: BoxDecoration(
                                  color: Colors.white.withValues(alpha: 0.16),
                                  borderRadius: BorderRadius.circular(8),
                                ),
                                child: Text(
                                  dateLabel,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(fontSize: 11.5, color: Colors.white),
                                ),
                              ),
                            ),
                          ],
                        ],
                      ),
                      const SizedBox(height: 3),
                      Text(
                        subMeta,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 13, color: Colors.white.withValues(alpha: 0.86)),
                      ),
                    ],
                  ),
                ),
                if (mainTemp.isNotEmpty) ...[
                  const SizedBox(width: 10),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.end,
                    children: [
                      Text(
                        mainTemp,
                        style: const TextStyle(
                          fontSize: 25,
                          fontWeight: FontWeight.w700,
                          color: Colors.white,
                          height: 1.08,
                        ),
                      ),
                      if (subRange.isNotEmpty)
                        Text(
                          subRange,
                          style: TextStyle(fontSize: 11.5, color: Colors.white.withValues(alpha: 0.78)),
                        ),
                    ],
                  ),
                ],
              ],
            ),
            if (tip.isNotEmpty) ...[
              const SizedBox(height: 9),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                decoration: BoxDecoration(
                  color: Colors.black.withValues(alpha: 0.14),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Row(
                  children: [
                    AppIcon(AppIcons.sparkles, size: 13.5, color: sky.accent),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        tip,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 12.5, height: 1.35, color: Colors.white.withValues(alpha: 0.93)),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ],
        ),
      );
    }

    final heroText = temp != null
        ? _fmtDeg(temp)
        : (low != null && high != null
            ? '${_fmtDeg(low)} ~ ${_fmtDeg(high)}'
            : (high != null || low != null ? _fmtDeg(high ?? low) : condition));

    return Container(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: sky.gradient,
        ),
        borderRadius: BorderRadius.circular(22),
        boxShadow: [
          BoxShadow(
            color: sky.gradient.last.withValues(alpha: dark ? 0.35 : 0.22),
            blurRadius: 16,
            offset: const Offset(0, 6),
          ),
        ],
      ),
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const AppIcon(AppIcons.mapPin, size: 16, color: Colors.white70),
                        const SizedBox(width: 5),
                        Flexible(
                          child: Text(
                            location.isEmpty ? '天气预报' : location,
                            style: const TextStyle(
                              fontSize: 18,
                              fontWeight: FontWeight.w700,
                              color: Colors.white,
                            ),
                          ),
                        ),
                      ],
                    ),
                    if (dateLabel.isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text(
                        dateLabel,
                        style: TextStyle(fontSize: 13, color: Colors.white.withValues(alpha: 0.78)),
                      ),
                    ],
                  ],
                ),
              ),
              Container(
                width: 48,
                height: 48,
                decoration: BoxDecoration(
                  color: Colors.white.withValues(alpha: 0.14),
                  shape: BoxShape.circle,
                ),
                child: Center(child: AppIcon(sky.icon, size: 28, color: sky.accent)),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: [
              Text(
                heroText,
                style: TextStyle(
                  fontSize: temp != null ? 42 : 32,
                  fontWeight: FontWeight.w700,
                  color: Colors.white,
                  height: 1.05,
                ),
              ),
              const SizedBox(width: 10),
              Flexible(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
                      decoration: BoxDecoration(
                        color: Colors.white.withValues(alpha: 0.18),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(
                        condition,
                        style: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: Colors.white),
                      ),
                    ),
                    if (temp != null && low != null && high != null) ...[
                      const SizedBox(height: 4),
                      Text(
                        '最高 ${_fmtDeg(high)}  ·  最低 ${_fmtDeg(low)}',
                        style: TextStyle(fontSize: 13, color: Colors.white.withValues(alpha: 0.85)),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
          if (metaPills.isNotEmpty) ...[
            const SizedBox(height: 14),
            Wrap(
              spacing: 8,
              runSpacing: 6,
              children: [
                for (final pill in metaPills)
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                    decoration: BoxDecoration(
                      color: Colors.white.withValues(alpha: 0.13),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        AppIcon(pill.$1, size: 14, color: Colors.white.withValues(alpha: 0.88)),
                        const SizedBox(width: 5),
                        Text(pill.$2, style: const TextStyle(fontSize: 12.5, color: Colors.white)),
                      ],
                    ),
                  ),
              ],
            ),
          ],
          if (tip.isNotEmpty) ...[
            const SizedBox(height: 12),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              decoration: BoxDecoration(
                color: Colors.black.withValues(alpha: 0.16),
                borderRadius: BorderRadius.circular(13),
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Padding(
                    padding: const EdgeInsets.only(top: 2),
                    child: AppIcon(AppIcons.sparkles, size: 15, color: sky.accent),
                  ),
                  const SizedBox(width: 7),
                  Expanded(
                    child: Text(
                      tip,
                      style: TextStyle(fontSize: 13.5, height: 1.4, color: Colors.white.withValues(alpha: 0.94)),
                    ),
                  ),
                ],
              ),
            ),
          ],
          if (forecast.isNotEmpty) ...[
            const SizedBox(height: 14),
            Container(
              padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
              decoration: BoxDecoration(
                color: Colors.white.withValues(alpha: 0.11),
                borderRadius: BorderRadius.circular(14),
              ),
              child: Column(
                children: [
                  for (var i = 0; i < forecast.length; i++) ...[
                    if (i > 0) const SizedBox(height: 8),
                    _ForecastRow(
                      item: forecast[i],
                      weekMin: weekMin,
                      weekMax: weekMax,
                      accent: sky.accent,
                    ),
                  ],
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _ForecastRow extends StatelessWidget {
  const _ForecastRow({
    required this.item,
    required this.weekMin,
    required this.weekMax,
    required this.accent,
  });

  final ({String day, String cond, double low, double high}) item;
  final double weekMin, weekMax;
  final Color accent;

  @override
  Widget build(BuildContext context) {
    final span = math.max(weekMax - weekMin, 1.0);
    final startFrac = ((item.low - weekMin) / span).clamp(0.0, 1.0);
    final endFrac = ((item.high - weekMin) / span).clamp(startFrac + 0.08, 1.0);
    final icon = WeatherCard._skyStyle(item.cond, false).icon;

    return Row(
      children: [
        SizedBox(
          width: 48,
          child: Text(
            item.day,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: Colors.white),
          ),
        ),
        AppIcon(icon, size: 16, color: accent),
        const SizedBox(width: 5),
        SizedBox(
          width: 46,
          child: Text(
            item.cond,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontSize: 12.5, color: Colors.white.withValues(alpha: 0.85)),
          ),
        ),
        SizedBox(
          width: 32,
          child: Text(
            WeatherCard._fmtDeg(item.low),
            textAlign: TextAlign.right,
            style: TextStyle(fontSize: 12.5, color: Colors.white.withValues(alpha: 0.75)),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: LayoutBuilder(
            builder: (_, c) {
              final w = c.maxWidth;
              return Container(
                height: 6,
                decoration: BoxDecoration(
                  color: Colors.white.withValues(alpha: 0.18),
                  borderRadius: BorderRadius.circular(4),
                ),
                child: Stack(
                  children: [
                    Positioned(
                      left: w * startFrac,
                      width: math.max(8, w * (endFrac - startFrac)),
                      top: 0,
                      bottom: 0,
                      child: DecoratedBox(
                        decoration: BoxDecoration(
                          gradient: LinearGradient(colors: [ Colors.lightBlueAccent.shade100, accent ]),
                          borderRadius: BorderRadius.circular(4),
                        ),
                      ),
                    ),
                  ],
                ),
              );
            },
          ),
        ),
        const SizedBox(width: 8),
        SizedBox(
          width: 32,
          child: Text(
            WeatherCard._fmtDeg(item.high),
            style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: Colors.white),
          ),
        ),
      ],
    );
  }
}

// ======================= 2. Interactive Todo Card =======================

class TodoCard extends ConsumerWidget {
  const TodoCard({super.key, required this.messageId, required this.component});
  final String messageId;
  final MessageComponent component;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final d = component.data;
    final title = '${d['title'] ?? '待办清单'}'.trim();
    final subtitle = '${d['subtitle'] ?? ''}'.trim();
    final rawItems = d['items'] is List ? (d['items'] as List) : const [];

    final hasSavedState = component.state.containsKey('done_ids');
    final doneSet = <String>{
      if (hasSavedState && component.state['done_ids'] is List)
        for (final id in component.state['done_ids'] as List) '$id',
    };

    final items = <({String id, String text, String note, String tag, bool done})>[];
    for (var i = 0; i < rawItems.length; i++) {
      final m = rawItems[i];
      if (m is! Map) continue;
      final id = '${m['id'] ?? 'i${i + 1}'}';
      final initialDone = m['done'] == true;
      final isDone = hasSavedState ? doneSet.contains(id) : initialDone;
      if (!hasSavedState && initialDone) doneSet.add(id);
      items.add((
        id: id,
        text: '${m['text'] ?? ''}',
        note: '${m['note'] ?? ''}',
        tag: '${m['tag'] ?? ''}',
        done: isDone,
      ));
    }
    if (items.isEmpty) return const SizedBox.shrink();

    final doneCount = items.where((e) => e.done).length;
    final progress = doneCount / items.length;
    final allDone = doneCount == items.length;

    void toggle(String id) {
      HapticFeedback.selectionClick();
      final next = {...doneSet};
      if (!next.remove(id)) next.add(id);
      final ordered = [for (final it in items) if (next.contains(it.id)) it.id];
      ref.read(chatProvider.notifier).updateComponentState(
        messageId,
        component.id,
        {'done_ids': ordered},
      );
    }

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: allDone ? p.good.withValues(alpha: 0.38) : p.line),
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
                decoration: BoxDecoration(
                  color: allDone ? p.good.withValues(alpha: 0.14) : p.accentSoft,
                  borderRadius: BorderRadius.circular(11),
                ),
                child: Center(
                  child: AppIcon(
                    allDone ? AppIcons.checkCircle : AppIcons.listChecks,
                    size: 19,
                    color: allDone ? p.good : p.accent,
                  ),
                ),
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
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                decoration: BoxDecoration(
                  color: allDone ? p.good.withValues(alpha: 0.14) : p.surfaceAlt,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  allDone ? '全部完成 ✓' : '$doneCount / ${items.length}',
                  style: TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                    color: allDone ? p.good : p.accent,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 11),
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: TweenAnimationBuilder<double>(
              tween: Tween(begin: 0, end: progress),
              duration: const Duration(milliseconds: 260),
              curve: Curves.easeOutCubic,
              builder: (_, val, _) => LinearProgressIndicator(
                value: val,
                minHeight: 5,
                backgroundColor: p.surfaceAlt,
                valueColor: AlwaysStoppedAnimation(allDone ? p.good : p.accent),
              ),
            ),
          ),
          const SizedBox(height: 8),
          for (var i = 0; i < items.length; i++) ...[
            if (i > 0) Divider(height: 1, color: p.line.withValues(alpha: 0.6)),
            Pressable(
              scale: 0.99,
              onTap: () => toggle(items[i].id),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 9),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 1),
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 180),
                        width: 22,
                        height: 22,
                        decoration: BoxDecoration(
                          color: items[i].done ? p.good : Colors.transparent,
                          borderRadius: BorderRadius.circular(7),
                          border: Border.all(
                            color: items[i].done ? p.good : p.faint,
                            width: 1.6,
                          ),
                        ),
                        child: items[i].done
                            ? const Center(child: Icon(Icons.check_rounded, size: 15, color: Colors.white))
                            : null,
                      ),
                    ),
                    const SizedBox(width: 11),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  items[i].text,
                                  style: TextStyle(
                                    fontSize: 15,
                                    height: 1.38,
                                    color: items[i].done ? p.muted : p.text,
                                    decoration: items[i].done ? TextDecoration.lineThrough : null,
                                    decorationColor: p.muted,
                                  ),
                                ),
                              ),
                              if (items[i].tag.isNotEmpty) ...[
                                const SizedBox(width: 6),
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: p.surfaceAlt,
                                    borderRadius: BorderRadius.circular(8),
                                  ),
                                  child: Text(
                                    items[i].tag,
                                    style: TextStyle(fontSize: 11.5, color: p.muted, fontWeight: FontWeight.w500),
                                  ),
                                ),
                              ],
                            ],
                          ),
                          if (items[i].note.isNotEmpty) ...[
                            const SizedBox(height: 2),
                            Text(items[i].note, style: TextStyle(fontSize: 12.5, color: p.faint)),
                          ],
                        ],
                      ),
                    ),
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

// ======================= 3. Visual Chart Card =======================

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

// ======================= 4. Interactive Quiz Card =======================

class QuizCard extends ConsumerWidget {
  const QuizCard({super.key, required this.messageId, required this.component});
  final String messageId;
  final MessageComponent component;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final d = component.data;
    final question = '${d['question'] ?? ''}'.trim();
    final tag = '${d['tag'] ?? '互动小测'}'.trim();
    final options = [
      if (d['options'] is List)
        for (final o in d['options'] as List) '$o',
    ];
    final answerIdx = ((d['answer_index'] as num?)?.toInt() ?? 0).clamp(0, math.max<int>(0, options.length - 1));
    final explanation = '${d['explanation'] ?? ''}'.trim();

    if (question.isEmpty || options.length < 2) return const SizedBox.shrink();

    final selectedIdx = (component.state['selected_index'] as num?)?.toInt() ?? -1;
    final answered = selectedIdx >= 0 && selectedIdx < options.length;
    final isRight = answered && selectedIdx == answerIdx;

    void choose(int idx) {
      HapticFeedback.lightImpact();
      ref.read(chatProvider.notifier).updateComponentState(
        messageId,
        component.id,
        {'selected_index': idx},
      );
    }

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(
          color: !answered
              ? p.line
              : (isRight ? p.good.withValues(alpha: 0.42) : p.accent.withValues(alpha: 0.42)),
        ),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 12, offset: const Offset(0, 4))],
      ),
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(10)),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(AppIcons.question, size: 15, color: p.accent),
                    const SizedBox(width: 5),
                    Text(
                      tag.isEmpty ? '互动小测' : tag,
                      style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.accent),
                    ),
                  ],
                ),
              ),
              const Spacer(),
              if (answered)
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(
                      isRight ? AppIcons.checkCircleFill : AppIcons.thinkOn,
                      size: 15,
                      color: isRight ? p.good : p.accent,
                    ),
                    const SizedBox(width: 4),
                    Text(
                      isRight ? '答对啦' : '看解析涨知识',
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                        color: isRight ? p.good : p.accent,
                      ),
                    ),
                  ],
                )
              else
                Text('点选项揭晓答案', style: TextStyle(fontSize: 12.5, color: p.faint)),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            question,
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, height: 1.45, color: p.text),
          ),
          const SizedBox(height: 12),
          for (var i = 0; i < options.length; i++) ...[
            if (i > 0) const SizedBox(height: 8),
            _QuizOptionTile(
              letter: String.fromCharCode(65 + i),
              text: options[i],
              answered: answered,
              isSelected: selectedIdx == i,
              isCorrect: answerIdx == i,
              onTap: () => choose(i),
            ),
          ],
          AnimatedSize(
            duration: const Duration(milliseconds: 240),
            curve: Curves.easeOutCubic,
            child: answered
                ? Container(
                    margin: const EdgeInsets.only(top: 12),
                    padding: const EdgeInsets.all(12),
                    decoration: BoxDecoration(
                      color: isRight ? p.good.withValues(alpha: 0.08) : p.surfaceAlt,
                      borderRadius: BorderRadius.circular(14),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Text(
                              '正确答案：${String.fromCharCode(65 + answerIdx)}',
                              style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w700, color: p.good),
                            ),
                            const Spacer(),
                            Pressable(
                              onTap: () => choose(-1),
                              child: Text('重做', style: TextStyle(fontSize: 12.5, color: p.muted)),
                            ),
                          ],
                        ),
                        if (explanation.isNotEmpty) ...[
                          const SizedBox(height: 5),
                          Text(
                            explanation,
                            style: TextStyle(fontSize: 14, height: 1.48, color: p.text),
                          ),
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

class _QuizOptionTile extends StatelessWidget {
  const _QuizOptionTile({
    required this.letter,
    required this.text,
    required this.answered,
    required this.isSelected,
    required this.isCorrect,
    required this.onTap,
  });

  final String letter, text;
  final bool answered, isSelected, isCorrect;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    Color bg = p.surfaceAlt.withValues(alpha: 0.55);
    Color border = p.line;
    Color badgeBg = p.surface;
    Color badgeFg = p.muted;

    if (answered) {
      if (isCorrect) {
        bg = p.good.withValues(alpha: 0.11);
        border = p.good.withValues(alpha: 0.55);
        badgeBg = p.good;
        badgeFg = Colors.white;
      } else if (isSelected) {
        bg = p.bad.withValues(alpha: 0.09);
        border = p.bad.withValues(alpha: 0.5);
        badgeBg = p.bad;
        badgeFg = Colors.white;
      }
    }

    return Pressable(
      scale: 0.99,
      onTap: answered ? null : onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 11),
        decoration: BoxDecoration(
          color: bg,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: border),
        ),
        child: Row(
          children: [
            Container(
              width: 26,
              height: 26,
              decoration: BoxDecoration(
                color: badgeBg,
                shape: BoxShape.circle,
                border: answered && (isCorrect || isSelected) ? null : Border.all(color: p.line),
              ),
              child: Center(
                child: Text(
                  letter,
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w700, color: badgeFg),
                ),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                text,
                style: TextStyle(
                  fontSize: 14.5,
                  height: 1.38,
                  fontWeight: (answered && isCorrect) ? FontWeight.w600 : FontWeight.w400,
                  color: p.text,
                ),
              ),
            ),
            if (answered && isCorrect)
              AppIcon(AppIcons.checkCircleFill, size: 19, color: p.good)
            else if (answered && isSelected && !isCorrect)
              AppIcon(AppIcons.closeCircle, size: 19, color: p.bad),
          ],
        ),
      ),
    );
  }
}

// ======================= 5. Core Metrics KPI Card =======================

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

// ======================= 6. Native Ask-User Question Card =======================

class AskUserCard extends ConsumerStatefulWidget {
  const AskUserCard({
    super.key,
    required this.messageId,
    required this.component,
    required this.isLastMessage,
  });

  final String messageId;
  final MessageComponent component;
  final bool isLastMessage;

  @override
  ConsumerState<AskUserCard> createState() => _AskUserCardState();
}

class _AskUserCardState extends ConsumerState<AskUserCard> {
  int _step = 0;
  final Map<String, Set<String>> _selected = {};
  final Map<String, bool> _useCustom = {};
  final Map<String, TextEditingController> _customCtrls = {};
  bool _hydrated = false;
  bool _forceExpanded = false;

  @override
  void dispose() {
    for (final c in _customCtrls.values) {
      c.dispose();
    }
    super.dispose();
  }

  TextEditingController _ctrlFor(String qid) =>
      _customCtrls.putIfAbsent(qid, () => TextEditingController()..addListener(() => setState(() {})));

  void _hydrateFromStateIfNeeded() {
    if (_hydrated) return;
    _hydrated = true;
    final ans = widget.component.state['answers'];
    if (ans is Map) {
      for (final entry in ans.entries) {
        final qid = '${entry.key}';
        final v = entry.value;
        if (v is Map) {
          if (v['selected'] is List) {
            _selected[qid] = {for (final s in v['selected'] as List) '$s'};
          }
          final custom = '${v['custom'] ?? ''}'.trim();
          if (custom.isNotEmpty) {
            _useCustom[qid] = true;
            _ctrlFor(qid).text = custom;
          }
        }
      }
    }
  }

  List<({
    String id,
    String question,
    bool multi,
    List<({String label, String desc, bool recommended})> options,
  })> _parseQuestions() {
    final raw = widget.component.data['questions'];
    if (raw is! List) return const [];
    final out = <({
      String id,
      String question,
      bool multi,
      List<({String label, String desc, bool recommended})> options,
    })>[];
    for (var i = 0; i < raw.length; i++) {
      final m = raw[i];
      if (m is! Map) continue;
      final opts = <({String label, String desc, bool recommended})>[];
      if (m['options'] is List) {
        for (final o in m['options'] as List) {
          if (o is Map) {
            final lbl = '${o['label'] ?? ''}'.trim();
            if (lbl.isNotEmpty) {
              opts.add((
                label: lbl,
                desc: '${o['description'] ?? ''}'.trim(),
                recommended: o['recommended'] == true,
              ));
            }
          } else {
            final lbl = '$o'.trim();
            if (lbl.isNotEmpty) opts.add((label: lbl, desc: '', recommended: false));
          }
        }
      }
      if (opts.isNotEmpty) {
        out.add((
          id: '${m['id'] ?? 'q${i + 1}'}',
          question: '${m['question'] ?? ''}'.trim(),
          multi: m['multi_select'] == true,
          options: opts,
        ));
      }
    }
    return out;
  }

  bool _questionHasAnswer(String qid) {
    final sel = _selected[qid];
    if (sel != null && sel.isNotEmpty) return true;
    if (_useCustom[qid] == true && _ctrlFor(qid).text.trim().isNotEmpty) return true;
    return false;
  }

  List<String> _answerLabelsFor(String qid) {
    final parts = <String>[...?_selected[qid]];
    if (_useCustom[qid] == true) {
      final c = _ctrlFor(qid).text.trim();
      if (c.isNotEmpty) parts.add(c);
    }
    return parts;
  }

  void _toggleOption(String qid, String label, bool multi, int totalQuestions) {
    HapticFeedback.selectionClick();
    setState(() {
      final set = _selected.putIfAbsent(qid, () => <String>{});
      if (multi) {
        if (!set.remove(label)) set.add(label);
      } else {
        set
          ..clear()
          ..add(label);
        _useCustom[qid] = false;
        // Smoothly step forward when answering single-choice multi-question prompts.
        if (_step < totalQuestions - 1) {
          Future.delayed(const Duration(milliseconds: 180), () {
            if (mounted && _step < totalQuestions - 1) {
              setState(() => _step++);
            }
          });
        }
      }
    });
  }

  void _toggleCustom(String qid, bool multi) {
    HapticFeedback.selectionClick();
    setState(() {
      final next = !(_useCustom[qid] ?? false);
      _useCustom[qid] = next;
      if (next && !multi) {
        _selected[qid]?.clear();
      }
    });
  }

  Future<void> _submit(
    List<({
      String id,
      String question,
      bool multi,
      List<({String label, String desc, bool recommended})> options,
    })> questions,
  ) async {
    HapticFeedback.mediumImpact();
    final answersJson = <String, dynamic>{};
    final lines = <String>[];

    for (var i = 0; i < questions.length; i++) {
      final q = questions[i];
      final sel = _selected[q.id]?.toList() ?? const <String>[];
      final custom = (_useCustom[q.id] == true) ? _ctrlFor(q.id).text.trim() : '';
      answersJson[q.id] = {
        if (sel.isNotEmpty) 'selected': sel,
        if (custom.isNotEmpty) 'custom': custom,
      };
      final combined = [...sel, if (custom.isNotEmpty) custom];
      if (combined.isNotEmpty) {
        if (questions.length == 1) {
          lines.add(combined.join('、'));
        } else {
          lines.add('${i + 1}. ${q.question}：${combined.join('、')}');
        }
      }
    }
    if (lines.isEmpty) return;

    final replyText = questions.length == 1 ? lines.first : '我的选择：\n${lines.join('\n')}';
    setState(() => _forceExpanded = false);
    await ref.read(chatProvider.notifier).submitAskUserAnswer(
          widget.messageId,
          widget.component.id,
          {'submitted': true, 'dismissed': false, 'answers': answersJson},
          replyText,
        );
  }

  Future<void> _dismiss(
    List<({
      String id,
      String question,
      bool multi,
      List<({String label, String desc, bool recommended})> options,
    })> questions,
  ) async {
    HapticFeedback.lightImpact();
    final answersJson = <String, dynamic>{};
    for (final q in questions) {
      final sel = _selected[q.id]?.toList() ?? const <String>[];
      final custom = (_useCustom[q.id] == true) ? _ctrlFor(q.id).text.trim() : '';
      if (sel.isNotEmpty || custom.isNotEmpty) {
        answersJson[q.id] = {
          if (sel.isNotEmpty) 'selected': sel,
          if (custom.isNotEmpty) 'custom': custom,
        };
      }
    }
    setState(() => _forceExpanded = false);
    await ref.read(chatProvider.notifier).updateComponentState(
          widget.messageId,
          widget.component.id,
          {'submitted': false, 'dismissed': true, 'answers': answersJson},
        );
  }

  Future<void> _reopen() async {
    HapticFeedback.selectionClick();
    setState(() => _forceExpanded = true);
    if (widget.component.state['dismissed'] == true) {
      final next = Map<String, dynamic>.from(widget.component.state)..['dismissed'] = false;
      await ref.read(chatProvider.notifier).updateComponentState(
            widget.messageId,
            widget.component.id,
            next,
          );
    }
  }

  @override
  Widget build(BuildContext context) {
    _hydrateFromStateIfNeeded();
    final p = context.pal;
    final questions = _parseQuestions();
    if (questions.isEmpty) return const SizedBox.shrink();

    final title = '${widget.component.data['title'] ?? ''}'.trim();
    final submitted = widget.component.state['submitted'] == true;
    final dismissed = widget.component.state['dismissed'] == true;
    final chatBusy = ref.watch(chatProvider.select((s) => s.busy));

    // ---- Confirmed / Submitted Summary View ----
    if (submitted) {
      return Container(
        decoration: BoxDecoration(
          color: p.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: p.good.withValues(alpha: 0.35)),
        ),
        padding: const EdgeInsets.fromLTRB(16, 13, 16, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                AppIcon(AppIcons.checkCircleFill, size: 18, color: p.good),
                const SizedBox(width: 7),
                Text(
                  title.isEmpty ? '已确认偏好' : '$title · 已确认',
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w700, color: p.good),
                ),
              ],
            ),
            const SizedBox(height: 10),
            for (var i = 0; i < questions.length; i++) ...[
              if (i > 0) const SizedBox(height: 8),
              Builder(
                builder: (_) {
                  final vals = _answerLabelsFor(questions[i].id);
                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Text(
                          questions[i].question,
                          style: TextStyle(fontSize: 13.5, color: p.muted),
                        ),
                      ),
                      const SizedBox(width: 10),
                      Flexible(
                        child: Text(
                          vals.isEmpty ? '未指定' : vals.join('、'),
                          textAlign: TextAlign.right,
                          style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.text),
                        ),
                      ),
                    ],
                  );
                },
              ),
            ],
          ],
        ),
      );
    }

    // ---- Dismissed or User Continued Chatting Without Answering ----
    final autoCollapsed = !widget.isLastMessage && !_forceExpanded;
    if ((dismissed && !_forceExpanded) || autoCollapsed) {
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        decoration: BoxDecoration(
          color: p.surfaceAlt.withValues(alpha: 0.72),
          borderRadius: BorderRadius.circular(16),
          border: Border.all(color: p.line),
        ),
        child: Row(
          children: [
            AppIcon(AppIcons.sparkles, size: 15, color: p.muted),
            const SizedBox(width: 7),
            Expanded(
              child: Text(
                dismissed ? '已跳过偏好选择，直接自由对话' : '未点选偏好 · 已继续对话',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 13.5, color: p.muted),
              ),
            ),
            const SizedBox(width: 8),
            Pressable(
              onTap: _reopen,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
                child: Text(
                  '重新展开',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: p.accent),
                ),
              ),
            ),
          ],
        ),
      );
    }

    // ---- Interactive Selection View ----
    final curIdx = _step.clamp(0, questions.length - 1);
    final q = questions[curIdx];
    final answeredCount = questions.where((item) => _questionHasAnswer(item.id)).length;
    final canSubmit = answeredCount > 0 && !chatBusy;
    final curSel = _selected[q.id] ?? const <String>{};
    final customOn = _useCustom[q.id] == true;

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: p.accent.withValues(alpha: 0.34), width: 1.2),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 14, offset: const Offset(0, 5))],
      ),
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(10)),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(AppIcons.sparkles, size: 14, color: p.accent),
                    const SizedBox(width: 5),
                    Text(
                      title.isEmpty ? '请帮小暖确认一下' : title,
                      style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.accent),
                    ),
                  ],
                ),
              ),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(8)),
                child: Text(
                  q.multi ? '可多选' : '单选',
                  style: TextStyle(fontSize: 11.5, color: p.muted, fontWeight: FontWeight.w500),
                ),
              ),
              const SizedBox(width: 6),
              Tooltip(
                message: '跳过不选',
                child: Pressable(
                  onTap: () => _dismiss(questions),
                  child: Container(
                    width: 26,
                    height: 26,
                    decoration: BoxDecoration(
                      color: p.surfaceAlt,
                      shape: BoxShape.circle,
                    ),
                    child: Center(child: AppIcon(AppIcons.close, size: 15, color: p.muted)),
                  ),
                ),
              ),
            ],
          ),
          if (questions.length > 1) ...[
            const SizedBox(height: 11),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (var i = 0; i < questions.length; i++) ...[
                    if (i > 0) const SizedBox(width: 6),
                    Pressable(
                      onTap: () => setState(() => _step = i),
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 180),
                        padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 5),
                        decoration: BoxDecoration(
                          color: curIdx == i
                              ? p.accent
                              : (_questionHasAnswer(questions[i].id) ? p.accentSoft : p.surfaceAlt),
                          borderRadius: BorderRadius.circular(14),
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(
                              '问题 ${i + 1}',
                              style: TextStyle(
                                fontSize: 12.5,
                                fontWeight: FontWeight.w600,
                                color: curIdx == i
                                    ? Colors.white
                                    : (_questionHasAnswer(questions[i].id) ? p.accent : p.muted),
                              ),
                            ),
                            if (_questionHasAnswer(questions[i].id) && curIdx != i) ...[
                              const SizedBox(width: 4),
                              Icon(Icons.check_rounded, size: 13, color: p.accent),
                            ],
                          ],
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
          const SizedBox(height: 12),
          Text(
            q.question,
            style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, height: 1.4, color: p.text),
          ),
          const SizedBox(height: 11),
          for (var i = 0; i < q.options.length; i++) ...[
            if (i > 0) const SizedBox(height: 8),
            Builder(
              builder: (_) {
                final opt = q.options[i];
                final active = curSel.contains(opt.label);
                return Pressable(
                  scale: 0.99,
                  onTap: () => _toggleOption(q.id, opt.label, q.multi, questions.length),
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 180),
                    padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 11),
                    decoration: BoxDecoration(
                      color: active ? p.accentSoft.withValues(alpha: 0.72) : p.surfaceAlt.withValues(alpha: 0.5),
                      borderRadius: BorderRadius.circular(14),
                      border: Border.all(
                        color: active ? p.accent : p.line,
                        width: active ? 1.5 : 1.0,
                      ),
                    ),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Padding(
                          padding: const EdgeInsets.only(top: 2),
                          child: AppIcon(
                            q.multi
                                ? (active ? AppIcons.checkSquare : AppIcons.square)
                                : (active ? AppIcons.radioOn : AppIcons.radioOff),
                            size: 19,
                            color: active ? p.accent : p.faint,
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  Flexible(
                                    child: Text(
                                      opt.label,
                                      style: TextStyle(
                                        fontSize: 15,
                                        fontWeight: active ? FontWeight.w600 : FontWeight.w500,
                                        color: p.text,
                                      ),
                                    ),
                                  ),
                                  if (opt.recommended) ...[
                                    const SizedBox(width: 6),
                                    Container(
                                      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1.5),
                                      decoration: BoxDecoration(
                                        color: p.accent.withValues(alpha: 0.15),
                                        borderRadius: BorderRadius.circular(6),
                                      ),
                                      child: Text(
                                        '推荐',
                                        style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: p.accent),
                                      ),
                                    ),
                                  ],
                                ],
                              ),
                              if (opt.desc.isNotEmpty) ...[
                                const SizedBox(height: 2),
                                Text(opt.desc, style: TextStyle(fontSize: 12.5, color: p.muted)),
                              ],
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                );
              },
            ),
          ],
          const SizedBox(height: 8),
          Pressable(
            scale: 0.99,
            onTap: () => _toggleCustom(q.id, q.multi),
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 180),
              padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 10),
              decoration: BoxDecoration(
                color: customOn ? p.accentSoft.withValues(alpha: 0.6) : Colors.transparent,
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: customOn ? p.accent : p.line),
              ),
              child: Row(
                children: [
                  AppIcon(
                    customOn ? AppIcons.edit : AppIcons.plus,
                    size: 17,
                    color: customOn ? p.accent : p.muted,
                  ),
                  const SizedBox(width: 9),
                  Text(
                    '其他（自己补充说明…）',
                    style: TextStyle(
                      fontSize: 14,
                      color: customOn ? p.text : p.muted,
                      fontWeight: customOn ? FontWeight.w600 : FontWeight.w400,
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (customOn) ...[
            const SizedBox(height: 8),
            TextField(
              controller: _ctrlFor(q.id),
              style: TextStyle(fontSize: 14.5, color: p.text),
              minLines: 1,
              maxLines: 3,
              decoration: InputDecoration(
                hintText: '写下你的想法…',
                contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
                fillColor: p.bg,
              ),
            ),
          ],
          const SizedBox(height: 13),
          Row(
            children: [
              Pressable(
                onTap: () => _dismiss(questions),
                child: Container(
                  height: 46,
                  padding: const EdgeInsets.symmetric(horizontal: 14),
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: p.surfaceAlt,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Text(
                    '暂不选择',
                    style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: p.muted),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              if (questions.length > 1 && curIdx > 0) ...[
                OutlinedButton(
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size(68, 44),
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                  ),
                  onPressed: () => setState(() => _step = curIdx - 1),
                  child: const Text('上一题'),
                ),
                const SizedBox(width: 6),
              ],
              if (questions.length > 1 && curIdx < questions.length - 1 && answeredCount < questions.length)
                Expanded(
                  child: OutlinedButton(
                    style: OutlinedButton.styleFrom(minimumSize: const Size(88, 44)),
                    onPressed: () => setState(() => _step = curIdx + 1),
                    child: const Text('下一题'),
                  ),
                )
              else
                Expanded(
                  child: FilledButton(
                    style: FilledButton.styleFrom(
                      minimumSize: const Size(112, 46),
                      backgroundColor: p.accent,
                      foregroundColor: Colors.white,
                    ),
                    onPressed: canSubmit ? () => _submit(questions) : null,
                    child: Text(
                      questions.length > 1
                          ? '确认发送（$answeredCount/${questions.length}）'
                          : '确认并发送',
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

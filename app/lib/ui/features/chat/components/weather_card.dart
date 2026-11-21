import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../../../core/models/models.dart';
import '../../../theme/icons.dart';

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

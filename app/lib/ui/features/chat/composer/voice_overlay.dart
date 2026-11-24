import 'dart:math' as math;
import 'dart:ui';

import 'package:flutter/material.dart';

import '../../../theme/icons.dart';
import '../../../theme/theme.dart';

/// Lifecycle phase of a hold-to-talk voice input session.
enum VoicePhase { idle, pressing, recording, cancelling, recognizing }

/// Elevated floating overlay shown while recording, cancelling, or transcribing voice input.
class VoiceOverlay extends StatefulWidget {
  const VoiceOverlay({
    super.key,
    required this.phase,
    required this.level,
    required this.seconds,
    required this.liveText,
    required this.cancelProgress,
    required this.streamMode,
    required this.maxSeconds,
  });

  final ValueNotifier<VoicePhase> phase;
  final ValueNotifier<double> level;
  final ValueNotifier<int> seconds;
  final ValueNotifier<String> liveText;
  final ValueNotifier<double> cancelProgress;
  final bool streamMode;
  final int maxSeconds;

  @override
  State<VoiceOverlay> createState() => _VoiceOverlayState();
}

class _VoiceOverlayState extends State<VoiceOverlay> with TickerProviderStateMixin {
  late final AnimationController _enterCtrl = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 220),
  )..forward();

  late final AnimationController _waveCtrl = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1400),
  )..repeat();

  late final Animation<double> _backdropFade = CurvedAnimation(
    parent: _enterCtrl,
    curve: Curves.easeOut,
  );

  late final Animation<double> _cardScale = Tween<double>(begin: 0.90, end: 1.0).animate(
    CurvedAnimation(parent: _enterCtrl, curve: Curves.easeOutCubic),
  );

  @override
  void dispose() {
    _enterCtrl.dispose();
    _waveCtrl.dispose();
    super.dispose();
  }

  String _formatDuration(int totalSeconds) {
    final m = (totalSeconds ~/ 60).toString().padLeft(2, '0');
    final s = (totalSeconds % 60).toString().padLeft(2, '0');
    return '$m:$s';
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isDark = Theme.of(context).brightness == Brightness.dark;

    return IgnorePointer(
      child: FadeTransition(
        opacity: _backdropFade,
        child: Stack(
          fit: StackFit.expand,
          children: [
            BackdropFilter(
              filter: ImageFilter.blur(sigmaX: 8, sigmaY: 8),
              child: ColoredBox(
                color: Colors.black.withValues(alpha: isDark ? 0.52 : 0.34),
              ),
            ),
            Material(
              type: MaterialType.transparency,
              child: SafeArea(
                child: Center(
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 24),
                    child: ScaleTransition(
                      scale: _cardScale,
                      child: ValueListenableBuilder<VoicePhase>(
                        valueListenable: widget.phase,
                        builder: (_, ph, _) {
                          final cancelling = ph == VoicePhase.cancelling;
                          final recognizing = ph == VoicePhase.recognizing;
                          final accentColor = cancelling ? p.bad : p.accent;

                          return AnimatedContainer(
                            duration: const Duration(milliseconds: 200),
                            curve: Curves.easeOutCubic,
                            constraints: BoxConstraints(
                              maxWidth: widget.streamMode ? 340 : 292,
                              minWidth: 260,
                            ),
                            padding: const EdgeInsets.fromLTRB(22, 18, 22, 22),
                            decoration: BoxDecoration(
                              color: p.surface.withValues(alpha: isDark ? 0.96 : 0.98),
                              borderRadius: BorderRadius.circular(28),
                              border: Border.all(
                                color: cancelling ? p.bad.withValues(alpha: 0.42) : p.line,
                                width: cancelling ? 1.5 : 1,
                              ),
                              boxShadow: [
                                BoxShadow(
                                  color: cancelling
                                      ? p.bad.withValues(alpha: isDark ? 0.24 : 0.16)
                                      : p.shadow.withValues(alpha: isDark ? 0.48 : 0.22),
                                  blurRadius: 36,
                                  spreadRadius: 2,
                                  offset: const Offset(0, 14),
                                ),
                              ],
                            ),
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                _StatusBadge(
                                  phase: ph,
                                  seconds: widget.seconds,
                                  maxSeconds: widget.maxSeconds,
                                  waveAnim: _waveCtrl,
                                  formatDuration: _formatDuration,
                                ),
                                const SizedBox(height: 18),
                                _OrbVisual(
                                  phase: ph,
                                  level: widget.level,
                                  waveAnim: _waveCtrl,
                                ),
                                const SizedBox(height: 14),
                                if (!recognizing)
                                  _WaveformBars(
                                    level: widget.level,
                                    waveAnim: _waveCtrl,
                                    color: accentColor,
                                    muted: cancelling,
                                  )
                                else
                                  const SizedBox(height: 26),
                                if (widget.streamMode) ...[
                                  const SizedBox(height: 16),
                                  _LiveTranscriptBox(
                                    liveText: widget.liveText,
                                    cancelling: cancelling,
                                    recognizing: recognizing,
                                  ),
                                ] else
                                  const SizedBox(height: 16),
                                Text(
                                  recognizing
                                      ? '正在整理语音内容…'
                                      : (cancelling ? '松开手指，取消发送' : '松开即发送'),
                                  textAlign: TextAlign.center,
                                  style: TextStyle(
                                    fontSize: 18,
                                    fontWeight: FontWeight.w700,
                                    letterSpacing: 0.2,
                                    color: cancelling ? p.bad : p.text,
                                  ),
                                ),
                                const SizedBox(height: 8),
                                if (!recognizing)
                                  _SlideCancelHint(
                                    cancelProgress: widget.cancelProgress,
                                    cancelling: cancelling,
                                  )
                                else
                                  Text(
                                    '请稍候片刻',
                                    style: TextStyle(fontSize: 13.5, color: p.muted),
                                  ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
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

class _StatusBadge extends StatelessWidget {
  const _StatusBadge({
    required this.phase,
    required this.seconds,
    required this.maxSeconds,
    required this.waveAnim,
    required this.formatDuration,
  });

  final VoicePhase phase;
  final ValueNotifier<int> seconds;
  final int maxSeconds;
  final Animation<double> waveAnim;
  final String Function(int) formatDuration;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final cancelling = phase == VoicePhase.cancelling;
    final recognizing = phase == VoicePhase.recognizing;

    return ValueListenableBuilder<int>(
      valueListenable: seconds,
      builder: (_, s, _) {
        final nearLimit = !recognizing && !cancelling && s >= maxSeconds - 10;
        final bg = cancelling
            ? p.bad.withValues(alpha: 0.12)
            : (nearLimit ? p.bad.withValues(alpha: 0.12) : p.surfaceAlt);
        final fg = cancelling || nearLimit ? p.bad : p.muted;

        return AnimatedContainer(
          duration: const Duration(milliseconds: 180),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 5),
          decoration: BoxDecoration(
            color: bg,
            borderRadius: BorderRadius.circular(999),
            border: Border.all(
              color: cancelling || nearLimit ? p.bad.withValues(alpha: 0.25) : p.line,
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (!recognizing) ...[
                AnimatedBuilder(
                  animation: waveAnim,
                  builder: (_, _) {
                    final pulse = 0.5 + 0.5 * math.sin(waveAnim.value * 2 * math.pi);
                    return Container(
                      width: 7,
                      height: 7,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: (cancelling ? p.bad : p.accent).withValues(alpha: 0.45 + 0.55 * pulse),
                      ),
                    );
                  },
                ),
                const SizedBox(width: 7),
              ],
              Text(
                recognizing
                    ? '语音识别中'
                    : cancelling
                        ? '准备取消'
                        : nearLimit
                            ? '还剩 ${maxSeconds - s} 秒'
                            : '正在聆听 · ${formatDuration(s)}',
                style: TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                  color: fg,
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _OrbVisual extends StatelessWidget {
  const _OrbVisual({
    required this.phase,
    required this.level,
    required this.waveAnim,
  });

  final VoicePhase phase;
  final ValueNotifier<double> level;
  final Animation<double> waveAnim;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final cancelling = phase == VoicePhase.cancelling;
    final recognizing = phase == VoicePhase.recognizing;

    return SizedBox(
      width: 120,
      height: 120,
      child: recognizing
          ? Center(
              child: Container(
                width: 74,
                height: 74,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: p.accentSoft,
                  border: Border.all(color: p.accent.withValues(alpha: 0.25)),
                ),
                child: Center(
                  child: SizedBox(
                    width: 30,
                    height: 30,
                    child: CircularProgressIndicator(
                      strokeWidth: 3,
                      strokeCap: StrokeCap.round,
                      color: p.accent,
                    ),
                  ),
                ),
              ),
            )
          : AnimatedBuilder(
              animation: Listenable.merge([level, waveAnim]),
              builder: (_, _) {
                final l = cancelling ? 0.0 : level.value;
                final breathe = 0.5 + 0.5 * math.sin(waveAnim.value * 2 * math.pi);
                final ringColor = cancelling ? p.bad : p.accent;
                final outerSize = 86 + (cancelling ? 6 * breathe : 26 * l + 8 * breathe);
                final innerSize = 78 + (cancelling ? 3 * breathe : 14 * l + 4 * breathe);

                return Stack(
                  alignment: Alignment.center,
                  children: [
                    Container(
                      width: outerSize,
                      height: outerSize,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: ringColor.withValues(alpha: cancelling ? 0.08 : 0.10 + 0.08 * l),
                      ),
                    ),
                    Container(
                      width: innerSize,
                      height: innerSize,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: ringColor.withValues(alpha: cancelling ? 0.14 : 0.18 + 0.10 * l),
                      ),
                    ),
                    AnimatedContainer(
                      duration: const Duration(milliseconds: 180),
                      curve: Curves.easeOutCubic,
                      width: cancelling ? 72 : 76,
                      height: cancelling ? 72 : 76,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        gradient: cancelling
                            ? LinearGradient(
                                begin: Alignment.topLeft,
                                end: Alignment.bottomRight,
                                colors: [p.bad.withValues(alpha: 0.88), p.bad],
                              )
                            : Palette.brandGradient,
                        boxShadow: [
                          BoxShadow(
                            color: ringColor.withValues(alpha: 0.36),
                            blurRadius: 18,
                            offset: const Offset(0, 6),
                          ),
                        ],
                      ),
                      child: AnimatedSwitcher(
                        duration: const Duration(milliseconds: 160),
                        transitionBuilder: (child, anim) => ScaleTransition(
                          scale: anim,
                          child: FadeTransition(opacity: anim, child: child),
                        ),
                        child: AppIcon(
                          cancelling ? AppIcons.close : AppIcons.mic,
                          key: ValueKey(cancelling),
                          color: Colors.white,
                          size: 32,
                        ),
                      ),
                    ),
                  ],
                );
              },
            ),
    );
  }
}

class _WaveformBars extends StatelessWidget {
  const _WaveformBars({
    required this.level,
    required this.waveAnim,
    required this.color,
    required this.muted,
  });

  final ValueNotifier<double> level;
  final Animation<double> waveAnim;
  final Color color;
  final bool muted;

  static const _barWeights = [0.36, 0.58, 0.78, 0.94, 1.0, 0.94, 0.78, 0.58, 0.36];

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 26,
      child: AnimatedBuilder(
        animation: Listenable.merge([level, waveAnim]),
        builder: (_, _) {
          final l = muted ? 0.0 : level.value;
          final phaseRad = waveAnim.value * 2 * math.pi;

          return Row(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.center,
            children: List.generate(_barWeights.length, (i) {
              final wave = 0.5 + 0.5 * math.sin(phaseRad * 2 + i * 0.75);
              final energy = muted ? 0.0 : (0.14 + 0.86 * l) * (0.55 + 0.45 * wave);
              final height = (5.0 + 20.0 * _barWeights[i] * energy).clamp(4.0, 26.0);

              return Padding(
                padding: const EdgeInsets.symmetric(horizontal: 2.5),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 70),
                  width: 3.5,
                  height: height,
                  decoration: BoxDecoration(
                    color: color.withValues(alpha: muted ? 0.28 : 0.45 + 0.55 * _barWeights[i]),
                    borderRadius: BorderRadius.circular(99),
                  ),
                ),
              );
            }),
          );
        },
      ),
    );
  }
}

class _LiveTranscriptBox extends StatelessWidget {
  const _LiveTranscriptBox({
    required this.liveText,
    required this.cancelling,
    required this.recognizing,
  });

  final ValueNotifier<String> liveText;
  final bool cancelling;
  final bool recognizing;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;

    return ValueListenableBuilder<String>(
      valueListenable: liveText,
      builder: (_, txt, _) {
        final hasText = txt.trim().isNotEmpty;
        return AnimatedContainer(
          duration: const Duration(milliseconds: 180),
          width: double.infinity,
          constraints: const BoxConstraints(minHeight: 54, maxHeight: 120),
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
          decoration: BoxDecoration(
            color: p.surfaceAlt,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(
              color: cancelling ? p.bad.withValues(alpha: 0.25) : p.line,
            ),
          ),
          child: Center(
            heightFactor: 1,
            child: SingleChildScrollView(
              reverse: true,
              physics: const NeverScrollableScrollPhysics(),
              child: Text(
                hasText ? txt : (recognizing ? '正在获取识别结果…' : '请直接说出你想问的…'),
                textAlign: TextAlign.center,
                style: TextStyle(
                  fontSize: hasText ? 16.5 : 14.5,
                  height: 1.48,
                  fontWeight: hasText ? FontWeight.w500 : FontWeight.w400,
                  color: !hasText
                      ? p.faint
                      : (cancelling ? p.muted : p.text),
                  decoration: hasText && cancelling ? TextDecoration.lineThrough : null,
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

class _SlideCancelHint extends StatelessWidget {
  const _SlideCancelHint({
    required this.cancelProgress,
    required this.cancelling,
  });

  final ValueNotifier<double> cancelProgress;
  final bool cancelling;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;

    return ValueListenableBuilder<double>(
      valueListenable: cancelProgress,
      builder: (_, progress, _) {
        final tint = Color.lerp(p.muted, p.bad, cancelling ? 1.0 : progress * 0.85)!;
        return Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Transform.translate(
              offset: Offset(0, -3 * progress),
              child: AppIcon(
                cancelling ? AppIcons.close : AppIcons.chevronUp,
                size: 15,
                color: tint,
              ),
            ),
            const SizedBox(width: 4),
            Text(
              cancelling ? '下滑可恢复发送' : '手指上滑取消',
              style: TextStyle(
                fontSize: 13.5,
                fontWeight: cancelling ? FontWeight.w600 : FontWeight.w400,
                color: tint,
              ),
            ),
          ],
        );
      },
    );
  }
}

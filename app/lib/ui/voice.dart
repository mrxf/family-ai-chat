import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

import '../core/api.dart';
import '../state/session.dart';
import 'theme.dart';
import 'widgets.dart';

/// Hold-to-talk microphone button (like WeChat): hold to record, release to
/// send, slide up to cancel. The recording is transcribed by the server.
class VoiceButton extends ConsumerStatefulWidget {
  const VoiceButton({super.key, required this.onText});
  final ValueChanged<String> onText;

  @override
  ConsumerState<VoiceButton> createState() => _VoiceButtonState();
}

enum _Phase { idle, recording, cancelling, recognizing }

class _VoiceButtonState extends ConsumerState<VoiceButton> {
  final _rec = AudioRecorder();
  final _phase = ValueNotifier(_Phase.idle);
  final _level = ValueNotifier(0.0);
  final _seconds = ValueNotifier(0);
  OverlayEntry? _overlay;
  StreamSubscription<Amplitude>? _ampSub;
  Timer? _timer;
  DateTime? _started;
  String? _path;
  bool _starting = false;

  static const _maxSeconds = 60;

  @override
  void dispose() {
    _cleanup();
    _rec.dispose();
    _phase.dispose();
    _level.dispose();
    _seconds.dispose();
    super.dispose();
  }

  void _cleanup() {
    _ampSub?.cancel();
    _ampSub = null;
    _timer?.cancel();
    _timer = null;
    _overlay?.remove();
    _overlay = null;
  }

  Future<void> _start() async {
    if (_starting || _phase.value != _Phase.idle) return;
    _starting = true;
    try {
      if (!await _rec.hasPermission()) {
        if (mounted) showToast(context, '需要麦克风权限才能说话，请在系统设置里允许');
        return;
      }
      final dir = await getTemporaryDirectory();
      _path = '${dir.path}/voice_${DateTime.now().millisecondsSinceEpoch}.m4a';
      await _rec.start(const RecordConfig(encoder: AudioEncoder.aacLc, sampleRate: 16000, numChannels: 1, bitRate: 48000), path: _path!);
      HapticFeedback.mediumImpact();
      _started = DateTime.now();
      _seconds.value = 0;
      _phase.value = _Phase.recording;
      _ampSub = _rec.onAmplitudeChanged(const Duration(milliseconds: 90)).listen((a) {
        // dBFS (-60..0) → 0..1
        _level.value = ((a.current + 50) / 50).clamp(0.0, 1.0);
      });
      _timer = Timer.periodic(const Duration(seconds: 1), (_) {
        _seconds.value++;
        if (_seconds.value >= _maxSeconds) _finish();
      });
      _showOverlay();
    } catch (_) {
      if (mounted) showToast(context, '录音没能启动，请再试一次');
      _phase.value = _Phase.idle;
    } finally {
      _starting = false;
    }
  }

  Future<void> _finish() async {
    final phase = _phase.value;
    if (phase != _Phase.recording && phase != _Phase.cancelling) return;
    _ampSub?.cancel();
    _timer?.cancel();
    final elapsed = DateTime.now().difference(_started ?? DateTime.now());
    if (phase == _Phase.cancelling || elapsed < const Duration(milliseconds: 700)) {
      await _rec.cancel();
      _cleanup();
      _phase.value = _Phase.idle;
      if (phase == _Phase.recording && mounted) showToast(context, '说话时间太短了，请按住麦克风再说');
      return;
    }
    final path = await _rec.stop();
    _phase.value = _Phase.recognizing;
    try {
      final text = await ref.read(apiProvider).transcribe(path ?? _path!);
      _cleanup();
      _phase.value = _Phase.idle;
      if (text.trim().isEmpty) {
        if (mounted) showToast(context, '没听清，请再说一次');
      } else {
        widget.onText(text.trim());
      }
    } on ApiError catch (e) {
      _cleanup();
      _phase.value = _Phase.idle;
      if (mounted) showToast(context, e.message);
    } finally {
      if (path != null) File(path).delete().ignore();
    }
  }

  void _showOverlay() {
    _overlay = OverlayEntry(builder: (_) => _VoiceOverlay(phase: _phase, level: _level, seconds: _seconds, max: _maxSeconds));
    Overlay.of(context).insert(_overlay!);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: '按住说话',
      child: GestureDetector(
        onTap: () => showToast(context, '按住麦克风说话，松开就发送'),
        onLongPressStart: (_) => _start(),
        onLongPressMoveUpdate: (d) {
          if (_phase.value == _Phase.recording || _phase.value == _Phase.cancelling) {
            final cancel = d.localOffsetFromOrigin.dy < -80;
            final next = cancel ? _Phase.cancelling : _Phase.recording;
            if (next != _phase.value) {
              HapticFeedback.selectionClick();
              _phase.value = next;
            }
          }
        },
        onLongPressEnd: (_) => _finish(),
        onLongPressCancel: () => _finish(),
        child: ValueListenableBuilder(
          valueListenable: _phase,
          builder: (_, phase, _) => AnimatedContainer(
            duration: const Duration(milliseconds: 160),
            width: 46,
            height: 46,
            decoration: BoxDecoration(
              gradient: phase == _Phase.idle ? null : Palette.brandGradient,
              color: phase == _Phase.idle ? p.accentSoft : null,
              shape: BoxShape.circle,
            ),
            child: Icon(Icons.mic_rounded, size: 26, color: phase == _Phase.idle ? p.accent : Colors.white),
          ),
        ),
      ),
    );
  }
}

class _VoiceOverlay extends StatelessWidget {
  const _VoiceOverlay({required this.phase, required this.level, required this.seconds, required this.max});
  final ValueNotifier<_Phase> phase;
  final ValueNotifier<double> level;
  final ValueNotifier<int> seconds;
  final int max;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return IgnorePointer(
      child: Material(
        color: Colors.black.withValues(alpha: 0.45),
        child: Center(
          child: ValueListenableBuilder(
            valueListenable: phase,
            builder: (_, ph, _) {
              final cancelling = ph == _Phase.cancelling;
              final recognizing = ph == _Phase.recognizing;
              return Container(
                width: 260,
                padding: const EdgeInsets.fromLTRB(24, 30, 24, 24),
                decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(32)),
                child: Column(mainAxisSize: MainAxisSize.min, children: [
                  SizedBox(
                    width: 132,
                    height: 132,
                    child: recognizing
                        ? Center(child: SizedBox(width: 44, height: 44, child: CircularProgressIndicator(strokeWidth: 3.5, color: p.accent)))
                        : ValueListenableBuilder(
                            valueListenable: level,
                            builder: (_, l, _) => Stack(alignment: Alignment.center, children: [
                              AnimatedContainer(
                                duration: const Duration(milliseconds: 110),
                                width: 84 + 48 * l,
                                height: 84 + 48 * l,
                                decoration: BoxDecoration(shape: BoxShape.circle, color: (cancelling ? p.bad : p.accent).withValues(alpha: 0.18)),
                              ),
                              Container(
                                width: 84,
                                height: 84,
                                decoration: BoxDecoration(shape: BoxShape.circle, gradient: cancelling ? null : Palette.brandGradient, color: cancelling ? p.bad : null),
                                child: Icon(cancelling ? Icons.close_rounded : Icons.mic_rounded, color: Colors.white, size: 42),
                              ),
                            ]),
                          ),
                  ),
                  const SizedBox(height: 18),
                  Text(
                    recognizing ? '正在识别…' : (cancelling ? '松开手指，取消发送' : '正在听，松开就发送'),
                    style: TextStyle(fontSize: 19, fontWeight: FontWeight.w700, color: cancelling ? p.bad : p.text),
                  ),
                  const SizedBox(height: 8),
                  if (!recognizing)
                    ValueListenableBuilder(
                      valueListenable: seconds,
                      builder: (_, s, _) => Text(
                        s >= max - 10 ? '还能说 ${max - s} 秒' : '手指上滑可以取消',
                        style: TextStyle(fontSize: 15, color: p.muted),
                      ),
                    ),
                ]),
              );
            },
          ),
        ),
      ),
    );
  }
}

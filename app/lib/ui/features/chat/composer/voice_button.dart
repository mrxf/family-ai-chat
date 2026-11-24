import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

import '../../../../core/api.dart';
import '../../../../state/session.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import 'voice_overlay.dart';

/// Hold-to-talk microphone button (like WeChat): press down for instant tactile
/// feedback, hold to record, release to send immediately, slide up to cancel.
/// Supports both real-time WebSocket streaming ("边说边出字") and batch upload.
class VoiceButton extends ConsumerStatefulWidget {
  const VoiceButton({super.key, required this.onText});
  final ValueChanged<String> onText;

  @override
  ConsumerState<VoiceButton> createState() => _VoiceButtonState();
}

class _VoiceButtonState extends ConsumerState<VoiceButton> {
  final _rec = AudioRecorder();
  final _phase = ValueNotifier(VoicePhase.idle);
  final _level = ValueNotifier(0.0);
  final _seconds = ValueNotifier(0);
  final _liveText = ValueNotifier('');
  final _cancelProgress = ValueNotifier(0.0);

  OverlayEntry? _overlay;
  StreamSubscription<Amplitude>? _ampSub;
  StreamSubscription<Uint8List>? _pcmSub;
  WebSocket? _ws;
  StreamSubscription<dynamic>? _wsSub;
  Completer<void>? _wsDone;
  String? _streamErr;
  final _pcmOutBuf = BytesBuilder(copy: false);

  Timer? _timer;
  DateTime? _started;
  String? _path;
  bool _starting = false;
  bool _finishing = false;
  bool _releasedDuringStart = false;
  bool _hovered = false;
  bool _streamMode = false;
  int _sessionGen = 0;

  static const _maxSeconds = 60;
  static const _cancelDragDistance = 76.0;
  // Shorter than Flutter's default 500ms long-press so recording starts crisply.
  static const _holdDelay = Duration(milliseconds: 160);
  // 200ms of 16kHz 16-bit mono PCM = 6400 bytes.
  static const _pcmPacketBytes = 6400;

  @override
  void dispose() {
    _cleanup();
    _rec.dispose();
    _phase.dispose();
    _level.dispose();
    _seconds.dispose();
    _liveText.dispose();
    _cancelProgress.dispose();
    super.dispose();
  }

  void _cleanup() {
    _ampSub?.cancel();
    _ampSub = null;
    _pcmSub?.cancel();
    _pcmSub = null;
    _wsSub?.cancel();
    _wsSub = null;
    try {
      _ws?.close();
    } catch (_) {}
    _ws = null;
    _pcmOutBuf.clear();
    _timer?.cancel();
    _timer = null;
    _overlay?.remove();
    _overlay = null;
    _cancelProgress.value = 0;
  }

  double _pcmLevel(Uint8List bytes) {
    final samples = bytes.length ~/ 2;
    if (samples == 0) return 0;
    final bd = ByteData.sublistView(bytes);
    double sumSq = 0;
    final step = math.max(1, samples ~/ 160);
    int count = 0;
    for (var i = 0; i < samples; i += step) {
      final s = bd.getInt16(i * 2, Endian.little) / 32768.0;
      sumSq += s * s;
      count++;
    }
    final rms = math.sqrt(sumSq / math.max(1, count));
    if (rms <= 0.002) return 0;
    final db = 20 * (math.log(rms) / math.ln10);
    return ((db + 45) / 40).clamp(0.0, 1.0);
  }

  void _flushPcm({bool all = false}) {
    final ws = _ws;
    if (ws == null || ws.readyState != WebSocket.open) return;
    final total = _pcmOutBuf.length;
    if (total == 0) return;
    if (!all && total < _pcmPacketBytes) return;

    final data = _pcmOutBuf.takeBytes();
    var offset = 0;
    while (offset + _pcmPacketBytes <= data.length) {
      ws.add(Uint8List.sublistView(data, offset, offset + _pcmPacketBytes));
      offset += _pcmPacketBytes;
    }
    if (offset < data.length) {
      if (all) {
        ws.add(Uint8List.sublistView(data, offset));
      } else {
        _pcmOutBuf.add(Uint8List.sublistView(data, offset));
      }
    }
  }

  Future<void> _connectStreamWs(int gen) async {
    final api = ref.read(apiProvider);
    try {
      final ws = await WebSocket.connect(api.asrStreamUrl(), headers: api.authHeaders).timeout(const Duration(seconds: 10));
      if (!mounted || gen != _sessionGen || _phase.value == VoicePhase.idle) {
        await ws.close();
        return;
      }
      ws.pingInterval = const Duration(seconds: 15);
      _ws = ws;
      _wsSub = ws.listen(
        (raw) {
          if (gen != _sessionGen || raw is! String) return;
          try {
            final j = (jsonDecode(raw) as Map).cast<String, dynamic>();
            final type = j['type'] as String? ?? '';
            if (type == 'partial' || type == 'segment') {
              _liveText.value = (j['full_text'] as String?) ?? '';
            } else if (type == 'done') {
              final full = (j['full_text'] as String?) ?? '';
              if (full.isNotEmpty) _liveText.value = full;
              if (_wsDone != null && !_wsDone!.isCompleted) _wsDone!.complete();
            } else if (type == 'error') {
              _streamErr = (j['message'] as String?) ?? '语音识别出错';
              if (_wsDone != null && !_wsDone!.isCompleted) _wsDone!.complete();
            }
          } catch (_) {}
        },
        onError: (_) {
          _streamErr ??= '语音连接中断，请再试一次';
          if (_wsDone != null && !_wsDone!.isCompleted) _wsDone!.complete();
        },
        onDone: () {
          if (_wsDone != null && !_wsDone!.isCompleted) _wsDone!.complete();
        },
      );
      // Flush any initial syllables buffered while the WebSocket handshake was completing.
      _flushPcm();
    } catch (_) {
      if (gen != _sessionGen) return;
      _streamErr = '无法连接语音识别服务，请检查网络';
      if (_wsDone != null && !_wsDone!.isCompleted) _wsDone!.complete();
    }
  }

  void _onPressDown() {
    if (_starting || _finishing || _phase.value != VoicePhase.idle) return;
    _releasedDuringStart = false;
    // Instant tactile & visual response at 0ms so the button never feels inert.
    HapticFeedback.lightImpact();
    _phase.value = VoicePhase.pressing;
  }

  void _onPressCancelBeforeRecord() {
    if (_starting) {
      _releasedDuringStart = true;
      return;
    }
    if (_phase.value == VoicePhase.pressing) {
      _phase.value = VoicePhase.idle;
      showToast(context, '按住麦克风说话，松开就发送');
    }
  }

  Future<void> _start() async {
    if (_starting || _finishing || (_phase.value != VoicePhase.idle && _phase.value != VoicePhase.pressing)) return;
    _starting = true;
    _releasedDuringStart = false;
    final gen = ++_sessionGen;
    try {
      if (!await _rec.hasPermission()) {
        if (mounted) showToast(context, '需要麦克风权限才能说话，请在系统设置里允许');
        _phase.value = VoicePhase.idle;
        return;
      }
      if (!mounted || _releasedDuringStart) {
        _phase.value = VoicePhase.idle;
        return;
      }

      final mode = ref.read(sessionProvider).boot?.voiceMode ?? 'upload';
      _streamMode = mode == 'stream';
      _liveText.value = '';
      _level.value = 0;
      _seconds.value = 0;
      _cancelProgress.value = 0;
      _streamErr = null;
      _pcmOutBuf.clear();

      if (_streamMode) {
        _wsDone = Completer<void>();
        final stream = await _rec.startStream(const RecordConfig(
          encoder: AudioEncoder.pcm16bits,
          sampleRate: 16000,
          numChannels: 1,
        ));
        if (!mounted || _releasedDuringStart) {
          await _rec.cancel();
          _phase.value = VoicePhase.idle;
          return;
        }
        HapticFeedback.mediumImpact();
        _started = DateTime.now();
        _phase.value = VoicePhase.recording;
        _pcmSub = stream.listen((chunk) {
          if (gen != _sessionGen || chunk.isEmpty) return;
          _level.value = _level.value * 0.35 + _pcmLevel(chunk) * 0.65;
          if (_pcmOutBuf.length < 512 * 1024) {
            _pcmOutBuf.add(chunk);
          }
          _flushPcm();
        });
        unawaited(_connectStreamWs(gen));
      } else {
        final dir = await getTemporaryDirectory();
        _path = '${dir.path}/voice_${DateTime.now().millisecondsSinceEpoch}.m4a';
        await _rec.start(
          const RecordConfig(encoder: AudioEncoder.aacLc, sampleRate: 16000, numChannels: 1, bitRate: 48000),
          path: _path!,
        );
        if (!mounted || _releasedDuringStart) {
          await _rec.cancel();
          _phase.value = VoicePhase.idle;
          return;
        }
        HapticFeedback.mediumImpact();
        _started = DateTime.now();
        _phase.value = VoicePhase.recording;
        _ampSub = _rec.onAmplitudeChanged(const Duration(milliseconds: 90)).listen((a) {
          _level.value = ((a.current + 50) / 50).clamp(0.0, 1.0);
        });
      }

      _timer = Timer.periodic(const Duration(seconds: 1), (_) {
        _seconds.value++;
        if (_seconds.value >= _maxSeconds) _finish();
      });
      _showOverlay();
    } catch (_) {
      _cleanup();
      if (mounted) showToast(context, '录音没能启动，请再试一次');
      _phase.value = VoicePhase.idle;
    } finally {
      _starting = false;
    }
  }

  void _onMoveUpdate(LongPressMoveUpdateDetails details) {
    final ph = _phase.value;
    if (ph != VoicePhase.recording && ph != VoicePhase.cancelling) return;
    final dy = -details.localOffsetFromOrigin.dy;
    _cancelProgress.value = (dy / _cancelDragDistance).clamp(0.0, 1.0);
    final next = dy >= _cancelDragDistance ? VoicePhase.cancelling : VoicePhase.recording;
    if (next != ph) {
      if (next == VoicePhase.cancelling) {
        HapticFeedback.heavyImpact();
      } else {
        HapticFeedback.selectionClick();
      }
      _phase.value = next;
    }
  }

  Future<void> _finish() async {
    if (_starting) {
      _releasedDuringStart = true;
      return;
    }
    final phase = _phase.value;
    if (phase == VoicePhase.pressing) {
      _phase.value = VoicePhase.idle;
      return;
    }
    if (_finishing || (phase != VoicePhase.recording && phase != VoicePhase.cancelling)) return;
    _finishing = true;
    _ampSub?.cancel();
    _pcmSub?.cancel();
    _timer?.cancel();

    try {
      final elapsed = DateTime.now().difference(_started ?? DateTime.now());
      final tooShort = elapsed < const Duration(milliseconds: 550) && _liveText.value.trim().isEmpty;
      if (phase == VoicePhase.cancelling || tooShort) {
        HapticFeedback.selectionClick();
        _sessionGen++;
        await _rec.cancel();
        _cleanup();
        _phase.value = VoicePhase.idle;
        if (phase == VoicePhase.recording && mounted) showToast(context, '说话时间太短了，请按住麦克风再说');
        return;
      }

      HapticFeedback.lightImpact();

      if (_streamMode) {
        await _rec.stop();
        _phase.value = VoicePhase.recognizing;

        // If user spoke very briefly while WS was still connecting, give handshake a brief moment.
        final deadline = DateTime.now().add(const Duration(seconds: 2));
        while (_ws == null && _streamErr == null && DateTime.now().isBefore(deadline)) {
          await Future<void>.delayed(const Duration(milliseconds: 40));
        }

        final ws = _ws;
        if (ws != null && ws.readyState == WebSocket.open) {
          try {
            _flushPcm(all: true);
            ws.add(jsonEncode({'type': 'end'}));
            await _wsDone?.future.timeout(const Duration(milliseconds: 3500));
          } catch (_) {}
        }

        final text = _liveText.value.trim();
        final err = _streamErr;
        _sessionGen++;
        _cleanup();
        _phase.value = VoicePhase.idle;

        if (text.isNotEmpty) {
          widget.onText(text);
        } else if (err != null) {
          if (mounted) showToast(context, err);
        } else {
          if (mounted) showToast(context, '没听清，请再说一次');
        }
        return;
      }

      // Upload mode (OpenAI-compatible /audio/transcriptions).
      final path = await _rec.stop();
      _phase.value = VoicePhase.recognizing;
      try {
        final text = await ref.read(apiProvider).transcribe(path ?? _path!);
        _cleanup();
        _phase.value = VoicePhase.idle;
        if (text.trim().isEmpty) {
          if (mounted) showToast(context, '没听清，请再说一次');
        } else {
          widget.onText(text.trim());
        }
      } on ApiError catch (e) {
        _cleanup();
        _phase.value = VoicePhase.idle;
        if (mounted) showToast(context, e.message);
      } finally {
        if (path != null) File(path).delete().ignore();
      }
    } finally {
      _finishing = false;
    }
  }

  void _showOverlay() {
    _overlay?.remove();
    _overlay = OverlayEntry(
      builder: (_) => VoiceOverlay(
        phase: _phase,
        level: _level,
        seconds: _seconds,
        liveText: _liveText,
        cancelProgress: _cancelProgress,
        streamMode: _streamMode,
        maxSeconds: _maxSeconds,
      ),
    );
    Overlay.of(context).insert(_overlay!);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;

    return Tooltip(
      message: '按住说话，上滑取消',
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: RawGestureDetector(
          behavior: HitTestBehavior.opaque,
          gestures: {
            LongPressGestureRecognizer: GestureRecognizerFactoryWithHandlers<LongPressGestureRecognizer>(
              () => LongPressGestureRecognizer(duration: _holdDelay),
              (instance) {
                instance.onLongPressDown = (_) => _onPressDown();
                instance.onLongPressCancel = _onPressCancelBeforeRecord;
                instance.onLongPressStart = (_) => _start();
                instance.onLongPressMoveUpdate = _onMoveUpdate;
                instance.onLongPressEnd = (_) => _finish();
              },
            ),
          },
          child: AnimatedBuilder(
            animation: Listenable.merge([_phase, _level]),
            builder: (_, _) {
              final phase = _phase.value;
              final active = phase != VoicePhase.idle;
              final pressing = phase == VoicePhase.pressing;
              final cancelling = phase == VoicePhase.cancelling;
              final recognizing = phase == VoicePhase.recognizing;

              final scale = switch (phase) {
                VoicePhase.pressing => 0.88,
                VoicePhase.recording => 1.08 + 0.04 * _level.value,
                VoicePhase.cancelling => 0.96,
                VoicePhase.recognizing => 1.0,
                VoicePhase.idle => _hovered ? 1.05 : 1.0,
              };

              final haloColor = cancelling ? p.bad : p.accent;
              final haloAlpha = switch (phase) {
                VoicePhase.recording => 0.20 + 0.16 * _level.value,
                VoicePhase.pressing => 0.16,
                VoicePhase.cancelling => 0.22,
                _ => 0.0,
              };
              final haloSize = switch (phase) {
                VoicePhase.recording => 54.0 + 12.0 * _level.value,
                VoicePhase.pressing => 52.0,
                VoicePhase.cancelling => 54.0,
                _ => 46.0,
              };

              return SizedBox(
                width: 46,
                height: 46,
                child: Stack(
                  clipBehavior: Clip.none,
                  alignment: Alignment.center,
                  children: [
                    IgnorePointer(
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 110),
                        curve: Curves.easeOutCubic,
                        width: haloSize,
                        height: haloSize,
                        decoration: BoxDecoration(
                          shape: BoxShape.circle,
                          color: haloColor.withValues(alpha: haloAlpha),
                        ),
                      ),
                    ),
                    AnimatedScale(
                      scale: scale,
                      duration: Duration(milliseconds: pressing ? 90 : 150),
                      curve: Curves.easeOutCubic,
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 150),
                        curve: Curves.easeOutCubic,
                        width: 46,
                        height: 46,
                        decoration: BoxDecoration(
                          gradient: active && !cancelling ? Palette.brandGradient : null,
                          color: cancelling
                              ? p.bad
                              : (active
                                  ? null
                                  : (_hovered ? p.accent.withValues(alpha: 0.18) : p.accentSoft)),
                          shape: BoxShape.circle,
                          border: active
                              ? null
                              : Border.all(
                                  color: p.accent.withValues(alpha: _hovered ? 0.38 : 0.20),
                                ),
                          boxShadow: active
                              ? [
                                  BoxShadow(
                                    color: haloColor.withValues(alpha: 0.38),
                                    blurRadius: 14,
                                    offset: const Offset(0, 4),
                                  ),
                                ]
                              : null,
                        ),
                        child: Center(
                          child: recognizing
                              ? const SizedBox(
                                  width: 20,
                                  height: 20,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2.2,
                                    color: Colors.white,
                                  ),
                                )
                              : AppIcon(
                                  cancelling ? AppIcons.close : AppIcons.mic,
                                  size: 23,
                                  color: active ? Colors.white : p.accent,
                                ),
                        ),
                      ),
                    ),
                  ],
                ),
              );
            },
          ),
        ),
      ),
    );
  }
}

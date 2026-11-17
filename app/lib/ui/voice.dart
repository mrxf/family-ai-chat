import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;
import 'dart:typed_data';

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
/// send immediately, slide up to cancel. Supports both real-time WebSocket
/// streaming ("边说边出字") and batch upload transcription.
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
  final _liveText = ValueNotifier('');

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
  bool _streamMode = false;
  int _sessionGen = 0;

  static const _maxSeconds = 60;
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
      if (!mounted || gen != _sessionGen || _phase.value == _Phase.idle) {
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

  Future<void> _start() async {
    if (_starting || _finishing || _phase.value != _Phase.idle) return;
    _starting = true;
    final gen = ++_sessionGen;
    try {
      if (!await _rec.hasPermission()) {
        if (mounted) showToast(context, '需要麦克风权限才能说话，请在系统设置里允许');
        return;
      }
      final mode = ref.read(sessionProvider).boot?.voiceMode ?? 'upload';
      _streamMode = mode == 'stream';
      _liveText.value = '';
      _level.value = 0;
      _seconds.value = 0;
      _streamErr = null;
      _pcmOutBuf.clear();

      if (_streamMode) {
        _wsDone = Completer<void>();
        final stream = await _rec.startStream(const RecordConfig(
          encoder: AudioEncoder.pcm16bits,
          sampleRate: 16000,
          numChannels: 1,
        ));
        HapticFeedback.mediumImpact();
        _started = DateTime.now();
        _phase.value = _Phase.recording;
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
        HapticFeedback.mediumImpact();
        _started = DateTime.now();
        _phase.value = _Phase.recording;
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
      _phase.value = _Phase.idle;
    } finally {
      _starting = false;
    }
  }

  Future<void> _finish() async {
    final phase = _phase.value;
    if (_finishing || (phase != _Phase.recording && phase != _Phase.cancelling)) return;
    _finishing = true;
    _ampSub?.cancel();
    _pcmSub?.cancel();
    _timer?.cancel();

    try {
      final elapsed = DateTime.now().difference(_started ?? DateTime.now());
      final tooShort = elapsed < const Duration(milliseconds: 650) && _liveText.value.trim().isEmpty;
      if (phase == _Phase.cancelling || tooShort) {
        _sessionGen++;
        await _rec.cancel();
        _cleanup();
        _phase.value = _Phase.idle;
        if (phase == _Phase.recording && mounted) showToast(context, '说话时间太短了，请按住麦克风再说');
        return;
      }

      if (_streamMode) {
        await _rec.stop();
        _phase.value = _Phase.recognizing;

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
        _phase.value = _Phase.idle;

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
    } finally {
      _finishing = false;
    }
  }

  void _showOverlay() {
    _overlay = OverlayEntry(
      builder: (_) => _VoiceOverlay(
        phase: _phase,
        level: _level,
        seconds: _seconds,
        liveText: _liveText,
        streamMode: _streamMode,
        max: _maxSeconds,
      ),
    );
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
  const _VoiceOverlay({
    required this.phase,
    required this.level,
    required this.seconds,
    required this.liveText,
    required this.streamMode,
    required this.max,
  });

  final ValueNotifier<_Phase> phase;
  final ValueNotifier<double> level;
  final ValueNotifier<int> seconds;
  final ValueNotifier<String> liveText;
  final bool streamMode;
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
                width: streamMode ? 300 : 260,
                padding: const EdgeInsets.fromLTRB(24, 28, 24, 24),
                decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(32)),
                child: Column(mainAxisSize: MainAxisSize.min, children: [
                  SizedBox(
                    width: 124,
                    height: 124,
                    child: recognizing
                        ? Center(child: SizedBox(width: 44, height: 44, child: CircularProgressIndicator(strokeWidth: 3.5, color: p.accent)))
                        : ValueListenableBuilder(
                            valueListenable: level,
                            builder: (_, l, _) => Stack(alignment: Alignment.center, children: [
                              AnimatedContainer(
                                duration: const Duration(milliseconds: 100),
                                width: 82 + 42 * l,
                                height: 82 + 42 * l,
                                decoration: BoxDecoration(shape: BoxShape.circle, color: (cancelling ? p.bad : p.accent).withValues(alpha: 0.18)),
                              ),
                              Container(
                                width: 82,
                                height: 82,
                                decoration: BoxDecoration(shape: BoxShape.circle, gradient: cancelling ? null : Palette.brandGradient, color: cancelling ? p.bad : null),
                                child: Icon(cancelling ? Icons.close_rounded : Icons.mic_rounded, color: Colors.white, size: 40),
                              ),
                            ]),
                          ),
                  ),
                  if (streamMode)
                    ValueListenableBuilder(
                      valueListenable: liveText,
                      builder: (_, txt, _) {
                        if (txt.isEmpty) return const SizedBox(height: 14);
                        return Container(
                          width: double.infinity,
                          margin: const EdgeInsets.only(top: 12, bottom: 14),
                          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                          decoration: BoxDecoration(
                            color: p.surfaceAlt,
                            borderRadius: BorderRadius.circular(16),
                          ),
                          child: Text(
                            txt,
                            maxLines: 4,
                            overflow: TextOverflow.ellipsis,
                            textAlign: TextAlign.center,
                            style: TextStyle(
                              fontSize: 16.5,
                              height: 1.45,
                              fontWeight: FontWeight.w500,
                              color: cancelling ? p.muted : p.text,
                              decoration: cancelling ? TextDecoration.lineThrough : null,
                            ),
                          ),
                        );
                      },
                    )
                  else
                    const SizedBox(height: 18),
                  Text(
                    recognizing ? '正在发送…' : (cancelling ? '松开手指，取消发送' : '正在听，松开就发送'),
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

import 'package:flutter/foundation.dart';
import 'package:flutter_tts/flutter_tts.dart';

/// Reads assistant replies aloud with the system TTS engine.
class Speaker {
  Speaker._();
  static final instance = Speaker._();

  final _tts = FlutterTts();
  bool _ready = false;

  /// Id of the message currently being read (null when idle).
  final speaking = ValueNotifier<String?>(null);

  Future<void> _init() async {
    if (_ready) return;
    _ready = true;
    try {
      await _tts.setLanguage('zh-CN');
      await _tts.awaitSpeakCompletion(false);
      _tts.setCompletionHandler(() => speaking.value = null);
      _tts.setCancelHandler(() => speaking.value = null);
      _tts.setErrorHandler((_) => speaking.value = null);
    } catch (_) {}
  }

  Future<bool> toggle(String id, String markdown, {double rate = 0.5}) async {
    await _init();
    if (speaking.value == id) {
      await stop();
      return true;
    }
    await _tts.stop();
    final text = plainText(markdown);
    if (text.isEmpty) return true;
    try {
      await _tts.setSpeechRate(rate);
      speaking.value = id;
      final r = await _tts.speak(text);
      if (r != 1) {
        speaking.value = null;
        return false;
      }
      return true;
    } catch (_) {
      speaking.value = null;
      return false;
    }
  }

  Future<void> stop() async {
    speaking.value = null;
    try {
      await _tts.stop();
    } catch (_) {}
  }

  /// Strips Markdown syntax so the engine doesn't read symbols aloud.
  static String plainText(String md) {
    var s = md;
    s = s.replaceAll(RegExp(r'```[\s\S]*?```'), '（代码略）');
    s = s.replaceAll(RegExp(r'\$\$[\s\S]*?\$\$'), '（公式略）');
    s = s.replaceAllMapped(RegExp(r'!\[[^\]]*\]\([^)]*\)'), (_) => '');
    s = s.replaceAllMapped(RegExp(r'\[([^\]]+)\]\([^)]*\)'), (m) => m[1]!);
    s = s.replaceAll(RegExp(r'^\s{0,3}#{1,6}\s*', multiLine: true), '');
    s = s.replaceAll(RegExp(r'^\s*>\s?', multiLine: true), '');
    s = s.replaceAll(RegExp(r'^\s*[-*+]\s+', multiLine: true), '');
    s = s.replaceAll(RegExp(r'^\s*\|?\s*:?-{3,}.*$', multiLine: true), '');
    s = s.replaceAll('|', '，');
    s = s.replaceAll(RegExp(r'[*_`~]'), '');
    s = s.replaceAll(RegExp(r'\n{2,}'), '\n');
    return s.trim();
  }
}

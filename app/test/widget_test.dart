import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/platform/speech/speaker.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('normalizeServer adds scheme and trims slashes', () {
    expect(normalizeServer('192.168.1.5:8080/'), 'http://192.168.1.5:8080');
    expect(normalizeServer('chat.example.com'), 'https://chat.example.com');
    expect(normalizeServer(' https://a.b/ '), 'https://a.b');
  });

  test('asrStreamUrl maps http/https to ws/wss', () {
    expect(Api('http://192.168.1.5:8080').asrStreamUrl(), 'ws://192.168.1.5:8080/api/asr/stream');
    expect(Api('https://chat.example.com').asrStreamUrl(), 'wss://chat.example.com/api/asr/stream');
  });

  test('Bootstrap parses voice_input and voice_mode gating', () {
    final off = Bootstrap.fromJson({
      'user': {'id': 'u1', 'username': 'a', 'display_name': 'A', 'role': 'member'},
      'features': {'voice_input': false, 'voice_mode': ''},
    });
    expect(off.voiceInput, isFalse);
    expect(off.voiceMode, '');

    final streamOn = Bootstrap.fromJson({
      'user': {'id': 'u1', 'username': 'a', 'display_name': 'A', 'role': 'member'},
      'features': {'voice_input': true, 'voice_mode': 'stream'},
    });
    expect(streamOn.voiceInput, isTrue);
    expect(streamOn.voiceMode, 'stream');
  });

  test('plainText strips markdown for TTS', () {
    expect(Speaker.plainText('# 标题\n**加粗** 和 `代码`\n- 列表'), '标题\n加粗 和 代码\n列表');
  });
}

import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/tts.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('normalizeServer adds scheme and trims slashes', () {
    expect(normalizeServer('192.168.1.5:8080/'), 'http://192.168.1.5:8080');
    expect(normalizeServer('chat.example.com'), 'https://chat.example.com');
    expect(normalizeServer(' https://a.b/ '), 'https://a.b');
  });

  test('plainText strips markdown for TTS', () {
    expect(Speaker.plainText('# 标题\n**加粗** 和 `代码`\n- 列表'), '标题\n加粗 和 代码\n列表');
  });
}

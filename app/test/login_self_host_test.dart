import 'dart:convert';

import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/invite_code.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/app/app.dart';
import 'package:family_chat/state/session.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemStorage implements Storage {
  _MemStorage(this.prefs);

  @override
  final SharedPreferences prefs;

  String? token;
  final Map<String, Object?> caches = {};

  @override
  Future<void> clearCache() async => caches.clear();

  @override
  Future<void> deleteCache(String key) async => caches.remove(key);

  @override
  Future<dynamic> readCache(String key) async => caches[key];

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeCache(String key, Object? value) async => caches[key] = value;

  @override
  Future<void> writeToken(String? value) async => token = value;
}

class _FakeServerCluster {
  final List<({String baseUrl, String username, String password, String device})> logins = [];
  final Map<String, bool> reachable = {
    kDefaultServer: true,
    'https://chat.family-nas.com': true,
    'https://nas.home.example.com': true,
  };
  final Set<String> needsSetupServers = {};

  Api create(String baseUrl, {String? token, void Function()? onUnauthorized}) {
    return _FakeServerApi(this, baseUrl, token: token, onUnauthorized: onUnauthorized);
  }
}

class _FakeServerApi extends Api {
  _FakeServerApi(this._cluster, super.baseUrl, {super.token, super.onUnauthorized});
  final _FakeServerCluster _cluster;

  @override
  Future<bool> ping() async => _cluster.reachable[baseUrl] ?? false;

  @override
  Future<bool> needsSetup() async => _cluster.needsSetupServers.contains(baseUrl);

  @override
  Future<(String, User)> login(String username, String password, String device) async {
    _cluster.logins.add((baseUrl: baseUrl, username: username, password: password, device: device));
    if (password == 'wrong') {
      throw const ApiError(401, 'bad_credentials', '用户名或密码不正确');
    }
    final user = User(
      id: 'u-$username',
      username: username,
      displayName: username == 'mama' ? '妈妈' : '小暖家人',
      avatar: '',
      role: username == 'admin' ? 'admin' : 'member',
    );
    return ('tok-$username', user);
  }

  @override
  Future<void> logout() async {}

  @override
  Future<Bootstrap> bootstrap() async {
    final uname = (token ?? 'tok-user').replaceFirst('tok-', '');
    return Bootstrap(
      user: User(
        id: 'u-$uname',
        username: uname,
        displayName: uname == 'mama' ? '妈妈' : '小暖家人',
        avatar: '',
        role: uname == 'admin' ? 'admin' : 'member',
      ),
      models: const [
        ChatModel(id: 'm1', displayName: '日常模型', description: '', vision: true, think: false),
      ],
      defaultModelId: 'm1',
      assistants: const [],
      voiceInput: false,
      voiceMode: '',
      imageGeneration: false,
      imagesLeft: -1,
      autoModel: false,
    );
  }

  @override
  Future<List<Conversation>> conversations({int? before, String? q, int limit = 50}) async => const [];

  @override
  Future<Map<String, dynamic>> appVersion() async => const {'available': false};
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  String clipboardText = '';

  setUp(() {
    clipboardText = '';
    InviteCodeGuard.clearLocalMarks();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        switch (call.method) {
          case 'Clipboard.getData':
            return <String, dynamic>{'text': clipboardText};
          case 'Clipboard.setData':
            final args = call.arguments as Map?;
            clipboardText = (args?['text'] as String?) ?? '';
            return null;
          case 'Clipboard.hasStrings':
            return <String, dynamic>{'value': clipboardText.isNotEmpty};
          default:
            return null;
        }
      },
    );
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      null,
    );
  });

  group('XnInviteCodec & ChaCha20', () {
    test('matches RFC 8439 Section 2.4.2 official ChaCha20 test vector', () {
      final key = Uint8List.fromList(List<int>.generate(32, (i) => i));
      final nonce = Uint8List.fromList([0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4a, 0x00, 0x00, 0x00, 0x00]);
      const sunscreen = "Ladies and Gentlemen of the class of '99: If I could offer you only one tip for the future, sunscreen would be it.";
      final cipher = chacha20Xor(
        key: key,
        nonce: nonce,
        counter: 1,
        input: Uint8List.fromList(utf8.encode(sunscreen)),
      );
      const expectedHex =
          '6e2e359a2568f98041ba0728dd0d6981e97e7aec1d4360c20a27afccfd9fae0bf91b65c5524733ab8f593dabcd62b3571639d624e65152ab8f530c359f0861d807ca0dbf500d6a6156a38e088a22b65e52bc514d16ccf806818ce91ab77937365af90bbf74a35be6b40b8eedf2785e42874d';
      final gotHex = cipher.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
      expect(gotHex, expectedHex);
    });

    test('encodes default-server credentials compactly with randomized nonces and zero plaintext leak', () {
      final payload = InvitePayload(
        server: kDefaultServer,
        username: 'mama',
        password: 'SecretPass88',
        displayName: '妈妈',
        issuedAt: 1795000000,
      );
      final code1 = XnInviteCodec.encode(payload);
      final code2 = XnInviteCodec.encode(payload);

      expect(code1, startsWith('#xn'));
      expect(code1, endsWith('xn#'));
      expect(code1, isNot(equals(code2)));
      expect(code1.length, lessThan(140));
      expect(code1.contains('mama'), isFalse);
      expect(code1.contains('SecretPass88'), isFalse);
      expect(code1.contains('/'), isFalse);
      expect(code1.contains('+'), isFalse);
      expect(code1.contains('='), isFalse);

      final d1 = XnInviteCodec.decode(code1);
      final d2 = XnInviteCodec.decode(code2);
      expect(d1.server, kDefaultServer);
      expect(d1.isSelfHosted, isFalse);
      expect(d1.username, 'mama');
      expect(d1.password, 'SecretPass88');
      expect(d1.displayName, '妈妈');
      expect(d2.password, 'SecretPass88');
    });

    test('extracts #xn...xn# from multi-line WeChat message even with line wraps, and rejects tampering', () {
      final payload = InvitePayload(
        server: 'nas.home.example.com',
        username: 'baba',
        password: 'p9x4m2k7',
        displayName: '老爸',
      );
      final rawCode = XnInviteCodec.encode(payload);
      // Simulate WeChat inserting a newline + zero-width space in the middle of the token.
      final mid = rawCode.length ~/ 2;
      final wrapped = '${rawCode.substring(0, mid)}\n\u200B${rawCode.substring(mid)}';
      final wechatMsg = '爸，复制整段话打开小暖就能直接用：\n$wrapped\n不用输密码！';

      final ins = XnInviteCodec.inspectText(wechatMsg);
      expect(ins.hasInvite, isTrue);
      expect(ins.payload!.server, 'https://nas.home.example.com');
      expect(ins.payload!.isSelfHosted, isTrue);
      expect(ins.payload!.username, 'baba');
      expect(ins.payload!.password, 'p9x4m2k7');
      expect(ins.payload!.displayName, '老爸');

      // Flip a character inside the body -> MAC authentication must reject it.
      final bodyIdx = 8;
      final flippedChar = rawCode[bodyIdx] == 'A' ? 'B' : 'A';
      final tampered = '${rawCode.substring(0, bodyIdx)}$flippedChar${rawCode.substring(bodyIdx + 1)}';
      final bad = XnInviteCodec.inspectText(tampered);
      expect(bad.hasInvite, isFalse);
      expect(bad.hasError, isTrue);
      expect(bad.error!.kind, InviteErrorKind.corrupted);

      // Truncated token missing closing xn#.
      final half = rawCode.substring(0, rawCode.length - 6);
      final trunc = XnInviteCodec.inspectText('这是半截：$half');
      expect(trunc.hasError, isTrue);
      expect(trunc.error!.kind, InviteErrorKind.truncated);
    });
  });

  group('Login & Self-Hosted UI flows', () {
    Future<void> pumpFrames(WidgetTester tester, [int count = 6]) async {
      for (var i = 0; i < count; i++) {
        await tester.pump(const Duration(milliseconds: 120));
      }
    }

    testWidgets('defaults to https://xiaonuan.miaojiai.com with zero server input field', (tester) async {
      SharedPreferences.setMockInitialValues({});
      final prefs = await SharedPreferences.getInstance();
      final storage = _MemStorage(prefs);
      final cluster = _FakeServerCluster();

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            storageProvider.overrideWithValue(storage),
            apiFactoryProvider.overrideWithValue(cluster.create),
          ],
          child: const NuanApp(),
        ),
      );
      await pumpFrames(tester);

      // Only 2 TextFields on default LoginPage: username and password (no server URL field!).
      expect(find.byType(TextField), findsNWidgets(2));
      expect(find.text('服务器地址'), findsNothing);
      expect(find.text('自托管服务器'), findsOneWidget);
      expect(find.text('粘贴登录口令'), findsOneWidget);

      await tester.enterText(find.byType(TextField).at(0), 'mama');
      await tester.enterText(find.byType(TextField).at(1), 'pass1234');
      await tester.tap(find.text('登 录'));
      await pumpFrames(tester);
      // Drain HomePage's 2-second silent update timer.
      await tester.pump(const Duration(seconds: 3));

      expect(cluster.logins.length, 1);
      expect(cluster.logins.single.baseUrl, 'https://xiaonuan.miaojiai.com');
      expect(cluster.logins.single.username, 'mama');
      // On official default server, Welcome does NOT show the self-hosted pill.
      expect(find.textContaining('自托管 ·'), findsNothing);
    });

    testWidgets('configuring self-hosted server shows capability on LoginPage and on Home Welcome', (tester) async {
      SharedPreferences.setMockInitialValues({});
      final prefs = await SharedPreferences.getInstance();
      final storage = _MemStorage(prefs);
      final cluster = _FakeServerCluster();

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            storageProvider.overrideWithValue(storage),
            apiFactoryProvider.overrideWithValue(cluster.create),
          ],
          child: const NuanApp(),
        ),
      );
      await pumpFrames(tester);

      // Open self-hosted config screen from LoginPage.
      await tester.tap(find.text('自托管服务器'));
      await pumpFrames(tester);
      expect(find.text('服务器与自托管'), findsOneWidget);

      // Select 自托管服务器 option card.
      await tester.tap(find.text('自托管服务器'));
      await pumpFrames(tester);

      await tester.enterText(find.byType(TextField).first, 'chat.family-nas.com');
      await pumpFrames(tester, 2);

      await tester.tap(find.text('测试连接'));
      await pumpFrames(tester);
      expect(find.textContaining('连接成功'), findsOneWidget);

      await tester.tap(find.text('保存自托管配置'));
      await pumpFrames(tester);

      // Back on LoginPage: self-hosted capability card is displayed with host & quick actions!
      expect(find.text('chat.family-nas.com'), findsOneWidget);
      expect(find.text('已连通'), findsOneWidget);
      expect(find.text('生成家人口令'), findsOneWidget);

      // Log in to the self-hosted server and verify Home Welcome displays the self-hosted capability pill.
      await tester.enterText(find.byType(TextField).at(0), 'admin');
      await tester.enterText(find.byType(TextField).at(1), 'admin1234');
      await tester.ensureVisible(find.text('登 录'));
      await pumpFrames(tester, 2);
      await tester.tap(find.text('登 录'));
      await pumpFrames(tester);
      await tester.pump(const Duration(seconds: 3));

      expect(cluster.logins.single.baseUrl, 'https://chat.family-nas.com');
      expect(find.text('自托管 · chat.family-nas.com'), findsOneWidget);

      // Tapping the self-hosted pill on Home opens the self-hosted info & invite sheet.
      await tester.tap(find.text('自托管 · chat.family-nas.com'));
      await pumpFrames(tester);
      expect(find.text('自托管服务器已连接'), findsOneWidget);
      expect(find.text('生成家人免输登录口令'), findsOneWidget);
    });

    testWidgets('auto-detects #xn...xn# in clipboard on LoginPage, pops auto-login dialog, logs in & wipes clipboard', (tester) async {
      SharedPreferences.setMockInitialValues({});
      final prefs = await SharedPreferences.getInstance();
      final storage = _MemStorage(prefs);
      final cluster = _FakeServerCluster();

      // Child generates an encrypted #xn...xn# WeChat message on ANOTHER phone (so clear local marks).
      final msg = XnInviteCodec.formatShareMessage(
        InvitePayload(
          server: 'nas.home.example.com',
          username: 'mama',
          password: 'MomAutoLogin99',
          displayName: '妈妈',
        ),
      );
      InviteCodeGuard.clearLocalMarks();
      clipboardText = msg;

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            storageProvider.overrideWithValue(storage),
            apiFactoryProvider.overrideWithValue(cluster.create),
          ],
          child: const NuanApp(),
        ),
      );
      await pumpFrames(tester);

      // Auto-login dialog pops up automatically without typing a single character!
      expect(find.text('你好，妈妈！'), findsOneWidget);
      expect(find.text('mama'), findsOneWidget);
      expect(find.text('自托管 · nas.home.example.com'), findsOneWidget);
      expect(find.text('立即自动登录'), findsOneWidget);

      await tester.tap(find.text('立即自动登录'));
      await pumpFrames(tester);
      await tester.pump(const Duration(seconds: 3));

      // Logged into the self-hosted server as mama, and clipboard is wiped clean!
      expect(cluster.logins.length, 1);
      expect(cluster.logins.single.baseUrl, 'https://nas.home.example.com');
      expect(cluster.logins.single.username, 'mama');
      expect(cluster.logins.single.password, 'MomAutoLogin99');
      expect(clipboardText, isEmpty);
      expect(find.text('自托管 · nas.home.example.com'), findsOneWidget);
    });
  });
}

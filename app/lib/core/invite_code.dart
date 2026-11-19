import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart' show normalizeServer;

/// Official default server so ordinary family users never need to type a URL.
const kDefaultServer = 'https://xiaonuan.miaojiai.com';

/// Resolves an optional/saved server string, falling back to [kDefaultServer].
String effectiveServer(String? raw) {
  final s = normalizeServer(raw ?? '');
  return s.isEmpty ? kDefaultServer : s;
}

/// Whether [raw] points to a custom self-hosted server rather than [kDefaultServer].
bool isSelfHostedServer(String? raw) => effectiveServer(raw) != kDefaultServer;

/// Human-friendly host[:port] without the scheme prefix.
String displayHost(String? raw) => effectiveServer(raw).replaceFirst(RegExp(r'^https?://'), '');

/// Validates that [raw] normalizes to a well-formed http/https server base URL.
bool isValidServerUrl(String raw) {
  final norm = normalizeServer(raw);
  if (norm.isEmpty || RegExp(r'\s').hasMatch(norm)) return false;
  final uri = Uri.tryParse(norm);
  if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) return false;
  if (uri.host.isEmpty || uri.userInfo.isNotEmpty) return false;
  return true;
}

enum InviteErrorKind { truncated, corrupted, unsupportedVersion, invalidPayload }

class InviteCodeException implements Exception {
  final InviteErrorKind kind;
  final String message;
  const InviteCodeException(this.kind, this.message);

  @override
  String toString() => message;
}

/// Decoded configuration carried inside a `#xn...xn#` invite code.
@immutable
class InvitePayload {
  final String server;
  final String username;
  final String password;
  final String displayName;
  final int issuedAt;

  InvitePayload({
    String? server,
    this.username = '',
    this.password = '',
    this.displayName = '',
    int? issuedAt,
  })  : server = effectiveServer(server),
        issuedAt = issuedAt ?? (DateTime.now().millisecondsSinceEpoch ~/ 1000);

  bool get isSelfHosted => isSelfHostedServer(server);
  bool get hasCredentials => username.trim().isNotEmpty && password.isNotEmpty;
  String get serverHost => displayHost(server);
}

/// Outcome of scanning clipboard or shared text for `#xn...xn#`.
@immutable
class InviteInspection {
  final InvitePayload? payload;
  final String? rawToken;
  final String? fingerprint;
  final InviteCodeException? error;

  const InviteInspection._({this.payload, this.rawToken, this.fingerprint, this.error});
  const InviteInspection.none() : this._();

  bool get hasInvite => payload != null;
  bool get hasError => error != null;
}

/// App-exclusive authenticated symmetric codec (`#xn<payload>xn#`).
///
/// Frame v1: `Header(1B = 0x01) || Nonce(12B) || ChaCha20(UTF8 JSON) || HMAC-SHA256-128(16B)`
/// encoded as unpadded Base64Url so WeChat/SMS never split or linkify it.
abstract final class XnInviteCodec {
  static const prefix = '#xn';
  static const suffix = 'xn#';
  static const _version = 0x01;
  static const _nonceLen = 12;
  static const _tagLen = 16;
  static const _maxFrameBytes = 2048;

  static final _fullTokenRe = RegExp(r'#xn([^#]+?)xn#');
  static final _danglingOpenRe = RegExp(r'#xn[A-Za-z0-9_\-+/\s]{8,}$');

  // Domain-separated 256-bit app key derived via SHA-256 over app-specific material.
  static final Uint8List _rootKey = () {
    const parts = [
      'xiaonuan.miaojiai.com',
      'family-ai-chat/xn-invite-codec/v1',
      'Nuan2026#SymmetricAEAD*WarmHome',
    ];
    return Uint8List.fromList(sha256.convert(utf8.encode(parts.join('|'))).bytes);
  }();

  static Uint8List _deriveKey(String purpose, Uint8List nonce) {
    final mac = Hmac(sha256, _rootKey);
    return Uint8List.fromList(mac.convert(<int>[...utf8.encode(purpose), ...nonce]).bytes);
  }

  /// Encrypts [payload] into a `#xn...xn#` token.
  static String encode(InvitePayload payload, {@visibleForTesting Uint8List? debugNonce}) {
    final user = payload.username.trim();
    final pass = payload.password;
    final name = payload.displayName.trim();
    if ((user.isEmpty || pass.isEmpty) && !payload.isSelfHosted) {
      throw const InviteCodeException(
        InviteErrorKind.invalidPayload,
        '请填写家人的用户名和密码，或先配置自托管服务器地址',
      );
    }
    if (!isValidServerUrl(payload.server)) {
      throw const InviteCodeException(InviteErrorKind.invalidPayload, '服务器地址格式不正确');
    }

    final jsonMap = <String, Object>{
      if (payload.isSelfHosted) 's': payload.server,
      if (user.isNotEmpty) 'u': user,
      if (pass.isNotEmpty) 'p': pass,
      if (name.isNotEmpty) 'n': name,
      't': payload.issuedAt,
    };
    final plain = Uint8List.fromList(utf8.encode(jsonEncode(jsonMap)));

    final nonce = Uint8List(_nonceLen);
    if (debugNonce != null) {
      if (debugNonce.length != _nonceLen) {
        throw ArgumentError.value(debugNonce.length, 'debugNonce', 'Must be $_nonceLen bytes');
      }
      nonce.setAll(0, debugNonce);
    } else {
      final rng = Random.secure();
      for (var i = 0; i < _nonceLen; i++) {
        nonce[i] = rng.nextInt(256);
      }
    }

    final encKey = _deriveKey('xn-invite-v1/enc', nonce);
    final macKey = _deriveKey('xn-invite-v1/mac', nonce);
    final cipher = chacha20Xor(key: encKey, nonce: nonce, counter: 1, input: plain);

    final bodyLen = 1 + _nonceLen + cipher.length;
    final frame = Uint8List(bodyLen + _tagLen);
    frame[0] = _version;
    frame.setRange(1, 1 + _nonceLen, nonce);
    frame.setRange(1 + _nonceLen, bodyLen, cipher);

    final fullTag = Hmac(sha256, macKey).convert(Uint8List.sublistView(frame, 0, bodyLen)).bytes;
    frame.setRange(bodyLen, frame.length, fullTag.sublist(0, _tagLen));

    final encoded = base64Url.encode(frame).replaceAll('=', '');
    return '$prefix$encoded$suffix';
  }

  /// Decrypts a `#xn...xn#` token (or raw inner Base64Url body).
  static InvitePayload decode(String tokenOrBody) {
    var inner = tokenOrBody.trim();
    final m = _fullTokenRe.firstMatch(inner);
    if (m != null) {
      inner = m.group(1)!;
    } else {
      if (inner.startsWith(prefix)) inner = inner.substring(prefix.length);
      if (inner.endsWith(suffix)) inner = inner.substring(0, inner.length - suffix.length);
    }
    // Strip any spaces, line breaks or zero-width characters inserted by chat apps.
    inner = inner.replaceAll(RegExp(r'[\s\u200B-\u200D\uFEFF]'), '');
    if (inner.isEmpty) {
      throw const InviteCodeException(InviteErrorKind.corrupted, '登录口令内容为空，请重新复制整条消息');
    }

    // Normalize standard base64 chars if any chat app mutated them, and restore '=' padding.
    inner = inner.replaceAll('+', '-').replaceAll('/', '_');
    final rem = inner.length % 4;
    if (rem == 1) {
      throw const InviteCodeException(InviteErrorKind.corrupted, '登录口令不完整或已损坏，请回微信重新复制整条消息');
    }
    final padded = rem == 0 ? inner : inner.padRight(inner.length + (4 - rem), '=');

    Uint8List frame;
    try {
      frame = base64Url.decode(padded);
    } on FormatException {
      throw const InviteCodeException(InviteErrorKind.corrupted, '登录口令格式有误，请回微信重新复制整条消息');
    }

    if (frame.length < 1 + _nonceLen + 2 + _tagLen || frame.length > _maxFrameBytes) {
      throw const InviteCodeException(InviteErrorKind.corrupted, '登录口令不完整，请确认已复制完整消息');
    }

    final header = frame[0];
    final version = header & 0x7F;
    if (version != _version || (header & 0x80) != 0) {
      throw const InviteCodeException(InviteErrorKind.unsupportedVersion, '口令版本较新，请先升级小暖 App 后再试');
    }

    final bodyLen = frame.length - _tagLen;
    final nonce = Uint8List.sublistView(frame, 1, 1 + _nonceLen);
    final cipher = Uint8List.sublistView(frame, 1 + _nonceLen, bodyLen);
    final gotTag = Uint8List.sublistView(frame, bodyLen);

    final macKey = _deriveKey('xn-invite-v1/mac', nonce);
    final wantFull = Hmac(sha256, macKey).convert(Uint8List.sublistView(frame, 0, bodyLen)).bytes;

    var diff = 0;
    for (var i = 0; i < _tagLen; i++) {
      diff |= gotTag[i] ^ wantFull[i];
    }
    if (diff != 0) {
      throw const InviteCodeException(InviteErrorKind.corrupted, '登录口令不完整或已损坏，请回微信重新复制整条消息');
    }

    final encKey = _deriveKey('xn-invite-v1/enc', nonce);
    final plain = chacha20Xor(key: encKey, nonce: nonce, counter: 1, input: cipher);

    Object? decoded;
    try {
      decoded = jsonDecode(utf8.decode(plain));
    } catch (_) {
      throw const InviteCodeException(InviteErrorKind.corrupted, '登录口令解析失败，请重新复制');
    }
    if (decoded is! Map) {
      throw const InviteCodeException(InviteErrorKind.invalidPayload, '登录口令内容无效');
    }

    final rawServer = (decoded['s'] as String?)?.trim() ?? '';
    if (rawServer.isNotEmpty && !isValidServerUrl(rawServer)) {
      throw const InviteCodeException(InviteErrorKind.invalidPayload, '口令中的服务器地址无效');
    }
    final server = effectiveServer(rawServer);
    final user = (decoded['u'] as String?)?.trim() ?? '';
    final pass = (decoded['p'] as String?) ?? '';
    final name = (decoded['n'] as String?)?.trim() ?? '';
    final ts = (decoded['t'] as num?)?.toInt() ?? 0;

    if ((user.isEmpty || pass.isEmpty) && !isSelfHostedServer(server)) {
      throw const InviteCodeException(InviteErrorKind.invalidPayload, '口令缺少账号信息，请让家人重新生成');
    }

    return InvitePayload(
      server: server,
      username: user,
      password: pass,
      displayName: name,
      issuedAt: ts,
    );
  }

  /// Scans arbitrary clipboard/shared text for `#xn...xn#` (preferring the latest match).
  static InviteInspection inspectText(String? text) {
    if (text == null || text.isEmpty || !text.contains(prefix)) {
      return const InviteInspection.none();
    }
    final matches = _fullTokenRe.allMatches(text).toList(growable: false);
    if (matches.isNotEmpty) {
      InviteCodeException? lastErr;
      for (var i = matches.length - 1; i >= 0; i--) {
        final raw = matches[i].group(0)!;
        final cleanedBody = matches[i].group(1)!.replaceAll(RegExp(r'[\s\u200B-\u200D\uFEFF]'), '');
        final fp = fingerprintOf(cleanedBody);
        try {
          final payload = decode(raw);
          return InviteInspection._(payload: payload, rawToken: raw, fingerprint: fp);
        } on InviteCodeException catch (e) {
          lastErr = e;
        }
      }
      if (lastErr != null) {
        final cleaned = matches.last.group(1)!.replaceAll(RegExp(r'[\s\u200B-\u200D\uFEFF]'), '');
        return InviteInspection._(error: lastErr, fingerprint: fingerprintOf(cleaned));
      }
    }

    if (_danglingOpenRe.hasMatch(text.trimRight())) {
      final tail = text.substring(text.lastIndexOf(prefix));
      return InviteInspection._(
        fingerprint: fingerprintOf(tail),
        error: const InviteCodeException(
          InviteErrorKind.truncated,
          '口令好像没复制全（缺少结尾 xn#），请回微信长按整条消息点「复制」',
        ),
      );
    }
    return const InviteInspection.none();
  }

  /// Short non-reversible digest of a token body for deduplicating clipboard prompts.
  static String fingerprintOf(String text) {
    final digest = sha256.convert(utf8.encode(text)).toString();
    return digest.substring(0, 16);
  }

  /// Formats a ready-to-send WeChat / SMS message containing the encrypted `#xn...xn#` code.
  static String formatShareMessage(InvitePayload payload, {String? prebuiltCode}) {
    final code = prebuiltCode ?? encode(payload);
    if (payload.hasCredentials) {
      final greet = payload.displayName.trim().isNotEmpty ? '${payload.displayName.trim()}，' : '';
      return '【小暖一键登录】\n'
          '$greet长按复制整条消息，打开「小暖」App 后会自动弹出登录提示，点一下就能直接用啦：\n'
          '$code';
    }
    return '【小暖自托管配置】\n'
        '长按复制整条消息，打开「小暖」App 即可自动切换到自托管服务器（${payload.serverHost}）：\n'
        '$code';
  }
}

/// Tracks locally-generated and already-consumed `#xn...xn#` codes so the sender's own
/// phone or a post-logout screen never nags with a stale clipboard popup.
abstract final class InviteCodeGuard {
  static const _prefsKey = 'consumed_xn_invites';
  static final Set<String> _locallyGenerated = <String>{};

  static void markGeneratedLocally(String codeOrText) {
    final m = RegExp(r'#xn([^#]+?)xn#').firstMatch(codeOrText);
    final body = (m != null ? m.group(1)! : codeOrText).replaceAll(RegExp(r'[\s\u200B-\u200D\uFEFF]'), '');
    if (body.isNotEmpty) {
      _locallyGenerated.add(XnInviteCodec.fingerprintOf(body));
    }
  }

  static bool wasGeneratedLocally(String? fingerprint) =>
      fingerprint != null && _locallyGenerated.contains(fingerprint);

  @visibleForTesting
  static void clearLocalMarks() => _locallyGenerated.clear();

  static bool wasConsumed(SharedPreferences prefs, String? fingerprint) {
    if (fingerprint == null || fingerprint.isEmpty) return false;
    final list = prefs.getStringList(_prefsKey);
    return list != null && list.contains(fingerprint);
  }

  static Future<void> markConsumed(SharedPreferences prefs, String? fingerprint) async {
    if (fingerprint == null || fingerprint.isEmpty) return;
    final current = List<String>.of(prefs.getStringList(_prefsKey) ?? const <String>[])
      ..remove(fingerprint)
      ..add(fingerprint);
    if (current.length > 12) {
      current.removeRange(0, current.length - 12);
    }
    await prefs.setStringList(_prefsKey, current);
  }
}

/// RFC 8439 Section 2.4 ChaCha20 stream cipher (20 rounds, 256-bit key, 96-bit nonce).
@visibleForTesting
Uint8List chacha20Xor({
  required Uint8List key,
  required Uint8List nonce,
  required int counter,
  required Uint8List input,
}) {
  if (key.length != 32) throw ArgumentError.value(key.length, 'key', 'Must be 32 bytes');
  if (nonce.length != 12) throw ArgumentError.value(nonce.length, 'nonce', 'Must be 12 bytes');

  final keyData = ByteData.sublistView(key);
  final nonceData = ByteData.sublistView(nonce);
  final init = Uint32List(16)
    ..[0] = 0x61707865
    ..[1] = 0x3320646e
    ..[2] = 0x79622d32
    ..[3] = 0x6b206574;
  for (var i = 0; i < 8; i++) {
    init[4 + i] = keyData.getUint32(i * 4, Endian.little);
  }
  init[12] = counter & 0xFFFFFFFF;
  for (var i = 0; i < 3; i++) {
    init[13 + i] = nonceData.getUint32(i * 4, Endian.little);
  }

  final out = Uint8List(input.length);
  final state = Uint32List(16);
  final blockBytes = Uint8List(64);
  final blockData = ByteData.sublistView(blockBytes);

  var offset = 0;
  while (offset < input.length) {
    state.setAll(0, init);
    for (var r = 0; r < 10; r++) {
      _qr(state, 0, 4, 8, 12);
      _qr(state, 1, 5, 9, 13);
      _qr(state, 2, 6, 10, 14);
      _qr(state, 3, 7, 11, 15);
      _qr(state, 0, 5, 10, 15);
      _qr(state, 1, 6, 11, 12);
      _qr(state, 2, 7, 8, 13);
      _qr(state, 3, 4, 9, 14);
    }
    for (var i = 0; i < 16; i++) {
      blockData.setUint32(i * 4, (state[i] + init[i]) & 0xFFFFFFFF, Endian.little);
    }
    final take = min(64, input.length - offset);
    for (var i = 0; i < take; i++) {
      out[offset + i] = input[offset + i] ^ blockBytes[i];
    }
    offset += take;
    init[12] = (init[12] + 1) & 0xFFFFFFFF;
  }
  return out;
}

int _rotl32(int v, int n) => ((v << n) & 0xFFFFFFFF) | ((v & 0xFFFFFFFF) >>> (32 - n));

void _qr(Uint32List s, int a, int b, int c, int d) {
  s[a] = (s[a] + s[b]) & 0xFFFFFFFF;
  s[d] = _rotl32(s[d] ^ s[a], 16);
  s[c] = (s[c] + s[d]) & 0xFFFFFFFF;
  s[b] = _rotl32(s[b] ^ s[c], 12);
  s[a] = (s[a] + s[b]) & 0xFFFFFFFF;
  s[d] = _rotl32(s[d] ^ s[a], 8);
  s[c] = (s[c] + s[d]) & 0xFFFFFFFF;
  s[b] = _rotl32(s[b] ^ s[c], 7);
}

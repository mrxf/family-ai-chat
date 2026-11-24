import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Outcome of scanning arbitrary text or clipboard content for a `#xns...xns#` share code.
@immutable
class ShareCodeInspection {
  final String? code;
  final String? rawToken;

  const ShareCodeInspection._({this.code, this.rawToken});
  const ShareCodeInspection.none() : this._();

  bool get hasShare => code != null && code!.isNotEmpty;
}

/// Short share-code formatter and scanner (`#xns<code>xns#`), modelled after [XnInviteCodec].
abstract final class XnShareCodec {
  static const prefix = '#xns';
  static const suffix = 'xns#';

  static final _fullTokenRe = RegExp(r'#xns([A-Za-z0-9\s\u200B-\u200D\uFEFF]{4,32})xns#');
  static final _rawCodeRe = RegExp(r'^[A-Za-z0-9]{6,20}$');

  /// Wraps a server-issued short [code] in a `#xns...xns#` token.
  static String encode(String code) => '$prefix${code.trim()}$suffix';

  /// Formats a Douyin-style shareable text snippet containing the `#xns...xns#` token.
  static String formatShareMessage({
    required String title,
    required String code,
    String? creatorName,
  }) {
    final cleanTitle = title.trim().isEmpty ? '一段有趣' : '《${title.trim()}》';
    final who = (creatorName ?? '').trim().isNotEmpty ? '${creatorName!.trim()}在小暖中分享了' : '我在小暖中分享了';
    return '$who$cleanTitle的对话历史，复制这段文字打开「小暖」查看并继续对话吧：\n${encode(code)}';
  }

  /// Extracts the latest valid `#xns<code>xns#` token (or a raw pasted code if [allowBareCode] is true).
  static ShareCodeInspection inspectText(String? text, {bool allowBareCode = false}) {
    if (text == null || text.trim().isEmpty) return const ShareCodeInspection.none();
    final matches = _fullTokenRe.allMatches(text).toList(growable: false);
    if (matches.isNotEmpty) {
      final last = matches.last;
      final cleaned = last.group(1)!.replaceAll(RegExp(r'[\s\u200B-\u200D\uFEFF]'), '');
      if (_rawCodeRe.hasMatch(cleaned)) {
        return ShareCodeInspection._(code: cleaned, rawToken: last.group(0));
      }
    }
    if (allowBareCode) {
      final bare = text.trim().replaceAll(RegExp(r'[\s\u200B-\u200D\uFEFF]'), '');
      if (_rawCodeRe.hasMatch(bare)) {
        return ShareCodeInspection._(code: bare, rawToken: bare);
      }
    }
    return const ShareCodeInspection.none();
  }
}

/// Prevents the sender's own device or an already-handled clipboard code from prompting repeatedly.
abstract final class ShareCodeGuard {
  static const _prefsKey = 'consumed_xn_shares';
  static final Set<String> _locallyGenerated = <String>{};

  static void markGeneratedLocally(String codeOrText) {
    final ins = XnShareCodec.inspectText(codeOrText, allowBareCode: true);
    if (ins.hasShare) _locallyGenerated.add(ins.code!);
  }

  static bool wasGeneratedLocally(String? code) => code != null && _locallyGenerated.contains(code);

  @visibleForTesting
  static void clearLocalMarks() => _locallyGenerated.clear();

  static bool wasConsumed(SharedPreferences prefs, String? code) {
    if (code == null || code.isEmpty) return false;
    final list = prefs.getStringList(_prefsKey);
    return list != null && list.contains(code);
  }

  static Future<void> markConsumed(SharedPreferences prefs, String? code) async {
    if (code == null || code.isEmpty) return;
    final current = List<String>.of(prefs.getStringList(_prefsKey) ?? const <String>[])
      ..remove(code)
      ..add(code);
    if (current.length > 20) {
      current.removeRange(0, current.length - 20);
    }
    await prefs.setStringList(_prefsKey, current);
  }
}

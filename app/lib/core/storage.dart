import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Small persistence helpers: secure token, preferences, and a JSON file cache
/// so the app can paint instantly on cold start before the network answers.
class Storage {
  Storage._(this.prefs, this._cacheDir);

  static Future<Storage> open() async {
    final prefs = await SharedPreferences.getInstance();
    final dir = Directory('${(await getApplicationSupportDirectory()).path}/cache');
    await dir.create(recursive: true);
    return Storage._(prefs, dir);
  }

  final SharedPreferences prefs;
  final Directory _cacheDir;
  // macOS: the legacy keychain needs no keychain-access-groups entitlement,
  // so unsigned / ad-hoc signed builds keep the login across launches and run
  // on any Mac. Other platforms ignore mOptions.
  static const _secure = FlutterSecureStorage(mOptions: MacOsOptions(usesDataProtectionKeychain: false));

  // ---- token ----
  Future<String?> readToken() async {
    try {
      return await _secure.read(key: 'token');
    } catch (_) {
      return null;
    }
  }

  Future<void> writeToken(String? token) async {
    try {
      if (token == null) {
        await _secure.delete(key: 'token');
      } else {
        await _secure.write(key: 'token', value: token);
      }
    } catch (_) {}
  }

  // ---- JSON cache ----
  File _file(String key) => File('${_cacheDir.path}/${key.replaceAll(RegExp(r'[^A-Za-z0-9_.-]'), '_')}.json');

  Future<dynamic> readCache(String key) async {
    try {
      final f = _file(key);
      if (!await f.exists()) return null;
      return jsonDecode(await f.readAsString());
    } catch (_) {
      return null;
    }
  }

  Future<void> writeCache(String key, Object? value) async {
    try {
      final f = _file(key);
      final tmp = File('${f.path}.tmp');
      await tmp.writeAsString(jsonEncode(value), flush: true);
      await tmp.rename(f.path);
    } catch (_) {}
  }

  Future<void> deleteCache(String key) async {
    try {
      await _file(key).delete();
    } catch (_) {}
  }

  Future<void> clearCache() async {
    try {
      await _cacheDir.delete(recursive: true);
      await _cacheDir.create(recursive: true);
    } catch (_) {}
  }
}

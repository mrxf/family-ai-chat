import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/invite_code.dart';
import '../core/models.dart';
import '../core/storage.dart';

/// Overridden in main() once storage is opened.
final storageProvider = Provider<Storage>((ref) => throw UnimplementedError());

typedef ApiFactory = Api Function(String baseUrl, {String? token, void Function()? onUnauthorized});

/// Allows widget tests to inject a deterministic [Api] for unauthenticated checks & login.
final apiFactoryProvider = Provider<ApiFactory>((_) => Api.new);

// ---------------- settings ----------------

class AppSettings {
  final ThemeMode themeMode;
  final double fontScale; // 1.0 标准, 1.15 大, 1.3 特大
  final double ttsRate;
  final bool haptics;
  const AppSettings({this.themeMode = ThemeMode.system, this.fontScale = 1.0, this.ttsRate = 0.5, this.haptics = true});

  AppSettings copyWith({ThemeMode? themeMode, double? fontScale, double? ttsRate, bool? haptics}) => AppSettings(
        themeMode: themeMode ?? this.themeMode,
        fontScale: fontScale ?? this.fontScale,
        ttsRate: ttsRate ?? this.ttsRate,
        haptics: haptics ?? this.haptics,
      );
}

class SettingsNotifier extends Notifier<AppSettings> {
  @override
  AppSettings build() {
    final p = ref.read(storageProvider).prefs;
    return AppSettings(
      themeMode: ThemeMode.values[(p.getInt('themeMode') ?? 0).clamp(0, 2)],
      fontScale: p.getDouble('fontScale') ?? 1.0,
      ttsRate: p.getDouble('ttsRate') ?? 0.5,
      haptics: p.getBool('haptics') ?? true,
    );
  }

  void update(AppSettings s) {
    state = s;
    final p = ref.read(storageProvider).prefs;
    p.setInt('themeMode', s.themeMode.index);
    p.setDouble('fontScale', s.fontScale);
    p.setDouble('ttsRate', s.ttsRate);
    p.setBool('haptics', s.haptics);
  }
}

final settingsProvider = NotifierProvider<SettingsNotifier, AppSettings>(SettingsNotifier.new);

// ---------------- session ----------------

enum SessionStatus { loading, loggedOut, loggedIn }

class Session {
  final SessionStatus status;
  final String server;
  final Api? api;
  final Bootstrap? boot;
  const Session({required this.status, this.server = kDefaultServer, this.api, this.boot});

  User? get user => boot?.user;
  bool get isSelfHosted => isSelfHostedServer(server);
  String get serverHost => displayHost(server);
}

class SessionNotifier extends Notifier<Session> {
  DateTime _lastRefresh = DateTime.now();
  Future<void> Function(Api api)? _beforeLogout;

  /// Allows ChatController to wipe an active private conversation while the
  /// session token is still valid, right before `/api/auth/logout` revokes it.
  void registerBeforeLogout(Future<void> Function(Api api)? fn) => _beforeLogout = fn;

  @override
  Session build() {
    Future.microtask(_restore);
    // Phones keep the app alive for days: refresh models & assistants when it comes back
    // to the foreground, so admin changes reach the family without a restart.
    final lifecycle = AppLifecycleListener(onResume: () {
      if (state.status == SessionStatus.loggedIn && DateTime.now().difference(_lastRefresh) > const Duration(minutes: 3)) {
        unawaited(refresh());
      }
    });
    ref.onDispose(lifecycle.dispose);
    return const Session(status: SessionStatus.loading);
  }

  Storage get _st => ref.read(storageProvider);
  ApiFactory get _apiFactory => ref.read(apiFactoryProvider);

  String get lastServer => _st.prefs.getString('server') ?? '';
  String get configuredServer => effectiveServer(lastServer);
  String get lastUsername => _st.prefs.getString('username') ?? '';

  Future<void> _restore() async {
    final saved = lastServer;
    final active = effectiveServer(saved);
    final token = await _st.readToken();
    if (saved.isEmpty || token == null) {
      state = Session(status: SessionStatus.loggedOut, server: active);
      return;
    }
    final api = _makeApi(active, token);
    // Paint from cache immediately, refresh in the background.
    final cached = await _st.readCache('bootstrap');
    if (cached is Map) {
      try {
        state = Session(status: SessionStatus.loggedIn, server: active, api: api, boot: Bootstrap.fromJson(cached.cast()));
        unawaited(refresh());
        return;
      } catch (_) {}
    }
    try {
      final boot = await api.bootstrap();
      await _st.writeCache('bootstrap', boot.toJson());
      state = Session(status: SessionStatus.loggedIn, server: active, api: api, boot: boot);
    } on ApiError catch (e) {
      if (e.isUnauthorized) {
        await _st.writeToken(null);
        state = Session(status: SessionStatus.loggedOut, server: active);
      } else {
        // Offline with no cache: still go to login so the user sees a clear error there.
        state = Session(status: SessionStatus.loggedOut, server: active);
      }
    }
  }

  Api _makeApi(String server, String token) {
    late final Api api;
    // A late 401 from an old account must not log out the current one.
    api = _apiFactory(server, token: token, onUnauthorized: () {
      if (identical(state.api, api)) unawaited(logout(remote: false));
    });
    return api;
  }

  /// Saves the target server (official default or self-hosted) while logged out.
  Future<void> setServer(String server) async {
    final next = effectiveServer(server);
    await _st.prefs.setString('server', next);
    if (state.status != SessionStatus.loggedIn) {
      state = Session(status: state.status, server: next);
    }
  }

  Future<void> refresh() async {
    final api = state.api;
    if (api == null) return;
    _lastRefresh = DateTime.now();
    try {
      final boot = await api.bootstrap();
      await _st.writeCache('bootstrap', boot.toJson());
      if (state.api == api) state = Session(status: SessionStatus.loggedIn, server: state.server, api: api, boot: boot);
    } catch (_) {}
  }

  Future<void> login(String server, String username, String password) async {
    final base = effectiveServer(server);
    final anon = _apiFactory(base);
    final (token, _) = await anon.login(username.trim(), password, 'Android');
    await loginWithToken(base, username, token);
  }

  /// Finishes a login when the token was obtained elsewhere (first-run setup).
  Future<void> loginWithToken(String server, String username, String token) async {
    final base = effectiveServer(server);
    final api = _makeApi(base, token);
    final boot = await api.bootstrap();
    await _st.writeToken(token);
    await _st.prefs.setString('server', base);
    await _st.prefs.setString('username', username.trim());
    await _st.writeCache('bootstrap', boot.toJson());
    state = Session(status: SessionStatus.loggedIn, server: base, api: api, boot: boot);
  }

  Future<void> logout({bool remote = true}) async {
    if (state.status != SessionStatus.loggedIn) return;
    final api = state.api;
    if (remote && api != null) {
      try {
        await _beforeLogout?.call(api);
      } catch (_) {}
      try {
        await api.logout();
      } catch (_) {}
    }
    await _st.writeToken(null);
    await _st.clearCache();
    state = Session(status: SessionStatus.loggedOut, server: state.server);
  }

  void setUser(User u) {
    final b = state.boot;
    if (b == null) return;
    final nb = b.copyWith(user: u);
    _st.writeCache('bootstrap', nb.toJson());
    state = Session(status: state.status, server: state.server, api: state.api, boot: nb);
  }
}

final sessionProvider = NotifierProvider<SessionNotifier, Session>(SessionNotifier.new);

/// Convenience: the authenticated API (only valid while logged in).
final apiProvider = Provider<Api>((ref) => ref.watch(sessionProvider).api!);

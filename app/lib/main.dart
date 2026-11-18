import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'core/storage.dart';
import 'state/incoming_share.dart';
import 'state/session.dart';
import 'ui/home_page.dart';
import 'ui/login_page.dart';
import 'ui/mascot_anim.dart';
import 'ui/theme.dart';
import 'ui/widgets.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
  final storage = await Storage.open();
  precacheMascot();
  runApp(ProviderScope(
    overrides: [storageProvider.overrideWithValue(storage)],
    child: const NuanApp(),
  ));
}

final navigatorKey = GlobalKey<NavigatorState>();

class NuanApp extends ConsumerWidget {
  const NuanApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Eagerly attach the native share channel before login/home finishes loading.
    ref.watch(incomingShareProvider.select((_) => 0));
    final settings = ref.watch(settingsProvider);
    // When the session ends (logout or expired token), drop any pages stacked
    // above home (settings, admin...) so nothing renders without a user.
    ref.listen(sessionProvider.select((s) => s.status), (prev, next) {
      if (next != SessionStatus.loggedIn) navigatorKey.currentState?.popUntil((r) => r.isFirst);
    });
    return MaterialApp(
      navigatorKey: navigatorKey,
      title: '小暖',
      debugShowCheckedModeBanner: false,
      theme: buildTheme(Brightness.light),
      darkTheme: buildTheme(Brightness.dark),
      themeMode: settings.themeMode,
      locale: const Locale('zh', 'CN'),
      supportedLocales: const [Locale('zh', 'CN'), Locale('en')],
      localizationsDelegates: const [
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      builder: (context, child) {
        // App-level font size setting on top of the system's own scaling.
        final mq = MediaQuery.of(context);
        final system = mq.textScaler.scale(1);
        return MediaQuery(
          data: mq.copyWith(textScaler: TextScaler.linear((system * settings.fontScale).clamp(0.8, 2.0))),
          child: child!,
        );
      },
      home: const _Gate(),
    );
  }
}

class _Gate extends ConsumerWidget {
  const _Gate();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final status = ref.watch(sessionProvider.select((s) => s.status));
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 350),
      child: switch (status) {
        SessionStatus.loading => const _Splash(),
        SessionStatus.loggedOut => const LoginPage(),
        SessionStatus.loggedIn => const HomePage(),
      },
    );
  }
}

class _Splash extends StatelessWidget {
  const _Splash();
  @override
  Widget build(BuildContext context) => const Scaffold(body: Center(child: Mascot(size: 120)));
}

import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Design tokens. Warm, soft, high-contrast enough for older eyes.
@immutable
class Palette extends ThemeExtension<Palette> {
  const Palette({
    required this.bg,
    required this.surface,
    required this.surfaceAlt,
    required this.text,
    required this.muted,
    required this.faint,
    required this.line,
    required this.userBubble,
    required this.userText,
    required this.accent,
    required this.accentSoft,
    required this.privateAccent,
    required this.privateSoft,
    required this.codeBg,
    required this.preBg,
    required this.preText,
    required this.good,
    required this.bad,
    required this.shadow,
  });

  final Color bg, surface, surfaceAlt, text, muted, faint, line, userBubble, userText, accent, accentSoft, privateAccent, privateSoft, codeBg, preBg, preText, good, bad, shadow;

  static const brand = Color(0xFFFF7A45);
  static const brandDeep = Color(0xFFF25A2A);
  static const brandGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [Color(0xFFFF9C5B), Color(0xFFFF6A3D)],
  );
  static const privateGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [Color(0xFF5A7099), Color(0xFF3E4F70)],
  );

  static const light = Palette(
    bg: Color(0xFFFFFAF6),
    surface: Color(0xFFFFFFFF),
    surfaceAlt: Color(0xFFFFF3EA),
    text: Color(0xFF2A211B),
    muted: Color(0xFF85776C),
    faint: Color(0xFFB9ABA0),
    line: Color(0xFFF1E5DB),
    userBubble: Color(0xFFFFE6D5),
    userText: Color(0xFF3A2414),
    accent: brand,
    accentSoft: Color(0xFFFFEDE2),
    privateAccent: Color(0xFF475B82),
    privateSoft: Color(0xFFEDF1F8),
    codeBg: Color(0xFFF8EFE8),
    preBg: Color(0xFF2B2420),
    preText: Color(0xFFF5EDE6),
    good: Color(0xFF2F9E55),
    bad: Color(0xFFDD4B39),
    shadow: Color(0x1A8A4A20),
  );

  static const dark = Palette(
    bg: Color(0xFF151211),
    surface: Color(0xFF211D1B),
    surfaceAlt: Color(0xFF1B1816),
    text: Color(0xFFF2EBE5),
    muted: Color(0xFF9D9189),
    faint: Color(0xFF6A605A),
    line: Color(0xFF2F2926),
    userBubble: Color(0xFF3B2A20),
    userText: Color(0xFFFBE9DC),
    accent: Color(0xFFFF8B57),
    accentSoft: Color(0xFF3A2519),
    privateAccent: Color(0xFF93ADD9),
    privateSoft: Color(0xFF202734),
    codeBg: Color(0xFF2A2522),
    preBg: Color(0xFF0F0D0C),
    preText: Color(0xFFEDE5DE),
    good: Color(0xFF45C06A),
    bad: Color(0xFFF0645A),
    shadow: Color(0x40000000),
  );

  @override
  Palette copyWith() => this;

  @override
  Palette lerp(Palette? other, double t) {
    if (other == null) return this;
    Color l(Color a, Color b) => Color.lerp(a, b, t)!;
    return Palette(
      bg: l(bg, other.bg),
      surface: l(surface, other.surface),
      surfaceAlt: l(surfaceAlt, other.surfaceAlt),
      text: l(text, other.text),
      muted: l(muted, other.muted),
      faint: l(faint, other.faint),
      line: l(line, other.line),
      userBubble: l(userBubble, other.userBubble),
      userText: l(userText, other.userText),
      accent: l(accent, other.accent),
      accentSoft: l(accentSoft, other.accentSoft),
      privateAccent: l(privateAccent, other.privateAccent),
      privateSoft: l(privateSoft, other.privateSoft),
      codeBg: l(codeBg, other.codeBg),
      preBg: l(preBg, other.preBg),
      preText: l(preText, other.preText),
      good: l(good, other.good),
      bad: l(bad, other.bad),
      shadow: l(shadow, other.shadow),
    );
  }
}

extension PaletteX on BuildContext {
  Palette get pal => Theme.of(this).extension<Palette>()!;
}

ThemeData buildTheme(Brightness b) {
  final p = b == Brightness.light ? Palette.light : Palette.dark;
  final scheme = ColorScheme.fromSeed(seedColor: Palette.brand, brightness: b).copyWith(
    primary: p.accent,
    onPrimary: Colors.white,
    surface: p.bg,
    onSurface: p.text,
    error: p.bad,
  );
  final base = ThemeData(useMaterial3: true, brightness: b, colorScheme: scheme);
  final text = base.textTheme.apply(bodyColor: p.text, displayColor: p.text).copyWith(
        bodyLarge: TextStyle(fontSize: 17, height: 1.6, color: p.text),
        bodyMedium: TextStyle(fontSize: 15, height: 1.5, color: p.text),
        bodySmall: TextStyle(fontSize: 13, height: 1.4, color: p.muted),
        titleLarge: TextStyle(fontSize: 22, fontWeight: FontWeight.w700, color: p.text, letterSpacing: 0.2),
        titleMedium: TextStyle(fontSize: 17, fontWeight: FontWeight.w600, color: p.text),
        titleSmall: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: p.text),
        labelLarge: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      );
  final overlay = b == Brightness.light ? SystemUiOverlayStyle.dark : SystemUiOverlayStyle.light;
  return base.copyWith(
    scaffoldBackgroundColor: p.bg,
    textTheme: text,
    extensions: [p],
    splashFactory: InkSparkle.splashFactory,
    appBarTheme: AppBarTheme(
      backgroundColor: p.bg,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      scrolledUnderElevation: 0,
      centerTitle: true,
      foregroundColor: p.text,
      titleTextStyle: TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: p.text),
      systemOverlayStyle: overlay.copyWith(statusBarColor: Colors.transparent, systemNavigationBarColor: p.bg),
    ),
    dividerTheme: DividerThemeData(color: p.line, thickness: 1, space: 1),
    drawerTheme: DrawerThemeData(backgroundColor: p.surfaceAlt, surfaceTintColor: Colors.transparent, width: 316),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      showDragHandle: true,
      dragHandleColor: p.faint,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(28))),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: b == Brightness.light ? const Color(0xFF2B2420) : const Color(0xFFEDE5DE),
      contentTextStyle: TextStyle(color: b == Brightness.light ? Colors.white : Colors.black, fontSize: 15),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: p.surface,
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
      hintStyle: TextStyle(color: p.faint),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(16), borderSide: BorderSide(color: p.line)),
      enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(16), borderSide: BorderSide(color: p.line)),
      focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(16), borderSide: BorderSide(color: p.accent, width: 1.6)),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(64, 52),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
        textStyle: const TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: p.text,
        backgroundColor: p.surface,
        side: BorderSide(color: p.line),
        minimumSize: const Size(64, 48),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
        textStyle: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w500),
      ),
    ),
    switchTheme: SwitchThemeData(
      trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
      trackOutlineWidth: const WidgetStatePropertyAll(0),
      thumbIcon: const WidgetStatePropertyAll(Icon(null)),
      thumbColor: WidgetStateProperty.resolveWith((states) {
        if (states.contains(WidgetState.disabled)) {
          return Colors.white.withValues(alpha: b == Brightness.light ? 0.75 : 0.35);
        }
        if (states.contains(WidgetState.selected)) return Colors.white;
        return b == Brightness.light ? Colors.white : p.muted;
      }),
      trackColor: WidgetStateProperty.resolveWith((states) {
        final on = states.contains(WidgetState.selected);
        if (states.contains(WidgetState.disabled)) {
          return (on ? p.accent : p.line).withValues(alpha: 0.45);
        }
        if (on) return p.accent;
        return b == Brightness.light ? const Color(0xFFE9DDD3) : const Color(0xFF352F2B);
      }),
    ),
    listTileTheme: ListTileThemeData(iconColor: p.muted, textColor: p.text, minVerticalPadding: 12),
    popupMenuTheme: PopupMenuThemeData(
      color: p.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      textStyle: TextStyle(fontSize: 16, color: p.text),
    ),
    pageTransitionsTheme: PageTransitionsTheme(builders: {
      TargetPlatform.android: const PredictiveBackPageTransitionsBuilder(),
      TargetPlatform.iOS: const CupertinoPageTransitionsBuilder(),
    }),
  );
}

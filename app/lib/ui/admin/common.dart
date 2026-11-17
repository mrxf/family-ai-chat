import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/admin_models.dart';
import '../theme.dart';
import '../widgets.dart';

// Shared building blocks for the admin "models & providers" screens.

String fmtUsd(double v) {
  if (v <= 0) return '\$0';
  if (v < 0.01) return '\$${v.toStringAsFixed(4)}';
  if (v < 1) return '\$${v.toStringAsFixed(3)}';
  return '\$${v.toStringAsFixed(2)}';
}

String fmtTokens(num n) {
  if (n >= 100000000) return '${(n / 100000000).toStringAsFixed(1)} 亿';
  if (n >= 10000) return '${(n / 10000).toStringAsFixed(1)} 万';
  return '$n';
}

String fmtContext(int n) {
  if (n <= 0) return '';
  if (n >= 1000000) return '${(n / 1000000).toStringAsFixed(n % 1000000 == 0 ? 0 : 1)}M';
  return '${(n / 1000).round()}K';
}

/// Per-million-token price, e.g. "$0.15 / $0.60".
String fmtPrice(double input, double output) {
  String f(double v) => v == 0 ? '0' : (v < 0.1 ? v.toStringAsFixed(3) : (v < 10 ? v.toStringAsFixed(2) : v.toStringAsFixed(1)));
  return '\$${f(input)} / \$${f(output)}';
}

/// Same rule as the server: a bare host gets /v1, a URL with a path is used as is.
String endpointPreview(String baseUrl) {
  var b = baseUrl.trim();
  while (b.endsWith('/')) {
    b = b.substring(0, b.length - 1);
  }
  if (b.isEmpty) return '';
  final u = Uri.tryParse(b);
  if (u == null || !u.hasScheme || u.host.isEmpty) return '';
  if (u.path.isEmpty) b += '/v1';
  return '$b/chat/completions';
}

String hostOf(String url) => Uri.tryParse(url.trim())?.host ?? '';

Future<void> openLink(BuildContext context, String url) async {
  final ok = await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
  if (!ok && context.mounted) showToast(context, '打不开链接：$url');
}

/// Colored badge standing in for a provider's logo.
class ProviderIcon extends StatelessWidget {
  const ProviderIcon({super.key, required this.kind, this.size = 44});
  final String kind;
  final double size;

  static const _styles = <String, (Color, String)>{
    'openrouter': (Color(0xFF6467F2), 'OR'),
    'openai': (Color(0xFF10A37F), 'AI'),
    'gemini': (Color(0xFF4285F4), 'G'),
    'deepseek': (Color(0xFF4D6BFE), 'DS'),
    'dashscope': (Color(0xFFFF6A00), '百'),
    'siliconflow': (Color(0xFF7C3AED), '硅'),
  };

  @override
  Widget build(BuildContext context) {
    final style = _styles[kind];
    final c = style?.$1 ?? const Color(0xFF8A7B70);
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        gradient: LinearGradient(begin: Alignment.topLeft, end: Alignment.bottomRight, colors: [Color.lerp(c, Colors.white, 0.12)!, c]),
        borderRadius: BorderRadius.circular(size * 0.3),
      ),
      child: style == null
          ? Icon(Icons.hub_rounded, color: Colors.white, size: size * 0.52)
          : Text(style.$2,
              textScaler: TextScaler.noScaling,
              style: TextStyle(color: Colors.white, fontWeight: FontWeight.w800, fontSize: size * (style.$2.length > 1 ? 0.34 : 0.44), letterSpacing: -0.5)),
    );
  }
}

/// Small rounded label: "能看图", "已停用", "128K" ...
class Tag extends StatelessWidget {
  const Tag(this.text, {super.key, this.color, this.icon, this.filled = true});
  final String text;
  final Color? color;
  final IconData? icon;
  final bool filled;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final c = color ?? p.muted;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2.5),
      decoration: BoxDecoration(
        color: filled ? c.withValues(alpha: 0.12) : Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        border: filled ? null : Border.all(color: c.withValues(alpha: 0.4)),
      ),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        if (icon != null) ...[Icon(icon, size: 13, color: c), const SizedBox(width: 3)],
        Text(text, style: TextStyle(fontSize: 12, color: c, fontWeight: FontWeight.w600, height: 1.2)),
      ]),
    );
  }
}

/// Capability tags shared by model rows.
List<Widget> modelTags(BuildContext context, {required bool vision, required bool think, int contextWindow = 0, double inPrice = 0, double outPrice = 0}) {
  final p = context.pal;
  return [
    if (vision) Tag('能看图', icon: Icons.image_outlined, color: const Color(0xFF3B82F6)),
    if (think) Tag('深度思考', icon: Icons.lightbulb_outline_rounded, color: const Color(0xFFD97706)),
    if (contextWindow > 0) Tag(fmtContext(contextWindow), color: p.muted),
    if (inPrice > 0 || outPrice > 0) Tag(fmtPrice(inPrice, outPrice), color: p.good),
  ];
}

/// Type scale for the admin screens: titles are clearly heavier than their
/// descriptions, and descriptions get room to breathe.
abstract final class AdminText {
  static const titleGap = 5.0;
  static TextStyle section(Palette p) => TextStyle(fontSize: 17, fontWeight: FontWeight.w700, color: p.text, height: 1.3);
  static TextStyle title(Palette p) => TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: p.text, height: 1.3);
  static TextStyle desc(Palette p) => TextStyle(fontSize: 13.5, color: p.muted, height: 1.5);
}

/// A setting's name with an optional one-line explanation underneath.
class TitleDesc extends StatelessWidget {
  const TitleDesc(this.title, {super.key, this.desc});
  final String title;
  final String? desc;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
      Text(title, style: AdminText.title(p)),
      if (desc != null && desc!.isNotEmpty) ...[
        const SizedBox(height: AdminText.titleGap),
        Text(desc!, style: AdminText.desc(p)),
      ],
    ]);
  }
}

/// Switch row for a GroupCard, same spacing as [InfoRow].
class SwitchRow extends StatelessWidget {
  const SwitchRow({super.key, required this.title, this.desc, required this.value, required this.onChanged});
  final String title;
  final String? desc;
  final bool value;
  final ValueChanged<bool>? onChanged;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onChanged == null ? null : () => onChanged!(!value),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 14, 12, 14),
        child: Row(children: [
          Expanded(child: TitleDesc(title, desc: desc)),
          const SizedBox(width: 12),
          Switch(value: value, onChanged: onChanged),
        ]),
      ),
    );
  }
}

class SectionHeader extends StatelessWidget {
  const SectionHeader(this.title, {super.key, this.trailing, this.subtitle});
  final String title;
  final String? subtitle;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 30, 4, 12),
      child: Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: AdminText.section(p)),
            if (subtitle != null) ...[
              const SizedBox(height: 4),
              Text(subtitle!, style: AdminText.desc(p)),
            ],
          ]),
        ),
        ?trailing,
      ]),
    );
  }
}

/// White rounded card that groups rows.
class GroupCard extends StatelessWidget {
  const GroupCard({super.key, required this.children, this.padding});
  final List<Widget> children;
  final EdgeInsetsGeometry? padding;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final items = <Widget>[];
    for (var i = 0; i < children.length; i++) {
      if (i > 0) items.add(Divider(height: 1, indent: 18, endIndent: 18, color: p.line));
      items.add(children[i]);
    }
    return Container(
      padding: padding,
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(20), border: Border.all(color: p.line)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: items),
    );
  }
}

/// A tappable settings row: icon, title, optional subtitle and a value on the right.
class InfoRow extends StatelessWidget {
  const InfoRow({super.key, required this.title, this.icon, this.subtitle, this.value, this.valueColor, this.onTap, this.trailing});
  final IconData? icon;
  final String title;
  final String? subtitle, value;
  final Color? valueColor;
  final VoidCallback? onTap;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return InkWell(
      onTap: onTap,
      child: ConstrainedBox(
        constraints: const BoxConstraints(minHeight: 60),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
          child: Row(children: [
            if (icon != null) ...[Icon(icon, color: p.accent, size: 24), const SizedBox(width: 14)],
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
                Text(title, style: AdminText.title(p)),
                if (subtitle != null && subtitle!.isNotEmpty) ...[
                  const SizedBox(height: AdminText.titleGap),
                  Text(subtitle!, style: AdminText.desc(p)),
                ],
              ]),
            ),
            if (value != null) ...[
              const SizedBox(width: 12),
              ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 170),
                child: Text(value!, textAlign: TextAlign.right, maxLines: 2, overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 15, color: valueColor ?? p.muted, fontWeight: FontWeight.w500)),
              ),
            ],
            if (trailing != null) ...[const SizedBox(width: 8), trailing!] else if (onTap != null) ...[
              const SizedBox(width: 4),
              Icon(Icons.chevron_right_rounded, color: p.faint),
            ],
          ]),
        ),
      ),
    );
  }
}

/// Inline result banner (success / failure) with a soft background.
class ResultBanner extends StatelessWidget {
  const ResultBanner({super.key, required this.ok, required this.title, this.lines = const []});
  final bool ok;
  final String title;
  final List<String> lines;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final c = ok ? p.good : p.bad;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: c.withValues(alpha: 0.09), borderRadius: BorderRadius.circular(16), border: Border.all(color: c.withValues(alpha: 0.25))),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(ok ? Icons.check_circle_rounded : Icons.error_outline_rounded, color: c, size: 22),
        const SizedBox(width: 10),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600, color: c)),
            for (final l in lines) ...[
              const SizedBox(height: 4),
              Text(l, style: TextStyle(fontSize: 14, color: p.text, height: 1.45)),
            ],
          ]),
        ),
      ]),
    );
  }
}

/// Explains test results of a provider connection.
List<String> providerTestLines(ProviderTestResult r) => [
      if (r.ok) '共 ${r.modelCount} 个模型${r.chatModelCount != r.modelCount ? '，其中 ${r.chatModelCount} 个可以聊天' : ''}，耗时 ${r.latencyMs} 毫秒',
      if (!r.ok) r.message,
      if (r.keyUsage != null) '这个 Key 已经用了 ${fmtUsd(r.keyUsage!)}${r.keyLimit != null ? '，限额 ${fmtUsd(r.keyLimit!)}' : '，没有设置限额'}',
    ];

/// Gradient primary button used at the bottom of forms.
class PrimaryButton extends StatelessWidget {
  const PrimaryButton({super.key, required this.label, this.onTap, this.busy = false, this.icon});
  final String label;
  final VoidCallback? onTap;
  final bool busy;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final enabled = onTap != null && !busy;
    return Pressable(
      scale: 0.98,
      onTap: enabled ? onTap : null,
      child: AnimatedOpacity(
        duration: const Duration(milliseconds: 150),
        opacity: enabled || busy ? 1 : 0.45,
        child: Container(
          height: 54,
          decoration: BoxDecoration(
            gradient: Palette.brandGradient,
            borderRadius: BorderRadius.circular(18),
            boxShadow: enabled ? [BoxShadow(color: Palette.brand.withValues(alpha: 0.28), blurRadius: 16, offset: const Offset(0, 5))] : null,
          ),
          alignment: Alignment.center,
          child: busy
              ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.6, color: Colors.white))
              : Row(mainAxisSize: MainAxisSize.min, children: [
                  if (icon != null) ...[Icon(icon, color: Colors.white, size: 20), const SizedBox(width: 6)],
                  Text(label, style: const TextStyle(color: Colors.white, fontSize: 17, fontWeight: FontWeight.w700)),
                ]),
        ),
      ),
    );
  }
}

/// Bottom bar that sits above the system navigation bar.
class BottomBar extends StatelessWidget {
  const BottomBar({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      decoration: BoxDecoration(color: p.bg, border: Border(top: BorderSide(color: p.line))),
      padding: EdgeInsets.fromLTRB(16, 12, 16, 12 + MediaQuery.paddingOf(context).bottom),
      child: child,
    );
  }
}

/// Segmented control with an optional caption under each option.
class Choice<T> extends StatelessWidget {
  const Choice({super.key, required this.value, required this.options, required this.onChanged});
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
      child: Row(children: [
        for (final (v, label) in options)
          Expanded(
            child: Pressable(
              onTap: () => onChanged(v),
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 180),
                height: 42,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: v == value ? p.surface : Colors.transparent,
                  borderRadius: BorderRadius.circular(11),
                  boxShadow: v == value ? [BoxShadow(color: p.shadow, blurRadius: 8, offset: const Offset(0, 2))] : null,
                ),
                padding: const EdgeInsets.symmetric(horizontal: 4),
                // Shrink rather than cut off long labels at large font sizes.
                child: FittedBox(
                  fit: BoxFit.scaleDown,
                  child: Text(label,
                      maxLines: 1,
                      style: TextStyle(fontSize: 14.5, fontWeight: v == value ? FontWeight.w700 : FontWeight.w500, color: v == value ? p.accent : p.muted)),
                ),
              ),
            ),
          ),
      ]),
    );
  }
}

/// Page-level loading / error placeholders.
class LoadingView extends StatelessWidget {
  const LoadingView({super.key, this.text});
  final String? text;
  @override
  Widget build(BuildContext context) => Center(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          const CircularProgressIndicator(),
          if (text != null) ...[const SizedBox(height: 16), Text(text!, style: TextStyle(color: context.pal.muted))],
        ]),
      );
}

class ErrorView extends StatelessWidget {
  const ErrorView({super.key, required this.message, this.onRetry, this.extra});
  final String message;
  final VoidCallback? onRetry;
  final Widget? extra;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          const Mascot(size: 96, thinking: true),
          const SizedBox(height: 16),
          Text(message, textAlign: TextAlign.center, style: TextStyle(fontSize: 16, color: p.text, height: 1.5)),
          const SizedBox(height: 20),
          if (onRetry != null) FilledButton.tonal(onPressed: onRetry, child: const Text('重试')),
          ?extra,
        ]),
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/admin_models.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

// Shared building blocks for the admin "models & providers" and "voice input" screens.

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

/// Soft warm-tinted squircle badge standing in for a provider's mark.
class ProviderIcon extends StatelessWidget {
  const ProviderIcon({super.key, required this.kind, this.size = 44, this.accentOverride, this.muted = false});
  final String kind;
  final double size;
  final Color? accentOverride;
  final bool muted;

  static const _styles = <String, (Color, IconData)>{
    'openrouter': (Color(0xFF6B66D6), AppIcons.route),
    'openai': (Color(0xFF2B8A73), AppIcons.aperture),
    'gemini': (Color(0xFF4A78D0), AppIcons.sparkles),
    'deepseek': (Color(0xFF4768B5), AppIcons.waves),
    'dashscope': (Color(0xFFE06D3B), AppIcons.cloud),
    'siliconflow': (Color(0xFF7B5CE6), AppIcons.cpu),
    'tencent': (Color(0xFF3D6EC9), AppIcons.waveform),
    'xfyun': (Color(0xFF4878B8), AppIcons.micOff),
  };

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final style = _styles[kind];
    final tone = muted ? p.muted : (accentOverride ?? style?.$1 ?? p.accent);
    final bg = muted
        ? p.surfaceAlt
        : (tone == p.accent ? p.accentSoft : Color.lerp(p.surfaceAlt, tone, isDark ? 0.24 : 0.14)!);
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(size * 0.30),
        border: size >= 32
            ? Border.all(color: muted ? p.line : tone.withValues(alpha: isDark ? 0.26 : 0.16))
            : null,
      ),
      child: Icon(style?.$2 ?? AppIcons.hub, color: tone, size: size * 0.50),
    );
  }
}

/// Small rounded label: warm neutral sand by default; semantic tint when [color] is given.
class Tag extends StatelessWidget {
  const Tag(this.text, {super.key, this.color, this.icon, this.filled = true});
  final String text;
  final Color? color;
  final IconData? icon;
  final bool filled;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final isSemantic = color != null && color != p.muted;
    final fg = color ?? p.muted;
    final bg = !filled
        ? Colors.transparent
        : (!isSemantic
            ? (isDark ? const Color(0xFF2B2522) : p.surfaceAlt)
            : (fg == p.accent ? p.accentSoft : Color.lerp(p.surfaceAlt, fg, isDark ? 0.24 : 0.15)!));
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(8),
        border: filled ? null : Border.all(color: fg.withValues(alpha: 0.35)),
      ),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        if (icon != null) ...[
          Icon(icon, size: 13, color: fg.withValues(alpha: isSemantic ? 0.95 : 0.85)),
          const SizedBox(width: 3.5),
        ],
        Text(
          text,
          style: TextStyle(
            fontSize: 12,
            color: fg,
            fontWeight: isSemantic ? FontWeight.w600 : FontWeight.w500,
            height: 1.2,
          ),
        ),
      ]),
    );
  }
}

/// Capability tags shared by model rows — unified warm neutral style to avoid rainbow clutter.
List<Widget> modelTags(BuildContext context, {required bool vision, required bool think, int contextWindow = 0, double inPrice = 0, double outPrice = 0}) {
  return [
    if (vision) const Tag('能看图', icon: AppIcons.image),
    if (think) const Tag('深度思考', icon: AppIcons.thinkOff),
    if (contextWindow > 0) Tag(fmtContext(contextWindow)),
    if (inPrice > 0 || outPrice > 0) Tag(fmtPrice(inPrice, outPrice)),
  ];
}

/// Type scale aligned with [SettingsPage]: calm muted section headers, gentle w500 row titles.
abstract final class AdminText {
  static const titleGap = 4.0;
  static TextStyle section(Palette p) => TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.muted, height: 1.35);
  static TextStyle title(Palette p) => TextStyle(fontSize: 16.5, fontWeight: FontWeight.w500, color: p.text, height: 1.32);
  static TextStyle desc(Palette p) => TextStyle(fontSize: 13.5, color: p.muted, height: 1.45);
}

/// A setting's name with an optional explanation underneath.
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

/// Switch row for a GroupCard, same rhythm as [InfoRow].
class SwitchRow extends StatelessWidget {
  const SwitchRow({super.key, required this.title, this.desc, this.icon, required this.value, required this.onChanged});
  final String title;
  final String? desc;
  final IconData? icon;
  final bool value;
  final ValueChanged<bool>? onChanged;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return InkWell(
      onTap: onChanged == null ? null : () => onChanged!(!value),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 14, 14, 14),
        child: Row(children: [
          if (icon != null) ...[
            Icon(icon, color: p.accent, size: 22),
            const SizedBox(width: 14),
          ],
          Expanded(child: TitleDesc(title, desc: desc)),
          const SizedBox(width: 12),
          Transform.scale(
            scale: 0.88,
            child: Switch(value: value, onChanged: onChanged),
          ),
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
      padding: const EdgeInsets.fromLTRB(8, 24, 8, 8),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Expanded(child: Text(title, style: AdminText.section(p))),
          ?trailing,
        ]),
        if (subtitle != null && subtitle!.isNotEmpty) ...[
          const SizedBox(height: 3),
          Text(subtitle!, style: TextStyle(fontSize: 12.5, color: p.muted.withValues(alpha: 0.88), height: 1.45)),
        ],
      ]),
    );
  }
}

/// Compact action link or soft pill button for [SectionHeader.trailing].
class SectionAction extends StatelessWidget {
  const SectionAction({super.key, required this.label, this.icon, required this.onTap, this.filled = false});
  final String label;
  final IconData? icon;
  final VoidCallback onTap;
  final bool filled;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.96,
      onTap: onTap,
      child: Container(
        padding: filled ? const EdgeInsets.symmetric(horizontal: 11, vertical: 5) : const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
        decoration: filled
            ? BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(999))
            : null,
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          if (icon != null) ...[
            Icon(icon, size: 15, color: p.accent),
            const SizedBox(width: 3.5),
          ],
          Text(label, style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: p.accent)),
        ]),
      ),
    );
  }
}

/// White rounded card that groups rows, matching [SettingsPage] section cards.
class GroupCard extends StatelessWidget {
  const GroupCard({super.key, required this.children, this.padding, this.dividerIndent});
  final List<Widget> children;
  final EdgeInsetsGeometry? padding;
  final double? dividerIndent;

  static bool _hasLeadingIcon(Widget w) =>
      (w is InfoRow && w.icon != null) || (w is SwitchRow && w.icon != null);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final items = <Widget>[];
    for (var i = 0; i < children.length; i++) {
      if (i > 0) {
        final indent = dividerIndent ?? (_hasLeadingIcon(children[i - 1]) ? 54.0 : 18.0);
        items.add(Divider(height: 1, thickness: 0.7, indent: indent, endIndent: 18, color: p.line.withValues(alpha: 0.75)));
      }
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

/// A tappable settings row: icon, title, optional subtitle and a soft muted value on the right.
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
        constraints: const BoxConstraints(minHeight: 58),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 13),
          child: Row(children: [
            if (icon != null) ...[Icon(icon, color: p.accent, size: 22), const SizedBox(width: 14)],
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
                constraints: const BoxConstraints(maxWidth: 168),
                child: Text(
                  value!,
                  textAlign: TextAlign.right,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 14.5, color: valueColor ?? p.muted, fontWeight: FontWeight.w400),
                ),
              ),
            ],
            if (trailing != null) ...[
              const SizedBox(width: 8),
              trailing!,
            ] else if (onTap != null) ...[
              const SizedBox(width: 4),
              Icon(AppIcons.chevronRight, color: p.faint, size: 18),
            ],
          ]),
        ),
      ),
    );
  }
}

/// Soft borderless form input used inside [GroupCard]s so inputs feel warm and integrated.
class AdminField extends StatelessWidget {
  const AdminField({
    super.key,
    required this.controller,
    required this.label,
    this.statusText,
    this.statusColor,
    this.hint,
    this.helper,
    this.prefixText,
    this.prefixIcon,
    this.suffix,
    this.obscureText = false,
    this.mono = false,
    this.number = false,
    this.decimal = false,
    this.keyboardType,
    this.maxLength,
  });

  final TextEditingController controller;
  final String label;
  final String? statusText;
  final Color? statusColor;
  final String? hint;
  final String? helper;
  final String? prefixText;
  final IconData? prefixIcon;
  final Widget? suffix;
  final bool obscureText;
  final bool mono;
  final bool number;
  final bool decimal;
  final TextInputType? keyboardType;
  final int? maxLength;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final fill = isDark ? const Color(0xFF191614) : p.surfaceAlt;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Padding(
        padding: const EdgeInsets.only(left: 2, right: 2, bottom: 7),
        child: Row(children: [
          Expanded(child: Text(label, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: p.muted))),
          if (statusText != null && statusText!.isNotEmpty)
            Text(statusText!, style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500, color: statusColor ?? p.faint)),
        ]),
      ),
      TextField(
        controller: controller,
        maxLength: maxLength,
        obscureText: obscureText,
        autocorrect: !mono && !obscureText,
        enableSuggestions: !mono && !obscureText,
        keyboardType: keyboardType ?? (number || decimal ? TextInputType.numberWithOptions(decimal: decimal) : null),
        inputFormatters: number
            ? [FilteringTextInputFormatter.digitsOnly]
            : (decimal ? [FilteringTextInputFormatter.allow(RegExp(r'[0-9.]'))] : null),
        style: TextStyle(fontSize: 15.5, color: p.text, fontFamily: mono ? 'monospace' : null),
        decoration: InputDecoration(
          filled: true,
          fillColor: fill,
          hintText: hint,
          hintStyle: TextStyle(fontSize: 14.5, color: p.faint, fontFamily: mono ? 'monospace' : null),
          counterText: '',
          prefixText: prefixText,
          prefixStyle: TextStyle(fontSize: 15.5, color: p.muted),
          prefixIcon: prefixIcon == null ? null : Icon(prefixIcon, size: 19, color: p.faint),
          suffixIcon: suffix == null
              ? null
              : Padding(padding: const EdgeInsets.only(right: 6), child: suffix),
          suffixIconConstraints: const BoxConstraints(minHeight: 36, minWidth: 36),
          contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
          border: OutlineInputBorder(borderRadius: BorderRadius.circular(13), borderSide: BorderSide.none),
          enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(13), borderSide: BorderSide.none),
          focusedBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(13),
            borderSide: BorderSide(color: p.accent.withValues(alpha: 0.65), width: 1.3),
          ),
        ),
      ),
      if (helper != null && helper!.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 6, left: 2, right: 2),
          child: Text(helper!, style: TextStyle(fontSize: 12.5, color: p.muted.withValues(alpha: 0.9), height: 1.45)),
        ),
    ]);
  }
}

/// Compact pill button for inside [AdminField.suffix] (e.g. "粘贴").
class FieldActionPill extends StatelessWidget {
  const FieldActionPill({super.key, required this.label, required this.onTap});
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.95,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
        decoration: BoxDecoration(
          color: p.surface,
          borderRadius: BorderRadius.circular(9),
          border: Border.all(color: p.line),
        ),
        child: Text(label, style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.accent)),
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
      decoration: BoxDecoration(
        color: c.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: c.withValues(alpha: 0.22)),
      ),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(ok ? AppIcons.checkCircle : AppIcons.error, color: c, size: 20),
        const SizedBox(width: 10),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: c)),
            for (final l in lines) ...[
              const SizedBox(height: 4),
              Text(l, style: TextStyle(fontSize: 13.5, color: p.text.withValues(alpha: 0.88), height: 1.45)),
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
          height: 50,
          decoration: BoxDecoration(
            gradient: Palette.brandGradient,
            borderRadius: BorderRadius.circular(16),
            boxShadow: enabled
                ? [BoxShadow(color: Palette.brand.withValues(alpha: 0.20), blurRadius: 14, offset: const Offset(0, 4))]
                : null,
          ),
          alignment: Alignment.center,
          child: busy
              ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2.4, color: Colors.white))
              : Row(mainAxisSize: MainAxisSize.min, children: [
                  if (icon != null) ...[Icon(icon, color: Colors.white, size: 19), const SizedBox(width: 6)],
                  Text(label, style: const TextStyle(color: Colors.white, fontSize: 16, fontWeight: FontWeight.w600)),
                ]),
        ),
      ),
    );
  }
}

/// Soft warm secondary button paired with [PrimaryButton].
class SecondaryButton extends StatelessWidget {
  const SecondaryButton({super.key, required this.label, this.onTap, this.busy = false, this.icon});
  final String label;
  final VoidCallback? onTap;
  final bool busy;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final enabled = onTap != null && !busy;
    return Pressable(
      scale: 0.98,
      onTap: enabled ? onTap : null,
      child: AnimatedOpacity(
        duration: const Duration(milliseconds: 150),
        opacity: enabled || busy ? 1 : 0.45,
        child: Container(
          height: 50,
          padding: const EdgeInsets.symmetric(horizontal: 18),
          decoration: BoxDecoration(
            color: p.surface,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(color: p.line),
          ),
          alignment: Alignment.center,
          child: busy
              ? SizedBox(width: 19, height: 19, child: CircularProgressIndicator(strokeWidth: 2.2, color: p.accent))
              : Row(mainAxisSize: MainAxisSize.min, children: [
                  if (icon != null) ...[Icon(icon, color: p.accent, size: 18), const SizedBox(width: 6)],
                  Text(label, style: TextStyle(color: p.text, fontSize: 15.5, fontWeight: FontWeight.w500)),
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
      decoration: BoxDecoration(
        color: p.bg,
        border: Border(top: BorderSide(color: p.line.withValues(alpha: 0.8))),
      ),
      padding: EdgeInsets.fromLTRB(16, 10, 16, 12 + MediaQuery.paddingOf(context).bottom),
      child: child,
    );
  }
}

/// Segmented control matching [SettingsPage]'s appearance selector.
class Choice<T> extends StatelessWidget {
  const Choice({super.key, required this.value, required this.options, required this.onChanged, this.enabled = true});
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T> onChanged;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Opacity(
      opacity: enabled ? 1 : 0.55,
      child: Container(
        padding: const EdgeInsets.all(4),
        decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
        child: Row(children: [
          for (final (v, label) in options)
            Expanded(
              child: Pressable(
                onTap: enabled ? () => onChanged(v) : null,
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 180),
                  height: 40,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: v == value ? p.surface : Colors.transparent,
                    borderRadius: BorderRadius.circular(11),
                    boxShadow: v == value ? [BoxShadow(color: p.shadow, blurRadius: 8, offset: const Offset(0, 2))] : null,
                  ),
                  padding: const EdgeInsets.symmetric(horizontal: 6),
                  child: FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text(
                      label,
                      maxLines: 1,
                      style: TextStyle(
                        fontSize: 14.5,
                        fontWeight: v == value ? FontWeight.w600 : FontWeight.w500,
                        color: v == value ? p.accent : p.muted,
                      ),
                    ),
                  ),
                ),
              ),
            ),
        ]),
      ),
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

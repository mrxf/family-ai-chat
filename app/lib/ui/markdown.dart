import 'package:flutter/material.dart';
import 'package:gpt_markdown/gpt_markdown.dart';
import 'package:url_launcher/url_launcher.dart';

import 'theme.dart';

/// Markdown renderer for assistant replies. While [streaming] the package's
/// incremental parser + fade reveal keeps long answers smooth.
class Md extends StatelessWidget {
  const Md(this.text, {super.key, this.streaming = false, this.animate = false});
  final String text;

  /// More text may still arrive.
  final bool streaming;

  /// Use the reveal pipeline. Stays true after a reply finishes streaming so the
  /// widget tree doesn't change at the end (which would flash).
  final bool animate;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final base = Theme.of(context).textTheme.bodyLarge!;
    final dark = Theme.of(context).brightness == Brightness.dark;
    TextStyle h(double size) => base.copyWith(fontSize: size, fontWeight: FontWeight.w700, height: 1.4);

    return GptMarkdownTheme(
      gptThemeData: GptMarkdownThemeData(
        brightness: Theme.of(context).brightness,
        h1: h(23),
        h2: h(21),
        h3: h(19),
        h4: h(18),
        h5: h(17),
        h6: h(17),
        linkColor: p.accent,
        linkHoverColor: p.accent,
        hrLineColor: p.line,
        highlightColor: p.codeBg,
      ),
      child: GptMarkdown(
        text,
        style: base,
        useDollarSignsForLatex: true,
        animation: animate ? GptMarkdownAnimation.fade : GptMarkdownAnimation.none,
        blockAnimation: animate ? GptMarkdownBlockAnimation.fadeIn : GptMarkdownBlockAnimation.none,
        isStreaming: streaming,
        charactersPerSecond: 90,
        revealFadeSeconds: 0.3,
        onLinkTap: (url, _) {
          final uri = Uri.tryParse(url);
          if (uri != null) launchUrl(uri, mode: LaunchMode.externalApplication);
        },
        inlineCodeStyle: InlineCodeStyle(
          backgroundColor: p.codeBg,
          color: dark ? const Color(0xFFFFB38A) : const Color(0xFFC2410C),
          fontSizeFactor: 0.9,
          borderRadius: const Radius.circular(6),
        ),
        styleSheet: GptMarkdownStyleSheet(
          blockSpacing: 12,
          heading: const HeadingStyle(showDivider: false),
          codeBlock: CodeBlockStyle(
            backgroundColor: p.preBg,
            textColor: p.preText,
            borderRadius: const Radius.circular(14),
            fontSize: 14,
            showLanguageLabel: true,
          ),
          table: TableStyle(
            borderColor: p.line,
            borderRadius: const Radius.circular(12),
            headerBackground: p.surfaceAlt,
            cellPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
          ),
          blockQuote: BlockQuoteStyle(barColor: p.accent.withValues(alpha: 0.5), barWidth: 3),
          list: ListStyle(bulletColor: p.accent),
        ),
      ),
    );
  }
}

import 'dart:io';
import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../chat/components/chat_components.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

/// Colour themes the user can pick for the poster.
enum SharePosterStyle {
  warm('暖阳', Color(0xFFFFF6EE), Color(0xFFFF7A45)),
  apricot('晴杏', Color(0xFFFFDFC8), Color(0xFFF25A2A)),
  night('夜阑', Color(0xFF1F1A17), Color(0xFFFF8B57));

  const SharePosterStyle(this.label, this.swatchBg, this.swatchDot);
  final String label;
  final Color swatchBg;
  final Color swatchDot;
}

// ============================================================================
// XiaoNuan Branded Poster Card (Rendered to High-Res PNG)
// ============================================================================

class _PosterTokens {
  const _PosterTokens({
    required this.canvasGradient,
    required this.glowPrimary,
    required this.glowSecondary,
    required this.sparkleColor,
    required this.cardBg,
    required this.cardBorder,
    required this.cardShadow,
    required this.headerText,
    required this.headerSub,
    required this.badgeBg,
    required this.badgeText,
    required this.haloColor,
    required this.sealBorder,
    required this.sealText,
    required this.markdownBrightness,
    required this.palette,
  });

  final LinearGradient canvasGradient;
  final Color glowPrimary;
  final Color glowSecondary;
  final Color sparkleColor;
  final Color cardBg;
  final Color cardBorder;
  final Color cardShadow;
  final Color headerText;
  final Color headerSub;
  final Color badgeBg;
  final Color badgeText;
  final Color haloColor;
  final Color sealBorder;
  final Color sealText;
  final Brightness markdownBrightness;
  final Palette palette;

  static _PosterTokens of(SharePosterStyle style) => switch (style) {
        SharePosterStyle.warm => const _PosterTokens(
            canvasGradient: LinearGradient(
              begin: Alignment.topLeft,
              end: Alignment.bottomRight,
              colors: [Color(0xFFFFF8F2), Color(0xFFFFEFE3), Color(0xFFFFF6EE)],
              stops: [0.0, 0.52, 1.0],
            ),
            glowPrimary: Color(0x36FF8A54),
            glowSecondary: Color(0x28FFB580),
            sparkleColor: Color(0x55FF7A45),
            cardBg: Color(0xFFFFFFFF),
            cardBorder: Color(0xFFF3E2D4),
            cardShadow: Color(0x169E4E20),
            headerText: Color(0xFF2B2019),
            headerSub: Color(0xFF8A7567),
            badgeBg: Color(0xFFFFEAE0),
            badgeText: Color(0xFFE05929),
            haloColor: Color(0x24FF7A45),
            sealBorder: Color(0x78EB6234),
            sealText: Color(0xFFDF5828),
            markdownBrightness: Brightness.light,
            palette: Palette.light,
          ),
        SharePosterStyle.apricot => const _PosterTokens(
            canvasGradient: LinearGradient(
              begin: Alignment.topLeft,
              end: Alignment.bottomRight,
              colors: [Color(0xFFFFDEC7), Color(0xFFFFECDD), Color(0xFFFFDFC8)],
              stops: [0.0, 0.5, 1.0],
            ),
            glowPrimary: Color(0x45FF6E38),
            glowSecondary: Color(0x36FFA76B),
            sparkleColor: Color(0x66E85825),
            cardBg: Color(0xFFFFFCFA),
            cardBorder: Color(0xFFF4D4BD),
            cardShadow: Color(0x1F8F3D14),
            headerText: Color(0xFF331F14),
            headerSub: Color(0xFF7D5D4B),
            badgeBg: Color(0x99FFFFFF),
            badgeText: Color(0xFFD94E1F),
            haloColor: Color(0x38FFFFFF),
            sealBorder: Color(0x88D94E1F),
            sealText: Color(0xFFD94E1F),
            markdownBrightness: Brightness.light,
            palette: Palette.light,
          ),
        SharePosterStyle.night => const _PosterTokens(
            canvasGradient: LinearGradient(
              begin: Alignment.topLeft,
              end: Alignment.bottomRight,
              colors: [Color(0xFF1E1917), Color(0xFF161311), Color(0xFF221B17)],
              stops: [0.0, 0.55, 1.0],
            ),
            glowPrimary: Color(0x2EFF824A),
            glowSecondary: Color(0x1FFFA463),
            sparkleColor: Color(0x55FF9462),
            cardBg: Color(0xFF231E1C),
            cardBorder: Color(0xFF382F2B),
            cardShadow: Color(0x4D000000),
            headerText: Color(0xFFF6EFE9),
            headerSub: Color(0xFFA6978D),
            badgeBg: Color(0xFF36251C),
            badgeText: Color(0xFFFF9868),
            haloColor: Color(0x24FF8B57),
            sealBorder: Color(0x75FF8B57),
            sealText: Color(0xFFFF9868),
            markdownBrightness: Brightness.dark,
            palette: Palette.dark,
          ),
      };
}

class SharePosterCard extends StatelessWidget {
  const SharePosterCard({
    super.key,
    required this.messages,
    this.style = SharePosterStyle.warm,
    this.conversationTitle,
    this.assistant,
    this.api,
    this.now,
  });

  final List<Message> messages;
  final SharePosterStyle style;
  final String? conversationTitle;
  final Assistant? assistant;
  final Api? api;
  final DateTime? now;

  static String _formatDate(DateTime d) {
    final m = d.month.toString().padLeft(2, '0');
    final day = d.day.toString().padLeft(2, '0');
    return '${d.year}.$m.$day';
  }

  @override
  Widget build(BuildContext context) {
    final t = _PosterTokens.of(style);
    final pal = t.palette;
    final outerBody = Theme.of(context).textTheme.bodyMedium;
    final baseCardTheme = buildTheme(t.markdownBrightness);
    final cardTheme = outerBody?.fontFamily == null
        ? baseCardTheme
        : baseCardTheme.copyWith(
            textTheme: baseCardTheme.textTheme.apply(
              fontFamily: outerBody!.fontFamily,
              fontFamilyFallback: outerBody.fontFamilyFallback,
            ),
          );
    final assistantName = assistant?.name ?? '小暖';
    final headline = (conversationTitle != null && conversationTitle!.isNotEmpty)
        ? conversationTitle!
        : (assistant == null ? '与小暖的温暖对话' : '与$assistantName的对话');
    final dateStr = _formatDate(now ?? DateTime.now());

    return Theme(
      data: cardTheme,
      child: MediaQuery(
        data: MediaQuery.of(context).copyWith(textScaler: TextScaler.noScaling),
        child: SizedBox(
          width: 384,
          child: DecoratedBox(
            decoration: BoxDecoration(gradient: t.canvasGradient),
            child: CustomPaint(
              painter: _PosterBackdropPainter(
                glowPrimary: t.glowPrimary,
                glowSecondary: t.glowSecondary,
                sparkleColor: t.sparkleColor,
              ),
              child: Padding(
                padding: const EdgeInsets.fromLTRB(18, 22, 18, 20),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    // ---------------- Brand Header ----------------
                    Padding(
                      padding: const EdgeInsets.fromLTRB(4, 0, 2, 16),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.center,
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Wrap(
                                  spacing: 8,
                                  runSpacing: 6,
                                  crossAxisAlignment: WrapCrossAlignment.center,
                                  children: [
                                    Container(
                                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                                      decoration: BoxDecoration(
                                        color: t.badgeBg,
                                        borderRadius: BorderRadius.circular(20),
                                      ),
                                      child: Row(
                                        mainAxisSize: MainAxisSize.min,
                                        children: [
                                          Container(
                                            width: 6.5,
                                            height: 6.5,
                                            decoration: BoxDecoration(color: t.badgeText, shape: BoxShape.circle),
                                          ),
                                          const SizedBox(width: 6),
                                          Flexible(
                                            child: Text(
                                              assistant == null
                                                  ? '小暖 · AI 对话分享'
                                                  : '小暖 · ${assistant!.avatar.isNotEmpty ? '${assistant!.avatar} ' : ''}${assistant!.name}',
                                              maxLines: 1,
                                              overflow: TextOverflow.ellipsis,
                                              style: TextStyle(
                                                fontSize: 12,
                                                fontWeight: FontWeight.w600,
                                                color: t.badgeText,
                                                letterSpacing: 0.2,
                                              ),
                                            ),
                                          ),
                                        ],
                                      ),
                                    ),
                                    Text(
                                      dateStr,
                                      style: TextStyle(fontSize: 12, color: t.headerSub, fontWeight: FontWeight.w500),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 11),
                                Text(
                                  headline,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(
                                    fontSize: 20.5,
                                    fontWeight: FontWeight.w700,
                                    height: 1.32,
                                    color: t.headerText,
                                    letterSpacing: 0.2,
                                  ),
                                ),
                                const SizedBox(height: 8),
                                Container(
                                  width: 30,
                                  height: 3.5,
                                  decoration: BoxDecoration(
                                    gradient: Palette.brandGradient,
                                    borderRadius: BorderRadius.circular(2),
                                  ),
                                ),
                              ],
                            ),
                          ),
                          const SizedBox(width: 12),
                          // XiaoNuan Mascot Hero Emblem
                          SizedBox(
                            width: 76,
                            height: 76,
                            child: Stack(
                              alignment: Alignment.center,
                              children: [
                                Container(
                                  width: 72,
                                  height: 72,
                                  decoration: BoxDecoration(
                                    shape: BoxShape.circle,
                                    color: t.haloColor,
                                    border: Border.all(color: t.sparkleColor.withValues(alpha: 0.22)),
                                  ),
                                ),
                                Image.asset(
                                  'assets/brand/mascot.png',
                                  width: 64,
                                  height: 64,
                                  filterQuality: FilterQuality.high,
                                  gaplessPlayback: true,
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),

                    // ---------------- Inner Dialogue Paper Sheet ----------------
                    Container(
                      padding: const EdgeInsets.fromLTRB(17, 18, 17, 20),
                      decoration: BoxDecoration(
                        color: t.cardBg,
                        borderRadius: BorderRadius.circular(24),
                        border: Border.all(color: t.cardBorder),
                        boxShadow: [
                          BoxShadow(color: t.cardShadow, blurRadius: 20, offset: const Offset(0, 8)),
                        ],
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          if (messages.isNotEmpty && !messages.first.isUser)
                            Padding(
                              padding: const EdgeInsets.only(bottom: 6),
                              child: AppIcon(AppIcons.quotes, size: 22, color: pal.accent.withValues(alpha: 0.4)),
                            ),
                          for (var i = 0; i < messages.length; i++) ...[
                            if (i > 0) _buildTurnDivider(messages[i - 1], messages[i], pal),
                            _PosterMessageItem(
                              message: messages[i],
                              assistant: assistant,
                              palette: pal,
                              api: api,
                            ),
                          ],
                        ],
                      ),
                    ),

                    // ---------------- Signature XiaoNuan Brand Footer ----------------
                    Padding(
                      padding: const EdgeInsets.fromLTRB(6, 16, 6, 2),
                      child: Row(
                        children: [
                          Container(
                            width: 42,
                            height: 42,
                            decoration: BoxDecoration(
                              color: t.cardBg,
                              borderRadius: BorderRadius.circular(13),
                              border: Border.all(color: t.cardBorder),
                              boxShadow: [
                                BoxShadow(color: t.cardShadow, blurRadius: 8, offset: const Offset(0, 3)),
                              ],
                            ),
                            padding: const EdgeInsets.all(5),
                            child: Image.asset(
                              'assets/brand/mascot.png',
                              filterQuality: FilterQuality.high,
                              gaplessPlayback: true,
                            ),
                          ),
                          const SizedBox(width: 11),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  '小暖 · 家人的贴心 AI 伙伴',
                                  style: TextStyle(
                                    fontSize: 13.5,
                                    fontWeight: FontWeight.w700,
                                    color: t.headerText,
                                    letterSpacing: 0.2,
                                  ),
                                ),
                                const SizedBox(height: 2.5),
                                Text(
                                  '随时问小暖，事事有回响 · 共 ${messages.length} 条对话',
                                  style: TextStyle(fontSize: 11.5, color: t.headerSub),
                                ),
                              ],
                            ),
                          ),
                          const SizedBox(width: 8),
                          _WarmSealStamp(borderColor: t.sealBorder, textColor: t.sealText),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildTurnDivider(Message prev, Message curr, Palette pal) {
    final newRound = !prev.isUser && curr.isUser;
    if (!newRound) return const SizedBox(height: 14);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 16),
      child: CustomPaint(
        size: const Size(double.infinity, 1),
        painter: _HorizontalDashedLinePainter(color: pal.line),
      ),
    );
  }
}

class _PosterMessageItem extends StatelessWidget {
  const _PosterMessageItem({
    required this.message,
    required this.assistant,
    required this.palette,
    required this.api,
  });

  final Message message;
  final Assistant? assistant;
  final Palette palette;
  final Api? api;

  @override
  Widget build(BuildContext context) {
    if (message.isUser) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          if (message.attachments.isNotEmpty)
            Padding(
              padding: EdgeInsets.only(bottom: message.content.isEmpty ? 0 : 8),
              child: _PosterAttachments(attachments: message.attachments, api: api, alignEnd: true, palette: palette),
            ),
          if (message.content.isNotEmpty)
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 292),
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 15, vertical: 11),
                decoration: BoxDecoration(
                  color: palette.userBubble,
                  borderRadius: const BorderRadius.only(
                    topLeft: Radius.circular(20),
                    topRight: Radius.circular(20),
                    bottomLeft: Radius.circular(20),
                    bottomRight: Radius.circular(6),
                  ),
                ),
                child: Text(
                  message.content,
                  style: TextStyle(
                    fontSize: 15.5,
                    height: 1.52,
                    fontWeight: FontWeight.w500,
                    color: palette.userText,
                  ),
                ),
              ),
            ),
        ],
      );
    }

    final name = assistant?.name ?? '小暖';
    final emoji = assistant?.avatar ?? '';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            if (emoji.isNotEmpty)
              Avatar(label: name, emoji: emoji, size: 22)
            else
              Container(
                width: 22,
                height: 22,
                decoration: const BoxDecoration(gradient: Palette.brandGradient, shape: BoxShape.circle),
                child: const AppIcon(AppIcons.sparkles, size: 12.5, color: Colors.white),
              ),
            const SizedBox(width: 7),
            Flexible(
              child: Text(
                name,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: palette.muted),
              ),
            ),
            if (message.reasoning.isNotEmpty) ...[
              const SizedBox(width: 7),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                decoration: BoxDecoration(
                  color: palette.accentSoft,
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    AppIcon(AppIcons.thinkOff, size: 11.5, color: palette.accent),
                    const SizedBox(width: 3),
                    Text('深度思考', style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: palette.accent)),
                  ],
                ),
              ),
            ],
          ],
        ),
        if (message.content.isNotEmpty || message.components.isNotEmpty) ...[
          const SizedBox(height: 8),
          InterleavedAssistantBody(
            messageId: message.id,
            content: message.content,
            components: message.components,
            streaming: false,
            animate: false,
            isLastMessage: false,
          ),
        ],
        if (message.attachments.isNotEmpty) ...[
          SizedBox(height: (message.content.isEmpty && message.components.isEmpty) ? 8 : 10),
          _PosterAttachments(attachments: message.attachments, api: api, large: true, palette: palette),
        ],
      ],
    );
  }
}

/// Static attachment renderer for share posters (no Hero tags to avoid route conflicts).
class _PosterAttachments extends StatelessWidget {
  const _PosterAttachments({
    required this.attachments,
    required this.api,
    required this.palette,
    this.alignEnd = false,
    this.large = false,
  });

  final List<Attachment> attachments;
  final Api? api;
  final Palette palette;
  final bool alignEnd;
  final bool large;

  Widget _image(Attachment a) {
    final local = a.localPath;
    if (local != null && File(local).existsSync()) {
      return Image.file(File(local), fit: BoxFit.cover, gaplessPlayback: true);
    }
    if (api == null || a.fileId.isEmpty) {
      return Container(color: palette.surfaceAlt, child: AppIcon(AppIcons.image, color: palette.faint));
    }
    return Image.network(
      api!.fileUrl(a.fileId),
      headers: api!.authHeaders,
      fit: BoxFit.cover,
      gaplessPlayback: true,
      errorBuilder: (_, _, _) => Container(
        color: palette.surfaceAlt,
        child: AppIcon(AppIcons.brokenImage, color: palette.faint),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (large) {
      return Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          for (final a in attachments)
            ClipRRect(
              borderRadius: BorderRadius.circular(16),
              child: SizedBox(
                width: 300,
                height: (300 / a.aspect.clamp(0.55, 2.0)),
                child: _image(a),
              ),
            ),
        ],
      );
    }
    if (attachments.length == 1) {
      final a = attachments.first;
      final w = a.aspect >= 1 ? 200.0 : 150.0;
      return ClipRRect(
        borderRadius: BorderRadius.circular(14),
        child: SizedBox(
          width: w,
          height: (w / a.aspect).clamp(90.0, 240.0),
          child: _image(a),
        ),
      );
    }
    return Wrap(
      alignment: alignEnd ? WrapAlignment.end : WrapAlignment.start,
      spacing: 6,
      runSpacing: 6,
      children: [
        for (final a in attachments)
          ClipRRect(
            borderRadius: BorderRadius.circular(12),
            child: SizedBox(width: 82, height: 82, child: _image(a)),
          ),
      ],
    );
  }
}

/// Delicate 4-character warm seal stamp ("温暖 / 相伴") in the footer.
class _WarmSealStamp extends StatelessWidget {
  const _WarmSealStamp({required this.borderColor, required this.textColor});
  final Color borderColor;
  final Color textColor;

  @override
  Widget build(BuildContext context) {
    return Transform.rotate(
      angle: -0.06,
      child: Container(
        padding: const EdgeInsets.all(2.2),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: borderColor, width: 1.1),
        ),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 5.5, vertical: 3.5),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(5.5),
            border: Border.all(color: borderColor.withValues(alpha: 0.45), width: 0.7),
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text('温暖', style: TextStyle(fontSize: 9.5, height: 1.1, fontWeight: FontWeight.w700, color: textColor, letterSpacing: 1.2)),
              Text('相伴', style: TextStyle(fontSize: 9.5, height: 1.15, fontWeight: FontWeight.w700, color: textColor, letterSpacing: 1.2)),
            ],
          ),
        ),
      ),
    );
  }
}

class _PosterBackdropPainter extends CustomPainter {
  const _PosterBackdropPainter({
    required this.glowPrimary,
    required this.glowSecondary,
    required this.sparkleColor,
  });

  final Color glowPrimary;
  final Color glowSecondary;
  final Color sparkleColor;

  @override
  void paint(Canvas canvas, Size size) {
    // Top-right warm sun glow behind XiaoNuan mascot.
    final topCenter = Offset(size.width - 52, 54);
    canvas.drawCircle(
      topCenter,
      130,
      Paint()
        ..shader = ui.Gradient.radial(
          topCenter,
          130,
          [glowPrimary, glowPrimary.withValues(alpha: 0)],
        ),
    );

    // Bottom-left gentle ambient glow.
    final bottomCenter = Offset(44, size.height - 48);
    canvas.drawCircle(
      bottomCenter,
      150,
      Paint()
        ..shader = ui.Gradient.radial(
          bottomCenter,
          150,
          [glowSecondary, glowSecondary.withValues(alpha: 0)],
        ),
    );

    // Subtle 4-point sparkles around the header mascot.
    _drawSparkle(canvas, Offset(size.width - 104, 28), 5.5, sparkleColor);
    _drawSparkle(canvas, Offset(size.width - 22, 102), 4.2, sparkleColor.withValues(alpha: 0.45));
  }

  void _drawSparkle(Canvas canvas, Offset c, double r, Color color) {
    final path = Path()
      ..moveTo(c.dx, c.dy - r)
      ..quadraticBezierTo(c.dx, c.dy, c.dx + r, c.dy)
      ..quadraticBezierTo(c.dx, c.dy, c.dx, c.dy + r)
      ..quadraticBezierTo(c.dx, c.dy, c.dx - r, c.dy)
      ..quadraticBezierTo(c.dx, c.dy, c.dx, c.dy - r)
      ..close();
    canvas.drawPath(path, Paint()..color = color);
  }

  @override
  bool shouldRepaint(_PosterBackdropPainter old) =>
      old.glowPrimary != glowPrimary || old.glowSecondary != glowSecondary || old.sparkleColor != sparkleColor;
}

class _HorizontalDashedLinePainter extends CustomPainter {
  const _HorizontalDashedLinePainter({required this.color});
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 1.0
      ..strokeCap = StrokeCap.round;
    var x = 0.0;
    while (x < size.width) {
      canvas.drawLine(Offset(x, 0), Offset(math.min(x + 5, size.width), 0), paint);
      x += 9.5;
    }
  }

  @override
  bool shouldRepaint(_HorizontalDashedLinePainter old) => old.color != color;
}

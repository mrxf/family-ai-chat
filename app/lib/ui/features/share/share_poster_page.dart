import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart' show HapticFeedback, Uint8List;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../platform/media/image_saver.dart';
import '../../../platform/platform_capabilities.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import 'share_poster_card.dart';

/// Optional hook for widget tests to intercept generated poster PNG bytes.
@visibleForTesting
Future<void> Function(Uint8List pngBytes)? debugShareSaveOverride;

// ============================================================================
// Step 2: Poster Preview & One-Tap Save to Gallery
// ============================================================================

class SharePosterPage extends ConsumerStatefulWidget {
  const SharePosterPage({
    super.key,
    required this.messages,
    this.conversationTitle,
    this.assistant,
  });

  final List<Message> messages;
  final String? conversationTitle;
  final Assistant? assistant;

  @override
  ConsumerState<SharePosterPage> createState() => _SharePosterPageState();
}

class _SharePosterPageState extends ConsumerState<SharePosterPage> {
  final _posterKey = GlobalKey();
  SharePosterStyle _style = SharePosterStyle.warm;
  bool _saving = false;
  bool _justSaved = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    precacheImage(const AssetImage('assets/brand/mascot.png'), context).ignore();
  }

  Future<void> _savePoster() async {
    if (_saving) return;
    HapticFeedback.lightImpact();
    setState(() => _saving = true);
    try {
      if (SchedulerBinding.instance.schedulerPhase != SchedulerPhase.idle) {
        await WidgetsBinding.instance.endOfFrame;
      }
      final ro = _posterKey.currentContext?.findRenderObject();
      if (ro is! RenderRepaintBoundary) {
        if (mounted) showToast(context, '图片还在准备中，请再试一次');
        return;
      }
      final longestSide = math.max(ro.size.width, ro.size.height);
      final ratio = longestSide > 0 ? (10000.0 / longestSide).clamp(1.5, 3.0) : 3.0;
      final image = await ro.toImage(pixelRatio: ratio);
      final byteData = await image.toByteData(format: ui.ImageByteFormat.png);
      image.dispose();
      if (byteData == null) {
        if (mounted) showToast(context, '生成图片失败，请再试一次');
        return;
      }
      final bytes = byteData.buffer.asUint8List();

      final place = ref.read(platformCapabilitiesProvider).savedImagesPlace;
      if (debugShareSaveOverride != null) {
        await debugShareSaveOverride!(bytes);
      } else {
        final res = await ref.read(imageSaverProvider).save([bytes], namePrefix: 'xiaonuan_share');
        if (!res.ok) {
          if (mounted) {
            showToast(
              context,
              res.status == SaveImagesStatus.permissionDenied ? '没有$place权限，请在系统设置里允许小暖保存照片' : '保存失败，请再试一次',
            );
          }
          return;
        }
      }

      if (mounted) {
        HapticFeedback.mediumImpact();
        setState(() => _justSaved = true);
        showToast(context, '分享图片已保存到$place，可以去微信发给家人啦');
      }
    } catch (_) {
      if (mounted) showToast(context, '保存图片失败，请再试一次');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final api = ref.watch(sessionProvider).api;

    return Scaffold(
      appBar: AppBar(
        title: const Text('生成分享图片'),
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 2, 16, 10),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                for (final s in SharePosterStyle.values)
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 5),
                    child: _StyleChip(
                      style: s,
                      selected: _style == s,
                      onTap: () => setState(() {
                        _style = s;
                        _justSaved = false;
                      }),
                    ),
                  ),
              ],
            ),
          ),
          Expanded(
            child: SingleChildScrollView(
              physics: const BouncingScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(18, 4, 18, 22),
              child: Column(
                children: [
                  Center(
                    child: Semantics(
                      button: true,
                      label: '点击分享图片保存到本地',
                      child: Pressable(
                        scale: 0.992,
                        onTap: _saving ? null : _savePoster,
                        child: Container(
                          decoration: BoxDecoration(
                            borderRadius: BorderRadius.circular(26),
                            boxShadow: [
                              BoxShadow(
                                color: p.shadow.withValues(alpha: 0.22),
                                blurRadius: 28,
                                offset: const Offset(0, 12),
                              ),
                            ],
                          ),
                          child: ClipRRect(
                            borderRadius: BorderRadius.circular(26),
                            child: FittedBox(
                              fit: BoxFit.scaleDown,
                              child: RepaintBoundary(
                                key: _posterKey,
                                child: SharePosterCard(
                                  messages: widget.messages,
                                  style: _style,
                                  conversationTitle: widget.conversationTitle,
                                  assistant: widget.assistant,
                                  api: api,
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 14),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      AppIcon(AppIcons.download, size: 15, color: p.muted),
                      const SizedBox(width: 5),
                      Text('轻触上方图片或点击下方按钮，即可保存到本地', style: TextStyle(fontSize: 13, color: p.muted)),
                    ],
                  ),
                ],
              ),
            ),
          ),
          Container(
            decoration: BoxDecoration(
              color: p.surface,
              border: Border(top: BorderSide(color: p.line)),
              boxShadow: [BoxShadow(color: p.shadow, blurRadius: 18, offset: const Offset(0, -4))],
            ),
            child: SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(20, 12, 20, 12),
                child: SizedBox(
                  width: double.infinity,
                  child: Pressable(
                    onTap: _saving ? null : _savePoster,
                    child: Container(
                      height: 52,
                      decoration: BoxDecoration(
                        gradient: Palette.brandGradient,
                        borderRadius: BorderRadius.circular(26),
                        boxShadow: [
                          BoxShadow(
                            color: Palette.brand.withValues(alpha: 0.32),
                            blurRadius: 16,
                            offset: const Offset(0, 5),
                          ),
                        ],
                      ),
                      child: Row(
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: [
                          if (_saving)
                            const SizedBox(
                              width: 20,
                              height: 20,
                              child: CircularProgressIndicator(strokeWidth: 2.2, color: Colors.white),
                            )
                          else
                            AppIcon(_justSaved ? AppIcons.checkBold : AppIcons.download, size: 20, color: Colors.white),
                          const SizedBox(width: 8),
                          Text(
                            _saving
                                ? '正在生成高清长图…'
                                : _justSaved
                                    ? '已保存到${ref.watch(platformCapabilitiesProvider).savedImagesPlace}（点此再存一张）'
                                    : '保存图片到${ref.watch(platformCapabilitiesProvider).savedImagesPlace}',
                            style: const TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600, color: Colors.white),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _StyleChip extends StatelessWidget {
  const _StyleChip({required this.style, required this.selected, required this.onTap});
  final SharePosterStyle style;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.96,
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
        decoration: BoxDecoration(
          color: selected ? p.accentSoft : p.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(
            color: selected ? p.accent : p.line,
            width: selected ? 1.4 : 1.0,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 16,
              height: 16,
              decoration: BoxDecoration(
                color: style.swatchBg,
                shape: BoxShape.circle,
                border: Border.all(color: style.swatchDot.withValues(alpha: 0.65), width: 1.5),
              ),
              child: Center(
                child: Container(
                  width: 6,
                  height: 6,
                  decoration: BoxDecoration(color: style.swatchDot, shape: BoxShape.circle),
                ),
              ),
            ),
            const SizedBox(width: 7),
            Text(
              style.label,
              style: TextStyle(
                fontSize: 14.5,
                fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                color: selected ? p.accent : p.text,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

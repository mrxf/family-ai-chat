import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback, Uint8List;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/models/models.dart';
import '../../../../platform/media/image_saver.dart';
import '../../../../platform/platform_capabilities.dart';
import '../../../../state/session.dart';
import '../../../theme/icons.dart';
import '../../../theme/theme.dart';
import '../../../widgets/base_widgets.dart';
import '../../../widgets/mascot/mascot_anim.dart';

class ImageGrid extends ConsumerWidget {
  const ImageGrid({super.key, required this.attachments, this.alignEnd = false, this.large = false});
  final List<Attachment> attachments;
  final bool alignEnd;

  /// Pictures the assistant drew: shown big, they are the answer.
  final bool large;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.watch(sessionProvider).api;
    Widget image(Attachment a, {BoxFit fit = BoxFit.cover}) {
      final local = a.localPath;
      if (local != null && File(local).existsSync()) {
        return Image.file(File(local), fit: fit, gaplessPlayback: true, cacheWidth: 900);
      }
      if (api == null || a.fileId.isEmpty) return const SizedBox();
      return Image.network(api.fileUrl(a.fileId), headers: api.authHeaders, fit: fit, gaplessPlayback: true, cacheWidth: 900,
          errorBuilder: (_, _, _) => Container(color: context.pal.surfaceAlt, child: AppIcon(AppIcons.brokenImage, color: context.pal.faint)));
    }

    void open(int i) => Navigator.of(context).push(PageRouteBuilder(
          opaque: false,
          barrierColor: Colors.black,
          pageBuilder: (_, _, _) => ImageViewer(
            images: [for (final a in attachments) image(a, fit: BoxFit.contain)],
            initial: i,
            saveable: large ? attachments : null,
          ),
          transitionsBuilder: (_, anim, _, child) => FadeTransition(opacity: anim, child: child),
        ));

    if (large) {
      final w = drawnImageWidth(context);
      return Wrap(spacing: 8, runSpacing: 8, children: [
        for (var i = 0; i < attachments.length; i++)
          Pressable(
            scale: 0.98,
            onTap: () => open(i),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(20),
              child: SizedBox(
                width: w,
                height: w / attachments[i].aspect.clamp(0.5, 2.0),
                child: _FadeIn(child: image(attachments[i])),
              ),
            ),
          ),
      ]);
    }
    if (attachments.length == 1) {
      final a = attachments.first;
      final w = a.aspect >= 1 ? 240.0 : 180.0;
      return Pressable(
        scale: 0.98,
        onTap: () => open(0),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(18),
          child: SizedBox(width: w, height: (w / a.aspect).clamp(100.0, 320.0), child: Hero(tag: 'img-${a.fileId}-${a.localPath}', child: image(a))),
        ),
      );
    }
    return Wrap(
      alignment: alignEnd ? WrapAlignment.end : WrapAlignment.start,
      spacing: 6,
      runSpacing: 6,
      children: [
        for (var i = 0; i < attachments.length; i++)
          Pressable(
            scale: 0.97,
            onTap: () => open(i),
            child: ClipRRect(borderRadius: BorderRadius.circular(14), child: SizedBox(width: 92, height: 92, child: image(attachments[i]))),
          ),
      ],
    );
  }
}

double drawnImageWidth(BuildContext context) => (MediaQuery.sizeOf(context).width * 0.72).clamp(200.0, 340.0);

class _FadeIn extends StatefulWidget {
  const _FadeIn({required this.child});
  final Widget child;
  @override
  State<_FadeIn> createState() => _FadeInState();
}

class _FadeInState extends State<_FadeIn> {
  bool _shown = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) setState(() => _shown = true);
    });
  }

  @override
  Widget build(BuildContext context) =>
      AnimatedOpacity(opacity: _shown ? 1 : 0, duration: const Duration(milliseconds: 450), curve: Curves.easeOut, child: widget.child);
}

/// Placeholder while a picture is being drawn: a soft shimmering card with
/// the mascot and how long it has been, so a 30-second wait feels alive.
class DrawingCard extends StatefulWidget {
  const DrawingCard({super.key, required this.label, this.since, this.isPrivate = false});
  final String label;
  final DateTime? since;
  final bool isPrivate;

  @override
  State<DrawingCard> createState() => _DrawingCardState();
}

class _DrawingCardState extends State<DrawingCard> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1800))..repeat();
  final DateTime _created = DateTime.now();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final w = drawnImageWidth(context);
    return ClipRRect(
      borderRadius: BorderRadius.circular(20),
      child: SizedBox(
        width: w,
        height: w,
        child: AnimatedBuilder(
          animation: _c,
          builder: (_, child) {
            final t = _c.value;
            final secs = DateTime.now().difference(widget.since ?? _created).inSeconds.clamp(0, 9999);
            return DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment(-1.6 + 3.2 * t, -1),
                  end: Alignment(-0.6 + 3.2 * t, 1),
                  colors: [p.surfaceAlt, p.accentSoft, p.surfaceAlt],
                ),
              ),
              child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
                child!,
                const SizedBox(height: 14),
                Text(widget.label, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: p.text)),
                const SizedBox(height: 6),
                Text(secs < 3 ? '一般要半分钟左右' : '已经 $secs 秒，一般要半分钟左右', style: TextStyle(fontSize: 13, color: p.muted)),
                const SizedBox(height: 2),
                Text(
                  widget.isPrivate ? '隐私对话关闭后会清除，画好后可保存到相册' : '离开这个页面也没关系，画好会留在这里',
                  style: TextStyle(fontSize: 12, color: p.faint),
                ),
              ]),
            );
          },
          child: const AnimatedMascot(size: 64, motion: MascotMotion.thinking),
        ),
      ),
    );
  }
}

class ImageViewer extends ConsumerStatefulWidget {
  const ImageViewer({super.key, required this.images, this.initial = 0, this.saveable});
  final List<Widget> images;
  final int initial;

  /// Pictures that can be saved to the gallery (same order as [images]).
  final List<Attachment>? saveable;

  @override
  ConsumerState<ImageViewer> createState() => _ImageViewerState();
}

class _ImageViewerState extends ConsumerState<ImageViewer> {
  late final _pages = PageController(initialPage: widget.initial);
  late int _page = widget.initial;
  bool _saving = false;

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final list = widget.saveable;
    if (list == null || _saving) return;
    setState(() => _saving = true);
    await saveToGallery(context, ref, [list[_page]]);
    if (mounted) setState(() => _saving = false);
  }

  @override
  Widget build(BuildContext context) {
    final canSave = widget.saveable != null;
    return Scaffold(
      backgroundColor: Colors.black,
      body: Stack(children: [
        GestureDetector(
          onTap: () => Navigator.pop(context),
          onLongPress: canSave ? _save : null,
          child: PageView(
            controller: _pages,
            onPageChanged: (i) => setState(() => _page = i),
            children: [for (final img in widget.images) InteractiveViewer(maxScale: 5, child: Center(child: img))],
          ),
        ),
        if (canSave)
          Positioned(
            left: 0,
            right: 0,
            bottom: 24 + MediaQuery.paddingOf(context).bottom,
            child: Center(
              child: FilledButton.icon(
                style: FilledButton.styleFrom(
                  backgroundColor: Colors.white.withValues(alpha: 0.16),
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(horizontal: 26, vertical: 14),
                  textStyle: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
                  shape: const StadiumBorder(),
                ),
                onPressed: _saving ? null : _save,
                icon: _saving
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const AppIcon(AppIcons.download),
                label: Text('保存到${ref.watch(platformCapabilitiesProvider).savedImagesPlace}'),
              ),
            ),
          ),
      ]),
    );
  }
}

/// Saves pictures where this device keeps photos and tells the user where they went.
Future<void> saveToGallery(BuildContext context, WidgetRef ref, List<Attachment> list) async {
  HapticFeedback.lightImpact();
  final caps = ref.read(platformCapabilitiesProvider);
  final api = ref.read(sessionProvider).api;
  final images = <Uint8List>[];
  try {
    for (final a in list) {
      final local = a.localPath;
      if (local != null && File(local).existsSync()) {
        images.add(await File(local).readAsBytes());
      } else if (api != null && a.fileId.isNotEmpty) {
        images.add(await api.fileBytes(a.fileId));
      }
    }
  } catch (_) {
    if (context.mounted) showToast(context, '图片下载失败，请检查网络后再试');
    return;
  }
  if (images.isEmpty) return;
  final res = await ref.read(imageSaverProvider).save(images);
  if (!context.mounted) return;
  switch (res.status) {
    case SaveImagesStatus.saved:
      showToast(context, '${res.count > 1 ? '${res.count} 张图' : '已'}保存到${caps.savedImagesPlace}，可以去微信发给家人了');
    case SaveImagesStatus.permissionDenied:
      showToast(context, '没有${caps.savedImagesPlace}权限，请在系统设置里允许小暖保存照片');
    case SaveImagesStatus.failed:
      showToast(context, '保存失败，请再试一次');
  }
}

import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_image_compress/flutter_image_compress.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:path_provider/path_provider.dart';

import '../state/chat.dart';
import '../state/session.dart';
import 'icons.dart';
import 'sheets.dart';
import 'theme.dart';
import 'voice.dart';
import 'widgets.dart';

/// The input area: text, photos, deep-thinking toggle and send / stop.
class Composer extends ConsumerStatefulWidget {
  const Composer({super.key, this.focusNode});
  final FocusNode? focusNode;

  @override
  ConsumerState<Composer> createState() => ComposerState();
}

class ComposerState extends ConsumerState<Composer> {
  final _ctrl = TextEditingController();
  late final FocusNode _focus = widget.focusNode ?? FocusNode();
  final List<PickedImage> _images = [];
  bool _picking = false;
  Future<void> _shareQueue = Future.value();

  @override
  void initState() {
    super.initState();
    _ctrl.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _ctrl.dispose();
    if (widget.focusNode == null) _focus.dispose();
    super.dispose();
  }

  /// Used by suggestion chips on the welcome screen.
  void sendText(String text) {
    ref.read(chatProvider.notifier).send(text);
  }

  /// Dismisses focus and soft keyboard from the input field.
  void unfocus() => _focus.unfocus();

  /// Clears unsent text and removes any locally compressed draft images.
  void clearDraft() {
    for (final img in _images) {
      if (img.path.isNotEmpty) {
        File(img.path).delete().catchError((_) => File(img.path));
      }
    }
    _ctrl.clear();
    if (_images.isNotEmpty && mounted) setState(() => _images.clear());
  }

  /// Populates the input box with text and/or images shared from external apps without auto-sending.
  Future<void> applyShared({
    String text = '',
    List<String> imagePaths = const [],
    int totalImages = 0,
    int failedImages = 0,
  }) =>
      _shareQueue = _shareQueue.then(
        (_) => _applySharedNow(text: text, imagePaths: imagePaths, totalImages: totalImages, failedImages: failedImages),
      );

  Future<void> _applySharedNow({
    required String text,
    required List<String> imagePaths,
    required int totalImages,
    required int failedImages,
  }) async {
    if (!mounted) return;
    final trimmed = text.trim();
    if (trimmed.isNotEmpty) {
      final existing = _ctrl.text.trim();
      final merged = existing.isEmpty ? trimmed : '$existing\n$trimmed';
      _ctrl.value = TextEditingValue(
        text: merged,
        selection: TextSelection.collapsed(offset: merged.length),
      );
    }

    var added = 0;
    var decodeFailed = failedImages;
    final room = 9 - _images.length;
    final overflow = totalImages > room || imagePaths.length > room;
    for (final path in imagePaths.take(room)) {
      try {
        final img = await _compress(path);
        if (img != null && mounted) {
          setState(() => _images.add(img));
          added++;
        } else {
          decodeFailed++;
        }
      } catch (_) {
        decodeFailed++;
      } finally {
        if (path.contains('shared_incoming')) {
          File(path).delete().catchError((_) => File(path));
        }
      }
    }
    for (final extra in imagePaths.skip(room)) {
      if (extra.contains('shared_incoming')) {
        File(extra).delete().catchError((_) => File(extra));
      }
    }

    if (!mounted) return;
    if (overflow) {
      showToast(context, '最多同时发 9 张图片，已选取前 9 张');
    } else if (added == 0 && decodeFailed > 0) {
      showToast(context, '没能读取分享过来的图片');
    } else if (added > 0) {
      final chat = ref.read(chatProvider);
      final boot = ref.read(sessionProvider).boot;
      final model = boot?.effectiveModel(chat.modelId, chat.assistantId);
      if (model != null && !model.vision) {
        showToast(context, '当前模型「${model.displayName}」看不了图片，点顶部可切换模型');
      }
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _focus.requestFocus();
    });
  }

  void _send() {
    final text = _ctrl.text.trim();
    if (text.isEmpty && _images.isEmpty) return;
    final chat = ref.read(chatProvider);
    if (chat.busy) return;
    final boot = ref.read(sessionProvider).boot;
    final model = boot?.effectiveModel(chat.modelId, chat.assistantId);
    if (_images.isNotEmpty && model != null && !model.vision) {
      showToast(context, '当前模型「${model.displayName}」看不了图片，请换一个模型');
      return;
    }
    HapticFeedback.lightImpact();
    ref.read(chatProvider.notifier).send(text, images: List.of(_images));
    _ctrl.clear();
    setState(() => _images.clear());
  }

  void _sendVoice(String text) {
    if (ref.read(chatProvider).busy) {
      _ctrl.text = text;
      return;
    }
    ref.read(chatProvider.notifier).send(text, images: List.of(_images));
    setState(() => _images.clear());
  }

  Future<void> _pick(ImageSource source) async {
    if (_picking) return;
    _picking = true;
    try {
      final picker = ImagePicker();
      final files = source == ImageSource.camera
          ? [?await picker.pickImage(source: ImageSource.camera)]
          : await picker.pickMultiImage(limit: 9 - _images.length);
      for (final f in files.take(9 - _images.length)) {
        final img = await _compress(f.path);
        if (img != null && mounted) setState(() => _images.add(img));
      }
    } catch (e) {
      if (mounted) showToast(context, '没能打开${source == ImageSource.camera ? '相机' : '相册'}，请检查权限');
    } finally {
      _picking = false;
    }
  }

  /// Re-encodes to JPEG (long edge ≤ 2048, keeps small print legible) and strips EXIF.
  /// Falls back to original image bytes (≤ 20MB, e.g. GIF stickers) if native re-encoding is unavailable.
  Future<PickedImage?> _compress(String path) async {
    Uint8List? bytes;
    var ext = 'jpg';
    try {
      bytes = await FlutterImageCompress.compressWithFile(path, minWidth: 2048, minHeight: 2048, quality: 85, keepExif: false, format: CompressFormat.jpeg);
    } catch (_) {}
    if (bytes == null) {
      final raw = await File(path).readAsBytes();
      if (raw.isEmpty || raw.length > 20 * 1024 * 1024) return null;
      bytes = raw;
      final dot = path.lastIndexOf('.');
      if (dot != -1 && dot < path.length - 1) ext = path.substring(dot + 1).toLowerCase();
    }
    String out = '';
    try {
      final dir = await getTemporaryDirectory();
      out = '${dir.path}/nuan_${DateTime.now().microsecondsSinceEpoch}.$ext';
      await File(out).writeAsBytes(bytes);
    } catch (_) {
      out = path.contains('shared_incoming') ? '' : path;
    }
    final codec = await ui.instantiateImageCodec(bytes);
    final frame = await codec.getNextFrame();
    final w = frame.image.width, h = frame.image.height;
    frame.image.dispose();
    return PickedImage(bytes: bytes, path: out, width: w, height: h);
  }

  void _photoMenu() {
    FocusScope.of(context).unfocus();
    showActionSheet(context, actions: [
      SheetAction(AppIcons.camera, '拍照', () => _pick(ImageSource.camera)),
      SheetAction(AppIcons.image, '从相册选择', () => _pick(ImageSource.gallery)),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    // Wipe unsent text & picked images whenever navigating out of a private session.
    ref.listen(chatProvider.select((s) => (s.isPrivate, s.epoch)), (prev, next) {
      if (prev != null && prev.$1 && prev != next) {
        clearDraft();
      }
    });
    final p = context.pal;
    final chat = ref.watch(chatProvider);
    final boot = ref.watch(sessionProvider).boot;
    final model = boot?.effectiveModel(chat.modelId, chat.assistantId);
    final canSend = (_ctrl.text.trim().isNotEmpty || _images.isNotEmpty) && !chat.busy;
    final showMic = boot?.voiceInput == true && !chat.busy && _ctrl.text.trim().isEmpty;

    return Container(
      margin: const EdgeInsets.fromLTRB(12, 4, 12, 10),
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(28),
        border: Border.all(color: chat.isPrivate ? p.privateAccent.withValues(alpha: 0.35) : p.line),
        boxShadow: [BoxShadow(color: p.shadow, blurRadius: 24, offset: const Offset(0, 6))],
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AnimatedSize(
            duration: const Duration(milliseconds: 220),
            curve: Curves.easeOutCubic,
            child: _images.isEmpty
                ? const SizedBox(width: double.infinity)
                : SizedBox(
                    height: 84,
                    child: ListView.separated(
                      scrollDirection: Axis.horizontal,
                      padding: const EdgeInsets.fromLTRB(14, 12, 14, 0),
                      itemCount: _images.length,
                      separatorBuilder: (_, _) => const SizedBox(width: 8),
                      itemBuilder: (_, i) => Stack(clipBehavior: Clip.none, children: [
                        ClipRRect(
                          borderRadius: BorderRadius.circular(12),
                          child: Image.memory(_images[i].bytes, width: 72, height: 72, fit: BoxFit.cover, cacheWidth: 216),
                        ),
                        Positioned(
                          right: -6,
                          top: -6,
                          child: Pressable(
                            onTap: () => setState(() => _images.removeAt(i)),
                            child: Container(
                              padding: const EdgeInsets.all(3.5),
                              decoration: const BoxDecoration(color: Color(0xCC000000), shape: BoxShape.circle),
                              child: const Icon(AppIcons.closeSmall, size: 13.5, color: Colors.white),
                            ),
                          ),
                        ),
                      ]),
                    ),
                  ),
          ),
          TextField(
            controller: _ctrl,
            focusNode: _focus,
            minLines: 1,
            maxLines: 6,
            textInputAction: TextInputAction.send,
            keyboardType: TextInputType.text,
            onSubmitted: (_) => _send(),
            style: const TextStyle(fontSize: 17, height: 1.45),
            decoration: InputDecoration(
              hintText: chat.isPrivate
                  ? (_images.isEmpty ? '在隐私对话中提问…' : '想问这张图片什么？（离开后清除）')
                  : (_images.isEmpty ? '有什么问题尽管问我…' : '想问这张图片什么？'),
              filled: false,
              border: InputBorder.none,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
              contentPadding: const EdgeInsets.fromLTRB(20, 14, 20, 6),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
            child: Row(
              children: [
                _ToolButton(icon: AppIcons.camera, label: '拍照/图片', onTap: chat.busy ? null : _photoMenu),
                if (model?.think == true) ...[
                  const SizedBox(width: 6),
                  _ThinkChip(on: chat.think, onTap: () => ref.read(chatProvider.notifier).setThink(!chat.think)),
                ],
                const Spacer(),
                if (showMic) ...[
                  VoiceButton(onText: _sendVoice),
                  if (_images.isNotEmpty) const SizedBox(width: 8),
                ],
                if (!showMic || _images.isNotEmpty)
                  _SendButton(
                    busy: chat.busy,
                    enabled: canSend,
                    isPrivate: chat.isPrivate,
                    onSend: _send,
                    onStop: () => ref.read(chatProvider.notifier).stop(),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ToolButton extends StatelessWidget {
  const _ToolButton({required this.icon, required this.label, this.onTap});
  final IconData icon;
  final String label;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: label,
      child: Pressable(
        onTap: onTap,
        child: Container(
          width: 44,
          height: 44,
          decoration: BoxDecoration(color: p.surfaceAlt, shape: BoxShape.circle),
          child: Icon(icon, size: 22, color: onTap == null ? p.faint : p.text),
        ),
      ),
    );
  }
}

class _ThinkChip extends StatelessWidget {
  const _ThinkChip({required this.on, required this.onTap});
  final bool on;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        height: 40,
        padding: const EdgeInsets.symmetric(horizontal: 14),
        decoration: BoxDecoration(
          color: on ? p.accentSoft : p.surfaceAlt,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: on ? p.accent.withValues(alpha: 0.6) : Colors.transparent),
        ),
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          Icon(on ? AppIcons.thinkOn : AppIcons.thinkOff, size: 18, color: on ? p.accent : p.muted),
          const SizedBox(width: 5),
          Text('深度思考', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w500, color: on ? p.accent : p.muted)),
        ]),
      ),
    );
  }
}

class _SendButton extends StatelessWidget {
  const _SendButton({required this.busy, required this.enabled, this.isPrivate = false, required this.onSend, required this.onStop});
  final bool busy, enabled, isPrivate;
  final VoidCallback onSend, onStop;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final grad = isPrivate ? Palette.privateGradient : Palette.brandGradient;
    final glow = isPrivate ? p.privateAccent : Palette.brand;
    final child = busy
        ? Container(
            key: const ValueKey('stop'),
            width: 46,
            height: 46,
            decoration: BoxDecoration(color: p.text, shape: BoxShape.circle),
            child: Center(child: Container(width: 15, height: 15, decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(3)))),
          )
        : AnimatedContainer(
            key: const ValueKey('send'),
            duration: const Duration(milliseconds: 200),
            width: 46,
            height: 46,
            decoration: BoxDecoration(
              gradient: enabled ? grad : null,
              color: enabled ? null : p.line,
              shape: BoxShape.circle,
              boxShadow: enabled ? [BoxShadow(color: glow.withValues(alpha: 0.35), blurRadius: 12, offset: const Offset(0, 4))] : null,
            ),
            child: Icon(AppIcons.arrowUp, color: enabled ? Colors.white : p.faint, size: 22),
          );
    return Tooltip(
      message: busy ? '停止回答' : '发送',
      child: Pressable(
        onTap: busy ? onStop : (enabled ? onSend : null),
        child: AnimatedSwitcher(
          duration: const Duration(milliseconds: 180),
          transitionBuilder: (c, a) => ScaleTransition(scale: a, child: c),
          child: child,
        ),
      ),
    );
  }
}

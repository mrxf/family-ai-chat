import 'dart:io';
import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart' show HapticFeedback, MissingPluginException, Uint8List;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:gal/gal.dart';
import 'package:path_provider/path_provider.dart';

import '../core/api.dart';
import '../core/models.dart';
import '../state/chat.dart';
import '../state/conversations.dart';
import '../state/session.dart';
import 'chat_components.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

/// Optional hook for widget tests to intercept generated poster PNG bytes.
@visibleForTesting
Future<void> Function(Uint8List pngBytes)? debugShareSaveOverride;

/// Collects messages in [chat] that have actual text or image content to share.
List<Message> shareableMessages(ChatState chat, LiveText live) {
  final out = <Message>[];
  for (final m in chat.messages) {
    if (m.id == chat.activeId) {
      if (live.content.trim().isNotEmpty || live.attachments.isNotEmpty || live.components.isNotEmpty) {
        out.add(m.copyWith(
          content: live.content,
          reasoning: live.reasoning,
          attachments: live.attachments,
          components: live.components,
          status: MsgStatus.done,
        ));
      }
      continue;
    }
    if (m.content.trim().isEmpty && m.attachments.isEmpty && m.components.isEmpty) continue;
    out.add(m);
  }
  return out;
}

/// Opens the two-step share flow: pick messages → preview & save XiaoNuan poster.
Future<void> openShareChat(BuildContext context, WidgetRef ref, {Message? focusMessage}) async {
  FocusManager.instance.primaryFocus?.unfocus();
  final chat = ref.read(chatProvider);
  final live = ref.read(chatProvider.notifier).live.value;
  final msgs = shareableMessages(chat, live);
  if (msgs.isEmpty) {
    showToast(context, chat.busy ? '等小暖回答完就可以分享啦' : '还没有可分享的对话内容');
    return;
  }
  final boot = ref.read(sessionProvider).boot;
  final assistant = boot?.assistant(chat.assistantId);
  final cid = chat.conversationId;
  final rawTitle = cid == null ? null : ref.read(conversationsTitlesProvider)[cid];
  final title = rawTitle != null && rawTitle.trim().isNotEmpty ? rawTitle.trim() : null;

  await Navigator.of(context).push(
    MaterialPageRoute(
      builder: (_) => ShareSelectPage(
        messages: msgs,
        initialFocusId: focusMessage?.id,
        conversationTitle: title,
        assistant: assistant,
      ),
    ),
  );
}

// ============================================================================
// Step 1: Select which messages in the conversation to include
// ============================================================================

class ShareSelectPage extends StatefulWidget {
  const ShareSelectPage({
    super.key,
    required this.messages,
    this.initialFocusId,
    this.conversationTitle,
    this.assistant,
  });

  final List<Message> messages;
  final String? initialFocusId;
  final String? conversationTitle;
  final Assistant? assistant;

  @override
  State<ShareSelectPage> createState() => _ShareSelectPageState();
}

class _ShareSelectPageState extends State<ShareSelectPage> {
  late final Set<String> _selected = _defaultSelection();

  Set<String> _defaultSelection() {
    final msgs = widget.messages;
    if (msgs.isEmpty) return {};
    final focusId = widget.initialFocusId;
    if (focusId != null) {
      final idx = msgs.indexWhere((m) => m.id == focusId);
      if (idx >= 0) {
        final ids = <String>{msgs[idx].id};
        if (!msgs[idx].isUser && idx > 0 && msgs[idx - 1].isUser) {
          ids.add(msgs[idx - 1].id);
        } else if (msgs[idx].isUser && idx + 1 < msgs.length && !msgs[idx + 1].isUser) {
          ids.add(msgs[idx + 1].id);
        }
        return ids;
      }
    }
    if (msgs.length <= 6) {
      return {for (final m in msgs) m.id};
    }
    return _lastTurnIds();
  }

  Set<String> _lastTurnIds() {
    final msgs = widget.messages;
    if (msgs.isEmpty) return {};
    final ids = <String>{msgs.last.id};
    if (!msgs.last.isUser && msgs.length >= 2 && msgs[msgs.length - 2].isUser) {
      ids.add(msgs[msgs.length - 2].id);
    }
    return ids;
  }

  bool get _allSelected => _selected.length == widget.messages.length;

  void _toggle(String id) {
    setState(() {
      if (!_selected.remove(id)) _selected.add(id);
    });
  }

  void _toggleAll() {
    HapticFeedback.selectionClick();
    setState(() {
      if (_allSelected) {
        _selected.clear();
      } else {
        _selected.addAll(widget.messages.map((m) => m.id));
      }
    });
  }

  void _selectLastTurn() {
    HapticFeedback.selectionClick();
    setState(() {
      _selected
        ..clear()
        ..addAll(_lastTurnIds());
    });
  }

  void _openPreview() {
    final chosen = [for (final m in widget.messages) if (_selected.contains(m.id)) m];
    if (chosen.isEmpty) return;
    Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => SharePosterPage(
          messages: chosen,
          conversationTitle: widget.conversationTitle,
          assistant: widget.assistant,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final count = _selected.length;
    final assistantName = widget.assistant?.name ?? '小暖';

    return Scaffold(
      appBar: AppBar(
        title: const Text('选择分享内容'),
        actions: [
          TextButton(
            onPressed: _toggleAll,
            child: Text(
              _allSelected ? '取消全选' : '全选',
              style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600, color: p.accent),
            ),
          ),
          const SizedBox(width: 6),
        ],
      ),
      body: Column(
        children: [
          Container(
            margin: const EdgeInsets.fromLTRB(16, 4, 16, 8),
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            decoration: BoxDecoration(
              color: p.surfaceAlt,
              borderRadius: BorderRadius.circular(16),
              border: Border.all(color: p.line),
            ),
            child: Row(
              children: [
                const Mascot(size: 28),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    '勾选要放进长图的对话，自动排版生成精美卡片',
                    style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.35),
                  ),
                ),
                if (widget.messages.length > 2) ...[
                  const SizedBox(width: 8),
                  Pressable(
                    onTap: _selectLastTurn,
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                      decoration: BoxDecoration(
                        color: p.surface,
                        borderRadius: BorderRadius.circular(12),
                        border: Border.all(color: p.line),
                      ),
                      child: Text('最近一轮', style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.text)),
                    ),
                  ),
                ],
              ],
            ),
          ),
          Expanded(
            child: ListView.separated(
              padding: const EdgeInsets.fromLTRB(16, 6, 16, 20),
              itemCount: widget.messages.length,
              separatorBuilder: (_, _) => const SizedBox(height: 10),
              itemBuilder: (context, i) {
                final m = widget.messages[i];
                final checked = _selected.contains(m.id);
                return _SelectableMessageTile(
                  message: m,
                  checked: checked,
                  assistantName: assistantName,
                  assistantAvatar: widget.assistant?.avatar ?? '',
                  onTap: () => _toggle(m.id),
                );
              },
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
                padding: const EdgeInsets.fromLTRB(20, 12, 16, 12),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            count > 0 ? '已选 $count 条对话' : '请勾选要分享的对话',
                            style: TextStyle(
                              fontSize: 15.5,
                              fontWeight: FontWeight.w600,
                              color: count > 0 ? p.text : p.muted,
                            ),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            '下一步可预览图片并切换卡片主题',
                            style: TextStyle(fontSize: 12.5, color: p.muted),
                          ),
                        ],
                      ),
                    ),
                    Pressable(
                      onTap: count > 0 ? _openPreview : null,
                      child: AnimatedContainer(
                        duration: const Duration(milliseconds: 180),
                        padding: const EdgeInsets.symmetric(horizontal: 22, vertical: 13),
                        decoration: BoxDecoration(
                          gradient: count > 0 ? Palette.brandGradient : null,
                          color: count > 0 ? null : p.line,
                          borderRadius: BorderRadius.circular(24),
                          boxShadow: count > 0
                              ? [BoxShadow(color: Palette.brand.withValues(alpha: 0.3), blurRadius: 14, offset: const Offset(0, 4))]
                              : null,
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            AppIcon(AppIcons.sparkles, size: 18, color: count > 0 ? Colors.white : p.faint),
                            const SizedBox(width: 6),
                            Text(
                              '生成分享图片',
                              style: TextStyle(
                                fontSize: 15.5,
                                fontWeight: FontWeight.w600,
                                color: count > 0 ? Colors.white : p.faint,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _SelectableMessageTile extends StatelessWidget {
  const _SelectableMessageTile({
    required this.message,
    required this.checked,
    required this.assistantName,
    required this.assistantAvatar,
    required this.onTap,
  });

  final Message message;
  final bool checked;
  final String assistantName;
  final String assistantAvatar;
  final VoidCallback onTap;

  static String _plainPreview(String raw) {
    return raw
        .replaceAll(RegExp(r'```[\s\S]*?```'), '[代码块]')
        .replaceAll(RegExp(r'^\s*#{1,6}\s+', multiLine: true), '')
        .replaceAllMapped(RegExp(r'\*\*(.+?)\*\*'), (m) => m.group(1)!)
        .replaceAllMapped(RegExp(r'`([^`]+)`'), (m) => m.group(1)!)
        .replaceAll(RegExp(r'^\s*>\s?', multiLine: true), '')
        .replaceAll(RegExp(r'\n{2,}'), '\n')
        .trim();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isUser = message.isUser;
    final preview = _plainPreview(message.content);
    final imgCount = message.attachments.length;

    return Pressable(
      scale: 0.985,
      onTap: onTap,
      child: AnimatedOpacity(
        duration: const Duration(milliseconds: 160),
        opacity: checked ? 1.0 : 0.68,
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 180),
          padding: const EdgeInsets.fromLTRB(14, 13, 15, 13),
          decoration: BoxDecoration(
            color: checked ? p.surface : p.surface.withValues(alpha: 0.7),
            borderRadius: BorderRadius.circular(20),
            border: Border.all(
              color: checked ? p.accent.withValues(alpha: 0.55) : p.line,
              width: checked ? 1.4 : 1.0,
            ),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 160),
                  width: 24,
                  height: 24,
                  decoration: BoxDecoration(
                    gradient: checked ? Palette.brandGradient : null,
                    color: checked ? null : p.surfaceAlt,
                    shape: BoxShape.circle,
                    border: checked ? null : Border.all(color: p.faint, width: 1.4),
                  ),
                  child: checked ? const AppIcon(AppIcons.checkBold, size: 14, color: Colors.white) : null,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        if (isUser)
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2.5),
                            decoration: BoxDecoration(
                              color: p.userBubble,
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: Text('提问', style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.userText)),
                          )
                        else ...[
                          if (assistantAvatar.isNotEmpty)
                            Avatar(label: assistantName, emoji: assistantAvatar, size: 20)
                          else
                            const Mascot(size: 20),
                          const SizedBox(width: 6),
                          Text(assistantName, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.text)),
                        ],
                        if (imgCount > 0) ...[
                          const SizedBox(width: 8),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                            decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(7)),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                AppIcon(AppIcons.image, size: 13, color: p.muted),
                                const SizedBox(width: 3),
                                Text(imgCount > 1 ? '$imgCount 张图片' : '含图片', style: TextStyle(fontSize: 12, color: p.muted)),
                              ],
                            ),
                          ),
                        ],
                      ],
                    ),
                    if (preview.isNotEmpty) ...[
                      const SizedBox(height: 7),
                      Text(
                        preview,
                        maxLines: 4,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 15,
                          height: 1.48,
                          color: p.text,
                          fontWeight: isUser ? FontWeight.w500 : FontWeight.w400,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// ============================================================================
// Step 2: Poster Preview & One-Tap Save to Gallery
// ============================================================================

enum SharePosterStyle {
  warm('暖阳', Color(0xFFFFF6EE), Color(0xFFFF7A45)),
  apricot('晴杏', Color(0xFFFFDFC8), Color(0xFFF25A2A)),
  night('夜阑', Color(0xFF1F1A17), Color(0xFFFF8B57));

  const SharePosterStyle(this.label, this.swatchBg, this.swatchDot);
  final String label;
  final Color swatchBg;
  final Color swatchDot;
}

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
      final name = 'xiaonuan_share_${DateTime.now().millisecondsSinceEpoch}';

      if (debugShareSaveOverride != null) {
        await debugShareSaveOverride!(bytes);
      } else {
        final ok = await _persistImageBytes(bytes, name);
        if (!ok) {
          if (mounted) showToast(context, '没有相册权限，请在手机设置里允许小暖保存照片');
          return;
        }
      }

      if (mounted) {
        HapticFeedback.mediumImpact();
        setState(() => _justSaved = true);
        showToast(context, '分享图片已保存到本地相册，可以去微信发给家人啦');
      }
    } on GalException catch (e) {
      if (mounted) {
        showToast(
          context,
          e.type == GalExceptionType.permissionDenied ? '没有相册权限，请在手机设置里允许小暖保存照片' : '保存失败，请再试一次',
        );
      }
    } catch (_) {
      if (mounted) showToast(context, '保存图片失败，请再试一次');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<bool> _persistImageBytes(Uint8List bytes, String name) async {
    try {
      if (!await Gal.hasPermission() && !await Gal.requestPermission()) {
        return false;
      }
      await Gal.putImageBytes(bytes, name: name);
      return true;
    } on MissingPluginException {
      final dir = await getDownloadsDirectory() ?? await getApplicationDocumentsDirectory();
      await File('${dir.path}/$name.png').writeAsBytes(bytes);
      return true;
    } on UnsupportedError {
      final dir = await getDownloadsDirectory() ?? await getApplicationDocumentsDirectory();
      await File('${dir.path}/$name.png').writeAsBytes(bytes);
      return true;
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
                                    ? '已保存到相册（点此再存一张）'
                                    : '保存图片到本地相册',
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

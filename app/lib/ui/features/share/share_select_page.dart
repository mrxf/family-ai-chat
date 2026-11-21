import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HapticFeedback;

import '../../../core/models/models.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import 'share_poster_page.dart';

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
      appRoute(
        (_) => SharePosterPage(
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

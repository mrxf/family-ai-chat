import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../state/chat.dart';
import '../../../state/conversations.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import '../../widgets/sheets.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

/// Opens the full-screen conversation search page with a soft fade transition.
/// Returns the selected conversation ID, or null if dismissed.
Future<String?> openSearchPage(BuildContext context) {
  return Navigator.of(context).push<String>(
    PageRouteBuilder<String>(
      transitionDuration: const Duration(milliseconds: 200),
      reverseTransitionDuration: const Duration(milliseconds: 170),
      pageBuilder: (_, _, _) => const SearchPage(),
      transitionsBuilder: (_, anim, _, child) {
        final curved = CurvedAnimation(parent: anim, curve: Curves.easeOutCubic, reverseCurve: Curves.easeInCubic);
        return FadeTransition(
          opacity: curved,
          child: SlideTransition(
            position: Tween<Offset>(begin: const Offset(0, 0.025), end: Offset.zero).animate(curved),
            child: child,
          ),
        );
      },
    ),
  );
}

/// Shared long-press menu for renaming, pinning, or deleting a conversation.
void showConversationMenu(
  BuildContext context,
  WidgetRef ref,
  Conversation c, {
  void Function(Conversation)? onUpdated,
  VoidCallback? onDeleted,
}) {
  FocusScope.of(context).unfocus();
  final convs = ref.read(conversationsProvider.notifier);
  showActionSheet(context, title: c.title.isEmpty ? '新对话' : c.title, actions: [
    SheetAction(AppIcons.edit, '重命名', () async {
      final t = await showTextEditor(context, title: '重命名对话', initial: c.title, maxLines: 2);
      if (t != null && t.isNotEmpty) {
        onUpdated?.call(c.copyWith(title: t));
        await convs.rename(c.id, t);
      }
    }),
    SheetAction(c.pinned ? AppIcons.unpin : AppIcons.pin, c.pinned ? '取消置顶' : '置顶', () {
      onUpdated?.call(c.copyWith(pinned: !c.pinned));
      convs.togglePin(c);
    }),
    SheetAction(AppIcons.delete, '删除', () async {
      if (!await confirm(context, title: '删除这个对话？', body: '删除后无法恢复。', ok: '删除', danger: true)) return;
      try {
        await convs.delete(c.id);
        onDeleted?.call();
        if (ref.read(chatProvider).conversationId == c.id) ref.read(chatProvider.notifier).newChat();
      } catch (_) {
        if (context.mounted) showToast(context, '删除失败，请检查网络');
      }
    }, danger: true),
  ]);
}

class SearchPage extends ConsumerStatefulWidget {
  const SearchPage({super.key});

  @override
  ConsumerState<SearchPage> createState() => _SearchPageState();
}

class _SearchPageState extends ConsumerState<SearchPage> {
  final _ctrl = TextEditingController();
  final _focus = FocusNode();
  Timer? _debounce;
  List<Conversation>? _results;
  bool _searching = false;
  int _seq = 0;

  String get _query => _ctrl.text.trim();

  @override
  void dispose() {
    _debounce?.cancel();
    _ctrl.dispose();
    _focus.dispose();
    super.dispose();
  }

  void _onChanged(String raw) {
    _debounce?.cancel();
    final q = raw.trim();
    if (q.isEmpty) {
      _seq++;
      setState(() {
        _results = null;
        _searching = false;
      });
      return;
    }
    setState(() => _searching = true);
    final ticket = ++_seq;
    _debounce = Timer(const Duration(milliseconds: 280), () async {
      try {
        final api = ref.read(sessionProvider).api;
        if (api == null) return;
        final r = await api.conversations(q: q, limit: 50);
        if (mounted && ticket == _seq) {
          setState(() {
            _results = r;
            _searching = false;
          });
        }
      } catch (_) {
        if (mounted && ticket == _seq) setState(() => _searching = false);
      }
    });
  }

  void _select(Conversation c) {
    _focus.unfocus();
    Navigator.of(context).pop(c.id);
  }

  void _close() {
    _focus.unfocus();
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final recent = ref.watch(conversationsProvider);
    final currentId = ref.watch(chatProvider.select((s) => s.conversationId));
    final q = _query;
    final isSearch = q.isNotEmpty;
    final items = isSearch ? (_results ?? const <Conversation>[]) : recent.items;

    return Scaffold(
      backgroundColor: p.bg,
      resizeToAvoidBottomInset: true,
      body: SafeArea(
        child: Stack(
          children: [
            Positioned.fill(
              child: _buildBody(p, recent: recent, items: items, currentId: currentId, q: q, isSearch: isSearch),
            ),
            Align(
              alignment: Alignment.bottomCenter,
              child: _buildFloatingBar(p),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildBody(
    Palette p, {
    required ConvListState recent,
    required List<Conversation> items,
    required String? currentId,
    required String q,
    required bool isSearch,
  }) {
    if (isSearch && _searching && _results == null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.2, color: p.accent)),
            const SizedBox(height: 12),
            Text('正在搜索…', style: TextStyle(color: p.faint, fontSize: 15)),
          ],
        ),
      );
    }

    if (items.isEmpty) {
      final msg = isSearch
          ? '没有找到与「$q」相关的对话'
          : recent.loaded
              ? '还没有聊天记录'
              : '加载中…';
      return GestureDetector(
        behavior: HitTestBehavior.translucent,
        onTap: () => _focus.unfocus(),
        child: Center(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(32, 0, 32, 72),
            child: Text(msg, textAlign: TextAlign.center, style: TextStyle(color: p.faint, fontSize: 15.5, height: 1.6)),
          ),
        ),
      );
    }

    return NotificationListener<ScrollNotification>(
      onNotification: (n) {
        if (!isSearch && n.metrics.extentAfter < 400) {
          ref.read(conversationsProvider.notifier).loadMore();
        }
        return false;
      },
      child: ListView.builder(
        keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
        padding: const EdgeInsets.fromLTRB(10, 8, 10, 96),
        itemCount: items.length + 1,
        itemBuilder: (ctx, index) {
          if (index == 0) {
            return Padding(
              padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
              child: Row(
                children: [
                  Text(
                    isSearch ? '搜索结果' : '近期对话',
                    style: TextStyle(fontSize: 14.5, color: p.muted, fontWeight: FontWeight.w600),
                  ),
                  if (isSearch && _searching) ...[
                    const SizedBox(width: 8),
                    SizedBox(width: 13, height: 13, child: CircularProgressIndicator(strokeWidth: 1.8, color: p.muted)),
                  ],
                ],
              ),
            );
          }
          final c = items[index - 1];
          return _SearchTile(
            conv: c,
            query: isSearch ? q : '',
            selected: c.id == currentId,
            onTap: () => _select(c),
            onLongPress: () => showConversationMenu(
              context,
              ref,
              c,
              onUpdated: (updated) {
                if (_results != null) {
                  setState(() => _results = [for (final x in _results!) x.id == updated.id ? updated : x]);
                }
              },
              onDeleted: () {
                if (_results != null) {
                  setState(() => _results = _results!.where((x) => x.id != c.id).toList());
                }
              },
            ),
          );
        },
      ),
    );
  }

  Widget _buildFloatingBar(Palette p) {
    return Container(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [p.bg.withValues(alpha: 0), p.bg.withValues(alpha: 0.92), p.bg],
          stops: const [0, 0.38, 1],
        ),
      ),
      padding: const EdgeInsets.fromLTRB(16, 18, 16, 12),
      child: Row(
        children: [
          Expanded(
            child: Container(
              height: 52,
              decoration: BoxDecoration(
                color: p.surface,
                borderRadius: BorderRadius.circular(999),
                border: Border.all(color: p.line),
                boxShadow: [BoxShadow(color: p.shadow, blurRadius: 18, offset: const Offset(0, 4))],
              ),
              child: Row(
                children: [
                  const SizedBox(width: 16),
                  AppIcon(AppIcons.search, color: p.muted, size: 21),
                  const SizedBox(width: 10),
                  Expanded(
                    child: TextField(
                      controller: _ctrl,
                      focusNode: _focus,
                      autofocus: true,
                      onChanged: _onChanged,
                      textInputAction: TextInputAction.search,
                      style: TextStyle(fontSize: 16.5, color: p.text),
                      decoration: InputDecoration(
                        hintText: '搜索',
                        hintStyle: TextStyle(color: p.faint, fontSize: 16.5),
                        filled: false,
                        isDense: true,
                        contentPadding: const EdgeInsets.symmetric(vertical: 12),
                        border: InputBorder.none,
                        enabledBorder: InputBorder.none,
                        focusedBorder: InputBorder.none,
                      ),
                    ),
                  ),
                  if (_ctrl.text.isNotEmpty)
                    Pressable(
                      onTap: () {
                        _ctrl.clear();
                        _onChanged('');
                        _focus.requestFocus();
                      },
                      child: Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                        child: AppIcon(AppIcons.closeCircle, size: 19, color: p.faint),
                      ),
                    )
                  else
                    const SizedBox(width: 16),
                ],
              ),
            ),
          ),
          const SizedBox(width: 10),
          Tooltip(
            message: '关闭搜索',
            child: Pressable(
              onTap: _close,
              child: Container(
                width: 52,
                height: 52,
                decoration: BoxDecoration(
                  color: p.surface,
                  shape: BoxShape.circle,
                  border: Border.all(color: p.line),
                  boxShadow: [BoxShadow(color: p.shadow, blurRadius: 18, offset: const Offset(0, 4))],
                ),
                child: AppIcon(AppIcons.close, color: p.text, size: 21),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _SearchTile extends StatelessWidget {
  const _SearchTile({
    required this.conv,
    required this.query,
    required this.selected,
    required this.onTap,
    required this.onLongPress,
  });

  final Conversation conv;
  final String query;
  final bool selected;
  final VoidCallback onTap, onLongPress;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final title = conv.title.isEmpty ? '新对话' : conv.title;
    final hasSnippet = query.isNotEmpty && conv.snippet.isNotEmpty;

    return Pressable(
      scale: 0.985,
      onTap: onTap,
      onLongPress: onLongPress,
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 1.5),
        padding: EdgeInsets.symmetric(horizontal: 12, vertical: hasSnippet ? 12 : 14),
        decoration: BoxDecoration(
          color: selected ? p.surface : Colors.transparent,
          borderRadius: BorderRadius.circular(16),
        ),
        child: Row(
          crossAxisAlignment: hasSnippet ? CrossAxisAlignment.start : CrossAxisAlignment.center,
          children: [
            Padding(
              padding: EdgeInsets.only(top: hasSnippet ? 3 : 0, right: 14),
              child: AppIcon(
                conv.pinned ? AppIcons.pin : AppIcons.chatBubble,
                size: 19,
                color: conv.pinned ? p.accent : p.muted,
              ),
            ),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  _HighlightedText(
                    text: title,
                    query: query,
                    maxLines: 1,
                    style: TextStyle(fontSize: 16.5, fontWeight: selected ? FontWeight.w600 : FontWeight.w400, color: p.text),
                    highlightStyle: TextStyle(fontSize: 16.5, fontWeight: FontWeight.w700, color: p.accent),
                  ),
                  if (hasSnippet) ...[
                    const SizedBox(height: 4),
                    _HighlightedText(
                      text: conv.snippet,
                      query: query,
                      maxLines: 2,
                      style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.4),
                      highlightStyle: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.accent, height: 1.4),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 12),
            Padding(
              padding: EdgeInsets.only(top: hasSnippet ? 2 : 0),
              child: Text(
                _formatAge(conv.updatedAt),
                style: TextStyle(fontSize: 13.5, color: p.faint),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _HighlightedText extends StatelessWidget {
  const _HighlightedText({
    required this.text,
    required this.query,
    required this.style,
    required this.highlightStyle,
    this.maxLines = 1,
  });

  final String text;
  final String query;
  final TextStyle style;
  final TextStyle highlightStyle;
  final int maxLines;

  @override
  Widget build(BuildContext context) {
    final q = query.trim().replaceAll(RegExp(r'\s+'), ' ');
    if (q.isEmpty) {
      return Text(text, maxLines: maxLines, overflow: TextOverflow.ellipsis, style: style);
    }
    final pattern = RegExp(RegExp.escape(q), caseSensitive: false);
    final matches = pattern.allMatches(text);
    if (matches.isEmpty) {
      return Text(text, maxLines: maxLines, overflow: TextOverflow.ellipsis, style: style);
    }
    final spans = <TextSpan>[];
    var cursor = 0;
    for (final m in matches) {
      if (m.start > cursor) {
        spans.add(TextSpan(text: text.substring(cursor, m.start)));
      }
      spans.add(TextSpan(text: text.substring(m.start, m.end), style: highlightStyle));
      cursor = m.end;
    }
    if (cursor < text.length) {
      spans.add(TextSpan(text: text.substring(cursor)));
    }
    return Text.rich(
      TextSpan(style: style, children: spans),
      maxLines: maxLines,
      overflow: TextOverflow.ellipsis,
    );
  }
}

String _formatAge(int updatedAtMs) {
  if (updatedAtMs <= 0) return '';
  final diff = DateTime.now().difference(DateTime.fromMillisecondsSinceEpoch(updatedAtMs));
  if (diff.inMinutes < 1) return '刚刚';
  if (diff.inHours < 1) return '${diff.inMinutes}分钟';
  if (diff.inDays < 1) return '${diff.inHours}小时';
  if (diff.inDays < 30) return '${diff.inDays}天';
  if (diff.inDays < 365) return '${diff.inDays ~/ 30}个月';
  return '${diff.inDays ~/ 365}年';
}

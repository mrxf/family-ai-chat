import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../state/chat.dart';
import '../state/conversations.dart';
import '../state/session.dart';
import 'settings_page.dart';
import 'sheets.dart';
import 'theme.dart';
import 'widgets.dart';

class HistoryDrawer extends ConsumerStatefulWidget {
  const HistoryDrawer({super.key});

  @override
  ConsumerState<HistoryDrawer> createState() => _HistoryDrawerState();
}

class _HistoryDrawerState extends ConsumerState<HistoryDrawer> {
  final _search = TextEditingController();
  Timer? _debounce;
  List<Conversation>? _results;
  bool _searching = false;

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    super.dispose();
  }

  void _onSearch(String q) {
    _debounce?.cancel();
    if (q.trim().isEmpty) {
      setState(() => _results = null);
      return;
    }
    _debounce = Timer(const Duration(milliseconds: 300), () async {
      setState(() => _searching = true);
      try {
        final r = await ref.read(apiProvider).conversations(q: q.trim(), limit: 50);
        if (mounted && _search.text.trim() == q.trim()) setState(() => _results = r);
      } catch (_) {
      } finally {
        if (mounted) setState(() => _searching = false);
      }
    });
  }

  void _open(Conversation c) {
    Navigator.pop(context);
    ref.read(chatProvider.notifier).open(c.id);
  }

  void _menu(Conversation c) {
    final convs = ref.read(conversationsProvider.notifier);
    showActionSheet(context, title: c.title.isEmpty ? '新对话' : c.title, actions: [
      SheetAction(Icons.edit_rounded, '重命名', () async {
        final t = await showTextEditor(context, title: '重命名对话', initial: c.title, maxLines: 2);
        if (t != null && t.isNotEmpty) convs.rename(c.id, t);
      }),
      SheetAction(c.pinned ? Icons.push_pin_outlined : Icons.push_pin_rounded, c.pinned ? '取消置顶' : '置顶', () => convs.togglePin(c)),
      SheetAction(Icons.delete_outline_rounded, '删除', () async {
        if (!await confirm(context, title: '删除这个对话？', body: '删除后无法恢复。', ok: '删除', danger: true)) return;
        try {
          await convs.delete(c.id);
          if (ref.read(chatProvider).conversationId == c.id) ref.read(chatProvider.notifier).newChat();
        } catch (_) {
          if (mounted) showToast(context, '删除失败，请检查网络');
        }
      }, danger: true),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final list = ref.watch(conversationsProvider);
    final currentId = ref.watch(chatProvider.select((s) => s.conversationId));
    final user = ref.watch(sessionProvider).user;
    final items = _results ?? list.items;

    final groups = <String, List<Conversation>>{};
    if (_results != null) {
      groups['搜索结果'] = items;
    } else {
      final now = DateTime.now();
      final today = DateTime(now.year, now.month, now.day);
      for (final c in items) {
        final d = DateTime.fromMillisecondsSinceEpoch(c.updatedAt);
        final key = c.pinned
            ? '置顶'
            : !d.isBefore(today)
                ? '今天'
                : !d.isBefore(today.subtract(const Duration(days: 1)))
                    ? '昨天'
                    : !d.isBefore(today.subtract(const Duration(days: 7)))
                        ? '最近 7 天'
                        : !d.isBefore(today.subtract(const Duration(days: 30)))
                            ? '最近 30 天'
                            : '更早';
        (groups[key] ??= []).add(c);
      }
    }
    const order = ['搜索结果', '置顶', '今天', '昨天', '最近 7 天', '最近 30 天', '更早'];

    return Drawer(
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 14, 18, 10),
              child: Row(children: [
                const Mascot(size: 36),
                const SizedBox(width: 10),
                const Text('小暖', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700)),
                const Spacer(),
              ]),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 10),
              child: Pressable(
                scale: 0.98,
                onTap: () {
                  Navigator.pop(context);
                  ref.read(chatProvider.notifier).newChat();
                },
                child: Container(
                  height: 50,
                  decoration: BoxDecoration(gradient: Palette.brandGradient, borderRadius: BorderRadius.circular(16), boxShadow: [
                    BoxShadow(color: Palette.brand.withValues(alpha: 0.28), blurRadius: 14, offset: const Offset(0, 5)),
                  ]),
                  child: const Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                    Icon(Icons.add_rounded, color: Colors.white, size: 24),
                    SizedBox(width: 6),
                    Text('新对话', style: TextStyle(color: Colors.white, fontSize: 17, fontWeight: FontWeight.w600)),
                  ]),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 6),
              child: TextField(
                controller: _search,
                onChanged: _onSearch,
                style: const TextStyle(fontSize: 16),
                decoration: InputDecoration(
                  hintText: '搜索聊天记录',
                  prefixIcon: Icon(Icons.search_rounded, color: p.faint),
                  suffixIcon: _search.text.isEmpty
                      ? null
                      : IconButton(
                          icon: Icon(Icons.close_rounded, color: p.faint),
                          onPressed: () {
                            _search.clear();
                            _onSearch('');
                          }),
                  isDense: true,
                  contentPadding: const EdgeInsets.symmetric(vertical: 12),
                  fillColor: p.surface,
                  enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide.none),
                  focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide(color: p.accent)),
                ),
              ),
            ),
            Expanded(
              child: RefreshIndicator(
                onRefresh: () => ref.read(conversationsProvider.notifier).refresh(),
                child: items.isEmpty
                    ? ListView(children: [
                        const SizedBox(height: 80),
                        Center(
                          child: Text(
                            _searching
                                ? '正在搜索…'
                                : _results != null
                                    ? '没有找到相关对话'
                                    : list.loaded
                                        ? '还没有聊天记录\n点上面的「新对话」开始吧'
                                        : '加载中…',
                            textAlign: TextAlign.center,
                            style: TextStyle(color: p.faint, fontSize: 15, height: 1.6),
                          ),
                        ),
                      ])
                    : NotificationListener<ScrollNotification>(
                        onNotification: (n) {
                          if (_results == null && n.metrics.extentAfter < 400) ref.read(conversationsProvider.notifier).loadMore();
                          return false;
                        },
                        child: ListView(
                          padding: const EdgeInsets.fromLTRB(10, 4, 10, 16),
                          children: [
                            for (final g in order)
                              if (groups[g] != null) ...[
                                Padding(
                                  padding: const EdgeInsets.fromLTRB(10, 14, 10, 6),
                                  child: Text(g, style: TextStyle(fontSize: 13.5, color: p.faint, fontWeight: FontWeight.w600)),
                                ),
                                for (final c in groups[g]!)
                                  _ConvTile(conv: c, selected: c.id == currentId, onTap: () => _open(c), onLongPress: () => _menu(c)),
                              ],
                          ],
                        ),
                      ),
              ),
            ),
            Divider(color: p.line, height: 1),
            if (user != null)
              InkWell(
                onTap: () {
                  Navigator.pop(context);
                  Navigator.of(context).push(MaterialPageRoute(builder: (_) => const SettingsPage()));
                },
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(18, 12, 12, 12),
                  child: Row(children: [
                    Avatar(label: user.displayName, emoji: user.avatar, size: 42),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text(user.displayName, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
                    ),
                    Icon(Icons.settings_outlined, color: p.muted),
                    const SizedBox(width: 6),
                  ]),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _ConvTile extends StatelessWidget {
  const _ConvTile({required this.conv, required this.selected, required this.onTap, required this.onLongPress});
  final Conversation conv;
  final bool selected;
  final VoidCallback onTap, onLongPress;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.98,
      onTap: onTap,
      onLongPress: onLongPress,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        margin: const EdgeInsets.symmetric(vertical: 1),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
        decoration: BoxDecoration(color: selected ? p.surface : Colors.transparent, borderRadius: BorderRadius.circular(14)),
        child: Row(children: [
          if (conv.pinned) Padding(padding: const EdgeInsets.only(right: 6), child: Icon(Icons.push_pin_rounded, size: 15, color: p.accent)),
          Expanded(
            child: Text(
              conv.title.isEmpty ? '新对话' : conv.title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 16, fontWeight: selected ? FontWeight.w600 : FontWeight.w400, color: p.text),
            ),
          ),
        ]),
      ),
    );
  }
}

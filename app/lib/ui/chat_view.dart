import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../state/chat.dart';
import 'messages.dart';
import 'theme.dart';
import 'widgets.dart';

/// The message list.
///
/// Uses a CustomScrollView with a `center` sliver:
///  * Messages before `state.anchor` grow upward from the anchor, so loading
///    older history never shifts what's on screen.
///  * Messages from the anchor on grow downward. After sending, the anchor is
///    the new question: it slides to the top and the answer streams in below
///    it without moving the text the user is reading.
///  * When just browsing (no anchor), the view is bottom-aligned like a
///    classic chat.
class ChatView extends ConsumerStatefulWidget {
  const ChatView({super.key});

  @override
  ConsumerState<ChatView> createState() => _ChatViewState();
}

class _ChatViewState extends ConsumerState<ChatView> {
  final _scroll = ScrollController();
  int _epoch = -1;
  String? _anchorId;
  bool _showDown = false;

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  void _afterLayout(void Function(ScrollPosition pos) f) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _scroll.hasClients) f(_scroll.position);
    });
  }

  bool _onMetrics(ScrollMetrics m) {
    final down = m.maxScrollExtent - m.pixels > 160;
    if (down != _showDown) setState(() => _showDown = down);
    if (m.pixels - m.minScrollExtent < 600) ref.read(chatProvider.notifier).loadOlder();
    return false;
  }

  void _toBottom() {
    final pos = _scroll.position;
    _scroll.animateTo(pos.maxScrollExtent, duration: const Duration(milliseconds: 420), curve: Curves.easeOutCubic);
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(chatProvider);
    final msgs = s.messages;
    final anchor = s.anchor.clamp(0, msgs.length);
    final topMode = anchor < msgs.length;
    final anchorId = topMode ? msgs[anchor].id : null;

    if (s.epoch != _epoch) {
      _epoch = s.epoch;
      _anchorId = anchorId;
      _afterLayout((pos) => pos.jumpTo(0));
    } else if (anchorId != _anchorId) {
      final idChangeOnly = _anchorId != null && anchorId != null && _anchorId!.startsWith('local-') && !anchorId.startsWith('local-');
      _anchorId = anchorId;
      if (anchorId != null && !idChangeOnly) {
        // New turn: start where the old bottom was, then glide the question to the top.
        _afterLayout((pos) {
          final from = math.max(pos.minScrollExtent, -pos.viewportDimension + 24);
          pos.jumpTo(from);
          _scroll.animateTo(0, duration: const Duration(milliseconds: 460), curve: Curves.easeOutCubic);
        });
      }
    }

    final centerKey = ValueKey('center-${anchorId ?? 'end'}-${s.epoch}');
    final lastAssistant = msgs.isNotEmpty && !msgs.last.isUser ? msgs.last.id : null;

    Widget item(Message m) => m.isUser
        ? UserMessage(key: ValueKey('u-${m.id}'), message: m)
        : AssistantMessage(key: ValueKey('a-${m.id}'), message: m, isLast: m.id == lastAssistant);

    final beforeExtra = topMode ? 0 : 1; // bottom padding lives in the "before" list when bottom-aligned
    final before = SliverList(
      delegate: SliverChildBuilderDelegate(
        (context, i) {
          if (!topMode && i == 0) return const SizedBox(height: 20);
          final idx = anchor - 1 - (i - beforeExtra);
          if (idx < 0) return _TopCap(loading: s.loadingOlder, hasMore: s.hasMore);
          return item(msgs[idx]);
        },
        childCount: anchor + beforeExtra + 1,
      ),
    );
    final after = SliverList(
      key: centerKey,
      delegate: SliverChildBuilderDelegate((context, i) => item(msgs[anchor + i]), childCount: msgs.length - anchor),
    );

    return Stack(
      children: [
        NotificationListener<ScrollMetricsNotification>(
          // Ignore horizontal scrollers inside messages (tables, code blocks).
          onNotification: (n) => n.depth == 0 && n.metrics.axis == Axis.vertical && _onMetrics(n.metrics),
          child: NotificationListener<ScrollUpdateNotification>(
            onNotification: (n) => n.depth == 0 && n.metrics.axis == Axis.vertical && _onMetrics(n.metrics),
            child: CustomScrollView(
              controller: _scroll,
              center: centerKey,
              anchor: topMode ? 0.0 : 1.0,
              keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
              physics: const AlwaysScrollableScrollPhysics(parent: BouncingScrollPhysics()),
              slivers: [
                before,
                after,
                if (topMode) const SliverToBoxAdapter(child: SizedBox(height: 28)),
              ],
            ),
          ),
        ),
        Positioned(
          right: 16,
          bottom: 8,
          child: IgnorePointer(
            ignoring: !_showDown,
            child: AnimatedOpacity(
              opacity: _showDown ? 1 : 0,
              duration: const Duration(milliseconds: 200),
              child: AnimatedScale(
                scale: _showDown ? 1 : 0.8,
                duration: const Duration(milliseconds: 200),
                child: SoftIconButton(icon: Icons.keyboard_arrow_down_rounded, filled: true, tooltip: '到底部', onTap: _toBottom),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class _TopCap extends StatelessWidget {
  const _TopCap({required this.loading, required this.hasMore});
  final bool loading, hasMore;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return SizedBox(
      height: 56,
      child: Center(
        child: loading || hasMore
            ? SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: p.faint))
            : const SizedBox(),
      ),
    );
  }
}

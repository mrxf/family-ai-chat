import 'package:flutter/material.dart';

import '../../../../core/models/models.dart';
import '../../../widgets/markdown.dart';
import 'component_card.dart';

// ======================= Inline Interleaving Host =======================

/// Splits assistant [content] by Unicode rune offsets ([MessageComponent.pos])
/// so rich interactive cards appear at the exact position the model invoked them.
class InterleavedAssistantBody extends StatelessWidget {
  const InterleavedAssistantBody({
    super.key,
    required this.messageId,
    required this.content,
    required this.components,
    required this.streaming,
    required this.animate,
    required this.isLastMessage,
  });

  final String messageId;
  final String content;
  final List<MessageComponent> components;
  final bool streaming;
  final bool animate;
  final bool isLastMessage;

  @override
  Widget build(BuildContext context) {
    if (components.isEmpty) {
      if (content.isEmpty) return const SizedBox.shrink();
      return Md(content, streaming: streaming, animate: animate);
    }

    final runes = content.runes.toList(growable: false);
    final sorted = [...components]..sort((a, b) => a.pos.compareTo(b.pos));

    final children = <Widget>[];
    var cursor = 0;

    void addTextSegment(String raw, {required bool isTail}) {
      final trimmed = raw.trim();
      if (trimmed.isEmpty) return;
      if (children.isNotEmpty) children.add(const SizedBox(height: 10));
      children.add(Md(trimmed, streaming: streaming && isTail, animate: animate));
    }

    for (final comp in sorted) {
      final pos = comp.pos.clamp(0, runes.length);
      if (pos > cursor) {
        addTextSegment(String.fromCharCodes(runes.sublist(cursor, pos)), isTail: false);
        cursor = pos;
      }
      if (children.isNotEmpty) children.add(const SizedBox(height: 10));
      children.add(
        _ComponentEntrance(
          key: ValueKey('comp-${comp.id}'),
          animate: animate,
          child: ChatComponentCard(
            messageId: messageId,
            component: comp,
            isLastMessage: isLastMessage,
          ),
        ),
      );
    }

    if (cursor < runes.length) {
      addTextSegment(String.fromCharCodes(runes.sublist(cursor)), isTail: true);
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: children,
    );
  }
}

class _ComponentEntrance extends StatefulWidget {
  const _ComponentEntrance({super.key, required this.animate, required this.child});
  final bool animate;
  final Widget child;

  @override
  State<_ComponentEntrance> createState() => _ComponentEntranceState();
}

class _ComponentEntranceState extends State<_ComponentEntrance> {
  late bool _visible = !widget.animate;

  @override
  void initState() {
    super.initState();
    if (!_visible) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() => _visible = true);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.animate) return widget.child;
    return AnimatedOpacity(
      opacity: _visible ? 1 : 0,
      duration: const Duration(milliseconds: 320),
      curve: Curves.easeOutCubic,
      child: AnimatedSlide(
        offset: _visible ? Offset.zero : const Offset(0, 0.03),
        duration: const Duration(milliseconds: 320),
        curve: Curves.easeOutCubic,
        child: widget.child,
      ),
    );
  }
}

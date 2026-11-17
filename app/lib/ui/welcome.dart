import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import '../state/chat.dart';
import '../state/session.dart';
import 'admin/models_page.dart';
import 'mascot_anim.dart';
import 'theme.dart';
import 'widgets.dart';

const _defaultSuggestions = [
  ('🎂', '帮我写一段给家人的生日祝福'),
  ('🥗', '高血压饮食上要注意什么？'),
  ('🍳', '教我做一道简单的家常菜'),
  ('🛡️', '怎么分辨诈骗电话和短信？'),
  ('😴', '晚上睡不着有什么好办法？'),
  ('📖', '给我讲个有意思的历史小故事'),
  ('🧳', '帮我列一份出门旅行的行李清单'),
  ('💡', '用大白话解释一下什么是通货膨胀'),
  ('📱', '手机内存满了怎么清理？'),
  ('🌱', '阳台适合种哪些好养的花？'),
  ('🧮', '帮孩子出 10 道口算练习题'),
  ('✈️', '周末带孩子去哪里玩比较好？'),
];

// Shown (one at a time) when the server can draw pictures.
const _drawSuggestions = [
  ('🎨', '帮我画一只可爱的小猫'),
  ('🖼️', '画一张春天公园的水彩画'),
  ('🧧', '帮我画一张新年贺卡，写上「阖家幸福」'),
  ('🌄', '画一张适合做手机壁纸的风景画'),
];

String greeting() {
  final h = DateTime.now().hour;
  if (h < 5) return '夜深了';
  if (h < 11) return '早上好';
  if (h < 13) return '中午好';
  if (h < 18) return '下午好';
  return '晚上好';
}

/// Empty-chat screen: mascot, greeting, suggestions and assistants.
class Welcome extends ConsumerStatefulWidget {
  const Welcome({super.key, required this.onSuggestion});
  final void Function(String text) onSuggestion;

  @override
  ConsumerState<Welcome> createState() => _WelcomeState();
}

class _WelcomeState extends ConsumerState<Welcome> {
  late List<(String, String)> _picks = _shuffle();
  late final _drawPick = _drawSuggestions[math.Random().nextInt(_drawSuggestions.length)];

  List<(String, String)> _shuffle() => (List.of(_defaultSuggestions)..shuffle(math.Random())).take(4).toList();

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final boot = ref.watch(sessionProvider).boot;
    if (boot == null) return const SizedBox();
    final assistantId = ref.watch(chatProvider.select((s) => s.assistantId));
    final assistant = boot.assistant(assistantId);
    final name = boot.user.displayName;

    final List<(String, String)> suggestions = assistant != null
        ? [for (final s in assistant.suggestions) ('', s)]
        : boot.imageGeneration && boot.imagesLeft != 0
            ? [_drawPick, ..._picks.take(3)]
            : _picks;

    return LayoutBuilder(builder: (context, c) {
      return SingleChildScrollView(
        keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
        padding: const EdgeInsets.fromLTRB(20, 12, 20, 20),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: c.maxHeight - 32),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const SizedBox(height: 8),
              Center(
                child: assistant == null
                    ? const Floating(child: AnimatedMascot(size: 132, greet: true))
                    : Floating(child: Avatar(label: assistant.name, emoji: assistant.avatar, size: 96)),
              ),
              const SizedBox(height: 18),
              Text(
                assistant == null ? '${greeting()}，$name' : assistant.name,
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 26, fontWeight: FontWeight.w700, letterSpacing: 0.3),
              ),
              const SizedBox(height: 8),
              Text(
                assistant == null ? '我是小暖，有什么可以帮你的？' : (assistant.greeting.isNotEmpty ? assistant.greeting : assistant.description),
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 16.5, color: p.muted, height: 1.5),
              ),
              if (boot.models.isEmpty) ...[
                const SizedBox(height: 22),
                _NoModels(isAdmin: boot.user.isAdmin),
              ],
              const SizedBox(height: 28),
              for (final (emoji, text) in suggestions)
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: _SuggestionCard(emoji: emoji, text: text, onTap: () => widget.onSuggestion(text)),
                ),
              if (assistant == null)
                Center(
                  child: TextButton.icon(
                    onPressed: () => setState(() => _picks = _shuffle()),
                    icon: Icon(Icons.refresh_rounded, size: 18, color: p.muted),
                    label: Text('换一批', style: TextStyle(color: p.muted, fontSize: 15)),
                  ),
                ),
              if (assistant == null && boot.assistants.isNotEmpty) ...[
                const SizedBox(height: 14),
                Padding(
                  padding: const EdgeInsets.only(left: 4, bottom: 10),
                  child: Text('找专门的助手', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: p.muted)),
                ),
                _AssistantStrip(assistants: boot.assistants),
              ],
            ],
          ),
        ),
      );
    });
  }
}

/// Shown when the server has no usable model yet (fresh install, or all disabled).
class _NoModels extends StatelessWidget {
  const _NoModels({required this.isAdmin});
  final bool isAdmin;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.98,
      onTap: isAdmin ? () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const ModelsPage())) : null,
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.accent.withValues(alpha: 0.4))),
        child: Row(children: [
          Icon(isAdmin ? Icons.hub_rounded : Icons.hourglass_empty_rounded, color: p.accent),
          const SizedBox(width: 12),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(isAdmin ? '还没有可用的模型' : '小暖还没准备好', style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
              const SizedBox(height: 2),
              Text(isAdmin ? '点这里添加模型服务，家人就能开始聊天了' : '请联系家里的管理员配置一下模型',
                  style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.4)),
            ]),
          ),
          if (isAdmin) Icon(Icons.chevron_right_rounded, color: p.accent),
        ]),
      ),
    );
  }
}

class _SuggestionCard extends StatelessWidget {
  const _SuggestionCard({required this.emoji, required this.text, required this.onTap});
  final String emoji, text;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.98,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        decoration: BoxDecoration(
          color: p.surface,
          borderRadius: BorderRadius.circular(18),
          border: Border.all(color: p.line),
        ),
        child: Row(children: [
          if (emoji.isNotEmpty) ...[Text(emoji, style: const TextStyle(fontSize: 20)), const SizedBox(width: 12)],
          Expanded(child: Text(text, style: const TextStyle(fontSize: 16.5, height: 1.4))),
          Icon(Icons.north_east_rounded, size: 18, color: p.faint),
        ]),
      ),
    );
  }
}

class _AssistantStrip extends ConsumerWidget {
  const _AssistantStrip({required this.assistants});
  final List<Assistant> assistants;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    return SizedBox(
      height: 128,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        clipBehavior: Clip.none,
        itemCount: assistants.length,
        separatorBuilder: (_, _) => const SizedBox(width: 10),
        itemBuilder: (_, i) {
          final a = assistants[i];
          return Pressable(
            scale: 0.97,
            onTap: () => ref.read(chatProvider.notifier).newChat(assistantId: a.id),
            child: Container(
              width: 132,
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(20)),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Avatar(label: a.name, emoji: a.avatar, size: 38),
                const SizedBox(height: 10),
                Text(a.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                const SizedBox(height: 3),
                Text(a.description, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.35)),
              ]),
            ),
          );
        },
      ),
    );
  }
}

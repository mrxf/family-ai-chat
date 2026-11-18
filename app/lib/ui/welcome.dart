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
    final isPrivate = ref.watch(chatProvider.select((s) => s.isPrivate));
    final assistant = boot.assistant(assistantId);
    final name = boot.user.displayName;

    if (isPrivate) {
      return _PrivateWelcome(
        assistant: assistant,
        assistants: boot.assistants,
        hasModels: boot.models.isNotEmpty,
        isAdmin: boot.user.isAdmin,
        onSuggestion: widget.onSuggestion,
      );
    }

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

class _PrivateWelcome extends ConsumerWidget {
  const _PrivateWelcome({
    required this.assistant,
    required this.assistants,
    required this.hasModels,
    required this.isAdmin,
    required this.onSuggestion,
  });

  final Assistant? assistant;
  final List<Assistant> assistants;
  final bool hasModels, isAdmin;
  final void Function(String text) onSuggestion;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final a = assistant;
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
                child: a == null
                    ? Container(
                        width: 88,
                        height: 88,
                        decoration: BoxDecoration(
                          color: p.privateSoft,
                          shape: BoxShape.circle,
                          border: Border.all(color: p.privateAccent.withValues(alpha: 0.25), width: 1.5),
                        ),
                        child: Icon(Icons.visibility_off_rounded, size: 40, color: p.privateAccent),
                      )
                    : Stack(
                        clipBehavior: Clip.none,
                        children: [
                          Avatar(label: a.name, emoji: a.avatar, size: 88),
                          Positioned(
                            right: -2,
                            bottom: -2,
                            child: Container(
                              width: 30,
                              height: 30,
                              decoration: BoxDecoration(
                                color: p.privateAccent,
                                shape: BoxShape.circle,
                                border: Border.all(color: p.bg, width: 2.5),
                              ),
                              child: const Icon(Icons.visibility_off_rounded, size: 15, color: Colors.white),
                            ),
                          ),
                        ],
                      ),
              ),
              const SizedBox(height: 18),
              Text(
                a == null ? '隐私对话' : '隐私对话 · ${a.name}',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 25, fontWeight: FontWeight.w700, letterSpacing: 0.3, color: p.text),
              ),
              const SizedBox(height: 8),
              Text(
                a == null ? '放心问，关闭后自动清除不留痕' : (a.greeting.isNotEmpty ? a.greeting : a.description),
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 16, color: p.muted, height: 1.5),
              ),
              if (!hasModels) ...[
                const SizedBox(height: 22),
                _NoModels(isAdmin: isAdmin),
              ],
              const SizedBox(height: 24),
              Container(
                padding: const EdgeInsets.fromLTRB(18, 16, 18, 16),
                decoration: BoxDecoration(
                  color: p.privateSoft,
                  borderRadius: BorderRadius.circular(20),
                  border: Border.all(color: p.privateAccent.withValues(alpha: 0.2)),
                ),
                child: const Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _PrivatePoint(
                      icon: Icons.history_toggle_off_rounded,
                      title: '不进聊天记录',
                      body: '不会出现在左侧历史列表和搜索结果里',
                    ),
                    SizedBox(height: 14),
                    _PrivatePoint(
                      icon: Icons.favorite_border_rounded,
                      title: '不读取也不写入记忆',
                      body: '不参考以往记忆，小暖也不会记住这次聊的事',
                    ),
                    SizedBox(height: 14),
                    _PrivatePoint(
                      icon: Icons.auto_delete_outlined,
                      title: '关闭即清理',
                      body: '退出或切换对话后，消息和图片立即删除',
                    ),
                  ],
                ),
              ),
              if (a != null) ...[
                if (a.suggestions.isNotEmpty) ...[
                  const SizedBox(height: 18),
                  for (final s in a.suggestions)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: _SuggestionCard(emoji: '', text: s, onTap: () => onSuggestion(s)),
                    ),
                ],
                Center(
                  child: TextButton.icon(
                    onPressed: () => ref.read(chatProvider.notifier).newChat(isPrivate: true),
                    icon: Icon(Icons.restart_alt_rounded, size: 18, color: p.privateAccent),
                    label: Text('切回通用隐私对话', style: TextStyle(color: p.privateAccent, fontSize: 15)),
                  ),
                ),
              ],
              if (a == null && assistants.isNotEmpty) ...[
                const SizedBox(height: 20),
                Padding(
                  padding: const EdgeInsets.only(left: 4, bottom: 10),
                  child: Text('找专门的助手（仍在隐私对话中）', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: p.muted)),
                ),
                _AssistantStrip(assistants: assistants, isPrivate: true),
              ],
            ],
          ),
        ),
      );
    });
  }
}

class _PrivatePoint extends StatelessWidget {
  const _PrivatePoint({required this.icon, required this.title, required this.body});
  final IconData icon;
  final String title, body;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Icon(icon, size: 21, color: p.privateAccent),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600, color: p.text)),
              const SizedBox(height: 2),
              Text(body, style: TextStyle(fontSize: 14, color: p.muted, height: 1.4)),
            ],
          ),
        ),
      ],
    );
  }
}

class _AssistantStrip extends ConsumerWidget {
  const _AssistantStrip({required this.assistants, this.isPrivate = false});
  final List<Assistant> assistants;
  final bool isPrivate;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final h = MediaQuery.textScalerOf(context).scale(112).clamp(128.0, 168.0);
    return SizedBox(
      height: h,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        clipBehavior: Clip.none,
        itemCount: assistants.length,
        separatorBuilder: (_, _) => const SizedBox(width: 10),
        itemBuilder: (_, i) {
          final a = assistants[i];
          return Pressable(
            scale: 0.97,
            onTap: () => ref.read(chatProvider.notifier).newChat(assistantId: a.id, isPrivate: isPrivate),
            child: Container(
              width: 136,
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(20)),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Avatar(label: a.name, emoji: a.avatar, size: 38),
                const SizedBox(height: 10),
                Text(a.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                const SizedBox(height: 3),
                Flexible(
                  child: Text(a.description, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.35)),
                ),
              ]),
            ),
          );
        },
      ),
    );
  }
}

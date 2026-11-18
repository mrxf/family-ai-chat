import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/tts.dart';
import '../state/chat.dart';
import '../state/conversations.dart';
import '../state/session.dart';
import 'chat_view.dart';
import 'composer.dart';
import 'drawer.dart';
import 'icons.dart';
import 'share_page.dart';
import 'sheets.dart';
import 'theme.dart';
import 'update.dart';
import 'welcome.dart';
import 'widgets.dart';

class HomePage extends ConsumerStatefulWidget {
  const HomePage({super.key});

  @override
  ConsumerState<HomePage> createState() => _HomePageState();
}

class _HomePageState extends ConsumerState<HomePage> with WidgetsBindingObserver {
  final _composerKey = GlobalKey<ComposerState>();
  StreamSubscription<String>? _notices;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _notices = ref.read(chatProvider.notifier).notices.stream.listen((m) {
      if (mounted) showToast(context, m);
    });
    Future.delayed(const Duration(seconds: 2), () {
      if (mounted) checkForUpdate(context, ref, silent: true);
    });
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _notices?.cancel();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      ref.read(chatProvider.notifier).onResume();
      ref.read(sessionProvider.notifier).refresh();
    } else if (state == AppLifecycleState.paused) {
      Speaker.instance.stop();
    }
  }

  void _unfocusComposer() {
    _composerKey.currentState?.unfocus();
    FocusManager.instance.primaryFocus?.unfocus();
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(chatProvider.select((s) => s.epoch), (_, _) => _unfocusComposer());
    final p = context.pal;
    final chat = ref.watch(chatProvider);
    final boot = ref.watch(sessionProvider).boot;
    // Can be null for a frame while logging out (the page is animating away).
    if (boot == null) return const Scaffold();
    final model = boot.effectiveModel(chat.modelId, chat.assistantId);
    final assistant = boot.assistant(chat.assistantId);
    final title = chat.isPrivate
        ? (assistant == null ? '隐私对话' : '隐私对话 · ${assistant.name}')
        : chat.conversationId == null
            ? (assistant?.name ?? '小暖')
            : ref.watch(_titleProvider(chat.conversationId!)) ?? (assistant?.name ?? '小暖');

    return Scaffold(
      drawer: const HistoryDrawer(),
      drawerEdgeDragWidth: MediaQuery.sizeOf(context).width * 0.25,
      onDrawerChanged: (opened) {
        if (opened) _unfocusComposer();
      },
      appBar: AppBar(
        leading: Builder(
          builder: (ctx) => IconButton(
            tooltip: '聊天记录',
            icon: const Icon(AppIcons.menu, size: 24),
            onPressed: () {
              _unfocusComposer();
              Scaffold.of(ctx).openDrawer();
            },
          ),
        ),
        title: Pressable(
          scale: 0.97,
          onTap: () async {
            final id = await showModelPicker(context, ref);
            if (id != null) ref.read(chatProvider.notifier).setModel(id);
          },
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            AnimatedSwitcher(
              duration: const Duration(milliseconds: 250),
              child: Row(
                key: ValueKey('${chat.isPrivate}-$title'),
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (chat.isPrivate) ...[
                    Icon(AppIcons.privateChat, size: 17, color: p.privateAccent),
                    const SizedBox(width: 5),
                  ],
                  Flexible(
                    child: Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 17.5,
                        fontWeight: FontWeight.w600,
                        color: chat.isPrivate ? p.privateAccent : p.text,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            if (model != null)
              Row(mainAxisSize: MainAxisSize.min, children: [
                Text(model.displayName, style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w400)),
                const SizedBox(width: 3),
                Icon(AppIcons.chevronDown, size: 13.5, color: p.muted),
              ]),
          ]),
        ),
        actions: [
          if (chat.isPrivate)
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Tooltip(
                message: '退出隐私对话',
                child: Pressable(
                  onTap: () {
                    FocusScope.of(context).unfocus();
                    _composerKey.currentState?.clearDraft();
                    ref.read(chatProvider.notifier).exitPrivateChat();
                  },
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(minHeight: 44, minWidth: 68),
                    child: Center(
                      child: Container(
                        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
                        decoration: BoxDecoration(
                          color: p.privateSoft,
                          borderRadius: BorderRadius.circular(20),
                          border: Border.all(color: p.privateAccent.withValues(alpha: 0.3)),
                        ),
                        child: Row(mainAxisSize: MainAxisSize.min, children: [
                          Icon(AppIcons.close, size: 15, color: p.privateAccent),
                          const SizedBox(width: 4),
                          Text('退出', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: p.privateAccent)),
                        ]),
                      ),
                    ),
                  ),
                ),
              ),
            )
          else ...[
            if (!chat.isEmpty && !chat.loading)
              IconButton(
                tooltip: '分享对话',
                icon: const Icon(AppIcons.share, size: 22),
                onPressed: () => openShareChat(context, ref),
              ),
            IconButton(
              tooltip: '新对话',
              icon: const Icon(AppIcons.newChat, size: 22),
              onPressed: chat.conversationId == null && chat.assistantId == null ? null : () => ref.read(chatProvider.notifier).newChat(),
            ),
            const SizedBox(width: 4),
          ],
        ],
      ),
      body: SafeArea(
        top: false,
        child: Column(
          children: [
            if (chat.isPrivate && !chat.isEmpty && !chat.loading) const _PrivateBanner(),
            Expanded(
              child: GestureDetector(
                behavior: HitTestBehavior.translucent,
                onTap: () => FocusScope.of(context).unfocus(),
                child: AnimatedSwitcher(
                  duration: const Duration(milliseconds: 280),
                  switchInCurve: Curves.easeOut,
                  child: chat.loading
                      ? const Center(key: ValueKey('loading'), child: CircularProgressIndicator(strokeWidth: 2.4))
                      : chat.isEmpty
                          ? Welcome(key: ValueKey('welcome-${chat.epoch}'), onSuggestion: (t) => _composerKey.currentState?.sendText(t))
                          : const ChatView(key: ValueKey('chat')),
                ),
              ),
            ),
            Composer(key: _composerKey),
          ],
        ),
      ),
    );
  }
}

class _PrivateBanner extends StatelessWidget {
  const _PrivateBanner();

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 7),
      decoration: BoxDecoration(
        color: p.privateSoft,
        border: Border(
          top: BorderSide(color: p.privateAccent.withValues(alpha: 0.14)),
          bottom: BorderSide(color: p.privateAccent.withValues(alpha: 0.14)),
        ),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(AppIcons.privateChat, size: 15, color: p.privateAccent),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              '隐私对话中 · 离开后记录与图片自动清除，小暖不会记住',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 13, color: p.privateAccent, fontWeight: FontWeight.w500),
            ),
          ),
        ],
      ),
    );
  }
}

/// Title of a conversation from the history list (updates live when the auto title arrives).
final _titleProvider = Provider.family<String?, String>((ref, id) {
  final items = ref.watch(conversationsTitlesProvider);
  final t = items[id];
  return t == null || t.isEmpty ? null : t;
});

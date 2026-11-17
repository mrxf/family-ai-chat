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

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final chat = ref.watch(chatProvider);
    final boot = ref.watch(sessionProvider).boot;
    // Can be null for a frame while logging out (the page is animating away).
    if (boot == null) return const Scaffold();
    final model = boot.effectiveModel(chat.modelId, chat.assistantId);
    final assistant = boot.assistant(chat.assistantId);
    final title = chat.conversationId == null
        ? (assistant?.name ?? '小暖')
        : ref.watch(_titleProvider(chat.conversationId!)) ?? (assistant?.name ?? '小暖');

    return Scaffold(
      drawer: const HistoryDrawer(),
      drawerEdgeDragWidth: MediaQuery.sizeOf(context).width * 0.25,
      appBar: AppBar(
        leading: Builder(
          builder: (ctx) => IconButton(
            tooltip: '聊天记录',
            icon: const Icon(Icons.menu_rounded, size: 27),
            onPressed: () {
              FocusScope.of(context).unfocus();
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
              child: Text(title, key: ValueKey(title), maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 17.5, fontWeight: FontWeight.w600)),
            ),
            if (model != null)
              Row(mainAxisSize: MainAxisSize.min, children: [
                Text(model.displayName, style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w400)),
                Icon(Icons.expand_more_rounded, size: 16, color: p.muted),
              ]),
          ]),
        ),
        actions: [
          IconButton(
            tooltip: '新对话',
            icon: const Icon(Icons.add_comment_outlined, size: 25),
            onPressed: chat.conversationId == null && chat.assistantId == null ? null : () => ref.read(chatProvider.notifier).newChat(),
          ),
          const SizedBox(width: 4),
        ],
      ),
      body: SafeArea(
        top: false,
        child: Column(
          children: [
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

/// Title of a conversation from the history list (updates live when the auto title arrives).
final _titleProvider = Provider.family<String?, String>((ref, id) {
  final items = ref.watch(conversationsTitlesProvider);
  final t = items[id];
  return t == null || t.isEmpty ? null : t;
});

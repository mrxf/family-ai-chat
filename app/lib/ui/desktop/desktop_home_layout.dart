import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/chat.dart';
import '../adaptive/breakpoints.dart';
import '../features/chat/chat_app_bar.dart';
import '../features/chat/chat_pane.dart';
import '../features/chat/composer/composer.dart';
import '../features/conversations/conversation_panel.dart';
import '../features/conversations/search_page.dart';
import '../theme/theme.dart';

/// Expanded windows (desktop, landscape tablets): a permanent conversation
/// sidebar next to the chat, plus keyboard shortcuts.
///
/// Shortcuts (⌘ on macOS, Ctrl elsewhere): N = new chat, K = search history.
class DesktopHomeLayout extends ConsumerWidget {
  const DesktopHomeLayout({super.key, required this.composerKey});

  final GlobalKey<ComposerState> composerKey;

  Future<void> _search(BuildContext context, WidgetRef ref) async {
    final id = await openSearchPage(context);
    if (id != null) ref.read(chatProvider.notifier).open(id);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final isMac = Theme.of(context).platform == TargetPlatform.macOS;
    SingleActivator key(LogicalKeyboardKey k) => SingleActivator(k, meta: isMac, control: !isMac);
    return CallbackShortcuts(
      bindings: {
        key(LogicalKeyboardKey.keyN): () {
          if (!ref.read(chatProvider).isPrivate) ref.read(chatProvider.notifier).newChat();
        },
        key(LogicalKeyboardKey.keyK): () => _search(context, ref),
      },
      // Keeps the shortcuts live when nothing else holds focus.
      child: Focus(
        autofocus: true,
        child: Scaffold(
          body: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              SizedBox(
                width: ContentWidth.sidebar,
                child: Material(
                  color: p.surfaceAlt,
                  child: const SafeArea(right: false, child: ConversationPanel()),
                ),
              ),
              VerticalDivider(width: 1, thickness: 1, color: p.line),
              Expanded(
                child: Scaffold(
                  appBar: ChatAppBar(onExitPrivate: () => composerKey.currentState?.clearDraft()),
                  body: SafeArea(top: false, left: false, child: ChatPane(composerKey: composerKey)),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

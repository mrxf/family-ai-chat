import 'package:flutter/material.dart';

import '../features/chat/chat_app_bar.dart';
import '../features/chat/chat_pane.dart';
import '../features/chat/composer/composer.dart';
import '../features/conversations/conversation_panel.dart';
import '../theme/icons.dart';

/// Compact windows (phones, narrow desktop windows): the chat fills the
/// screen and the conversation list slides in from the left.
class MobileHomeLayout extends StatelessWidget {
  const MobileHomeLayout({super.key, required this.composerKey, required this.onUnfocus});

  final GlobalKey<ComposerState> composerKey;

  /// Hides the keyboard before the drawer covers the composer.
  final VoidCallback onUnfocus;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      drawer: const HistoryDrawer(),
      drawerEnableOpenDragGesture: false,
      onDrawerChanged: (opened) {
        if (opened) onUnfocus();
      },
      appBar: ChatAppBar(
        leading: Builder(
          builder: (ctx) => IconButton(
            tooltip: '聊天记录',
            icon: const AppIcon(AppIcons.menu, size: 24),
            onPressed: () {
              onUnfocus();
              Scaffold.of(ctx).openDrawer();
            },
          ),
        ),
        onExitPrivate: () => composerKey.currentState?.clearDraft(),
      ),
      body: SafeArea(top: false, child: ChatPane(composerKey: composerKey)),
    );
  }
}

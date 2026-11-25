import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/chat.dart';
import '../features/chat/chat_app_bar.dart';
import '../features/chat/chat_pane.dart';
import '../features/chat/composer/composer.dart';
import '../features/conversations/conversation_panel.dart';
import '../theme/icons.dart';

/// Compact windows (phones, narrow desktop windows): the chat fills the
/// screen and the conversation list slides in from the left.
class MobileHomeLayout extends ConsumerStatefulWidget {
  const MobileHomeLayout({super.key, required this.composerKey, required this.onUnfocus});

  final GlobalKey<ComposerState> composerKey;

  /// Hides the keyboard before the drawer covers the composer.
  final VoidCallback onUnfocus;

  @override
  ConsumerState<MobileHomeLayout> createState() => _MobileHomeLayoutState();
}

class _MobileHomeLayoutState extends ConsumerState<MobileHomeLayout> {
  final _scaffoldKey = GlobalKey<ScaffoldState>();

  void _handleBack() {
    if (_scaffoldKey.currentState?.isDrawerOpen == true) {
      _scaffoldKey.currentState?.closeDrawer();
      return;
    }
    widget.onUnfocus();
    final chat = ref.read(chatProvider);
    if (chat.isPrivate) {
      widget.composerKey.currentState?.clearDraft();
      ref.read(chatProvider.notifier).exitPrivateChat();
    } else {
      ref.read(chatProvider.notifier).newChat();
    }
  }

  @override
  Widget build(BuildContext context) {
    final isAtHome = ref.watch(chatProvider.select((s) => s.isAtHome));
    return PopScope(
      canPop: isAtHome,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) return;
        _handleBack();
      },
      child: Scaffold(
        key: _scaffoldKey,
        drawer: const HistoryDrawer(),
        drawerEnableOpenDragGesture: false,
        onDrawerChanged: (opened) {
          if (opened) widget.onUnfocus();
        },
        appBar: ChatAppBar(
          leading: Builder(
            builder: (ctx) => IconButton(
              tooltip: '聊天记录',
              icon: const AppIcon(AppIcons.menu, size: 24),
              onPressed: () {
                widget.onUnfocus();
                Scaffold.of(ctx).openDrawer();
              },
            ),
          ),
          onExitPrivate: () => widget.composerKey.currentState?.clearDraft(),
        ),
        body: SafeArea(top: false, child: ChatPane(composerKey: widget.composerKey)),
      ),
    );
  }
}

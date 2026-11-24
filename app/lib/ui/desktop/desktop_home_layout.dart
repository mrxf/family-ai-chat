import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../platform/platform_capabilities.dart';
import '../../state/chat.dart';
import '../../state/desktop_nav.dart';
import '../../state/memories.dart';
import '../../state/scheduled_tasks.dart';
import '../../state/schedules.dart';
import '../../state/session.dart';
import '../adaptive/breakpoints.dart';
import '../features/admin/admin_users_page.dart';
import '../features/admin/asr_page.dart';
import '../features/admin/models_page.dart';
import '../features/admin/usage_page.dart';
import '../features/admin/web_search_page.dart';
import '../features/chat/chat_app_bar.dart';
import '../features/chat/chat_pane.dart';
import '../features/chat/composer/composer.dart';
import '../features/conversations/conversation_panel.dart';
import '../features/conversations/search_page.dart';
import '../features/automations/scheduled_tasks_page.dart';
import '../features/memories/memories_page.dart';
import '../features/schedules/schedules_page.dart';
import '../theme/theme.dart';
import 'desktop_page_header.dart';
import 'desktop_settings_panes.dart';
import 'desktop_settings_sidebar.dart';

/// Expanded windows (desktop, landscape tablets): a permanent two-pane shell
/// whose sidebar switches smoothly between the conversation list and the
/// settings navigation rail, plus keyboard shortcuts.
///
/// Shortcuts (⌘ on macOS, Ctrl elsewhere):
/// - N = new chat
/// - K = search history
/// - , = toggle settings
/// - Esc = pop nested detail route or return to chat
class DesktopHomeLayout extends ConsumerStatefulWidget {
  const DesktopHomeLayout({super.key, required this.composerKey});

  final GlobalKey<ComposerState> composerKey;

  @override
  ConsumerState<DesktopHomeLayout> createState() => _DesktopHomeLayoutState();
}

class _DesktopHomeLayoutState extends ConsumerState<DesktopHomeLayout> {
  String _paneRouteKey = '';
  GlobalKey<NavigatorState> _paneNavKey = GlobalKey<NavigatorState>();

  Future<void> _search() async {
    final id = await openSearchPage(context);
    if (id != null && mounted) {
      ref.read(desktopNavProvider.notifier).showChat();
      ref.read(chatProvider.notifier).open(id);
    }
  }

  void _handleEscape() {
    final nav = ref.read(desktopNavProvider);
    if (nav.section == DesktopSection.chat) return;
    final nested = _paneNavKey.currentState;
    if (nested != null && nested.canPop()) {
      nested.pop();
      return;
    }
    ref.read(desktopNavProvider.notifier).showChat();
  }

  Widget _buildSecondaryRoot(DesktopNavState nav, bool isAdmin) {
    if (nav.section == DesktopSection.schedules) {
      return const SchedulesPage(embedded: true);
    }
    if (nav.section == DesktopSection.automations) {
      return const ScheduledTasksPage(embedded: true);
    }
    final tab = nav.settingsTab.isAdminOnly && !isAdmin ? SettingsTab.general : nav.settingsTab;
    return switch (tab) {
      SettingsTab.general => const DesktopGeneralSettingsPane(),
      SettingsTab.memories => const MemoriesPage(embedded: true),
      SettingsTab.schedules => const SchedulesPage(embedded: true),
      SettingsTab.automations => const ScheduledTasksPage(embedded: true),
      SettingsTab.account => const DesktopAccountSettingsPane(),
      SettingsTab.adminUsers => const AdminUsersPage(embedded: true),
      SettingsTab.adminModels => const ModelsPage(embedded: true),
      SettingsTab.adminVoice => const AsrAdminPage(embedded: true),
      SettingsTab.adminWebSearch => const WebSearchAdminPage(embedded: true),
      SettingsTab.adminUsage => const UsagePage(embedded: true),
    };
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final cmd = ref.watch(platformCapabilitiesProvider).usesCommandKey;
    final nav = ref.watch(desktopNavProvider);
    final isAdmin = ref.watch(sessionProvider.select((s) => s.user?.isAdmin == true));
    final inSettings = nav.section == DesktopSection.settings;
    final routeKey = '${nav.section}-${nav.settingsTab}-${nav.paneEpoch}';
    if (routeKey != _paneRouteKey) {
      _paneRouteKey = routeKey;
      _paneNavKey = GlobalKey<NavigatorState>();
    }

    ref.listen(desktopNavProvider.select((s) => s.section), (prev, next) {
      if (next == DesktopSection.settings && prev != DesktopSection.settings) {
        ref.invalidate(memoriesProvider);
        ref.invalidate(schedulesProvider);
        ref.invalidate(scheduledTasksProvider);
      }
    });

    SingleActivator key(LogicalKeyboardKey k) => SingleActivator(k, meta: cmd, control: !cmd);
    return CallbackShortcuts(
      bindings: {
        key(LogicalKeyboardKey.keyN): () {
          ref.read(desktopNavProvider.notifier).showChat();
          if (!ref.read(chatProvider).isPrivate) ref.read(chatProvider.notifier).newChat();
        },
        key(LogicalKeyboardKey.keyK): _search,
        key(LogicalKeyboardKey.comma): () => ref.read(desktopNavProvider.notifier).toggleSettings(),
        const SingleActivator(LogicalKeyboardKey.escape): _handleEscape,
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
                  child: SafeArea(
                    right: false,
                    child: AnimatedSwitcher(
                      duration: const Duration(milliseconds: 200),
                      switchInCurve: Curves.easeOutCubic,
                      switchOutCurve: Curves.easeInCubic,
                      transitionBuilder: (child, anim) => FadeTransition(
                        opacity: anim,
                        child: SlideTransition(
                          position: Tween<Offset>(
                            begin: const Offset(-0.03, 0),
                            end: Offset.zero,
                          ).animate(anim),
                          child: child,
                        ),
                      ),
                      child: inSettings
                          ? const DesktopSettingsSidebar(key: ValueKey('settings-sidebar'))
                          : const ConversationPanel(key: ValueKey('conv-sidebar')),
                    ),
                  ),
                ),
              ),
              VerticalDivider(width: 1, thickness: 1, color: p.line),
              Expanded(
                child: IndexedStack(
                  index: nav.section == DesktopSection.chat ? 0 : 1,
                  children: [
                    Scaffold(
                      appBar: ChatAppBar(onExitPrivate: () => widget.composerKey.currentState?.clearDraft()),
                      body: SafeArea(top: false, left: false, child: ChatPane(composerKey: widget.composerKey)),
                    ),
                    if (nav.section != DesktopSection.chat)
                      DesktopPaneScope(
                        child: SafeArea(
                          top: false,
                          left: false,
                          child: Navigator(
                            key: _paneNavKey,
                            onGenerateRoute: (_) => MaterialPageRoute<void>(
                              builder: (_) => _buildSecondaryRoot(nav, isAdmin),
                            ),
                          ),
                        ),
                      )
                    else
                      const SizedBox.shrink(),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

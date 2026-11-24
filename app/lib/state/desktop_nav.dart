import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Top-level section displayed in the desktop workspace right pane.
enum DesktopSection {
  chat,
  schedules,
  automations,
  settings,
}

/// Active category inside the desktop settings workspace.
enum SettingsTab {
  general,
  memories,
  schedules,
  automations,
  account,
  adminUsers,
  adminModels,
  adminVoice,
  adminWebSearch,
  adminUsage;

  bool get isAdminOnly => switch (this) {
        SettingsTab.adminUsers ||
        SettingsTab.adminModels ||
        SettingsTab.adminVoice ||
        SettingsTab.adminWebSearch ||
        SettingsTab.adminUsage =>
          true,
        _ => false,
      };
}

/// Navigation state for the expanded (desktop) shell.
///
/// Kept separate from mobile route navigation so narrow windows continue to use
/// standard push routes without interference.
class DesktopNavState {
  const DesktopNavState({
    this.section = DesktopSection.chat,
    this.settingsTab = SettingsTab.general,
    this.paneEpoch = 0,
  });

  final DesktopSection section;
  final SettingsTab settingsTab;

  /// Increments whenever the user re-selects a tab or switches section,
  /// allowing the nested right-pane navigator to reset to its root page.
  final int paneEpoch;

  DesktopNavState copyWith({
    DesktopSection? section,
    SettingsTab? settingsTab,
    int? paneEpoch,
  }) =>
      DesktopNavState(
        section: section ?? this.section,
        settingsTab: settingsTab ?? this.settingsTab,
        paneEpoch: paneEpoch ?? this.paneEpoch,
      );
}

class DesktopNavNotifier extends Notifier<DesktopNavState> {
  @override
  DesktopNavState build() => const DesktopNavState();

  void showChat() {
    if (state.section == DesktopSection.chat) return;
    state = state.copyWith(section: DesktopSection.chat);
  }

  void showSchedules() {
    state = state.copyWith(
      section: DesktopSection.schedules,
      paneEpoch: state.paneEpoch + 1,
    );
  }

  void showAutomations() {
    state = state.copyWith(
      section: DesktopSection.automations,
      paneEpoch: state.paneEpoch + 1,
    );
  }

  void openSettings([SettingsTab? tab]) {
    state = state.copyWith(
      section: DesktopSection.settings,
      settingsTab: tab ?? state.settingsTab,
      paneEpoch: state.paneEpoch + 1,
    );
  }

  void selectSettingsTab(SettingsTab tab) {
    state = state.copyWith(
      section: DesktopSection.settings,
      settingsTab: tab,
      paneEpoch: state.paneEpoch + 1,
    );
  }

  void toggleSettings() {
    if (state.section == DesktopSection.settings) {
      showChat();
    } else {
      openSettings();
    }
  }

  void reset() {
    state = const DesktopNavState();
  }
}

final desktopNavProvider = NotifierProvider<DesktopNavNotifier, DesktopNavState>(DesktopNavNotifier.new);

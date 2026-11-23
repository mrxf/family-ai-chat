import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../platform/platform_capabilities.dart';
import '../../state/desktop_nav.dart';
import '../../state/memories.dart';
import '../../state/schedules.dart';
import '../../state/session.dart';
import '../features/auth/self_host_info_sheet.dart';
import '../theme/icons.dart';
import '../theme/theme.dart';
import '../widgets/base_widgets.dart';

/// Left sidebar shown on expanded windows when the user enters Settings.
///
/// Replaces the conversation list with grouped settings categories and a quick
/// "返回对话" header (Linear / Claude Desktop style), keeping the layout to two
/// comfortable columns at any desktop window width.
class DesktopSettingsSidebar extends ConsumerWidget {
  const DesktopSettingsSidebar({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final nav = ref.watch(desktopNavProvider);
    final navCtrl = ref.read(desktopNavProvider.notifier);
    final session = ref.watch(sessionProvider);
    final user = session.user;
    final isAdmin = user?.isAdmin == true;
    final memCount = ref.watch(memoriesProvider).value?.total ?? 0;
    final pendingSchedules = ref.watch(schedulesProvider).value?.pendingTotal ?? 0;
    final cmdLabel = ref.watch(platformCapabilitiesProvider).usesCommandKey ? '⌘,' : 'Ctrl+,';

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
          child: Pressable(
            scale: 0.98,
            onTap: navCtrl.showChat,
            child: Container(
              height: 44,
              padding: const EdgeInsets.symmetric(horizontal: 12),
              decoration: BoxDecoration(
                color: p.surface,
                borderRadius: BorderRadius.circular(13),
                border: Border.all(color: p.line),
              ),
              child: Row(
                children: [
                  AppIcon(AppIcons.back, size: 17, color: p.text),
                  const SizedBox(width: 8),
                  Text('返回对话', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: p.text)),
                  const Spacer(),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                    decoration: BoxDecoration(
                      color: p.surfaceAlt,
                      borderRadius: BorderRadius.circular(6),
                      border: Border.all(color: p.line),
                    ),
                    child: Text('Esc', style: TextStyle(fontSize: 11, color: p.muted, fontWeight: FontWeight.w600)),
                  ),
                ],
              ),
            ),
          ),
        ),
        Expanded(
          child: ListView(
            padding: const EdgeInsets.fromLTRB(10, 6, 10, 16),
            children: [
              const _GroupLabel('个人'),
              _NavItem(
                icon: AppIcons.tune,
                label: '通用与外观',
                selected: nav.settingsTab == SettingsTab.general,
                onTap: () => navCtrl.selectSettingsTab(SettingsTab.general),
              ),
              _NavItem(
                icon: AppIcons.heart,
                label: '小暖记住的事',
                badge: memCount > 0 ? '$memCount' : null,
                selected: nav.settingsTab == SettingsTab.memories,
                onTap: () => navCtrl.selectSettingsTab(SettingsTab.memories),
              ),
              _NavItem(
                icon: AppIcons.calendar,
                label: '我的日程',
                badge: pendingSchedules > 0 ? '$pendingSchedules' : null,
                accentBadge: pendingSchedules > 0,
                selected: nav.settingsTab == SettingsTab.schedules,
                onTap: () => navCtrl.selectSettingsTab(SettingsTab.schedules),
              ),
              _NavItem(
                icon: AppIcons.shield,
                label: '账号与安全',
                selected: nav.settingsTab == SettingsTab.account,
                onTap: () => navCtrl.selectSettingsTab(SettingsTab.account),
              ),
              if (isAdmin) ...[
                const _GroupLabel('家庭管理'),
                _NavItem(
                  icon: AppIcons.family,
                  label: '家庭成员',
                  selected: nav.settingsTab == SettingsTab.adminUsers,
                  onTap: () => navCtrl.selectSettingsTab(SettingsTab.adminUsers),
                ),
                _NavItem(
                  icon: AppIcons.hub,
                  label: '模型与供应商',
                  selected: nav.settingsTab == SettingsTab.adminModels,
                  onTap: () => navCtrl.selectSettingsTab(SettingsTab.adminModels),
                ),
                _NavItem(
                  icon: AppIcons.mic,
                  label: '语音输入',
                  selected: nav.settingsTab == SettingsTab.adminVoice,
                  onTap: () => navCtrl.selectSettingsTab(SettingsTab.adminVoice),
                ),
                _NavItem(
                  icon: AppIcons.globe,
                  label: '联网搜索',
                  selected: nav.settingsTab == SettingsTab.adminWebSearch,
                  onTap: () => navCtrl.selectSettingsTab(SettingsTab.adminWebSearch),
                ),
                _NavItem(
                  icon: AppIcons.chart,
                  label: '用量与花费',
                  selected: nav.settingsTab == SettingsTab.adminUsage,
                  onTap: () => navCtrl.selectSettingsTab(SettingsTab.adminUsage),
                ),
              ],
            ],
          ),
        ),
        Divider(color: p.line, height: 1),
        if (user != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 10, 14, 10),
            child: Row(
              children: [
                Avatar(label: user.displayName, emoji: user.avatar, size: 34),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        user.displayName,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600),
                      ),
                      Text(
                        session.isSelfHosted ? '自托管 · $cmdLabel 切回' : '设置 · $cmdLabel 切回',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 12, color: p.muted),
                      ),
                    ],
                  ),
                ),
                Tooltip(
                  message: '服务器信息',
                  child: Pressable(
                    scale: 0.92,
                    onTap: () => showSelfHostInfoSheet(context, ref),
                    child: Padding(
                      padding: const EdgeInsets.all(6),
                      child: AppIcon(AppIcons.server, size: 18, color: p.muted),
                    ),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class _GroupLabel extends StatelessWidget {
  const _GroupLabel(this.text);
  final String text;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 14, 12, 6),
      child: Text(
        text,
        style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: p.faint, letterSpacing: 0.3),
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  const _NavItem({
    required this.icon,
    required this.label,
    required this.selected,
    required this.onTap,
    this.badge,
    this.accentBadge = false,
  });

  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;
  final String? badge;
  final bool accentBadge;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Pressable(
      scale: 0.985,
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 160),
        margin: const EdgeInsets.symmetric(vertical: 1.5),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: selected ? p.surface : Colors.transparent,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: selected ? p.line : Colors.transparent),
        ),
        child: Row(
          children: [
            AppIcon(icon, size: 19, color: selected ? p.accent : p.muted),
            const SizedBox(width: 11),
            Expanded(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: 15,
                  fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                  color: selected ? p.text : p.text.withValues(alpha: 0.85),
                ),
              ),
            ),
            if (badge != null)
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                decoration: BoxDecoration(
                  color: accentBadge ? p.accentSoft : p.surfaceAlt,
                  borderRadius: BorderRadius.circular(999),
                  border: accentBadge ? null : Border.all(color: p.line),
                ),
                child: Text(
                  badge!,
                  style: TextStyle(
                    fontSize: 11.5,
                    fontWeight: FontWeight.w700,
                    color: accentBadge ? p.accent : p.muted,
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

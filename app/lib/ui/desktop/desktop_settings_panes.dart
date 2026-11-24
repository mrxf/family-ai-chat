import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../core/models/app_settings_steps.dart';
import '../../platform/platform_capabilities.dart';
import '../../state/desktop_nav.dart';
import '../../state/session.dart';
import '../features/auth/invite_generator_sheet.dart';
import '../features/auth/self_host_info_sheet.dart';
import '../features/settings/settings_dialogs.dart';
import '../features/settings/update.dart';
import '../theme/icons.dart';
import '../theme/theme.dart';
import '../widgets/base_widgets.dart';
import '../widgets/sheets.dart';
import 'desktop_page_header.dart';

/// Desktop "通用与外观" settings pane.
class DesktopGeneralSettingsPane extends ConsumerWidget {
  const DesktopGeneralSettingsPane({super.key});

  static const _fontSteps = fontSteps;

  static int _fontIndex(double value) => nearestFontStep(value);

  static String _rateLabel(double r) => switch (r) {
        <= 0.35 => '很慢',
        <= 0.45 => '较慢',
        <= 0.55 => '正常',
        <= 0.65 => '较快',
        _ => '很快',
      };

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final session = ref.watch(sessionProvider);
    final user = session.user;
    if (user == null) return const SizedBox.shrink();
    final s = ref.watch(settingsProvider);
    final settings = ref.read(settingsProvider.notifier);
    final fontIdx = _fontIndex(s.fontScale);

    return Scaffold(
      body: Column(
        children: [
          const DesktopPageHeader(
            title: '通用与外观',
            subtitle: '调整个人称呼、界面主题、字号与语音朗读偏好',
          ),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.fromLTRB(32, 24, 32, 48),
              children: [
                DesktopPageBody(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      // Profile card
                      _DesktopCard(
                        child: Padding(
                          padding: const EdgeInsets.all(20),
                          child: Row(
                            children: [
                              Avatar(label: user.displayName, emoji: user.avatar, size: 58),
                              const SizedBox(width: 16),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Row(
                                      children: [
                                        Flexible(
                                          child: Text(
                                            user.displayName,
                                            maxLines: 1,
                                            overflow: TextOverflow.ellipsis,
                                            style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
                                          ),
                                        ),
                                        if (user.isAdmin) ...[
                                          const SizedBox(width: 8),
                                          Container(
                                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                                            decoration: BoxDecoration(
                                              color: p.accentSoft,
                                              borderRadius: BorderRadius.circular(8),
                                            ),
                                            child: Text('管理员', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: p.accent)),
                                          ),
                                        ],
                                      ],
                                    ),
                                    const SizedBox(height: 4),
                                    Text('用户名：${user.username}', style: TextStyle(fontSize: 14, color: p.muted)),
                                  ],
                                ),
                              ),
                              OutlinedButton.icon(
                                onPressed: () => showEditProfileDialog(context, ref),
                                icon: const AppIcon(AppIcons.edit, size: 16),
                                label: const Text('编辑资料'),
                              ),
                            ],
                          ),
                        ),
                      ),

                      const _SectionTitle('外观与阅读'),
                      _DesktopCard(
                        child: Column(
                          children: [
                            _SettingRow(
                              title: '主题模式',
                              subtitle: '切换浅色、深色或自动跟随系统',
                              trailing: SizedBox(
                                width: 280,
                                child: _CompactSegmented<ThemeMode>(
                                  value: s.themeMode,
                                  options: const {
                                    ThemeMode.system: '跟随系统',
                                    ThemeMode.light: '浅色',
                                    ThemeMode.dark: '深色',
                                  },
                                  onChanged: (v) => settings.update(s.copyWith(themeMode: v)),
                                ),
                              ),
                            ),
                            Divider(height: 1, indent: 20, endIndent: 20, color: p.line),
                            Padding(
                              padding: const EdgeInsets.fromLTRB(20, 16, 20, 18),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  Row(
                                    children: [
                                      Expanded(
                                        child: Column(
                                          crossAxisAlignment: CrossAxisAlignment.start,
                                          children: [
                                            const Text('字体大小', style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600)),
                                            const SizedBox(height: 2),
                                            Text('当前：${_fontSteps[fontIdx].$2}', style: TextStyle(fontSize: 13.5, color: p.muted)),
                                          ],
                                        ),
                                      ),
                                      SizedBox(
                                        width: 280,
                                        child: Row(
                                          children: [
                                            Text('A', textScaler: TextScaler.noScaling, style: TextStyle(fontSize: 13, color: p.muted, fontWeight: FontWeight.w600)),
                                            Expanded(
                                              child: Slider(
                                                value: fontIdx.toDouble(),
                                                min: 0,
                                                max: (_fontSteps.length - 1).toDouble(),
                                                divisions: _fontSteps.length - 1,
                                                label: _fontSteps[fontIdx].$2,
                                                onChanged: (v) {
                                                  final n = v.round();
                                                  if (n != fontIdx) {
                                                    HapticFeedback.selectionClick();
                                                    settings.update(s.copyWith(fontScale: _fontSteps[n].$1));
                                                  }
                                                },
                                              ),
                                            ),
                                            Text('A', textScaler: TextScaler.noScaling, style: TextStyle(fontSize: 21, color: p.muted, fontWeight: FontWeight.w600)),
                                          ],
                                        ),
                                      ),
                                    ],
                                  ),
                                  const SizedBox(height: 12),
                                  Container(
                                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                                    decoration: BoxDecoration(
                                      color: p.surfaceAlt,
                                      borderRadius: BorderRadius.circular(12),
                                    ),
                                    child: Text(
                                      '小暖会用这个大小显示文字。拖动上面的滑块，找一个看着最舒服的大小。',
                                      style: Theme.of(context).textTheme.bodyLarge,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                      ),

                      const _SectionTitle('语音朗读'),
                      _DesktopCard(
                        child: _SettingRow(
                          title: '朗读语速',
                          subtitle: '控制消息语音朗读的快慢',
                          trailing: SizedBox(
                            width: 280,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Slider(
                                    value: s.ttsRate,
                                    min: 0.3,
                                    max: 0.7,
                                    divisions: 4,
                                    label: _rateLabel(s.ttsRate),
                                    onChanged: (v) => settings.update(s.copyWith(ttsRate: v)),
                                  ),
                                ),
                                SizedBox(
                                  width: 40,
                                  child: Text(
                                    _rateLabel(s.ttsRate),
                                    textAlign: TextAlign.right,
                                    style: TextStyle(fontSize: 13.5, color: p.muted, fontWeight: FontWeight.w500),
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Desktop "账号与安全" settings pane.
class DesktopAccountSettingsPane extends ConsumerWidget {
  const DesktopAccountSettingsPane({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final session = ref.watch(sessionProvider);
    final user = session.user;
    if (user == null) return const SizedBox.shrink();
    final caps = ref.watch(platformCapabilitiesProvider);

    return Scaffold(
      body: Column(
        children: [
          const DesktopPageHeader(
            title: '账号与安全',
            subtitle: '管理登录密码、家人免输口令、服务器连接与版本信息',
          ),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.fromLTRB(32, 24, 32, 48),
              children: [
                DesktopPageBody(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      const _SectionTitle('账号安全', topPadding: 0),
                      _DesktopCard(
                        child: Column(
                          children: [
                            _SettingRow(
                              icon: AppIcons.key,
                              title: '修改密码',
                              subtitle: '更新当前账号的登录密码',
                              trailing: OutlinedButton(
                                onPressed: () => showChangePasswordDialog(context, ref),
                                child: const Text('修改'),
                              ),
                            ),
                            Divider(height: 1, indent: 20, endIndent: 20, color: p.line),
                            _SettingRow(
                              icon: AppIcons.magicWand,
                              title: '生成免输登录口令',
                              subtitle: '生成 #xn...xn# 加密口令，方便异地家人或另一台设备直接登录',
                              trailing: OutlinedButton(
                                onPressed: () => showInviteGeneratorSheet(
                                  context,
                                  ref,
                                  initialUsername: user.username,
                                  initialDisplayName: user.displayName,
                                ),
                                child: const Text('生成口令'),
                              ),
                            ),
                          ],
                        ),
                      ),

                      const _SectionTitle('服务器与关于'),
                      _DesktopCard(
                        child: Column(
                          children: [
                            _SettingRow(
                              icon: AppIcons.server,
                              title: '服务器',
                              subtitle: session.isSelfHosted ? '自托管 · ${session.serverHost}' : '官方默认 · ${session.serverHost}',
                              trailing: OutlinedButton(
                                onPressed: () => showSelfHostInfoSheet(context, ref),
                                child: const Text('查看详情'),
                              ),
                            ),
                            Divider(height: 1, indent: 20, endIndent: 20, color: p.line),
                            FutureBuilder<PackageInfo>(
                              future: PackageInfo.fromPlatform(),
                              builder: (_, snap) => _SettingRow(
                                icon: AppIcons.info,
                                title: '应用版本',
                                subtitle: '小暖桌面端',
                                trailing: Text(
                                  snap.data?.version ?? '—',
                                  style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w500),
                                ),
                              ),
                            ),
                            if (caps.selfUpdateViaApk) ...[
                              Divider(height: 1, indent: 20, endIndent: 20, color: p.line),
                              _SettingRow(
                                icon: AppIcons.update,
                                title: '检查更新',
                                subtitle: '查看是否有新版本安装包',
                                trailing: OutlinedButton(
                                  onPressed: () => checkForUpdate(context, ref),
                                  child: const Text('检查'),
                                ),
                              ),
                            ],
                          ],
                        ),
                      ),

                      const _SectionTitle('登录会话'),
                      _DesktopCard(
                        child: _SettingRow(
                          title: '退出登录',
                          subtitle: '退出后需要重新输入密码，聊天记录保存在服务器上不会丢失',
                          trailing: OutlinedButton(
                            style: OutlinedButton.styleFrom(
                              foregroundColor: p.bad,
                              side: BorderSide(color: p.bad.withValues(alpha: 0.45)),
                            ),
                            onPressed: () async {
                              final ok = await confirm(
                                context,
                                title: '退出登录？',
                                body: '退出后需要重新输入密码。聊天记录保存在服务器上，不会丢失。',
                                ok: '退出',
                                danger: true,
                              );
                              if (!ok) return;
                              ref.read(desktopNavProvider.notifier).reset();
                              await ref.read(sessionProvider.notifier).logout();
                            },
                            child: const Text('退出登录'),
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text, {this.topPadding = 24});
  final String text;
  final double topPadding;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: EdgeInsets.fromLTRB(4, topPadding, 4, 10),
      child: Text(text, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.muted)),
    );
  }
}

class _DesktopCard extends StatelessWidget {
  const _DesktopCard({required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: p.line),
      ),
      child: child,
    );
  }
}

class _SettingRow extends StatelessWidget {
  const _SettingRow({
    this.icon,
    required this.title,
    this.subtitle,
    required this.trailing,
  });

  final IconData? icon;
  final String title;
  final String? subtitle;
  final Widget trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 15),
      child: Row(
        children: [
          if (icon != null) ...[
            AppIcon(icon!, size: 19, color: p.accent),
            const SizedBox(width: 14),
          ],
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600)),
                if (subtitle != null && subtitle!.isNotEmpty) ...[
                  const SizedBox(height: 3),
                  Text(subtitle!, style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.4)),
                ],
              ],
            ),
          ),
          const SizedBox(width: 16),
          trailing,
        ],
      ),
    );
  }
}

class _CompactSegmented<T> extends StatelessWidget {
  const _CompactSegmented({required this.value, required this.options, required this.onChanged});
  final T value;
  final Map<T, String> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(12)),
      child: Row(
        children: [
          for (final e in options.entries)
            Expanded(
              child: Pressable(
                onTap: () => onChanged(e.key),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 180),
                  height: 34,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: e.key == value ? p.surface : Colors.transparent,
                    borderRadius: BorderRadius.circular(9),
                    boxShadow: e.key == value ? [BoxShadow(color: p.shadow, blurRadius: 6, offset: const Offset(0, 1))] : null,
                  ),
                  child: Text(
                    e.value,
                    style: TextStyle(
                      fontSize: 13.5,
                      fontWeight: e.key == value ? FontWeight.w700 : FontWeight.w500,
                      color: e.key == value ? p.accent : p.muted,
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../../core/models/app_settings_steps.dart';
import '../../../platform/platform_capabilities.dart';
import '../../../state/memories.dart';
import '../../../state/scheduled_tasks.dart';
import '../../../state/schedules.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../../widgets/sheets.dart';
import '../admin/admin_users_page.dart';
import '../admin/asr_page.dart';
import '../admin/models_page.dart';
import '../admin/usage_page.dart';
import '../admin/web_search_page.dart';
import '../auth/invite_generator_sheet.dart';
import '../auth/self_host_info_sheet.dart';
import '../automations/scheduled_tasks_page.dart';
import '../memories/memories_page.dart';
import '../schedules/schedules_page.dart';
import 'settings_dialogs.dart';
import 'update.dart';

export 'settings_dialogs.dart' show avatarChoices;

class SettingsPage extends ConsumerStatefulWidget {
  const SettingsPage({super.key});

  @override
  ConsumerState<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends ConsumerState<SettingsPage> {
  @override
  void initState() {
    super.initState();
    ref.invalidate(memoriesProvider);
    ref.invalidate(schedulesProvider);
    ref.invalidate(scheduledTasksProvider);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final session = ref.watch(sessionProvider);
    final user = session.user;
    if (user == null) return const Scaffold();
    final s = ref.watch(settingsProvider);
    final settings = ref.read(settingsProvider.notifier);
    final memories = ref.watch(memoriesProvider);
    final memorySubtitle = memories.when(
      skipLoadingOnRefresh: false,
      data: (m) => !m.enabled ? '已关闭' : '已记住 ${m.total} 件事${m.newCount > 0 ? ' · ${m.newCount} 条新的' : ''}',
      loading: () => '让小暖记住你的情况，换哪个模型都认识你',
      error: (_, _) => '让小暖记住你的情况，换哪个模型都认识你',
    );
    final schedules = ref.watch(schedulesProvider);
    final scheduleSubtitle = schedules.when(
      skipLoadingOnRefresh: false,
      data: (sc) => sc.todayPending > 0
          ? '今天有 ${sc.todayPending} 项待办 · 共 ${sc.pendingTotal} 项日程'
          : (sc.pendingTotal > 0 ? '共 ${sc.pendingTotal} 项日程提醒' : '跟小暖说句话就能在手机上定准点提醒'),
      loading: () => '跟小暖说句话就能在手机上定准点提醒',
      error: (_, _) => '跟小暖说句话就能在手机上定准点提醒',
    );

    final scheduledTasks = ref.watch(scheduledTasksProvider);
    final automationSubtitle = scheduledTasks.when(
      skipLoadingOnRefresh: false,
      data: (t) => t.activeCount > 0
          ? '已启用 ${t.activeCount} 个 · 共 ${t.totalCount} 个自动任务'
          : (t.totalCount > 0 ? '共 ${t.totalCount} 个任务（已暂停）' : '让小暖按时查新闻、分析任务并更新首页卡片'),
      loading: () => '让小暖按时查新闻、分析任务并更新首页卡片',
      error: (_, _) => '让小暖按时查新闻、分析任务并更新首页卡片',
    );

    return Scaffold(
      appBar: AppBar(title: const Text('设置')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 40),
        children: [
          // ---- profile ----
          Pressable(
            scale: 0.98,
            onTap: () => _editProfile(context, ref),
            child: Container(
              padding: const EdgeInsets.all(18),
              decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(22), border: Border.all(color: p.line)),
              child: Row(children: [
                Avatar(label: user.displayName, emoji: user.avatar, size: 60),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(user.displayName, style: const TextStyle(fontSize: 21, fontWeight: FontWeight.w700)),
                    const SizedBox(height: 4),
                    Text('用户名：${user.username}${user.isAdmin ? ' · 管理员' : ''}', style: TextStyle(fontSize: 14, color: p.muted)),
                  ]),
                ),
                AppIcon(AppIcons.chevronRight, color: p.faint, size: 19),
              ]),
            ),
          ),

          Padding(
            padding: const EdgeInsets.only(top: 16),
            child: Material(
              clipBehavior: Clip.antiAlias,
              color: p.surface,
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20), side: BorderSide(color: p.line)),
              child: Column(
                children: [
                  _Tile(
                    icon: AppIcons.heart,
                    title: '小暖记住的事',
                    subtitle: memorySubtitle,
                    onTap: () => Navigator.of(context).push(appRoute((_) => const MemoriesPage())),
                  ),
                  Divider(height: 1, indent: 56, color: p.line),
                  _Tile(
                    icon: AppIcons.calendar,
                    title: '我的日程',
                    subtitle: scheduleSubtitle,
                    onTap: () => Navigator.of(context).push(appRoute((_) => const SchedulesPage())),
                  ),
                  Divider(height: 1, indent: 56, color: p.line),
                  _Tile(
                    icon: AppIcons.repeat,
                    title: 'AI 定时任务',
                    subtitle: automationSubtitle,
                    onTap: () => Navigator.of(context).push(appRoute((_) => const ScheduledTasksPage())),
                  ),
                ],
              ),
            ),
          ),

          _Section('字体大小', children: [
            _FontScaleSlider(value: s.fontScale, onChanged: (v) => settings.update(s.copyWith(fontScale: v))),
          ]),

          _Section('外观', children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: _Segmented<ThemeMode>(
                value: s.themeMode,
                options: const {ThemeMode.system: '跟随系统', ThemeMode.light: '浅色', ThemeMode.dark: '深色'},
                onChanged: (v) => settings.update(s.copyWith(themeMode: v)),
              ),
            ),
          ]),

          _Section('朗读', children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 12, 18, 4),
              child: Row(children: [
                const Text('语速', style: TextStyle(fontSize: 16)),
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
                SizedBox(width: 36, child: Text(_rateLabel(s.ttsRate), style: TextStyle(color: p.muted))),
              ]),
            ),
          ]),

          _Section('账号', children: [
            _Tile(icon: AppIcons.key, title: '修改密码', onTap: () => _changePassword(context, ref)),
            _Tile(
              icon: AppIcons.magicWand,
              title: '生成免输登录口令',
              subtitle: '生成 #xn...xn# 加密口令，方便异地家人或另一台手机直接登录',
              onTap: () => showInviteGeneratorSheet(
                context,
                ref,
                initialUsername: user.username,
                initialDisplayName: user.displayName,
              ),
            ),
          ]),

          if (user.isAdmin)
            _Section('家庭管理', children: [
              _Tile(
                icon: AppIcons.family,
                title: '家庭成员',
                subtitle: '给家人创建账号、重置密码',
                onTap: () => Navigator.of(context).push(appRoute((_) => const AdminUsersPage())),
              ),
              _Tile(
                icon: AppIcons.hub,
                title: '模型与供应商',
                subtitle: session.boot?.defaultModel == null ? '还没有可用的模型，点这里添加' : '默认：${session.boot!.defaultModel!.displayName} · 共 ${session.boot!.models.length} 个模型${session.boot!.imageGeneration ? (session.boot!.imagesLeft >= 0 ? ' · 今天还能画 ${session.boot!.imagesLeft} 张' : ' · 能画图') : ''}',
                onTap: () => Navigator.of(context).push(appRoute((_) => const ModelsPage())),
              ),
              _Tile(
                icon: AppIcons.mic,
                title: '语音输入',
                subtitle: session.boot?.voiceInput == true
                    ? (session.boot?.voiceMode == 'stream' ? '已开启 · 实时边说边出字' : '已开启 · 录音转文字')
                    : '未开启 · 配置密钥后聊天框才会出现麦克风',
                onTap: () => Navigator.of(context).push(appRoute((_) => const AsrAdminPage())),
              ),
              _Tile(
                icon: AppIcons.globe,
                title: '联网搜索',
                subtitle: session.boot?.webSearch == true
                    ? '已开启 · 需要实时新闻、行情或政策时自动上网查证'
                    : '未开启 · 配置密钥后全家人都能查实时信息',
                onTap: () => Navigator.of(context).push(appRoute((_) => const WebSearchAdminPage())),
              ),
              _Tile(
                icon: AppIcons.chart,
                title: '用量与花费',
                subtitle: '每个人、每个模型用了多少钱',
                onTap: () => Navigator.of(context).push(appRoute((_) => const UsagePage())),
              ),
            ]),

          _Section('关于', children: [
            if (ref.watch(platformCapabilitiesProvider).selfUpdateViaApk)
              _Tile(icon: AppIcons.update, title: '检查更新', onTap: () => checkForUpdate(context, ref)),
            FutureBuilder<PackageInfo>(
              future: PackageInfo.fromPlatform(),
              builder: (_, snap) => _Tile(icon: AppIcons.info, title: '版本', trailing: Text(snap.data?.version ?? '', style: TextStyle(color: p.muted))),
            ),
            _Tile(
              icon: AppIcons.server,
              title: '服务器',
              subtitle: session.isSelfHosted ? '自托管 · ${session.serverHost}' : '官方默认 · ${session.serverHost}',
              onTap: () => showSelfHostInfoSheet(context, ref),
            ),
          ]),

          const SizedBox(height: 24),
          Pressable(
            scale: 0.98,
            onTap: () async {
              if (await confirm(context, title: '退出登录？', body: '退出后需要重新输入密码。聊天记录保存在服务器上，不会丢失。', ok: '退出', danger: true)) {
                if (context.mounted) Navigator.of(context).popUntil((r) => r.isFirst);
                await ref.read(sessionProvider.notifier).logout();
              }
            },
            child: Container(
              height: 54,
              alignment: Alignment.center,
              decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.line)),
              child: Text('退出登录', style: TextStyle(color: p.bad, fontSize: 17, fontWeight: FontWeight.w600)),
            ),
          ),
        ],
      ),
    );
  }

  static String _rateLabel(double r) => switch (r) {
        <= 0.35 => '很慢',
        <= 0.45 => '较慢',
        <= 0.55 => '正常',
        <= 0.65 => '较快',
        _ => '很快',
      };

  Future<void> _editProfile(BuildContext context, WidgetRef ref) => showEditProfileDialog(context, ref);

  Future<void> _changePassword(BuildContext context, WidgetRef ref) => showChangePasswordDialog(context, ref);
}

/// Five font sizes on a stepped slider, with a live preview.
class _FontScaleSlider extends StatelessWidget {
  const _FontScaleSlider({required this.value, required this.onChanged});
  final double value;
  final ValueChanged<double> onChanged;

  static const _steps = fontSteps;

  int get _index => nearestFontStep(value);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final i = _index;
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 14, 12, 16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(crossAxisAlignment: CrossAxisAlignment.center, children: [
          Padding(padding: const EdgeInsets.only(left: 8), child: Text('A', textScaler: TextScaler.noScaling, style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w600))),
          Expanded(
            child: Slider(
              value: i.toDouble(),
              min: 0,
              max: (_steps.length - 1).toDouble(),
              divisions: _steps.length - 1,
              label: _steps[i].$2,
              onChanged: (v) {
                final n = v.round();
                if (n != i) {
                  HapticFeedback.selectionClick();
                  onChanged(_steps[n].$1);
                }
              },
            ),
          ),
          Padding(padding: const EdgeInsets.only(right: 8), child: Text('A', textScaler: TextScaler.noScaling, style: TextStyle(fontSize: 24, color: p.muted, fontWeight: FontWeight.w600))),
        ]),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24),
          child: Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
            for (final (j, (_, label)) in _steps.indexed)
              SizedBox(
                width: 40,
                child: Text(label,
                    textAlign: TextAlign.center,
                    textScaler: TextScaler.noScaling,
                    style: TextStyle(fontSize: 13, color: j == i ? p.accent : p.faint, fontWeight: j == i ? FontWeight.w700 : FontWeight.w500)),
              ),
          ]),
        ),
        const SizedBox(height: 14),
        Container(
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
          child: Text('小暖会用这个大小显示文字。拖动上面的滑块，找一个看着最舒服的大小。', style: Theme.of(context).textTheme.bodyLarge),
        ),
      ]),
    );
  }
}

class _Section extends StatelessWidget {
  const _Section(this.title, {required this.children});
  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(8, 24, 8, 8),
        child: Text(title, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: p.muted)),
      ),
      Container(
        clipBehavior: Clip.antiAlias,
        decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(20), border: Border.all(color: p.line)),
        child: Column(children: children),
      ),
    ]);
  }
}

class _Tile extends StatelessWidget {
  const _Tile({required this.icon, required this.title, this.subtitle, this.onTap, this.trailing});
  final IconData icon;
  final String title;
  final String? subtitle;
  final VoidCallback? onTap;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 2),
      leading: AppIcon(icon, color: p.accent),
      title: Text(title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w500)),
      subtitle: subtitle == null || subtitle!.isEmpty
          ? null
          : Padding(padding: const EdgeInsets.only(top: 4), child: Text(subtitle!, style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.45))),
      trailing: trailing ?? (onTap != null ? AppIcon(AppIcons.chevronRight, color: p.faint, size: 18) : null),
      onTap: onTap,
    );
  }
}

class _Segmented<T> extends StatelessWidget {
  const _Segmented({required this.value, required this.options, required this.onChanged});
  final T value;
  final Map<T, String> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(16)),
      child: Row(children: [
        for (final e in options.entries)
          Expanded(
            child: Pressable(
              onTap: () => onChanged(e.key),
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 200),
                height: 44,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: e.key == value ? p.surface : Colors.transparent,
                  borderRadius: BorderRadius.circular(12),
                  boxShadow: e.key == value ? [BoxShadow(color: p.shadow, blurRadius: 8, offset: const Offset(0, 2))] : null,
                ),
                child: Text(e.value,
                    style: TextStyle(fontSize: 16, fontWeight: e.key == value ? FontWeight.w700 : FontWeight.w500, color: e.key == value ? p.accent : p.muted)),
              ),
            ),
          ),
      ]),
    );
  }
}

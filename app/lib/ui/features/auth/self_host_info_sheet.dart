import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../admin/admin_common.dart';
import '../admin/admin_users_page.dart';
import 'invite_generator_sheet.dart';

/// Sheet presented from Home (`Welcome`) or `SettingsPage` showcasing self-hosted status & family invite tools.
Future<void> showSelfHostInfoSheet(BuildContext context, WidgetRef ref) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) {
      final p = ctx.pal;
      final session = ref.watch(sessionProvider);
      final isSelfHost = session.isSelfHosted;
      final isAdmin = session.user?.isAdmin == true;

      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(children: [
                Container(
                  width: 46,
                  height: 46,
                  decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(15)),
                  child: AppIcon(isSelfHost ? AppIcons.server : AppIcons.sparkles, color: p.accent, size: 24),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Row(children: [
                      Flexible(
                        child: Text(
                          isSelfHost ? '自托管服务器已连接' : '官方默认服务器',
                          style: const TextStyle(fontSize: 19, fontWeight: FontWeight.w700),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Tag(isSelfHost ? '私有部署' : '默认', color: p.accent),
                    ]),
                    const SizedBox(height: 3),
                    Text(session.serverHost, style: TextStyle(fontSize: 14, color: p.muted, fontFamily: 'monospace')),
                  ]),
                ),
              ]),
              const SizedBox(height: 18),
              Container(
                padding: const EdgeInsets.all(16),
                decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(18)),
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  _CapItem(
                    icon: AppIcons.shield,
                    title: isSelfHost ? '数据完全自主保存' : '免配置即开即用',
                    body: isSelfHost
                        ? '当前所有对话记录、照片与小暖记忆均独立保存在这台自托管服务器上。'
                        : '当前连接小暖官方默认服务，无需配置地址即可使用；如需切换自托管可先退出当前账号。',
                  ),
                  const SizedBox(height: 12),
                  const _CapItem(
                    icon: AppIcons.magicWand,
                    title: '异地父母免输口令（#xn...xn#）',
                    body: '为家人生成加密登录口令发到微信，父母复制后打开小暖就会自动弹出登录提示，无需手敲网址与密码。',
                  ),
                ]),
              ),
              const SizedBox(height: 18),
              PrimaryButton(
                label: '生成家人免输登录口令',
                icon: AppIcons.copy,
                onTap: () {
                  Navigator.pop(ctx);
                  showInviteGeneratorSheet(context, ref, initialServer: session.server);
                },
              ),
              if (isAdmin) ...[
                const SizedBox(height: 10),
                SecondaryButton(
                  label: '前往家庭成员管理（建号发口令）',
                  icon: AppIcons.family,
                  onTap: () {
                    Navigator.pop(ctx);
                    Navigator.of(context).push(appRoute((_) => const AdminUsersPage()));
                  },
                ),
              ],
            ],
          ),
        ),
      );
    },
  );
}

class _CapItem extends StatelessWidget {
  const _CapItem({required this.icon, required this.title, required this.body});
  final IconData icon;
  final String title, body;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Padding(
        padding: const EdgeInsets.only(top: 2),
        child: AppIcon(icon, size: 20, color: p.accent),
      ),
      const SizedBox(width: 10),
      Expanded(
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(title, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: p.text)),
          const SizedBox(height: 2),
          Text(body, style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.45)),
        ]),
      ),
    ]);
  }
}

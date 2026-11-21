import 'package:flutter/material.dart';

import '../../theme/icons.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';
import '../admin/admin_common.dart' show Tag;

/// Reachability of the configured server, as probed by the login page.
enum ServerReach { unknown, checking, ok, fail }

/// Displayed on the login screen whenever the user has configured a self-hosted server,
/// surfacing live reachability, server configuration, and one-tap family invite generation.
class SelfHostStatusCard extends StatelessWidget {
  const SelfHostStatusCard({
    super.key,
    required this.host,
    required this.reach,
    required this.onConfigure,
    required this.onInvite,
    required this.onResetDefault,
  });

  final String host;
  final ServerReach reach;
  final VoidCallback onConfigure;
  final VoidCallback onInvite;
  final VoidCallback onResetDefault;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final (statusLabel, statusColor) = switch (reach) {
      ServerReach.ok => ('已连通', p.good),
      ServerReach.fail => ('连不上', p.bad),
      ServerReach.checking => ('检测中', p.muted),
      ServerReach.unknown => ('待检测', p.muted),
    };

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: reach == ServerReach.fail ? p.bad.withValues(alpha: 0.45) : p.line),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 13, 12, 12),
            child: Row(children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(12)),
                child: AppIcon(AppIcons.server, color: p.accent, size: 20),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Row(children: [
                    const Flexible(
                      child: Text('自托管服务器', maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w700)),
                    ),
                    const SizedBox(width: 7),
                    Tag(
                      statusLabel,
                      color: statusColor,
                      icon: reach == ServerReach.ok ? AppIcons.checkBold : (reach == ServerReach.fail ? AppIcons.error : null),
                    ),
                  ]),
                  const SizedBox(height: 3),
                  Text(
                    host,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13.5, color: p.muted, fontFamily: 'monospace'),
                  ),
                ]),
              ),
              const SizedBox(width: 8),
              Pressable(
                scale: 0.95,
                onTap: onConfigure,
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 7),
                  decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(11)),
                  child: Text('配置', style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.accent)),
                ),
              ),
            ]),
          ),
          Divider(height: 1, color: p.line),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            child: Row(children: [
              Expanded(
                child: Pressable(
                  scale: 0.97,
                  onTap: onInvite,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 6),
                    child: Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                      AppIcon(AppIcons.magicWand, size: 16, color: p.accent),
                      const SizedBox(width: 5),
                      Flexible(
                        child: Text(
                          '生成家人口令',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.text),
                        ),
                      ),
                    ]),
                  ),
                ),
              ),
              Container(width: 1, height: 18, color: p.line),
              Expanded(
                child: Pressable(
                  scale: 0.97,
                  onTap: onResetDefault,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 6),
                    child: Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                      AppIcon(AppIcons.repeat, size: 15, color: p.muted),
                      const SizedBox(width: 5),
                      Flexible(
                        child: Text(
                          '切回默认服务',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: p.muted),
                        ),
                      ),
                    ]),
                  ),
                ),
              ),
            ]),
          ),
        ],
      ),
    );
  }
}

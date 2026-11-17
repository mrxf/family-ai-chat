import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../state/session.dart';
import 'widgets.dart';

/// Checks the server for a newer APK (see server README: data/app/latest.json).
/// Family members install the app outside any store, so the app updates itself.
Future<void> checkForUpdate(BuildContext context, WidgetRef ref, {bool silent = false}) async {
  final session = ref.read(sessionProvider);
  final api = session.api;
  if (api == null) return;
  try {
    final info = await PackageInfo.fromPlatform();
    final v = await api.appVersion();
    final latestBuild = (v['build'] as num?)?.toInt() ?? 0;
    final current = int.tryParse(info.buildNumber) ?? 0;
    if (v['available'] != true || latestBuild <= current || v['url'] == null) {
      if (!silent && context.mounted) showToast(context, '已经是最新版本（${info.version}）');
      return;
    }
    if (!context.mounted) return;
    final url = '${session.server}${v['url']}';
    final force = v['force'] == true;
    final go = await showDialog<bool>(
      context: context,
      barrierDismissible: !force,
      builder: (ctx) => AlertDialog(
        title: Text('发现新版本 ${v['version'] ?? ''}'),
        content: SingleChildScrollView(
          child: Text(
            '${(v['notes'] as String?)?.trim().isNotEmpty == true ? '${v['notes']}\n\n' : ''}点「立即更新」下载安装包，下载完成后点开安装即可，聊天记录不会丢失。',
            style: const TextStyle(fontSize: 16, height: 1.55),
          ),
        ),
        actions: [
          if (!force) TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('以后再说')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('立即更新')),
        ],
      ),
    );
    if (go == true) await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
  } catch (_) {
    if (!silent && context.mounted) showToast(context, '检查更新失败，请检查网络');
  }
}

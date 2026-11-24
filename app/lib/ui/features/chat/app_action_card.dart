import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/models.dart';
import '../../../platform/platform_capabilities.dart';
import '../../../state/app_actions.dart';
import '../../../state/session.dart';

/// Shows a device action receipt with accessible touch and mouse undo on its originating device.
class AppActionCard extends ConsumerStatefulWidget {
  const AppActionCard({super.key, required this.messageId, required this.component});
  final String messageId;
  final MessageComponent component;

  @override
  ConsumerState<AppActionCard> createState() => _AppActionCardState();
}

class _AppActionCardState extends ConsumerState<AppActionCard> {
  bool _busy = false;

  Future<void> _undo() async {
    setState(() => _busy = true);
    try {
      await ref.read(appActionsProvider).undo(widget.messageId, widget.component);
    } catch (_) {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('暂时无法撤销，请稍后重试')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final component = widget.component;
    final caps = ref.watch(platformCapabilitiesProvider);
    final isTarget = component.data['target_device_id'] == ref.watch(deviceIdProvider);
    final status = component.state['status'];
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('${component.data['summary'] ?? '应用设置'}'),
            if (isTarget && status == 'applied')
              Tooltip(
                message: '恢复调整前的设置',
                child: TextButton(onPressed: _busy ? null : _undo, child: const Text('撤销')),
              ),
            if (isTarget && status == 'pending') const Text('等待应用'),
            if (isTarget && status == 'undone') const Text('已撤销'),
            if (isTarget && status == 'unsupported') Text('这台${caps.deviceNoun}不支持'),
          ],
        ),
      ),
    );
  }
}

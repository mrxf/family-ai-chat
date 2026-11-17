import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../core/models.dart';
import '../../state/session.dart';
import '../theme.dart';
import '../widgets.dart';
import 'common.dart';

final _usageProvider = FutureProvider.autoDispose.family<(UsageReport, List<User>), int>((ref, days) async {
  final api = ref.watch(apiProvider);
  final (report, users) = await (api.adminUsage(days), api.adminUsers()).wait;
  return (report, users);
});

/// 设置 → 用量与花费.
class UsagePage extends ConsumerStatefulWidget {
  const UsagePage({super.key});

  @override
  ConsumerState<UsagePage> createState() => _UsagePageState();
}

class _UsagePageState extends ConsumerState<UsagePage> {
  var _days = 30;

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_usageProvider(_days));
    return Scaffold(
      appBar: AppBar(title: const Text('用量与花费')),
      body: Column(children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 4),
          child: Choice<int>(value: _days, options: const [(7, '最近 7 天'), (30, '最近 30 天'), (90, '最近 90 天')], onChanged: (v) => setState(() => _days = v)),
        ),
        Expanded(
          child: data.when(
            skipLoadingOnReload: true,
            loading: () => const LoadingView(),
            error: (e, _) => ErrorView(message: ApiError.from(e).message, onRetry: () => ref.invalidate(_usageProvider(_days))),
            data: (d) => RefreshIndicator(onRefresh: () => ref.refresh(_usageProvider(_days).future), child: _body(context, d.$1, d.$2)),
          ),
        ),
      ]),
    );
  }

  Widget _body(BuildContext context, UsageReport r, List<User> users) {
    final p = context.pal;
    if (r.total.messages == 0) {
      return ListView(children: [
        const SizedBox(height: 80),
        const Center(child: Mascot(size: 96)),
        const SizedBox(height: 16),
        Text('这段时间还没有人用过', textAlign: TextAlign.center, style: TextStyle(color: p.muted, fontSize: 16)),
      ]);
    }
    final byId = {for (final u in users) u.id: u};
    final hasCost = r.total.cost > 0;
    final unpriced = r.models.where((m) => m.cost == 0 && m.tokens > 0).toList();
    double metric(UsageRow x) => hasCost ? x.cost : x.tokens.toDouble();
    final maxUser = r.users.fold<double>(0, (a, x) => metric(x) > a ? metric(x) : a);
    final maxModel = r.models.fold<double>(0, (a, x) => metric(x) > a ? metric(x) : a);

    return ListView(padding: const EdgeInsets.fromLTRB(16, 12, 16, 40), children: [
      // ---- summary ----
      Container(
        padding: const EdgeInsets.all(20),
        decoration: BoxDecoration(
          gradient: LinearGradient(colors: [p.accentSoft, p.surface], begin: Alignment.topLeft, end: Alignment.bottomRight),
          borderRadius: BorderRadius.circular(22),
          border: Border.all(color: p.line),
        ),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('全家 $_days 天${hasCost ? '花费' : '用量'}', style: TextStyle(fontSize: 14, color: p.muted, fontWeight: FontWeight.w600)),
          const SizedBox(height: 6),
          Text(hasCost ? fmtUsd(r.total.cost) : '${fmtTokens(r.total.tokens)} tokens',
              style: TextStyle(fontSize: 34, fontWeight: FontWeight.w800, color: p.text, letterSpacing: -0.5)),
          const SizedBox(height: 6),
          Text(
            '${r.total.messages} 次回答 · ${hasCost ? '${fmtTokens(r.total.tokens)} tokens · ' : ''}'
            '平均每天 ${hasCost ? fmtUsd(r.total.cost / _days) : '${(r.total.messages / _days).toStringAsFixed(1)} 次'}',
            style: TextStyle(fontSize: 14, color: p.muted),
          ),
        ]),
      ),
      if (unpriced.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(color: p.surfaceAlt, borderRadius: BorderRadius.circular(14)),
            child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Icon(Icons.info_outline_rounded, size: 18, color: p.muted),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '${unpriced.map((m) => m.name.isEmpty ? '已删除的模型' : '「${m.name}」').take(3).join('、')}${unpriced.length > 3 ? ' 等' : ''}没有价格信息，花费没算进去。可以在模型设置里填写单价。',
                  style: TextStyle(fontSize: 13, color: p.muted, height: 1.45),
                ),
              ),
            ]),
          ),
        ),

      // ---- per member ----
      const SectionHeader('按家人'),
      for (final row in r.users)
        _Bar(
          leading: Avatar(label: byId[row.id]?.displayName ?? '?', emoji: byId[row.id]?.avatar ?? '', size: 36),
          title: byId[row.id]?.displayName ?? '已删除的成员',
          value: hasCost ? fmtUsd(row.cost) : '${fmtTokens(row.tokens)} tokens',
          detail: '${row.messages} 次回答 · 输入 ${fmtTokens(row.promptTokens)} · 输出 ${fmtTokens(row.completionTokens)}',
          ratio: maxUser == 0 ? 0 : metric(row) / maxUser,
        ),

      // ---- per model ----
      const SectionHeader('按模型'),
      for (final row in r.models)
        _Bar(
          title: row.name.isEmpty ? '已删除的模型' : row.name,
          value: row.cost > 0 ? fmtUsd(row.cost) : '${fmtTokens(row.tokens)} tokens',
          detail: '${row.messages} 次回答 · 输入 ${fmtTokens(row.promptTokens)} · 输出 ${fmtTokens(row.completionTokens)}',
          ratio: maxModel == 0 ? 0 : metric(row) / maxModel,
        ),
    ]);
  }
}

class _Bar extends StatelessWidget {
  const _Bar({this.leading, required this.title, required this.value, required this.detail, required this.ratio});
  final Widget? leading;
  final String title, value, detail;
  final double ratio;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(18), border: Border.all(color: p.line)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          if (leading != null) ...[leading!, const SizedBox(width: 10)],
          Expanded(child: Text(title, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600))),
          Text(value, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: p.text)),
        ]),
        const SizedBox(height: 10),
        ClipRRect(
          borderRadius: BorderRadius.circular(6),
          child: TweenAnimationBuilder<double>(
            tween: Tween(begin: 0, end: ratio.clamp(0, 1)),
            duration: const Duration(milliseconds: 500),
            curve: Curves.easeOutCubic,
            builder: (_, v, _) => LinearProgressIndicator(value: v, minHeight: 8, backgroundColor: p.surfaceAlt, color: p.accent),
          ),
        ),
        const SizedBox(height: 8),
        Text(detail, style: TextStyle(fontSize: 13, color: p.muted)),
      ]),
    );
  }
}

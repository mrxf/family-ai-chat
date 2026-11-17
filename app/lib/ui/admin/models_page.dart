import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../state/catalog.dart';
import '../../state/session.dart';
import '../theme.dart';
import '../sheets.dart';
import '../widgets.dart';
import 'common.dart';
import 'model_edit_page.dart';
import 'provider_pages.dart';

/// 设置 → 模型与供应商: who answers the family, which models they see, and where they come from.
class ModelsPage extends ConsumerWidget {
  const ModelsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final catalog = ref.watch(catalogProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('模型与供应商')),
      body: catalog.when(
        skipLoadingOnReload: true,
        loading: () => const LoadingView(),
        error: (e, _) => ErrorView(message: ApiError.from(e).message, onRetry: () => ref.invalidate(catalogProvider)),
        data: (c) => RefreshIndicator(
          onRefresh: () => ref.read(catalogProvider.notifier).reload(),
          child: c.providers.isEmpty ? _Empty() : _Body(c),
        ),
      ),
    );
  }
}

class _Empty extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return ListView(padding: const EdgeInsets.fromLTRB(28, 48, 28, 40), children: [
      const Center(child: Floating(child: Mascot(size: 120))),
      const SizedBox(height: 20),
      const Text('还没有接入模型服务', textAlign: TextAlign.center, style: TextStyle(fontSize: 22, fontWeight: FontWeight.w800)),
      const SizedBox(height: 10),
      Text('添加一个供应商（比如 OpenRouter、DeepSeek），填上 API Key，再挑几个模型，家人就能开始聊天了。',
          textAlign: TextAlign.center, style: TextStyle(fontSize: 15.5, color: p.muted, height: 1.6)),
      const SizedBox(height: 28),
      PrimaryButton(label: '添加供应商', icon: Icons.add_rounded, onTap: () => openAddProvider(context)),
    ]);
  }
}

class _Body extends ConsumerWidget {
  const _Body(this.c);
  final Catalog c;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final s = c.settings;
    String nameOf(String id, String empty) => id.isEmpty ? empty : (c.model(id)?.displayName ?? '已删除');
    final noUsable = c.usableModels.isEmpty;
    final chat = c.chatModels, images = c.imageModels;

    return CustomScrollView(slivers: [
      SliverPadding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 0),
        sliver: SliverList.list(children: [
          if (noUsable)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: ResultBanner(ok: false, title: '家人现在没有可用的模型', lines: [
                chat.isEmpty ? '到下面的供应商里添加几个模型。' : '请至少启用一个模型，并确认它所在的供应商没有停用。',
              ]),
            ),
          const SectionHeader('全家使用'),
          GroupCard(children: [
            InfoRow(
              icon: Icons.star_rounded,
              title: '默认模型',
              subtitle: '家人开新对话时默认用它',
              value: nameOf(s.defaultModelId, '未设置'),
              valueColor: s.defaultModelId.isEmpty ? p.bad : p.text,
              onTap: noUsable ? null : () => _assign(context, ref, 'default_model_id', '全家默认模型', s.defaultModelId, allowNone: false),
            ),
            InfoRow(
              icon: Icons.health_and_safety_rounded,
              title: '备用模型',
              subtitle: '默认模型出错时自动换它回答，家人不会看到报错',
              value: nameOf(s.fallbackModelId, '不启用'),
              onTap: noUsable ? null : () => _assign(context, ref, 'fallback_model_id', '备用模型', s.fallbackModelId, noneLabel: '不启用备用模型'),
            ),
            InfoRow(
              icon: Icons.short_text_rounded,
              title: '起标题的模型',
              subtitle: '自动给对话起标题，选便宜、快的就行',
              value: nameOf(s.titleModelId, '跟随对话'),
              onTap: noUsable ? null : () => _assign(context, ref, 'title_model_id', '起标题的模型', s.titleModelId, noneLabel: '跟随当前对话的模型'),
            ),
          ]),
          if (chat.isNotEmpty)
            SectionHeader('家人能看到的模型', subtitle: '按住右边的把手拖动排序，开关控制家人能不能选', trailing: Text('${c.usableModels.length} 个', style: TextStyle(color: p.muted))),
        ]),
      ),
      if (chat.isNotEmpty)
        SliverPadding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          sliver: SliverReorderableList(
            itemCount: chat.length,
            onReorderItem: (from, to) {
              final ids = chat.map((m) => m.id).toList();
              ids.insert(to, ids.removeAt(from));
              HapticFeedback.selectionClick();
              ref.read(catalogProvider.notifier).reorder(ids).catchError((Object e) {
                if (context.mounted) showToast(context, ApiError.from(e).message);
              });
            },
            proxyDecorator: (child, _, anim) => AnimatedBuilder(
              animation: anim,
              builder: (_, child) => Transform.scale(scale: 1 + 0.03 * anim.value, child: Material(color: Colors.transparent, elevation: 6 * anim.value, borderRadius: BorderRadius.circular(18), child: child)),
              child: child,
            ),
            itemBuilder: (context, i) {
              final m = chat[i];
              return Padding(
                key: ValueKey(m.id),
                padding: const EdgeInsets.only(bottom: 8),
                child: ModelRow(model: m, catalog: c, dragIndex: i),
              );
            },
          ),
        ),
      SliverPadding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 40),
        sliver: SliverList.list(children: [
          SectionHeader('画图', subtitle: images.isEmpty ? '家人说「帮我画一张…」时，小暖会自动调用画图模型。先到供应商里添加一个画图模型（比如 gpt-image）' : '家人说「帮我画一张…」时，小暖会自动调用它画'),
          GroupCard(children: [
            InfoRow(
              icon: Icons.palette_rounded,
              title: '画图模型',
              subtitle: images.isEmpty ? '还没有添加' : '用哪个模型画',
              value: s.imageModelId.isEmpty ? '未开通' : nameOf(s.imageModelId, '未开通'),
              valueColor: c.imageModel == null ? p.muted : p.text,
              onTap: images.isEmpty
                  ? () => showToast(context, '先到下面的供应商里，挑选一个画图模型（列表里标着「画图」）')
                  : () => _assign(context, ref, 'image_model_id', '画图模型', s.imageModelId, noneLabel: '不开通画图', image: true),
            ),
            if (s.imageModelId.isNotEmpty)
              InfoRow(
                icon: Icons.event_repeat_rounded,
                title: '每人每天',
                subtitle: '最多画几张，管理员不受限',
                value: s.imageDailyLimit > 0 ? '${s.imageDailyLimit} 张' : '不限',
                onTap: () => _pickDailyLimit(context, ref, s.imageDailyLimit),
              ),
          ]),
          for (final m in images) Padding(padding: const EdgeInsets.only(top: 8), child: ModelRow(model: m, catalog: c)),
          SectionHeader('供应商',
              subtitle: '模型从这里来，费用也由它们收取',
              trailing: TextButton.icon(onPressed: () => openAddProvider(context), icon: const Icon(Icons.add_rounded), label: const Text('添加'))),
          for (final pr in c.providers)
            Padding(padding: const EdgeInsets.only(bottom: 10), child: ProviderCard(provider: pr)),
        ]),
      ),
    ]);
  }

  Future<void> _pickDailyLimit(BuildContext context, WidgetRef ref, int current) async {
    Future<void> save(int n) async {
      try {
        final s = await ref.read(apiProvider).adminPutModelSettings({'image_daily_limit': '$n'});
        ref.read(catalogProvider.notifier).setSettings(s);
        if (context.mounted) showToast(context, '已保存');
      } on ApiError catch (e) {
        if (context.mounted) showToast(context, e.message);
      }
    }

    SheetAction option(int n) => SheetAction(n == current ? Icons.check_circle_rounded : Icons.circle_outlined, n == 0 ? '不限' : '$n 张', () => save(n));
    await showActionSheet(context, title: '每人每天最多画几张（画好的才算，失败不算）', actions: [
      for (final n in const [0, 5, 10, 20]) option(n),
      if (current > 0 && !const [5, 10, 20].contains(current)) option(current),
      SheetAction(Icons.edit_rounded, '自定义…', () async {
        final v = await showTextEditor(context, title: '每人每天最多画几张', initial: current > 0 ? '$current' : '', maxLines: 1, hint: '填数字，0 表示不限');
        if (v == null) return;
        final n = int.tryParse(v.trim());
        if (n == null || n < 0) {
          if (context.mounted) showToast(context, '请填一个数字');
          return;
        }
        await save(n);
      }),
    ]);
  }

  Future<void> _assign(BuildContext context, WidgetRef ref, String key, String title, String current,
      {bool allowNone = true, String noneLabel = '不使用', bool image = false}) async {
    final picked = await showAdminModelChooser(context, c, title: title, current: current, noneLabel: allowNone ? noneLabel : null, image: image);
    if (picked == null || picked == current) return;
    try {
      final s = await ref.read(apiProvider).adminPutModelSettings({key: picked});
      ref.read(catalogProvider.notifier).setSettings(s);
      await ref.read(catalogProvider.notifier).reload();
      if (context.mounted) showToast(context, '已保存');
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }
}

/// One model: name, provider, tags and an enable switch. Tap to edit.
class ModelRow extends ConsumerWidget {
  const ModelRow({super.key, required this.model, required this.catalog, this.dragIndex, this.showProvider = true});
  final AdminModel model;
  final Catalog catalog;
  final int? dragIndex;
  final bool showProvider;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = context.pal;
    final m = model;
    final provider = catalog.provider(m.providerId);
    final providerOff = provider != null && !provider.enabled;
    final visible = m.isImage ? catalog.enabledNow(m) : catalog.usable(m);
    final isDefault = catalog.settings.defaultModelId == m.id;
    final isFallback = catalog.settings.fallbackModelId == m.id;
    return Pressable(
      scale: 0.985,
      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => ModelEditPage(modelId: m.id))),
      child: Container(
        padding: EdgeInsets.fromLTRB(16, 12, dragIndex == null ? 8 : 4, 12),
        decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(18), border: Border.all(color: isDefault ? p.accent.withValues(alpha: 0.5) : p.line)),
        child: Row(children: [
          Expanded(
            child: AnimatedOpacity(
              duration: const Duration(milliseconds: 200),
              opacity: visible ? 1 : 0.5,
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Row(children: [
                  Flexible(child: Text(m.displayName, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600))),
                  if (isDefault) ...[const SizedBox(width: 6), Tag('默认', color: p.accent)],
                  if (isFallback) ...[const SizedBox(width: 6), const Tag('备用', color: Color(0xFF0EA5E9))],
                ]),
                const SizedBox(height: 4),
                Text(
                  [if (showProvider) provider?.name ?? '?', m.upstreamModel].join(' · '),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13, color: p.muted),
                ),
                const SizedBox(height: 7),
                Wrap(spacing: 6, runSpacing: 6, children: [
                  if (providerOff) Tag('供应商已停用', color: p.bad),
                  if (m.isImage) Tag('画图', icon: Icons.palette_outlined, color: const Color(0xFFDB2777)),
                  if (m.isImage && catalog.settings.imageModelId == m.id) Tag('正在使用', color: p.accent),
                  if (!m.isImage) ...modelTags(context, vision: m.vision, think: m.think),
                ]),
              ]),
            ),
          ),
          Switch(
            value: m.enabled,
            onChanged: providerOff
                ? null
                : (v) async {
                    try {
                      await ref.read(apiProvider).adminUpdateModel(m.id, {'enabled': v});
                      await ref.read(catalogProvider.notifier).reload();
                    } on ApiError catch (e) {
                      if (context.mounted) showToast(context, e.message);
                    }
                  },
          ),
          if (dragIndex != null)
            ReorderableDragStartListener(
              index: dragIndex!,
              child: Padding(padding: const EdgeInsets.all(8), child: Icon(Icons.drag_indicator_rounded, color: p.faint)),
            ),
        ]),
      ),
    );
  }
}

class ProviderCard extends StatelessWidget {
  const ProviderCard({super.key, required this.provider});
  final AdminProvider provider;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final pr = provider;
    return Pressable(
      scale: 0.985,
      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => ProviderDetailPage(providerId: pr.id))),
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(20), border: Border.all(color: p.line)),
        child: Row(children: [
          Opacity(opacity: pr.enabled ? 1 : 0.45, child: ProviderIcon(kind: pr.kind, size: 48)),
          const SizedBox(width: 14),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                Flexible(child: Text(pr.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600))),
                if (!pr.enabled) ...[const SizedBox(width: 6), Tag('已停用', color: p.bad)],
              ]),
              const SizedBox(height: 3),
              Text(pr.host, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, color: p.muted)),
              const SizedBox(height: 3),
              Text(
                pr.modelCount == 0 ? '还没有添加模型' : '${pr.enabledModelCount} / ${pr.modelCount} 个模型启用 · Key ${pr.apiKeyHint}',
                style: TextStyle(fontSize: 13, color: pr.modelCount == 0 ? p.accent : p.muted),
              ),
            ]),
          ),
          Icon(Icons.chevron_right_rounded, color: p.faint),
        ]),
      ),
    );
  }
}

/// Bottom sheet listing the family-visible models, grouped by provider.
/// Returns the chosen id, '' for [noneLabel], or null when dismissed.
Future<String?> showAdminModelChooser(BuildContext context, Catalog c, {required String title, required String current, String? noneLabel, bool image = false}) {
  return showModalBottomSheet<String>(
    context: context,
    isScrollControlled: true,
    builder: (ctx) {
      final p = ctx.pal;
      Widget option({required String id, required String name, String? sub, List<Widget> tags = const []}) {
        final selected = id == current;
        return Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Pressable(
            scale: 0.98,
            onTap: () => Navigator.pop(ctx, id),
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 180),
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: selected ? p.accentSoft : p.surfaceAlt,
                borderRadius: BorderRadius.circular(16),
                border: Border.all(color: selected ? p.accent : Colors.transparent, width: 1.5),
              ),
              child: Row(children: [
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(name, style: const TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600)),
                    if (sub != null && sub.isNotEmpty) ...[const SizedBox(height: 3), Text(sub, style: TextStyle(fontSize: 13.5, color: p.muted))],
                    if (tags.isNotEmpty) ...[const SizedBox(height: 6), Wrap(spacing: 6, runSpacing: 6, children: tags)],
                  ]),
                ),
                if (selected) Icon(Icons.check_circle_rounded, color: p.accent),
              ]),
            ),
          ),
        );
      }

      final groups = <Widget>[];
      for (final pr in c.providers.where((x) => x.enabled)) {
        final ms = c.modelsOf(pr.id).where((m) => m.enabled && m.isImage == image).toList();
        if (ms.isEmpty) continue;
        groups.add(Padding(
          padding: const EdgeInsets.fromLTRB(4, 10, 4, 8),
          child: Row(children: [
            ProviderIcon(kind: pr.kind, size: 20),
            const SizedBox(width: 8),
            Text(pr.name, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: p.muted)),
          ]),
        ));
        for (final m in ms) {
          groups.add(option(id: m.id, name: m.displayName, sub: m.description, tags: modelTags(ctx, vision: m.vision, think: m.think)));
        }
      }
      return DraggableScrollableSheet(
        expand: false,
        initialChildSize: 0.7,
        maxChildSize: 0.92,
        minChildSize: 0.4,
        builder: (_, scroll) => ListView(
          controller: scroll,
          padding: EdgeInsets.fromLTRB(16, 0, 16, 16 + MediaQuery.paddingOf(ctx).bottom),
          children: [
            Padding(padding: const EdgeInsets.fromLTRB(4, 0, 4, 8), child: Text(title, style: Theme.of(ctx).textTheme.titleMedium)),
            if (noneLabel != null) option(id: '', name: noneLabel),
            ...groups,
          ],
        ),
      );
    },
  );
}

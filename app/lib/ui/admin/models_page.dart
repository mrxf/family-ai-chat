import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../core/models.dart';
import '../../state/catalog.dart';
import '../../state/session.dart';
import '../icons.dart';
import '../sheets.dart';
import '../theme.dart';
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
      Text('还没有接入模型服务', textAlign: TextAlign.center, style: TextStyle(fontSize: 21, fontWeight: FontWeight.w600, color: p.text)),
      const SizedBox(height: 10),
      Text(
        '添加一个供应商（比如 OpenRouter、DeepSeek），填上 API Key，再挑几个模型，家人就能开始聊天了。',
        textAlign: TextAlign.center,
        style: TextStyle(fontSize: 15, color: p.muted, height: 1.6),
      ),
      const SizedBox(height: 28),
      PrimaryButton(label: '添加供应商', icon: AppIcons.plus, onTap: () => openAddProvider(context)),
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
    String nameOf(String id, String empty) {
      if (id.isEmpty) return empty;
      if (id == kAutoModelId) return '自动选模型';
      return c.model(id)?.displayName ?? '已删除';
    }
    final noUsable = c.usableModels.isEmpty;
    final chat = c.chatModels, images = c.imageModels, embeddings = c.embeddingModels;

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
              icon: AppIcons.star,
              title: '默认模型',
              subtitle: '家人开新对话时默认用它',
              value: nameOf(s.defaultModelId, '未设置'),
              valueColor: s.defaultModelId.isEmpty ? p.bad : (s.defaultModelId == kAutoModelId ? p.accent : p.muted),
              onTap: noUsable
                  ? null
                  : () => _assign(context, ref, 'default_model_id', '全家默认模型', s.defaultModelId, allowNone: false, allowAuto: s.autoEnabled),
            ),
            InfoRow(
              icon: AppIcons.shield,
              title: '备用模型',
              subtitle: '默认模型出错时自动换它回答，家人不会看到报错',
              value: nameOf(s.fallbackModelId, '不启用'),
              valueColor: s.fallbackModelId.isEmpty ? p.faint : p.muted,
              onTap: noUsable ? null : () => _assign(context, ref, 'fallback_model_id', '备用模型', s.fallbackModelId, noneLabel: '不启用备用模型'),
            ),
            InfoRow(
              icon: AppIcons.rename,
              title: '起标题的模型',
              subtitle: '自动给对话起标题，选便宜、快的就行',
              value: nameOf(s.titleModelId, '跟随对话'),
              valueColor: s.titleModelId.isEmpty ? p.faint : p.muted,
              onTap: noUsable ? null : () => _assign(context, ref, 'title_model_id', '起标题的模型', s.titleModelId, noneLabel: '跟随当前对话的模型'),
            ),
          ]),
          const SectionHeader(
            'Auto 智能选模型',
            subtitle: '家人不用懂各种模型代号，由服务端根据问题难度、模型介绍与内置知识库自动挑模型',
          ),
          GroupCard(children: [
            SwitchRow(
              icon: AppIcons.sparkles,
              title: '开启 Auto 模式',
              desc: '开启后家人在聊天顶部可选「自动选模型」，也可设为全家默认',
              value: s.autoEnabled,
              onChanged: noUsable ? null : (v) => _toggleAuto(context, ref, v, s),
            ),
            if (s.autoEnabled) ...[
              InfoRow(
                icon: AppIcons.cpu,
                title: 'Auto 判断模型',
                subtitle: '负责分析问题并挑模型，建议选反应快、便宜的大语言模型',
                value: nameOf(s.autoModelId, '请选择'),
                valueColor: s.autoModelId.isEmpty ? p.bad : p.muted,
                onTap: () => _assign(context, ref, 'auto_model_id', 'Auto 判断模型（建议选快、便宜的模型）', s.autoModelId, allowNone: false),
              ),
              InfoRow(
                icon: AppIcons.route,
                title: '路由判断引擎',
                subtitle: '结合管理员写的适用场景与服务端御三家/开源模型兜底库做判断；已预留未来接入 JEV 模型',
                value: s.autoRouterKind == 'jev' ? 'JEV 模型' : '大语言模型',
                onTap: () => _showRouterEngineSheet(context, s.autoRouterKind),
              ),
              SwitchRow(
                icon: AppIcons.swap,
                title: '对话中动态调整模型',
                desc: s.autoDynamic
                    ? '已开启：聊的过程中若话题或难度明显变化（如从探讨物理变成辅导孩子数学题），自动升降级模型'
                    : '默认关闭：在第一个问题定下模型后保持不变，避免切换影响上下文缓存命中',
                value: s.autoDynamic,
                onChanged: (v) => _putSettings(context, ref, {'auto_dynamic': v ? '1' : '0'}),
              ),
            ],
          ]),
          const SectionHeader('记忆'),
          GroupCard(children: [
            InfoRow(
              icon: AppIcons.sparkles,
              title: '记忆整理模型',
              subtitle: s.memoryKeywordsPending > 0
                  ? '正在后台为 ${s.memoryKeywordsPending} 条记忆补充口语检索词'
                  : '从聊天中整理家人的情况并生成同义检索词',
              value: nameOf(s.memoryModelId, '跟随起标题的模型'),
              valueColor: s.memoryModelId.isEmpty ? p.faint : p.muted,
              onTap: () => _assign(context, ref, 'memory_model_id', '记忆整理模型', s.memoryModelId, noneLabel: '跟随起标题的模型'),
            ),
            InfoRow(
              icon: AppIcons.manageSearch,
              title: '记忆检索模型',
              subtitle: s.embeddingModelId.isEmpty
                  ? (embeddings.isEmpty
                      ? '当前使用关键词 + AI 主动检索；可在供应商中添加 bge-m3 等向量模型增强口语召回'
                      : '当前仅用关键词 + AI 主动检索，点此启用已添加的向量模型')
                  : (s.memoryActiveCount > 0
                      ? '向量 + 关键词混合检索（已向量化 ${s.memoryEmbeddedCount}/${s.memoryActiveCount} 条）'
                      : '向量 + 关键词混合查找相关记忆'),
              value: nameOf(s.embeddingModelId, '仅关键词'),
              valueColor: s.embeddingModelId.isEmpty ? p.accent : p.muted,
              onTap: embeddings.isEmpty && s.embeddingModelId.isEmpty
                  ? () => showToast(context, '先到下面的供应商里挑选一个标着「向量」的模型（如 bge-m3、text-embedding-3-small）')
                  : () => _assign(context, ref, 'embedding_model_id', '记忆检索模型', s.embeddingModelId, noneLabel: '不用向量，只按关键词找', kind: 'embedding'),
            ),
          ]),
          if (chat.isNotEmpty)
            SectionHeader(
              '家人能看到的模型',
              subtitle: '按住右边把手拖动排序，开关控制家人能不能选',
              trailing: Tag('${c.usableModels.length} 个可用'),
            ),
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
              builder: (_, child) => Transform.scale(
                scale: 1 + 0.025 * anim.value,
                child: Material(
                  color: Colors.transparent,
                  elevation: 6 * anim.value,
                  shadowColor: p.shadow,
                  borderRadius: BorderRadius.circular(20),
                  child: child,
                ),
              ),
              child: child,
            ),
            itemBuilder: (context, i) {
              final m = chat[i];
              return Padding(
                key: ValueKey(m.id),
                padding: const EdgeInsets.only(bottom: 10),
                child: ModelRow(model: m, catalog: c, dragIndex: i),
              );
            },
          ),
        ),
      SliverPadding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 44),
        sliver: SliverList.list(children: [
          SectionHeader(
            '画图',
            subtitle: images.isEmpty
                ? '家人说「帮我画一张…」时自动调用。先到供应商里添加一个画图模型（如 gpt-image）'
                : '家人说「帮我画一张…」时，小暖会自动调用它',
          ),
          GroupCard(children: [
            InfoRow(
              icon: AppIcons.palette,
              title: '画图模型',
              subtitle: images.isEmpty ? '还没有添加' : '用哪个模型画',
              value: s.imageModelId.isEmpty ? '未开通' : nameOf(s.imageModelId, '未开通'),
              valueColor: c.imageModel == null ? p.faint : p.muted,
              onTap: images.isEmpty
                  ? () => showToast(context, '先到下面的供应商里，挑选一个画图模型（列表里标着「画图」）')
                  : () => _assign(context, ref, 'image_model_id', '画图模型', s.imageModelId, noneLabel: '不开通画图', kind: 'image'),
            ),
            if (s.imageModelId.isNotEmpty)
              InfoRow(
                icon: AppIcons.repeat,
                title: '每人每天',
                subtitle: '最多画几张，管理员不受限',
                value: s.imageDailyLimit > 0 ? '${s.imageDailyLimit} 张' : '不限',
                onTap: () => _pickDailyLimit(context, ref, s.imageDailyLimit),
              ),
          ]),
          for (final m in images) Padding(padding: const EdgeInsets.only(top: 10), child: ModelRow(model: m, catalog: c)),
          if (embeddings.isNotEmpty) const SectionHeader('向量模型', subtitle: '供记忆检索使用，不出现在家人的模型列表里'),
          for (final m in embeddings) Padding(padding: const EdgeInsets.only(top: 10), child: ModelRow(model: m, catalog: c)),
          SectionHeader(
            '供应商',
            subtitle: '模型从这里来，费用也由它们收取',
            trailing: SectionAction(
              label: '添加供应商',
              icon: AppIcons.plus,
              filled: true,
              onTap: () => openAddProvider(context),
            ),
          ),
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

    SheetAction option(int n) => SheetAction(n == current ? AppIcons.checkCircle : AppIcons.circle, n == 0 ? '不限' : '$n 张', () => save(n));
    await showActionSheet(context, title: '每人每天最多画几张（画好的才算，失败不算）', actions: [
      for (final n in const [0, 5, 10, 20]) option(n),
      if (current > 0 && !const [5, 10, 20].contains(current)) option(current),
      SheetAction(AppIcons.edit, '自定义…', () async {
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

  Future<void> _putSettings(BuildContext context, WidgetRef ref, Map<String, String> patch) async {
    try {
      final s = await ref.read(apiProvider).adminPutModelSettings(patch);
      ref.read(catalogProvider.notifier).setSettings(s);
      await ref.read(catalogProvider.notifier).reload();
      if (context.mounted) showToast(context, '已保存');
    } on ApiError catch (e) {
      if (context.mounted) showToast(context, e.message);
    }
  }

  Future<void> _toggleAuto(BuildContext context, WidgetRef ref, bool enable, ModelSettings s) async {
    if (!enable) {
      await _putSettings(context, ref, {'auto_enabled': '0'});
      return;
    }
    var judgeId = s.autoModelId;
    if (judgeId.isEmpty || c.model(judgeId) == null || !c.usable(c.model(judgeId)!)) {
      final picked = await showAdminModelChooser(
        context,
        c,
        title: '选择 Auto 的判断模型（建议选反应快、便宜的模型）',
        current: '',
      );
      if (picked == null || picked.isEmpty) return;
      judgeId = picked;
    }
    if (!context.mounted) return;
    await _putSettings(context, ref, {'auto_enabled': '1', 'auto_model_id': judgeId});
  }

  void _showRouterEngineSheet(BuildContext context, String current) {
    showActionSheet(
      context,
      title: '选择 Auto 路由判断方式',
      actions: [
        SheetAction(
          current != 'jev' ? AppIcons.checkCircle : AppIcons.circle,
          '大语言模型（当前使用）',
          () {},
          subtitle: '由所选判断模型结合管理员场景说明与服务端内置模型库挑选模型',
        ),
        SheetAction(
          AppIcons.hourglass,
          'JEV 专用路由模型（即将支持）',
          () => showToast(context, '服务端已预留 JEV 路由接口，后续版本可直接接入'),
          subtitle: '超低延迟专用判别模型接口，服务端架构已就绪',
        ),
      ],
    );
  }

  Future<void> _assign(BuildContext context, WidgetRef ref, String key, String title, String current,
      {bool allowNone = true, String noneLabel = '不使用', String kind = 'chat', bool allowAuto = false}) async {
    final picked = await showAdminModelChooser(
      context,
      c,
      title: title,
      current: current,
      noneLabel: allowNone ? noneLabel : null,
      kind: kind,
      allowAuto: allowAuto,
    );
    if (picked == null || picked == current || !context.mounted) return;
    await _putSettings(context, ref, {key: picked});
  }
}

/// One model: name, provider, calm warm tags and a sleek switch. Tap to edit.
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
    final visible = catalog.enabledNow(m);
    final isDefault = catalog.settings.defaultModelId == m.id;
    final isFallback = catalog.settings.fallbackModelId == m.id;
    final isAutoJudge = catalog.settings.autoEnabled && catalog.settings.autoModelId == m.id;

    final tags = <Widget>[
      if (providerOff) Tag('供应商已停用', color: p.bad),
      if (m.isImage) const Tag('画图', icon: AppIcons.palette),
      if (m.isEmbedding) const Tag('向量', icon: AppIcons.manageSearch),
      if (m.isEmbedding && catalog.settings.embeddingModelId == m.id) Tag('正在使用', color: p.good),
      if (m.isImage && catalog.settings.imageModelId == m.id) Tag('正在使用', color: p.good),
      if (isAutoJudge) Tag('Auto 判断', icon: AppIcons.sparkles, color: p.accent),
      if (m.isChat) ...modelTags(context, vision: m.vision, think: m.think),
    ];

    return Pressable(
      scale: 0.985,
      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => ModelEditPage(modelId: m.id))),
      child: Container(
        padding: EdgeInsets.fromLTRB(16, 13, dragIndex == null ? 10 : 6, 13),
        decoration: BoxDecoration(
          color: p.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: isDefault ? p.accent.withValues(alpha: 0.38) : p.line),
        ),
        child: Row(children: [
          Expanded(
            child: AnimatedOpacity(
              duration: const Duration(milliseconds: 200),
              opacity: visible ? 1 : 0.48,
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Row(children: [
                  Flexible(
                    child: Text(
                      m.displayName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 16.5, fontWeight: isDefault ? FontWeight.w600 : FontWeight.w500, color: p.text),
                    ),
                  ),
                  if (isDefault) ...[const SizedBox(width: 7), Tag('默认', color: p.accent)],
                  if (isFallback) ...[const SizedBox(width: 7), const Tag('备用')],
                ]),
                const SizedBox(height: 4),
                Text(
                  [if (showProvider) provider?.name ?? '?', m.upstreamModel].join(' · '),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13, color: p.muted),
                ),
                if (m.isChat && m.routingNote.isNotEmpty) ...[
                  const SizedBox(height: 3),
                  Text(
                    '适用场景：${m.routingNote}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12.5, color: p.accent.withValues(alpha: 0.9)),
                  ),
                ],
                if (tags.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Wrap(spacing: 6, runSpacing: 6, children: tags),
                ],
              ]),
            ),
          ),
          const SizedBox(width: 6),
          Transform.scale(
            scale: 0.85,
            child: Switch(
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
          ),
          if (dragIndex != null)
            ReorderableDragStartListener(
              index: dragIndex!,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(4, 8, 6, 8),
                child: AppIcon(AppIcons.dragHandle, size: 19, color: p.faint.withValues(alpha: 0.75)),
              ),
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
          ProviderIcon(kind: pr.kind, size: 46, muted: !pr.enabled),
          const SizedBox(width: 14),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                Flexible(
                  child: Text(
                    pr.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 16.5, fontWeight: FontWeight.w500, color: p.text),
                  ),
                ),
                if (!pr.enabled) ...[const SizedBox(width: 7), Tag('已停用', color: p.bad)],
              ]),
              const SizedBox(height: 3),
              Text(pr.host, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.faint)),
              const SizedBox(height: 3),
              Text(
                pr.modelCount == 0 ? '还没有添加模型，点这里挑选' : '${pr.enabledModelCount} / ${pr.modelCount} 个模型已启用 · Key ${pr.apiKeyHint}',
                style: TextStyle(fontSize: 13, color: pr.modelCount == 0 ? p.accent : p.muted),
              ),
            ]),
          ),
          AppIcon(AppIcons.chevronRight, color: p.faint, size: 18),
        ]),
      ),
    );
  }
}

/// Bottom sheet listing the family-visible models, grouped by provider.
/// Returns the chosen id, '' for [noneLabel], or null when dismissed.
Future<String?> showAdminModelChooser(
  BuildContext context,
  Catalog c, {
  required String title,
  required String current,
  String? noneLabel,
  String kind = 'chat',
  bool allowAuto = false,
}) {
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
            scale: 0.985,
            onTap: () => Navigator.pop(ctx, id),
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 180),
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: selected ? Color.lerp(p.surface, p.accentSoft, 0.45) : p.surfaceAlt,
                borderRadius: BorderRadius.circular(16),
                border: Border.all(color: selected ? p.accent.withValues(alpha: 0.6) : Colors.transparent, width: 1.3),
              ),
              child: Row(children: [
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(name, style: TextStyle(fontSize: 16, fontWeight: selected ? FontWeight.w600 : FontWeight.w500, color: p.text)),
                    if (sub != null && sub.isNotEmpty) ...[const SizedBox(height: 3), Text(sub, style: TextStyle(fontSize: 13.5, color: p.muted))],
                    if (tags.isNotEmpty) ...[const SizedBox(height: 6), Wrap(spacing: 6, runSpacing: 6, children: tags)],
                  ]),
                ),
                if (selected) AppIcon(AppIcons.checkCircle, color: p.accent, size: 20),
              ]),
            ),
          ),
        );
      }

      final groups = <Widget>[];
      for (final pr in c.providers.where((x) => x.enabled)) {
        final ms = c.modelsOf(pr.id).where((m) => m.enabled && m.kind == kind).toList();
        if (ms.isEmpty) continue;
        groups.add(Padding(
          padding: const EdgeInsets.fromLTRB(4, 12, 4, 8),
          child: Row(children: [
            ProviderIcon(kind: pr.kind, size: 20),
            const SizedBox(width: 8),
            Text(pr.name, style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: p.muted)),
          ]),
        ));
        for (final m in ms) {
          groups.add(option(id: m.id, name: m.displayName, sub: m.description, tags: m.isChat ? modelTags(ctx, vision: m.vision, think: m.think) : const []));
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
            Padding(padding: const EdgeInsets.fromLTRB(6, 0, 6, 12), child: Text(title, style: Theme.of(ctx).textTheme.titleMedium)),
            if (noneLabel != null) option(id: '', name: noneLabel),
            if (allowAuto)
              option(
                id: kAutoModelId,
                name: '自动选模型（Auto）',
                sub: '家人开新对话默认进入 Auto 模式，由服务端根据问题自动挑选模型',
                tags: [Tag('省心推荐', icon: AppIcons.sparkles, color: p.accent)],
              ),
            ...groups,
          ],
        ),
      );
    },
  );
}

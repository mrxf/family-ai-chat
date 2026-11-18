import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../state/catalog.dart';
import '../../state/session.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';
import 'common.dart';
import 'model_edit_page.dart';

final _upstreamProvider = FutureProvider.autoDispose.family<List<UpstreamItem>, String>((ref, id) => ref.watch(apiProvider).adminUpstreamModels(id));

enum _Filter { all, recommended, vision, think }

/// Lists the provider's models with suggested settings; the admin ticks a few and adds them.
class UpstreamPickerPage extends ConsumerStatefulWidget {
  const UpstreamPickerPage({super.key, required this.providerId, this.firstTime = false});
  final String providerId;
  final bool firstTime;

  @override
  ConsumerState<UpstreamPickerPage> createState() => _UpstreamPickerPageState();
}

class _UpstreamPickerPageState extends ConsumerState<UpstreamPickerPage> {
  final _search = TextEditingController();
  final _selected = <String>{};
  var _filter = _Filter.all;
  var _showOthers = false;
  var _busy = false;

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  bool _match(UpstreamItem it) {
    final q = _search.text.trim().toLowerCase();
    if (q.isNotEmpty && !it.upstreamModel.toLowerCase().contains(q) && !it.displayName.toLowerCase().contains(q)) return false;
    return switch (_filter) {
      _Filter.all => true,
      _Filter.recommended => it.recommended,
      _Filter.vision => it.vision,
      _Filter.think => it.think,
    };
  }

  Future<void> _add(List<UpstreamItem> all) async {
    final picks = all.where((it) => _selected.contains(it.upstreamModel)).toList();
    if (picks.isEmpty) return;
    final chatPicks = picks.where((it) => it.isChat).toList();
    Map<String, String> routingNotes = const {};
    if (chatPicks.isNotEmpty) {
      final res = await _promptRoutingNotes(chatPicks);
      if (res == null || !mounted) return;
      routingNotes = res;
    }
    setState(() => _busy = true);
    try {
      final before = ref.read(catalogProvider).value?.settings.defaultModelId ?? '';
      final imageBefore = ref.read(catalogProvider).value?.settings.imageModelId ?? '';
      final added = await ref.read(apiProvider).adminBatchModels(
            widget.providerId,
            picks.map((e) => e.toModelJson(routingNote: routingNotes[e.upstreamModel] ?? '')).toList(),
          );
      await ref.read(catalogProvider.notifier).reload();
      if (!mounted) return;
      HapticFeedback.mediumImpact();
      final c = ref.read(catalogProvider).value;
      final def = c?.settings.defaultModelId ?? '';
      final becameDefault = before.isEmpty && def.isNotEmpty;
      Navigator.of(context).pop();
      final img = c?.settings.imageModelId ?? '';
      final becameImage = imageBefore.isEmpty && img.isNotEmpty;
      showToast(
        context,
        '已添加 ${added.length} 个模型${becameDefault ? '，「${c?.model(def)?.displayName}」设为全家默认' : ''}${becameImage ? '，「${c?.model(img)?.displayName}」用来画图' : ''}',
      );
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Optional step when adding chat models: lets the admin describe what scenarios
  /// each model is best for (Auto mode guide), while previewing the server's built-in fallback.
  Future<Map<String, String>?> _promptRoutingNotes(List<UpstreamItem> chatPicks) async {
    final ctrls = {for (final it in chatPicks) it.upstreamModel: TextEditingController()};
    try {
      final confirmed = await showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        builder: (ctx) {
          final p = ctx.pal;
          final bottomInset = MediaQuery.viewInsetsOf(ctx).bottom;
          return SafeArea(
            child: Padding(
              padding: EdgeInsets.only(bottom: bottomInset),
              child: ConstrainedBox(
                constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(ctx).height * 0.82),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 4, 20, 6),
                      child: Text('介绍一下适用场景？（选填）', style: Theme.of(ctx).textTheme.titleMedium),
                    ),
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
                      child: Text(
                        '写一句它最擅长的场景（如「日常聊天」「医疗问题和研究」），Auto 模式会优先按你的介绍挑模型；留空则直接使用服务端内置兜底。',
                        style: TextStyle(fontSize: 13.5, color: p.muted, height: 1.45),
                      ),
                    ),
                    Flexible(
                      child: ListView.separated(
                        shrinkWrap: true,
                        padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                        itemCount: chatPicks.length,
                        separatorBuilder: (_, _) => const SizedBox(height: 10),
                        itemBuilder: (_, i) {
                          final it = chatPicks[i];
                          return Container(
                            padding: const EdgeInsets.all(14),
                            decoration: BoxDecoration(
                              color: p.surface,
                              borderRadius: BorderRadius.circular(16),
                              border: Border.all(color: p.line),
                            ),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(it.displayName, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: p.text)),
                                if (it.builtinRoutingNote.isNotEmpty) ...[
                                  const SizedBox(height: 4),
                                  Text('内置兜底：${it.builtinRoutingNote}', style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.4)),
                                ],
                                const SizedBox(height: 10),
                                AdminField(
                                  controller: ctrls[it.upstreamModel]!,
                                  label: '管理员场景说明',
                                  statusText: '选填',
                                  hint: '例如：日常闲聊 / 医疗问题和研究',
                                  maxLength: 300,
                                  maxLines: 2,
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                    ),
                    Padding(
                      padding: const EdgeInsets.fromLTRB(16, 4, 16, 14),
                      child: PrimaryButton(
                        label: '确认添加（未填项使用内置兜底）',
                        onTap: () => Navigator.pop(ctx, true),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          );
        },
      );
      if (confirmed != true) return null;
      return {
        for (final entry in ctrls.entries)
          if (entry.value.text.trim().isNotEmpty) entry.key: entry.value.text.trim(),
      };
    } finally {
      for (final c in ctrls.values) {
        c.dispose();
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final data = ref.watch(_upstreamProvider(widget.providerId));
    final provider = ref.watch(catalogProvider).value?.provider(widget.providerId);
    final all = data.value ?? const <UpstreamItem>[];

    return Scaffold(
      appBar: AppBar(
        title: Column(children: [
          const Text('选择模型'),
          if (provider != null) Text(provider.name, style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w400)),
        ]),
        actions: [
          if (widget.firstTime)
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: Text('跳过', style: TextStyle(color: p.muted)),
            ),
        ],
      ),
      bottomNavigationBar: data.hasValue && all.isNotEmpty
          ? BottomBar(
              child: Row(children: [
                Expanded(
                  child: Text(
                    _selected.isEmpty ? '建议选 2～4 个：快的日常用，聪明的解难题，再配一个能看图' : '已选 ${_selected.length} 个模型',
                    style: TextStyle(
                      fontSize: _selected.isEmpty ? 13 : 15.5,
                      color: _selected.isEmpty ? p.muted : p.text,
                      fontWeight: _selected.isEmpty ? FontWeight.w400 : FontWeight.w600,
                      height: 1.4,
                    ),
                  ),
                ),
                const SizedBox(width: 12),
                SizedBox(
                  width: 124,
                  child: PrimaryButton(label: '添加', busy: _busy, onTap: _selected.isEmpty ? null : () => _add(all)),
                ),
              ]),
            )
          : null,
      body: data.when(
        loading: () => const LoadingView(text: '正在获取模型列表…'),
        error: (e, _) => ErrorView(
          message: ApiError.from(e).message,
          onRetry: () => ref.invalidate(_upstreamProvider(widget.providerId)),
          extra: Padding(
            padding: const EdgeInsets.only(top: 8),
            child: TextButton(
              onPressed: () => Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => ModelEditPage(providerId: widget.providerId))),
              child: const Text('这个服务不提供模型列表？手动添加'),
            ),
          ),
        ),
        data: (_) => all.isEmpty
            ? ErrorView(
                message: '这个服务没有返回任何模型',
                extra: TextButton(
                  onPressed: () => Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => ModelEditPage(providerId: widget.providerId))),
                  child: const Text('手动添加模型'),
                ),
              )
            : _list(context, all),
      ),
    );
  }

  Widget _list(BuildContext context, List<UpstreamItem> all) {
    final p = context.pal;
    final shown = all.where(_match).toList();
    final rec = shown.where((e) => e.isChat && e.recommended).toList();
    final chat = shown.where((e) => e.isChat && !e.recommended).toList();
    final images = shown.where((e) => e.isImage).toList();
    final embeddings = shown.where((e) => e.isEmbedding).toList();
    final others = shown.where((e) => !e.isChat && !e.isImage && !e.isEmbedding).toList();
    final searching = _search.text.trim().isNotEmpty;

    final rows = <Widget>[];
    void group(String title, List<UpstreamItem> items, {String? subtitle}) {
      if (items.isEmpty) return;
      rows.add(SectionHeader('$title（${items.length}）', subtitle: subtitle));
      for (final it in items) {
        rows.add(_ItemTile(
          item: it,
          selected: _selected.contains(it.upstreamModel),
          onTap: it.added
              ? null
              : () => setState(() {
                    if (!_selected.remove(it.upstreamModel)) _selected.add(it.upstreamModel);
                  }),
        ));
      }
    }

    group('推荐', rec, subtitle: '主流的新模型，参数已经帮你调好');
    group(rec.isEmpty ? '对话模型' : '其他对话模型', chat);
    group('画图模型', images, subtitle: '加一个就够了：家人说「帮我画…」时，聊天模型会自动调用它');
    group('向量模型', embeddings, subtitle: '供记忆检索使用，添加后到「记忆检索模型」中指定');
    if (others.isNotEmpty) {
      if (_showOthers || searching) {
        group('其他模型', others, subtitle: '语音、视频等，不能用来聊天，一般不需要添加');
      } else {
        rows.add(Padding(
          padding: const EdgeInsets.only(top: 16),
          child: Center(
            child: SectionAction(
              label: '显示其他 ${others.length} 个模型（语音、视频等）',
              icon: AppIcons.chevronDown,
              onTap: () => setState(() => _showOthers = true),
            ),
          ),
        ));
      }
    }
    if (shown.isEmpty) {
      rows.add(Padding(
        padding: const EdgeInsets.only(top: 60),
        child: Text('没有符合条件的模型', textAlign: TextAlign.center, style: TextStyle(color: p.muted, fontSize: 15)),
      ));
    }

    return Column(children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
        child: TextField(
          controller: _search,
          onChanged: (_) => setState(() {}),
          autocorrect: false,
          style: TextStyle(fontSize: 15.5, color: p.text),
          decoration: InputDecoration(
            hintText: '搜索模型，例如 gpt、claude、gemini',
            prefixIcon: Icon(AppIcons.search, color: p.faint, size: 19),
            suffixIcon: _search.text.isEmpty
                ? null
                : IconButton(icon: Icon(AppIcons.close, size: 18, color: p.muted), onPressed: () => setState(_search.clear)),
            contentPadding: const EdgeInsets.symmetric(vertical: 12),
          ),
        ),
      ),
      SizedBox(
        height: 50,
        child: ListView(scrollDirection: Axis.horizontal, padding: const EdgeInsets.fromLTRB(16, 10, 16, 4), children: [
          for (final (f, label) in [(_Filter.all, '全部 ${all.length}'), (_Filter.recommended, '推荐'), (_Filter.vision, '能看图'), (_Filter.think, '能深度思考')])
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Pressable(
                scale: 0.96,
                onTap: () => setState(() => _filter = f),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 160),
                  alignment: Alignment.center,
                  padding: const EdgeInsets.symmetric(horizontal: 14),
                  decoration: BoxDecoration(
                    color: _filter == f ? p.accentSoft : p.surface,
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: _filter == f ? p.accent.withValues(alpha: 0.55) : p.line),
                  ),
                  child: Text(
                    label,
                    style: TextStyle(
                      fontSize: 13.5,
                      color: _filter == f ? p.accent : p.muted,
                      fontWeight: _filter == f ? FontWeight.w600 : FontWeight.w500,
                    ),
                  ),
                ),
              ),
            ),
        ]),
      ),
      Expanded(
        child: ListView(
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 28),
          children: rows,
        ),
      ),
    ]);
  }
}

class _ItemTile extends StatelessWidget {
  const _ItemTile({required this.item, required this.selected, this.onTap});
  final UpstreamItem item;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final it = item;
    final tags = <Widget>[
      if (it.isChat) ...modelTags(context, vision: it.vision, think: it.think, inPrice: it.inputPrice, outPrice: it.outputPrice),
      if (it.isEmbedding) const Tag('向量（记忆检索）', icon: AppIcons.manageSearch),
      if (it.isImage) const Tag('画图', icon: AppIcons.palette),
    ];

    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Pressable(
        scale: 0.985,
        haptic: false,
        onTap: onTap == null
            ? null
            : () {
                HapticFeedback.selectionClick();
                onTap!();
              },
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 160),
          padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
          decoration: BoxDecoration(
            color: selected ? Color.lerp(p.surface, p.accentSoft, 0.35) : p.surface,
            borderRadius: BorderRadius.circular(18),
            border: Border.all(color: selected ? p.accent.withValues(alpha: 0.55) : p.line, width: selected ? 1.3 : 1),
          ),
          child: Row(children: [
            AnimatedSwitcher(
              duration: const Duration(milliseconds: 160),
              child: Icon(
                it.added ? AppIcons.checkCircle : (selected ? AppIcons.checkCircle : AppIcons.circle),
                key: ValueKey('${it.added}$selected'),
                color: it.added ? p.faint : (selected ? p.accent : p.line),
                size: 22,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Opacity(
                opacity: it.added ? 0.52 : 1,
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Row(children: [
                    Flexible(
                      child: Text(
                        it.displayName,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                          color: p.text,
                        ),
                      ),
                    ),
                    if (it.added) ...[const SizedBox(width: 7), const Tag('已添加')],
                  ]),
                  if (it.displayName != it.upstreamModel) ...[
                    const SizedBox(height: 3),
                    Text(it.upstreamModel, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.muted, fontFamily: 'monospace')),
                  ],
                  if (tags.isNotEmpty) ...[
                    const SizedBox(height: 7),
                    Wrap(spacing: 6, runSpacing: 6, children: tags),
                  ],
                ]),
              ),
            ),
          ]),
        ),
      ),
    );
  }
}
